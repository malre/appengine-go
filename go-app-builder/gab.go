// Copyright 2011 Google Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

/*
go-app-builder is a program that builds Go App Engine apps.

It takes a list of source file names, loads and parses them,
deduces their package structure, creates a synthetic main package,
and finally compiles and links all these pieces.

Files named *_test.go will be ignored.

Usage:
	go-app-builder [options] [file.go ...]
*/
package main

import (
	"crypto/sha1"
	"errors"
	"flag"
	"fmt"
	"go/scanner"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

var (
	appBase         = flag.String("app_base", ".", "Path to app root. Command-line filenames are relative to this.")
	arch            = flag.String("arch", defaultArch(), `The Go architecture specifier (e.g. "5", "6", "8").`)
	binaryName      = flag.String("binary_name", "_go_app.bin", "Name of final binary, relative to --work_dir.")
	dynamic         = flag.Bool("dynamic", false, "Create a binary with a dynamic linking header.")
	extraImports    = flag.String("extra_imports", "", "A comma-separated list of extra packages to import.")
	gcFlags         = flag.String("gcflags", "", "Comma-separated list of extra compiler flags.")
	goPath          = flag.String("gopath", os.Getenv("GOPATH"), "Location of extra packages.")
	goRoot          = flag.String("goroot", os.Getenv("GOROOT"), "Root of the Go installation.")
	internalPkg     = flag.String("internal_pkg", "appengine_internal", "If set, the import path of the internal package containing Main; if empty, auto-detect.")
	ldFlags         = flag.String("ldflags", "", "Comma-separated list of extra linker flags.")
	logFile         = flag.String("log_file", "", "If set, a file to write messages to.")
	noBuildFiles    = flag.String("nobuild_files", "", "Regular expression matching files to not build.")
	pkgDupes        = flag.String("pkg_dupe_whitelist", "", "Comma-separated list of packages that are okay to duplicate.")
	printExtras     = flag.Bool("print_extras", false, "Whether to skip building and just print extra-app files.")
	printExtrasHash = flag.Bool("print_extras_hash", false, "Whether to skip building and just print a hash of the extra-app files.")
	trampoline      = flag.String("trampoline", "", "If set, a binary to invoke tools with.")
	trampolineFlags = flag.String("trampoline_flags", "", "Comma-separated flags to pass to trampoline.")
	unsafe          = flag.Bool("unsafe", false, "Permit unsafe packages.")
	useAllPackages  = flag.Bool("use_all_packages", false, "Whether to link all packages into the binary.")
	verbose         = flag.Bool("v", false, "Noisy output.")
	workDir         = flag.String("work_dir", "/tmp", "Directory to use for intermediate and output files.")
)

func defaultArch() string {
	switch runtime.GOARCH {
	case "386":
		return "8"
	case "amd64":
		return "6"
	case "arm":
		return "5"
	}
	// Default to amd64.
	return "6"
}

func fullArch(c string) string {
	switch c {
	case "5":
		return "arm"
	case "6":
		return "amd64"
	case "8":
		return "386"
	}
	return "amd64"
}

func main() {
	flag.Usage = usage
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE|os.O_SYNC, 0644)
		if err != nil {
			log.Fatalf("go-app-builder: Failed opening log file: %v", err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	app, err := ParseFiles(*appBase, flag.Args())
	if err != nil {
		if errl, ok := err.(scanner.ErrorList); ok {
			log.Printf("go-app-builder: Failed parsing input (%d error%s)", len(errl), plural(len(errl), "s"))
			for _, err := range errl {
				log.Println(err)
			}
			os.Exit(1)
		}
		log.Fatalf("go-app-builder: Failed parsing input: %v", err)
	}

	if *printExtras {
		printExtraFiles(os.Stdout, app)
		return
	}
	if *printExtrasHash {
		printExtraFilesHash(os.Stdout, app)
		return
	}

	gTimer.name = *arch + "g"
	pTimer.name = "gopack"
	lTimer.name = *arch + "l"

	err = buildApp(app)
	log.Printf("go-app-builder: build timing: %v, %v, %v", &gTimer, &pTimer, &lTimer)
	if err != nil {
		log.Fatalf("go-app-builder: %v", err)
	}
}

// Timers that are manipulated in buildApp.
var gTimer, pTimer, lTimer timer // manipulated in buildApp

func plural(n int, suffix string) string {
	if n == 1 {
		return ""
	}
	return suffix
}

func buildApp(app *App) error {
	var extra []string
	if *extraImports != "" {
		extra = strings.Split(*extraImports, ",")
	}
	mainStr, err := MakeMain(app)
	if err != nil {
		return fmt.Errorf("failed creating main: %v", err)
	}
	mainFile := filepath.Join(*workDir, "_go_main.go")
	defer os.Remove(mainFile)
	if err := ioutil.WriteFile(mainFile, []byte(mainStr), 0640); err != nil {
		return fmt.Errorf("failed writing main: %v", err)
	}
	app.Packages = append(app.Packages, &Package{
		ImportPath: "main",
		Files: []*File{
			&File{
				Name:        mainFile,
				PackageName: "main",
				// don't care about ImportPaths
			},
		},
	})

	// Common environment for compiler and linker.
	env := []string{
		"GOROOT=" + *goRoot,
		// Use a less efficient, but stricter malloc/free.
		"MALLOC_CHECK_=3",
	}
	// Since we pass -I *workDir and -L *workDir to 6g and 6l respectively,
	// we must also pass -I/-L $GOROOT/pkg/$GOOS_$GOARCH to them before that
	// to ensure that the $GOROOT versions of dupe packages take precedence.
	goRootSearchPath := filepath.Join(*goRoot, "pkg", runtime.GOOS+"_"+runtime.GOARCH)

	// Compile phase.
	compiler := toolPath(*arch + "g")
	gopack := toolPath("pack")
	for i, pkg := range app.Packages {
		objectFile := filepath.Join(*workDir, pkg.ImportPath) + "." + *arch
		objectDir, _ := filepath.Split(objectFile)
		if err := os.MkdirAll(objectDir, 0750); err != nil {
			return fmt.Errorf("failed creating directory %v: %v", objectDir, err)
		}
		args := []string{
			compiler,
			"-I", goRootSearchPath,
			"-I", *workDir,
			"-o", objectFile,
		}
		if !*unsafe {
			// reject unsafe code
			args = append(args, "-u")
		}
		if *gcFlags != "" {
			args = append(args, parseToolFlags(*gcFlags)...)
		}
		if i < len(app.Packages)-1 {
			// regular package
			base := *appBase
			if pkg.BaseDir != "" {
				base = pkg.BaseDir
			}
			for _, f := range pkg.Files {
				args = append(args, filepath.Join(base, f.Name))
			}
			// Don't generate synthetic extra imports for dupe packages.
			// They won't be linked into the binary anyway,
			// and this avoids triggering a circular import.
			if len(pkg.Files) > 0 && len(extra) > 0 && !pkg.Dupe {
				// synthetic extra imports
				extraImportsStr, err := MakeExtraImports(pkg.Files[0].PackageName, extra)
				if err != nil {
					return fmt.Errorf("failed creating extra-imports file: %v", err)
				}
				extraImportsFile := filepath.Join(*workDir, fmt.Sprintf("_extra_imports_%d.go", i))
				defer os.Remove(extraImportsFile)
				if err := ioutil.WriteFile(extraImportsFile, []byte(extraImportsStr), 0640); err != nil {
					return fmt.Errorf("failed writing extra-imports file: %v", err)
				}
				args = append(args, extraImportsFile)
			}
		} else {
			// synthetic main package
			args = append(args, mainFile)
		}
		defer os.Remove(objectFile)
		if err := gTimer.run(args, env); err != nil {
			return err
		}

		// Turn the object file into an archive file, stripped of file path information.
		// The paths we strip depends on whether this object file is based on user code
		// or the synthetic main code.
		archiveFile := filepath.Join(*workDir, pkg.ImportPath) + ".a"
		srcDir := *appBase
		if i == len(app.Packages)-1 {
			srcDir = *workDir
		}
		srcDir, _ = filepath.Abs(srcDir) // assume os.Getwd doesn't fail
		args = []string{
			gopack,
			"grcP", srcDir,
			archiveFile,
			objectFile,
		}
		defer os.Remove(archiveFile)
		if err := pTimer.run(args, env); err != nil {
			return err
		}
		if i != len(app.Packages)-1 && len(extra) > 0 {
			// Run gopack again, this time stripping the absolute workDir prefix.
			absWorkDir, _ := filepath.Abs(*workDir) // assume os.Getwd doesn't fail
			args = []string{
				gopack,
				"grcP", absWorkDir,
				archiveFile,
			}
			if err := pTimer.run(args, env); err != nil {
				return err
			}
		}
	}

	// Link phase.
	linker := toolPath(*arch + "l")
	archiveFile := filepath.Join(*workDir, app.Packages[len(app.Packages)-1].ImportPath) + ".a"
	binaryFile := filepath.Join(*workDir, *binaryName)
	args := []string{
		linker,
		"-L", goRootSearchPath,
		"-L", *workDir,
		"-o", binaryFile,
	}
	if !*dynamic {
		// force the binary to be statically linked, disable dwarf generation, and strip binary
		args = append(args, "-d", "-w", "-s")
	}
	if !*unsafe {
		// reject unsafe code
		args = append(args, "-u")
	}
	if *ldFlags != "" {
		args = append(args, parseToolFlags(*ldFlags)...)
	}
	args = append(args, archiveFile)
	if err := lTimer.run(args, env); err != nil {
		return err
	}

	// Check the final binary. A zero-length file indicates an unexpected linker failure.
	fi, err := os.Stat(binaryFile)
	if err != nil {
		return err
	}
	if fi.Size() == 0 {
		return errors.New("created binary has zero size")
	}

	return nil
}

type timer struct {
	name  string
	n     int
	total time.Duration
}

func (t *timer) run(args, env []string) error {
	start := time.Now()
	err := run(args, env)

	t.n++
	t.total += time.Since(start)

	return err
}

func (t *timer) String() string {
	return fmt.Sprintf("%d×%s (%v total)", t.n, t.name, t.total)
}

func printExtraFiles(w io.Writer, app *App) {
	for _, pkg := range app.Packages {
		if pkg.BaseDir == "" {
			continue // app file
		}
		for _, f := range pkg.Files {
			// The app-relative path should always use forward slash.
			// The code in dev_appserver only deals with those paths.
			rel := path.Join(pkg.ImportPath, f.Name)
			dst := filepath.Join(pkg.BaseDir, f.Name)
			fmt.Fprintf(w, "%s|%s\n", rel, dst)
		}
	}
}

func printExtraFilesHash(w io.Writer, app *App) {
	// Compute a hash of the extra files information, namely the name and mtime
	// of all the extra files. This is sufficient information for the dev_appserver
	// to be able to decide whether a rebuild is necessary based on GOPATH changes.
	h := sha1.New()
	sort.Sort(byImportPath(app.Packages)) // be deterministic
	for _, pkg := range app.Packages {
		if pkg.BaseDir == "" {
			continue // app file
		}
		sort.Sort(byFileName(pkg.Files)) // be deterministic
		for _, f := range pkg.Files {
			dst := filepath.Join(pkg.BaseDir, f.Name)
			fi, err := os.Stat(dst)
			if err != nil {
				log.Fatalf("go-app-builder: os.Stat(%q): %v", dst, err)
			}
			fmt.Fprintf(h, "%s: %v\n", dst, fi.ModTime())
		}
	}
	fmt.Fprintf(w, "%x", h.Sum(nil))
}

func toolPath(x string) string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return filepath.Join(*goRoot, "pkg", "tool", runtime.GOOS+"_"+fullArch(*arch), x+ext)
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage:  %s [options] <foo.go> ...\n", os.Args[0])
	flag.PrintDefaults()
}

func run(args []string, env []string) error {
	if *verbose {
		log.Printf("run %v", args)
	}
	tool := filepath.Base(args[0])
	if *trampoline != "" {
		// Add trampoline binary, its flags, and -- to the start.
		newArgs := []string{*trampoline}
		if *trampolineFlags != "" {
			newArgs = append(newArgs, strings.Split(*trampolineFlags, ",")...)
		}
		newArgs = append(newArgs, "--")
		args = append(newArgs, args...)
	}
	cmd := &exec.Cmd{
		Path:   args[0],
		Args:   args,
		Env:    env,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed running %v: %v", tool, err)
	}
	return nil
}
