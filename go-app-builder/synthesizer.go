// Copyright 2011 Google Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"text/template"
)

// MakeMain creates the synthetic main package for a Go App Engine app.
func MakeMain(app *App, extraImports []string) (string, error) {
	buf := new(bytes.Buffer)
	err := mainTemplate.Execute(buf, struct {
		App          *App
		ExtraImports []string
	}{
		app,
		extraImports,
	})
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// MakeExtraImports creates the synthetic extra-imports file for a single package.
func MakeExtraImports(packageName string, extraImports []string) (string, error) {
	buf := new(bytes.Buffer)
	err := extraImportsTemplate.Execute(buf, struct {
		PackageName  string
		ExtraImports []string
	}{
		packageName,
		extraImports,
	})
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// TODO: remove the ExtraImports from mainTemplate.

var mainTemplate = template.Must(template.New("main").Parse(
	`package main

import (
	"appengine_internal"
	{{range .ExtraImports}}
	_ "{{.}}"
	{{end}}

	// Top-level app packages
	{{range .App.RootPackages}}
	_ "{{.ImportPath}}"
	{{end}}
)

func main() {
	appengine_internal.Main()
}
`))

var extraImportsTemplate = template.Must(template.New("extraImports").Parse(
	`package {{.PackageName}}

import (
	{{range .ExtraImports}}
	_ "{{.}}"
	{{end}}
)
`))
