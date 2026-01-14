package dnsweaver

import (
	"log/slog"
	"os"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestParser_SimpleHostname(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.hostname": "app.example.com",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 1 {
		t.Fatalf("expected 1 extraction, got %d", len(extractions))
	}

	e := extractions[0]
	if e.Hostname != "app.example.com" {
		t.Errorf("hostname = %q, want %q", e.Hostname, "app.example.com")
	}
	if e.RecordName != "" {
		t.Errorf("recordName = %q, want empty", e.RecordName)
	}
	if e.HasHints() {
		t.Error("expected no hints for simple hostname")
	}
}

func TestParser_SimpleHostname_Trimmed(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.hostname": "  app.example.com  ",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 1 {
		t.Fatalf("expected 1 extraction, got %d", len(extractions))
	}

	if extractions[0].Hostname != "app.example.com" {
		t.Errorf("hostname not trimmed: %q", extractions[0].Hostname)
	}
}

func TestParser_SimpleHostname_Empty(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.hostname": "",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 0 {
		t.Errorf("expected 0 extractions for empty hostname, got %d", len(extractions))
	}
}

func TestParser_NamedRecord_Basic(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.records.myapp.hostname": "app.example.com",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 1 {
		t.Fatalf("expected 1 extraction, got %d", len(extractions))
	}

	e := extractions[0]
	if e.Hostname != "app.example.com" {
		t.Errorf("hostname = %q, want %q", e.Hostname, "app.example.com")
	}
	if e.RecordName != "myapp" {
		t.Errorf("recordName = %q, want %q", e.RecordName, "myapp")
	}
}

func TestParser_NamedRecord_AllFields(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.records.myapp.hostname": "app.example.com",
		"dnsweaver.records.myapp.type":     "A",
		"dnsweaver.records.myapp.target":   "10.1.20.100",
		"dnsweaver.records.myapp.provider": "internal-dns",
		"dnsweaver.records.myapp.ttl":      "600",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 1 {
		t.Fatalf("expected 1 extraction, got %d", len(extractions))
	}

	e := extractions[0]
	if e.Hostname != "app.example.com" {
		t.Errorf("hostname = %q, want %q", e.Hostname, "app.example.com")
	}
	if e.Type != "A" {
		t.Errorf("type = %q, want %q", e.Type, "A")
	}
	if e.Target != "10.1.20.100" {
		t.Errorf("target = %q, want %q", e.Target, "10.1.20.100")
	}
	if e.Provider != "internal-dns" {
		t.Errorf("provider = %q, want %q", e.Provider, "internal-dns")
	}
	if e.TTL != 600 {
		t.Errorf("ttl = %d, want %d", e.TTL, 600)
	}
	if !e.HasHints() {
		t.Error("expected hints to be set")
	}
}

func TestParser_NamedRecord_TypeCaseInsensitive(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.records.myapp.hostname": "app.example.com",
		"dnsweaver.records.myapp.type":     "aaaa",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 1 {
		t.Fatalf("expected 1 extraction, got %d", len(extractions))
	}

	if extractions[0].Type != "AAAA" {
		t.Errorf("type = %q, want %q (should be uppercased)", extractions[0].Type, "AAAA")
	}
}

func TestParser_NamedRecord_SRV(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.records.mc.hostname": "_minecraft._tcp.mc.example.com",
		"dnsweaver.records.mc.type":     "SRV",
		"dnsweaver.records.mc.target":   "mc-server.example.com",
		"dnsweaver.records.mc.port":     "25565",
		"dnsweaver.records.mc.priority": "0",
		"dnsweaver.records.mc.weight":   "5",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 1 {
		t.Fatalf("expected 1 extraction, got %d", len(extractions))
	}

	e := extractions[0]
	if e.Hostname != "_minecraft._tcp.mc.example.com" {
		t.Errorf("hostname = %q", e.Hostname)
	}
	if e.Type != "SRV" {
		t.Errorf("type = %q, want %q", e.Type, "SRV")
	}
	if e.SRV == nil {
		t.Fatal("SRV data is nil")
	}
	if e.SRV.Port != 25565 {
		t.Errorf("port = %d, want %d", e.SRV.Port, 25565)
	}
	if e.SRV.Priority != 0 {
		t.Errorf("priority = %d, want %d", e.SRV.Priority, 0)
	}
	if e.SRV.Weight != 5 {
		t.Errorf("weight = %d, want %d", e.SRV.Weight, 5)
	}
}

func TestParser_MultipleRecords(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		// Simple hostname
		"dnsweaver.hostname": "simple.example.com",
		// Named record 1 - IPv4
		"dnsweaver.records.v4.hostname": "app.example.com",
		"dnsweaver.records.v4.type":     "A",
		"dnsweaver.records.v4.target":   "10.1.20.100",
		// Named record 2 - IPv6
		"dnsweaver.records.v6.hostname": "app.example.com",
		"dnsweaver.records.v6.type":     "AAAA",
		"dnsweaver.records.v6.target":   "fd00::1",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 3 {
		t.Fatalf("expected 3 extractions, got %d", len(extractions))
	}

	// Verify we have all expected hostnames (order not guaranteed for named records)
	found := make(map[string]bool)
	for _, e := range extractions {
		key := e.Hostname + "/" + e.Type
		found[key] = true
	}

	if !found["simple.example.com/"] {
		t.Error("missing simple.example.com")
	}
	if !found["app.example.com/A"] {
		t.Error("missing app.example.com/A")
	}
	if !found["app.example.com/AAAA"] {
		t.Error("missing app.example.com/AAAA")
	}
}

func TestParser_MissingHostname(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	// Named record with type but no hostname - should be skipped
	labels := map[string]string{
		"dnsweaver.records.myapp.type":   "A",
		"dnsweaver.records.myapp.target": "10.1.20.100",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 0 {
		t.Errorf("expected 0 extractions for missing hostname, got %d", len(extractions))
	}
}

func TestParser_InvalidTTL(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.records.myapp.hostname": "app.example.com",
		"dnsweaver.records.myapp.ttl":      "not-a-number",
	}

	extractions := parser.ExtractHostnames(labels)

	// Should still extract the hostname, just without TTL
	if len(extractions) != 1 {
		t.Fatalf("expected 1 extraction, got %d", len(extractions))
	}

	if extractions[0].TTL != 0 {
		t.Errorf("ttl = %d, want 0 (default for invalid)", extractions[0].TTL)
	}
}

func TestParser_NoLabels(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`app.example.com`)",
		"com.docker.compose.service":      "myapp",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 0 {
		t.Errorf("expected 0 extractions for non-dnsweaver labels, got %d", len(extractions))
	}
}

func TestParser_RecordNameVariations(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.records.simple.hostname":          "a.example.com",
		"dnsweaver.records.with-hyphen.hostname":     "b.example.com",
		"dnsweaver.records.with_underscore.hostname": "c.example.com",
		"dnsweaver.records.MixedCase123.hostname":    "d.example.com",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 4 {
		t.Fatalf("expected 4 extractions, got %d", len(extractions))
	}
}

func TestParser_PTRRecord(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.records.ptr.hostname": "1.20.1.10.in-addr.arpa",
		"dnsweaver.records.ptr.type":     "PTR",
		"dnsweaver.records.ptr.target":   "mail.example.com",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 1 {
		t.Fatalf("expected 1 extraction, got %d", len(extractions))
	}

	e := extractions[0]
	if e.Hostname != "1.20.1.10.in-addr.arpa" {
		t.Errorf("hostname = %q", e.Hostname)
	}
	if e.Type != "PTR" {
		t.Errorf("type = %q, want PTR", e.Type)
	}
	if e.Target != "mail.example.com" {
		t.Errorf("target = %q", e.Target)
	}
}

func TestParser_ProviderTargeting(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.records.internal.hostname": "app.local.example.com",
		"dnsweaver.records.internal.provider": "internal-dns",
		"dnsweaver.records.public.hostname":   "app.example.com",
		"dnsweaver.records.public.provider":   "cloudflare",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 2 {
		t.Fatalf("expected 2 extractions, got %d", len(extractions))
	}

	providers := make(map[string]string)
	for _, e := range extractions {
		providers[e.Hostname] = e.Provider
	}

	if providers["app.local.example.com"] != "internal-dns" {
		t.Errorf("internal provider = %q, want internal-dns", providers["app.local.example.com"])
	}
	if providers["app.example.com"] != "cloudflare" {
		t.Errorf("public provider = %q, want cloudflare", providers["app.example.com"])
	}
}

func TestParser_EnabledFalse_SkipsWorkload(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.hostname": "app.example.com",
		"dnsweaver.enabled":  "false",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 0 {
		t.Errorf("expected 0 extractions when enabled=false, got %d", len(extractions))
	}
}

func TestParser_EnabledFalse_CaseInsensitive(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	testCases := []string{"false", "FALSE", "False", "  false  "}

	for _, value := range testCases {
		labels := map[string]string{
			"dnsweaver.hostname": "app.example.com",
			"dnsweaver.enabled":  value,
		}

		extractions := parser.ExtractHostnames(labels)

		if len(extractions) != 0 {
			t.Errorf("expected 0 extractions for enabled=%q, got %d", value, len(extractions))
		}
	}
}

func TestParser_EnabledTrue_ProcessesWorkload(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.hostname": "app.example.com",
		"dnsweaver.enabled":  "true",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 1 {
		t.Fatalf("expected 1 extraction when enabled=true, got %d", len(extractions))
	}
}

func TestParser_EnabledNotSet_ProcessesWorkload(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.hostname": "app.example.com",
		// enabled not set - should default to processing
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 1 {
		t.Fatalf("expected 1 extraction when enabled not set, got %d", len(extractions))
	}
}

func TestParser_SimpleHostname_WithTTL(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.hostname": "app.example.com",
		"dnsweaver.ttl":      "60",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 1 {
		t.Fatalf("expected 1 extraction, got %d", len(extractions))
	}

	if extractions[0].TTL != 60 {
		t.Errorf("ttl = %d, want 60", extractions[0].TTL)
	}
}

func TestParser_SimpleHostname_WithTTL_Trimmed(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.hostname": "app.example.com",
		"dnsweaver.ttl":      "  120  ",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 1 {
		t.Fatalf("expected 1 extraction, got %d", len(extractions))
	}

	if extractions[0].TTL != 120 {
		t.Errorf("ttl = %d, want 120", extractions[0].TTL)
	}
}

func TestParser_SimpleHostname_InvalidTTL(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.hostname": "app.example.com",
		"dnsweaver.ttl":      "invalid",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 1 {
		t.Fatalf("expected 1 extraction, got %d", len(extractions))
	}

	// Invalid TTL should result in 0 (use default)
	if extractions[0].TTL != 0 {
		t.Errorf("ttl = %d, want 0 for invalid value", extractions[0].TTL)
	}
}

func TestParser_NamedRecord_EnabledFalse(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.records.myapp.hostname": "app.example.com",
		"dnsweaver.records.myapp.enabled":  "false",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 0 {
		t.Errorf("expected 0 extractions for named record with enabled=false, got %d", len(extractions))
	}
}

func TestParser_NamedRecord_MixedEnabled(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"dnsweaver.records.enabled.hostname":  "enabled.example.com",
		"dnsweaver.records.disabled.hostname": "disabled.example.com",
		"dnsweaver.records.disabled.enabled":  "false",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 1 {
		t.Fatalf("expected 1 extraction, got %d", len(extractions))
	}

	if extractions[0].Hostname != "enabled.example.com" {
		t.Errorf("hostname = %q, expected enabled.example.com", extractions[0].Hostname)
	}
}
