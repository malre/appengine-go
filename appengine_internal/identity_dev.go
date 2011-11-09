// Copyright 2011 Google Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

package appengine_internal

import (
	"http"
	"net"
	"os"
	"strconv"
)

// These functions are the dev implementations of the wrapper functions
// in ../appengine/identity.go. See that file for commentary.

const (
	hVersionId = "X-AppEngine-Inbound-Version-Id"
)

func BackendHostname(req interface{}, name string, index int) string {
	ev := "BACKEND_PORT." + name
	if index != -1 {
		ev += "." + strconv.Itoa(index)
	}
	host, _, _ := net.SplitHostPort(DefaultVersionHostname(req))
	return host + ":" + os.Getenv(ev)
}

func DefaultVersionHostname(req interface{}) string {
	return req.(*http.Request).Host
}

func Instance() int {
	i, err := strconv.Atoi(os.Getenv("INSTANCE_ID"))
	if err != nil {
		return -1
	}
	return i
}

func VersionID(req interface{}) string {
	return req.(*http.Request).Header.Get(hVersionId)
}
