// Copyright 2011 Google Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

/*
Package log provides the means of querying an application's logs from
within an App Engine application.

Example:
	c := appengine.NewContext(r)
	query := &log.Query{
		AppLogs:  true,
		Versions: []string{"1"},
	}

	for results := query.Run(c); ; {
		record, err := results.Next()
		if err == log.Done {
			c.Infof("Done processing results")
			break
		}
		if err != nil {
			c.Errorf("Failed to retrieve next log: %v", err)
			break
		}
		c.Infof("Saw record %v", record)
	}
*/
package log

import (
	"errors"
	"strings"
	"time"

	"appengine"
	"appengine_internal"
	"code.google.com/p/goprotobuf/proto"

	log_proto "appengine_internal/log"
)

// Query defines a logs query.
type Query struct {
	// Start time specifies the earliest log to return (inclusive).
	StartTime time.Time

	// End time specifies the latest log to return (exclusive).
	EndTime time.Time

	// Incomplete controls whether active (incomplete) requests should be included.
	Incomplete bool

	// AppLogs indicates if application-level logs should be included.
	AppLogs bool

	// ApplyMinLevel indicates if MinLevel should be used to filter results.
	ApplyMinLevel bool

	// If ApplyMinLevel is true, only logs for requests with at least one
	// application log of MinLevel or higher will be returned.
	MinLevel int

	// The major version IDs whose logs should be retrieved.
	Versions []string
}

// AppLog represents a single application-level log.
type AppLog struct {
	Time    time.Time
	Level   int
	Message string
}

// Record contains all the information for a single web request.
type Record struct {
	AppID     string
	VersionID string
	RequestID []byte
	IP        string
	Nickname  string

	// The time when this request started.
	StartTime time.Time

	// The time when this request finished.
	EndTime time.Time

	// The time required to process the request.
	Latency     time.Duration
	MCycles     int64
	Method      string
	Resource    string
	HTTPVersion string
	Status      int32

	// The size of the request sent back to the client, in bytes.
	ResponseSize int64
	Referrer     string
	UserAgent    string
	URLMapEntry  string
	Combined     string
	APIMCycles   int64
	Host         string

	// The estimated cost of this request, in dollars.
	Cost              float64
	TaskQueueName     string
	TaskName          string
	WasLoadingRequest bool
	PendingTime       time.Duration
	Finished          bool
	AppLogs           []AppLog
}

// Result represents the result of a query.
type Result struct {
	logs        []*Record
	context     appengine.Context
	request     *log_proto.LogReadRequest
	resultsSeen bool
}

// Next returns the next log record,
func (qr *Result) Next() (*Record, error) {
	if len(qr.logs) > 0 {
		lr := qr.logs[0]
		qr.logs = qr.logs[1:]
		return lr, nil
	}

	if qr.request.Offset == nil && qr.resultsSeen {
		return nil, Done
	}

	if err := qr.run(); err != nil {
		return nil, err
	}

	return qr.Next()
}

// Done is returned when a query iteration has completed.
var Done = errors.New("log: query has no more results")

// protoToAppLogs takes as input an array of pointers to LogLines, the internal
// Protocol Buffer representation of a single application-level log,
// and converts it to an array of AppLogs, the external representation
// of an application-level log.
func protoToAppLogs(logLines []*log_proto.LogLine) []AppLog {
	appLogs := make([]AppLog, len(logLines))

	for i, line := range logLines {
		appLogs[i] = AppLog{
			Time:    time.Unix(0, *line.Time*1e3),
			Level:   int(*line.Level),
			Message: *line.LogMessage,
		}
	}

	return appLogs
}

// protoToRecord converts a RequestLog, the internal Protocol Buffer
// representation of a single request-level log, to a Record, its
// corresponding external representation.
func protoToRecord(rl *log_proto.RequestLog) *Record {
	finished := log_proto.Default_RequestLog_Finished
	if rl.Finished != nil {
		finished = *rl.Finished
	}

	return &Record{
		AppID:             *rl.AppId,
		VersionID:         *rl.VersionId,
		RequestID:         rl.RequestId,
		IP:                *rl.Ip,
		Nickname:          proto.GetString(rl.Nickname),
		StartTime:         time.Unix(0, *rl.StartTime*1e3),
		EndTime:           time.Unix(0, *rl.EndTime*1e3),
		Latency:           time.Duration(*rl.Latency) * time.Microsecond,
		MCycles:           *rl.Mcycles,
		Method:            *rl.Method,
		Resource:          *rl.Resource,
		HTTPVersion:       *rl.HttpVersion,
		Status:            *rl.Status,
		ResponseSize:      *rl.ResponseSize,
		Referrer:          proto.GetString(rl.Referrer),
		UserAgent:         proto.GetString(rl.UserAgent),
		URLMapEntry:       *rl.UrlMapEntry,
		Combined:          *rl.Combined,
		APIMCycles:        proto.GetInt64(rl.ApiMcycles),
		Host:              proto.GetString(rl.Host),
		Cost:              proto.GetFloat64(rl.Cost),
		TaskQueueName:     proto.GetString(rl.TaskQueueName),
		TaskName:          proto.GetString(rl.TaskName),
		WasLoadingRequest: proto.GetBool(rl.WasLoadingRequest),
		PendingTime:       time.Duration(proto.GetInt64(rl.PendingTime)) * time.Microsecond,
		Finished:          finished,
		AppLogs:           protoToAppLogs(rl.Line),
	}
}

// Run starts a query for log records, which contain request and application
// level log information.
func (params *Query) Run(c appengine.Context) *Result {
	req := &log_proto.LogReadRequest{}
	appId := c.FullyQualifiedAppID()
	req.AppId = &appId
	if !params.StartTime.IsZero() {
		req.StartTime = proto.Int64(params.StartTime.UnixNano() / 1e3)
	}
	if !params.EndTime.IsZero() {
		req.EndTime = proto.Int64(params.EndTime.UnixNano() / 1e3)
	}
	if params.Incomplete {
		req.IncludeIncomplete = &params.Incomplete
	}
	if params.AppLogs {
		req.IncludeAppLogs = &params.AppLogs
	}
	if params.ApplyMinLevel {
		req.MinimumLogLevel = proto.Int32(int32(params.MinLevel))
	}
	if params.Versions == nil {
		// If no versions were specified, default to the major version
		// used by this app.
		versionID := appengine.VersionID(c)
		if i := strings.Index(versionID, "."); i >= 0 {
			versionID = versionID[:i]
		}
		req.VersionId = []string{versionID}
	} else {
		req.VersionId = params.Versions
	}

	return &Result{context: c, request: req}
}

// run takes the query Result produced by a call to Run and updates it with
// more Records. The updated Result contains a new set of logs as well as an
// offset to where more logs can be found. We also convert the items in the
// response from their internal representations to external versions of the
// same structs.
func (r *Result) run() error {
	res := &log_proto.LogReadResponse{}
	if err := r.context.Call("logservice", "Read", r.request, res, nil); err != nil {
		return err
	}

	r.logs = make([]*Record, len(res.Log))
	r.request.Offset = res.Offset
	r.resultsSeen = true

	for i, log := range res.Log {
		r.logs[i] = protoToRecord(log)
	}

	return nil
}

func init() {
	appengine_internal.RegisterErrorCodeMap("logservice", log_proto.LogServiceError_ErrorCode_name)
}
