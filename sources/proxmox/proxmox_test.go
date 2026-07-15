package proxmox

import (
	"context"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

func TestExtract_SkipsNonProxmox(t *testing.T) {
	src := New()
	w := workload.Workload{
		Platform: workload.PlatformDocker,
		Name:     "some-container",
		Metadata: map[string]string{"ip": "10.1.20.5"},
	}
	hostnames, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hostnames) != 0 {
		t.Errorf("expected no hostnames for non-proxmox workload, got %d", len(hostnames))
	}
}

func TestExtract_SkipsNoIP(t *testing.T) {
	src := New(WithDomain("home.example.com"))
	w := workload.Workload{
		Platform: workload.PlatformProxmox,
		Name:     "webserver",
		Metadata: map[string]string{"node": "pve-00", "vmid": "100"},
	}
	hostnames, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hostnames) != 0 {
		t.Errorf("expected no hostnames when IP is missing, got %d", len(hostnames))
	}
}

func TestExtract_NoDomainPlainName_Skips(t *testing.T) {
	src := New() // no domain configured
	w := workload.Workload{
		Platform: workload.PlatformProxmox,
		Name:     "webserver",
		Metadata: map[string]string{"ip": "10.1.20.5", "node": "pve-00", "vmid": "100"},
	}
	hostnames, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hostnames) != 0 {
		t.Errorf("expected no hostnames when no domain and name is not FQDN, got %d", len(hostnames))
	}
}

func TestExtract_WithDomain(t *testing.T) {
	src := New(WithDomain("home.example.com"))
	w := workload.Workload{
		Platform: workload.PlatformProxmox,
		Name:     "webserver",
		Metadata: map[string]string{"ip": "10.1.20.5", "node": "pve-00", "vmid": "100"},
	}
	hostnames, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hostnames) != 1 {
		t.Fatalf("expected 1 hostname, got %d", len(hostnames))
	}
	h := hostnames[0]
	if h.Name != "webserver.home.example.com" {
		t.Errorf("expected Name=%q, got %q", "webserver.home.example.com", h.Name)
	}
	if h.Source != "proxmox" {
		t.Errorf("expected Source=%q, got %q", "proxmox", h.Source)
	}
	if h.Router != "pve-00/100" {
		t.Errorf("expected Router=%q, got %q", "pve-00/100", h.Router)
	}
	if h.RecordHints == nil {
		t.Fatal("expected RecordHints to be set")
	}
	if h.RecordHints.Type != "A" {
		t.Errorf("expected RecordHints.Type=%q, got %q", "A", h.RecordHints.Type)
	}
	if h.RecordHints.Target != "10.1.20.5" {
		t.Errorf("expected RecordHints.Target=%q, got %q", "10.1.20.5", h.RecordHints.Target)
	}
}

func TestExtract_WithHostnameTagPrefixOverrideFQDN(t *testing.T) {
	src := New(WithDomain("home.example.com"), WithHostnameTagPrefix("dnsweaver"))
	w := workload.Workload{
		Platform: workload.PlatformProxmox,
		Name:     "webserver",
		Metadata: map[string]string{"ip": "10.1.20.5", "node": "pve-00", "vmid": "100", "tags": "dnsweaver+webapp.home.example.com;other:keep"},
	}
	hostnames, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hostnames) != 1 {
		t.Fatalf("expected 1 hostname, got %d", len(hostnames))
	}
	if hostnames[0].Name != "webapp.home.example.com" {
		t.Errorf("expected Name=%q, got %q", "webapp.home.example.com", hostnames[0].Name)
	}
}

func TestExtract_WithHostnameTagPrefixOverridePlainName(t *testing.T) {
	src := New(WithDomain("home.example.com"), WithHostnameTagPrefix("dnsweaver"))
	w := workload.Workload{
		Platform: workload.PlatformProxmox,
		Name:     "webserver",
		Metadata: map[string]string{"ip": "10.1.20.5", "node": "pve-00", "vmid": "100", "tags": "dnsweaver+webapp;other:keep"},
	}
	hostnames, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hostnames) != 1 {
		t.Fatalf("expected 1 hostname, got %d", len(hostnames))
	}
	if hostnames[0].Name != "webapp.home.example.com" {
		t.Errorf("expected Name=%q, got %q", "webapp.home.example.com", hostnames[0].Name)
	}
}

func TestExtract_WithHostnameTagPrefixMissingFallsBack(t *testing.T) {
	src := New(WithDomain("home.example.com"), WithHostnameTagPrefix("dnsweaver"))
	w := workload.Workload{
		Platform: workload.PlatformProxmox,
		Name:     "webserver",
		Metadata: map[string]string{"ip": "10.1.20.5", "node": "pve-00", "vmid": "100", "tags": "other:keep;foo:bar"},
	}
	hostnames, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hostnames) != 1 {
		t.Fatalf("expected 1 hostname, got %d", len(hostnames))
	}
	if hostnames[0].Name != "webserver.home.example.com" {
		t.Errorf("expected Name=%q, got %q", "webserver.home.example.com", hostnames[0].Name)
	}
}

func TestExtract_NameIsFQDN(t *testing.T) {
	src := New() // no domain — VM name is already FQDN
	w := workload.Workload{
		Platform: workload.PlatformProxmox,
		Name:     "webserver.home.example.com",
		Metadata: map[string]string{"ip": "10.1.20.5", "node": "pve-00", "vmid": "100"},
	}
	hostnames, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hostnames) != 1 {
		t.Fatalf("expected 1 hostname, got %d", len(hostnames))
	}
	if hostnames[0].Name != "webserver.home.example.com" {
		t.Errorf("expected Name=%q, got %q", "webserver.home.example.com", hostnames[0].Name)
	}
}

func TestExtract_DomainWithLeadingDot(t *testing.T) {
	src := New(WithDomain(".home.example.com")) // leading dot should be stripped
	w := workload.Workload{
		Platform: workload.PlatformProxmox,
		Name:     "db",
		Metadata: map[string]string{"ip": "10.1.20.6", "node": "pve-01", "vmid": "101"},
	}
	hostnames, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hostnames) != 1 {
		t.Fatalf("expected 1 hostname, got %d", len(hostnames))
	}
	if hostnames[0].Name != "db.home.example.com" {
		t.Errorf("expected Name=%q, got %q", "db.home.example.com", hostnames[0].Name)
	}
}

func TestSupportedPlatforms(t *testing.T) {
	src := New()
	platforms := src.SupportedPlatforms()
	if len(platforms) != 1 {
		t.Fatalf("expected 1 supported platform, got %d", len(platforms))
	}
	if platforms[0] != workload.PlatformProxmox {
		t.Errorf("expected PlatformProxmox, got %q", platforms[0])
	}
}

func TestSupportsDiscovery(t *testing.T) {
	src := New()
	if src.SupportsDiscovery() {
		t.Error("expected SupportsDiscovery() to return false")
	}
}

func TestName(t *testing.T) {
	src := New()
	if src.Name() != "proxmox" {
		t.Errorf("expected Name()=%q, got %q", "proxmox", src.Name())
	}
}

func TestExtract_TargetModeInstance_OmitsRecordHints(t *testing.T) {
	src := New(
		WithDomain("home.example.com"),
		WithTargetMode(TargetModeInstance),
	)
	w := workload.Workload{
		Platform: workload.PlatformProxmox,
		Name:     "webserver",
		Metadata: map[string]string{"ip": "10.1.20.5", "node": "pve-00", "vmid": "100"},
	}
	hostnames, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hostnames) != 1 {
		t.Fatalf("expected 1 hostname, got %d", len(hostnames))
	}
	h := hostnames[0]
	if h.Name != "webserver.home.example.com" {
		t.Errorf("expected Name=%q, got %q", "webserver.home.example.com", h.Name)
	}
	if h.RecordHints != nil {
		t.Errorf("expected RecordHints=nil in instance mode, got %+v", h.RecordHints)
	}
}

func TestExtract_TargetModeInstance_StillSkipsNoIP(t *testing.T) {
	// IP existence acts as the liveness gate even in instance mode.
	src := New(
		WithDomain("home.example.com"),
		WithTargetMode(TargetModeInstance),
	)
	w := workload.Workload{
		Platform: workload.PlatformProxmox,
		Name:     "webserver",
		Metadata: map[string]string{"node": "pve-00", "vmid": "100"},
	}
	hostnames, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hostnames) != 0 {
		t.Errorf("expected no hostnames when IP missing (liveness gate), got %d", len(hostnames))
	}
}

func TestExtract_DefaultMode_PreservesGuestIPBehavior(t *testing.T) {
	// Regression guard: zero-value / default construction must keep emitting
	// guest-ip A records to avoid breaking existing users.
	src := New(WithDomain("home.example.com"))
	w := workload.Workload{
		Platform: workload.PlatformProxmox,
		Name:     "webserver",
		Metadata: map[string]string{"ip": "10.1.20.5", "node": "pve-00", "vmid": "100"},
	}
	hostnames, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hostnames) != 1 {
		t.Fatalf("expected 1 hostname, got %d", len(hostnames))
	}
	h := hostnames[0]
	if h.RecordHints == nil {
		t.Fatal("expected RecordHints to be set in default (guest-ip) mode")
	}
	if h.RecordHints.Type != "A" || h.RecordHints.Target != "10.1.20.5" {
		t.Errorf("expected A/%s, got %s/%s", "10.1.20.5", h.RecordHints.Type, h.RecordHints.Target)
	}
}

func TestParseTargetMode(t *testing.T) {
	tests := []struct {
		in      string
		want    TargetMode
		wantErr bool
	}{
		{"", TargetModeGuestIP, false},
		{"guest-ip", TargetModeGuestIP, false},
		{"GUEST-IP", TargetModeGuestIP, false},
		{"vm-ip", "", true},
		{"  instance  ", TargetModeInstance, false},
		{"instance", TargetModeInstance, false},
		{"node-ip", "", true},
		{"garbage", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseTargetMode(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseTargetMode(%q) err=%v wantErr=%v", tt.in, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseTargetMode(%q)=%q want %q", tt.in, got, tt.want)
			}
		})
	}
}
