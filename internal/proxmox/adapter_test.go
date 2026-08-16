package proxmox

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
)

// testLogger discards output so tests stay quiet; the resolver logs at debug
// level on the paths these tests exercise.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestToWorkload_VM(t *testing.T) {
	r := ClusterResource{
		VMID:   100,
		Name:   "web-server",
		Node:   "pve-00",
		Type:   "qemu",
		Status: "running",
		Tags:   "dnsweaver;web",
	}

	w := toWorkload(r, ResolvedIPs{V4: "10.1.20.100"}, IPVersionIPv4)

	if w.ID != "pve-00/100" {
		t.Errorf("ID = %q, want %q", w.ID, "pve-00/100")
	}
	if w.Name != "web-server" {
		t.Errorf("Name = %q, want %q", w.Name, "web-server")
	}
	if string(w.Kind) != "vm" {
		t.Errorf("Kind = %q, want %q", w.Kind, "vm")
	}
	if string(w.Platform) != "proxmox" {
		t.Errorf("Platform = %q, want %q", w.Platform, "proxmox")
	}
	if w.Metadata["ip"] != "10.1.20.100" {
		t.Errorf("Metadata[ip] = %q, want %q", w.Metadata["ip"], "10.1.20.100")
	}
	if w.Metadata["node"] != "pve-00" {
		t.Errorf("Metadata[node] = %q, want %q", w.Metadata["node"], "pve-00")
	}
	if w.Labels["proxmox.tag/dnsweaver"] != "true" {
		t.Errorf("expected proxmox.tag/dnsweaver label, got %v", w.Labels)
	}
}

func TestToWorkload_LXC(t *testing.T) {
	r := ClusterResource{
		VMID:   200,
		Name:   "db-lxc",
		Node:   "pve-01",
		Type:   "lxc",
		Status: "running",
		Tags:   "",
	}

	w := toWorkload(r, ResolvedIPs{V4: "10.1.20.200"}, IPVersionIPv4)

	if string(w.Kind) != "lxc" {
		t.Errorf("Kind = %q, want %q", w.Kind, "lxc")
	}
}

func TestToWorkload_NoIP(t *testing.T) {
	r := ClusterResource{VMID: 100, Name: "vm", Node: "pve-00", Type: "qemu", Status: "running"}
	w := toWorkload(r, ResolvedIPs{}, IPVersionIPv4)

	if _, ok := w.Metadata["ip"]; ok {
		t.Error("expected no 'ip' key in Metadata when IP is empty")
	}
}

func TestHasTagWithPrefix(t *testing.T) {
	tests := []struct {
		tags   string
		prefix string
		want   bool
	}{
		{"dnsweaver;web", "dnsweaver", true},
		{"dns;web", "dnsweaver", false},
		{"", "dnsweaver", false},
		{"web;dnsweaver-host=foo.example.com", "dnsweaver", true},
		{"other", "dnsweaver", false},
	}

	for _, tt := range tests {
		got := hasTagWithPrefix(tt.tags, tt.prefix)
		if got != tt.want {
			t.Errorf("hasTagWithPrefix(%q, %q) = %v, want %v", tt.tags, tt.prefix, got, tt.want)
		}
	}
}

func TestParseTags(t *testing.T) {
	labels := parseTags("dns;web;production")

	if labels["proxmox.tag/dns"] != "true" {
		t.Errorf("expected proxmox.tag/dns=true, got %v", labels)
	}
	if labels["proxmox.tag/web"] != "true" {
		t.Errorf("expected proxmox.tag/web=true, got %v", labels)
	}
	if labels["proxmox.tag/production"] != "true" {
		t.Errorf("expected proxmox.tag/production=true, got %v", labels)
	}

	empty := parseTags("")
	if len(empty) != 0 {
		t.Errorf("expected empty labels for empty tags, got %v", empty)
	}
}

func TestMatchesFilters(t *testing.T) {
	adapter := &WorkloadListerAdapter{
		cfg: AdapterConfig{
			StateFilter: "running",
			NodeFilter:  "pve-00",
			TagFilter:   "dnsweaver",
		},
	}

	tests := []struct {
		name string
		r    ClusterResource
		want bool
	}{
		{
			name: "all filters match",
			r:    ClusterResource{Status: "running", Node: "pve-00", Tags: "dnsweaver;web"},
			want: true,
		},
		{
			name: "wrong state",
			r:    ClusterResource{Status: "stopped", Node: "pve-00", Tags: "dnsweaver"},
			want: false,
		},
		{
			name: "wrong node",
			r:    ClusterResource{Status: "running", Node: "pve-01", Tags: "dnsweaver"},
			want: false,
		},
		{
			name: "missing tag",
			r:    ClusterResource{Status: "running", Node: "pve-00", Tags: "web"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adapter.matchesFilters(tt.r)
			if got != tt.want {
				t.Errorf("matchesFilters = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestResolveLXCIPs_DHCPFallsBackToInterfaces covers the core gap: a container
// configured with ip=dhcp has no address in its config, and was previously
// dropped entirely. It must now be resolved from the live interfaces endpoint.
func TestResolveLXCIPs_DHCPFallsBackToInterfaces(t *testing.T) {
	var configCalls, interfaceCalls int

	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api2/json/nodes/pve-00/lxc/200/config":
			configCalls++
			_, _ = w.Write([]byte(`{"data":{"net0":"name=eth0,bridge=vmbr0,ip=dhcp,ip6=auto"}}`))
		case "/api2/json/nodes/pve-00/lxc/200/interfaces":
			interfaceCalls++
			_, _ = w.Write([]byte(`{"data":[
				{"name":"lo","ip-addresses":[{"ip-address-type":"inet","ip-address":"127.0.0.1"}]},
				{"name":"eth0","ip-addresses":[
					{"ip-address-type":"inet","ip-address":"10.30.0.42"},
					{"ip-address-type":"inet6","ip-address":"fd00::42"}
				]}
			]}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	})

	r := ClusterResource{VMID: 200, Name: "db-lxc", Node: "pve-00", Type: "lxc", Status: "running"}

	got, err := ResolveIPs(context.Background(), client, r, testLogger(), ResolveOptions{WantV6: true})
	if err != nil {
		t.Fatalf("ResolveIPs: %v", err)
	}
	if got.V4 != "10.30.0.42" {
		t.Errorf("V4 = %q, want %q", got.V4, "10.30.0.42")
	}
	if got.V6 != "fd00::42" {
		t.Errorf("V6 = %q, want %q", got.V6, "fd00::42")
	}
	if configCalls != 1 || interfaceCalls != 1 {
		t.Errorf("calls: config=%d interfaces=%d, want 1 and 1", configCalls, interfaceCalls)
	}
}

// A statically configured container must not incur the extra interfaces call.
func TestResolveLXCIPs_StaticConfigSkipsInterfacesCall(t *testing.T) {
	var interfaceCalls int

	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api2/json/nodes/pve-00/lxc/200/config":
			_, _ = w.Write([]byte(`{"data":{"net0":"name=eth0,bridge=vmbr0,ip=10.20.0.7/24"}}`))
		case "/api2/json/nodes/pve-00/lxc/200/interfaces":
			interfaceCalls++
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	})

	r := ClusterResource{VMID: 200, Name: "db-lxc", Node: "pve-00", Type: "lxc", Status: "running"}

	got, err := ResolveIPs(context.Background(), client, r, testLogger(), ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveIPs: %v", err)
	}
	if got.V4 != "10.20.0.7" {
		t.Errorf("V4 = %q, want %q", got.V4, "10.20.0.7")
	}
	if interfaceCalls != 0 {
		t.Errorf("interfaces endpoint called %d times, want 0", interfaceCalls)
	}
}

// Older PVE releases lack the interfaces endpoint. A 501 there must degrade to
// "no addresses", not fail the resource.
func TestResolveLXCIPs_MissingInterfacesEndpointIsNotFatal(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/nodes/pve-00/lxc/200/config":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"net0":"name=eth0,ip=dhcp"}}`))
		default:
			w.WriteHeader(http.StatusNotImplemented)
		}
	})

	r := ClusterResource{VMID: 200, Name: "db-lxc", Node: "pve-00", Type: "lxc", Status: "running"}

	got, err := ResolveIPs(context.Background(), client, r, testLogger(), ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveIPs returned error on legacy PVE: %v", err)
	}
	if !got.IsEmpty() {
		t.Errorf("got %+v, want empty", got)
	}
}

func TestToWorkload_Pool(t *testing.T) {
	r := ClusterResource{
		VMID: 100, Name: "web", Node: "pve-00", Type: "qemu", Status: "running",
		Pool: "tenant-alice",
	}

	w := toWorkload(r, ResolvedIPs{V4: "10.1.20.100"}, IPVersionIPv4)

	if w.Metadata["pool"] != "tenant-alice" {
		t.Errorf("Metadata[pool] = %q, want %q", w.Metadata["pool"], "tenant-alice")
	}
	if w.Labels["proxmox.pool/tenant-alice"] != "true" {
		t.Errorf("expected proxmox.pool/tenant-alice label, got %v", w.Labels)
	}

	// A resource with no pool must not gain an empty label or metadata key.
	unpooled := toWorkload(ClusterResource{VMID: 101, Node: "pve-00", Type: "qemu"}, ResolvedIPs{}, IPVersionIPv4)
	if _, ok := unpooled.Metadata["pool"]; ok {
		t.Error("expected no pool metadata key when the resource has no pool")
	}
	if _, ok := unpooled.Labels["proxmox.pool/"]; ok {
		t.Error("expected no empty pool label")
	}
}

func TestToWorkload_IPVersionGatesMetadata(t *testing.T) {
	r := ClusterResource{VMID: 100, Name: "web", Node: "pve-00", Type: "qemu", Status: "running"}
	ips := ResolvedIPs{V4: "10.1.20.100", V6: "fd00::100"}

	v4 := toWorkload(r, ips, IPVersionIPv4)
	if v4.Metadata["ip"] != "10.1.20.100" {
		t.Errorf("ipv4 mode: Metadata[ip] = %q", v4.Metadata["ip"])
	}
	if _, ok := v4.Metadata["ip6"]; ok {
		t.Error("ipv4 mode must not set ip6")
	}

	v6 := toWorkload(r, ips, IPVersionIPv6)
	if _, ok := v6.Metadata["ip"]; ok {
		t.Error("ipv6 mode must not set ip")
	}
	if v6.Metadata["ip6"] != "fd00::100" {
		t.Errorf("ipv6 mode: Metadata[ip6] = %q", v6.Metadata["ip6"])
	}

	dual := toWorkload(r, ips, IPVersionDual)
	if dual.Metadata["ip"] != "10.1.20.100" || dual.Metadata["ip6"] != "fd00::100" {
		t.Errorf("dual mode: got ip=%q ip6=%q", dual.Metadata["ip"], dual.Metadata["ip6"])
	}
}

func TestParseIPVersion(t *testing.T) {
	tests := []struct {
		in      string
		want    IPVersion
		wantErr bool
	}{
		{"", IPVersionIPv4, false},
		{"ipv4", IPVersionIPv4, false},
		{"4", IPVersionIPv4, false},
		{"IPv6", IPVersionIPv6, false},
		{"v6", IPVersionIPv6, false},
		{"dual", IPVersionDual, false},
		{"both", IPVersionDual, false},
		{" Dual-Stack ", IPVersionDual, false},
		{"nonsense", "", true},
	}

	for _, tt := range tests {
		got, err := ParseIPVersion(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseIPVersion(%q) expected error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseIPVersion(%q): %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseIPVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
