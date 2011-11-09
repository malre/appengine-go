// Copyright 2011 Google Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

/*
Package xmpp provides the means to send and receive instant messages
to and from users of XMPP-compatible services.

Example:
	TODO
*/
package xmpp

import (
	"fmt"
	"http"
	"os"

	"appengine"
	"appengine_internal"

	xmpp_proto "appengine_internal/xmpp"
)

// Message represents an incoming chat message.
type Message struct {
	// Sender is the JID of the sender.
	// Optional for outgoing messages.
	Sender string

	// To is the intended recipients of the message.
	// Incoming messages will have exactly one element.
	To []string

	// Body is the body of the message.
	Body string

	// Type is the message type, per RFC 3921.
	// It defaults to "chat".
	Type string

	// TODO: RawXML
}

// ChatHandler is a function that can handle an XMPP chat message.
type ChatHandler func(c appengine.Context, m *Message)

func (f ChatHandler) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	f(appengine.NewContext(r), &Message{
		Sender: r.FormValue("from"),
		To:     []string{r.FormValue("to")},
		Body:   r.FormValue("body"),
	})
}

// RegisterChatHandler arranges for h to be called for incoming XMPP messages.
// Only messages of type "chat" or "normal" will be handled.
// Any previously registered handler will be replaced.
func RegisterChatHandler(h ChatHandler) {
	http.Handle("/_ah/xmpp/message/chat/", h)
}

// SendMessageError represents a failure to send a message to one or more JIDs.
type SendMessageError map[string]os.Error

func (sme SendMessageError) String() string {
	var jid string
	var err os.Error
	for k, v := range sme {
		jid, err = k, v
		break
	}
	switch n := len(sme); n {
	case 0:
		// should not normally happen
		return "no errors"
	case 1:
		return fmt.Sprintf("xmpp: failed sending to %v: %v", jid, err)
	default:
		return fmt.Sprintf("xmpp: failed sending to %v: %v (and %d other failures)", jid, err, n-1)
	}
	panic("unreachable")
}

// Send sends a message.
func (m *Message) Send(c appengine.Context) os.Error {
	req := &xmpp_proto.XmppMessageRequest{
		Jid:  m.To,
		Body: &m.Body,
	}
	if m.Type != "" && m.Type != "chat" {
		req.Type = &m.Type
	}
	if m.Sender != "" {
		req.FromJid = &m.Sender
	}
	res := &xmpp_proto.XmppMessageResponse{}
	if err := c.Call("xmpp", "SendMessage", req, res, nil); err != nil {
		return err
	}

	var sme SendMessageError
	if len(res.Status) != len(req.Jid) {
		return fmt.Errorf("xmpp: sent message to %d JIDs, but only got %d statuses back", len(req.Jid), len(res.Status))
	}
	for i, st := range res.Status {
		if st != xmpp_proto.XmppMessageResponse_NO_ERROR {
			if sme == nil {
				sme = make(SendMessageError)
			}
			sme[req.Jid[i]] = st
		}
	}
	if len(sme) > 0 {
		return sme
	}
	return nil
}

func init() {
	appengine_internal.RegisterErrorCodeMap("xmpp", xmpp_proto.XmppServiceError_ErrorCode_name)
}
