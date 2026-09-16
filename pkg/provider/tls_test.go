package provider

import (
	"crypto/tls"
	"testing"
)

func TestExtractTLSConfig_Empty(t *testing.T) {
	if got, err := extractTLSConfig(nil, "i"); err != nil || got != nil {
		t.Errorf("nil map: got %+v, want nil", got)
	}
	if got, err := extractTLSConfig(map[string]string{}, "i"); err != nil || got != nil {
		t.Errorf("empty map: got %+v, want nil", got)
	}
	if got, err := extractTLSConfig(map[string]string{"URL": "x"}, "i"); err != nil || got != nil {
		t.Errorf("unrelated keys: got %+v, want nil", got)
	}
}

func TestExtractTLSConfig_Populated(t *testing.T) {
	cfg, err := extractTLSConfig(map[string]string{
		"TLS_CA_FILE":     "/etc/ssl/ca.pem",
		"TLS_CERT_FILE":   "/etc/ssl/c.crt",
		"TLS_KEY_FILE":    "/etc/ssl/c.key",
		"TLS_SERVER_NAME": "internal.example.com",
		"TLS_SKIP_VERIFY": "true",
		"TLS_MIN_VERSION": "1.3",
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil TLSConfig")
	}
	if cfg.CAFile != "/etc/ssl/ca.pem" {
		t.Errorf("CAFile = %q", cfg.CAFile)
	}
	if cfg.CertFile != "/etc/ssl/c.crt" || cfg.KeyFile != "/etc/ssl/c.key" {
		t.Errorf("client keypair not parsed: %+v", cfg)
	}
	if cfg.ServerName != "internal.example.com" {
		t.Errorf("ServerName = %q", cfg.ServerName)
	}
	if !cfg.InsecureSkip {
		t.Error("InsecureSkip should be true")
	}
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %x, want %x", cfg.MinVersion, tls.VersionTLS13)
	}
}

func TestExtractTLSConfig_InvalidMinVersionRejected(t *testing.T) {
	cfg, err := extractTLSConfig(map[string]string{
		"TLS_SKIP_VERIFY": "true",
		"TLS_MIN_VERSION": "1.1",
	}, "test")
	if err == nil || cfg != nil {
		t.Fatalf("extractTLSConfig() = (%+v, %v), want error", cfg, err)
	}
}

func TestParseBoolish(t *testing.T) {
	truthy := []string{"true", "TRUE", "True", "1", "yes", "YES", "on", "ON"}
	for _, v := range truthy {
		if !parseBoolish(v) {
			t.Errorf("parseBoolish(%q) = false, want true", v)
		}
	}
	falsy := []string{"", "false", "FALSE", "0", "no", "off", "random"}
	for _, v := range falsy {
		if parseBoolish(v) {
			t.Errorf("parseBoolish(%q) = true, want false", v)
		}
	}
}
