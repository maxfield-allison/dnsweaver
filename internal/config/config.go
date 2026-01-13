// Package config handles loading and validation of DNSWeaver configuration
// from environment variables and optional YAML configuration files.
//
// Configuration follows the patterns defined in docs/DECISIONS.md:
//   - All env vars use DNSWEAVER_ prefix
//   - _FILE suffix for Docker secrets (e.g., TOKEN_FILE)
//   - YAML config file via DNSWEAVER_CONFIG env var or --config flag
//   - Priority: env vars > config file > defaults
//   - Fail fast on any configuration error
package config

import (
	"fmt"
	"log/slog"
	"time"
)

// Config holds the complete application configuration.
// All settings use the DNSWEAVER_ prefix as per DECISIONS.md.
type Config struct {
	// Global contains application-wide settings.
	Global *GlobalConfig

	// ProviderNames is the ordered list of instance names
	// from DNSWEAVER_INSTANCES. Order determines matching priority.
	ProviderNames []string

	// ProviderInstances contains configuration for each provider.
	// The order matches ProviderNames.
	ProviderInstances []*ProviderInstanceConfig

	// Sources contains configuration for hostname sources (traefik, caddy, etc.).
	// Includes file-based discovery configuration per source.
	Sources *SourceConfig

	// ConfigFile is the path to the config file used, if any.
	ConfigFile string
}

// Load reads configuration from environment variables and an optional YAML file.
// Returns an error if any required configuration is missing or invalid.
//
// Configuration priority (highest to lowest):
//  1. Environment variables
//  2. Config file values (if DNSWEAVER_CONFIG is set)
//  3. Default values
//
// Per DECISIONS.md: Fail fast with clear error messages. Do not start
// with partial configuration.
func Load() (*Config, error) {
	var allErrors []string

	// Check for config file
	configPath := GetConfigFilePath()

	var fileGlobal *GlobalConfig
	var fileProviders []*ProviderInstanceConfig
	var fileSources *SourceConfig

	if configPath != "" {
		// Load from file first
		var fileErrs []string
		fileGlobal, fileProviders, fileSources, fileErrs = loadFromFile(configPath)
		allErrors = append(allErrors, fileErrs...)

		// If file loading had errors, we still try to proceed with env vars
		if len(fileErrs) == 0 && fileGlobal != nil {
			slog.Debug("config file loaded, applying environment overrides")
		}
	}

	// Merge global config with env var overrides
	var global *GlobalConfig
	var globalErrs []string
	if fileGlobal != nil {
		global, globalErrs = mergeGlobalConfig(fileGlobal)
	} else {
		global, globalErrs = loadGlobalConfig()
	}
	allErrors = append(allErrors, globalErrs...)

	// Determine providers: file config + env var overrides/additions
	var providerNames []string
	var instances []*ProviderInstanceConfig

	// Check if env vars define providers (takes precedence over file)
	envProviderNames := parseInstances()
	if len(envProviderNames) > 0 {
		// Env vars define providers - use env var loading
		providerNames = envProviderNames
		for _, name := range providerNames {
			inst, instErrs := loadInstanceConfig(name, global.DefaultTTL)
			allErrors = append(allErrors, instErrs...)
			instances = append(instances, inst)
		}
	} else if len(fileProviders) > 0 {
		// Use file providers
		for _, fp := range fileProviders {
			providerNames = append(providerNames, fp.Name)
			instances = append(instances, fp)
		}
	} else {
		allErrors = append(allErrors, "no providers configured: set DNSWEAVER_INSTANCES or configure providers in config file")
	}

	// Determine sources: env vars take precedence
	var sources *SourceConfig
	if getEnv("DNSWEAVER_SOURCES") != "" {
		// Env vars define sources
		sources = loadSourceConfig()
	} else if fileSources != nil {
		// Use file sources
		sources = fileSources
	} else {
		// Use default source loading (which defaults to "traefik")
		sources = loadSourceConfig()
	}

	cfg := &Config{
		Global:            global,
		ProviderNames:     providerNames,
		ProviderInstances: instances,
		Sources:           sources,
		ConfigFile:        configPath,
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

// CleanupOrphans returns whether orphan cleanup is enabled.
func (c *Config) CleanupOrphans() bool {
	return c.Global.CleanupOrphans
}

// CleanupOnStop returns whether DNS records should be cleaned up when containers stop.
// If true (default), stopped containers are treated as orphans and their DNS records are removed.
// If false, DNS records are only removed when containers are deleted, not when stopped.
func (c *Config) CleanupOnStop() bool {
	return c.Global.CleanupOnStop
}

// OwnershipTracking returns whether TXT ownership tracking is enabled.
func (c *Config) OwnershipTracking() bool {
	return c.Global.OwnershipTracking
}

// AdoptExisting returns whether existing DNS records should be adopted
// by creating ownership TXT records for them.
func (c *Config) AdoptExisting() bool {
	return c.Global.AdoptExisting
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
