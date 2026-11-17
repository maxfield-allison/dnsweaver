// Package config handles loading and validation of DNSWeaver configuration.
package config

import (
	"log/slog"
	"strings"
	"time"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
)

// loadFromFile loads configuration from a YAML file and converts it to runtime types.
// Applies environment overrides before dependent defaults (such as provider TTL).
// Returns nil values if no file is configured; unreadable files return errors.
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

	// Resolve globals before providers inherit the default TTL.
	global, globalErrs := mergeGlobalConfig(fileCfg.ToGlobalConfig())
	errs = append(errs, globalErrs...)

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
		Name:                        fp.Name,
		TypeName:                    strings.ToLower(fp.Type),
		RecordType:                  provider.RecordType(strings.ToUpper(fp.RecordType)),
		Target:                      fp.Target,
		Mode:                        provider.OperationalMode(fp.Mode),
		Domains:                     fp.Domains,
		DomainsRegex:                fp.DomainsRegex,
		ExcludeDomains:              fp.ExcludeDomains,
		ExcludeDomainsRegex:         fp.ExcludeDomainsRegex,
		AdoptExisting:               fp.AdoptExisting,
		AdoptExistingAllowOverrides: fp.AdoptExistingAllowOverrides,
		ProviderConfig:              make(map[string]string),
	}

	// Validate name
	if cfg.Name == "" {
		errs = append(errs, configErr("providers[].name", "name is required for each provider"))
	}

	// TTL
	if fp.TTL > 0 {
		cfg.TTL = fp.TTL
	} else {
		cfg.TTL = defaultTTL
	}

	// Provider-specific config
	for k, v := range fp.Config {
		// Normalize keys to uppercase for consistency with env var loading
		cfg.ProviderConfig[strings.ToUpper(k)] = v
	}

	// Validate the effective provider after environment overrides, so required
	// fields can be supplied by either configuration channel.
	errs = append(errs, mergeProviderEnvOverrides(cfg)...)
	errs = append(errs, validateProviderRequired(cfg)...)
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

		applySourceEnv(inst)
		cfg.Instances = append(cfg.Instances, inst)
	}

	return cfg
}

// mergeGlobalConfig merges environment variable overrides into a GlobalConfig.
// Environment variables always take precedence over file config.
func mergeGlobalConfig(base *GlobalConfig) (*GlobalConfig, []*ConfigError) {
	if base == nil {
		base = (&FileConfig{}).ToGlobalConfig()
	}
	cfg := *base
	errs := applyGlobalEnv(&cfg)
	return &cfg, errs
}
