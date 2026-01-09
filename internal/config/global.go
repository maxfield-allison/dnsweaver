package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Global configuration defaults.
const (
	DefaultLogLevel           = "info"
	DefaultLogFormat          = "json"
	DefaultDryRun             = false
	DefaultCleanupOrphans     = true
	DefaultOwnershipTracking  = true
	DefaultAdoptExisting      = false
	DefaultTTL                = 300
	DefaultReconcileInterval  = 60 * time.Second
	DefaultHealthPort         = 8080
	DefaultDockerHost         = "unix:///var/run/docker.sock"
	DefaultDockerMode         = "auto"
	DefaultSource             = "traefik"
)

// GlobalConfig holds application-wide settings.
// These are parsed from DNSWEAVER_* environment variables.
type GlobalConfig struct {
	// Logging configuration
	LogLevel  string // debug, info, warn, error
	LogFormat string // json, text

	// Behavior
	DryRun            bool          // If true, don't make actual DNS changes
	CleanupOrphans    bool          // If true, delete DNS records for removed workloads
	OwnershipTracking bool          // If true, use TXT records to track record ownership
	AdoptExisting     bool          // If true, adopt existing DNS records by creating ownership TXT records
	DefaultTTL        int           // Default TTL for records if not specified per-provider
	ReconcileInterval time.Duration // How often to reconcile DNS records
	HealthPort        int           // Port for health/metrics endpoints

	// Docker connection
	DockerHost string // Docker socket path or TCP URL
	DockerMode string // auto, swarm, standalone

	// Source
	Source string // traefik, labels, or custom source name
}

// loadGlobalConfig loads global configuration from environment variables.
// Returns a list of validation errors (may be empty).
func loadGlobalConfig() (*GlobalConfig, []string) {
	var errs []string

	cfg := &GlobalConfig{
		LogLevel:   getEnv("DNSWEAVER_LOG_LEVEL"),
		LogFormat:  getEnv("DNSWEAVER_LOG_FORMAT"),
		DockerHost: getEnv("DNSWEAVER_DOCKER_HOST"),
		DockerMode: getEnv("DNSWEAVER_DOCKER_MODE"),
		Source:     getEnv("DNSWEAVER_SOURCE"),
	}

	// Apply defaults for empty values
	if cfg.LogLevel == "" {
		cfg.LogLevel = DefaultLogLevel
	}
	if cfg.LogFormat == "" {
		cfg.LogFormat = DefaultLogFormat
	}
	if cfg.DockerHost == "" {
		cfg.DockerHost = DefaultDockerHost
	}
	if cfg.DockerMode == "" {
		cfg.DockerMode = DefaultDockerMode
	}
	if cfg.Source == "" {
		cfg.Source = DefaultSource
	}

	// Validate log level
	cfg.LogLevel = strings.ToLower(cfg.LogLevel)
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
		// Valid
	default:
		errs = append(errs, fmt.Sprintf("DNSWEAVER_LOG_LEVEL: invalid value %q (must be debug, info, warn, or error)", cfg.LogLevel))
	}

	// Validate log format
	cfg.LogFormat = strings.ToLower(cfg.LogFormat)
	switch cfg.LogFormat {
	case "json", "text":
		// Valid
	default:
		errs = append(errs, fmt.Sprintf("DNSWEAVER_LOG_FORMAT: invalid value %q (must be json or text)", cfg.LogFormat))
	}

	// Validate Docker mode
	cfg.DockerMode = strings.ToLower(cfg.DockerMode)
	switch cfg.DockerMode {
	case "auto", "swarm", "standalone":
		// Valid
	default:
		errs = append(errs, fmt.Sprintf("DNSWEAVER_DOCKER_MODE: invalid value %q (must be auto, swarm, or standalone)", cfg.DockerMode))
	}

	// Parse DRY_RUN
	if dryRunStr := getEnv("DNSWEAVER_DRY_RUN"); dryRunStr != "" {
		cfg.DryRun = parseBool(dryRunStr, DefaultDryRun)
	} else {
		cfg.DryRun = DefaultDryRun
	}

	// Parse CLEANUP_ORPHANS
	if cleanupStr := getEnv("DNSWEAVER_CLEANUP_ORPHANS"); cleanupStr != "" {
		cfg.CleanupOrphans = parseBool(cleanupStr, DefaultCleanupOrphans)
	} else {
		cfg.CleanupOrphans = DefaultCleanupOrphans
	}

	// Parse OWNERSHIP_TRACKING
	if ownershipStr := getEnv("DNSWEAVER_OWNERSHIP_TRACKING"); ownershipStr != "" {
		cfg.OwnershipTracking = parseBool(ownershipStr, DefaultOwnershipTracking)
	} else {
		cfg.OwnershipTracking = DefaultOwnershipTracking
	}

	// Parse ADOPT_EXISTING
	if adoptStr := getEnv("DNSWEAVER_ADOPT_EXISTING"); adoptStr != "" {
		cfg.AdoptExisting = parseBool(adoptStr, DefaultAdoptExisting)
	} else {
		cfg.AdoptExisting = DefaultAdoptExisting
	}

	// Parse DEFAULT_TTL
	if ttlStr := getEnv("DNSWEAVER_DEFAULT_TTL"); ttlStr != "" {
		ttl, err := strconv.Atoi(ttlStr)
		if err != nil {
			errs = append(errs, fmt.Sprintf("DNSWEAVER_DEFAULT_TTL: invalid integer %q", ttlStr))
		} else if ttl < 1 {
			errs = append(errs, "DNSWEAVER_DEFAULT_TTL: must be at least 1")
		} else {
			cfg.DefaultTTL = ttl
		}
	} else {
		cfg.DefaultTTL = DefaultTTL
	}

	// Parse RECONCILE_INTERVAL (supports Go duration format: 60s, 5m, etc.)
	if intervalStr := getEnv("DNSWEAVER_RECONCILE_INTERVAL"); intervalStr != "" {
		interval, err := time.ParseDuration(intervalStr)
		if err != nil {
			errs = append(errs, fmt.Sprintf("DNSWEAVER_RECONCILE_INTERVAL: invalid duration %q (use format like 60s, 5m)", intervalStr))
		} else if interval < time.Second {
			errs = append(errs, "DNSWEAVER_RECONCILE_INTERVAL: must be at least 1s")
		} else {
			cfg.ReconcileInterval = interval
		}
	} else {
		cfg.ReconcileInterval = DefaultReconcileInterval
	}

	// Parse HEALTH_PORT
	if portStr := getEnv("DNSWEAVER_HEALTH_PORT"); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			errs = append(errs, fmt.Sprintf("DNSWEAVER_HEALTH_PORT: invalid integer %q", portStr))
		} else if port < 1 || port > 65535 {
			errs = append(errs, fmt.Sprintf("DNSWEAVER_HEALTH_PORT: must be between 1 and 65535, got %d", port))
		} else {
			cfg.HealthPort = port
		}
	} else {
		cfg.HealthPort = DefaultHealthPort
	}

	return cfg, errs
}
