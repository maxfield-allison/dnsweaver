// Package config handles loading and validation of DNSWeaver configuration
// from environment variables.
//
// Configuration follows the patterns defined in docs/DECISIONS.md:
//   - All env vars use DNSWEAVER_ prefix
//   - _FILE suffix for Docker secrets (e.g., TOKEN_FILE)
//   - Fail fast on any configuration error
package config

import (
	"fmt"
	"time"
)

// Config holds the complete application configuration.
// All settings use the DNSWEAVER_ prefix as per DECISIONS.md.
type Config struct {
	// Global contains application-wide settings.
	Global *GlobalConfig

	// ProviderNames is the ordered list of provider instance names
	// from DNSWEAVER_PROVIDERS. Order determines matching priority.
	ProviderNames []string

	// ProviderInstances contains configuration for each provider.
	// The order matches ProviderNames.
	ProviderInstances []*ProviderInstanceConfig

	// Sources contains configuration for hostname sources (traefik, caddy, etc.).
	// Includes file-based discovery configuration per source.
	Sources *SourceConfig
}

// Load reads configuration from environment variables and validates it.
// Returns an error if any required configuration is missing or invalid.
//
// Per DECISIONS.md: Fail fast with clear error messages. Do not start
// with partial configuration.
func Load() (*Config, error) {
	var allErrors []string

	// Load global configuration
	global, globalErrs := loadGlobalConfig()
	allErrors = append(allErrors, globalErrs...)

	// Parse provider instance names
	providerNames := parseProviders()
	if len(providerNames) == 0 {
		allErrors = append(allErrors, "DNSWEAVER_PROVIDERS: required but not set (comma-separated list of provider instance names)")
	}

	// Load each provider instance configuration
	var instances []*ProviderInstanceConfig
	for _, name := range providerNames {
		inst, instErrs := loadInstanceConfig(name, global.DefaultTTL)
		allErrors = append(allErrors, instErrs...)
		instances = append(instances, inst)
	}

	// Load source configuration (traefik, caddy, etc.)
	sources := loadSourceConfig()

	cfg := &Config{
		Global:            global,
		ProviderNames:     providerNames,
		ProviderInstances: instances,
		Sources:           sources,
	}

	// Run cross-field validation
	allErrors = append(allErrors, validateConfig(cfg)...)

	if len(allErrors) > 0 {
		return nil, &ValidationError{Errors: allErrors}
	}

	return cfg, nil
}

// LogLevel returns the configured log level.
func (c *Config) LogLevel() string {
	return c.Global.LogLevel
}

// LogFormat returns the configured log format.
func (c *Config) LogFormat() string {
	return c.Global.LogFormat
}

// DryRun returns whether dry-run mode is enabled.
func (c *Config) DryRun() bool {
	return c.Global.DryRun
}

// ReconcileInterval returns the reconciliation interval.
func (c *Config) ReconcileInterval() time.Duration {
	return c.Global.ReconcileInterval
}

// HealthPort returns the health server port.
func (c *Config) HealthPort() int {
	return c.Global.HealthPort
}

// DockerHost returns the Docker socket/host path.
func (c *Config) DockerHost() string {
	return c.Global.DockerHost
}

// DockerMode returns the Docker mode (auto/swarm/standalone).
func (c *Config) DockerMode() string {
	return c.Global.DockerMode
}

// Source returns the hostname source type.
func (c *Config) Source() string {
	return c.Global.Source
}

// GetProviderInstance returns the configuration for a specific provider instance.
func (c *Config) GetProviderInstance(name string) (*ProviderInstanceConfig, bool) {
	for _, inst := range c.ProviderInstances {
		if inst.Name == name {
			return inst, true
		}
	}
	return nil, false
}

// GetSourceInstance returns the configuration for a specific source by name.
func (c *Config) GetSourceInstance(name string) *SourceInstanceConfig {
	if c.Sources == nil {
		return nil
	}
	return c.Sources.GetSourceInstance(name)
}

// SourceNames returns the list of configured source names.
func (c *Config) SourceNames() []string {
	if c.Sources == nil {
		return nil
	}
	return c.Sources.Names
}

// HasFileDiscovery returns true if any source has file discovery configured.
func (c *Config) HasFileDiscovery() bool {
	return c.Sources != nil && c.Sources.HasFileDiscovery()
}

// String returns a summary of the configuration (without secrets).
func (c *Config) String() string {
	sourceNames := "[]"
	if c.Sources != nil {
		sourceNames = fmt.Sprintf("%v", c.Sources.Names)
	}
	return fmt.Sprintf(
		"Config{LogLevel=%s, DryRun=%v, ReconcileInterval=%s, Providers=%v, Sources=%s}",
		c.Global.LogLevel,
		c.Global.DryRun,
		c.Global.ReconcileInterval,
		c.ProviderNames,
		sourceNames,
	)
}
