package config

import (
	"testing"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

func TestConvertFileProvider(t *testing.T) {
	tests := []struct {
		name       string
		input      FileProviderConfig
		defaultTTL int
		wantName   string
		wantType   string
		wantTarget string
		wantTTL    int
		wantMode   provider.OperationalMode
		wantErrCnt int
	}{
		{
			name: "valid minimal config",
			input: FileProviderConfig{
				Name:    "test",
				Type:    "technitium",
				Domains: []string{"*.example.com"},
				Target:  "10.0.0.100",
			},
			defaultTTL: 300,
			wantName:   "test",
			wantType:   "technitium",
			wantTarget: "10.0.0.100",
			wantTTL:    300,
			wantMode:   provider.ModeManaged,
			wantErrCnt: 0,
		},
		{
			name: "with custom TTL and mode",
			input: FileProviderConfig{
				Name:       "internal",
				Type:       "cloudflare",
				Domains:    []string{"*.example.com"},
				Target:     "lb.example.com",
				TTL:        600,
				RecordType: "CNAME",
				Mode:       "authoritative",
			},
			defaultTTL: 300,
			wantName:   "internal",
			wantType:   "cloudflare",
			wantTarget: "lb.example.com",
			wantTTL:    600,
			wantMode:   provider.ModeAuthoritative,
			wantErrCnt: 0,
		},
		{
			name: "missing name",
			input: FileProviderConfig{
				Type:    "technitium",
				Domains: []string{"*.example.com"},
				Target:  "10.0.0.100",
			},
			defaultTTL: 300,
			wantErrCnt: 1,
		},
		{
			name: "missing type",
			input: FileProviderConfig{
				Name:    "test",
				Domains: []string{"*.example.com"},
				Target:  "10.0.0.100",
			},
			defaultTTL: 300,
			wantErrCnt: 1,
		},
		{
			name: "missing target",
			input: FileProviderConfig{
				Name:    "test",
				Type:    "technitium",
				Domains: []string{"*.example.com"},
			},
			defaultTTL: 300,
			wantErrCnt: 1,
		},
		{
			name: "missing domains",
			input: FileProviderConfig{
				Name:   "test",
				Type:   "technitium",
				Target: "10.0.0.100",
			},
			defaultTTL: 300,
			wantErrCnt: 1,
		},
		{
			name: "both domains and domains_regex",
			input: FileProviderConfig{
				Name:         "test",
				Type:         "technitium",
				Target:       "10.0.0.100",
				Domains:      []string{"*.example.com"},
				DomainsRegex: []string{".*\\.example\\.com"},
			},
			defaultTTL: 300,
			wantErrCnt: 1,
		},
		{
			name: "invalid record type",
			input: FileProviderConfig{
				Name:       "test",
				Type:       "technitium",
				Domains:    []string{"*.example.com"},
				Target:     "10.0.0.100",
				RecordType: "MX",
			},
			defaultTTL: 300,
			wantErrCnt: 1,
		},
		{
			name: "invalid mode",
			input: FileProviderConfig{
				Name:    "test",
				Type:    "technitium",
				Domains: []string{"*.example.com"},
				Target:  "10.0.0.100",
				Mode:    "invalid",
			},
			defaultTTL: 300,
			wantErrCnt: 1,
		},
		{
			name: "provider config normalization",
			input: FileProviderConfig{
				Name:    "test",
				Type:    "technitium",
				Domains: []string{"*.example.com"},
				Target:  "10.0.0.100",
				Config: map[string]string{
					"url":   "http://dns:5380",
					"Token": "secret123",
				},
			},
			defaultTTL: 300,
			wantName:   "test",
			wantType:   "technitium",
			wantTarget: "10.0.0.100",
			wantTTL:    300,
			wantMode:   provider.ModeManaged,
			wantErrCnt: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, errs := convertFileProvider(tt.input, tt.defaultTTL)

			if len(errs) != tt.wantErrCnt {
				t.Errorf("error count = %d, want %d; errors: %v", len(errs), tt.wantErrCnt, errs)
			}

			if tt.wantErrCnt == 0 {
				if cfg.Name != tt.wantName {
					t.Errorf("Name = %q, want %q", cfg.Name, tt.wantName)
				}
				if cfg.TypeName != tt.wantType {
					t.Errorf("TypeName = %q, want %q", cfg.TypeName, tt.wantType)
				}
				if cfg.Target != tt.wantTarget {
					t.Errorf("Target = %q, want %q", cfg.Target, tt.wantTarget)
				}
				if cfg.TTL != tt.wantTTL {
					t.Errorf("TTL = %d, want %d", cfg.TTL, tt.wantTTL)
				}
				if cfg.Mode != tt.wantMode {
					t.Errorf("Mode = %v, want %v", cfg.Mode, tt.wantMode)
				}
			}
		})
	}
}

func TestConvertFileSources(t *testing.T) {
	tests := []struct {
		name      string
		input     []FileSourceConfig
		wantNil   bool
		wantCount int
	}{
		{
			name:    "empty sources",
			input:   nil,
			wantNil: true,
		},
		{
			name: "single source without file discovery",
			input: []FileSourceConfig{
				{Name: "traefik"},
			},
			wantCount: 1,
		},
		{
			name: "source with file discovery",
			input: []FileSourceConfig{
				{
					Name: "traefik",
					FileDiscovery: &FileFileDiscoveryConfig{
						Paths:        []string{"/config/traefik"},
						Pattern:      "*.yml",
						PollInterval: "30s",
						WatchMethod:  "poll",
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "multiple sources",
			input: []FileSourceConfig{
				{Name: "traefik"},
				{Name: "dnsweaver"},
				{Name: "caddy"},
			},
			wantCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertFileSources(tt.input)

			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil result")
				}
				return
			}

			if result == nil {
				t.Fatalf("unexpected nil result")
			}

			if len(result.Names) != tt.wantCount {
				t.Errorf("Names count = %d, want %d", len(result.Names), tt.wantCount)
			}
			if len(result.Instances) != tt.wantCount {
				t.Errorf("Instances count = %d, want %d", len(result.Instances), tt.wantCount)
			}
		})
	}
}

func TestConvertFileSourcesWithFileDiscovery(t *testing.T) {
	input := []FileSourceConfig{
		{
			Name: "traefik",
			FileDiscovery: &FileFileDiscoveryConfig{
				Paths:        []string{"/config/traefik", "/rules"},
				Pattern:      "*.yaml",
				PollInterval: "2m",
				WatchMethod:  "inotify",
			},
		},
	}

	result := convertFileSources(input)
	if result == nil {
		t.Fatal("unexpected nil result")
	}

	if len(result.Instances) != 1 {
		t.Fatalf("Instances count = %d, want 1", len(result.Instances))
	}

	inst := result.Instances[0]
	if inst.Name != "traefik" {
		t.Errorf("Name = %q, want %q", inst.Name, "traefik")
	}

	fd := inst.FileDiscovery
	if len(fd.FilePaths) != 2 {
		t.Errorf("FilePaths count = %d, want 2", len(fd.FilePaths))
	}
	if fd.FilePattern != "*.yaml" {
		t.Errorf("FilePattern = %q, want %q", fd.FilePattern, "*.yaml")
	}
	if fd.PollInterval.String() != "2m0s" {
		t.Errorf("PollInterval = %s, want 2m0s", fd.PollInterval)
	}
	if fd.WatchMethod != "inotify" {
		t.Errorf("WatchMethod = %q, want %q", fd.WatchMethod, "inotify")
	}
}
