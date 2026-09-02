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

		_, err := ResolveHealthPort(filepath.Join(t.TempDir(), "missing.yml"))
		if err == nil {
			t.Fatal("ResolveHealthPort() error = nil, want an error")
		}
	})
}

func writeHealthConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
