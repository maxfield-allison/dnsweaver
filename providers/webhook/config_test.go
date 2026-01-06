package webhook

import (
	"os"
	"testing"
	"time"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid minimal config",
			config: Config{
				URL: "http://webhook.example.com",
			},
			wantErr: false,
		},
		{
			name: "valid with https",
			config: Config{
				URL: "https://webhook.example.com",
			},
			wantErr: false,
		},
		{
			name: "valid with auth",
			config: Config{
				URL:        "http://webhook.example.com",
				AuthHeader: "X-API-Key",
				AuthToken:  "secret123",
			},
			wantErr: false,
		},
		{
			name: "valid with all options",
			config: Config{
				URL:        "http://webhook.example.com",
				Timeout:    60 * time.Second,
				AuthHeader: "Authorization",
				AuthToken:  "Bearer token",
				Retries:    5,
				RetryDelay: 2 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "missing URL",
			config: Config{
				URL: "",
			},
			wantErr: true,
			errMsg:  "URL is required",
		},
		{
			name: "invalid URL scheme",
			config: Config{
				URL: "ftp://webhook.example.com",
			},
			wantErr: true,
			errMsg:  "must start with http://",
		},
		{
			name: "auth header without token",
			config: Config{
				URL:        "http://webhook.example.com",
				AuthHeader: "X-API-Key",
				AuthToken:  "",
			},
			wantErr: true,
			errMsg:  "AUTH_TOKEN is required",
		},
		{
			name: "negative timeout",
			config: Config{
				URL:     "http://webhook.example.com",
				Timeout: -1 * time.Second,
			},
			wantErr: true,
			errMsg:  "TIMEOUT must be non-negative",
		},
		{
			name: "negative retries",
			config: Config{
				URL:     "http://webhook.example.com",
				Retries: -1,
			},
			wantErr: true,
			errMsg:  "RETRIES must be non-negative",
		},
		{
			name: "negative retry delay",
			config: Config{
				URL:        "http://webhook.example.com",
				RetryDelay: -1 * time.Second,
			},
			wantErr: true,
			errMsg:  "RETRY_DELAY must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error, got nil")
					return
				}
				if tt.errMsg != "" && !containsString(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	// Helper to set environment variables for a test
	setEnv := func(vars map[string]string) func() {
		for k, v := range vars {
			os.Setenv(k, v)
		}
		return func() {
			for k := range vars {
				os.Unsetenv(k)
			}
		}
	}

	t.Run("loads minimal config", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"DNSWEAVER_CUSTOM_DNS_URL": "http://webhook.example.com",
		})
		defer cleanup()

		config, err := LoadConfig("custom-dns")
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}

		if config.URL != "http://webhook.example.com" {
			t.Errorf("URL = %q, want %q", config.URL, "http://webhook.example.com")
		}
		if config.Timeout != DefaultTimeout {
			t.Errorf("Timeout = %v, want %v", config.Timeout, DefaultTimeout)
		}
		if config.Retries != DefaultRetries {
			t.Errorf("Retries = %d, want %d", config.Retries, DefaultRetries)
		}
	})

	t.Run("loads full config", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"DNSWEAVER_WEBHOOK_URL":         "https://api.example.com",
			"DNSWEAVER_WEBHOOK_TIMEOUT":     "60s",
			"DNSWEAVER_WEBHOOK_AUTH_HEADER": "X-API-Key",
			"DNSWEAVER_WEBHOOK_AUTH_TOKEN":  "secret",
			"DNSWEAVER_WEBHOOK_RETRIES":     "5",
			"DNSWEAVER_WEBHOOK_RETRY_DELAY": "2s",
		})
		defer cleanup()

		config, err := LoadConfig("webhook")
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}

		if config.URL != "https://api.example.com" {
			t.Errorf("URL = %q, want %q", config.URL, "https://api.example.com")
		}
		if config.Timeout != 60*time.Second {
			t.Errorf("Timeout = %v, want %v", config.Timeout, 60*time.Second)
		}
		if config.AuthHeader != "X-API-Key" {
			t.Errorf("AuthHeader = %q, want %q", config.AuthHeader, "X-API-Key")
		}
		if config.AuthToken != "secret" {
			t.Errorf("AuthToken = %q, want %q", config.AuthToken, "secret")
		}
		if config.Retries != 5 {
			t.Errorf("Retries = %d, want %d", config.Retries, 5)
		}
		if config.RetryDelay != 2*time.Second {
			t.Errorf("RetryDelay = %v, want %v", config.RetryDelay, 2*time.Second)
		}
	})

	t.Run("loads token from file", func(t *testing.T) {
		// Create temp file with token
		tmpfile, err := os.CreateTemp("", "token")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmpfile.Name())

		if _, err := tmpfile.WriteString("secret-from-file\n"); err != nil {
			t.Fatal(err)
		}
		tmpfile.Close()

		cleanup := setEnv(map[string]string{
			"DNSWEAVER_TEST_URL":             "http://webhook.example.com",
			"DNSWEAVER_TEST_AUTH_HEADER":     "X-Token",
			"DNSWEAVER_TEST_AUTH_TOKEN_FILE": tmpfile.Name(),
		})
		defer cleanup()

		config, err := LoadConfig("test")
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}

		if config.AuthToken != "secret-from-file" {
			t.Errorf("AuthToken = %q, want %q", config.AuthToken, "secret-from-file")
		}
	})

	t.Run("file takes precedence over direct value", func(t *testing.T) {
		// Create temp file with token
		tmpfile, err := os.CreateTemp("", "token")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmpfile.Name())

		if _, err := tmpfile.WriteString("from-file"); err != nil {
			t.Fatal(err)
		}
		tmpfile.Close()

		cleanup := setEnv(map[string]string{
			"DNSWEAVER_TEST_URL":             "http://webhook.example.com",
			"DNSWEAVER_TEST_AUTH_HEADER":     "X-Token",
			"DNSWEAVER_TEST_AUTH_TOKEN":      "direct-value",
			"DNSWEAVER_TEST_AUTH_TOKEN_FILE": tmpfile.Name(),
		})
		defer cleanup()

		config, err := LoadConfig("test")
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}

		// File should take precedence
		if config.AuthToken != "from-file" {
			t.Errorf("AuthToken = %q, want %q (file should take precedence)", config.AuthToken, "from-file")
		}
	})

	t.Run("normalizes instance name", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"DNSWEAVER_MY_CUSTOM_DNS_URL": "http://webhook.example.com",
		})
		defer cleanup()

		config, err := LoadConfig("my-custom-dns")
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}

		if config.URL != "http://webhook.example.com" {
			t.Errorf("URL = %q, want %q", config.URL, "http://webhook.example.com")
		}
	})

	t.Run("invalid timeout duration", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"DNSWEAVER_TEST_URL":     "http://webhook.example.com",
			"DNSWEAVER_TEST_TIMEOUT": "invalid",
		})
		defer cleanup()

		_, err := LoadConfig("test")
		if err == nil {
			t.Error("LoadConfig() expected error for invalid TIMEOUT")
		}
	})

	t.Run("invalid retries value", func(t *testing.T) {
		cleanup := setEnv(map[string]string{
			"DNSWEAVER_TEST_URL":     "http://webhook.example.com",
			"DNSWEAVER_TEST_RETRIES": "not-a-number",
		})
		defer cleanup()

		_, err := LoadConfig("test")
		if err == nil {
			t.Error("LoadConfig() expected error for invalid RETRIES")
		}
	})

	t.Run("missing URL returns error", func(t *testing.T) {
		// Ensure no relevant env vars are set
		os.Unsetenv("DNSWEAVER_EMPTY_URL")

		_, err := LoadConfig("empty")
		if err == nil {
			t.Error("LoadConfig() expected error for missing URL")
		}
	})
}

func TestEnvPrefix(t *testing.T) {
	tests := []struct {
		name         string
		instanceName string
		want         string
	}{
		{"simple name", "webhook", "DNSWEAVER_WEBHOOK_"},
		{"with hyphen", "custom-dns", "DNSWEAVER_CUSTOM_DNS_"},
		{"multiple hyphens", "my-custom-dns", "DNSWEAVER_MY_CUSTOM_DNS_"},
		{"already uppercase", "WEBHOOK", "DNSWEAVER_WEBHOOK_"},
		{"mixed case", "Custom-DNS", "DNSWEAVER_CUSTOM_DNS_"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := envPrefix(tt.instanceName); got != tt.want {
				t.Errorf("envPrefix(%q) = %q, want %q", tt.instanceName, got, tt.want)
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStringHelper(s, substr))
}

func containsStringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
