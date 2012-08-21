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
	"errors"
	"flag"
	"fmt"
	"go/scanner"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	appBase         = flag.String("app_base", ".", "Path to app root. Command-line filenames are relative to this.")
	arch            = flag.String("arch", defaultArch(), `The Go architecture specifier (e.g. "5", "6", "8").`)
	binaryName      = flag.String("binary_name", "_go_app.bin", "Name of final binary, relative to --work_dir.")
	dynamic         = flag.Bool("dynamic", false, "Create a binary with a dynamic linking header.")
	extraImports    = flag.String("extra_imports", "", "A comma-separated list of extra packages to import.")
	goRoot          = flag.String("goroot", os.Getenv("GOROOT"), "Root of the Go installation.")
	logFile         = flag.String("log_file", "", "If set, a file to write messages to.")
	pkgDupes        = flag.String("pkg_dupe_whitelist", "", "Comma-separated list of packages that are okay to duplicate.")
	trampoline      = flag.String("trampoline", "", "If set, a binary to invoke tools with.")
	trampolineFlags = flag.String("trampoline_flags", "", "Comma-separated flags to pass to trampoline.")
	unsafe          = flag.Bool("unsafe", false, "Permit unsafe packages.")
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

	if err := buildApp(app); err != nil {
		log.Fatalf("go-app-builder: Failed building app: %v", err)
	}
}

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
	mainStr, err := MakeMain(app, extra)
	if err != nil {
		return fmt.Errorf("failed creating main: %v", err)
	}
	const mainName = "_go_main.go"
	mainFile := filepath.Join(*workDir, mainName)
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
			"-I", *workDir,
			"-o", objectFile,
		}
		if !*unsafe {
			// reject unsafe code
			args = append(args, "-u")
		}
		if i < len(app.Packages)-1 {
			// regular package
			for _, f := range pkg.Files {
				args = append(args, filepath.Join(*appBase, f.Name))
			}
		} else {
			// synthetic main package
			args = append(args, mainFile)
		}
		defer os.Remove(objectFile)
		if err := run(args, env); err != nil {
			return err
		}

		// Turn the object file into an archive file, stripped of file path information.
		// The path we strip depends on whether this object file is based on user code
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
		if err := run(args, env); err != nil {
			return err
		}
	}

	// Link phase.
	linker := toolPath(*arch + "l")
	archiveFile := filepath.Join(*workDir, app.Packages[len(app.Packages)-1].ImportPath) + ".a"
	binaryFile := filepath.Join(*workDir, *binaryName)
	args := []string{
		linker,
		"-L", *workDir,
		"-o", binaryFile,
		"-w", // disable dwarf generation
	}
	if !*dynamic {
		// force the binary to be statically linked
		args = append(args, "-d")
	}
	if !*unsafe {
		// reject unsafe code
		args = append(args, "-u")
	}
	args = append(args, archiveFile)
	if err := run(args, env); err != nil {
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
