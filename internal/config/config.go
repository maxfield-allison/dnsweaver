// Package config handles loading and validation of DNSWeaver configuration
// from environment variables.
package config

// Config holds the application configuration loaded from environment variables.
// All settings use the DNSWEAVER_ prefix as per DECISIONS.md.
type Config struct {
	// Global settings
	LogLevel          string
	LogFormat         string
	DryRun            bool
	DefaultTTL        int
	ReconcileInterval string
	HealthPort        int

	// Docker settings
	DockerHost string
	DockerMode string

	// Source settings
	Source string

	// Provider instances (parsed from DNSWEAVER_PROVIDERS)
	Providers []string
}

// TODO: Implement in Issue #4 - Configuration system
// - Load() function
// - Environment variable parsing with _FILE support
// - Validation with fail-fast on errors
// - Provider instance parsing
