package config

import (
	"os"
	"testing"
	"time"

	"github.com/maxfield-allison/dnsweaver/pkg/source"
)

func TestParseSources(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     []string
	}{
		{
			name:     "empty defaults to traefik",
			envValue: "",
			want:     []string{"traefik"},
		},
		{
			name:     "single source",
			envValue: "caddy",
			want:     []string{"caddy"},
		},
		{
			name:     "multiple sources",
			envValue: "traefik,caddy,nginx",
			want:     []string{"traefik", "caddy", "nginx"},
		},
		{
			name:     "sources with whitespace",
			envValue: " traefik , caddy , nginx ",
			want:     []string{"traefik", "caddy", "nginx"},
		},
		{
			name:     "mixed case normalized to lowercase",
			envValue: "Traefik,CADDY,NginX",
			want:     []string{"traefik", "caddy", "nginx"},
		},
		{
			name:     "empty parts filtered",
			envValue: "traefik,,caddy,",
			want:     []string{"traefik", "caddy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()
			if tt.envValue != "" {
				os.Setenv("DNSWEAVER_SOURCES", tt.envValue)
			}

			got := parseSources()

			if len(got) != len(tt.want) {
				t.Fatalf("parseSources() = %v, want %v", got, tt.want)
			}
			for i, g := range got {
				if g != tt.want[i] {
					t.Errorf("parseSources()[%d] = %q, want %q", i, g, tt.want[i])
				}
			}
		})
	}
}

func TestParseSources_DeprecatedSingularFallback(t *testing.T) {
	os.Clearenv()
	// Only set the deprecated singular form
	os.Setenv("DNSWEAVER_SOURCE", "dnsweaver")

	got := parseSources()

	if len(got) != 1 || got[0] != "dnsweaver" {
		t.Errorf("parseSources() with deprecated DNSWEAVER_SOURCE = %v, want [dnsweaver]", got)
	}
}

func TestParseSources_PluralTakesPrecedence(t *testing.T) {
	os.Clearenv()
	// Set both — plural should win
	os.Setenv("DNSWEAVER_SOURCES", "traefik,caddy")
	os.Setenv("DNSWEAVER_SOURCE", "dnsweaver")

	got := parseSources()

	if len(got) != 2 || got[0] != "traefik" || got[1] != "caddy" {
		t.Errorf("parseSources() = %v, want [traefik caddy] (plural should take precedence)", got)
	}
}

func TestLoadSourceInstanceConfig(t *testing.T) {
	tests := []struct {
		name       string
		sourceName string
		envVars    map[string]string
		wantPaths  []string
		wantPoll   time.Duration
		wantMethod string
	}{
		{
			name:       "no config uses defaults",
			sourceName: "traefik",
			envVars:    map[string]string{},
			wantPaths:  nil,
			wantPoll:   60 * time.Second,
			wantMethod: "auto",
		},
		{
			name:       "file paths parsed",
			sourceName: "traefik",
			envVars: map[string]string{
				"DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS": "/rules,/config/traefik",
			},
			wantPaths:  []string{"/rules", "/config/traefik"},
			wantPoll:   60 * time.Second,
			wantMethod: "auto",
		},
		{
			name:       "file paths with whitespace trimmed",
			sourceName: "traefik",
			envVars: map[string]string{
				"DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS": " /rules , /config/traefik ",
			},
			wantPaths:  []string{"/rules", "/config/traefik"},
			wantPoll:   60 * time.Second,
			wantMethod: "auto",
		},
		{
			name:       "poll interval custom",
			sourceName: "traefik",
			envVars: map[string]string{
				"DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS":    "/rules",
				"DNSWEAVER_SOURCE_TRAEFIK_POLL_INTERVAL": "30s",
			},
			wantPaths:  []string{"/rules"},
			wantPoll:   30 * time.Second,
			wantMethod: "auto",
		},
		{
			name:       "poll interval 5s allowed",
			sourceName: "traefik",
			envVars: map[string]string{
				"DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS":    "/rules",
				"DNSWEAVER_SOURCE_TRAEFIK_POLL_INTERVAL": "5s",
			},
			wantPaths:  []string{"/rules"},
			wantPoll:   5 * time.Second,
			wantMethod: "auto",
		},
		{
			name:       "invalid poll interval uses default",
			sourceName: "traefik",
			envVars: map[string]string{
				"DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS":    "/rules",
				"DNSWEAVER_SOURCE_TRAEFIK_POLL_INTERVAL": "invalid",
			},
			wantPaths:  []string{"/rules"},
			wantPoll:   60 * time.Second, // default
			wantMethod: "auto",
		},
		{
			name:       "poll interval below 1s uses default",
			sourceName: "traefik",
			envVars: map[string]string{
				"DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS":    "/rules",
				"DNSWEAVER_SOURCE_TRAEFIK_POLL_INTERVAL": "500ms",
			},
			wantPaths:  []string{"/rules"},
			wantPoll:   60 * time.Second, // default
			wantMethod: "auto",
		},
		{
			name:       "watch method poll",
			sourceName: "traefik",
			envVars: map[string]string{
				"DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS":   "/rules",
				"DNSWEAVER_SOURCE_TRAEFIK_WATCH_METHOD": "poll",
			},
			wantPaths:  []string{"/rules"},
			wantPoll:   60 * time.Second,
			wantMethod: "poll",
		},
		{
			name:       "watch method inotify",
			sourceName: "traefik",
			envVars: map[string]string{
				"DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS":   "/rules",
				"DNSWEAVER_SOURCE_TRAEFIK_WATCH_METHOD": "inotify",
			},
			wantPaths:  []string{"/rules"},
			wantPoll:   60 * time.Second,
			wantMethod: "inotify",
		},
		{
			name:       "caddy source config",
			sourceName: "caddy",
			envVars: map[string]string{
				"DNSWEAVER_SOURCE_CADDY_FILE_PATHS": "/etc/caddy",
			},
			wantPaths:  []string{"/etc/caddy"},
			wantPoll:   60 * time.Second,
			wantMethod: "auto",
		},
		{
			name:       "file pattern custom",
			sourceName: "traefik",
			envVars: map[string]string{
				"DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS":   "/rules",
				"DNSWEAVER_SOURCE_TRAEFIK_FILE_PATTERN": "*.toml",
			},
			wantPaths:  []string{"/rules"},
			wantPoll:   60 * time.Second,
			wantMethod: "auto",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			got := loadSourceInstanceConfig(tt.sourceName)

			if got.Name != tt.sourceName {
				t.Errorf("Name = %q, want %q", got.Name, tt.sourceName)
			}

			// Check file paths
			if len(got.FileDiscovery.FilePaths) != len(tt.wantPaths) {
				t.Errorf("FilePaths = %v, want %v", got.FileDiscovery.FilePaths, tt.wantPaths)
			} else {
				for i, p := range got.FileDiscovery.FilePaths {
					if p != tt.wantPaths[i] {
						t.Errorf("FilePaths[%d] = %q, want %q", i, p, tt.wantPaths[i])
					}
				}
			}

			if got.FileDiscovery.PollInterval != tt.wantPoll {
				t.Errorf("PollInterval = %v, want %v", got.FileDiscovery.PollInterval, tt.wantPoll)
			}

			if got.FileDiscovery.WatchMethod != tt.wantMethod {
				t.Errorf("WatchMethod = %q, want %q", got.FileDiscovery.WatchMethod, tt.wantMethod)
			}
		})
	}
}

func TestSourceConfig_GetSourceInstance(t *testing.T) {
	os.Clearenv()
	os.Setenv("DNSWEAVER_SOURCES", "traefik,caddy")
	os.Setenv("DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS", "/rules")

	cfg := loadSourceConfig()

	t.Run("finds existing source", func(t *testing.T) {
		got := cfg.GetSourceInstance("traefik")
		if got == nil {
			t.Fatal("GetSourceInstance(traefik) = nil, want non-nil")
		}
		if got.Name != "traefik" {
			t.Errorf("Name = %q, want traefik", got.Name)
		}
	})

	t.Run("returns nil for unknown source", func(t *testing.T) {
		got := cfg.GetSourceInstance("unknown")
		if got != nil {
			t.Errorf("GetSourceInstance(unknown) = %v, want nil", got)
		}
	})
}

func TestSourceConfig_HasFileDiscovery(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    bool
	}{
		{
			name:    "no file paths configured",
			envVars: map[string]string{},
			want:    false,
		},
		{
			name: "file paths configured for traefik",
			envVars: map[string]string{
				"DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS": "/rules",
			},
			want: true,
		},
		{
			name: "file paths configured for one of multiple sources",
			envVars: map[string]string{
				"DNSWEAVER_SOURCES":                 "traefik,caddy",
				"DNSWEAVER_SOURCE_CADDY_FILE_PATHS": "/etc/caddy",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			cfg := loadSourceConfig()
			got := cfg.HasFileDiscovery()

			if got != tt.want {
				t.Errorf("HasFileDiscovery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSourceEnvPrefix(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"traefik", "DNSWEAVER_SOURCE_TRAEFIK_"},
		{"caddy", "DNSWEAVER_SOURCE_CADDY_"},
		{"nginx-proxy", "DNSWEAVER_SOURCE_NGINX-PROXY_"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sourceEnvPrefix(tt.name)
			if got != tt.want {
				t.Errorf("sourceEnvPrefix(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// TestLoadSourceConfig_DefaultEntryPoints covers #180:
// DNSWEAVER_SOURCE_TRAEFIK_DEFAULT_ENTRYPOINTS parses into
// SourceInstanceConfig.DefaultEntryPoints with whitespace tolerance.
func TestLoadSourceConfig_DefaultEntryPoints(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    []string
	}{
		{
			name: "single value",
			envVars: map[string]string{
				"DNSWEAVER_SOURCES":                            "traefik",
				"DNSWEAVER_SOURCE_TRAEFIK_DEFAULT_ENTRYPOINTS": "webA",
			},
			want: []string{"webA"},
		},
		{
			name: "multiple comma-separated",
			envVars: map[string]string{
				"DNSWEAVER_SOURCES":                            "traefik",
				"DNSWEAVER_SOURCE_TRAEFIK_DEFAULT_ENTRYPOINTS": "webA,webC",
			},
			want: []string{"webA", "webC"},
		},
		{
			name: "whitespace and empty entries tolerated",
			envVars: map[string]string{
				"DNSWEAVER_SOURCES":                            "traefik",
				"DNSWEAVER_SOURCE_TRAEFIK_DEFAULT_ENTRYPOINTS": "  webA , ,  webC ,",
			},
			want: []string{"webA", "webC"},
		},
		{
			name: "unset → nil",
			envVars: map[string]string{
				"DNSWEAVER_SOURCES": "traefik",
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			cfg := loadSourceConfig()
			inst := cfg.GetSourceInstance("traefik")
			if inst == nil {
				t.Fatal("traefik source instance not found")
			}
			got := inst.DefaultEntryPoints
			if len(got) != len(tt.want) {
				t.Fatalf("DefaultEntryPoints len = %d, want %d (got=%+v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("DefaultEntryPoints[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFileDiscoveryPollInterval(t *testing.T) {
	enabled := func(name string, d time.Duration) *SourceInstanceConfig {
		inst := &SourceInstanceConfig{Name: name, FileDiscovery: source.DefaultFileDiscoveryConfig()}
		inst.FileDiscovery.FilePaths = []string{"/rules"}
		inst.FileDiscovery.PollInterval = d
		return inst
	}
	disabled := &SourceInstanceConfig{Name: "caddy", FileDiscovery: source.DefaultFileDiscoveryConfig()}
	disabled.FileDiscovery.PollInterval = time.Second // ignored: no file paths

	tests := []struct {
		name      string
		instances []*SourceInstanceConfig
		want      time.Duration
	}{
		{name: "no file discovery", instances: []*SourceInstanceConfig{disabled}, want: 0},
		{name: "single source", instances: []*SourceInstanceConfig{enabled("traefik", 15*time.Second)}, want: 15 * time.Second},
		{name: "default interval", instances: []*SourceInstanceConfig{enabled("traefik", 60*time.Second)}, want: 60 * time.Second},
		{name: "shortest enabled wins", instances: []*SourceInstanceConfig{enabled("traefik", 45*time.Second), disabled, enabled("caddy", 20*time.Second)}, want: 20 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &SourceConfig{Instances: tt.instances}
			if got := cfg.FileDiscoveryPollInterval(); got != tt.want {
				t.Fatalf("FileDiscoveryPollInterval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileDiscoveryPollIntervalFromEnv(t *testing.T) {
	clearAllEnv(t)
	defer clearAllEnv(t)
	t.Setenv("DNSWEAVER_SOURCES", "traefik")
	t.Setenv("DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS", "/rules")
	t.Setenv("DNSWEAVER_SOURCE_TRAEFIK_POLL_INTERVAL", "15s")
	if got := (&Config{Sources: loadSourceConfig()}).FileDiscoveryPollInterval(); got != 15*time.Second {
		t.Fatalf("FileDiscoveryPollInterval() = %v, want 15s", got)
	}
}
