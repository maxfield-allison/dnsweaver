package cloudflare

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfig_Validate_Success(t *testing.T) {
	config := &Config{
		Token:  "test-token",
		ZoneID: "zone-123",
		TTL:    300,
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestConfig_Validate_WithZoneName(t *testing.T) {
	config := &Config{
		Token: "test-token",
		Zone:  "example.com",
		TTL:   300,
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestConfig_Validate_MissingToken(t *testing.T) {
	config := &Config{
		ZoneID: "zone-123",
		TTL:    300,
	}

	err := config.Validate()
	if err == nil {
		t.Error("expected validation error for missing token, got nil")
	}
}

func TestConfig_Validate_MissingZone(t *testing.T) {
	config := &Config{
		Token: "test-token",
		TTL:   300,
	}

	err := config.Validate()
	if err == nil {
		t.Error("expected validation error for missing zone, got nil")
	}
}

func TestConfig_Validate_InvalidTTL(t *testing.T) {
	tests := []struct {
		name    string
		ttl     int
		wantErr bool
	}{
		{"valid 300", 300, false},
		{"valid 60", 60, false},
		{"valid automatic", 1, false},
		{"valid 86400", 86400, false},
		{"invalid 30", 30, true},  // Less than minimum
		{"invalid 59", 59, true},  // Less than minimum
		{"negative", -1, true},
		{"zero is ok", 0, false}, // Zero TTL is allowed (default will be used)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				Token:  "test-token",
				ZoneID: "zone-123",
				TTL:    tt.ttl,
			}

			err := config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("TTL=%d: expected error=%v, got error=%v", tt.ttl, tt.wantErr, err)
			}
		})
	}
}

func TestLoadConfig_Success(t *testing.T) {
	// Set test environment variables
	t.Setenv("DNSWEAVER_PUBLIC_DNS_TOKEN", "test-token")
	t.Setenv("DNSWEAVER_PUBLIC_DNS_ZONE_ID", "zone-123")
	t.Setenv("DNSWEAVER_PUBLIC_DNS_TTL", "600")
	t.Setenv("DNSWEAVER_PUBLIC_DNS_PROXIED", "true")

	config, err := LoadConfig("public-dns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if config.Token != "test-token" {
		t.Errorf("expected token test-token, got %s", config.Token)
	}
	if config.ZoneID != "zone-123" {
		t.Errorf("expected zone ID zone-123, got %s", config.ZoneID)
	}
	if config.TTL != 600 {
		t.Errorf("expected TTL 600, got %d", config.TTL)
	}
	if !config.Proxied {
		t.Error("expected proxied true, got false")
	}
}

func TestLoadConfig_WithZoneName(t *testing.T) {
	t.Setenv("DNSWEAVER_CLOUDFLARE_TOKEN", "test-token")
	t.Setenv("DNSWEAVER_CLOUDFLARE_ZONE", "example.com")

	config, err := LoadConfig("cloudflare")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if config.Zone != "example.com" {
		t.Errorf("expected zone example.com, got %s", config.Zone)
	}
	if config.ZoneID != "" {
		t.Errorf("expected empty zone ID, got %s", config.ZoneID)
	}
}

func TestLoadConfig_DefaultValues(t *testing.T) {
	t.Setenv("DNSWEAVER_CF_TOKEN", "test-token")
	t.Setenv("DNSWEAVER_CF_ZONE_ID", "zone-123")

	config, err := LoadConfig("cf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if config.TTL != DefaultTTL {
		t.Errorf("expected default TTL %d, got %d", DefaultTTL, config.TTL)
	}
	if config.Proxied {
		t.Error("expected proxied false by default, got true")
	}
}

func TestLoadConfig_TokenFromFile(t *testing.T) {
	// Create a temporary file with token
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("file-token\n"), 0600); err != nil {
		t.Fatalf("failed to write token file: %v", err)
	}

	t.Setenv("DNSWEAVER_SECRETS_TOKEN_FILE", tokenFile)
	t.Setenv("DNSWEAVER_SECRETS_ZONE_ID", "zone-123")

	config, err := LoadConfig("secrets")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if config.Token != "file-token" {
		t.Errorf("expected token from file 'file-token', got %s", config.Token)
	}
}

func TestLoadConfig_FilePrecedenceOverDirect(t *testing.T) {
	// Create a temporary file with token
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(tokenFile, []byte("file-token"), 0600); err != nil {
		t.Fatalf("failed to write token file: %v", err)
	}

	// Both direct and file are set - file should take precedence
	t.Setenv("DNSWEAVER_PREC_TOKEN", "direct-token")
	t.Setenv("DNSWEAVER_PREC_TOKEN_FILE", tokenFile)
	t.Setenv("DNSWEAVER_PREC_ZONE_ID", "zone-123")

	config, err := LoadConfig("prec")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if config.Token != "file-token" {
		t.Errorf("expected file token to take precedence, got %s", config.Token)
	}
}

func TestLoadConfig_InvalidTTL(t *testing.T) {
	t.Setenv("DNSWEAVER_BADTTL_TOKEN", "test-token")
	t.Setenv("DNSWEAVER_BADTTL_ZONE_ID", "zone-123")
	t.Setenv("DNSWEAVER_BADTTL_TTL", "not-a-number")

	_, err := LoadConfig("badttl")
	if err == nil {
		t.Error("expected error for invalid TTL, got nil")
	}
}

func TestLoadConfig_ProxiedVariations(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    bool
	}{
		{"true", "true", true},
		{"TRUE", "TRUE", true},
		{"1", "1", true},
		{"yes", "yes", true},
		{"on", "on", true},
		{"false", "false", false},
		{"FALSE", "FALSE", false},
		{"0", "0", false},
		{"no", "no", false},
		{"off", "off", false},
		{"empty", "", false},
		{"invalid", "maybe", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DNSWEAVER_PROXY_TOKEN", "test-token")
			t.Setenv("DNSWEAVER_PROXY_ZONE_ID", "zone-123")
			if tt.value != "" {
				t.Setenv("DNSWEAVER_PROXY_PROXIED", tt.value)
			} else {
				os.Unsetenv("DNSWEAVER_PROXY_PROXIED")
			}

			config, err := LoadConfig("proxy")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if config.Proxied != tt.want {
				t.Errorf("PROXIED=%q: expected %v, got %v", tt.value, tt.want, config.Proxied)
			}
		})
	}
}

func TestEnvPrefix(t *testing.T) {
	tests := []struct {
		name     string
		instance string
		want     string
	}{
		{"simple", "cloudflare", "DNSWEAVER_CLOUDFLARE_"},
		{"with hyphen", "public-dns", "DNSWEAVER_PUBLIC_DNS_"},
		{"multiple hyphens", "my-public-dns", "DNSWEAVER_MY_PUBLIC_DNS_"},
		{"uppercase", "CLOUDFLARE", "DNSWEAVER_CLOUDFLARE_"},
		{"mixed case", "Public-DNS", "DNSWEAVER_PUBLIC_DNS_"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := envPrefix(tt.instance)
			if got != tt.want {
				t.Errorf("envPrefix(%q) = %q, want %q", tt.instance, got, tt.want)
			}
		})
	}
}

func TestParseBool(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"1", true},
		{"yes", true},
		{"YES", true},
		{"on", true},
		{"ON", true},
		{"false", false},
		{"FALSE", false},
		{"0", false},
		{"no", false},
		{"off", false},
		{"", false},
		{"invalid", false},
		{"maybe", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseBool(tt.input)
			if got != tt.want {
				t.Errorf("parseBool(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
