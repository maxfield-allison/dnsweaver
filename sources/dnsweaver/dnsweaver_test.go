package dnsweaver

import (
	"context"
	"testing"
)

func TestDNSWeaver_Name(t *testing.T) {
	d := New(WithLogger(testLogger()))

	if d.Name() != "dnsweaver" {
		t.Errorf("Name() = %q, want %q", d.Name(), "dnsweaver")
	}
}

func TestDNSWeaver_SupportsDiscovery(t *testing.T) {
	d := New(WithLogger(testLogger()))

	if d.SupportsDiscovery() {
		t.Error("SupportsDiscovery() = true, want false (native labels don't support file discovery)")
	}
}

func TestDNSWeaver_Discover(t *testing.T) {
	d := New(WithLogger(testLogger()))

	hostnames, err := d.Discover(context.Background())

	if err != nil {
		t.Errorf("Discover() error = %v, want nil", err)
	}
	if hostnames != nil {
		t.Errorf("Discover() = %v, want nil", hostnames)
	}
}

func TestDNSWeaver_Extract_Empty(t *testing.T) {
	d := New(WithLogger(testLogger()))

	hostnames, err := d.Extract(context.Background(), nil)

	if err != nil {
		t.Errorf("Extract(nil) error = %v", err)
	}
	if hostnames != nil {
		t.Errorf("Extract(nil) = %v, want nil", hostnames)
	}
}

func TestDNSWeaver_Extract_SimpleHostname(t *testing.T) {
	d := New(WithLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.hostname": "app.example.com",
	}

	hostnames, err := d.Extract(context.Background(), labels)

	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(hostnames) != 1 {
		t.Fatalf("Extract() returned %d hostnames, want 1", len(hostnames))
	}

	h := hostnames[0]
	if h.Name != "app.example.com" {
		t.Errorf("Name = %q, want %q", h.Name, "app.example.com")
	}
	if h.Source != "dnsweaver" {
		t.Errorf("Source = %q, want %q", h.Source, "dnsweaver")
	}
	if h.Router != "" {
		t.Errorf("Router = %q, want empty (simple hostname)", h.Router)
	}
	if h.RecordHints != nil {
		t.Error("RecordHints should be nil for simple hostname")
	}
}

func TestDNSWeaver_Extract_NamedRecordWithHints(t *testing.T) {
	d := New(WithLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.records.myapp.hostname": "app.example.com",
		"dnsweaver.records.myapp.type":     "A",
		"dnsweaver.records.myapp.target":   "10.1.20.100",
		"dnsweaver.records.myapp.provider": "internal-dns",
		"dnsweaver.records.myapp.ttl":      "600",
	}

	hostnames, err := d.Extract(context.Background(), labels)

	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(hostnames) != 1 {
		t.Fatalf("Extract() returned %d hostnames, want 1", len(hostnames))
	}

	h := hostnames[0]
	if h.Name != "app.example.com" {
		t.Errorf("Name = %q, want %q", h.Name, "app.example.com")
	}
	if h.Source != "dnsweaver" {
		t.Errorf("Source = %q, want %q", h.Source, "dnsweaver")
	}
	if h.Router != "myapp" {
		t.Errorf("Router = %q, want %q (record name)", h.Router, "myapp")
	}

	if h.RecordHints == nil {
		t.Fatal("RecordHints is nil, want non-nil")
	}
	if h.RecordHints.Type != "A" {
		t.Errorf("RecordHints.Type = %q, want %q", h.RecordHints.Type, "A")
	}
	if h.RecordHints.Target != "10.1.20.100" {
		t.Errorf("RecordHints.Target = %q, want %q", h.RecordHints.Target, "10.1.20.100")
	}
	if h.RecordHints.Provider != "internal-dns" {
		t.Errorf("RecordHints.Provider = %q, want %q", h.RecordHints.Provider, "internal-dns")
	}
	if h.RecordHints.TTL != 600 {
		t.Errorf("RecordHints.TTL = %d, want %d", h.RecordHints.TTL, 600)
	}
}

func TestDNSWeaver_Extract_SRVRecord(t *testing.T) {
	d := New(WithLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.records.mc.hostname": "_minecraft._tcp.mc.example.com",
		"dnsweaver.records.mc.type":     "SRV",
		"dnsweaver.records.mc.target":   "mc-server.example.com",
		"dnsweaver.records.mc.port":     "25565",
		"dnsweaver.records.mc.priority": "10",
		"dnsweaver.records.mc.weight":   "5",
	}

	hostnames, err := d.Extract(context.Background(), labels)

	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(hostnames) != 1 {
		t.Fatalf("Extract() returned %d hostnames, want 1", len(hostnames))
	}

	h := hostnames[0]
	if h.RecordHints == nil {
		t.Fatal("RecordHints is nil")
	}
	if h.RecordHints.SRV == nil {
		t.Fatal("RecordHints.SRV is nil")
	}

	srv := h.RecordHints.SRV
	if srv.Port != 25565 {
		t.Errorf("SRV.Port = %d, want %d", srv.Port, 25565)
	}
	if srv.Priority != 10 {
		t.Errorf("SRV.Priority = %d, want %d", srv.Priority, 10)
	}
	if srv.Weight != 5 {
		t.Errorf("SRV.Weight = %d, want %d", srv.Weight, 5)
	}
}

func TestDNSWeaver_Extract_MixedWithNonDnsweaverLabels(t *testing.T) {
	d := New(WithLogger(testLogger()))

	labels := map[string]string{
		// Non-dnsweaver labels
		"traefik.http.routers.myapp.rule": "Host(`app.example.com`)",
		"com.docker.compose.service":      "myapp",
		// dnsweaver label
		"dnsweaver.hostname": "dns.example.com",
	}

	hostnames, err := d.Extract(context.Background(), labels)

	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(hostnames) != 1 {
		t.Fatalf("Extract() returned %d hostnames, want 1", len(hostnames))
	}
	if hostnames[0].Name != "dns.example.com" {
		t.Errorf("Name = %q, want %q", hostnames[0].Name, "dns.example.com")
	}
}

func TestDNSWeaver_Extract_MultipleRecords(t *testing.T) {
	d := New(WithLogger(testLogger()))

	labels := map[string]string{
		// Simple
		"dnsweaver.hostname": "simple.example.com",
		// Named internal
		"dnsweaver.records.internal.hostname": "app.local.example.com",
		"dnsweaver.records.internal.provider": "internal-dns",
		// Named public
		"dnsweaver.records.public.hostname": "app.example.com",
		"dnsweaver.records.public.provider": "cloudflare",
	}

	hostnames, err := d.Extract(context.Background(), labels)

	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(hostnames) != 3 {
		t.Fatalf("Extract() returned %d hostnames, want 3", len(hostnames))
	}

	// Check all sources are "dnsweaver"
	for _, h := range hostnames {
		if h.Source != "dnsweaver" {
			t.Errorf("Source = %q, want dnsweaver", h.Source)
		}
	}
}
