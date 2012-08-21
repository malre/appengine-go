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

	// RawXML is whether the body contains raw XML.
	RawXML bool
}

// Presence represents an outgoing presence update.
type Presence struct {
	// Sender is the JID (optional).
	Sender string

	// The intended recipient of the presence update.
	To string

	// Type, per RFC 3921 (optional). Defaults to "available".
	Type string

	// State of presence (optional).
	// Valid values: "away", "chat", "xa", "dnd" (RFC 3921).
	State string

	// Free text status message (optional).
	Status string
}

var (
	ErrPresenceUnavailable = errors.New("xmpp: presence unavailable")
)

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
		Jid:    m.To,
		Body:   &m.Body,
		RawXml: &m.RawXML,
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

// Invite sends an invitation. If the from address is an empty string
// the default (yourapp@appspot.com/bot) will be used.
func Invite(c appengine.Context, to, from string) error {
	req := &xmpp_proto.XmppInviteRequest{
		Jid: &to,
	}
	if from != "" {
		req.FromJid = &from
	}
	res := &xmpp_proto.XmppInviteResponse{}
	return c.Call("xmpp", "SendInvite", req, res, nil)
}

// Send sends a presence update.
func (p *Presence) Send(c appengine.Context) error {
	req := &xmpp_proto.XmppSendPresenceRequest{
		Jid: &p.To,
	}
	if p.State != "" {
		req.Show = &p.State
	}
	if p.Type != "" {
		req.Type = &p.Type
	}
	if p.Sender != "" {
		req.FromJid = &p.Sender
	}
	if p.Status != "" {
		req.Status = &p.Status
	}
	res := &xmpp_proto.XmppSendPresenceResponse{}
	return c.Call("xmpp", "SendPresence", req, res, nil)
}

// GetPresence retrieves a user's presence.
// If the from address is an empty string the default
// (yourapp@appspot.com/bot) will be used.
// Possible return values are "", "away", "dnd", "chat", "xa".
// ErrPresenceUnavailable is returned if the presence is unavailable.
func GetPresence(c appengine.Context, to string, from string) (string, error) {
	req := &xmpp_proto.PresenceRequest{
		Jid: &to,
	}
	if from != "" {
		req.FromJid = &from
	}
	res := &xmpp_proto.PresenceResponse{}
	if err := c.Call("xmpp", "GetPresence", req, res, nil); err != nil {
		return "", err
	}
	if !*res.IsAvailable || res.Presence == nil {
		return "", ErrPresenceUnavailable
	}
	switch *res.Presence {
	case xmpp_proto.PresenceResponse_NORMAL:
		return "", nil
	case xmpp_proto.PresenceResponse_AWAY:
		return "away", nil
	case xmpp_proto.PresenceResponse_DO_NOT_DISTURB:
		return "dnd", nil
	case xmpp_proto.PresenceResponse_CHAT:
		return "chat", nil
	case xmpp_proto.PresenceResponse_EXTENDED_AWAY:
		return "xa", nil
	}
	return "", fmt.Errorf("xmpp: unknown presence %v", *res.Presence)
}

func init() {
	appengine_internal.RegisterErrorCodeMap("xmpp", xmpp_proto.XmppServiceError_ErrorCode_name)
}
