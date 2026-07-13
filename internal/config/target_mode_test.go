package config

import (
	"os"
	"testing"
	"time"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

func TestLoadInstanceConfig_TargetMode(t *testing.T) {
	const instanceName = "auto-target"
	clearInstanceEnv(t, instanceName)
	defer clearInstanceEnv(t, instanceName)

	prefix := envPrefix(instanceName)
	os.Setenv(prefix+"TYPE", "technitium")
	os.Setenv(prefix+"TARGET_MODE", "public")
	os.Setenv(prefix+"TARGET_REFRESH_INTERVAL", "2m")
	os.Setenv(prefix+"TARGET_PUBLIC_ENDPOINTS", "https://a.example, https://b.example")
	os.Setenv(prefix+"DOMAINS", "*.example.com")
	// TARGET intentionally omitted; it is optional when TARGET_MODE is set.

	cfg, errs := loadInstanceConfig(instanceName, 300)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if cfg.TargetMode != "public" {
		t.Errorf("TargetMode = %q, want public", cfg.TargetMode)
	}
	if cfg.Target != "" {
		t.Errorf("Target = %q, want empty", cfg.Target)
	}
	if cfg.TargetRefreshInterval != 2*time.Minute {
		t.Errorf("TargetRefreshInterval = %v, want 2m", cfg.TargetRefreshInterval)
	}
	if len(cfg.TargetPublicEndpoints) != 2 {
		t.Errorf("TargetPublicEndpoints = %v, want 2 entries", cfg.TargetPublicEndpoints)
	}
}

func TestValidateTargetRecordType_ModeAware(t *testing.T) {
	tests := []struct {
		name       string
		recordType provider.RecordType
		mode       string
		target     string
		wantErr    bool
	}{
		{"public mode A no target ok", provider.RecordTypeA, "public", "", false},
		{"interface mode AAAA no target ok", provider.RecordTypeAAAA, "interface:eth0", "", false},
		{"mode with valid fallback ok", provider.RecordTypeA, "public", "10.0.0.1", false},
		{"mode with mismatched fallback errors", provider.RecordTypeA, "public", "not-an-ip", true},
		{"cname with mode rejected", provider.RecordTypeCNAME, "public", "", true},
		{"bad mode rejected", provider.RecordTypeA, "bogus", "", true},
		{"no mode still requires valid A target", provider.RecordTypeA, "", "10.0.0.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := &ProviderInstanceConfig{
				Name:       "t",
				RecordType: tt.recordType,
				TargetMode: tt.mode,
				Target:     tt.target,
			}
			errs := validateTargetRecordType(inst)
			if tt.wantErr && len(errs) == 0 {
				t.Errorf("expected error, got none")
			}
			if !tt.wantErr && len(errs) != 0 {
				t.Errorf("unexpected errors: %v", errs)
			}
		})
	}
}
