package config

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// ProviderInstanceConfig holds configuration for a single provider instance.
// This is created during config loading and passed to the provider registry.
type ProviderInstanceConfig struct {
	// Name is the user-provided instance name (e.g., "internal-dns").
	Name string

	// TypeName is the provider type (e.g., "technitium", "cloudflare").
	TypeName string

	// RecordType is "A", "AAAA", or "CNAME".
	RecordType provider.RecordType

	// Target is the IPv4 (for A), IPv6 (for AAAA), or hostname (for CNAME) target.
	Target string

	// TTL for DNS records.
	TTL int

	// Mode is the operational mode (managed, authoritative, additive).
	// Defaults to "managed" if not set.
	Mode provider.OperationalMode

	// Domain matching patterns
	Domains             []string // Glob patterns (default)
	DomainsRegex        []string // Regex patterns (opt-in)
	ExcludeDomains      []string // Glob exclude patterns
	ExcludeDomainsRegex []string // Regex exclude patterns

	// ProviderConfig holds provider-specific settings.
	// Keys are setting names (e.g., "URL", "TOKEN", "ZONE").
	ProviderConfig map[string]string
}

// ToProviderConfig converts this config to the provider package's config type.
func (c *ProviderInstanceConfig) ToProviderConfig() provider.ProviderInstanceConfig {
	return provider.ProviderInstanceConfig{
		Name:                c.Name,
		TypeName:            c.TypeName,
		RecordType:          c.RecordType,
		Target:              c.Target,
		TTL:                 c.TTL,
		Mode:                c.Mode,
		Domains:             c.Domains,
		DomainsRegex:        c.DomainsRegex,
		ExcludeDomains:      c.ExcludeDomains,
		ExcludeDomainsRegex: c.ExcludeDomainsRegex,
		ProviderConfig:      c.ProviderConfig,
	}
}

// parseInstances parses the DNSWEAVER_INSTANCES environment variable.
// For backward compatibility, DNSWEAVER_PROVIDERS is also accepted but deprecated.
// Returns the list of instance names in order.
func parseInstances() []string {
	// Prefer DNSWEAVER_INSTANCES, fall back to deprecated DNSWEAVER_PROVIDERS
	instancesStr := getEnv("DNSWEAVER_INSTANCES")
	if instancesStr == "" {
		instancesStr = getEnv("DNSWEAVER_PROVIDERS")
		if instancesStr != "" {
			slog.Warn("DNSWEAVER_PROVIDERS is deprecated, use DNSWEAVER_INSTANCES instead")
		}
	}
	if instancesStr == "" {
		return nil
	}

	var instances []string
	for _, p := range strings.Split(instancesStr, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			instances = append(instances, p)
		}
	}
	return instances
}

// loadInstanceConfig loads configuration for a single provider instance.
// It reads all DNSWEAVER_{INSTANCE_NAME}_* environment variables.
func loadInstanceConfig(instanceName string, defaultTTL int) (*ProviderInstanceConfig, []string) {
	var errs []string
	prefix := envPrefix(instanceName)

	cfg := &ProviderInstanceConfig{
		Name:           instanceName,
		ProviderConfig: make(map[string]string),
	}

	// TYPE is required
	cfg.TypeName = strings.ToLower(getEnv(prefix + "TYPE"))
	if cfg.TypeName == "" {
		errs = append(errs, fmt.Sprintf("%sTYPE: required but not set", prefix))
	}

	// RECORD_TYPE (default: A)
	recordTypeStr := strings.ToUpper(getEnv(prefix + "RECORD_TYPE"))
	switch recordTypeStr {
	case "", "A":
		cfg.RecordType = provider.RecordTypeA
	case "AAAA":
		cfg.RecordType = provider.RecordTypeAAAA
	case "CNAME":
		cfg.RecordType = provider.RecordTypeCNAME
	default:
		errs = append(errs, fmt.Sprintf("%sRECORD_TYPE: invalid value %q (must be A, AAAA, or CNAME)", prefix, recordTypeStr))
	}

	// TARGET is required
	cfg.Target = getEnv(prefix + "TARGET")
	if cfg.Target == "" {
		errs = append(errs, fmt.Sprintf("%sTARGET: required but not set", prefix))
	}

	// TTL (optional, defaults to global default)
	if ttlStr := getEnv(prefix + "TTL"); ttlStr != "" {
		ttl, err := strconv.Atoi(ttlStr)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%sTTL: invalid integer %q", prefix, ttlStr))
		} else if ttl < 1 {
			errs = append(errs, fmt.Sprintf("%sTTL: must be at least 1", prefix))
		} else {
			cfg.TTL = ttl
		}
	} else {
		cfg.TTL = defaultTTL
	}

	// MODE (optional, defaults to "managed")
	if modeStr := getEnv(prefix + "MODE"); modeStr != "" {
		mode, err := provider.ParseOperationalMode(modeStr)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%sMODE: %s", prefix, err.Error()))
		} else {
			cfg.Mode = mode
		}
	} else {
		cfg.Mode = provider.ModeManaged
	}

	// Domain patterns - either DOMAINS or DOMAINS_REGEX, not both
	domainsStr := getEnv(prefix + "DOMAINS")
	domainsRegexStr := getEnv(prefix + "DOMAINS_REGEX")

	if domainsStr != "" && domainsRegexStr != "" {
		errs = append(errs, fmt.Sprintf("%s: cannot set both DOMAINS and DOMAINS_REGEX", prefix[:len(prefix)-1]))
	} else if domainsStr == "" && domainsRegexStr == "" {
		errs = append(errs, fmt.Sprintf("%sDOMAINS: required but not set", prefix))
	} else if domainsStr != "" {
		cfg.Domains = splitPatterns(domainsStr)
	} else {
		cfg.DomainsRegex = splitPatterns(domainsRegexStr)
	}

	// Exclude patterns - either EXCLUDE_DOMAINS or EXCLUDE_DOMAINS_REGEX
	excludeDomainsStr := getEnv(prefix + "EXCLUDE_DOMAINS")
	excludeDomainsRegexStr := getEnv(prefix + "EXCLUDE_DOMAINS_REGEX")

	if excludeDomainsStr != "" && excludeDomainsRegexStr != "" {
		errs = append(errs, fmt.Sprintf("%s: cannot set both EXCLUDE_DOMAINS and EXCLUDE_DOMAINS_REGEX", prefix[:len(prefix)-1]))
	} else if excludeDomainsStr != "" {
		cfg.ExcludeDomains = splitPatterns(excludeDomainsStr)
	} else if excludeDomainsRegexStr != "" {
		cfg.ExcludeDomainsRegex = splitPatterns(excludeDomainsRegexStr)
	}

	// Load provider-specific config using shared field definitions
	// Secrets support the _FILE suffix for Docker secrets
	for _, field := range providerConfigFields {
		var value string
		if field.isSecret {
			value = getEnvWithFileFallback(prefix, field.name)
		} else {
			value = getEnv(prefix + field.name)
		}
		if value != "" {
			cfg.ProviderConfig[field.name] = value
		}
	}

	return cfg, errs
}

// providerConfigFields defines all provider-specific configuration fields.
// This is shared between env var loading and file config merging.
// Fields marked as secrets support the _FILE suffix pattern for Docker secrets.
var providerConfigFields = []struct {
	name     string
	isSecret bool
}{
	{"URL", false},
	{"TOKEN", true},
	{"ZONE", false},
	{"ZONE_ID", false},
	{"API_KEY", true},
	{"API_EMAIL", false},
	{"PROXIED", false},              // Cloudflare-specific
	{"AUTH_HEADER", false},          // Webhook-specific
	{"AUTH_TOKEN", true},            // Webhook-specific
	{"TIMEOUT", false},              // Webhook-specific
	{"RETRIES", false},              // Webhook-specific
	{"RETRY_DELAY", false},          // Webhook-specific
	{"HOST_FILE", false},            // dnsmasq-specific
	{"BACKUP", false},               // dnsmasq-specific
	{"INCLUDE_MARKER", false},       // dnsmasq-specific
	{"RELOAD_COMMAND", false},       // dnsmasq-specific
	{"MODE", false},                 // Pi-hole specific (api/file)
	{"PASSWORD", true},              // Pi-hole specific
	{"INSECURE_SKIP_VERIFY", false}, // TLS certificate verification skip
}

// mergeProviderEnvOverrides applies environment variable overrides to a
// file-based provider configuration. This allows users to:
//  1. Define most config in YAML for readability
//  2. Override specific values (especially secrets) via env vars
//  3. Use Docker secrets with the _FILE suffix pattern
//
// Environment variables use the pattern: DNSWEAVER_{PROVIDER_NAME}_{FIELD}
// For secrets, DNSWEAVER_{PROVIDER_NAME}_{FIELD}_FILE is also checked.
//
// Any env var that is set will override the corresponding YAML value.
func mergeProviderEnvOverrides(cfg *ProviderInstanceConfig) {
	prefix := envPrefix(cfg.Name)

	// Ensure ProviderConfig map exists
	if cfg.ProviderConfig == nil {
		cfg.ProviderConfig = make(map[string]string)
	}

	// Check for provider-specific config field overrides
	for _, field := range providerConfigFields {
		var value string
		if field.isSecret {
			value = getEnvWithFileFallback(prefix, field.name)
		} else {
			value = getEnv(prefix + field.name)
		}
		// Only override if env var is explicitly set
		if value != "" {
			slog.Debug("env override applied to provider config",
				slog.String("provider", cfg.Name),
				slog.String("field", field.name),
			)
			cfg.ProviderConfig[field.name] = value
		}
	}

	// Also check for top-level provider settings that might be overridden
	// TARGET override
	if target := getEnv(prefix + "TARGET"); target != "" {
		slog.Debug("env override applied to provider target",
			slog.String("provider", cfg.Name),
			slog.String("target", target),
		)
		cfg.Target = target
	}

	// TTL override
	if ttlStr := getEnv(prefix + "TTL"); ttlStr != "" {
		if ttl, err := strconv.Atoi(ttlStr); err == nil && ttl >= 1 {
			slog.Debug("env override applied to provider TTL",
				slog.String("provider", cfg.Name),
				slog.Int("ttl", ttl),
			)
			cfg.TTL = ttl
		}
	}

	// MODE override
	if modeStr := getEnv(prefix + "MODE"); modeStr != "" {
		if mode, err := provider.ParseOperationalMode(modeStr); err == nil {
			slog.Debug("env override applied to provider mode",
				slog.String("provider", cfg.Name),
				slog.String("mode", modeStr),
			)
			cfg.Mode = mode
		}
	}
}

// splitPatterns splits a comma-separated pattern string into individual patterns.
// Whitespace around patterns is trimmed.
func splitPatterns(s string) []string {
	var patterns []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			patterns = append(patterns, p)
		}
	}
	return patterns
}
