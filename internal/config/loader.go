// Package config handles loading and validation of DNSWeaver configuration.
package config

import (
	"log/slog"
	"strings"
	"time"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/source"
)

// loadFromFile loads configuration from a YAML file and converts it to runtime types.
// Returns nil values if no file is configured or file doesn't exist.
func loadFromFile(path string) (*GlobalConfig, []*ProviderInstanceConfig, *SourceConfig, []string) {
	if path == "" {
		return nil, nil, nil, nil
	}

	fileCfg, err := LoadFile(path)
	if err != nil {
		return nil, nil, nil, []string{"config file: " + err.Error()}
	}

	slog.Info("loaded configuration from file", slog.String("path", path))

	var errs []string

	// Convert to runtime types
	global := fileCfg.ToGlobalConfig()

	// Convert providers
	var providers []*ProviderInstanceConfig
	for _, fp := range fileCfg.Providers {
		p, pErrs := convertFileProvider(fp, global.DefaultTTL)
		providers = append(providers, p)
		errs = append(errs, pErrs...)
	}

	// Convert sources
	sources := convertFileSources(fileCfg.Sources)

	return global, providers, sources, errs
}

// convertFileProvider converts a FileProviderConfig to ProviderInstanceConfig.
func convertFileProvider(fp FileProviderConfig, defaultTTL int) (*ProviderInstanceConfig, []string) {
	var errs []string

	cfg := &ProviderInstanceConfig{
		Name:                fp.Name,
		TypeName:            strings.ToLower(fp.Type),
		Domains:             fp.Domains,
		DomainsRegex:        fp.DomainsRegex,
		ExcludeDomains:      fp.ExcludeDomains,
		ExcludeDomainsRegex: fp.ExcludeDomainsRegex,
		ProviderConfig:      make(map[string]string),
	}

	// Validate name
	if cfg.Name == "" {
		errs = append(errs, "provider: name is required")
	}

	// Validate type
	if cfg.TypeName == "" {
		errs = append(errs, "provider "+cfg.Name+": type is required")
	}

	// Record type
	recordTypeStr := strings.ToUpper(fp.RecordType)
	switch recordTypeStr {
	case "", "A":
		cfg.RecordType = provider.RecordTypeA
	case "AAAA":
		cfg.RecordType = provider.RecordTypeAAAA
	case "CNAME":
		cfg.RecordType = provider.RecordTypeCNAME
	default:
		errs = append(errs, "provider "+cfg.Name+": invalid record_type "+fp.RecordType)
	}

	// Target
	cfg.Target = fp.Target
	if cfg.Target == "" {
		errs = append(errs, "provider "+cfg.Name+": target is required")
	}

	// TTL
	if fp.TTL > 0 {
		cfg.TTL = fp.TTL
	} else {
		cfg.TTL = defaultTTL
	}

	// Mode
	if fp.Mode != "" {
		mode, err := provider.ParseOperationalMode(fp.Mode)
		if err != nil {
			errs = append(errs, "provider "+cfg.Name+": "+err.Error())
		} else {
			cfg.Mode = mode
		}
	} else {
		cfg.Mode = provider.ModeManaged
	}

	// Domains validation
	if len(fp.Domains) == 0 && len(fp.DomainsRegex) == 0 {
		errs = append(errs, "provider "+cfg.Name+": domains or domains_regex is required")
	}
	if len(fp.Domains) > 0 && len(fp.DomainsRegex) > 0 {
		errs = append(errs, "provider "+cfg.Name+": cannot set both domains and domains_regex")
	}
	if len(fp.ExcludeDomains) > 0 && len(fp.ExcludeDomainsRegex) > 0 {
		errs = append(errs, "provider "+cfg.Name+": cannot set both exclude_domains and exclude_domains_regex")
	}

	// Provider-specific config
	for k, v := range fp.Config {
		// Normalize keys to uppercase for consistency with env var loading
		cfg.ProviderConfig[strings.ToUpper(k)] = v
	}

	return cfg, errs
}

// convertFileSources converts FileSourceConfig list to SourceConfig.
func convertFileSources(fileSources []FileSourceConfig) *SourceConfig {
	if len(fileSources) == 0 {
		return nil
	}

	cfg := &SourceConfig{
		Names:     make([]string, 0, len(fileSources)),
		Instances: make([]*SourceInstanceConfig, 0, len(fileSources)),
	}

	for _, fs := range fileSources {
		cfg.Names = append(cfg.Names, fs.Name)

		inst := &SourceInstanceConfig{
			Name:          fs.Name,
			FileDiscovery: source.DefaultFileDiscoveryConfig(),
		}

		if fs.FileDiscovery != nil {
			inst.FileDiscovery.FilePaths = fs.FileDiscovery.Paths
			if fs.FileDiscovery.Pattern != "" {
				inst.FileDiscovery.FilePattern = fs.FileDiscovery.Pattern
			}
			if fs.FileDiscovery.PollInterval != "" {
				if interval, err := time.ParseDuration(fs.FileDiscovery.PollInterval); err == nil && interval >= time.Second {
					inst.FileDiscovery.PollInterval = interval
				}
			}
			if fs.FileDiscovery.WatchMethod != "" {
				inst.FileDiscovery.WatchMethod = strings.ToLower(fs.FileDiscovery.WatchMethod)
			}
		}

		cfg.Instances = append(cfg.Instances, inst)
	}

	return cfg
}

// mergeGlobalConfig merges environment variable overrides into a GlobalConfig.
// Environment variables always take precedence over file config.
func mergeGlobalConfig(base *GlobalConfig) (*GlobalConfig, []string) {
	if base == nil {
		// No file config, load everything from env vars
		return loadGlobalConfig()
	}

	var errs []string

	// Start with file values, override with env vars if set
	cfg := *base // Copy the struct

	// Override with env vars if explicitly set
	if v := getEnv("DNSWEAVER_LOG_LEVEL"); v != "" {
		cfg.LogLevel = strings.ToLower(v)
		switch cfg.LogLevel {
		case "debug", "info", "warn", "error":
			// Valid
		default:
			errs = append(errs, "DNSWEAVER_LOG_LEVEL: invalid value (must be debug, info, warn, or error)")
		}
	}

	if v := getEnv("DNSWEAVER_LOG_FORMAT"); v != "" {
		cfg.LogFormat = strings.ToLower(v)
		switch cfg.LogFormat {
		case "json", "text":
			// Valid
		default:
			errs = append(errs, "DNSWEAVER_LOG_FORMAT: invalid value (must be json or text)")
		}
	}

	if v := getEnv("DNSWEAVER_DOCKER_HOST"); v != "" {
		cfg.DockerHost = v
	}

	if v := getEnv("DNSWEAVER_DOCKER_MODE"); v != "" {
		cfg.DockerMode = strings.ToLower(v)
		switch cfg.DockerMode {
		case "auto", "swarm", "standalone":
			// Valid
		default:
			errs = append(errs, "DNSWEAVER_DOCKER_MODE: invalid value (must be auto, swarm, or standalone)")
		}
	}

	if v := getEnv("DNSWEAVER_DRY_RUN"); v != "" {
		cfg.DryRun = parseBool(v, cfg.DryRun)
	}

	if v := getEnv("DNSWEAVER_CLEANUP_ORPHANS"); v != "" {
		cfg.CleanupOrphans = parseBool(v, cfg.CleanupOrphans)
	}

	if v := getEnv("DNSWEAVER_CLEANUP_ON_STOP"); v != "" {
		cfg.CleanupOnStop = parseBool(v, cfg.CleanupOnStop)
	}

	if v := getEnv("DNSWEAVER_OWNERSHIP_TRACKING"); v != "" {
		cfg.OwnershipTracking = parseBool(v, cfg.OwnershipTracking)
	}

	if v := getEnv("DNSWEAVER_ADOPT_EXISTING"); v != "" {
		cfg.AdoptExisting = parseBool(v, cfg.AdoptExisting)
	}

	if v := getEnv("DNSWEAVER_DEFAULT_TTL"); v != "" {
		if ttl, err := parseIntEnv(v); err == nil && ttl >= 1 {
			cfg.DefaultTTL = ttl
		} else {
			errs = append(errs, "DNSWEAVER_DEFAULT_TTL: invalid or negative integer")
		}
	}

	if v := getEnv("DNSWEAVER_RECONCILE_INTERVAL"); v != "" {
		if interval, err := time.ParseDuration(v); err == nil && interval >= time.Second {
			cfg.ReconcileInterval = interval
		} else {
			errs = append(errs, "DNSWEAVER_RECONCILE_INTERVAL: invalid duration")
		}
	}

	if v := getEnv("DNSWEAVER_HEALTH_PORT"); v != "" {
		if port, err := parseIntEnv(v); err == nil && port >= 1 && port <= 65535 {
			cfg.HealthPort = port
		} else {
			errs = append(errs, "DNSWEAVER_HEALTH_PORT: invalid port number")
		}
	}

	if v := getEnv("DNSWEAVER_SOURCE"); v != "" {
		cfg.Source = v
	}

	return &cfg, errs
}

// parseIntEnv parses an integer from string using strconv.
func parseIntEnv(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			if c == '-' && n == 0 {
				continue
			}
			return 0, errInvalidInt
		}
		n = n*10 + int(c-'0')
	}
	if len(s) > 0 && s[0] == '-' {
		n = -n
	}
	return n, nil
}

var errInvalidInt = &ValidationError{Errors: []string{"invalid integer"}}
