package config

import (
	"os"
	"testing"
	"time"
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
