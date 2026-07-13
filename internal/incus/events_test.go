package incus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// eventsTestServer starts an httptest server that upgrades /1.0/events to a
// WebSocket and writes each provided message (raw JSON) to the client, then
// blocks until the client disconnects. It returns an incus Client pointed at it.
func eventsTestServer(t *testing.T, messages []string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/1.0/events") {
			http.NotFound(w, r)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.Close(websocket.StatusNormalClosure, "") }()
		ctx := r.Context()
		for _, m := range messages {
			if err := c.Write(ctx, websocket.MessageText, []byte(m)); err != nil {
				return
			}
		}
		<-ctx.Done()
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(ClientConfig{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestEventAction(t *testing.T) {
	ev := Event{
		Type:     EventTypeLifecycle,
		Metadata: []byte(`{"action":"instance-started","source":"/1.0/instances/web"}`),
	}
	if got := ev.Action(); got != "instance-started" {
		t.Errorf("Action() = %q, want instance-started", got)
	}

	// Non-lifecycle event returns empty action.
	other := Event{Type: "logging", Metadata: []byte(`{"message":"hi"}`)}
	if got := other.Action(); got != "" {
		t.Errorf("Action() = %q, want empty for non-lifecycle", got)
	}
}

func TestEventsURL(t *testing.T) {
	tests := []struct {
		name        string
		base        string
		project     string
		allProjects bool
		want        string
	}{
		{"socket", "http://incus", "", false, "ws://incus/1.0/events?type=lifecycle"},
		{"https", "https://incus:8443", "", false, "wss://incus:8443/1.0/events?type=lifecycle"},
		{"with project", "https://incus:8443", "prod", false, "wss://incus:8443/1.0/events?type=lifecycle&project=prod"},
		{"all projects", "https://incus:8443", "", true, "wss://incus:8443/1.0/events?type=lifecycle&all-projects=true"},
		{"all projects overrides project", "https://incus:8443", "prod", true, "wss://incus:8443/1.0/events?type=lifecycle&all-projects=true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{baseURL: tt.base, project: tt.project, allProjects: tt.allProjects}
			got := c.eventsURL([]string{EventTypeLifecycle})
			if got != tt.want {
				t.Errorf("eventsURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStreamEvents(t *testing.T) {
	client := eventsTestServer(t, []string{
		`{"type":"lifecycle","project":"default","metadata":{"action":"instance-started","source":"/1.0/instances/web"}}`,
		`{"type":"lifecycle","project":"default","metadata":{"action":"instance-stopped","source":"/1.0/instances/web"}}`,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var (
		mu      sync.Mutex
		actions []string
	)
	got2 := make(chan struct{})
	go func() {
		_ = client.StreamEvents(ctx, nil, func(ev Event) {
			mu.Lock()
			actions = append(actions, ev.Action())
			n := len(actions)
			mu.Unlock()
			if n == 2 {
				close(got2)
			}
		})
	}()

	select {
	case <-got2:
		// Received both events; cancel to unblock the stream.
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("StreamEvents did not deliver both events in time")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(actions) < 2 {
		t.Fatalf("expected 2 actions, got %d: %v", len(actions), actions)
	}
	if actions[0] != "instance-started" || actions[1] != "instance-stopped" {
		t.Errorf("unexpected actions: %v", actions)
	}
}
