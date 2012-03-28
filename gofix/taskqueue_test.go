// Copyright 2011 Google Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

package main

func init() {
	addTestCases(taskqueueTests, taskqueue)
}

var taskqueueTests = []testCase{
	{
		Name: "taskqueue.0",
		In: `package foo

import "appengine/taskqueue"

func f() {
	tasks, err := taskqueue.LeaseTasks(c, max, queue, time)
	tasks, err := taskqueue.LeaseTasksByTag(c, max, queue, time, tag)
	err := taskqueue.ModifyTaskLease(c, t, queue, time)
}
`,
		Out: `package foo

import "appengine/taskqueue"

func f() {
	tasks, err := taskqueue.Lease(c, max, queue, time)
	tasks, err := taskqueue.LeaseByTag(c, max, queue, time, tag)
	err := taskqueue.ModifyLease(c, t, queue, time)
}
`,
	},
}
