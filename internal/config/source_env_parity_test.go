package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestFileSourcesApplyEnvironmentOverrides(t *testing.T) {
	for _, name := range []string{"traefik", "caddy", "dnsweaver", "nginx-proxy", "kubernetes", "proxmox", "incus"} {
		t.Run(name, func(t *testing.T) {
			clearAllEnv(t)
			defer clearAllEnv(t)
			prefix := sourceEnvPrefix(name)
			t.Setenv(prefix+"FILE_PATHS", "/env/one,/env/two")
			t.Setenv(prefix+"FILE_PATTERN", "*.conf")
			t.Setenv(prefix+"POLL_INTERVAL", "12s")
			t.Setenv(prefix+"WATCH_METHOD", "POLL")
			t.Setenv(prefix+"DEFAULT_ENTRYPOINTS", "web, websecure")
			cfg := convertFileSources([]FileSourceConfig{{Name: name, FileDiscovery: &FileFileDiscoveryConfig{Paths: []string{"/file"}, Pattern: "*.yaml", PollInterval: "30s", WatchMethod: "auto"}}})
			got := cfg.Instances[0]
			if !reflect.DeepEqual(got.FileDiscovery.FilePaths, []string{"/env/one", "/env/two"}) || got.FileDiscovery.FilePattern != "*.conf" || got.FileDiscovery.PollInterval != 12*time.Second || got.FileDiscovery.WatchMethod != "poll" || !reflect.DeepEqual(got.DefaultEntryPoints, []string{"web", "websecure"}) {
				t.Fatalf("lost source environment overrides: %+v", got)
			}
		})
	}
}

func TestFileSourceRetainsUnoverriddenSettings(t *testing.T) {
	clearAllEnv(t)
	defer clearAllEnv(t)
	t.Setenv("DNSWEAVER_SOURCE_TRAEFIK_DEFAULT_ENTRYPOINTS", "websecure")
	cfg := convertFileSources([]FileSourceConfig{{Name: "traefik", FileDiscovery: &FileFileDiscoveryConfig{Paths: []string{"/file"}, Pattern: "*.yaml", PollInterval: "30s", WatchMethod: "poll"}}})
	got := cfg.Instances[0]
	if !reflect.DeepEqual(got.FileDiscovery.FilePaths, []string{"/file"}) || got.FileDiscovery.FilePattern != "*.yaml" || got.FileDiscovery.PollInterval != 30*time.Second || got.FileDiscovery.WatchMethod != "poll" || !reflect.DeepEqual(got.DefaultEntryPoints, []string{"websecure"}) {
		t.Fatalf("file/source merge lost settings: %+v", got)
	}
}

func TestLoadDeprecatedSourceOverridesFileList(t *testing.T) {
	clearAllEnv(t)
	defer clearAllEnv(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("sources:\n  - name: traefik\nproviders:\n  - name: dns\n    type: technitium\n    target: 10.0.0.1\n    domains: ['*.example.com']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DNSWEAVER_CONFIG", path)
	t.Setenv("DNSWEAVER_SOURCE", "caddy")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.SourceNames(), []string{"caddy"}) {
		t.Fatalf("source list=%v, want caddy", cfg.SourceNames())
	}
}
