package unifi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfig_Validate(t *testing.T) {
	valid := func() Config {
		return Config{
			URL:    "https://192.168.1.1",
			APIKey: "test-key",
			Site:   "default",
			TTL:    300,
		}
	}

	tests := []struct {
		name    string
		mutate  func(c *Config)
		wantErr bool
	}{
		{name: "valid config", mutate: func(*Config) {}, wantErr: false},
		{name: "http URL is valid", mutate: func(c *Config) { c.URL = "http://unifi:8080" }, wantErr: false},
		{name: "zero TTL is valid", mutate: func(c *Config) { c.TTL = 0 }, wantErr: false},
		{name: "zone is optional", mutate: func(c *Config) { c.Zone = "" }, wantErr: false},
		{name: "missing URL", mutate: func(c *Config) { c.URL = "" }, wantErr: true},
		{name: "invalid URL scheme", mutate: func(c *Config) { c.URL = "ftp://unifi" }, wantErr: true},
		{name: "URL with embedded credentials", mutate: func(c *Config) { c.URL = "https://admin:pass@unifi" }, wantErr: true},
		{name: "missing API key", mutate: func(c *Config) { c.APIKey = "" }, wantErr: true},
		{name: "empty site", mutate: func(c *Config) { c.Site = "" }, wantErr: true},
		{name: "negative TTL", mutate: func(c *Config) { c.TTL = -1 }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := valid()
			tt.mutate(&config)
			err := config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadConfig_Success(t *testing.T) {
	t.Setenv("DNSWEAVER_UNIFI_DNS_URL", "https://192.168.1.1")
	t.Setenv("DNSWEAVER_UNIFI_DNS_API_KEY", "my-secret-key")
	t.Setenv("DNSWEAVER_UNIFI_DNS_SITE", "branch")
	t.Setenv("DNSWEAVER_UNIFI_DNS_ZONE", "home.example.com")
	t.Setenv("DNSWEAVER_UNIFI_DNS_TTL", "600")

	config, err := LoadConfig("unifi-dns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if config.URL != "https://192.168.1.1" {
		t.Errorf("URL = %q, want https://192.168.1.1", config.URL)
	}
	if config.APIKey != "my-secret-key" {
		t.Errorf("APIKey = %q, want my-secret-key", config.APIKey)
	}
	if config.Site != "branch" {
		t.Errorf("Site = %q, want branch", config.Site)
	}
	if config.Zone != "home.example.com" {
		t.Errorf("Zone = %q, want home.example.com", config.Zone)
	}
	if config.TTL != 600 {
		t.Errorf("TTL = %d, want 600", config.TTL)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("DNSWEAVER_UNIFI_URL", "https://192.168.1.1")
	t.Setenv("DNSWEAVER_UNIFI_API_KEY", "key")

	config, err := LoadConfig("unifi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if config.Site != DefaultSite {
		t.Errorf("Site = %q, want default %q", config.Site, DefaultSite)
	}
	if config.TTL != DefaultTTL {
		t.Errorf("TTL = %d, want default %d", config.TTL, DefaultTTL)
	}
	if config.Zone != "" {
		t.Errorf("Zone = %q, want empty", config.Zone)
	}
}

func TestLoadConfig_FileSecret(t *testing.T) {
	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "api_key")
	if err := os.WriteFile(secretFile, []byte("file-based-key\n"), 0600); err != nil {
		t.Fatalf("failed to write secret file: %v", err)
	}

	t.Setenv("DNSWEAVER_FILE_TEST_URL", "https://192.168.1.1")
	t.Setenv("DNSWEAVER_FILE_TEST_API_KEY_FILE", secretFile)

	config, err := LoadConfig("file-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if config.APIKey != "file-based-key" {
		t.Errorf("APIKey = %q, want 'file-based-key' (trimmed)", config.APIKey)
	}
}

func TestLoadConfig_MissingRequired(t *testing.T) {
	t.Setenv("DNSWEAVER_INCOMPLETE_URL", "https://192.168.1.1")

	if _, err := LoadConfig("incomplete"); err == nil {
		t.Error("expected error for missing API_KEY, got nil")
	}
}

func TestLoadConfig_InvalidTTL(t *testing.T) {
	t.Setenv("DNSWEAVER_BADTTL_URL", "https://192.168.1.1")
	t.Setenv("DNSWEAVER_BADTTL_API_KEY", "key")
	t.Setenv("DNSWEAVER_BADTTL_TTL", "not-a-number")

	if _, err := LoadConfig("badttl"); err == nil {
		t.Error("expected error for invalid TTL, got nil")
	}
}

func TestLoadConfigFromMap(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]string
		want    Config
		wantErr bool
	}{
		{
			name: "uppercase keys",
			input: map[string]string{
				"URL":     "https://192.168.1.1",
				"API_KEY": "key",
				"SITE":    "branch",
				"ZONE":    "home.example.com",
				"TTL":     "120",
			},
			want: Config{URL: "https://192.168.1.1", APIKey: "key", Site: "branch", Zone: "home.example.com", TTL: 120},
		},
		{
			name: "lowercase keys",
			input: map[string]string{
				"url":     "https://192.168.1.1",
				"api_key": "key",
			},
			want: Config{URL: "https://192.168.1.1", APIKey: "key", Site: DefaultSite, TTL: DefaultTTL},
		},
		{
			name:    "missing API key",
			input:   map[string]string{"URL": "https://192.168.1.1"},
			wantErr: true,
		},
		{
			name:    "invalid TTL",
			input:   map[string]string{"URL": "https://192.168.1.1", "API_KEY": "key", "TTL": "soon"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := LoadConfigFromMap("test", tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadConfigFromMap() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if *config != tt.want {
				t.Errorf("config = %+v, want %+v", *config, tt.want)
			}
		})
	}
}

func TestEnvPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"unifi", "DNSWEAVER_UNIFI_"},
		{"unifi-dns", "DNSWEAVER_UNIFI_DNS_"},
		{"home-network-dns", "DNSWEAVER_HOME_NETWORK_DNS_"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := envPrefix(tt.input); got != tt.expected {
				t.Errorf("envPrefix(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
