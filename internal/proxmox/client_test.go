package proxmox

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := NewClient(ClientConfig{
		BaseURL:     srv.URL,
		TokenID:     "root@pam!test",
		TokenSecret: "test-secret",
		VerifyTLS:   false,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	return srv, client
}

func TestListClusterResources(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/cluster/resources" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("type") != "vm" {
			t.Errorf("expected type=vm query param, got %s", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "PVEAPIToken=root@pam!test=test-secret" {
			t.Errorf("unexpected Authorization header: %s", r.Header.Get("Authorization"))
		}

		resp := map[string]any{
			"data": []map[string]any{
				{"vmid": 100, "name": "web-server", "node": "pve-00", "type": "qemu", "status": "running", "tags": "dns;web"},
				{"vmid": 200, "name": "db-lxc", "node": "pve-01", "type": "lxc", "status": "running", "tags": ""},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	resources, err := client.ListClusterResources(context.Background())
	if err != nil {
		t.Fatalf("ListClusterResources: %v", err)
	}

	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}

	vm := resources[0]
	if vm.VMID != 100 {
		t.Errorf("VMID = %d, want 100", vm.VMID)
	}
	if vm.Name != "web-server" {
		t.Errorf("Name = %q, want %q", vm.Name, "web-server")
	}
	if vm.Type != "qemu" {
		t.Errorf("Type = %q, want %q", vm.Type, "qemu")
	}
	if vm.Tags != "dns;web" {
		t.Errorf("Tags = %q, want %q", vm.Tags, "dns;web")
	}
}

func TestGetLXCConfig(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/nodes/pve-00/lxc/200/config" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := map[string]any{
			"data": map[string]any{
				"net0": "name=eth0,bridge=vmbr0,hwaddr=AA:BB:CC:DD:EE:FF,ip=192.0.2.50/24,ip6=auto",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	cfg, err := client.GetLXCConfig(context.Background(), "pve-00", 200)
	if err != nil {
		t.Fatalf("GetLXCConfig: %v", err)
	}

	expected := "name=eth0,bridge=vmbr0,hwaddr=AA:BB:CC:DD:EE:FF,ip=192.0.2.50/24,ip6=auto"
	if cfg.Nets["net0"] != expected {
		t.Errorf("Nets[net0] = %q, want %q", cfg.Nets["net0"], expected)
	}
}

func TestGetVMAgentNetworks(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"result": []map[string]any{
					{
						"name": "lo",
						"ip-addresses": []map[string]any{
							{"ip-address-type": "ipv4", "ip-address": "127.0.0.1"},
						},
					},
					{
						"name": "eth0",
						"ip-addresses": []map[string]any{
							{"ip-address-type": "ipv4", "ip-address": "10.1.20.100"},
							{"ip-address-type": "ipv6", "ip-address": "fe80::1"},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	ifaces, err := client.GetVMAgentNetworks(context.Background(), "pve-00", 100)
	if err != nil {
		t.Fatalf("GetVMAgentNetworks: %v", err)
	}

	if len(ifaces) != 2 {
		t.Fatalf("expected 2 interfaces, got %d", len(ifaces))
	}

	eth0 := ifaces[1]
	if eth0.Name != "eth0" {
		t.Errorf("Name = %q, want %q", eth0.Name, "eth0")
	}
	if len(eth0.IPAddresses) != 2 {
		t.Errorf("IPAddresses count = %d, want 2", len(eth0.IPAddresses))
	}
}

func TestGetVMAgentNetworks_AgentNotRunning(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		resp := map[string]any{
			"errors": map[string]string{
				"command": "QEMU guest agent is not running",
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	_, err := client.GetVMAgentNetworks(context.Background(), "pve-00", 100)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var agentErr *ErrAgentNotRunning
	if !errors.As(err, &agentErr) {
		t.Errorf("expected *ErrAgentNotRunning, got %T: %v", err, err)
	}
}

func TestNewClient_MissingFields(t *testing.T) {
	tests := []struct {
		name string
		cfg  ClientConfig
	}{
		{"no BaseURL", ClientConfig{TokenID: "x", TokenSecret: "y"}},
		{"no TokenID", ClientConfig{BaseURL: "http://x", TokenSecret: "y"}},
		{"no TokenSecret", ClientConfig{BaseURL: "http://x", TokenID: "x"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClient(tt.cfg)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestAPIError(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"permission check failed"}`))
	})

	_, err := client.ListClusterResources(context.Background())
	if err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}

func TestGetLXCConfig_MultipleNICs(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"net0":     "name=eth0,bridge=vmbr0,ip=dhcp",
				"net1":     "name=eth1,bridge=vmbr1,ip=10.20.0.7/24",
				"net10":    "name=eth10,bridge=vmbr2,ip=10.30.0.7/24",
				"hostname": "db-lxc",
				"cores":    2,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	cfg, err := client.GetLXCConfig(context.Background(), "pve-00", 200)
	if err != nil {
		t.Fatalf("GetLXCConfig: %v", err)
	}

	if len(cfg.Nets) != 3 {
		t.Fatalf("Nets has %d entries, want 3: %v", len(cfg.Nets), cfg.Nets)
	}
	if cfg.Nets["net1"] != "name=eth1,bridge=vmbr1,ip=10.20.0.7/24" {
		t.Errorf("Nets[net1] = %q", cfg.Nets["net1"])
	}
	// Non-net keys must not leak in.
	if _, ok := cfg.Nets["hostname"]; ok {
		t.Error("hostname must not be collected as a network interface")
	}

	// net10 must sort after net1, not between net0 and net1.
	want := []string{"net0", "net1", "net10"}
	got := cfg.SortedNetKeys()
	if len(got) != len(want) {
		t.Fatalf("SortedNetKeys() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SortedNetKeys() = %v, want %v", got, want)
		}
	}
}

func TestGetLXCInterfaces(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/nodes/pve-00/lxc/200/interfaces" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := map[string]any{
			"data": []map[string]any{
				{
					"name":             "eth0",
					"hardware-address": "AA:BB:CC:DD:EE:FF",
					"inet":             "10.30.0.42/24",
					"ip-addresses": []map[string]any{
						{"ip-address-type": "inet", "ip-address": "10.30.0.42", "prefix": 24},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	ifaces, err := client.GetLXCInterfaces(context.Background(), "pve-00", 200)
	if err != nil {
		t.Fatalf("GetLXCInterfaces: %v", err)
	}
	if len(ifaces) != 1 {
		t.Fatalf("got %d interfaces, want 1", len(ifaces))
	}
	if ifaces[0].Name != "eth0" || ifaces[0].HardwareAddress != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("unexpected interface: %+v", ifaces[0])
	}
	if len(ifaces[0].IPAddresses) != 1 || ifaces[0].IPAddresses[0].IPAddress != "10.30.0.42" {
		t.Errorf("unexpected addresses: %+v", ifaces[0].IPAddresses)
	}
}

func TestGetLXCInterfaces_StoppedContainerReturnsNull(t *testing.T) {
	// PVE resolves the container PID first and bails out with a null payload
	// (HTTP 200) when the container is not running.
	_, client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":null}`))
	})

	ifaces, err := client.GetLXCInterfaces(context.Background(), "pve-00", 200)
	if err != nil {
		t.Fatalf("GetLXCInterfaces returned error for stopped container: %v", err)
	}
	if len(ifaces) != 0 {
		t.Errorf("got %d interfaces, want 0", len(ifaces))
	}
}
