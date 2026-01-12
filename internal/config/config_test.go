package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// clearAllEnv removes all DNSWEAVER_ environment variables for clean test state.
func clearAllEnv(t *testing.T) {
	t.Helper()
	for _, env := range os.Environ() {
		if len(env) > 10 && env[:10] == "DNSWEAVER_" {
			key := env[:findEquals(env)]
			os.Unsetenv(key)
		}
	}
}

func findEquals(s string) int {
	for i, c := range s {
		if c == '=' {
			return i
		}
	}
	return len(s)
}

func TestLoad_MinimalConfig(t *testing.T) {
	clearAllEnv(t)
	defer clearAllEnv(t)

	// Minimal required config
	os.Setenv("DNSWEAVER_INSTANCES", "internal-dns")
	os.Setenv("DNSWEAVER_INTERNAL_DNS_TYPE", "technitium")
	os.Setenv("DNSWEAVER_INTERNAL_DNS_TARGET", "10.1.20.210")
	os.Setenv("DNSWEAVER_INTERNAL_DNS_DOMAINS", "*.example.com")

	cfg, err := Load()

	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	// Check global defaults
	if cfg.LogLevel() != DefaultLogLevel {
		t.Errorf("LogLevel() = %q, want %q", cfg.LogLevel(), DefaultLogLevel)
	}
	if cfg.LogFormat() != DefaultLogFormat {
		t.Errorf("LogFormat() = %q, want %q", cfg.LogFormat(), DefaultLogFormat)
	}
	if cfg.DryRun() != DefaultDryRun {
		t.Errorf("DryRun() = %v, want %v", cfg.DryRun(), DefaultDryRun)
	}
	if cfg.ReconcileInterval() != DefaultReconcileInterval {
		t.Errorf("ReconcileInterval() = %v, want %v", cfg.ReconcileInterval(), DefaultReconcileInterval)
	}
	if cfg.HealthPort() != DefaultHealthPort {
		t.Errorf("HealthPort() = %d, want %d", cfg.HealthPort(), DefaultHealthPort)
	}
	if cfg.DockerHost() != DefaultDockerHost {
		t.Errorf("DockerHost() = %q, want %q", cfg.DockerHost(), DefaultDockerHost)
	}
	if cfg.DockerMode() != DefaultDockerMode {
		t.Errorf("DockerMode() = %q, want %q", cfg.DockerMode(), DefaultDockerMode)
	}
	if cfg.Source() != DefaultSource {
		t.Errorf("Source() = %q, want %q", cfg.Source(), DefaultSource)
	}

	// Check providers
	if len(cfg.ProviderNames) != 1 {
		t.Fatalf("ProviderNames length = %d, want 1", len(cfg.ProviderNames))
	}
	if cfg.ProviderNames[0] != "internal-dns" {
		t.Errorf("ProviderNames[0] = %q, want %q", cfg.ProviderNames[0], "internal-dns")
	}

	// Check provider instance
	inst, ok := cfg.GetProviderInstance("internal-dns")
	if !ok {
		t.Fatal("GetProviderInstance(internal-dns) returned false")
	}
	if inst.TypeName != "technitium" {
		t.Errorf("inst.TypeName = %q, want %q", inst.TypeName, "technitium")
	}
	if inst.Target != "10.1.20.210" {
		t.Errorf("inst.Target = %q, want %q", inst.Target, "10.1.20.210")
	}
}

func TestLoad_CompleteConfig(t *testing.T) {
	clearAllEnv(t)
	defer clearAllEnv(t)

	// Create temp file for secrets
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "internal-token")
	if err := os.WriteFile(tokenFile, []byte("secret-internal-token"), 0600); err != nil {
		t.Fatal(err)
	}

	// Global settings
	os.Setenv("DNSWEAVER_LOG_LEVEL", "debug")
	os.Setenv("DNSWEAVER_LOG_FORMAT", "text")
	os.Setenv("DNSWEAVER_DRY_RUN", "true")
	os.Setenv("DNSWEAVER_DEFAULT_TTL", "600")
	os.Setenv("DNSWEAVER_RECONCILE_INTERVAL", "2m")
	os.Setenv("DNSWEAVER_HEALTH_PORT", "9090")
	os.Setenv("DNSWEAVER_DOCKER_HOST", "tcp://localhost:2375")
	os.Setenv("DNSWEAVER_DOCKER_MODE", "swarm")
	os.Setenv("DNSWEAVER_SOURCE", "labels")

	// Instances
	os.Setenv("DNSWEAVER_INSTANCES", "internal-dns,public-dns")

	// Internal DNS (Technitium with secrets file)
	os.Setenv("DNSWEAVER_INTERNAL_DNS_TYPE", "technitium")
	os.Setenv("DNSWEAVER_INTERNAL_DNS_RECORD_TYPE", "A")
	os.Setenv("DNSWEAVER_INTERNAL_DNS_TARGET", "10.1.20.210")
	os.Setenv("DNSWEAVER_INTERNAL_DNS_TTL", "300")
	os.Setenv("DNSWEAVER_INTERNAL_DNS_DOMAINS", "*.internal.example.com")
	os.Setenv("DNSWEAVER_INTERNAL_DNS_EXCLUDE_DOMAINS", "admin.internal.example.com")
	os.Setenv("DNSWEAVER_INTERNAL_DNS_URL", "http://dns.internal:5380")
	os.Setenv("DNSWEAVER_INTERNAL_DNS_TOKEN_FILE", tokenFile)
	os.Setenv("DNSWEAVER_INTERNAL_DNS_ZONE", "internal.example.com")

	// Public DNS (Cloudflare with CNAME)
	os.Setenv("DNSWEAVER_PUBLIC_DNS_TYPE", "cloudflare")
	os.Setenv("DNSWEAVER_PUBLIC_DNS_RECORD_TYPE", "CNAME")
	os.Setenv("DNSWEAVER_PUBLIC_DNS_TARGET", "example.com")
	os.Setenv("DNSWEAVER_PUBLIC_DNS_DOMAINS", "*.example.com")
	os.Setenv("DNSWEAVER_PUBLIC_DNS_EXCLUDE_DOMAINS", "*.internal.example.com")
	os.Setenv("DNSWEAVER_PUBLIC_DNS_TOKEN", "cf-token-direct")
	os.Setenv("DNSWEAVER_PUBLIC_DNS_ZONE_ID", "zone123")

	cfg, err := Load()

	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	// Check global settings
	if cfg.LogLevel() != "debug" {
		t.Errorf("LogLevel() = %q, want %q", cfg.LogLevel(), "debug")
	}
	if cfg.DryRun() != true {
		t.Error("DryRun() = false, want true")
	}
	if cfg.ReconcileInterval() != 2*time.Minute {
		t.Errorf("ReconcileInterval() = %v, want %v", cfg.ReconcileInterval(), 2*time.Minute)
	}
	if cfg.HealthPort() != 9090 {
		t.Errorf("HealthPort() = %d, want %d", cfg.HealthPort(), 9090)
	}

	// Check provider order preserved
	if len(cfg.ProviderNames) != 2 {
		t.Fatalf("ProviderNames length = %d, want 2", len(cfg.ProviderNames))
	}
	if cfg.ProviderNames[0] != "internal-dns" {
		t.Errorf("ProviderNames[0] = %q, want %q", cfg.ProviderNames[0], "internal-dns")
	}
	if cfg.ProviderNames[1] != "public-dns" {
		t.Errorf("ProviderNames[1] = %q, want %q", cfg.ProviderNames[1], "public-dns")
	}

	// Check internal DNS config
	internal, ok := cfg.GetProviderInstance("internal-dns")
	if !ok {
		t.Fatal("GetProviderInstance(internal-dns) returned false")
	}
	if internal.RecordType != provider.RecordTypeA {
		t.Errorf("internal.RecordType = %q, want %q", internal.RecordType, provider.RecordTypeA)
	}
	if internal.TTL != 300 {
		t.Errorf("internal.TTL = %d, want %d", internal.TTL, 300)
	}
	if internal.ProviderConfig["TOKEN"] != "secret-internal-token" {
		t.Error("TOKEN should be loaded from file")
	}

	// Check public DNS config
	public, ok := cfg.GetProviderInstance("public-dns")
	if !ok {
		t.Fatal("GetProviderInstance(public-dns) returned false")
	}
	if public.RecordType != provider.RecordTypeCNAME {
		t.Errorf("public.RecordType = %q, want %q", public.RecordType, provider.RecordTypeCNAME)
	}
	if public.Target != "example.com" {
		t.Errorf("public.Target = %q, want %q", public.Target, "example.com")
	}
	if public.ProviderConfig["ZONE_ID"] != "zone123" {
		t.Errorf("ZONE_ID = %q, want %q", public.ProviderConfig["ZONE_ID"], "zone123")
	}
}

func TestLoad_MissingInstances(t *testing.T) {
	clearAllEnv(t)
	defer clearAllEnv(t)

	// No DNSWEAVER_INSTANCES set

	_, err := Load()

	if err == nil {
		t.Fatal("Load() should return error when DNSWEAVER_INSTANCES is not set")
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error should be *ValidationError, got %T", err)
	}

	found := false
	for _, e := range validationErr.Errors {
		if containsSubstring(e, "DNSWEAVER_INSTANCES") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("error should mention DNSWEAVER_INSTANCES, got %v", validationErr.Errors)
	}
}

func TestLoad_MultipleErrors(t *testing.T) {
	clearAllEnv(t)
	defer clearAllEnv(t)

	// Set up config with multiple errors
	os.Setenv("DNSWEAVER_LOG_LEVEL", "invalid")
	os.Setenv("DNSWEAVER_HEALTH_PORT", "-1")
	os.Setenv("DNSWEAVER_INSTANCES", "broken")
	// Missing TYPE, TARGET, DOMAINS for "broken" instance

	_, err := Load()

	if err == nil {
		t.Fatal("Load() should return error")
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error should be *ValidationError, got %T", err)
	}

	// Should have multiple errors
	if len(validationErr.Errors) < 3 {
		t.Errorf("expected at least 3 errors, got %d: %v", len(validationErr.Errors), validationErr.Errors)
	}
}

func TestLoad_TargetRecordTypeMismatch(t *testing.T) {
	clearAllEnv(t)
	defer clearAllEnv(t)

	// A record with hostname target (invalid)
	os.Setenv("DNSWEAVER_INSTANCES", "bad-a-record")
	os.Setenv("DNSWEAVER_BAD_A_RECORD_TYPE", "technitium")
	os.Setenv("DNSWEAVER_BAD_A_RECORD_RECORD_TYPE", "A")
	os.Setenv("DNSWEAVER_BAD_A_RECORD_TARGET", "example.com") // Should be IP
	os.Setenv("DNSWEAVER_BAD_A_RECORD_DOMAINS", "*")

	_, err := Load()

	if err == nil {
		t.Fatal("Load() should return error for A record with hostname target")
	}

	if !containsSubstring(err.Error(), "A records must point to") {
		t.Errorf("error should mention A records needing IP, got: %v", err)
	}
}

func TestLoad_CNAMEWithIPTarget(t *testing.T) {
	clearAllEnv(t)
	defer clearAllEnv(t)

	// CNAME record with IP target (invalid)
	os.Setenv("DNSWEAVER_INSTANCES", "bad-cname")
	os.Setenv("DNSWEAVER_BAD_CNAME_TYPE", "cloudflare")
	os.Setenv("DNSWEAVER_BAD_CNAME_RECORD_TYPE", "CNAME")
	os.Setenv("DNSWEAVER_BAD_CNAME_TARGET", "10.1.20.210") // Should be hostname
	os.Setenv("DNSWEAVER_BAD_CNAME_DOMAINS", "*")

	_, err := Load()

	if err == nil {
		t.Fatal("Load() should return error for CNAME record with IP target")
	}

	if !containsSubstring(err.Error(), "CNAME records cannot point to") {
		t.Errorf("error should mention CNAME needing hostname, got: %v", err)
	}
}

func TestConfig_String(t *testing.T) {
	cfg := &Config{
		Global: &GlobalConfig{
			LogLevel:          "info",
			DryRun:            false,
			ReconcileInterval: 60 * time.Second,
		},
		ProviderNames: []string{"dns1", "dns2"},
	}

	s := cfg.String()

	if !containsSubstring(s, "info") {
		t.Error("String() should contain log level")
	}
	if !containsSubstring(s, "dns1") {
		t.Error("String() should contain provider names")
	}
	if !containsSubstring(s, "dns2") {
		t.Error("String() should contain provider names")
	}
}

func TestConfig_GetProviderInstance_NotFound(t *testing.T) {
	cfg := &Config{
		Global:            &GlobalConfig{},
		ProviderInstances: []*ProviderInstanceConfig{},
	}

	_, ok := cfg.GetProviderInstance("nonexistent")

	if ok {
		t.Error("GetProviderInstance(nonexistent) should return false")
	}
}

func TestProviderInstanceConfig_ToProviderConfig(t *testing.T) {
	cfg := &ProviderInstanceConfig{
		Name:           "test-dns",
		TypeName:       "technitium",
		RecordType:     provider.RecordTypeA,
		Target:         "10.0.0.1",
		TTL:            300,
		Domains:        []string{"*.example.com"},
		ExcludeDomains: []string{"admin.example.com"},
		ProviderConfig: map[string]string{"URL": "http://dns:5380"},
	}

	provCfg := cfg.ToProviderConfig()

	if provCfg.Name != cfg.Name {
		t.Errorf("Name = %q, want %q", provCfg.Name, cfg.Name)
	}
	if provCfg.TypeName != cfg.TypeName {
		t.Errorf("TypeName = %q, want %q", provCfg.TypeName, cfg.TypeName)
	}
	if provCfg.RecordType != cfg.RecordType {
		t.Errorf("RecordType = %q, want %q", provCfg.RecordType, cfg.RecordType)
	}
	if provCfg.Target != cfg.Target {
		t.Errorf("Target = %q, want %q", provCfg.Target, cfg.Target)
	}
	if provCfg.TTL != cfg.TTL {
		t.Errorf("TTL = %d, want %d", provCfg.TTL, cfg.TTL)
	}
}
