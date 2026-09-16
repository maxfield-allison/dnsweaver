package provider

import (
	"crypto/tls"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
)

// TestRegistry_PropagatesTLSConfig is a regression test for the TLS hardening
// work tracked in #183 / GitHub #89. It pins the contract that every TLS_*
// key written into ProviderConfig by the loader is materialized into
// FactoryConfig.HTTP.TLS before the factory runs, so individual providers do
// NOT need to know anything about TLS configuration themselves.
func TestRegistry_PropagatesTLSConfig(t *testing.T) {
	r := NewRegistry(testLogger())

	var captured FactoryConfig
	r.RegisterFactory("capture", func(cfg FactoryConfig) (Provider, error) {
		captured = cfg
		return &mockProvider{name: cfg.Name, typeName: "capture"}, nil
	})

	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "tls-instance",
		TypeName:   "capture",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
		ProviderConfig: map[string]string{
			"URL":             "https://dns.example.com",
			"TOKEN":           "abc",
			"ZONE":            "example.com",
			"TLS_SERVER_NAME": "dns.internal",
			"TLS_SKIP_VERIFY": "true",
			"TLS_MIN_VERSION": "1.3",
		},
	})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	if captured.HTTP.TLS == nil {
		t.Fatal("HTTP.TLS not propagated to factory")
	}
	got := captured.HTTP.TLS
	want := &httputil.TLSConfig{
		ServerName:   "dns.internal",
		InsecureSkip: true,
		MinVersion:   tls.VersionTLS13,
	}
	if *got != *want {
		t.Errorf("TLS config mismatch\n got: %+v\nwant: %+v", *got, *want)
	}
}

func TestRegistry_RejectsTLSConfigurationBeforeFactory(t *testing.T) {
	r := NewRegistry(testLogger())
	factoryCalled := false
	r.RegisterFactory("capture", func(cfg FactoryConfig) (Provider, error) {
		factoryCalled = true
		return &mockProvider{name: cfg.Name, typeName: "capture"}, nil
	})

	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "bad-tls",
		TypeName:   "capture",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
		ProviderConfig: map[string]string{
			"TLS_CA_FILE": filepath.Join(t.TempDir(), "missing-ca.pem"),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid TLS configuration") {
		t.Fatalf("CreateInstance() error = %v, want TLS configuration error", err)
	}
	if factoryCalled {
		t.Fatal("provider factory ran with invalid explicit TLS configuration")
	}
}

// TestRegistry_NoTLSKeys_PropagatesNil ensures we don't synthesize an empty
// TLSConfig when the operator hasn't asked for one — the factory should see
// HTTP.TLS == nil so the default transport applies.
func TestRegistry_NoTLSKeys_PropagatesNil(t *testing.T) {
	r := NewRegistry(testLogger())

	var captured FactoryConfig
	r.RegisterFactory("capture", func(cfg FactoryConfig) (Provider, error) {
		captured = cfg
		return &mockProvider{name: cfg.Name, typeName: "capture"}, nil
	})

	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "default-tls",
		TypeName:   "capture",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
		ProviderConfig: map[string]string{
			"URL":   "https://dns.example.com",
			"TOKEN": "abc",
			"ZONE":  "example.com",
		},
	})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if captured.HTTP.TLS != nil {
		t.Errorf("expected HTTP.TLS to be nil, got %+v", *captured.HTTP.TLS)
	}
}
