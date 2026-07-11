package incus

import (
	"context"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

func incusWorkload(name, ip string, labels map[string]string) workload.Workload {
	return workload.Workload{
		Platform: workload.PlatformIncus,
		Name:     name,
		Labels:   labels,
		Metadata: map[string]string{"ip": ip, "project": "default", "type": "container"},
	}
}

func TestExtract_SkipsNonIncus(t *testing.T) {
	src := New(WithDomain("home.example.com"))
	w := workload.Workload{
		Platform: workload.PlatformDocker,
		Name:     "some-container",
		Metadata: map[string]string{"ip": "10.0.0.5"},
	}
	got, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no hostnames for non-incus workload, got %d", len(got))
	}
}

func TestExtract_SkipsNoIP(t *testing.T) {
	src := New(WithDomain("home.example.com"))
	w := workload.Workload{
		Platform: workload.PlatformIncus,
		Name:     "web",
		Metadata: map[string]string{"project": "default"},
	}
	got, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no hostnames when IP missing, got %d", len(got))
	}
}

func TestExtract_NoDomainPlainName_Skips(t *testing.T) {
	src := New()
	w := incusWorkload("web", "10.0.0.5", nil)
	got, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no hostnames when no domain and name not FQDN, got %d", len(got))
	}
}

func TestExtract_WithDomain(t *testing.T) {
	src := New(WithDomain("home.example.com"))
	w := incusWorkload("web", "10.0.0.5", nil)
	got, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 hostname, got %d", len(got))
	}
	if got[0].Name != "web.home.example.com" {
		t.Errorf("Name = %q, want web.home.example.com", got[0].Name)
	}
	if got[0].Source != "incus" {
		t.Errorf("Source = %q, want incus", got[0].Source)
	}
	if got[0].Router != "default/web" {
		t.Errorf("Router = %q, want default/web", got[0].Router)
	}
	if got[0].RecordHints == nil || got[0].RecordHints.Type != "A" || got[0].RecordHints.Target != "10.0.0.5" {
		t.Errorf("RecordHints = %+v, want A/10.0.0.5", got[0].RecordHints)
	}
}

func TestExtract_FQDNNameUsedDirectly(t *testing.T) {
	src := New(WithDomain("home.example.com"))
	w := incusWorkload("db.corp.example.com", "10.0.0.6", nil)
	got, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "db.corp.example.com" {
		t.Fatalf("expected FQDN name used directly, got %+v", got)
	}
}

func TestExtract_HostnameLabelOverride(t *testing.T) {
	src := New(WithDomain("home.example.com"))
	w := incusWorkload("web", "10.0.0.5", map[string]string{
		hostnameLabel: "custom.example.net",
	})
	got, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "custom.example.net" {
		t.Fatalf("expected override hostname, got %+v", got)
	}
}

// TestExtract_ComposeHostnameLabel verifies the incus-compose "dnsweaver.hostname"
// label (de-prefixed from "user.label.dnsweaver.hostname") overrides the derived
// hostname.
func TestExtract_ComposeHostnameLabel(t *testing.T) {
	src := New(WithDomain("home.example.com"))
	w := incusWorkload("web", "10.0.0.5", map[string]string{
		composeHostnameLabel: "compose.example.net",
	})
	got, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "compose.example.net" {
		t.Fatalf("expected compose override hostname, got %+v", got)
	}
}

// TestExtract_NativeHostnameLabelWins verifies the native "user.dnsweaver.hostname"
// key takes precedence over the incus-compose "dnsweaver.hostname" label.
func TestExtract_NativeHostnameLabelWins(t *testing.T) {
	src := New(WithDomain("home.example.com"))
	w := incusWorkload("web", "10.0.0.5", map[string]string{
		hostnameLabel:        "native.example.net",
		composeHostnameLabel: "compose.example.net",
	})
	got, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "native.example.net" {
		t.Fatalf("expected native override to win, got %+v", got)
	}
}

func TestExtract_InstanceTargetMode_NoRecordHints(t *testing.T) {
	src := New(WithDomain("home.example.com"), WithTargetMode(TargetModeInstance))
	w := incusWorkload("web", "10.0.0.5", nil)
	got, err := src.Extract(context.Background(), w)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 hostname, got %d", len(got))
	}
	if got[0].RecordHints != nil {
		t.Errorf("expected nil RecordHints in instance mode, got %+v", got[0].RecordHints)
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
		{"instance", TargetModeInstance, false},
		{"INSTANCE", TargetModeInstance, false},
		{"bogus", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseTargetMode(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSourceMetadata(t *testing.T) {
	src := New()
	if src.Name() != "incus" {
		t.Errorf("Name = %q, want incus", src.Name())
	}
	if src.SupportsDiscovery() {
		t.Error("SupportsDiscovery should be false")
	}
	platforms := src.SupportedPlatforms()
	if len(platforms) != 1 || platforms[0] != workload.PlatformIncus {
		t.Errorf("SupportedPlatforms = %v, want [incus]", platforms)
	}
	if got, _ := src.Discover(context.Background()); got != nil {
		t.Errorf("Discover should return nil, got %v", got)
	}
	var _ source.Source = src
}
