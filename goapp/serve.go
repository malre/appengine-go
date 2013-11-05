// To be placed in the output Go repo at cmd/go.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
)

var cmdServe = &Command{
	UsageLine: "serve [serve flags] application_dir | [yaml_files...]",
	Short:     "starts a local development App Engine server",
	Long: `
Serve launches your application on a local development App Engine server.

The argument to this command should be your application's root directory that
contains the app.yaml file. If you are using the Modules feature, then you may
pass multiple YAML files to serve, rather than a directory, to specify
which modules to serve.

This command wraps the dev_appserver.py command provided as part of the
App Engine SDK. For help using that command directly, run:
  ./dev_appserver.py --help
  `,
	CustomFlags: true,
}

func init() {
	// break init cycle
	cmdServe.Run = runServe
}

func runServe(cmd *Command, args []string) {
	devAppserver, err := findDevAppserver()
	if err != nil {
		fatalf("goapp serve: %v", err)
	}
	runSDKTool(devAppserver, args)
}

func runSDKTool(tool string, args []string) {
	python, err := findPython()
	if err != nil {
		fatalf("could not find python interpreter: %v", err)
	}

	toolName := filepath.Base(tool)

	cmd := exec.Command(python, tool)
	cmd.Args = append(cmd.Args, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err = cmd.Start(); err != nil {
		fatalf("error starting %s: %v", toolName, err)
	}

	// Swallow SIGINT. The tool will catch it and shut down cleanly.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		for _ = range sig {
			logf("goapp: caught SIGINT, waiting for %s to shut down", toolName)
		}
	}()

	if err = cmd.Wait(); err != nil {
		errorf("error while running %s: %v", toolName, err)
	}
}

func findPython() (path string, err error) {
	for _, name := range []string{"python2.7", "python"} {
		path, err = exec.LookPath(name)
		if err == nil {
			return
		}
	}
	return
}

func findDevAppserver() (string, error) {
	if p := os.Getenv("APPENGINE_DEV_APPSERVER"); p != "" {
		return p, nil
	}
	return "", fmt.Errorf("unable to find dev_appserver.py")
}
