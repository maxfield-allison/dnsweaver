package config

import (
	"os"
	"testing"
)

func TestNormalizePlatform(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"docker lowercase", "docker", "docker"},
		{"docker mixed case", "Docker", "docker"},
		{"kubernetes", "kubernetes", "kubernetes"},
		{"both", "both", "both"},
		{"none", "none", "none"},
		{"none uppercase", "NONE", "none"},
		{"standalone alias", "standalone", "none"},
		{"standalone mixed case", "Standalone", "none"},
		{"standalone with whitespace", "  standalone  ", "none"},
		{"unknown passthrough lowercased", "K8S", "k8s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizePlatform(tt.in); got != tt.want {
				t.Errorf("normalizePlatform(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestLoadGlobalConfig_PlatformNone(t *testing.T) {
	clearGlobalEnv(t)
	os.Setenv("DNSWEAVER_PLATFORM", "none")
	defer os.Unsetenv("DNSWEAVER_PLATFORM")

	cfg, errs := loadGlobalConfig()
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if cfg.Platform != "none" {
		t.Errorf("Platform = %q, want %q", cfg.Platform, "none")
	}
}

func TestLoadGlobalConfig_PlatformStandaloneAlias(t *testing.T) {
	clearGlobalEnv(t)
	os.Setenv("DNSWEAVER_PLATFORM", "standalone")
	defer os.Unsetenv("DNSWEAVER_PLATFORM")

	cfg, errs := loadGlobalConfig()
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if cfg.Platform != "none" {
		t.Errorf("Platform = %q, want %q (standalone should normalize to none)", cfg.Platform, "none")
	}
}

func TestLoadGlobalConfig_PlatformInvalid(t *testing.T) {
	clearGlobalEnv(t)
	os.Setenv("DNSWEAVER_PLATFORM", "vmware")
	defer os.Unsetenv("DNSWEAVER_PLATFORM")

	_, errs := loadGlobalConfig()
	if len(errs) == 0 {
		t.Fatal("expected a validation error for invalid platform, got none")
	}
}

// TestConfig_UsePlatformClients_None verifies that the "none" platform creates
// neither a Docker nor a Kubernetes client, which is what lets dnsweaver run as
// a standalone binary (issue #116).
func TestConfig_UsePlatformClients_None(t *testing.T) {
	cfg := &Config{Global: &GlobalConfig{Platform: "none"}}
	if cfg.UseDocker() {
		t.Error("UseDocker() = true for platform none, want false")
	}
	if cfg.UseKubernetes() {
		t.Error("UseKubernetes() = true for platform none, want false")
	}
}
