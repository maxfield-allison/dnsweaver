// Package incus events streaming.
//
// The Incus REST API exposes an event stream at GET /1.0/events, upgraded to a
// WebSocket connection. Events are JSON messages describing lifecycle changes
// (instance created/started/stopped/deleted), logging, and operations. dnsweaver
// subscribes to the "lifecycle" type so it can trigger a reconcile the moment an
// instance changes, instead of waiting for the next poll tick.
//
// The stream deliberately uses github.com/coder/websocket (a minimal, near
// zero-dependency WebSocket library) rather than the official lxc/incus SDK, in
// keeping with the client's stdlib-first design (see client.go).
package incus

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/coder/websocket"
)

// EventTypeLifecycle is the Incus event type for instance lifecycle changes
// (created, started, stopped, deleted, etc.). This is the only type dnsweaver
// needs to trigger reconciliation.
const EventTypeLifecycle = "lifecycle"

// Event is a single Incus event as delivered over /1.0/events. Only the fields
// relevant to reconciliation triggering are modeled; Metadata is left raw so
// callers can decode the action for logging without this package taking on a
// dependency on the full event schema.
type Event struct {
	// Type is the event class: "lifecycle", "logging", or "operation".
	Type string `json:"type"`

	// Project is the Incus project the event originated from.
	Project string `json:"project"`

	// Metadata holds the type-specific payload. For lifecycle events it decodes
	// into LifecycleMetadata.
	Metadata json.RawMessage `json:"metadata"`
}

// LifecycleMetadata is the payload of a lifecycle Event.
type LifecycleMetadata struct {
	// Action is the lifecycle action, e.g. "instance-started",
	// "instance-stopped", "instance-created", "instance-deleted".
	Action string `json:"action"`

	// Source is the API path of the affected object, e.g. "/1.0/instances/web".
	Source string `json:"source"`
}

// Action decodes and returns the lifecycle action for the event, or "" if the
// event is not a lifecycle event or the metadata cannot be decoded.
func (e Event) Action() string {
	if e.Type != EventTypeLifecycle || len(e.Metadata) == 0 {
		return ""
	}
	var m LifecycleMetadata
	if err := json.Unmarshal(e.Metadata, &m); err != nil {
		return ""
	}
	return m.Action
}

// eventsURL builds the WebSocket URL for the events endpoint, restricted to the
// given event types and the client's configured project (or all projects when
// AllProjects is set). The base URL scheme is
// switched to ws/wss because coder/websocket dials WebSocket schemes; the custom
// transport on the client's http.Client (unix socket dialer or TLS config) is
// reused for the handshake, so socket and remote HTTPS endpoints both work.
func (c *Client) eventsURL(types []string) string {
	base := c.baseURL
	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	}

	url := base + "/1.0/events"
	params := make([]string, 0, 2)
	if len(types) > 0 {
		params = append(params, "type="+strings.Join(types, ","))
	}
	switch {
	case c.allProjects:
		params = append(params, "all-projects=true")
	case c.project != "":
		params = append(params, "project="+c.project)
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}
	return url
}

// StreamEvents connects to the Incus events endpoint and invokes handler for
// each received event of the given types until ctx is canceled or the stream
// errors. It blocks for the lifetime of the connection; callers that want
// reconnect behavior should wrap it (see WorkloadWatcher).
//
// A nil or empty types slice subscribes to lifecycle events only.
func (c *Client) StreamEvents(ctx context.Context, types []string, handler func(Event)) error {
	if len(types) == 0 {
		types = []string{EventTypeLifecycle}
	}

	url := c.eventsURL(types)

	// coder/websocket treats a non-zero http.Client.Timeout as a deadline on the
	// whole connection, which would force-drop this long-lived event stream (the
	// Incus client sets a 15s request timeout for normal REST reads). Dial with a
	// timeout-free shallow copy that keeps the same transport (unix-socket dialer
	// or TLS config).
	dialClient := c.http
	if dialClient.Timeout != 0 {
		clone := *dialClient
		clone.Timeout = 0
		dialClient = &clone
	}

	// bodyclose is a false positive here: after a successful WebSocket upgrade,
	// coder/websocket owns the response body and closing it would tear down the
	// connection. The connection (and thus the body) is released via conn.Close.
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{ //nolint:bodyclose
		HTTPClient: dialClient,
	})
	if err != nil {
		return fmt.Errorf("dialing incus events stream: %w", err)
	}
	// Generous read limit: Incus lifecycle payloads are small, but operation
	// events can be larger. 1 MiB is ample and bounds memory.
	conn.SetReadLimit(1 << 20)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("reading incus event: %w", err)
		}

		var ev Event
		if err := json.Unmarshal(data, &ev); err != nil {
			c.logger.Debug("skipping undecodable incus event", "error", err)
			continue
		}
		handler(ev)
	}
}
