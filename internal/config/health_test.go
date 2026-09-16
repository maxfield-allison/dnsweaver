package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveHealthPort(t *testing.T) {
	t.Run("compiled default", func(t *testing.T) {
		t.Setenv("DNSWEAVER_HEALTH_PORT", "")
		t.Setenv("DNSWEAVER_HEALTH_ADDRESS", "")
		t.Setenv("DNSWEAVER_HEALTH_ALLOW_NETWORK", "")

		port, err := ResolveHealthPort("")
		if err != nil {
			t.Fatalf("ResolveHealthPort() error = %v", err)
		}
		if port != DefaultHealthPort {
			t.Errorf("ResolveHealthPort() = %d, want %d", port, DefaultHealthPort)
		}
	})

	t.Run("YAML value", func(t *testing.T) {
		t.Setenv("DNSWEAVER_HEALTH_PORT", "")
		t.Setenv("DNSWEAVER_HEALTH_ADDRESS", "")
		t.Setenv("DNSWEAVER_HEALTH_ALLOW_NETWORK", "")
		path := writeHealthConfig(t, "server:\n  port: 18080\n")

		port, err := ResolveHealthPort(path)
		if err != nil {
			t.Fatalf("ResolveHealthPort() error = %v", err)
		}
		if port != 18080 {
			t.Errorf("ResolveHealthPort() = %d, want 18080", port)
		}
	})

	t.Run("environment overrides YAML", func(t *testing.T) {
		t.Setenv("DNSWEAVER_HEALTH_PORT", "19090")
		t.Setenv("DNSWEAVER_HEALTH_ADDRESS", "")
		t.Setenv("DNSWEAVER_HEALTH_ALLOW_NETWORK", "")
		path := writeHealthConfig(t, "server:\n  port: 18080\n")

		port, err := ResolveHealthPort(path)
		if err != nil {
			t.Fatalf("ResolveHealthPort() error = %v", err)
		}
		if port != 19090 {
			t.Errorf("ResolveHealthPort() = %d, want 19090", port)
		}
	})

	t.Run("invalid environment value", func(t *testing.T) {
		t.Setenv("DNSWEAVER_HEALTH_PORT", "not-a-port")
		t.Setenv("DNSWEAVER_HEALTH_ADDRESS", "")
		t.Setenv("DNSWEAVER_HEALTH_ALLOW_NETWORK", "")

		_, err := ResolveHealthPort("")
		if err == nil {
			t.Fatal("ResolveHealthPort() error = nil, want an error")
		}
		if !strings.Contains(err.Error(), "DNSWEAVER_HEALTH_PORT") {
			t.Errorf("ResolveHealthPort() error = %q, want field name", err)
		}
	})

	t.Run("missing YAML file", func(t *testing.T) {
		t.Setenv("DNSWEAVER_HEALTH_PORT", "")
		t.Setenv("DNSWEAVER_HEALTH_ADDRESS", "")
		t.Setenv("DNSWEAVER_HEALTH_ALLOW_NETWORK", "")

		_, err := ResolveHealthPort(filepath.Join(t.TempDir(), "missing.yml"))
		if err == nil {
			t.Fatal("ResolveHealthPort() error = nil, want an error")
		}
	})
}

func TestResolveHealthEndpoint(t *testing.T) {
	t.Setenv("DNSWEAVER_HEALTH_PORT", "")
	t.Setenv("DNSWEAVER_HEALTH_ADDRESS", "")
	t.Setenv("DNSWEAVER_HEALTH_ALLOW_NETWORK", "")

	t.Run("YAML loopback", func(t *testing.T) {
		path := writeHealthConfig(t, "server:\n  address: ::1\n  port: 18080\n")
		address, port, err := ResolveHealthEndpoint(path)
		if err != nil {
			t.Fatalf("ResolveHealthEndpoint() error = %v", err)
		}
		if address != "::1" || port != 18080 {
			t.Fatalf("ResolveHealthEndpoint() = (%q, %d), want (::1, 18080)", address, port)
		}
	})

	t.Run("YAML address interpolation", func(t *testing.T) {
		t.Setenv("MANAGEMENT_ADDRESS", "::1")
		path := writeHealthConfig(t, "server:\n  address: ${MANAGEMENT_ADDRESS}\n")
		address, _, err := ResolveHealthEndpoint(path)
		if err != nil {
			t.Fatalf("ResolveHealthEndpoint() error = %v", err)
		}
		if address != "::1" {
			t.Fatalf("ResolveHealthEndpoint() address = %q, want ::1", address)
		}
	})

	t.Run("YAML network listener denied", func(t *testing.T) {
		path := writeHealthConfig(t, "server:\n  address: 0.0.0.0\n")
		_, _, err := ResolveHealthEndpoint(path)
		if err == nil || !strings.Contains(err.Error(), "DNSWEAVER_HEALTH_ALLOW_NETWORK") {
			t.Fatalf("ResolveHealthEndpoint() error = %v, want network opt-in error", err)
		}
	})

	t.Run("YAML network listener enabled", func(t *testing.T) {
		path := writeHealthConfig(t, "server:\n  address: 0.0.0.0\n  allow_network: true\n")
		address, _, err := ResolveHealthEndpoint(path)
		if err != nil {
			t.Fatalf("ResolveHealthEndpoint() error = %v", err)
		}
		if address != "0.0.0.0" {
			t.Fatalf("ResolveHealthEndpoint() address = %q, want 0.0.0.0", address)
		}
	})
}

func TestHealthListenerConfiguration(t *testing.T) {
	tests := []struct {
		name         string
		address      string
		allowNetwork string
		wantAddress  string
		wantAllow    bool
		wantErr      string
	}{
		{name: "loopback default", wantAddress: DefaultHealthAddress},
		{name: "IPv6 loopback", address: "::1", wantAddress: "::1"},
		{name: "network address denied without opt-in", address: "0.0.0.0", wantAddress: "0.0.0.0", wantErr: "DNSWEAVER_HEALTH_ALLOW_NETWORK"},
		{name: "network address explicitly enabled", address: "0.0.0.0", allowNetwork: "true", wantAddress: "0.0.0.0", wantAllow: true},
		{name: "unicast address explicitly enabled", address: "192.0.2.10", allowNetwork: "true", wantAddress: "192.0.2.10", wantAllow: true},
		{name: "hostname rejected", address: "localhost", wantAddress: "localhost", wantErr: "DNSWEAVER_HEALTH_ADDRESS"},
		{name: "address with port rejected", address: "127.0.0.1:8080", wantAddress: "127.0.0.1:8080", wantErr: "DNSWEAVER_HEALTH_ADDRESS"},
		{name: "invalid opt-in boolean rejected", allowNetwork: "sometimes", wantAddress: DefaultHealthAddress, wantErr: "DNSWEAVER_HEALTH_ALLOW_NETWORK"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DNSWEAVER_HEALTH_ADDRESS", tt.address)
			t.Setenv("DNSWEAVER_HEALTH_ALLOW_NETWORK", tt.allowNetwork)
			address, allow, errs := healthListenerFromEnvironment(DefaultHealthAddress, DefaultHealthAllowNetwork)
			if address != tt.wantAddress || allow != tt.wantAllow {
				t.Fatalf("healthListenerFromEnvironment() = (%q, %v), want (%q, %v)", address, allow, tt.wantAddress, tt.wantAllow)
			}
			if tt.wantErr == "" {
				if len(errs) != 0 {
					t.Fatalf("errors = %v, want none", errs)
				}
				return
			}
			if len(errs) == 0 || !strings.Contains(errs[0].Error(), tt.wantErr) {
				t.Fatalf("errors = %v, want %s", errs, tt.wantErr)
			}
		})
	}
}

func writeHealthConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
