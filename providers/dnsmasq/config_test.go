package dnsmasq

import (
	"os"
	"testing"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid local config",
			config: Config{
				ConfigDir:     "/etc/dnsmasq.d",
				ConfigFile:    "dnsweaver.conf",
				ReloadCommand: "systemctl reload dnsmasq",
				TTL:           300,
			},
			wantErr: false,
		},
		{
			name: "missing config dir",
			config: Config{
				ConfigFile:    "dnsweaver.conf",
				ReloadCommand: "systemctl reload dnsmasq",
			},
			wantErr: true,
		},
		{
			name: "missing config file",
			config: Config{
				ConfigDir:     "/etc/dnsmasq.d",
				ReloadCommand: "systemctl reload dnsmasq",
			},
			wantErr: true,
		},
		{
			name: "missing reload command",
			config: Config{
				ConfigDir:  "/etc/dnsmasq.d",
				ConfigFile: "dnsweaver.conf",
			},
			wantErr: true,
		},
		{
			name: "negative TTL",
			config: Config{
				ConfigDir:     "/etc/dnsmasq.d",
				ConfigFile:    "dnsweaver.conf",
				ReloadCommand: "systemctl reload dnsmasq",
				TTL:           -1,
			},
			wantErr: true,
		},
		{
			name: "valid SSH config",
			config: Config{
				ConfigDir:     "/etc/dnsmasq.d",
				ConfigFile:    "dnsweaver.conf",
				ReloadCommand: "systemctl reload dnsmasq",
				SSHHost:       "pihole.local",
				SSHPort:       22,
				SSHUser:       "pi",
				SSHKeyFile:    "/home/user/.ssh/id_rsa",
			},
			wantErr: false,
		},
		{
			name: "SSH missing host",
			config: Config{
				ConfigDir:     "/etc/dnsmasq.d",
				ConfigFile:    "dnsweaver.conf",
				ReloadCommand: "systemctl reload dnsmasq",
				SSHUser:       "pi",
				SSHKeyFile:    "/home/user/.ssh/id_rsa",
			},
			wantErr: true,
		},
		{
			name: "SSH missing user",
			config: Config{
				ConfigDir:     "/etc/dnsmasq.d",
				ConfigFile:    "dnsweaver.conf",
				ReloadCommand: "systemctl reload dnsmasq",
				SSHHost:       "pihole.local",
				SSHKeyFile:    "/home/user/.ssh/id_rsa",
			},
			wantErr: true,
		},
		{
			name: "SSH missing credentials",
			config: Config{
				ConfigDir:     "/etc/dnsmasq.d",
				ConfigFile:    "dnsweaver.conf",
				ReloadCommand: "systemctl reload dnsmasq",
				SSHHost:       "pihole.local",
				SSHUser:       "pi",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_IsSSHEnabled(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   bool
	}{
		{
			name:   "local config",
			config: Config{},
			want:   false,
		},
		{
			name: "SSH host set",
			config: Config{
				SSHHost: "pihole.local",
			},
			want: true,
		},
		{
			name: "SSH user set",
			config: Config{
				SSHUser: "pi",
			},
			want: true,
		},
		{
			name: "SSH key set",
			config: Config{
				SSHKeyFile: "/path/to/key",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.IsSSHEnabled(); got != tt.want {
				t.Errorf("Config.IsSSHEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_ConfigFilePath(t *testing.T) {
	config := Config{
		ConfigDir:  "/etc/dnsmasq.d",
		ConfigFile: "dnsweaver.conf",
	}

	got := config.ConfigFilePath()
	want := "/etc/dnsmasq.d/dnsweaver.conf"

	if got != want {
		t.Errorf("Config.ConfigFilePath() = %v, want %v", got, want)
	}
}

func TestLoadConfigFromMap(t *testing.T) {
	tests := []struct {
		name       string
		configMap  map[string]string
		wantErr    bool
		wantConfig *Config
	}{
		{
			name: "minimal config uses defaults",
			configMap: map[string]string{
				// No overrides, use defaults
			},
			wantErr: false,
			wantConfig: &Config{
				ConfigDir:     DefaultConfigDir,
				ConfigFile:    DefaultConfigFile,
				ReloadCommand: DefaultReloadCommand,
				TTL:           DefaultTTL,
			},
		},
		{
			name: "full config",
			configMap: map[string]string{
				"CONFIG_DIR":     "/custom/dnsmasq.d",
				"CONFIG_FILE":    "custom.conf",
				"RELOAD_COMMAND": "killall -HUP dnsmasq",
				"ZONE":           "example.com",
				"TTL":            "600",
			},
			wantErr: false,
			wantConfig: &Config{
				ConfigDir:     "/custom/dnsmasq.d",
				ConfigFile:    "custom.conf",
				ReloadCommand: "killall -HUP dnsmasq",
				Zone:          "example.com",
				TTL:           600,
			},
		},
		{
			name: "invalid TTL",
			configMap: map[string]string{
				"TTL": "not-a-number",
			},
			wantErr: true,
		},
		{
			name: "SSH config",
			configMap: map[string]string{
				"SSH_HOST":     "192.168.1.100",
				"SSH_PORT":     "2222",
				"SSH_USER":     "admin",
				"SSH_KEY_FILE": "/path/to/key",
			},
			wantErr: false,
			wantConfig: &Config{
				ConfigDir:     DefaultConfigDir,
				ConfigFile:    DefaultConfigFile,
				ReloadCommand: DefaultReloadCommand,
				TTL:           DefaultTTL,
				SSHHost:       "192.168.1.100",
				SSHPort:       2222,
				SSHUser:       "admin",
				SSHKeyFile:    "/path/to/key",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadConfigFromMap("test", tt.configMap)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadConfigFromMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			// Compare relevant fields
			if got.ConfigDir != tt.wantConfig.ConfigDir {
				t.Errorf("ConfigDir = %v, want %v", got.ConfigDir, tt.wantConfig.ConfigDir)
			}
			if got.ConfigFile != tt.wantConfig.ConfigFile {
				t.Errorf("ConfigFile = %v, want %v", got.ConfigFile, tt.wantConfig.ConfigFile)
			}
			if got.ReloadCommand != tt.wantConfig.ReloadCommand {
				t.Errorf("ReloadCommand = %v, want %v", got.ReloadCommand, tt.wantConfig.ReloadCommand)
			}
			if got.TTL != tt.wantConfig.TTL {
				t.Errorf("TTL = %v, want %v", got.TTL, tt.wantConfig.TTL)
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
			name:         "hyphenated name",
			instanceName: "pihole-dns",
			want:         "DNSWEAVER_PIHOLE_DNS_",
		},
		{
			name:         "mixed case",
			instanceName: "PiHole-DNS",
			want:         "DNSWEAVER_PIHOLE_DNS_",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := envPrefix(tt.instanceName); got != tt.want {
				t.Errorf("envPrefix() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	// Set up environment variables for test
	os.Setenv("DNSWEAVER_TEST_DNS_CONFIG_DIR", "/custom/path")
	os.Setenv("DNSWEAVER_TEST_DNS_CONFIG_FILE", "test.conf")
	os.Setenv("DNSWEAVER_TEST_DNS_RELOAD_COMMAND", "echo reload")
	os.Setenv("DNSWEAVER_TEST_DNS_ZONE", "test.local")
	os.Setenv("DNSWEAVER_TEST_DNS_TTL", "120")
	defer func() {
		os.Unsetenv("DNSWEAVER_TEST_DNS_CONFIG_DIR")
		os.Unsetenv("DNSWEAVER_TEST_DNS_CONFIG_FILE")
		os.Unsetenv("DNSWEAVER_TEST_DNS_RELOAD_COMMAND")
		os.Unsetenv("DNSWEAVER_TEST_DNS_ZONE")
		os.Unsetenv("DNSWEAVER_TEST_DNS_TTL")
	}()

	config, err := LoadConfig("test-dns")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.ConfigDir != "/custom/path" {
		t.Errorf("ConfigDir = %v, want /custom/path", config.ConfigDir)
	}
	if config.ConfigFile != "test.conf" {
		t.Errorf("ConfigFile = %v, want test.conf", config.ConfigFile)
	}
	if config.ReloadCommand != "echo reload" {
		t.Errorf("ReloadCommand = %v, want echo reload", config.ReloadCommand)
	}
	if config.Zone != "test.local" {
		t.Errorf("Zone = %v, want test.local", config.Zone)
	}
	if config.TTL != 120 {
		t.Errorf("TTL = %v, want 120", config.TTL)
	}
}
