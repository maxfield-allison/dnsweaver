package technitium

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				URL:   "http://localhost:5380",
				Token: "test-token",
				Zone:  "example.com",
				TTL:   300,
			},
			wantErr: false,
		},
		{
			name: "missing URL",
			config: Config{
				Token: "test-token",
				Zone:  "example.com",
				TTL:   300,
			},
			wantErr: true,
		},
		{
			name: "missing token",
			config: Config{
				URL:  "http://localhost:5380",
				Zone: "example.com",
				TTL:  300,
			},
			wantErr: true,
		},
		{
			name: "missing zone",
			config: Config{
				URL:   "http://localhost:5380",
				Token: "test-token",
				TTL:   300,
			},
			wantErr: true,
		},
		{
			name: "negative TTL",
			config: Config{
				URL:   "http://localhost:5380",
				Token: "test-token",
				Zone:  "example.com",
				TTL:   -1,
			},
			wantErr: true,
		},
		{
			name: "zero TTL is valid",
			config: Config{
				URL:   "http://localhost:5380",
				Token: "test-token",
				Zone:  "example.com",
				TTL:   0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadConfig_Success(t *testing.T) {
	// Set up environment variables
	os.Setenv("DNSWEAVER_TEST_DNS_URL", "http://localhost:5380")
	os.Setenv("DNSWEAVER_TEST_DNS_TOKEN", "my-secret-token")
	os.Setenv("DNSWEAVER_TEST_DNS_ZONE", "example.com")
	os.Setenv("DNSWEAVER_TEST_DNS_TTL", "600")
	defer func() {
		os.Unsetenv("DNSWEAVER_TEST_DNS_URL")
		os.Unsetenv("DNSWEAVER_TEST_DNS_TOKEN")
		os.Unsetenv("DNSWEAVER_TEST_DNS_ZONE")
		os.Unsetenv("DNSWEAVER_TEST_DNS_TTL")
	}()

	config, err := LoadConfig("test-dns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if config.URL != "http://localhost:5380" {
		t.Errorf("expected URL http://localhost:5380, got %s", config.URL)
	}
	if config.Token != "my-secret-token" {
		t.Errorf("expected Token my-secret-token, got %s", config.Token)
	}
	if config.Zone != "example.com" {
		t.Errorf("expected Zone example.com, got %s", config.Zone)
	}
	if config.TTL != 600 {
		t.Errorf("expected TTL 600, got %d", config.TTL)
	}
}

func TestLoadConfig_DefaultTTL(t *testing.T) {
	os.Setenv("DNSWEAVER_INTERNAL_DNS_URL", "http://localhost:5380")
	os.Setenv("DNSWEAVER_INTERNAL_DNS_TOKEN", "token")
	os.Setenv("DNSWEAVER_INTERNAL_DNS_ZONE", "example.com")
	defer func() {
		os.Unsetenv("DNSWEAVER_INTERNAL_DNS_URL")
		os.Unsetenv("DNSWEAVER_INTERNAL_DNS_TOKEN")
		os.Unsetenv("DNSWEAVER_INTERNAL_DNS_ZONE")
	}()

	config, err := LoadConfig("internal-dns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if config.TTL != DefaultTTL {
		t.Errorf("expected default TTL %d, got %d", DefaultTTL, config.TTL)
	}
}

func TestLoadConfig_FileSecret(t *testing.T) {
	// Create a temp file with the secret
	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(secretFile, []byte("file-based-secret\n"), 0600); err != nil {
		t.Fatalf("failed to write secret file: %v", err)
	}

	os.Setenv("DNSWEAVER_FILE_TEST_URL", "http://localhost:5380")
	os.Setenv("DNSWEAVER_FILE_TEST_TOKEN_FILE", secretFile)
	os.Setenv("DNSWEAVER_FILE_TEST_ZONE", "example.com")
	defer func() {
		os.Unsetenv("DNSWEAVER_FILE_TEST_URL")
		os.Unsetenv("DNSWEAVER_FILE_TEST_TOKEN_FILE")
		os.Unsetenv("DNSWEAVER_FILE_TEST_ZONE")
	}()

	config, err := LoadConfig("file-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Token should be trimmed
	if config.Token != "file-based-secret" {
		t.Errorf("expected Token 'file-based-secret', got '%s'", config.Token)
	}
}

func TestLoadConfig_MissingRequired(t *testing.T) {
	// Only set URL, missing token and zone
	os.Setenv("DNSWEAVER_INCOMPLETE_URL", "http://localhost:5380")
	defer os.Unsetenv("DNSWEAVER_INCOMPLETE_URL")

	_, err := LoadConfig("incomplete")
	if err == nil {
		t.Error("expected error for missing required fields, got nil")
	}
}

func TestLoadConfig_InvalidTTL(t *testing.T) {
	os.Setenv("DNSWEAVER_BADTTL_URL", "http://localhost:5380")
	os.Setenv("DNSWEAVER_BADTTL_TOKEN", "token")
	os.Setenv("DNSWEAVER_BADTTL_ZONE", "example.com")
	os.Setenv("DNSWEAVER_BADTTL_TTL", "not-a-number")
	defer func() {
		os.Unsetenv("DNSWEAVER_BADTTL_URL")
		os.Unsetenv("DNSWEAVER_BADTTL_TOKEN")
		os.Unsetenv("DNSWEAVER_BADTTL_ZONE")
		os.Unsetenv("DNSWEAVER_BADTTL_TTL")
	}()

	_, err := LoadConfig("badttl")
	if err == nil {
		t.Error("expected error for invalid TTL, got nil")
	}
}

func TestEnvPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"internal-dns", "DNSWEAVER_INTERNAL_DNS_"},
		{"public-dns", "DNSWEAVER_PUBLIC_DNS_"},
		{"technitium", "DNSWEAVER_TECHNITIUM_"},
		{"my-awesome-dns-provider", "DNSWEAVER_MY_AWESOME_DNS_PROVIDER_"},
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

func TestLoadConfig_InsecureSkipVerify(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected bool
	}{
		{"true lowercase", "true", true},
		{"TRUE uppercase", "TRUE", true},
		{"True mixed", "True", true},
		{"1", "1", true},
		{"false", "false", false},
		{"0", "0", false},
		{"empty", "", false},
		{"random string", "whatever", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up minimal valid config
			os.Setenv("DNSWEAVER_SKIP_TEST_URL", "http://localhost:5380")
			os.Setenv("DNSWEAVER_SKIP_TEST_TOKEN", "token")
			os.Setenv("DNSWEAVER_SKIP_TEST_ZONE", "example.com")
			if tt.envValue != "" {
				os.Setenv("DNSWEAVER_SKIP_TEST_INSECURE_SKIP_VERIFY", tt.envValue)
			}
			defer func() {
				os.Unsetenv("DNSWEAVER_SKIP_TEST_URL")
				os.Unsetenv("DNSWEAVER_SKIP_TEST_TOKEN")
				os.Unsetenv("DNSWEAVER_SKIP_TEST_ZONE")
				os.Unsetenv("DNSWEAVER_SKIP_TEST_INSECURE_SKIP_VERIFY")
			}()

			config, err := LoadConfig("skip-test")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if config.InsecureSkipVerify != tt.expected {
				t.Errorf("InsecureSkipVerify = %v, want %v", config.InsecureSkipVerify, tt.expected)
			}
		})
	}
}

func TestLoadConfigFromMap_InsecureSkipVerify(t *testing.T) {
	tests := []struct {
		name     string
		mapValue string
		expected bool
	}{
		{"true lowercase", "true", true},
		{"TRUE uppercase", "TRUE", true},
		{"1", "1", true},
		{"false", "false", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configMap := map[string]string{
				"URL":                  "http://localhost:5380",
				"TOKEN":                "token",
				"ZONE":                 "example.com",
				"INSECURE_SKIP_VERIFY": tt.mapValue,
			}

			config, err := LoadConfigFromMap("test", configMap)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if config.InsecureSkipVerify != tt.expected {
				t.Errorf("InsecureSkipVerify = %v, want %v", config.InsecureSkipVerify, tt.expected)
			}
		})
	}
}
