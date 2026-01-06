package config

import (
	"fmt"
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

	// RecordType is "A" or "CNAME".
	RecordType provider.RecordType

	// Target is the IP (for A) or hostname (for CNAME) target.
	Target string

	// TTL for DNS records.
	TTL int

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
		Domains:             c.Domains,
		DomainsRegex:        c.DomainsRegex,
		ExcludeDomains:      c.ExcludeDomains,
		ExcludeDomainsRegex: c.ExcludeDomainsRegex,
		ProviderConfig:      c.ProviderConfig,
	}
}

// parseProviders parses the DNSWEAVER_PROVIDERS environment variable.
// Returns the list of provider instance names in order.
func parseProviders() []string {
	providersStr := getEnv("DNSWEAVER_PROVIDERS")
	if providersStr == "" {
		return nil
	}

	var providers []string
	for _, p := range strings.Split(providersStr, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			providers = append(providers, p)
		}
	}
	return providers
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
	case "CNAME":
		cfg.RecordType = provider.RecordTypeCNAME
	default:
		errs = append(errs, fmt.Sprintf("%sRECORD_TYPE: invalid value %q (must be A or CNAME)", prefix, recordTypeStr))
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

	// Load provider-specific config
	// Common fields that most providers need (with _FILE support for secrets)
	providerFields := []struct {
		name      string
		isSecret  bool
		required  bool // Note: providers validate their own required fields
	}{
		{"URL", false, false},
		{"TOKEN", true, false},
		{"ZONE", false, false},
		{"ZONE_ID", false, false},
		{"API_KEY", true, false},
		{"API_EMAIL", false, false},
	}

	for _, field := range providerFields {
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
