// Copyright 2011 Google Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

/*
Package xmpp provides the means to send and receive instant messages
to and from users of XMPP-compatible services.

To send a message,
	m := &xmpp.Message{
		To:   []string{"kaylee@example.com"},
		Body: `Hi! How's the carrot?`,
	}
	err := m.Send(c)

To receive messages,
	func init() {
		xmpp.Handle(handleChat)
	}

	func handleChat(c appengine.Context, m *xmpp.Message) {
		// ...
	}
*/
package xmpp

import (
	"errors"
	"fmt"
	"net/http"

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

// Handle arranges for f to be called for incoming XMPP messages.
// Only messages of type "chat" or "normal" will be handled.
// Any previously registered handler will be replaced.
func Handle(f func(c appengine.Context, m *Message)) {
	http.HandleFunc("/_ah/xmpp/message/chat/", func(_ http.ResponseWriter, r *http.Request) {
		f(appengine.NewContext(r), &Message{
			Sender: r.FormValue("from"),
			To:     []string{r.FormValue("to")},
			Body:   r.FormValue("body"),
		})
	})
}

// Send sends a message.
// If any failures occur with specific recipients, the error will be an appengine.MultiError.
func (m *Message) Send(c appengine.Context) error {
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

	if len(res.Status) != len(req.Jid) {
		return fmt.Errorf("xmpp: sent message to %d JIDs, but only got %d statuses back", len(req.Jid), len(res.Status))
	}
	me, any := make(appengine.MultiError, len(req.Jid)), false
	for i, st := range res.Status {
		if st != xmpp_proto.XmppMessageResponse_NO_ERROR {
			me[i] = errors.New(st.String())
			any = true
		}
	}
	if any {
		return me
	}
	return nil
}

func init() {
	appengine_internal.RegisterErrorCodeMap("xmpp", xmpp_proto.XmppServiceError_ErrorCode_name)
}
