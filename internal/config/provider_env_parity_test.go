package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

func TestProviderFileEnvironmentOverrides(t *testing.T) {
	for key, value := range map[string]string{
		"TYPE": "cloudflare", "RECORD_TYPE": "AAAA", "TARGET": "2001:db8::1",
		"TARGET_MODE": "public", "TARGET_REFRESH_INTERVAL": "5m",
		"TARGET_PUBLIC_ENDPOINTS": "https://example.com/ip", "TTL": "45", "MODE": "additive",
		"DOMAINS_REGEX": `.*\.example\.org`, "EXCLUDE_DOMAINS_REGEX": `private\..*`,
		"ENTRYPOINTS": "web, websecure",
	} {
		t.Setenv("DNSWEAVER_PARITY_"+key, value)
	}
	cfg, errs := convertFileProvider(FileProviderConfig{
		Name: "parity", Type: "technitium", RecordType: "A", Target: "192.0.2.1",
		Domains: []string{"*.example.com"}, ExcludeDomains: []string{"private.example.com"},
	}, 300)
	if len(errs) != 0 {
		t.Fatalf("convertFileProvider errors: %v", errs)
	}
	if cfg.TypeName != "cloudflare" || cfg.RecordType != provider.RecordTypeAAAA || cfg.Target != "2001:db8::1" || cfg.TTL != 45 || cfg.Mode != provider.ModeAdditive {
		t.Fatalf("effective provider settings ignored environment: %#v", cfg)
	}
	if cfg.TargetMode != "public" || cfg.TargetRefreshInterval != 5*time.Minute || !reflect.DeepEqual(cfg.TargetPublicEndpoints, []string{"https://example.com/ip"}) {
		t.Fatalf("dynamic target settings ignored environment: %#v", cfg)
	}
	if len(cfg.Domains) != 0 || len(cfg.ExcludeDomains) != 0 || !reflect.DeepEqual(cfg.DomainsRegex, []string{`.*\.example\.org`}) || !reflect.DeepEqual(cfg.ExcludeDomainsRegex, []string{`private\..*`}) {
		t.Fatalf("environment pattern kind did not replace file pattern kind: %#v", cfg)
	}
	if !reflect.DeepEqual(cfg.MetadataFilters["traefik.entrypoint"], []string{"web", "websecure"}) {
		t.Fatalf("entrypoints ignored: %v", cfg.MetadataFilters)
	}
}

func TestProviderFileEnvironmentFillsRequiredFields(t *testing.T) {
	t.Setenv("DNSWEAVER_PARITY_TYPE", "cloudflare")
	t.Setenv("DNSWEAVER_PARITY_DOMAINS", "*.example.com")
	t.Setenv("DNSWEAVER_PARITY_TARGET_MODE", "public")
	cfg, errs := convertFileProvider(FileProviderConfig{Name: "parity"}, 300)
	if len(errs) != 0 || cfg.TargetMode != "public" {
		t.Fatalf("environment must fill file omissions before required validation: cfg=%#v errors=%v", cfg, errs)
	}
}

func TestProviderFileInvalidEnvironmentOverridesFail(t *testing.T) {
	for _, field := range []string{"TTL", "MODE", "RECORD_TYPE", "TARGET_REFRESH_INTERVAL"} {
		t.Run(field, func(t *testing.T) {
			t.Setenv("DNSWEAVER_PARITY_"+field, "invalid")
			_, errs := convertFileProvider(FileProviderConfig{Name: "parity", Type: "cloudflare", Target: "192.0.2.1", Domains: []string{"*.example.com"}}, 300)
			if len(errs) == 0 {
				t.Fatal("invalid environment override silently accepted")
			}
		})
	}
}

func TestProviderEnvironmentPassesFilesystemAndVersionSettings(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(secretPath, []byte("fixture-password"), 0600); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"TYPE": "dnsmasq", "TARGET": "192.0.2.1", "DOMAINS": "*.example.com",
		"CONFIG_DIR": "/fixture/config", "CONFIG_FILE": "records.conf", "API_VERSION": "v6",
		"SSH_HOST": "example.com", "SSH_PORT": "2222", "SSH_USER": "fixture",
		"SSH_KEY_FILE": "/fixture/key", "SSH_KNOWN_HOSTS_FILE": "/fixture/known_hosts",
		"SSH_STRICT_HOST_KEY_CHECKING": "true", "SSH_PASSWORD_FILE": secretPath,
	} {
		t.Setenv("DNSWEAVER_PARITY_"+key, value)
	}
	cfg, errs := loadInstanceConfig("parity", 300)
	if len(errs) != 0 {
		t.Fatalf("loading environment: %v", errs)
	}
	want := map[string]string{"CONFIG_DIR": "/fixture/config", "CONFIG_FILE": "records.conf", "API_VERSION": "v6", "SSH_HOST": "example.com", "SSH_PORT": "2222", "SSH_USER": "fixture", "SSH_KEY_FILE": "/fixture/key", "SSH_KNOWN_HOSTS_FILE": "/fixture/known_hosts", "SSH_STRICT_HOST_KEY_CHECKING": "true", "SSH_PASSWORD": "fixture-password"}
	if !reflect.DeepEqual(cfg.ProviderConfig, want) {
		t.Fatalf("provider settings lost: got %#v want %#v", cfg.ProviderConfig, want)
	}
}

func TestProviderFileConflictingEnvironmentPatternsFail(t *testing.T) {
	for _, fields := range [][]string{{"DOMAINS", "DOMAINS_REGEX"}, {"EXCLUDE_DOMAINS", "EXCLUDE_DOMAINS_REGEX"}} {
		t.Run(fields[0], func(t *testing.T) {
			for _, field := range fields {
				t.Setenv("DNSWEAVER_PARITY_"+field, "example.com")
			}
			_, errs := convertFileProvider(FileProviderConfig{Name: "parity", Type: "cloudflare", Target: "192.0.2.1", Domains: []string{"*.example.com"}}, 300)
			if len(errs) == 0 {
				t.Fatal("conflicting environment patterns must fail")
			}
		})
	}
}

func TestProviderFileUnreadableSSHSecretFails(t *testing.T) {
	t.Setenv("DNSWEAVER_PARITY_SSH_PASSWORD_FILE", filepath.Join(t.TempDir(), "missing"))
	cfg, errs := convertFileProvider(FileProviderConfig{
		Name: "parity", Type: "dnsmasq", Target: "192.0.2.1", Domains: []string{"*.example.com"},
		Config: map[string]string{"SSH_PASSWORD": "old-fixture"},
	}, 300)
	if len(errs) != 1 || errs[0].Field != "DNSWEAVER_PARITY_SSH_PASSWORD_FILE" {
		t.Fatalf("unreadable SSH secret must block configuration: %v", errs)
	}
	if cfg.ProviderConfig["SSH_PASSWORD"] != "old-fixture" {
		t.Fatal("failed secret read must not replace previous value")
	}
}

func TestProviderFileInheritsEffectiveGlobalTTL(t *testing.T) {
	t.Setenv("DNSWEAVER_DEFAULT_TTL", "45")
	path := filepath.Join(t.TempDir(), "config.yaml")
	const contents = `providers:
  - name: implicit
    type: cloudflare
    target: 192.0.2.1
    domains: ["*.example.com"]
  - name: explicit
    type: cloudflare
    target: 192.0.2.1
    domains: ["*.example.org"]
    ttl: 600
`
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	global, instances, _, errs := loadFromFile(path)
	if len(errs) != 0 {
		t.Fatalf("loading file: %v", errs)
	}
	if global.DefaultTTL != 45 || len(instances) != 2 || instances[0].TTL != 45 || instances[1].TTL != 600 {
		t.Fatalf("global environment TTL must affect only inherited TTLs: global=%d instances=%#v", global.DefaultTTL, instances)
	}
}
