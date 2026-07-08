// Package config handles loading and validation of DNSWeaver configuration.
package config

import (
	"fmt"
	"log/slog"
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

	// Platform selection: docker, kubernetes, or both (default: docker)
	Platform string `yaml:"platform,omitempty"`

	// Multi-instance coordination (preferred location since v0.10.0)
	// Also accepted under reconciler.instance_id for backward compatibility.
	InstanceID string `yaml:"instance_id,omitempty"`

	// Docker connection settings
	Docker *FileDockerConfig `yaml:"docker,omitempty"`

	// Kubernetes watcher settings
	Kubernetes *FileKubernetesConfig `yaml:"kubernetes,omitempty"`

	// Hostname sources
	Sources []FileSourceConfig `yaml:"sources,omitempty"`

	// DNS providers
	Providers []FileProviderConfig `yaml:"providers,omitempty"`

	// Health and metrics server
	Server *FileServerConfig `yaml:"server,omitempty"`
}

// FileLoggingConfig holds logging settings.
type FileLoggingConfig struct {
	Level    string              `yaml:"level,omitempty"`    // debug, info, warn, error
	Format   string              `yaml:"format,omitempty"`   // json, text
	File     string              `yaml:"file,omitempty"`     // Path to log file; empty = stdout
	Rotation *FileRotationConfig `yaml:"rotation,omitempty"` // Log rotation settings
}

// FileRotationConfig holds log file rotation settings.
type FileRotationConfig struct {
	MaxSize    *int  `yaml:"max_size,omitempty"`    // Max size in MB before rotation
	MaxBackups *int  `yaml:"max_backups,omitempty"` // Number of old log files to keep
	MaxAge     *int  `yaml:"max_age,omitempty"`     // Days to retain old log files
	Compress   *bool `yaml:"compress,omitempty"`    // Compress rotated log files
}

// FileReconcilerConfig holds reconciliation settings.
type FileReconcilerConfig struct {
	Interval          string `yaml:"interval,omitempty"`           // Go duration format (e.g., "60s", "5m")
	ShutdownTimeout   string `yaml:"shutdown_timeout,omitempty"`   // Max time to wait for in-flight ops during shutdown
	DryRun            *bool  `yaml:"dry_run,omitempty"`            // Pointer to distinguish unset from false
	CleanupOrphans    *bool  `yaml:"cleanup_orphans,omitempty"`    // Delete records for removed workloads
	CleanupOnStop     *bool  `yaml:"cleanup_on_stop,omitempty"`    // Delete records when containers stop
	OwnershipTracking *bool  `yaml:"ownership_tracking,omitempty"` // Use TXT records for ownership
	AdoptExisting     *bool  `yaml:"adopt_existing,omitempty"`     // Adopt pre-existing DNS records
	InstanceID        string `yaml:"instance_id,omitempty"`        // Unique ID for multi-instance coordination
}

// FileDockerConfig holds Docker connection settings.
type FileDockerConfig struct {
	Host           string `yaml:"host,omitempty"`            // unix:///var/run/docker.sock or tcp://...
	Mode           string `yaml:"mode,omitempty"`            // auto, swarm, standalone
	ConnectTimeout string `yaml:"connect_timeout,omitempty"` // Max time to retry the initial connect before failing hard (e.g. 30s); "0" fails immediately
}

// FileKubernetesConfig holds Kubernetes watcher settings.
type FileKubernetesConfig struct {
	Kubeconfig        string   `yaml:"kubeconfig,omitempty"`         // Path to kubeconfig; empty = in-cluster
	Namespaces        []string `yaml:"namespaces,omitempty"`         // Namespaces to watch; empty = all
	WatchIngress      *bool    `yaml:"watch_ingress,omitempty"`      // Watch Ingress resources (default: true)
	WatchIngressRoute *bool    `yaml:"watch_ingressroute,omitempty"` // Watch IngressRoute CRDs (default: true)
	WatchHTTPRoute    *bool    `yaml:"watch_httproute,omitempty"`    // Watch HTTPRoute CRDs (default: true)
	WatchServices     *bool    `yaml:"watch_services,omitempty"`     // Watch Service resources (default: false)
	LabelSelector     string   `yaml:"label_selector,omitempty"`     // Label selector filter
	AnnotationFilter  string   `yaml:"annotation_filter,omitempty"`  // Annotation key=value filter
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
		c.Logging.File = InterpolateEnvVars(c.Logging.File)
	}

	if c.Reconciler != nil {
		c.Reconciler.Interval = InterpolateEnvVars(c.Reconciler.Interval)
		c.Reconciler.ShutdownTimeout = InterpolateEnvVars(c.Reconciler.ShutdownTimeout)
		c.Reconciler.InstanceID = InterpolateEnvVars(c.Reconciler.InstanceID)
	}

	c.Platform = InterpolateEnvVars(c.Platform)
	c.InstanceID = InterpolateEnvVars(c.InstanceID)

	if c.Docker != nil {
		c.Docker.Host = InterpolateEnvVars(c.Docker.Host)
		c.Docker.Mode = InterpolateEnvVars(c.Docker.Mode)
	}

	if c.Kubernetes != nil {
		c.Kubernetes.Kubeconfig = InterpolateEnvVars(c.Kubernetes.Kubeconfig)
		for i := range c.Kubernetes.Namespaces {
			c.Kubernetes.Namespaces[i] = InterpolateEnvVars(c.Kubernetes.Namespaces[i])
		}
		c.Kubernetes.LabelSelector = InterpolateEnvVars(c.Kubernetes.LabelSelector)
		c.Kubernetes.AnnotationFilter = InterpolateEnvVars(c.Kubernetes.AnnotationFilter)
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
		LogLevel:             DefaultLogLevel,
		LogFormat:            DefaultLogFormat,
		LogMaxSize:           DefaultLogMaxSize,
		LogMaxBackups:        DefaultLogMaxBackups,
		LogMaxAge:            DefaultLogMaxAge,
		LogCompress:          DefaultLogCompress,
		DryRun:               DefaultDryRun,
		CleanupOrphans:       DefaultCleanupOrphans,
		CleanupOnStop:        DefaultCleanupOnStop,
		OwnershipTracking:    DefaultOwnershipTracking,
		AdoptExisting:        DefaultAdoptExisting,
		DefaultTTL:           DefaultTTL,
		ReconcileInterval:    DefaultReconcileInterval,
		ShutdownTimeout:      DefaultShutdownTimeout,
		HealthPort:           DefaultHealthPort,
		Platform:             DefaultPlatform,
		DockerHost:           DefaultDockerHost,
		DockerMode:           DefaultDockerMode,
		DockerConnectTimeout: DefaultDockerConnectTimeout,
		K8sWatchIngress:      true,
		K8sWatchIngressRoute: true,
		K8sWatchHTTPRoute:    true,
		K8sWatchServices:     false,
		Source:               DefaultSource,
	}

	if c.Logging != nil {
		if c.Logging.Level != "" {
			cfg.LogLevel = strings.ToLower(c.Logging.Level)
		}
		if c.Logging.Format != "" {
			cfg.LogFormat = strings.ToLower(c.Logging.Format)
		}
		if c.Logging.File != "" {
			cfg.LogFile = c.Logging.File
		}
		if c.Logging.Rotation != nil {
			if c.Logging.Rotation.MaxSize != nil {
				cfg.LogMaxSize = *c.Logging.Rotation.MaxSize
			}
			if c.Logging.Rotation.MaxBackups != nil {
				cfg.LogMaxBackups = *c.Logging.Rotation.MaxBackups
			}
			if c.Logging.Rotation.MaxAge != nil {
				cfg.LogMaxAge = *c.Logging.Rotation.MaxAge
			}
			if c.Logging.Rotation.Compress != nil {
				cfg.LogCompress = *c.Logging.Rotation.Compress
			}
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
		if c.Reconciler.ShutdownTimeout != "" {
			if timeout, err := time.ParseDuration(c.Reconciler.ShutdownTimeout); err == nil && timeout >= time.Second {
				cfg.ShutdownTimeout = timeout
			}
		}
		if c.Reconciler.InstanceID != "" {
			cfg.InstanceID = c.Reconciler.InstanceID
			slog.Warn("reconciler.instance_id is deprecated in YAML config, use top-level instance_id instead")
		}
	}

	// Top-level instance_id takes precedence over deprecated reconciler.instance_id
	if c.InstanceID != "" {
		cfg.InstanceID = c.InstanceID
	}

	if c.Docker != nil {
		if c.Docker.Host != "" {
			cfg.DockerHost = c.Docker.Host
		}
		if c.Docker.Mode != "" {
			cfg.DockerMode = strings.ToLower(c.Docker.Mode)
		}
		if c.Docker.ConnectTimeout != "" {
			if timeout, err := time.ParseDuration(c.Docker.ConnectTimeout); err == nil && timeout >= 0 {
				cfg.DockerConnectTimeout = timeout
			}
		}
	}

	if c.Platform != "" {
		cfg.Platform = strings.ToLower(c.Platform)
	}

	if c.Kubernetes != nil {
		if c.Kubernetes.Kubeconfig != "" {
			cfg.K8sKubeconfig = c.Kubernetes.Kubeconfig
		}
		if len(c.Kubernetes.Namespaces) > 0 {
			cfg.K8sNamespaces = strings.Join(c.Kubernetes.Namespaces, ",")
		}
		if c.Kubernetes.WatchIngress != nil {
			cfg.K8sWatchIngress = *c.Kubernetes.WatchIngress
		}
		if c.Kubernetes.WatchIngressRoute != nil {
			cfg.K8sWatchIngressRoute = *c.Kubernetes.WatchIngressRoute
		}
		if c.Kubernetes.WatchHTTPRoute != nil {
			cfg.K8sWatchHTTPRoute = *c.Kubernetes.WatchHTTPRoute
		}
		if c.Kubernetes.WatchServices != nil {
			cfg.K8sWatchServices = *c.Kubernetes.WatchServices
		}
		if c.Kubernetes.LabelSelector != "" {
			cfg.K8sLabelSelector = c.Kubernetes.LabelSelector
		}
		if c.Kubernetes.AnnotationFilter != "" {
			cfg.K8sAnnotationFilter = c.Kubernetes.AnnotationFilter
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
