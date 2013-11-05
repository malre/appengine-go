// To be placed in the output Go repo at cmd/go.

package main

import (
	"path/filepath"
)

var cmdDeploy = &Command{
	UsageLine: "deploy [deploy flags] application_dir | [yaml_files...]",
	Short:     "deploys your application to App Engine",
	Long: `
Deploy uploads your application files to Google App Engine, and then compiles
and lauches your application.

The argument to this command should be your application's root directory that
contains the app.yaml file. If you are using the Modules feature, then you may
pass multiple YAML files to serve, rather than a directory, to specify
which modules to update.

This command wraps the appcfg.py command provided as part of the App Engine
SDK. For help using that command directly, run:
  ./appcfg.py help update
  `,
	CustomFlags: true,
}

func init() {
	// break init cycle
	cmdDeploy.Run = runDeploy
}

func runDeploy(cmd *Command, args []string) {
	appcfg, err := findAppcfg()
	if err != nil {
		fatalf("goapp serve: %v", err)
	}
	runSDKTool(appcfg, append([]string{"update"}, args...))
}

func findAppcfg() (string, error) {
	devAppserver, err := findDevAppserver()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(devAppserver), "appcfg.py"), nil
}
