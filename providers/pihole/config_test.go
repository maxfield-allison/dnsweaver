package pihole

import (
	"os"
	"testing"
)

func TestLoadConfigFromMap(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]string
		wantErr bool
	}{
		{
			name: "valid API mode",
			config: map[string]string{
				"mode":     "api",
				"url":      "http://pihole.local",
				"password": "secret",
			},
			wantErr: false,
		},
		{
			name: "valid file mode",
			config: map[string]string{
				"mode":           "file",
				"config_dir":     "/etc/pihole",
				"config_file":    "custom.list",
				"reload_command": "pihole restartdns",
			},
			wantErr: false,
		},
		{
			name: "API mode with zone and TTL",
			config: map[string]string{
				"mode":     "api",
				"url":      "http://pihole.local",
				"password": "secret",
				"zone":     "example.com",
				"ttl":      "600",
			},
			wantErr: false,
		},
		{
			name: "missing mode uses default API",
			config: map[string]string{
				"url":      "http://pihole.local",
				"password": "secret",
			},
			wantErr: false,
		},
		{
			name: "invalid TTL",
			config: map[string]string{
				"mode":     "api",
				"url":      "http://pihole.local",
				"password": "secret",
				"ttl":      "invalid",
			},
			wantErr: true,
		},
		{
			name: "API mode missing URL",
			config: map[string]string{
				"mode":     "api",
				"password": "secret",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadConfigFromMap("test", tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadConfigFromMap() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && cfg == nil {
				t.Error("LoadConfigFromMap() returned nil config without error")
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	// Clean up environment after test
	defer func() {
		os.Unsetenv("DNSWEAVER_TEST_MODE")
		os.Unsetenv("DNSWEAVER_TEST_URL")
		os.Unsetenv("DNSWEAVER_TEST_PASSWORD")
		os.Unsetenv("DNSWEAVER_TEST_TTL")
		os.Unsetenv("DNSWEAVER_TEST_ZONE")
	}()

	tests := []struct {
		name     string
		envVars  map[string]string
		wantErr  bool
		wantMode Mode
		wantTTL  int
	}{
		{
			name: "API mode from env",
			envVars: map[string]string{
				"DNSWEAVER_TEST_MODE":     "api",
				"DNSWEAVER_TEST_URL":      "http://pihole.local",
				"DNSWEAVER_TEST_PASSWORD": "secret",
			},
			wantErr:  false,
			wantMode: ModeAPI,
			wantTTL:  DefaultTTL,
		},
		{
			name: "custom TTL",
			envVars: map[string]string{
				"DNSWEAVER_TEST_MODE":     "api",
				"DNSWEAVER_TEST_URL":      "http://pihole.local",
				"DNSWEAVER_TEST_PASSWORD": "secret",
				"DNSWEAVER_TEST_TTL":      "600",
			},
			wantErr:  false,
			wantMode: ModeAPI,
			wantTTL:  600,
		},
		{
			name: "file mode from env",
			envVars: map[string]string{
				"DNSWEAVER_TEST_MODE": "file",
				// Uses defaults for config_dir, config_file, reload_command
			},
			wantErr:  false,
			wantMode: ModeFile,
			wantTTL:  DefaultTTL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all test env vars first
			os.Unsetenv("DNSWEAVER_TEST_MODE")
			os.Unsetenv("DNSWEAVER_TEST_URL")
			os.Unsetenv("DNSWEAVER_TEST_PASSWORD")
			os.Unsetenv("DNSWEAVER_TEST_TTL")
			os.Unsetenv("DNSWEAVER_TEST_ZONE")

			// Set env vars for this test
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			cfg, err := LoadConfig("test")
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil {
				if cfg.Mode != tt.wantMode {
					t.Errorf("LoadConfig() Mode = %v, want %v", cfg.Mode, tt.wantMode)
				}
				if cfg.TTL != tt.wantTTL {
					t.Errorf("LoadConfig() TTL = %v, want %v", cfg.TTL, tt.wantTTL)
				}
			}
		})
	}
}

func TestEnvPrefix(t *testing.T) {
	tests := []struct {
		name         string
		instanceName string
		want         string
	}{
		{
			name:         "simple name",
			instanceName: "pihole",
			want:         "DNSWEAVER_PIHOLE_",
		},
		{
			name:         "name with hyphen",
			instanceName: "pihole-dns",
			want:         "DNSWEAVER_PIHOLE_DNS_",
		},
		{
			name:         "lowercase name",
			instanceName: "my-pihole",
			want:         "DNSWEAVER_MY_PIHOLE_",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := envPrefix(tt.instanceName)
			if got != tt.want {
				t.Errorf("envPrefix() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_ConfigFilePath(t *testing.T) {
	config := &Config{
		ConfigDir:  "/etc/pihole",
		ConfigFile: "custom.list",
	}

	got := config.ConfigFilePath()
	want := "/etc/pihole/custom.list"

	if got != want {
		t.Errorf("ConfigFilePath() = %v, want %v", got, want)
	}
}
