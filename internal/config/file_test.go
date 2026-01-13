package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInterpolateEnvVars(t *testing.T) {
	// Set up test environment variables
	os.Setenv("TEST_VAR", "test-value")
	os.Setenv("API_TOKEN", "secret123")
	defer os.Unsetenv("TEST_VAR")
	defer os.Unsetenv("API_TOKEN")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple variable",
			input:    "${TEST_VAR}",
			expected: "test-value",
		},
		{
			name:     "variable in string",
			input:    "prefix-${TEST_VAR}-suffix",
			expected: "prefix-test-value-suffix",
		},
		{
			name:     "multiple variables",
			input:    "${TEST_VAR}:${API_TOKEN}",
			expected: "test-value:secret123",
		},
		{
			name:     "unset variable",
			input:    "${NONEXISTENT_VAR}",
			expected: "",
		},
		{
			name:     "default value",
			input:    "${NONEXISTENT_VAR:-default}",
			expected: "default",
		},
		{
			name:     "default value not used when set",
			input:    "${TEST_VAR:-default}",
			expected: "test-value",
		},
		{
			name:     "no variables",
			input:    "plain string",
			expected: "plain string",
		},
		{
			name:     "empty default",
			input:    "${NONEXISTENT:-}",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := InterpolateEnvVars(tt.input)
			if result != tt.expected {
				t.Errorf("InterpolateEnvVars(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLoadFile(t *testing.T) {
	// Set up test environment variable for interpolation
	os.Setenv("TEST_TOKEN", "secret-from-env")
	defer os.Unsetenv("TEST_TOKEN")

	// Create a temporary config file
	configContent := `
logging:
  level: debug
  format: text

reconciler:
  interval: 30s
  dry_run: true
  cleanup_orphans: false

docker:
  host: unix:///var/run/docker.sock
  mode: swarm

sources:
  - name: traefik
    file_discovery:
      paths:
        - /config/traefik/dynamic
      pattern: "*.yml"
      poll_interval: 60s

providers:
  - name: internal
    type: technitium
    domains:
      - "*.internal.example.com"
    target: 10.1.20.210
    ttl: 300
    config:
      url: http://dns.example.com:5380
      token: ${TEST_TOKEN}

server:
  port: 9090
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Load the config
	cfg, err := LoadFile(configPath)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}

	// Verify logging
	if cfg.Logging == nil {
		t.Fatal("logging config is nil")
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("logging.level = %q, want %q", cfg.Logging.Level, "debug")
	}
	if cfg.Logging.Format != "text" {
		t.Errorf("logging.format = %q, want %q", cfg.Logging.Format, "text")
	}

	// Verify reconciler
	if cfg.Reconciler == nil {
		t.Fatal("reconciler config is nil")
	}
	if cfg.Reconciler.Interval != "30s" {
		t.Errorf("reconciler.interval = %q, want %q", cfg.Reconciler.Interval, "30s")
	}
	if cfg.Reconciler.DryRun == nil || !*cfg.Reconciler.DryRun {
		t.Error("reconciler.dry_run should be true")
	}
	if cfg.Reconciler.CleanupOrphans == nil || *cfg.Reconciler.CleanupOrphans {
		t.Error("reconciler.cleanup_orphans should be false")
	}

	// Verify docker
	if cfg.Docker == nil {
		t.Fatal("docker config is nil")
	}
	if cfg.Docker.Mode != "swarm" {
		t.Errorf("docker.mode = %q, want %q", cfg.Docker.Mode, "swarm")
	}

	// Verify sources
	if len(cfg.Sources) != 1 {
		t.Fatalf("sources count = %d, want 1", len(cfg.Sources))
	}
	if cfg.Sources[0].Name != "traefik" {
		t.Errorf("sources[0].name = %q, want %q", cfg.Sources[0].Name, "traefik")
	}
	if cfg.Sources[0].FileDiscovery == nil {
		t.Fatal("sources[0].file_discovery is nil")
	}
	if len(cfg.Sources[0].FileDiscovery.Paths) != 1 {
		t.Errorf("sources[0].file_discovery.paths count = %d, want 1", len(cfg.Sources[0].FileDiscovery.Paths))
	}

	// Verify providers
	if len(cfg.Providers) != 1 {
		t.Fatalf("providers count = %d, want 1", len(cfg.Providers))
	}
	p := cfg.Providers[0]
	if p.Name != "internal" {
		t.Errorf("providers[0].name = %q, want %q", p.Name, "internal")
	}
	if p.Type != "technitium" {
		t.Errorf("providers[0].type = %q, want %q", p.Type, "technitium")
	}
	if p.Target != "10.1.20.210" {
		t.Errorf("providers[0].target = %q, want %q", p.Target, "10.1.20.210")
	}
	// Verify env var interpolation in config
	if p.Config["token"] != "secret-from-env" {
		t.Errorf("providers[0].config[token] = %q, want %q", p.Config["token"], "secret-from-env")
	}

	// Verify server
	if cfg.Server == nil {
		t.Fatal("server config is nil")
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("server.port = %d, want %d", cfg.Server.Port, 9090)
	}
}

func TestToGlobalConfig(t *testing.T) {
	dryRun := true
	cleanup := false

	fileCfg := &FileConfig{
		Logging: &FileLoggingConfig{
			Level:  "warn",
			Format: "json",
		},
		Reconciler: &FileReconcilerConfig{
			Interval:       "5m",
			DryRun:         &dryRun,
			CleanupOrphans: &cleanup,
		},
		Docker: &FileDockerConfig{
			Host: "tcp://docker:2375",
			Mode: "standalone",
		},
		Server: &FileServerConfig{
			Port: 8081,
		},
	}

	global := fileCfg.ToGlobalConfig()

	if global.LogLevel != "warn" {
		t.Errorf("LogLevel = %q, want %q", global.LogLevel, "warn")
	}
	if global.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want %q", global.LogFormat, "json")
	}
	if !global.DryRun {
		t.Error("DryRun should be true")
	}
	if global.CleanupOrphans {
		t.Error("CleanupOrphans should be false")
	}
	if global.ReconcileInterval.String() != "5m0s" {
		t.Errorf("ReconcileInterval = %s, want 5m0s", global.ReconcileInterval)
	}
	if global.DockerHost != "tcp://docker:2375" {
		t.Errorf("DockerHost = %q, want %q", global.DockerHost, "tcp://docker:2375")
	}
	if global.DockerMode != "standalone" {
		t.Errorf("DockerMode = %q, want %q", global.DockerMode, "standalone")
	}
	if global.HealthPort != 8081 {
		t.Errorf("HealthPort = %d, want %d", global.HealthPort, 8081)
	}
}

func TestLoadFileNotFound(t *testing.T) {
	_, err := LoadFile("/nonexistent/path/config.yml")
	if err == nil {
		t.Error("LoadFile should fail for nonexistent file")
	}
}

func TestLoadFileInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yml")
	if err := os.WriteFile(configPath, []byte("invalid: yaml: content: ["), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, err := LoadFile(configPath)
	if err == nil {
		t.Error("LoadFile should fail for invalid YAML")
	}
}

func TestGetConfigFilePath(t *testing.T) {
	// Test with no env var set
	os.Unsetenv("DNSWEAVER_CONFIG")
	path := GetConfigFilePath()
	if path != "" {
		t.Errorf("GetConfigFilePath() = %q, want empty string", path)
	}

	// Test with env var set
	os.Setenv("DNSWEAVER_CONFIG", "/path/to/config.yml")
	defer os.Unsetenv("DNSWEAVER_CONFIG")
	path = GetConfigFilePath()
	if path != "/path/to/config.yml" {
		t.Errorf("GetConfigFilePath() = %q, want %q", path, "/path/to/config.yml")
	}
}
