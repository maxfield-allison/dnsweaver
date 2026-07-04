// Package config handles loading and validation of DNSWeaver configuration.
package config

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
)

// loadFromFile loads configuration from a YAML file and converts it to runtime types.
// Returns nil values if no file is configured or file doesn't exist.
func loadFromFile(path string) (*GlobalConfig, []*ProviderInstanceConfig, *SourceConfig, []*ConfigError) {
	if path == "" {
		return nil, nil, nil, nil
	}

	fileCfg, err := LoadFile(path)
	if err != nil {
		return nil, nil, nil, []*ConfigError{configErr("config_file", err.Error())}
	}

	slog.Info("loaded configuration from file", slog.String("path", path))

	var errs []*ConfigError

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
func convertFileProvider(fp FileProviderConfig, defaultTTL int) (*ProviderInstanceConfig, []*ConfigError) {
	var errs []*ConfigError

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
		errs = append(errs, configErr("providers[].name", "name is required for each provider"))
	}

	// Validate type
	if cfg.TypeName == "" {
		errs = append(errs, configErrFull("providers["+cfg.Name+"].type", "type is required", fmt.Sprintf("Provider %q needs a type", cfg.Name), "type: technitium"))
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
		errs = append(errs, configErrFull("providers["+cfg.Name+"].record_type", fmt.Sprintf("invalid value %q", fp.RecordType), "Must be one of: A, AAAA, CNAME", "record_type: A"))
	}

	// Target
	cfg.Target = fp.Target
	if cfg.Target == "" {
		errs = append(errs, configErrFull("providers["+cfg.Name+"].target", "target is required", fmt.Sprintf("Provider %q needs a target IP or hostname", cfg.Name), "target: 10.0.0.1"))
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
			errs = append(errs, configErr("providers["+cfg.Name+"].mode", err.Error()))
		} else {
			cfg.Mode = mode
		}
	} else {
		cfg.Mode = provider.ModeManaged
	}

	// Domains validation
	if len(fp.Domains) == 0 && len(fp.DomainsRegex) == 0 {
		errs = append(errs, configErrFull("providers["+cfg.Name+"].domains", "domains or domains_regex is required", "Specify which domains this provider should manage", "domains: [\"*.example.com\"]"))
	}
	if len(fp.Domains) > 0 && len(fp.DomainsRegex) > 0 {
		errs = append(errs, configErrHelp("providers["+cfg.Name+"]", "cannot set both domains and domains_regex", "Use either glob patterns or regex patterns, not both"))
	}
	if len(fp.ExcludeDomains) > 0 && len(fp.ExcludeDomainsRegex) > 0 {
		errs = append(errs, configErrHelp("providers["+cfg.Name+"]", "cannot set both exclude_domains and exclude_domains_regex", "Use either glob patterns or regex patterns for exclusions, not both"))
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
func mergeGlobalConfig(base *GlobalConfig) (*GlobalConfig, []*ConfigError) {
	if base == nil {
		// No file config, load everything from env vars
		return loadGlobalConfig()
	}

	var errs []*ConfigError

	// Start with file values, override with env vars if set
	cfg := *base // Copy the struct

	// Override with env vars if explicitly set
	if v := getEnv("DNSWEAVER_LOG_LEVEL"); v != "" {
		cfg.LogLevel = strings.ToLower(v)
		switch cfg.LogLevel {
		case "debug", "info", "warn", "error":
			// Valid
		default:
			errs = append(errs, configErrFull("DNSWEAVER_LOG_LEVEL", fmt.Sprintf("invalid value %q", v), "Must be one of: debug, info, warn, error", "DNSWEAVER_LOG_LEVEL=info"))
		}
	}

	if v := getEnv("DNSWEAVER_LOG_FORMAT"); v != "" {
		cfg.LogFormat = strings.ToLower(v)
		switch cfg.LogFormat {
		case "json", "text":
			// Valid
		default:
			errs = append(errs, configErrFull("DNSWEAVER_LOG_FORMAT", fmt.Sprintf("invalid value %q", v), "Must be one of: json, text", "DNSWEAVER_LOG_FORMAT=json"))
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
			errs = append(errs, configErrFull("DNSWEAVER_DOCKER_MODE", fmt.Sprintf("invalid value %q", v), "Must be one of: auto, swarm, standalone", "DNSWEAVER_DOCKER_MODE=auto"))
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
			errs = append(errs, configErrFull("DNSWEAVER_DEFAULT_TTL", fmt.Sprintf("invalid value %q", v), "Must be a positive integer (seconds)", "DNSWEAVER_DEFAULT_TTL=300"))
		}
	}

	if v := getEnv("DNSWEAVER_RECONCILE_INTERVAL"); v != "" {
		if interval, err := time.ParseDuration(v); err == nil && interval >= time.Second {
			cfg.ReconcileInterval = interval
		} else {
			errs = append(errs, configErrFull("DNSWEAVER_RECONCILE_INTERVAL", fmt.Sprintf("invalid value %q", v), "Use Go duration format, minimum 1s", "DNSWEAVER_RECONCILE_INTERVAL=60s"))
		}
	}

	if v := getEnv("DNSWEAVER_HEALTH_PORT"); v != "" {
		if port, err := parseIntEnv(v); err == nil && port >= 1 && port <= 65535 {
			cfg.HealthPort = port
		} else {
			errs = append(errs, configErrFull("DNSWEAVER_HEALTH_PORT", fmt.Sprintf("invalid value %q", v), "Must be a valid TCP port (1-65535)", "DNSWEAVER_HEALTH_PORT=8080"))
		}
	}

	// Note: DNSWEAVER_SOURCE (singular) is deprecated. Source list is
	// determined by parseSources() which reads DNSWEAVER_SOURCES and
	// falls back to DNSWEAVER_SOURCE with a deprecation warning.

	// Override instance ID if set in env
	if v := getEnv("DNSWEAVER_INSTANCE_ID"); v != "" {
		if err := validateInstanceID(v); err != nil {
			errs = append(errs, configErrFull("DNSWEAVER_INSTANCE_ID", err.Error(), "Must be 1-63 alphanumeric characters with hyphens, underscores, or dots", "DNSWEAVER_INSTANCE_ID=prod-01"))
		} else {
			cfg.InstanceID = v
		}
	}

	// Override platform if set in env
	if v := getEnv("DNSWEAVER_PLATFORM"); v != "" {
		cfg.Platform = normalizePlatform(v)
		switch cfg.Platform {
		case "docker", "kubernetes", "both", "none":
			// Valid
		default:
			errs = append(errs, configErrFull("DNSWEAVER_PLATFORM", fmt.Sprintf("invalid value %q", v), "Must be one of: docker, kubernetes, both, none", "DNSWEAVER_PLATFORM=docker"))
		}
	}

	// Override Kubernetes settings if set in env
	if v := getEnv("DNSWEAVER_K8S_KUBECONFIG"); v != "" {
		cfg.K8sKubeconfig = v
	}
	if v := getEnv("DNSWEAVER_K8S_NAMESPACES"); v != "" {
		cfg.K8sNamespaces = v
	}
	if v := getEnv("DNSWEAVER_K8S_LABEL_SELECTOR"); v != "" {
		cfg.K8sLabelSelector = v
	}
	if v := getEnv("DNSWEAVER_K8S_ANNOTATION_FILTER"); v != "" {
		cfg.K8sAnnotationFilter = v
	}
	if v := getEnv("DNSWEAVER_K8S_WATCH_INGRESS"); v != "" {
		cfg.K8sWatchIngress = parseBool(v, cfg.K8sWatchIngress)
	}
	if v := getEnv("DNSWEAVER_K8S_WATCH_INGRESSROUTE"); v != "" {
		cfg.K8sWatchIngressRoute = parseBool(v, cfg.K8sWatchIngressRoute)
	}
	if v := getEnv("DNSWEAVER_K8S_WATCH_HTTPROUTE"); v != "" {
		cfg.K8sWatchHTTPRoute = parseBool(v, cfg.K8sWatchHTTPRoute)
	}
	if v := getEnv("DNSWEAVER_K8S_WATCH_SERVICES"); v != "" {
		cfg.K8sWatchServices = parseBool(v, cfg.K8sWatchServices)
	}

	return &cfg, errs
}

// parseIntEnv parses an integer from string using strconv.
func parseIntEnv(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("invalid integer: %w", err)
	}
	return n, nil
}
