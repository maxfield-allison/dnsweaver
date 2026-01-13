package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// clearInstanceEnv removes all environment variables for a given instance.
func clearInstanceEnv(t *testing.T, instanceName string) {
	t.Helper()
	prefix := envPrefix(instanceName)
	envVars := []string{
		prefix + "TYPE",
		prefix + "RECORD_TYPE",
		prefix + "TARGET",
		prefix + "TTL",
		prefix + "MODE",
		prefix + "DOMAINS",
		prefix + "DOMAINS_REGEX",
		prefix + "EXCLUDE_DOMAINS",
		prefix + "EXCLUDE_DOMAINS_REGEX",
		prefix + "URL",
		prefix + "TOKEN",
		prefix + "TOKEN_FILE",
		prefix + "ZONE",
		prefix + "ZONE_ID",
		prefix + "API_KEY",
		prefix + "API_KEY_FILE",
		prefix + "API_EMAIL",
	}
	for _, v := range envVars {
		os.Unsetenv(v)
	}
}

func TestParseInstances(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected []string
	}{
		{
			name:     "single instance",
			envValue: "internal-dns",
			expected: []string{"internal-dns"},
		},
		{
			name:     "multiple instances",
			envValue: "internal-dns,public-dns,backup-dns",
			expected: []string{"internal-dns", "public-dns", "backup-dns"},
		},
		{
			name:     "with whitespace",
			envValue: " internal-dns , public-dns , backup-dns ",
			expected: []string{"internal-dns", "public-dns", "backup-dns"},
		},
		{
			name:     "empty value",
			envValue: "",
			expected: nil,
		},
		{
			name:     "empty entries filtered",
			envValue: "internal-dns,,public-dns",
			expected: []string{"internal-dns", "public-dns"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			os.Unsetenv("DNSWEAVER_INSTANCES")
			os.Unsetenv("DNSWEAVER_PROVIDERS")
			if tc.envValue != "" {
				os.Setenv("DNSWEAVER_INSTANCES", tc.envValue)
			}
			defer os.Unsetenv("DNSWEAVER_INSTANCES")
			defer os.Unsetenv("DNSWEAVER_PROVIDERS")

			got := parseInstances()

			if len(got) != len(tc.expected) {
				t.Errorf("parseInstances() returned %d items, want %d: %v", len(got), len(tc.expected), got)
				return
			}
			for i, want := range tc.expected {
				if got[i] != want {
					t.Errorf("parseInstances()[%d] = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

func TestParseInstances_DeprecatedAlias(t *testing.T) {
	// Verify that DNSWEAVER_PROVIDERS still works as a deprecated alias
	os.Unsetenv("DNSWEAVER_INSTANCES")
	os.Unsetenv("DNSWEAVER_PROVIDERS")
	defer os.Unsetenv("DNSWEAVER_INSTANCES")
	defer os.Unsetenv("DNSWEAVER_PROVIDERS")

	os.Setenv("DNSWEAVER_PROVIDERS", "legacy-instance")

	got := parseInstances()

	if len(got) != 1 || got[0] != "legacy-instance" {
		t.Errorf("parseInstances() should accept deprecated DNSWEAVER_PROVIDERS, got %v", got)
	}
}

func TestParseInstances_NewVarTakesPrecedence(t *testing.T) {
	// Verify that DNSWEAVER_INSTANCES takes precedence over DNSWEAVER_PROVIDERS
	os.Unsetenv("DNSWEAVER_INSTANCES")
	os.Unsetenv("DNSWEAVER_PROVIDERS")
	defer os.Unsetenv("DNSWEAVER_INSTANCES")
	defer os.Unsetenv("DNSWEAVER_PROVIDERS")

	os.Setenv("DNSWEAVER_INSTANCES", "new-instance")
	os.Setenv("DNSWEAVER_PROVIDERS", "old-instance")

	got := parseInstances()

	if len(got) != 1 || got[0] != "new-instance" {
		t.Errorf("parseInstances() should prefer DNSWEAVER_INSTANCES, got %v", got)
	}
}

func TestLoadInstanceConfig_Minimal(t *testing.T) {
	const instanceName = "test-instance"
	clearInstanceEnv(t, instanceName)
	defer clearInstanceEnv(t, instanceName)

	prefix := envPrefix(instanceName)
	os.Setenv(prefix+"TYPE", "technitium")
	os.Setenv(prefix+"TARGET", "10.0.0.100")
	os.Setenv(prefix+"DOMAINS", "*.example.com")

	cfg, errs := loadInstanceConfig(instanceName, 300)

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}

	if cfg.Name != instanceName {
		t.Errorf("Name = %q, want %q", cfg.Name, instanceName)
	}
	if cfg.TypeName != "technitium" {
		t.Errorf("TypeName = %q, want %q", cfg.TypeName, "technitium")
	}
	if cfg.RecordType != provider.RecordTypeA {
		t.Errorf("RecordType = %q, want %q", cfg.RecordType, provider.RecordTypeA)
	}
	if cfg.Target != "10.0.0.100" {
		t.Errorf("Target = %q, want %q", cfg.Target, "10.0.0.100")
	}
	if cfg.TTL != 300 {
		t.Errorf("TTL = %d, want %d", cfg.TTL, 300)
	}
	if len(cfg.Domains) != 1 || cfg.Domains[0] != "*.example.com" {
		t.Errorf("Domains = %v, want [*.example.com]", cfg.Domains)
	}
}

func TestLoadInstanceConfig_Complete(t *testing.T) {
	const instanceName = "internal-dns"
	clearInstanceEnv(t, instanceName)
	defer clearInstanceEnv(t, instanceName)

	// Create temp file for token
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("secret-token"), 0600); err != nil {
		t.Fatal(err)
	}

	prefix := envPrefix(instanceName)
	os.Setenv(prefix+"TYPE", "technitium")
	os.Setenv(prefix+"RECORD_TYPE", "A")
	os.Setenv(prefix+"TARGET", "10.0.0.100")
	os.Setenv(prefix+"TTL", "600")
	os.Setenv(prefix+"DOMAINS", "*.internal.example.com,app.example.com")
	os.Setenv(prefix+"EXCLUDE_DOMAINS", "admin.internal.example.com")
	os.Setenv(prefix+"URL", "http://dns.example.com:5380")
	os.Setenv(prefix+"TOKEN_FILE", tokenFile)
	os.Setenv(prefix+"ZONE", "internal.example.com")

	cfg, errs := loadInstanceConfig(instanceName, 300)

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}

	if cfg.TypeName != "technitium" {
		t.Errorf("TypeName = %q, want %q", cfg.TypeName, "technitium")
	}
	if cfg.RecordType != provider.RecordTypeA {
		t.Errorf("RecordType = %q, want %q", cfg.RecordType, provider.RecordTypeA)
	}
	if cfg.Target != "10.0.0.100" {
		t.Errorf("Target = %q, want %q", cfg.Target, "10.0.0.100")
	}
	if cfg.TTL != 600 {
		t.Errorf("TTL = %d, want %d", cfg.TTL, 600)
	}
	if len(cfg.Domains) != 2 {
		t.Errorf("Domains length = %d, want 2", len(cfg.Domains))
	}
	if len(cfg.ExcludeDomains) != 1 {
		t.Errorf("ExcludeDomains length = %d, want 1", len(cfg.ExcludeDomains))
	}

	// Check provider config
	if cfg.ProviderConfig["URL"] != "http://dns.example.com:5380" {
		t.Errorf("ProviderConfig[URL] = %q, want %q", cfg.ProviderConfig["URL"], "http://dns.example.com:5380")
	}
	if cfg.ProviderConfig["TOKEN"] != "secret-token" {
		t.Errorf("ProviderConfig[TOKEN] = %q, want %q", cfg.ProviderConfig["TOKEN"], "secret-token")
	}
	if cfg.ProviderConfig["ZONE"] != "internal.example.com" {
		t.Errorf("ProviderConfig[ZONE] = %q, want %q", cfg.ProviderConfig["ZONE"], "internal.example.com")
	}
}

func TestLoadInstanceConfig_CNAMERecord(t *testing.T) {
	const instanceName = "cname-test"
	clearInstanceEnv(t, instanceName)
	defer clearInstanceEnv(t, instanceName)

	prefix := envPrefix(instanceName)
	os.Setenv(prefix+"TYPE", "cloudflare")
	os.Setenv(prefix+"RECORD_TYPE", "CNAME")
	os.Setenv(prefix+"TARGET", "example.com")
	os.Setenv(prefix+"DOMAINS", "*.example.com")

	cfg, errs := loadInstanceConfig(instanceName, 300)

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}

	if cfg.RecordType != provider.RecordTypeCNAME {
		t.Errorf("RecordType = %q, want %q", cfg.RecordType, provider.RecordTypeCNAME)
	}
	if cfg.Target != "example.com" {
		t.Errorf("Target = %q, want %q", cfg.Target, "example.com")
	}
}

func TestLoadInstanceConfig_RegexDomains(t *testing.T) {
	const instanceName = "regex-test"
	clearInstanceEnv(t, instanceName)
	defer clearInstanceEnv(t, instanceName)

	prefix := envPrefix(instanceName)
	os.Setenv(prefix+"TYPE", "technitium")
	os.Setenv(prefix+"TARGET", "10.0.0.1")
	os.Setenv(prefix+"DOMAINS_REGEX", "^[a-z0-9-]+\\.example\\.com$")
	os.Setenv(prefix+"EXCLUDE_DOMAINS_REGEX", "^(test|dev)\\.")

	cfg, errs := loadInstanceConfig(instanceName, 300)

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}

	if len(cfg.Domains) != 0 {
		t.Errorf("Domains should be empty when using regex, got %v", cfg.Domains)
	}
	if len(cfg.DomainsRegex) != 1 {
		t.Errorf("DomainsRegex length = %d, want 1", len(cfg.DomainsRegex))
	}
	if len(cfg.ExcludeDomainsRegex) != 1 {
		t.Errorf("ExcludeDomainsRegex length = %d, want 1", len(cfg.ExcludeDomainsRegex))
	}
}

func TestLoadInstanceConfig_MissingRequired(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(prefix string)
		errMatch string
	}{
		{
			name:     "missing type",
			setup:    func(p string) { os.Setenv(p+"TARGET", "10.0.0.1"); os.Setenv(p+"DOMAINS", "*") },
			errMatch: "TYPE",
		},
		{
			name:     "missing target",
			setup:    func(p string) { os.Setenv(p+"TYPE", "technitium"); os.Setenv(p+"DOMAINS", "*") },
			errMatch: "TARGET",
		},
		{
			name:     "missing domains",
			setup:    func(p string) { os.Setenv(p+"TYPE", "technitium"); os.Setenv(p+"TARGET", "10.0.0.1") },
			errMatch: "DOMAINS",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const instanceName = "missing-test"
			clearInstanceEnv(t, instanceName)
			defer clearInstanceEnv(t, instanceName)

			prefix := envPrefix(instanceName)
			tc.setup(prefix)

			_, errs := loadInstanceConfig(instanceName, 300)

			if len(errs) == 0 {
				t.Error("expected validation error, got none")
				return
			}

			found := false
			for _, err := range errs {
				if containsSubstring(err, tc.errMatch) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected error containing %q, got %v", tc.errMatch, errs)
			}
		})
	}
}

func TestLoadInstanceConfig_InvalidValues(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(prefix string)
		errMatch string
	}{
		{
			name: "invalid record type",
			setup: func(p string) {
				os.Setenv(p+"TYPE", "technitium")
				os.Setenv(p+"RECORD_TYPE", "MX")
				os.Setenv(p+"TARGET", "10.0.0.1")
				os.Setenv(p+"DOMAINS", "*")
			},
			errMatch: "RECORD_TYPE",
		},
		{
			name: "invalid TTL not a number",
			setup: func(p string) {
				os.Setenv(p+"TYPE", "technitium")
				os.Setenv(p+"TARGET", "10.0.0.1")
				os.Setenv(p+"TTL", "abc")
				os.Setenv(p+"DOMAINS", "*")
			},
			errMatch: "TTL",
		},
		{
			name: "invalid TTL negative",
			setup: func(p string) {
				os.Setenv(p+"TYPE", "technitium")
				os.Setenv(p+"TARGET", "10.0.0.1")
				os.Setenv(p+"TTL", "-10")
				os.Setenv(p+"DOMAINS", "*")
			},
			errMatch: "TTL",
		},
		{
			name: "both DOMAINS and DOMAINS_REGEX",
			setup: func(p string) {
				os.Setenv(p+"TYPE", "technitium")
				os.Setenv(p+"TARGET", "10.0.0.1")
				os.Setenv(p+"DOMAINS", "*")
				os.Setenv(p+"DOMAINS_REGEX", ".*")
			},
			errMatch: "cannot set both",
		},
		{
			name: "both EXCLUDE_DOMAINS and EXCLUDE_DOMAINS_REGEX",
			setup: func(p string) {
				os.Setenv(p+"TYPE", "technitium")
				os.Setenv(p+"TARGET", "10.0.0.1")
				os.Setenv(p+"DOMAINS", "*")
				os.Setenv(p+"EXCLUDE_DOMAINS", "admin.*")
				os.Setenv(p+"EXCLUDE_DOMAINS_REGEX", "^admin\\.")
			},
			errMatch: "cannot set both",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const instanceName = "invalid-test"
			clearInstanceEnv(t, instanceName)
			defer clearInstanceEnv(t, instanceName)

			prefix := envPrefix(instanceName)
			tc.setup(prefix)

			_, errs := loadInstanceConfig(instanceName, 300)

			if len(errs) == 0 {
				t.Error("expected validation error, got none")
				return
			}

			found := false
			for _, err := range errs {
				if containsSubstring(err, tc.errMatch) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected error containing %q, got %v", tc.errMatch, errs)
			}
		})
	}
}

func TestLoadInstanceConfig_TypeNormalization(t *testing.T) {
	const instanceName = "type-test"
	clearInstanceEnv(t, instanceName)
	defer clearInstanceEnv(t, instanceName)

	prefix := envPrefix(instanceName)
	os.Setenv(prefix+"TYPE", "TECHNITIUM") // Uppercase
	os.Setenv(prefix+"TARGET", "10.0.0.1")
	os.Setenv(prefix+"DOMAINS", "*")

	cfg, errs := loadInstanceConfig(instanceName, 300)

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}

	if cfg.TypeName != "technitium" {
		t.Errorf("TypeName = %q, want %q (lowercased)", cfg.TypeName, "technitium")
	}
}

func TestSplitPatterns(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"*.example.com", []string{"*.example.com"}},
		{"*.example.com,*.test.com", []string{"*.example.com", "*.test.com"}},
		{" *.example.com , *.test.com ", []string{"*.example.com", "*.test.com"}},
		{"", nil},
		{"a,,b", []string{"a", "b"}},
	}

	for _, tc := range tests {
		got := splitPatterns(tc.input)

		if len(got) != len(tc.expected) {
			t.Errorf("splitPatterns(%q) = %v, want %v", tc.input, got, tc.expected)
			continue
		}
		for i := range tc.expected {
			if got[i] != tc.expected[i] {
				t.Errorf("splitPatterns(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.expected[i])
			}
		}
	}
}

func TestLoadInstanceConfig_OperationalMode(t *testing.T) {
	tests := []struct {
		name        string
		modeEnv     string
		wantMode    provider.OperationalMode
		wantErr     bool
		errContains string
	}{
		{
			name:     "default mode when not set",
			modeEnv:  "",
			wantMode: provider.ModeManaged,
		},
		{
			name:     "explicit managed mode",
			modeEnv:  "managed",
			wantMode: provider.ModeManaged,
		},
		{
			name:     "authoritative mode",
			modeEnv:  "authoritative",
			wantMode: provider.ModeAuthoritative,
		},
		{
			name:     "additive mode",
			modeEnv:  "additive",
			wantMode: provider.ModeAdditive,
		},
		{
			name:     "uppercase mode",
			modeEnv:  "AUTHORITATIVE",
			wantMode: provider.ModeAuthoritative,
		},
		{
			name:        "invalid mode",
			modeEnv:     "readonly",
			wantErr:     true,
			errContains: "invalid operational mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const instanceName = "mode-test"
			clearInstanceEnv(t, instanceName)
			defer clearInstanceEnv(t, instanceName)

			prefix := envPrefix(instanceName)
			os.Setenv(prefix+"TYPE", "technitium")
			os.Setenv(prefix+"TARGET", "10.0.0.1")
			os.Setenv(prefix+"DOMAINS", "*.example.com")
			if tt.modeEnv != "" {
				os.Setenv(prefix+"MODE", tt.modeEnv)
			}

			cfg, errs := loadInstanceConfig(instanceName, 300)

			if tt.wantErr {
				if len(errs) == 0 {
					t.Error("expected error but got none")
					return
				}
				found := false
				for _, err := range errs {
					if strings.Contains(err, tt.errContains) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got: %v", tt.errContains, errs)
				}
				return
			}

			if len(errs) > 0 {
				t.Errorf("unexpected errors: %v", errs)
				return
			}

			if cfg.Mode != tt.wantMode {
				t.Errorf("Mode = %q, want %q", cfg.Mode, tt.wantMode)
			}
		})
	}
}

func TestMergeProviderEnvOverrides(t *testing.T) {
	t.Run("overrides TOKEN from env var", func(t *testing.T) {
		instanceName := "test-override"
		prefix := envPrefix(instanceName)
		defer os.Unsetenv(prefix + "TOKEN")

		cfg := &ProviderInstanceConfig{
			Name:     instanceName,
			TypeName: "technitium",
			Target:   "10.0.0.1",
			TTL:      300,
			ProviderConfig: map[string]string{
				"URL":   "http://dns:5380",
				"TOKEN": "yaml-token-value",
				"ZONE":  "example.com",
			},
		}

		// Set env var override
		os.Setenv(prefix+"TOKEN", "env-token-value")

		mergeProviderEnvOverrides(cfg)

		if cfg.ProviderConfig["TOKEN"] != "env-token-value" {
			t.Errorf("TOKEN = %q, want %q", cfg.ProviderConfig["TOKEN"], "env-token-value")
		}
		// Other values should remain unchanged
		if cfg.ProviderConfig["URL"] != "http://dns:5380" {
			t.Errorf("URL should not change, got %q", cfg.ProviderConfig["URL"])
		}
	})

	t.Run("overrides TOKEN from _FILE env var", func(t *testing.T) {
		instanceName := "test-file-override"
		prefix := envPrefix(instanceName)

		// Create temp file with secret
		tmpDir := t.TempDir()
		secretFile := filepath.Join(tmpDir, "token-secret")
		if err := os.WriteFile(secretFile, []byte("file-secret-token\n"), 0600); err != nil {
			t.Fatal(err)
		}
		defer os.Unsetenv(prefix + "TOKEN_FILE")

		cfg := &ProviderInstanceConfig{
			Name:     instanceName,
			TypeName: "technitium",
			Target:   "10.0.0.1",
			TTL:      300,
			ProviderConfig: map[string]string{
				"URL":   "http://dns:5380",
				"TOKEN": "yaml-token-value",
				"ZONE":  "example.com",
			},
		}

		// Set env var to point to file
		os.Setenv(prefix+"TOKEN_FILE", secretFile)

		mergeProviderEnvOverrides(cfg)

		if cfg.ProviderConfig["TOKEN"] != "file-secret-token" {
			t.Errorf("TOKEN = %q, want %q", cfg.ProviderConfig["TOKEN"], "file-secret-token")
		}
	})

	t.Run("overrides TARGET from env var", func(t *testing.T) {
		instanceName := "test-target-override"
		prefix := envPrefix(instanceName)
		defer os.Unsetenv(prefix + "TARGET")

		cfg := &ProviderInstanceConfig{
			Name:     instanceName,
			TypeName: "technitium",
			Target:   "10.0.0.1",
			TTL:      300,
			ProviderConfig: map[string]string{
				"URL": "http://dns:5380",
			},
		}

		// Set env var override
		os.Setenv(prefix+"TARGET", "192.168.1.1")

		mergeProviderEnvOverrides(cfg)

		if cfg.Target != "192.168.1.1" {
			t.Errorf("Target = %q, want %q", cfg.Target, "192.168.1.1")
		}
	})

	t.Run("overrides TTL from env var", func(t *testing.T) {
		instanceName := "test-ttl-override"
		prefix := envPrefix(instanceName)
		defer os.Unsetenv(prefix + "TTL")

		cfg := &ProviderInstanceConfig{
			Name:     instanceName,
			TypeName: "technitium",
			Target:   "10.0.0.1",
			TTL:      300,
			ProviderConfig: map[string]string{
				"URL": "http://dns:5380",
			},
		}

		os.Setenv(prefix+"TTL", "60")

		mergeProviderEnvOverrides(cfg)

		if cfg.TTL != 60 {
			t.Errorf("TTL = %d, want %d", cfg.TTL, 60)
		}
	})

	t.Run("overrides MODE from env var", func(t *testing.T) {
		instanceName := "test-mode-override"
		prefix := envPrefix(instanceName)
		defer os.Unsetenv(prefix + "MODE")

		cfg := &ProviderInstanceConfig{
			Name:     instanceName,
			TypeName: "technitium",
			Target:   "10.0.0.1",
			TTL:      300,
			Mode:     provider.ModeManaged,
			ProviderConfig: map[string]string{
				"URL": "http://dns:5380",
			},
		}

		os.Setenv(prefix+"MODE", "authoritative")

		mergeProviderEnvOverrides(cfg)

		if cfg.Mode != provider.ModeAuthoritative {
			t.Errorf("Mode = %q, want %q", cfg.Mode, provider.ModeAuthoritative)
		}
	})

	t.Run("does not override when env var not set", func(t *testing.T) {
		instanceName := "test-no-override"
		prefix := envPrefix(instanceName)

		// Ensure no env vars are set
		os.Unsetenv(prefix + "TOKEN")
		os.Unsetenv(prefix + "TOKEN_FILE")
		os.Unsetenv(prefix + "TARGET")
		os.Unsetenv(prefix + "TTL")
		os.Unsetenv(prefix + "MODE")

		cfg := &ProviderInstanceConfig{
			Name:     instanceName,
			TypeName: "technitium",
			Target:   "10.0.0.1",
			TTL:      300,
			Mode:     provider.ModeManaged,
			ProviderConfig: map[string]string{
				"URL":   "http://dns:5380",
				"TOKEN": "yaml-token",
				"ZONE":  "example.com",
			},
		}

		mergeProviderEnvOverrides(cfg)

		// All values should remain unchanged
		if cfg.ProviderConfig["TOKEN"] != "yaml-token" {
			t.Errorf("TOKEN changed unexpectedly to %q", cfg.ProviderConfig["TOKEN"])
		}
		if cfg.Target != "10.0.0.1" {
			t.Errorf("Target changed unexpectedly to %q", cfg.Target)
		}
		if cfg.TTL != 300 {
			t.Errorf("TTL changed unexpectedly to %d", cfg.TTL)
		}
		if cfg.Mode != provider.ModeManaged {
			t.Errorf("Mode changed unexpectedly to %q", cfg.Mode)
		}
	})

	t.Run("initializes nil ProviderConfig map", func(t *testing.T) {
		instanceName := "test-nil-map"
		prefix := envPrefix(instanceName)
		defer os.Unsetenv(prefix + "TOKEN")

		cfg := &ProviderInstanceConfig{
			Name:           instanceName,
			TypeName:       "technitium",
			Target:         "10.0.0.1",
			TTL:            300,
			ProviderConfig: nil, // nil map
		}

		os.Setenv(prefix+"TOKEN", "new-token")

		mergeProviderEnvOverrides(cfg)

		if cfg.ProviderConfig == nil {
			t.Error("ProviderConfig should be initialized")
		}
		if cfg.ProviderConfig["TOKEN"] != "new-token" {
			t.Errorf("TOKEN = %q, want %q", cfg.ProviderConfig["TOKEN"], "new-token")
		}
	})
}
