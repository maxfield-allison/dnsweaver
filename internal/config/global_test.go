package config

import (
	"os"
	"testing"
	"time"
)

// clearGlobalEnv removes all DNSWEAVER_ environment variables.
func clearGlobalEnv(t *testing.T) {
	t.Helper()
	envVars := []string{
		"DNSWEAVER_LOG_LEVEL",
		"DNSWEAVER_LOG_FORMAT",
		"DNSWEAVER_DRY_RUN",
		"DNSWEAVER_CLEANUP_ORPHANS",
		"DNSWEAVER_OWNERSHIP_TRACKING",
		"DNSWEAVER_ADOPT_EXISTING",
		"DNSWEAVER_DEFAULT_TTL",
		"DNSWEAVER_RECONCILE_INTERVAL",
		"DNSWEAVER_HEALTH_PORT",
		"DNSWEAVER_DOCKER_HOST",
		"DNSWEAVER_DOCKER_MODE",
		"DNSWEAVER_SOURCE",
	}
	for _, v := range envVars {
		os.Unsetenv(v)
	}
}

func TestLoadGlobalConfig_Defaults(t *testing.T) {
	clearGlobalEnv(t)

	cfg, errs := loadGlobalConfig()

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}

	// Check defaults
	if cfg.LogLevel != DefaultLogLevel {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, DefaultLogLevel)
	}
	if cfg.LogFormat != DefaultLogFormat {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, DefaultLogFormat)
	}
	if cfg.DryRun != DefaultDryRun {
		t.Errorf("DryRun = %v, want %v", cfg.DryRun, DefaultDryRun)
	}
	if cfg.CleanupOrphans != DefaultCleanupOrphans {
		t.Errorf("CleanupOrphans = %v, want %v", cfg.CleanupOrphans, DefaultCleanupOrphans)
	}
	if cfg.OwnershipTracking != DefaultOwnershipTracking {
		t.Errorf("OwnershipTracking = %v, want %v", cfg.OwnershipTracking, DefaultOwnershipTracking)
	}
	if cfg.AdoptExisting != DefaultAdoptExisting {
		t.Errorf("AdoptExisting = %v, want %v", cfg.AdoptExisting, DefaultAdoptExisting)
	}
	if cfg.DefaultTTL != DefaultTTL {
		t.Errorf("DefaultTTL = %d, want %d", cfg.DefaultTTL, DefaultTTL)
	}
	if cfg.ReconcileInterval != DefaultReconcileInterval {
		t.Errorf("ReconcileInterval = %v, want %v", cfg.ReconcileInterval, DefaultReconcileInterval)
	}
	if cfg.HealthPort != DefaultHealthPort {
		t.Errorf("HealthPort = %d, want %d", cfg.HealthPort, DefaultHealthPort)
	}
	if cfg.DockerHost != DefaultDockerHost {
		t.Errorf("DockerHost = %q, want %q", cfg.DockerHost, DefaultDockerHost)
	}
	if cfg.DockerMode != DefaultDockerMode {
		t.Errorf("DockerMode = %q, want %q", cfg.DockerMode, DefaultDockerMode)
	}
	if cfg.Source != DefaultSource {
		t.Errorf("Source = %q, want %q", cfg.Source, DefaultSource)
	}
}

func TestLoadGlobalConfig_CustomValues(t *testing.T) {
	clearGlobalEnv(t)
	defer clearGlobalEnv(t)

	os.Setenv("DNSWEAVER_LOG_LEVEL", "debug")
	os.Setenv("DNSWEAVER_LOG_FORMAT", "text")
	os.Setenv("DNSWEAVER_DRY_RUN", "true")
	os.Setenv("DNSWEAVER_DEFAULT_TTL", "600")
	os.Setenv("DNSWEAVER_RECONCILE_INTERVAL", "5m")
	os.Setenv("DNSWEAVER_HEALTH_PORT", "9090")
	os.Setenv("DNSWEAVER_DOCKER_HOST", "tcp://localhost:2375")
	os.Setenv("DNSWEAVER_DOCKER_MODE", "swarm")
	os.Setenv("DNSWEAVER_SOURCE", "labels")

	cfg, errs := loadGlobalConfig()

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}

	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.LogFormat != "text" {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, "text")
	}
	if !cfg.DryRun {
		t.Error("DryRun = false, want true")
	}
	if cfg.DefaultTTL != 600 {
		t.Errorf("DefaultTTL = %d, want %d", cfg.DefaultTTL, 600)
	}
	if cfg.ReconcileInterval != 5*time.Minute {
		t.Errorf("ReconcileInterval = %v, want %v", cfg.ReconcileInterval, 5*time.Minute)
	}
	if cfg.HealthPort != 9090 {
		t.Errorf("HealthPort = %d, want %d", cfg.HealthPort, 9090)
	}
	if cfg.DockerHost != "tcp://localhost:2375" {
		t.Errorf("DockerHost = %q, want %q", cfg.DockerHost, "tcp://localhost:2375")
	}
	if cfg.DockerMode != "swarm" {
		t.Errorf("DockerMode = %q, want %q", cfg.DockerMode, "swarm")
	}
	if cfg.Source != "labels" {
		t.Errorf("Source = %q, want %q", cfg.Source, "labels")
	}
}

func TestLoadGlobalConfig_InvalidValues(t *testing.T) {
	tests := []struct {
		name     string
		envVar   string
		value    string
		errMatch string
	}{
		{
			name:     "invalid log level",
			envVar:   "DNSWEAVER_LOG_LEVEL",
			value:    "verbose",
			errMatch: "LOG_LEVEL",
		},
		{
			name:     "invalid log format",
			envVar:   "DNSWEAVER_LOG_FORMAT",
			value:    "xml",
			errMatch: "LOG_FORMAT",
		},
		{
			name:     "invalid docker mode",
			envVar:   "DNSWEAVER_DOCKER_MODE",
			value:    "kubernetes",
			errMatch: "DOCKER_MODE",
		},
		{
			name:     "invalid TTL not a number",
			envVar:   "DNSWEAVER_DEFAULT_TTL",
			value:    "abc",
			errMatch: "DEFAULT_TTL",
		},
		{
			name:     "invalid TTL negative",
			envVar:   "DNSWEAVER_DEFAULT_TTL",
			value:    "-1",
			errMatch: "DEFAULT_TTL",
		},
		{
			name:     "invalid reconcile interval",
			envVar:   "DNSWEAVER_RECONCILE_INTERVAL",
			value:    "not-a-duration",
			errMatch: "RECONCILE_INTERVAL",
		},
		{
			name:     "reconcile interval too short",
			envVar:   "DNSWEAVER_RECONCILE_INTERVAL",
			value:    "500ms",
			errMatch: "RECONCILE_INTERVAL",
		},
		{
			name:     "invalid health port",
			envVar:   "DNSWEAVER_HEALTH_PORT",
			value:    "abc",
			errMatch: "HEALTH_PORT",
		},
		{
			name:     "health port out of range",
			envVar:   "DNSWEAVER_HEALTH_PORT",
			value:    "70000",
			errMatch: "HEALTH_PORT",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearGlobalEnv(t)
			defer clearGlobalEnv(t)

			os.Setenv(tc.envVar, tc.value)

			_, errs := loadGlobalConfig()

			if len(errs) == 0 {
				t.Error("expected validation error, got none")
				return
			}

			found := false
			for _, err := range errs {
				if contains(err, tc.errMatch) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected error containing %q, got %v", tc.errMatch, errs)
			}
		})
	}
}

func TestLoadGlobalConfig_CaseInsensitive(t *testing.T) {
	clearGlobalEnv(t)
	defer clearGlobalEnv(t)

	// Set uppercase values that should be normalized to lowercase
	os.Setenv("DNSWEAVER_LOG_LEVEL", "DEBUG")
	os.Setenv("DNSWEAVER_LOG_FORMAT", "JSON")
	os.Setenv("DNSWEAVER_DOCKER_MODE", "SWARM")

	cfg, errs := loadGlobalConfig()

	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}

	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q (lowercased)", cfg.LogLevel, "debug")
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want %q (lowercased)", cfg.LogFormat, "json")
	}
	if cfg.DockerMode != "swarm" {
		t.Errorf("DockerMode = %q, want %q (lowercased)", cfg.DockerMode, "swarm")
	}
}

func TestLoadGlobalConfig_AdoptExisting(t *testing.T) {
	tests := []struct {
		name   string
		envVal string
		want   bool
	}{
		{"default when unset", "", false},
		{"explicit true", "true", true},
		{"explicit false", "false", false},
		{"1 means true", "1", true},
		{"0 means false", "0", false},
		{"yes means true", "yes", true},
		{"no means false", "no", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearGlobalEnv(t)
			defer clearGlobalEnv(t)

			if tt.envVal != "" {
				os.Setenv("DNSWEAVER_ADOPT_EXISTING", tt.envVal)
			}

			cfg, errs := loadGlobalConfig()
			if len(errs) > 0 {
				t.Errorf("unexpected errors: %v", errs)
			}

			if cfg.AdoptExisting != tt.want {
				t.Errorf("AdoptExisting = %v, want %v", cfg.AdoptExisting, tt.want)
			}
		})
	}
}

// contains checks if s contains substr (case-insensitive for simplicity).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
