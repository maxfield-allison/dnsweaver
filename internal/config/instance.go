package config

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
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

	// MetadataFilters scopes this instance to hostnames whose Metadata map
	// satisfies every key. Today the only knob that populates this is
	// DNSWEAVER_{NAME}_ENTRYPOINTS (key: "traefik.entrypoint"); future
	// per-source filters layer in here without touching the matcher.
	MetadataFilters map[string][]string

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
		MetadataFilters:     c.MetadataFilters,
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
func loadInstanceConfig(instanceName string, defaultTTL int) (*ProviderInstanceConfig, []*ConfigError) {
	var errs []*ConfigError
	prefix := envPrefix(instanceName)

	cfg := &ProviderInstanceConfig{
		Name:           instanceName,
		ProviderConfig: make(map[string]string),
	}

	// TYPE is required
	cfg.TypeName = strings.ToLower(getEnv(prefix + "TYPE"))
	if cfg.TypeName == "" {
		errs = append(errs, configErrFull(prefix+"TYPE", "required but not set", fmt.Sprintf("Provider %q needs a type (e.g., technitium, cloudflare, pihole)", instanceName), prefix+"TYPE=technitium"))
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
		errs = append(errs, configErrFull(prefix+"RECORD_TYPE", fmt.Sprintf("invalid value %q", recordTypeStr), "Must be one of: A, AAAA, CNAME", prefix+"RECORD_TYPE=A"))
	}

	// TARGET is required
	cfg.Target = getEnv(prefix + "TARGET")
	if cfg.Target == "" {
		errs = append(errs, configErrFull(prefix+"TARGET", "required but not set", fmt.Sprintf("Provider %q needs a target IP or hostname for DNS records", instanceName), prefix+"TARGET=10.0.0.1"))
	}

	// TTL (optional, defaults to global default)
	if ttlStr := getEnv(prefix + "TTL"); ttlStr != "" {
		ttl, err := strconv.Atoi(ttlStr)
		if err != nil {
			errs = append(errs, configErrFull(prefix+"TTL", fmt.Sprintf("invalid integer %q", ttlStr), "Must be a positive integer (seconds)", prefix+"TTL=300"))
		} else if ttl < 1 {
			errs = append(errs, configErrFull(prefix+"TTL", "must be at least 1", "TTL is in seconds; typical values are 60-3600", prefix+"TTL=300"))
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
			errs = append(errs, configErrFull(prefix+"MODE", err.Error(), "Must be one of: managed, authoritative, additive", prefix+"MODE=managed"))
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
		errs = append(errs, configErrHelp(prefix[:len(prefix)-1], "cannot set both DOMAINS and DOMAINS_REGEX", "Use either glob patterns (DOMAINS) or regex patterns (DOMAINS_REGEX), not both"))
	} else if domainsStr == "" && domainsRegexStr == "" {
		errs = append(errs, configErrFull(prefix+"DOMAINS", "required but not set", fmt.Sprintf("Provider %q needs at least one domain pattern to match", instanceName), prefix+"DOMAINS=*.example.com"))
	} else if domainsStr != "" {
		cfg.Domains = splitPatterns(domainsStr)
	} else {
		cfg.DomainsRegex = splitPatterns(domainsRegexStr)
	}

	// Exclude patterns - either EXCLUDE_DOMAINS or EXCLUDE_DOMAINS_REGEX
	excludeDomainsStr := getEnv(prefix + "EXCLUDE_DOMAINS")
	excludeDomainsRegexStr := getEnv(prefix + "EXCLUDE_DOMAINS_REGEX")

	if excludeDomainsStr != "" && excludeDomainsRegexStr != "" {
		errs = append(errs, configErrHelp(prefix[:len(prefix)-1], "cannot set both EXCLUDE_DOMAINS and EXCLUDE_DOMAINS_REGEX", "Use either glob patterns or regex patterns for exclusions, not both"))
	} else if excludeDomainsStr != "" {
		cfg.ExcludeDomains = splitPatterns(excludeDomainsStr)
	} else if excludeDomainsRegexStr != "" {
		cfg.ExcludeDomainsRegex = splitPatterns(excludeDomainsRegexStr)
	}

	// ENTRYPOINTS — Traefik-source filter that scopes the instance to one or
	// more Traefik entrypoints. Translates to a generic metadata filter on
	// the "traefik.entrypoint" key. Empty/unset = no filter (today's
	// behavior).
	if entrypointsStr := getEnv(prefix + "ENTRYPOINTS"); entrypointsStr != "" {
		eps := splitPatterns(entrypointsStr)
		if len(eps) > 0 {
			if cfg.MetadataFilters == nil {
				cfg.MetadataFilters = make(map[string][]string)
			}
			cfg.MetadataFilters["traefik.entrypoint"] = eps
		}
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

	// Resolve the legacy INSECURE_SKIP_VERIFY alias. The new canonical name
	// is TLS_SKIP_VERIFY. If both are set, the new name wins and we log a
	// conflict; if only the legacy name is set, promote it and log a
	// one-line deprecation notice. The legacy key is always stripped from
	// the map so downstream consumers see only the canonical TLS_* keys.
	resolveLegacyTLSSkipVerify(cfg.ProviderConfig, instanceName)

	return cfg, errs
}

// resolveLegacyTLSSkipVerify migrates DNSWEAVER_{NAME}_INSECURE_SKIP_VERIFY
// into TLS_SKIP_VERIFY in the per-instance ProviderConfig map and emits a
// deprecation warning when the legacy key is in use. Scheduled for removal in
// a future major release.
func resolveLegacyTLSSkipVerify(cfgMap map[string]string, instanceName string) {
	const (
		legacy    = "INSECURE_SKIP_VERIFY"
		canonical = "TLS_SKIP_VERIFY"
	)
	legacyVal, hasLegacy := cfgMap[legacy]
	_, hasCanonical := cfgMap[canonical]
	switch {
	case hasLegacy && hasCanonical:
		slog.Warn("both DNSWEAVER_{NAME}_INSECURE_SKIP_VERIFY (deprecated) and DNSWEAVER_{NAME}_TLS_SKIP_VERIFY are set; the new TLS_SKIP_VERIFY value wins. Remove INSECURE_SKIP_VERIFY — it will be removed in a future major release.",
			slog.String("instance", instanceName),
		)
		delete(cfgMap, legacy)
	case hasLegacy:
		slog.Warn("DNSWEAVER_{NAME}_INSECURE_SKIP_VERIFY is deprecated; use DNSWEAVER_{NAME}_TLS_SKIP_VERIFY instead (the legacy name will be removed in a future major release)",
			slog.String("instance", instanceName),
		)
		cfgMap[canonical] = legacyVal
		delete(cfgMap, legacy)
	}
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
	{"ACCESS_MODE", false},          // Pi-hole specific (api/file) — renamed from MODE in v0.10.0
	{"USERNAME", false},             // AdGuard Home specific
	{"PASSWORD", true},              // Pi-hole and AdGuard Home specific
	{"API_SECRET", true},            // OPNsense specific: paired with API_KEY for basic auth
	{"ENGINE", false},               // OPNsense/pfSense: unbound|dnsmasq resolver selection
	{"RECONFIGURE_MODE", false},     // OPNsense/pfSense: per_write|never
	{"INSECURE_SKIP_VERIFY", false}, // DEPRECATED: alias for TLS_SKIP_VERIFY
	// Unified TLS fields — consumed by pkg/provider/extractTLSConfig and
	// passed to every HTTP-based provider via FactoryConfig.HTTP.TLS.
	{"TLS_CA_FILE", false},        // PEM CA bundle path (appended to system roots)
	{"TLS_CERT_FILE", false},      // mTLS client certificate path
	{"TLS_KEY_FILE", false},       // mTLS client key path
	{"TLS_SERVER_NAME", false},    // SNI / verification hostname override
	{"TLS_SKIP_VERIFY", false},    // Bypass certificate verification (warning logged)
	{"TLS_MIN_VERSION", false},    // "1.2" or "1.3" (default "1.2")
	{"AUTO_HTTPS_RECORDS", false}, // Technitium-specific: companion HTTPS record creation
	{"AUTO_HTTPS_ALPN", false},    // Technitium-specific: ALPN value for companion HTTPS records
	// RFC 2136 specific fields
	{"SERVER", false},         // RFC 2136: DNS server address (host:port)
	{"TSIG_KEY_NAME", false},  // RFC 2136: TSIG key name
	{"TSIG_SECRET", true},     // RFC 2136: TSIG secret (supports _FILE)
	{"TSIG_ALGORITHM", false}, // RFC 2136: TSIG algorithm (hmac-sha256, etc.)
	{"USE_TCP", false},        // RFC 2136: Force TCP transport
	// OVH specific fields
	{"APPLICATION_KEY", true},    // OVH: application key (supports _FILE)
	{"APPLICATION_SECRET", true}, // OVH: application secret (supports _FILE)
	{"CONSUMER_KEY", true},       // OVH: consumer key (supports _FILE)
	{"ENDPOINT", false},          // OVH: API region (e.g. ovh-eu)
	// PowerDNS specific fields
	{"SERVER_ID", false}, // PowerDNS: server id segment in the API path (default "localhost")
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

	// Migrate the legacy INSECURE_SKIP_VERIFY alias (whether it arrived
	// from YAML or env vars) to the canonical TLS_SKIP_VERIFY key.
	resolveLegacyTLSSkipVerify(cfg.ProviderConfig, cfg.Name)
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
