// Copyright 2011 Google Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

package main

import (
	"go/ast"
)

var taskqueueFix = fix{
	"taskqueue",
	"2011-09-15",
	taskqueue,
	`Update the names of appengine/taskqueue functions.`,
}

func init() {
	register(taskqueueFix)
}

var taskqueueRenames = map[string]string{
	"LeaseTasks":      "Lease",
	"LeaseTasksByTag": "LeaseByTag",
	"ModifyTaskLease": "ModifyLease",
}

func taskqueue(f *ast.File) bool {
	if !imports(f, "appengine/taskqueue") {
		return false
	}

	fixed := false
	walk(f, func(n interface{}) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !isTopName(sel.X, "taskqueue") {
			return
		}
		if q, ok := taskqueueRenames[sel.Sel.String()]; ok {
			sel.Sel.Name = q
			fixed = true
		}
	})
	return fixed
}
