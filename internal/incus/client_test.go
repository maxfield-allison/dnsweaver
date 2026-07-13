package incus

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// instancesEnvelope wraps the given metadata in the standard Incus sync response.
func instancesEnvelope(t *testing.T, instances []map[string]any) []byte {
	t.Helper()
	meta, err := json.Marshal(instances)
	if err != nil {
		t.Fatalf("marshaling metadata: %v", err)
	}
	env := map[string]any{
		"type":        "sync",
		"status":      "Success",
		"status_code": 200,
		"metadata":    json.RawMessage(meta),
	}
	body, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshaling envelope: %v", err)
	}
	return body
}

func TestNewClientValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ClientConfig
		wantErr bool
	}{
		{name: "neither set", cfg: ClientConfig{}, wantErr: true},
		{name: "both set", cfg: ClientConfig{BaseURL: "https://x:8443", SocketPath: "/run/incus.sock"}, wantErr: true},
		{name: "url only", cfg: ClientConfig{BaseURL: "https://x:8443"}, wantErr: false},
		{name: "socket only", cfg: ClientConfig{SocketPath: "/run/incus.sock"}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClient(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewClient err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestListInstancesHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1.0/instances" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("recursion") != "2" {
			t.Errorf("expected recursion=2, got %q", r.URL.RawQuery)
		}
		if r.URL.Query().Get("project") != "prod" {
			t.Errorf("expected project=prod, got %q", r.URL.Query().Get("project"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(instancesEnvelope(t, []map[string]any{
			{
				"name":    "web",
				"type":    "container",
				"status":  "Running",
				"project": "prod",
				"config":  map[string]string{"user.dnsweaver.hostname": "web.example.com"},
				"state": map[string]any{
					"network": map[string]any{
						"eth0": map[string]any{
							"addresses": []map[string]any{
								{"family": "inet", "address": "10.0.0.5", "scope": "global"},
							},
						},
					},
				},
			},
		}))
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(ClientConfig{BaseURL: srv.URL, Project: "prod"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	instances, err := client.ListInstances(context.Background())
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	inst := instances[0]
	if inst.Name != "web" {
		t.Errorf("Name = %q, want web", inst.Name)
	}
	if inst.Type != "container" {
		t.Errorf("Type = %q, want container", inst.Type)
	}
	if inst.Config["user.dnsweaver.hostname"] != "web.example.com" {
		t.Errorf("config not parsed: %v", inst.Config)
	}
	if got := ResolveIP(inst); got != "10.0.0.5" {
		t.Errorf("ResolveIP = %q, want 10.0.0.5", got)
	}
}

// TestListInstancesAllProjects verifies the client requests all-projects mode
// (and does not send a project filter) when AllProjects is set.
func TestListInstancesAllProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("all-projects") != "true" {
			t.Errorf("expected all-projects=true, got %q", r.URL.RawQuery)
		}
		if r.URL.Query().Get("project") != "" {
			t.Errorf("expected no project filter, got %q", r.URL.Query().Get("project"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"sync","status_code":200,"metadata":[]}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{BaseURL: srv.URL, Project: "ignored", AllProjects: true})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.ListInstances(context.Background()); err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
}

func TestListInstancesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"type":"error","status_code":403,"error":"not authorized"}`))
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(ClientConfig{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if _, err := client.ListInstances(context.Background()); err == nil {
		t.Fatal("expected error for 403 response, got nil")
	}
}

func TestListInstancesSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "incus.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	srv := &http.Server{ //nolint:gosec // test server, no timeouts needed
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/1.0/instances" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(instancesEnvelope(t, []map[string]any{
				{"name": "db", "type": "virtual-machine", "status": "Running"},
			}))
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	client, err := NewClient(ClientConfig{SocketPath: socketPath})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	instances, err := client.ListInstances(context.Background())
	if err != nil {
		t.Fatalf("ListInstances over socket: %v", err)
	}
	if len(instances) != 1 || instances[0].Name != "db" {
		t.Fatalf("unexpected instances: %+v", instances)
	}
	if instances[0].Type != "virtual-machine" {
		t.Errorf("Type = %q, want virtual-machine", instances[0].Type)
	}
}
