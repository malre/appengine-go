// Copyright 2013 Google Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

/*
Package aetest provides an appengine.Context for use in tests.

An example test file:

	package foo_test

	import (
		"testing"

		"appengine/memcache"
		"appengine/aetest"
	)

	func TestFoo(t *testing.T) {
		c, err := aetest.NewContext(nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()

		it := &memcache.Item{
			Key:   "some-key",
			Value: []byte("some-value"),
		}
		err = memcache.Set(c, it)
		if err != nil {
			t.Fatalf("Set err: %v", err)
		}
		it, err = memcache.Get(c, "some-key")
		if err != nil {
			t.Fatalf("Get err: %v; want no error", err)
		}
		if g, w := string(it.Value), "some-value" ; g != w {
			t.Errorf("retrieved Item.Value = %q, want %q", g, w)
		}
	}

The environment variable APPENGINE_API_SERVER specifies the location of the
api_server.py executable to use. If unset, the system PATH is consulted.
*/
package aetest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"

	"appengine"
	"appengine_internal"
	"code.google.com/p/goprotobuf/proto"

	basepb "appengine_internal/base"
	remoteapipb "appengine_internal/remote_api"
)

// Context is an appengine.Context that sends all App Engine API calls to an
// instance of the API server.
type Context interface {
	appengine.Context

	// Close kills the child api_server.py process,
	// releasing its resources.
	io.Closer
}

// PickPort picks a free port on which to start the API server.
var PickPort = func() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	addr := l.Addr().(*net.TCPAddr)
	return addr.Port, nil
}

// NewContext launches an instance of api_server.py and returns a Context
// that delegates all App Engine API calls to that instance.
// If opts is nil the default values are used.
func NewContext(opts *Options) (Context, error) {
	req, _ := http.NewRequest("GET", "/", nil)
	c := &context{
		appID: opts.appID(),
		req:   req,
	}
	if err := c.startChild(); err != nil {
		return nil, err
	}
	return c, nil
}

// TODO: option to pass flags to api_server.py

// Options is used to specify options when creating a Context.
type Options struct {
	// AppID specifies the App ID to use during tests.
	// By default, "testapp".
	AppID string
}

func (o *Options) appID() string {
	if o == nil || o.AppID == "" {
		return "testapp"
	}
	return o.AppID
}

// context implements appengine.Context by running an api_server.py
// process as a child and proxying all Context calls to the child.
type context struct {
	appID string
	req   *http.Request
	child *exec.Cmd
	port  int // of child api_server.py http server
	addr  string
}

func (c *context) AppID() string               { return c.appID }
func (c *context) Request() interface{}        { return c.req }
func (c *context) FullyQualifiedAppID() string { return "dev~" + c.appID }

func (c *context) logf(level, format string, args ...interface{}) {
	log.Printf(level+": "+format, args...)
}

func (c *context) Debugf(format string, args ...interface{})    { c.logf("DEBUG", format, args...) }
func (c *context) Infof(format string, args ...interface{})     { c.logf("INFO", format, args...) }
func (c *context) Warningf(format string, args ...interface{})  { c.logf("WARNING", format, args...) }
func (c *context) Errorf(format string, args ...interface{})    { c.logf("ERROR", format, args...) }
func (c *context) Criticalf(format string, args ...interface{}) { c.logf("CRITICAL", format, args...) }

// Call is an implementation of appengine.Context's Call that delegates
// to a child api_server.py instance.
func (c *context) Call(service, method string, in, out appengine_internal.ProtoMessage, opts *appengine_internal.CallOptions) error {
	if service == "__go__" && (method == "GetNamespace" || method == "GetDefaultNamespace") {
		out.(*basepb.StringProto).Value = proto.String("")
		return nil
	}
	data, err := proto.Marshal(in)
	if err != nil {
		return err
	}
	req, err := proto.Marshal(&remoteapipb.Request{
		ServiceName: proto.String(service),
		Method:      proto.String(method),
		Request:     data,
	})
	if err != nil {
		return err
	}
	url := "http://" + c.addr
	res, err := http.Post(url, "application/x-google-protobuf", bytes.NewReader(req))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if res.StatusCode != 200 {
		return fmt.Errorf("got status %d; body: %q", res.StatusCode, body)
	}
	if err != nil {
		return err
	}
	resp := &remoteapipb.Response{}
	err = proto.Unmarshal(body, resp)
	if err != nil {
		return err
	}
	if e := resp.GetApplicationError(); e != nil {
		return fmt.Errorf("remote_api error (%v): %v", *e.Code, *e.Detail)
	}
	return proto.Unmarshal(resp.Response, out)
}

// Close kills the child api_server.py process, releasing its resources.
// Close is not part of the appengine.Context interface.
func (c *context) Close() error {
	if c.child == nil {
		return nil
	}
	if p := c.child.Process; p != nil {
		p.Kill()
	}
	c.child = nil
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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

func findAPIServer() (string, error) {
	if p := os.Getenv("APPENGINE_API_SERVER"); p != "" {
		if fileExists(p) {
			return p, nil
		}
		return "", fmt.Errorf("invalid APPENGINE_API_SERVER environment variable; path %q doesn't exist", p)
	}
	return exec.LookPath("api_server.py")
}

func (c *context) startChild() (err error) {
	python, err := findPython()
	if err != nil {
		return fmt.Errorf("Could not find python interpreter: %v", err)
	}
	apiServer, err := findAPIServer()
	if err != nil {
		return fmt.Errorf("Could not find api_server.py: %v", err)
	}
	c.port, err = PickPort()
	if err != nil {
		return err
	}
	c.addr = fmt.Sprintf("127.0.0.1:%v", c.port)
	c.child = exec.Command(
		python,
		apiServer,
		fmt.Sprintf("-A%s", c.FullyQualifiedAppID()),
		fmt.Sprintf("--api_port=%v", c.port),
	)
	c.child.Stdout = os.Stdout
	c.child.Stderr = os.Stderr
	if err = c.child.Start(); err != nil {
		return err
	}

	timeout := time.After(15 * time.Second)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			conn, err := net.DialTimeout("tcp", c.addr, 400*time.Millisecond)
			if err == nil {
				conn.Close()
				return nil
			}
		case <-timeout:
			if p := c.child.Process; p != nil {
				p.Kill()
			}
			return errors.New("timeout starting child process")
		}
	}
}
