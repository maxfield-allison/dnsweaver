package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetEnv(t *testing.T) {
	const key = "TEST_DNSWEAVER_GETENV"
	const value = "test-value"

	os.Setenv(key, value)
	defer os.Unsetenv(key)

	got := getEnv(key)
	if got != value {
		t.Errorf("getEnv(%q) = %q, want %q", key, got, value)
	}
}

func TestGetEnvOrFile_DirectValue(t *testing.T) {
	const directKey = "TEST_DNSWEAVER_TOKEN"
	const fileKey = "TEST_DNSWEAVER_TOKEN_FILE"
	const value = "direct-token"

	os.Setenv(directKey, value)
	defer os.Unsetenv(directKey)
	os.Unsetenv(fileKey)

	got := getEnvOrFile(directKey, fileKey)
	if got != value {
		t.Errorf("getEnvOrFile() = %q, want %q", got, value)
	}
}

func TestGetEnvOrFile_FileValue(t *testing.T) {
	const directKey = "TEST_DNSWEAVER_TOKEN"
	const fileKey = "TEST_DNSWEAVER_TOKEN_FILE"
	const secretValue = "file-secret-value"

	// Create temp file with secret
	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(secretFile, []byte(secretValue+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	os.Unsetenv(directKey)
	os.Setenv(fileKey, secretFile)
	defer os.Unsetenv(fileKey)

	got := getEnvOrFile(directKey, fileKey)
	if got != secretValue {
		t.Errorf("getEnvOrFile() = %q, want %q (file content trimmed)", got, secretValue)
	}
}

func TestGetEnvOrFile_FileTakesPrecedence(t *testing.T) {
	const directKey = "TEST_DNSWEAVER_TOKEN"
	const fileKey = "TEST_DNSWEAVER_TOKEN_FILE"
	const directValue = "direct-value"
	const fileValue = "file-value"

	// Create temp file
	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "token")
	if err := os.WriteFile(secretFile, []byte(fileValue), 0600); err != nil {
		t.Fatal(err)
	}

	os.Setenv(directKey, directValue)
	os.Setenv(fileKey, secretFile)
	defer os.Unsetenv(directKey)
	defer os.Unsetenv(fileKey)

	got := getEnvOrFile(directKey, fileKey)
	if got != fileValue {
		t.Errorf("getEnvOrFile() = %q, want %q (file should take precedence)", got, fileValue)
	}
}

func TestGetEnvOrFile_NonexistentFile(t *testing.T) {
	const directKey = "TEST_DNSWEAVER_TOKEN"
	const fileKey = "TEST_DNSWEAVER_TOKEN_FILE"
	const directValue = "fallback-value"

	os.Setenv(directKey, directValue)
	os.Setenv(fileKey, "/nonexistent/path/to/secret")
	defer os.Unsetenv(directKey)
	defer os.Unsetenv(fileKey)

	got := getEnvOrFile(directKey, fileKey)
	if got != directValue {
		t.Errorf("getEnvOrFile() = %q, want %q (should fallback to direct value)", got, directValue)
	}
}

func TestParseBool(t *testing.T) {
	tests := []struct {
		input    string
		defVal   bool
		expected bool
	}{
		{"true", false, true},
		{"TRUE", false, true},
		{"True", false, true},
		{"1", false, true},
		{"yes", false, true},
		{"YES", false, true},
		{"on", false, true},
		{"ON", false, true},
		{"false", true, false},
		{"FALSE", true, false},
		{"0", true, false},
		{"no", true, false},
		{"off", true, false},
		{"", false, false},
		{"", true, true},
		{"invalid", false, false},
		{"invalid", true, true},
		{"  true  ", false, true},
	}

	for _, tc := range tests {
		got := parseBool(tc.input, tc.defVal)
		if got != tc.expected {
			t.Errorf("parseBool(%q, %v) = %v, want %v", tc.input, tc.defVal, got, tc.expected)
		}
	}
}

func TestNormalizeInstanceName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"internal-dns", "INTERNAL_DNS"},
		{"public-dns", "PUBLIC_DNS"},
		{"mydns", "MYDNS"},
		{"my-super-dns", "MY_SUPER_DNS"},
		{"already_underscore", "ALREADY_UNDERSCORE"},
		{"MixedCase", "MIXEDCASE"},
	}

	for _, tc := range tests {
		got := normalizeInstanceName(tc.input)
		if got != tc.expected {
			t.Errorf("normalizeInstanceName(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestEnvPrefix(t *testing.T) {
	tests := []struct {
		instanceName string
		expected     string
	}{
		{"internal-dns", "DNSWEAVER_INTERNAL_DNS_"},
		{"public-dns", "DNSWEAVER_PUBLIC_DNS_"},
		{"cloudflare", "DNSWEAVER_CLOUDFLARE_"},
	}

	for _, tc := range tests {
		got := envPrefix(tc.instanceName)
		if got != tc.expected {
			t.Errorf("envPrefix(%q) = %q, want %q", tc.instanceName, got, tc.expected)
		}
	}
}

func TestGetEnvWithFileFallback(t *testing.T) {
	const prefix = "DNSWEAVER_TEST_"
	const key = "SECRET"
	const value = "my-secret"

	// Create temp file
	tmpDir := t.TempDir()
	secretFile := filepath.Join(tmpDir, "secret")
	if err := os.WriteFile(secretFile, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}

	// Test with _FILE suffix
	os.Setenv(prefix+key+"_FILE", secretFile)
	defer os.Unsetenv(prefix + key + "_FILE")

	got := getEnvWithFileFallback(prefix, key)
	if got != value {
		t.Errorf("getEnvWithFileFallback() = %q, want %q", got, value)
	}
}
