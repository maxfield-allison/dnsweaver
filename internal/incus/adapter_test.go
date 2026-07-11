package incus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

func newAdapterTestServer(t *testing.T, instances []map[string]any) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(instancesEnvelope(t, instances))
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(ClientConfig{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestAdapterListWorkloads(t *testing.T) {
	client := newAdapterTestServer(t, []map[string]any{
		{
			"name":    "web",
			"type":    "container",
			"status":  "Running",
			"project": "default",
			"config":  map[string]string{"user.dnsweaver.hostname": "web.lan"},
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
		{
			"name":    "vmguest",
			"type":    "virtual-machine",
			"status":  "Running",
			"project": "default",
			"state": map[string]any{
				"network": map[string]any{
					"enp5s0": map[string]any{
						"addresses": []map[string]any{
							{"family": "inet", "address": "10.0.0.6", "scope": "global"},
						},
					},
				},
			},
		},
		{
			"name":    "stopped-box",
			"type":    "container",
			"status":  "Stopped",
			"project": "default",
		},
	})

	adapter := NewWorkloadListerAdapter(client, AdapterConfig{}, nil)
	if adapter.Platform() != workload.PlatformIncus {
		t.Errorf("Platform = %q, want incus", adapter.Platform())
	}

	workloads, err := adapter.ListWorkloads(context.Background())
	if err != nil {
		t.Fatalf("ListWorkloads: %v", err)
	}

	// Stopped instance filtered out by default "running" state filter.
	if len(workloads) != 2 {
		t.Fatalf("expected 2 workloads, got %d: %+v", len(workloads), workloads)
	}

	byName := make(map[string]workload.Workload, len(workloads))
	for _, w := range workloads {
		byName[w.Name] = w
	}

	web, ok := byName["web"]
	if !ok {
		t.Fatal("web workload missing")
	}
	if web.Kind != workload.KindIncusContainer {
		t.Errorf("web Kind = %q, want incus-container", web.Kind)
	}
	if web.Platform != workload.PlatformIncus {
		t.Errorf("web Platform = %q, want incus", web.Platform)
	}
	if web.ID != "default/web" {
		t.Errorf("web ID = %q, want default/web", web.ID)
	}
	if web.Metadata["ip"] != "10.0.0.5" {
		t.Errorf("web ip = %q, want 10.0.0.5", web.Metadata["ip"])
	}
	if web.Metadata["type"] != "container" {
		t.Errorf("web type = %q, want container", web.Metadata["type"])
	}
	if web.Labels["user.dnsweaver.hostname"] != "web.lan" {
		t.Errorf("web label missing: %v", web.Labels)
	}

	vm, ok := byName["vmguest"]
	if !ok {
		t.Fatal("vmguest workload missing")
	}
	if vm.Kind != workload.KindIncusVM {
		t.Errorf("vmguest Kind = %q, want incus-vm", vm.Kind)
	}
}

func TestAdapterStateFilter(t *testing.T) {
	client := newAdapterTestServer(t, []map[string]any{
		{"name": "a", "type": "container", "status": "Running", "project": "default"},
		{"name": "b", "type": "container", "status": "Stopped", "project": "default"},
	})

	adapter := NewWorkloadListerAdapter(client, AdapterConfig{StateFilter: "stopped"}, nil)
	workloads, err := adapter.ListWorkloads(context.Background())
	if err != nil {
		t.Fatalf("ListWorkloads: %v", err)
	}
	if len(workloads) != 1 || workloads[0].Name != "b" {
		t.Fatalf("expected only stopped instance b, got %+v", workloads)
	}
}

// TestAdapterComposeLabels verifies that incus-compose "user.label.*" config
// keys are surfaced both verbatim and under their stripped form, and that the
// stripped alias never overwrites an existing label.
func TestAdapterComposeLabels(t *testing.T) {
	client := newAdapterTestServer(t, []map[string]any{
		{
			"name":    "app",
			"type":    "container",
			"status":  "Running",
			"project": "default",
			"config": map[string]string{
				"user.label.traefik.http.routers.app.rule": "Host(`app.example.com`)",
				"user.label.dnsweaver.hostname":            "app.example.net",
				"user.label.incus-compose.service":         "app",
				// Pre-existing stripped key must win over the compose alias.
				"dnsweaver.enabled":            "true",
				"user.label.dnsweaver.enabled": "false",
			},
			"state": map[string]any{
				"network": map[string]any{
					"eth0": map[string]any{
						"addresses": []map[string]any{
							{"family": "inet", "address": "10.0.0.9", "scope": "global"},
						},
					},
				},
			},
		},
	})

	workloads, err := NewWorkloadListerAdapter(client, AdapterConfig{}, nil).
		ListWorkloads(context.Background())
	if err != nil {
		t.Fatalf("ListWorkloads: %v", err)
	}
	if len(workloads) != 1 {
		t.Fatalf("expected 1 workload, got %d", len(workloads))
	}
	labels := workloads[0].Labels

	// Raw keys retained.
	if labels["user.label.traefik.http.routers.app.rule"] != "Host(`app.example.com`)" {
		t.Errorf("raw traefik label missing: %v", labels)
	}
	// Stripped aliases added.
	if labels["traefik.http.routers.app.rule"] != "Host(`app.example.com`)" {
		t.Errorf("stripped traefik label missing: %v", labels)
	}
	if labels["dnsweaver.hostname"] != "app.example.net" {
		t.Errorf("stripped dnsweaver.hostname missing: %v", labels)
	}
	if labels["incus-compose.service"] != "app" {
		t.Errorf("stripped incus-compose.service missing: %v", labels)
	}
	// Existing stripped key not overwritten by the compose alias.
	if labels["dnsweaver.enabled"] != "true" {
		t.Errorf("dnsweaver.enabled = %q, want true (must not be overwritten)", labels["dnsweaver.enabled"])
	}
}
