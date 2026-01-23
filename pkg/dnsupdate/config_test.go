package dnsupdate

import (
	"os"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config without TSIG",
			config: Config{
				Server: "ns1.example.com",
				Zone:   "example.com.",
			},
			wantErr: false,
		},
		{
			name: "valid config with TSIG",
			config: Config{
				Server:        "ns1.example.com:53",
				Zone:          "example.com.",
				TSIGKeyName:   "dnsweaver.",
				TSIGSecret:    "c2VjcmV0", // base64 of "secret"
				TSIGAlgorithm: "hmac-sha256",
			},
			wantErr: false,
		},
		{
			name: "missing server",
			config: Config{
				Zone: "example.com.",
			},
			wantErr: true,
			errMsg:  "server is required",
		},
		{
			name: "missing zone",
			config: Config{
				Server: "ns1.example.com",
			},
			wantErr: true,
			errMsg:  "zone is required",
		},
		{
			name: "zone without trailing dot",
			config: Config{
				Server: "ns1.example.com",
				Zone:   "example.com",
			},
			wantErr: true,
			errMsg:  "zone must end with a dot",
		},
		{
			name: "TSIG key name without secret",
			config: Config{
				Server:      "ns1.example.com",
				Zone:        "example.com.",
				TSIGKeyName: "dnsweaver.",
			},
			wantErr: true,
			errMsg:  "tsig_secret is required",
		},
		{
			name: "TSIG secret without key name",
			config: Config{
				Server:     "ns1.example.com",
				Zone:       "example.com.",
				TSIGSecret: "c2VjcmV0",
			},
			wantErr: true,
			errMsg:  "tsig_key_name is required",
		},
		{
			name: "TSIG key name without trailing dot",
			config: Config{
				Server:      "ns1.example.com",
				Zone:        "example.com.",
				TSIGKeyName: "dnsweaver",
				TSIGSecret:  "c2VjcmV0",
			},
			wantErr: true,
			errMsg:  "tsig_key_name must end with a dot",
		},
		{
			name: "invalid TSIG algorithm",
			config: Config{
				Server:        "ns1.example.com",
				Zone:          "example.com.",
				TSIGKeyName:   "dnsweaver.",
				TSIGSecret:    "c2VjcmV0",
				TSIGAlgorithm: "invalid-algo",
			},
			wantErr: true,
			errMsg:  "unsupported tsig_algorithm",
		},
		{
			name: "negative timeout",
			config: Config{
				Server:  "ns1.example.com",
				Zone:    "example.com.",
				Timeout: -1,
			},
			wantErr: true,
			errMsg:  "timeout must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestConfigGetServer(t *testing.T) {
	tests := []struct {
		name   string
		server string
		want   string
	}{
		{
			name:   "server without port",
			server: "ns1.example.com",
			want:   "ns1.example.com:53",
		},
		{
			name:   "server with port",
			server: "ns1.example.com:5353",
			want:   "ns1.example.com:5353",
		},
		{
			name:   "empty server",
			server: "",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{Server: tt.server}
			if got := config.GetServer(); got != tt.want {
				t.Errorf("GetServer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigGetTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{
			name:    "custom timeout",
			timeout: 30 * time.Second,
			want:    30 * time.Second,
		},
		{
			name:    "zero timeout returns default",
			timeout: 0,
			want:    DefaultTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{Timeout: tt.timeout}
			if got := config.GetTimeout(); got != tt.want {
				t.Errorf("GetTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigGetTSIGAlgorithm(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		want      string
	}{
		{
			name:      "empty returns default",
			algorithm: "",
			want:      DefaultTSIGAlgorithm,
		},
		{
			name:      "hmac-sha256",
			algorithm: "hmac-sha256",
			want:      dns.HmacSHA256,
		},
		{
			name:      "SHA256 shorthand",
			algorithm: "sha256",
			want:      dns.HmacSHA256,
		},
		{
			name:      "hmac-sha512",
			algorithm: "hmac-sha512",
			want:      dns.HmacSHA512,
		},
		{
			name:      "hmac-md5",
			algorithm: "hmac-md5",
			want:      dns.HmacMD5,
		},
		{
			name:      "case insensitive",
			algorithm: "HMAC-SHA256",
			want:      dns.HmacSHA256,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{TSIGAlgorithm: tt.algorithm}
			if got := config.GetTSIGAlgorithm(); got != tt.want {
				t.Errorf("GetTSIGAlgorithm() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	// Set up environment variables for the test
	prefix := "DNSUPDATE_TEST_"
	t.Setenv(prefix+"SERVER", "ns1.example.com")
	t.Setenv(prefix+"ZONE", "example.com.")
	t.Setenv(prefix+"TSIG_KEY_NAME", "dnsweaver.")
	t.Setenv(prefix+"TSIG_SECRET", "c2VjcmV0")
	t.Setenv(prefix+"TSIG_ALGORITHM", "hmac-sha256")
	t.Setenv(prefix+"TIMEOUT", "20")
	t.Setenv(prefix+"USE_TCP", "true")

	config, err := LoadConfig(prefix)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.Server != "ns1.example.com" {
		t.Errorf("Server = %v, want ns1.example.com", config.Server)
	}
	if config.Zone != "example.com." {
		t.Errorf("Zone = %v, want example.com.", config.Zone)
	}
	if config.TSIGKeyName != "dnsweaver." {
		t.Errorf("TSIGKeyName = %v, want dnsweaver.", config.TSIGKeyName)
	}
	if config.TSIGSecret != "c2VjcmV0" {
		t.Errorf("TSIGSecret = %v, want c2VjcmV0", config.TSIGSecret)
	}
	if config.Timeout != 20*time.Second {
		t.Errorf("Timeout = %v, want 20s", config.Timeout)
	}
	if !config.UseTCP {
		t.Error("UseTCP = false, want true")
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	// Create a temporary file for the secret
	tmpFile, err := os.CreateTemp("", "tsig-secret-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString("  supersecret123  \n"); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	prefix := "DNSUPDATE_FILE_TEST_"
	t.Setenv(prefix+"SERVER", "ns1.example.com")
	t.Setenv(prefix+"ZONE", "example.com.")
	t.Setenv(prefix+"TSIG_KEY_NAME", "dnsweaver.")
	t.Setenv(prefix+"TSIG_SECRET_FILE", tmpFile.Name())

	config, err := LoadConfig(prefix)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Secret should be trimmed
	if config.TSIGSecret != "supersecret123" {
		t.Errorf("TSIGSecret = %q, want %q", config.TSIGSecret, "supersecret123")
	}
}

func TestLoadConfigFromMap(t *testing.T) {
	configMap := map[string]string{
		"SERVER":         "ns1.example.com",
		"ZONE":           "example.com.",
		"TSIG_KEY_NAME":  "dnsweaver.",
		"TSIG_SECRET":    "c2VjcmV0",
		"TSIG_ALGORITHM": "hmac-sha512",
		"TIMEOUT":        "15",
		"USE_TCP":        "true",
	}

	config, err := LoadConfigFromMap(configMap)
	if err != nil {
		t.Fatalf("LoadConfigFromMap failed: %v", err)
	}

	if config.Server != "ns1.example.com" {
		t.Errorf("Server = %v, want ns1.example.com", config.Server)
	}
	if config.TSIGAlgorithm != "hmac-sha512" {
		t.Errorf("TSIGAlgorithm = %v, want hmac-sha512", config.TSIGAlgorithm)
	}
	if config.Timeout != 15*time.Second {
		t.Errorf("Timeout = %v, want 15s", config.Timeout)
	}
	if !config.UseTCP {
		t.Error("UseTCP = false, want true")
	}
}

func TestConfigHasTSIG(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   bool
	}{
		{
			name: "with TSIG",
			config: Config{
				TSIGKeyName: "dnsweaver.",
				TSIGSecret:  "secret",
			},
			want: true,
		},
		{
			name: "without TSIG",
			config: Config{
				Server: "ns1.example.com",
				Zone:   "example.com.",
			},
			want: false,
		},
		{
			name: "key name only",
			config: Config{
				TSIGKeyName: "dnsweaver.",
			},
			want: false,
		},
		{
			name: "secret only",
			config: Config{
				TSIGSecret: "secret",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.HasTSIG(); got != tt.want {
				t.Errorf("HasTSIG() = %v, want %v", got, tt.want)
			}
		})
	}
}

// contains checks if substr is in s
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
