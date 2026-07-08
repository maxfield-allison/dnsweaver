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
		"DNSWEAVER_LOG_FILE",
		"DNSWEAVER_LOG_MAX_SIZE",
		"DNSWEAVER_LOG_MAX_BACKUPS",
		"DNSWEAVER_LOG_MAX_AGE",
		"DNSWEAVER_LOG_COMPRESS",
		"DNSWEAVER_DRY_RUN",
		"DNSWEAVER_CLEANUP_ORPHANS",
		"DNSWEAVER_OWNERSHIP_TRACKING",
		"DNSWEAVER_ADOPT_EXISTING",
		"DNSWEAVER_DEFAULT_TTL",
		"DNSWEAVER_RECONCILE_INTERVAL",
		"DNSWEAVER_SHUTDOWN_TIMEOUT",
		"DNSWEAVER_HEALTH_PORT",
		"DNSWEAVER_DOCKER_HOST",
		"DNSWEAVER_DOCKER_MODE",
		"DNSWEAVER_DOCKER_CONNECT_TIMEOUT",
		"DNSWEAVER_SOURCES",
		"DNSWEAVER_SOURCE",
		"DNSWEAVER_INSTANCE_ID",
		"DNSWEAVER_PLATFORM",
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
	if cfg.LogFile != "" {
		t.Errorf("LogFile = %q, want empty", cfg.LogFile)
	}
	if cfg.LogMaxSize != DefaultLogMaxSize {
		t.Errorf("LogMaxSize = %d, want %d", cfg.LogMaxSize, DefaultLogMaxSize)
	}
	if cfg.LogMaxBackups != DefaultLogMaxBackups {
		t.Errorf("LogMaxBackups = %d, want %d", cfg.LogMaxBackups, DefaultLogMaxBackups)
	}
	if cfg.LogMaxAge != DefaultLogMaxAge {
		t.Errorf("LogMaxAge = %d, want %d", cfg.LogMaxAge, DefaultLogMaxAge)
	}
	if cfg.LogCompress != DefaultLogCompress {
		t.Errorf("LogCompress = %v, want %v", cfg.LogCompress, DefaultLogCompress)
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
	if cfg.ShutdownTimeout != DefaultShutdownTimeout {
		t.Errorf("ShutdownTimeout = %v, want %v", cfg.ShutdownTimeout, DefaultShutdownTimeout)
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
	if cfg.DockerConnectTimeout != DefaultDockerConnectTimeout {
		t.Errorf("DockerConnectTimeout = %v, want %v", cfg.DockerConnectTimeout, DefaultDockerConnectTimeout)
	}
	if cfg.Source != DefaultSource {
		t.Errorf("Source = %q, want %q", cfg.Source, DefaultSource)
	}
	if cfg.InstanceID != DefaultInstanceID {
		t.Errorf("InstanceID = %q, want %q", cfg.InstanceID, DefaultInstanceID)
		os.Setenv("DNSWEAVER_LOG_FILE", "/var/log/dnsweaver.log")
		os.Setenv("DNSWEAVER_LOG_MAX_SIZE", "50")
		os.Setenv("DNSWEAVER_LOG_MAX_BACKUPS", "3")
		os.Setenv("DNSWEAVER_LOG_MAX_AGE", "14")
		os.Setenv("DNSWEAVER_LOG_COMPRESS", "false")
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
	os.Setenv("DNSWEAVER_DOCKER_CONNECT_TIMEOUT", "45s")
	os.Setenv("DNSWEAVER_LOG_FILE", "/var/log/dnsweaver.log")
	os.Setenv("DNSWEAVER_LOG_MAX_SIZE", "50")
	os.Setenv("DNSWEAVER_LOG_MAX_BACKUPS", "3")
	os.Setenv("DNSWEAVER_LOG_MAX_AGE", "14")
	os.Setenv("DNSWEAVER_LOG_COMPRESS", "false")
	os.Setenv("DNSWEAVER_SHUTDOWN_TIMEOUT", "45s")
	// Note: DNSWEAVER_SOURCE is deprecated; GlobalConfig.Source is now always the default.
	// Source list is controlled by DNSWEAVER_SOURCES via parseSources().

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
	if cfg.LogFile != "/var/log/dnsweaver.log" {
		t.Errorf("LogFile = %q, want %q", cfg.LogFile, "/var/log/dnsweaver.log")
	}
	if cfg.LogMaxSize != 50 {
		t.Errorf("LogMaxSize = %d, want %d", cfg.LogMaxSize, 50)
	}
	if cfg.LogMaxBackups != 3 {
		t.Errorf("LogMaxBackups = %d, want %d", cfg.LogMaxBackups, 3)
	}
	if cfg.LogMaxAge != 14 {
		t.Errorf("LogMaxAge = %d, want %d", cfg.LogMaxAge, 14)
	}
	if cfg.LogCompress {
		t.Error("LogCompress = true, want false")
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
	if cfg.ShutdownTimeout != 45*time.Second {
		t.Errorf("ShutdownTimeout = %v, want %v", cfg.ShutdownTimeout, 45*time.Second)
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
	if cfg.DockerConnectTimeout != 45*time.Second {
		t.Errorf("DockerConnectTimeout = %v, want %v", cfg.DockerConnectTimeout, 45*time.Second)
	}
	if cfg.Source != DefaultSource {
		t.Errorf("Source = %q, want %q (deprecated DNSWEAVER_SOURCE should not set GlobalConfig.Source)", cfg.Source, DefaultSource)
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
			name:     "invalid log max size not a number",
			envVar:   "DNSWEAVER_LOG_MAX_SIZE",
			value:    "abc",
			errMatch: "LOG_MAX_SIZE",
		},
		{
			name:     "log max size too small",
			envVar:   "DNSWEAVER_LOG_MAX_SIZE",
			value:    "0",
			errMatch: "LOG_MAX_SIZE",
		},
		{
			name:     "invalid log max backups not a number",
			envVar:   "DNSWEAVER_LOG_MAX_BACKUPS",
			value:    "abc",
			errMatch: "LOG_MAX_BACKUPS",
		},
		{
			name:     "log max backups negative",
			envVar:   "DNSWEAVER_LOG_MAX_BACKUPS",
			value:    "-1",
			errMatch: "LOG_MAX_BACKUPS",
		},
		{
			name:     "invalid log max age not a number",
			envVar:   "DNSWEAVER_LOG_MAX_AGE",
			value:    "abc",
			errMatch: "LOG_MAX_AGE",
		},
		{
			name:     "log max age negative",
			envVar:   "DNSWEAVER_LOG_MAX_AGE",
			value:    "-1",
			errMatch: "LOG_MAX_AGE",
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
		{
			name:     "invalid shutdown timeout",
			envVar:   "DNSWEAVER_SHUTDOWN_TIMEOUT",
			value:    "not-a-duration",
			errMatch: "SHUTDOWN_TIMEOUT",
		},
		{
			name:     "shutdown timeout too short",
			envVar:   "DNSWEAVER_SHUTDOWN_TIMEOUT",
			value:    "500ms",
			errMatch: "SHUTDOWN_TIMEOUT",
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
				if contains(err.Error(), tc.errMatch) {
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

func TestLoadGlobalConfig_InstanceID(t *testing.T) {
	clearGlobalEnv(t)

	t.Run("valid instance ID", func(t *testing.T) {
		os.Setenv("DNSWEAVER_INSTANCE_ID", "pi5-dns")
		defer os.Unsetenv("DNSWEAVER_INSTANCE_ID")

		cfg, errs := loadGlobalConfig()
		if len(errs) > 0 {
			t.Errorf("unexpected errors: %v", errs)
		}
		if cfg.InstanceID != "pi5-dns" {
			t.Errorf("InstanceID = %q, want %q", cfg.InstanceID, "pi5-dns")
		}
	})

	t.Run("empty instance ID is allowed", func(t *testing.T) {
		os.Setenv("DNSWEAVER_INSTANCE_ID", "")
		defer os.Unsetenv("DNSWEAVER_INSTANCE_ID")

		cfg, errs := loadGlobalConfig()
		if len(errs) > 0 {
			t.Errorf("unexpected errors: %v", errs)
		}
		if cfg.InstanceID != "" {
			t.Errorf("InstanceID = %q, want empty", cfg.InstanceID)
		}
	})

	t.Run("instance ID with dots dashes underscores", func(t *testing.T) {
		os.Setenv("DNSWEAVER_INSTANCE_ID", "k8s-node_01.prod")
		defer os.Unsetenv("DNSWEAVER_INSTANCE_ID")

		cfg, errs := loadGlobalConfig()
		if len(errs) > 0 {
			t.Errorf("unexpected errors: %v", errs)
		}
		if cfg.InstanceID != "k8s-node_01.prod" {
			t.Errorf("InstanceID = %q, want %q", cfg.InstanceID, "k8s-node_01.prod")
		}
	})

	t.Run("instance ID too long", func(t *testing.T) {
		os.Setenv("DNSWEAVER_INSTANCE_ID", "a123456789012345678901234567890123456789012345678901234567890TOOLONG")
		defer os.Unsetenv("DNSWEAVER_INSTANCE_ID")

		_, errs := loadGlobalConfig()
		if len(errs) == 0 {
			t.Error("expected error for instance ID exceeding max length")
		}
	})

	t.Run("instance ID with invalid characters", func(t *testing.T) {
		os.Setenv("DNSWEAVER_INSTANCE_ID", "invalid id!")
		defer os.Unsetenv("DNSWEAVER_INSTANCE_ID")

		_, errs := loadGlobalConfig()
		if len(errs) == 0 {
			t.Error("expected error for instance ID with invalid characters")
		}
	})

	t.Run("instance ID starting with dash", func(t *testing.T) {
		os.Setenv("DNSWEAVER_INSTANCE_ID", "-invalid")
		defer os.Unsetenv("DNSWEAVER_INSTANCE_ID")

		_, errs := loadGlobalConfig()
		if len(errs) == 0 {
			t.Error("expected error for instance ID starting with dash")
		}
	})
}

func TestValidateInstanceID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{name: "empty is valid", id: "", wantErr: false},
		{name: "simple alphanumeric", id: "pi5dns", wantErr: false},
		{name: "with dashes", id: "pi5-dns", wantErr: false},
		{name: "with underscores", id: "pi5_dns", wantErr: false},
		{name: "with dots", id: "pi5.dns", wantErr: false},
		{name: "complex valid", id: "k8s-node_01.prod", wantErr: false},
		{name: "single char", id: "a", wantErr: false},
		{name: "max length 63", id: "a23456789012345678901234567890123456789012345678901234567890123", wantErr: false},
		{name: "64 chars too long", id: "a234567890123456789012345678901234567890123456789012345678901234", wantErr: true},
		{name: "starts with dash", id: "-invalid", wantErr: true},
		{name: "starts with dot", id: ".invalid", wantErr: true},
		{name: "starts with underscore", id: "_invalid", wantErr: true},
		{name: "contains space", id: "invalid id", wantErr: true},
		{name: "contains exclamation", id: "invalid!", wantErr: true},
		{name: "contains slash", id: "path/id", wantErr: true},
		{name: "contains comma", id: "a,b", wantErr: true},
		{name: "contains equals", id: "a=b", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInstanceID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateInstanceID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
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
