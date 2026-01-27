package rfc2136

import (
	"os"
	"testing"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config without TSIG",
			config: Config{
				Server: "ns1.example.com:53",
				Zone:   "example.com.",
				TTL:    300,
			},
			wantErr: false,
		},
		{
			name: "valid config with TSIG",
			config: Config{
				Server:        "ns1.example.com",
				Zone:          "example.com.",
				TSIGKeyName:   "dnsweaver.",
				TSIGSecret:    "c2VjcmV0",
				TSIGAlgorithm: "hmac-sha256",
				TTL:           300,
			},
			wantErr: false,
		},
		{
			name: "missing server",
			config: Config{
				Zone: "example.com.",
				TTL:  300,
			},
			wantErr: true,
			errMsg:  "SERVER is required",
		},
		{
			name: "missing zone",
			config: Config{
				Server: "ns1.example.com:53",
				TTL:    300,
			},
			wantErr: true,
			errMsg:  "ZONE is required",
		},
		{
			name: "zone without trailing dot",
			config: Config{
				Server: "ns1.example.com:53",
				Zone:   "example.com",
				TTL:    300,
			},
			wantErr: true,
			errMsg:  "ZONE must end with a dot",
		},
		{
			name: "TSIG key name without secret",
			config: Config{
				Server:      "ns1.example.com:53",
				Zone:        "example.com.",
				TSIGKeyName: "dnsweaver.",
				TTL:         300,
			},
			wantErr: true,
			errMsg:  "TSIG_SECRET is required",
		},
		{
			name: "TSIG secret without key name",
			config: Config{
				Server:     "ns1.example.com:53",
				Zone:       "example.com.",
				TSIGSecret: "c2VjcmV0",
				TTL:        300,
			},
			wantErr: true,
			errMsg:  "TSIG_KEY_NAME is required",
		},
		{
			name: "TSIG key name without trailing dot",
			config: Config{
				Server:      "ns1.example.com:53",
				Zone:        "example.com.",
				TSIGKeyName: "dnsweaver",
				TSIGSecret:  "c2VjcmV0",
				TTL:         300,
			},
			wantErr: true,
			errMsg:  "TSIG_KEY_NAME must end with a dot",
		},
		{
			name: "negative TTL",
			config: Config{
				Server: "ns1.example.com:53",
				Zone:   "example.com.",
				TTL:    -1,
			},
			wantErr: true,
			errMsg:  "TTL must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !contains(err.Error(), tt.errMsg) {
					t.Errorf("Config.Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestConfig_ToDNSUpdateConfig(t *testing.T) {
	config := &Config{
		Server:        "ns1.example.com:53",
		Zone:          "example.com.",
		TSIGKeyName:   "dnsweaver.",
		TSIGSecret:    "c2VjcmV0",
		TSIGAlgorithm: "hmac-sha256",
		Timeout:       15,
		UseTCP:        true,
		TTL:           600,
	}

	dnsConfig := config.ToDNSUpdateConfig()

	if dnsConfig.Server != config.Server {
		t.Errorf("Server = %v, want %v", dnsConfig.Server, config.Server)
	}
	if dnsConfig.Zone != config.Zone {
		t.Errorf("Zone = %v, want %v", dnsConfig.Zone, config.Zone)
	}
	if dnsConfig.TSIGKeyName != config.TSIGKeyName {
		t.Errorf("TSIGKeyName = %v, want %v", dnsConfig.TSIGKeyName, config.TSIGKeyName)
	}
	if dnsConfig.TSIGSecret != config.TSIGSecret {
		t.Errorf("TSIGSecret = %v, want %v", dnsConfig.TSIGSecret, config.TSIGSecret)
	}
	if dnsConfig.UseTCP != config.UseTCP {
		t.Errorf("UseTCP = %v, want %v", dnsConfig.UseTCP, config.UseTCP)
	}
}

func TestLoadConfigFromMap(t *testing.T) {
	tests := []struct {
		name      string
		configMap map[string]string
		wantErr   bool
		check     func(*Config) error
	}{
		{
			name: "basic config",
			configMap: map[string]string{
				"SERVER": "ns1.example.com:53",
				"ZONE":   "example.com.",
			},
			wantErr: false,
			check: func(c *Config) error {
				if c.Server != "ns1.example.com:53" {
					return errorMsg("Server mismatch")
				}
				if c.Zone != "example.com." {
					return errorMsg("Zone mismatch")
				}
				if c.TTL != DefaultTTL {
					return errorMsg("TTL should be default")
				}
				return nil
			},
		},
		{
			name: "full config with TSIG",
			configMap: map[string]string{
				"SERVER":         "ns1.example.com",
				"ZONE":           "example.com.",
				"TSIG_KEY_NAME":  "dnsweaver.",
				"TSIG_SECRET":    "c2VjcmV0",
				"TSIG_ALGORITHM": "hmac-sha512",
				"TIMEOUT":        "30",
				"USE_TCP":        "true",
				"TTL":            "600",
			},
			wantErr: false,
			check: func(c *Config) error {
				if c.TSIGKeyName != "dnsweaver." {
					return errorMsg("TSIGKeyName mismatch")
				}
				if c.TSIGAlgorithm != "hmac-sha512" {
					return errorMsg("TSIGAlgorithm mismatch")
				}
				if c.Timeout != 30 {
					return errorMsg("Timeout mismatch")
				}
				if !c.UseTCP {
					return errorMsg("UseTCP should be true")
				}
				if c.TTL != 600 {
					return errorMsg("TTL mismatch")
				}
				return nil
			},
		},
		{
			name: "missing server",
			configMap: map[string]string{
				"ZONE": "example.com.",
			},
			wantErr: true,
		},
		{
			name: "invalid timeout",
			configMap: map[string]string{
				"SERVER":  "ns1.example.com:53",
				"ZONE":    "example.com.",
				"TIMEOUT": "not-a-number",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := LoadConfigFromMap("test-instance", tt.configMap)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadConfigFromMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				if err := tt.check(config); err != nil {
					t.Errorf("LoadConfigFromMap() check failed: %v", err)
				}
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	// Set up environment variables
	os.Setenv("DNSWEAVER_TEST_RFC2136_SERVER", "ns1.example.com:53")
	os.Setenv("DNSWEAVER_TEST_RFC2136_ZONE", "example.com.")
	os.Setenv("DNSWEAVER_TEST_RFC2136_TTL", "600")
	defer func() {
		os.Unsetenv("DNSWEAVER_TEST_RFC2136_SERVER")
		os.Unsetenv("DNSWEAVER_TEST_RFC2136_ZONE")
		os.Unsetenv("DNSWEAVER_TEST_RFC2136_TTL")
	}()

	config, err := LoadConfig("test-rfc2136")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.Server != "ns1.example.com:53" {
		t.Errorf("Server = %v, want %v", config.Server, "ns1.example.com:53")
	}
	if config.Zone != "example.com." {
		t.Errorf("Zone = %v, want %v", config.Zone, "example.com.")
	}
	if config.TTL != 600 {
		t.Errorf("TTL = %v, want %v", config.TTL, 600)
	}
}

func TestEnvPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"internal-dns", "DNSWEAVER_INTERNAL_DNS_"},
		{"bind", "DNSWEAVER_BIND_"},
		{"my-dns-server", "DNSWEAVER_MY_DNS_SERVER_"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := envPrefix(tt.input)
			if result != tt.expected {
				t.Errorf("envPrefix(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

type simpleError string

func (e simpleError) Error() string {
	return string(e)
}

func errorMsg(msg string) error {
	return simpleError(msg)
}
