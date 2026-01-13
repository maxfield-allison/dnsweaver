// Package config handles loading and validation of DNSWeaver configuration.
package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FileConfig represents the YAML configuration file structure.
// This mirrors the runtime Config but uses YAML-friendly types.
type FileConfig struct {
	// Logging configuration
	Logging *FileLoggingConfig `yaml:"logging,omitempty"`

	// Reconciler settings
	Reconciler *FileReconcilerConfig `yaml:"reconciler,omitempty"`

	// Docker connection settings
	Docker *FileDockerConfig `yaml:"docker,omitempty"`

	// Hostname sources
	Sources []FileSourceConfig `yaml:"sources,omitempty"`

	// DNS providers
	Providers []FileProviderConfig `yaml:"providers,omitempty"`

	// Health and metrics server
	Server *FileServerConfig `yaml:"server,omitempty"`
}

// FileLoggingConfig holds logging settings.
type FileLoggingConfig struct {
	Level  string `yaml:"level,omitempty"`  // debug, info, warn, error
	Format string `yaml:"format,omitempty"` // json, text
}

// FileReconcilerConfig holds reconciliation settings.
type FileReconcilerConfig struct {
	Interval          string `yaml:"interval,omitempty"`           // Go duration format (e.g., "60s", "5m")
	DryRun            *bool  `yaml:"dry_run,omitempty"`            // Pointer to distinguish unset from false
	CleanupOrphans    *bool  `yaml:"cleanup_orphans,omitempty"`    // Delete records for removed workloads
	CleanupOnStop     *bool  `yaml:"cleanup_on_stop,omitempty"`    // Delete records when containers stop
	OwnershipTracking *bool  `yaml:"ownership_tracking,omitempty"` // Use TXT records for ownership
	AdoptExisting     *bool  `yaml:"adopt_existing,omitempty"`     // Adopt pre-existing DNS records
	OrphanDelay       string `yaml:"orphan_delay,omitempty"`       // Delay before orphan cleanup
}

// FileDockerConfig holds Docker connection settings.
type FileDockerConfig struct {
	Host string `yaml:"host,omitempty"` // unix:///var/run/docker.sock or tcp://...
	Mode string `yaml:"mode,omitempty"` // auto, swarm, standalone
}

// FileSourceConfig holds configuration for a hostname source.
type FileSourceConfig struct {
	Name          string                   `yaml:"name"`                     // traefik, caddy, dnsweaver, etc.
	FileDiscovery *FileFileDiscoveryConfig `yaml:"file_discovery,omitempty"` // Optional file discovery settings
}

// FileFileDiscoveryConfig holds file-based discovery settings.
type FileFileDiscoveryConfig struct {
	Paths        []string `yaml:"paths,omitempty"`         // List of paths to watch
	Pattern      string   `yaml:"pattern,omitempty"`       // Glob pattern for files
	PollInterval string   `yaml:"poll_interval,omitempty"` // How often to check files
	WatchMethod  string   `yaml:"watch_method,omitempty"`  // auto, inotify, poll
}

// FileProviderConfig holds configuration for a DNS provider instance.
type FileProviderConfig struct {
	Name                string            `yaml:"name"`                            // Unique instance name
	Type                string            `yaml:"type"`                            // technitium, cloudflare, pihole, etc.
	Domains             []string          `yaml:"domains,omitempty"`               // Glob patterns
	DomainsRegex        []string          `yaml:"domains_regex,omitempty"`         // Regex patterns
	ExcludeDomains      []string          `yaml:"exclude_domains,omitempty"`       // Glob exclude patterns
	ExcludeDomainsRegex []string          `yaml:"exclude_domains_regex,omitempty"` // Regex exclude patterns
	RecordType          string            `yaml:"record_type,omitempty"`           // A, AAAA, CNAME
	Target              string            `yaml:"target"`                          // IP or hostname
	TTL                 int               `yaml:"ttl,omitempty"`                   // Default TTL
	Mode                string            `yaml:"mode,omitempty"`                  // managed, authoritative, additive
	Config              map[string]string `yaml:"config,omitempty"`                // Provider-specific settings
}

// FileServerConfig holds health/metrics server settings.
type FileServerConfig struct {
	Port int `yaml:"port,omitempty"` // Port for health/metrics endpoints
}

// envVarPattern matches ${VAR} or ${VAR:-default} syntax.
var envVarPattern = regexp.MustCompile(`\$\{([^}:]+)(?::-([^}]*))?\}`)

// InterpolateEnvVars replaces ${VAR} patterns with environment variable values.
// Supports ${VAR:-default} syntax for default values.
func InterpolateEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		groups := envVarPattern.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}
		varName := groups[1]
		defaultValue := ""
		if len(groups) >= 3 {
			defaultValue = groups[2]
		}

		if value := os.Getenv(varName); value != "" {
			return value
		}
		return defaultValue
	})
}

// interpolateConfigStrings recursively interpolates environment variables
// in all string fields of the config structure.
func (c *FileConfig) interpolateEnvVars() {
	if c.Logging != nil {
		c.Logging.Level = InterpolateEnvVars(c.Logging.Level)
		c.Logging.Format = InterpolateEnvVars(c.Logging.Format)
	}

	if c.Reconciler != nil {
		c.Reconciler.Interval = InterpolateEnvVars(c.Reconciler.Interval)
		c.Reconciler.OrphanDelay = InterpolateEnvVars(c.Reconciler.OrphanDelay)
	}

	if c.Docker != nil {
		c.Docker.Host = InterpolateEnvVars(c.Docker.Host)
		c.Docker.Mode = InterpolateEnvVars(c.Docker.Mode)
	}

	for i := range c.Sources {
		c.Sources[i].Name = InterpolateEnvVars(c.Sources[i].Name)
		if c.Sources[i].FileDiscovery != nil {
			fd := c.Sources[i].FileDiscovery
			for j := range fd.Paths {
				fd.Paths[j] = InterpolateEnvVars(fd.Paths[j])
			}
			fd.Pattern = InterpolateEnvVars(fd.Pattern)
			fd.PollInterval = InterpolateEnvVars(fd.PollInterval)
			fd.WatchMethod = InterpolateEnvVars(fd.WatchMethod)
		}
	}

	for i := range c.Providers {
		p := &c.Providers[i]
		p.Name = InterpolateEnvVars(p.Name)
		p.Type = InterpolateEnvVars(p.Type)
		p.Target = InterpolateEnvVars(p.Target)
		p.RecordType = InterpolateEnvVars(p.RecordType)
		p.Mode = InterpolateEnvVars(p.Mode)
		for j := range p.Domains {
			p.Domains[j] = InterpolateEnvVars(p.Domains[j])
		}
		for j := range p.DomainsRegex {
			p.DomainsRegex[j] = InterpolateEnvVars(p.DomainsRegex[j])
		}
		for j := range p.ExcludeDomains {
			p.ExcludeDomains[j] = InterpolateEnvVars(p.ExcludeDomains[j])
		}
		for j := range p.ExcludeDomainsRegex {
			p.ExcludeDomainsRegex[j] = InterpolateEnvVars(p.ExcludeDomainsRegex[j])
		}
		for k, v := range p.Config {
			p.Config[k] = InterpolateEnvVars(v)
		}
	}
}

// LoadFile reads and parses a YAML configuration file.
// Environment variables in ${VAR} format are interpolated.
func LoadFile(path string) (*FileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg FileConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing YAML config: %w", err)
	}

	// Interpolate environment variables in all string fields
	cfg.interpolateEnvVars()

	return &cfg, nil
}

// ToGlobalConfig converts file config to GlobalConfig, applying defaults.
// Values from file take precedence over defaults; env vars override later.
func (c *FileConfig) ToGlobalConfig() *GlobalConfig {
	cfg := &GlobalConfig{
		LogLevel:          DefaultLogLevel,
		LogFormat:         DefaultLogFormat,
		DryRun:            DefaultDryRun,
		CleanupOrphans:    DefaultCleanupOrphans,
		CleanupOnStop:     DefaultCleanupOnStop,
		OwnershipTracking: DefaultOwnershipTracking,
		AdoptExisting:     DefaultAdoptExisting,
		DefaultTTL:        DefaultTTL,
		ReconcileInterval: DefaultReconcileInterval,
		HealthPort:        DefaultHealthPort,
		DockerHost:        DefaultDockerHost,
		DockerMode:        DefaultDockerMode,
		Source:            DefaultSource,
	}

	if c.Logging != nil {
		if c.Logging.Level != "" {
			cfg.LogLevel = strings.ToLower(c.Logging.Level)
		}
		if c.Logging.Format != "" {
			cfg.LogFormat = strings.ToLower(c.Logging.Format)
		}
	}

	if c.Reconciler != nil {
		if c.Reconciler.DryRun != nil {
			cfg.DryRun = *c.Reconciler.DryRun
		}
		if c.Reconciler.CleanupOrphans != nil {
			cfg.CleanupOrphans = *c.Reconciler.CleanupOrphans
		}
		if c.Reconciler.CleanupOnStop != nil {
			cfg.CleanupOnStop = *c.Reconciler.CleanupOnStop
		}
		if c.Reconciler.OwnershipTracking != nil {
			cfg.OwnershipTracking = *c.Reconciler.OwnershipTracking
		}
		if c.Reconciler.AdoptExisting != nil {
			cfg.AdoptExisting = *c.Reconciler.AdoptExisting
		}
		if c.Reconciler.Interval != "" {
			if interval, err := time.ParseDuration(c.Reconciler.Interval); err == nil && interval >= time.Second {
				cfg.ReconcileInterval = interval
			}
		}
	}

	if c.Docker != nil {
		if c.Docker.Host != "" {
			cfg.DockerHost = c.Docker.Host
		}
		if c.Docker.Mode != "" {
			cfg.DockerMode = strings.ToLower(c.Docker.Mode)
		}
	}

	if c.Server != nil {
		if c.Server.Port > 0 && c.Server.Port <= 65535 {
			cfg.HealthPort = c.Server.Port
		}
	}

	// Source is derived from sources list, keeping first one as primary
	if len(c.Sources) > 0 {
		cfg.Source = c.Sources[0].Name
	}

	return cfg
}

// GetConfigFilePath returns the config file path from env var or flag.
// Returns empty string if no config file is specified.
func GetConfigFilePath() string {
	// Check command-line flag first (would be set before this is called)
	// For now, just check environment variable
	return os.Getenv("DNSWEAVER_CONFIG")
}
