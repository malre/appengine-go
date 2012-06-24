// Copyright 2011 Google Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

/*
Package runtime exposes information about the resource usage of the application.
*/
package runtime

import (
	"appengine"
	"appengine_internal"
	"code.google.com/p/goprotobuf/proto"

	system_proto "appengine_internal/system"
)

// Statistics represents the system's statistics.
type Statistics struct {
	// CPU records the CPU consumed by this instance, in megacycles.
	CPU struct {
		Total   float64
		Rate1M  float64 // consumption rate over one minute
		Rate10M float64 // consumption rate over ten minutes
	}
	// RAM records the memory used by the instance, in megabytes.
	RAM struct {
		Current    float64
		Average1M  float64 // average usage over one minute
		Average10M float64 // average usage over ten minutes
	}
}

func Stats(c appengine.Context) (*Statistics, error) {
	req := &system_proto.GetSystemStatsRequest{}
	res := &system_proto.GetSystemStatsResponse{}
	if err := c.Call("system", "GetSystemStats", req, res, nil); err != nil {
		return nil, err
	}
	s := &Statistics{}
	if res.Cpu != nil {
		s.CPU.Total = proto.GetFloat64(res.Cpu.Total)
		s.CPU.Rate1M = proto.GetFloat64(res.Cpu.Rate1M)
		s.CPU.Rate10M = proto.GetFloat64(res.Cpu.Rate10M)
	}
	if res.Memory != nil {
		s.RAM.Current = proto.GetFloat64(res.Memory.Current)
		s.RAM.Average1M = proto.GetFloat64(res.Memory.Average1M)
		s.RAM.Average10M = proto.GetFloat64(res.Memory.Average10M)
	}
	return s, nil
}

func init() {
	appengine_internal.RegisterErrorCodeMap("system", system_proto.SystemServiceError_ErrorCode_name)
}
