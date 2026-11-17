package config

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

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
	// When TargetMode is set, Target is optional and acts as the fallback used
	// until the first successful dynamic resolution.
	Target string

	// TargetMode enables dynamic target resolution instead of a literal Target.
	// Empty means Target is used verbatim. Recognized values: "public"
	// (auto-detect the host's public IP) and "interface:<name>" (read a named
	// NIC's primary IP). Parsed by internal/target.
	TargetMode string

	// TargetRefreshInterval is how often a dynamic target is re-resolved.
	// Zero means the default (see DefaultTargetRefreshInterval). Ignored when
	// TargetMode is empty.
	TargetRefreshInterval time.Duration

	// TargetPublicEndpoints optionally overrides the public-IP echo endpoints
	// used by TargetMode=public. Empty uses the built-in defaults.
	TargetPublicEndpoints []string

	// TTL for DNS records.
	TTL int

	// Mode is the operational mode (managed, authoritative, additive).
	// Defaults to "managed" if not set.
	Mode provider.OperationalMode

	// AdoptExisting overrides the global adoption policy for this provider.
	// nil inherits the global DNSWEAVER_ADOPT_EXISTING setting.
	AdoptExisting *bool

	// AdoptExistingAllowOverrides permits workload-controlled adopt=true hints
	// to enable adoption for this provider. adopt=false hints are always honored.
	AdoptExistingAllowOverrides bool

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
		Name:                        c.Name,
		TypeName:                    c.TypeName,
		RecordType:                  c.RecordType,
		Target:                      c.Target,
		TargetMode:                  c.TargetMode,
		TTL:                         c.TTL,
		Mode:                        c.Mode,
		AdoptExisting:               c.AdoptExisting,
		AdoptExistingAllowOverrides: c.AdoptExistingAllowOverrides,
		Domains:                     c.Domains,
		DomainsRegex:                c.DomainsRegex,
		ExcludeDomains:              c.ExcludeDomains,
		ExcludeDomainsRegex:         c.ExcludeDomainsRegex,
		MetadataFilters:             c.MetadataFilters,
		ProviderConfig:              c.ProviderConfig,
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
	cfg := &ProviderInstanceConfig{
		Name:           instanceName,
		TTL:            defaultTTL,
		ProviderConfig: make(map[string]string),
	}
	errs := applyProviderEnv(cfg)
	errs = append(errs, validateProviderRequired(cfg)...)
	return cfg, errs
}

// applyProviderEnv applies the same environment contract to file-backed and
// environment-only instances. Required fields are checked after merging.
func applyProviderEnv(cfg *ProviderInstanceConfig) []*ConfigError {
	var errs []*ConfigError
	instanceName := cfg.Name
	prefix := envPrefix(instanceName)
	if cfg.ProviderConfig == nil {
		cfg.ProviderConfig = make(map[string]string)
	}

	// TYPE is required
	if v := getEnv(prefix + "TYPE"); v != "" {
		cfg.TypeName = strings.ToLower(v)
	}

	// RECORD_TYPE (default: A)
	recordTypeStr := string(cfg.RecordType)
	if v := getEnv(prefix + "RECORD_TYPE"); v != "" {
		recordTypeStr = strings.ToUpper(v)
	}
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

	// TARGET_MODE (optional): enables dynamic target resolution. When set,
	// TARGET becomes an optional fallback rather than required.
	if v := getEnv(prefix + "TARGET_MODE"); v != "" {
		cfg.TargetMode = strings.TrimSpace(v)
	}

	// TARGET is required unless a dynamic TARGET_MODE is configured, in which
	// case it is an optional fallback used until the first resolution succeeds.
	if v := getEnv(prefix + "TARGET"); v != "" {
		cfg.Target = v
	}

	// TARGET_REFRESH_INTERVAL (optional): how often to re-resolve a dynamic
	// target. Only meaningful when TARGET_MODE is set.
	if refreshStr := getEnv(prefix + "TARGET_REFRESH_INTERVAL"); refreshStr != "" {
		d, err := time.ParseDuration(refreshStr)
		if err != nil {
			errs = append(errs, configErrFull(prefix+"TARGET_REFRESH_INTERVAL", fmt.Sprintf("invalid duration %q", refreshStr), "Use a Go duration such as 5m, 30s, or 1h", prefix+"TARGET_REFRESH_INTERVAL=5m"))
		} else if d < time.Second {
			errs = append(errs, configErrFull(prefix+"TARGET_REFRESH_INTERVAL", "must be at least 1s", "Refreshing more than once per second is not useful", prefix+"TARGET_REFRESH_INTERVAL=5m"))
		} else {
			cfg.TargetRefreshInterval = d
		}
	}

	// TARGET_PUBLIC_ENDPOINTS (optional): override the public-IP echo endpoints.
	if eps := getEnv(prefix + "TARGET_PUBLIC_ENDPOINTS"); eps != "" {
		cfg.TargetPublicEndpoints = splitPatterns(eps)
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
	}

	// Validate the effective mode after the environment has overridden the file.
	modeStr := string(cfg.Mode)
	if v := getEnv(prefix + "MODE"); v != "" {
		modeStr = v
	}
	mode, err := provider.ParseOperationalMode(modeStr)
	if err != nil {
		errs = append(errs, configErrFull(prefix+"MODE", err.Error(), "Must be one of: managed, authoritative, additive", prefix+"MODE=managed"))
	} else {
		cfg.Mode = mode
	}

	// ADOPT_EXISTING (optional) overrides the global setting for this instance.
	if value := getEnv(prefix + "ADOPT_EXISTING"); value != "" {
		if parsed, ok := parseBoolValue(value); ok {
			cfg.AdoptExisting = &parsed
		} else {
			errs = append(errs, configErrFull(prefix+"ADOPT_EXISTING", fmt.Sprintf("invalid boolean %q", value), "Use true or false", prefix+"ADOPT_EXISTING=false"))
		}
	}

	// Workload labels may only elevate adoption when the operator explicitly
	// enables that capability for this provider instance.
	if value := getEnv(prefix + "ADOPT_EXISTING_ALLOW_OVERRIDES"); value != "" {
		if parsed, ok := parseBoolValue(value); ok {
			cfg.AdoptExistingAllowOverrides = parsed
		} else {
			errs = append(errs, configErrFull(prefix+"ADOPT_EXISTING_ALLOW_OVERRIDES", fmt.Sprintf("invalid boolean %q", value), "Use true or false", prefix+"ADOPT_EXISTING_ALLOW_OVERRIDES=false"))
		}
	}

	// Domain patterns - either DOMAINS or DOMAINS_REGEX, not both
	domainsStr := getEnv(prefix + "DOMAINS")
	domainsRegexStr := getEnv(prefix + "DOMAINS_REGEX")

	if domainsStr != "" || domainsRegexStr != "" {
		cfg.Domains = splitPatterns(domainsStr)
		cfg.DomainsRegex = splitPatterns(domainsRegexStr)
	}
	if len(cfg.Domains) > 0 && len(cfg.DomainsRegex) > 0 {
		errs = append(errs, configErrHelp(prefix[:len(prefix)-1], "cannot set both DOMAINS and DOMAINS_REGEX", "Use either glob patterns (DOMAINS) or regex patterns (DOMAINS_REGEX), not both"))
	}

	// Changing pattern kinds in the environment replaces the file's kind.
	excludeDomainsStr := getEnv(prefix + "EXCLUDE_DOMAINS")
	excludeDomainsRegexStr := getEnv(prefix + "EXCLUDE_DOMAINS_REGEX")
	if excludeDomainsStr != "" || excludeDomainsRegexStr != "" {
		cfg.ExcludeDomains = splitPatterns(excludeDomainsStr)
		cfg.ExcludeDomainsRegex = splitPatterns(excludeDomainsRegexStr)
	}
	if len(cfg.ExcludeDomains) > 0 && len(cfg.ExcludeDomainsRegex) > 0 {
		errs = append(errs, configErrHelp(prefix[:len(prefix)-1], "cannot set both EXCLUDE_DOMAINS and EXCLUDE_DOMAINS_REGEX", "Use either glob patterns or regex patterns for exclusions, not both"))
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
		configured := false
		if field.isSecret {
			var err error
			value, configured, err = getEnvWithFileFallback(prefix, field.name)
			if err != nil {
				errs = append(errs, configErrHelp(prefix+field.name+"_FILE", err.Error(), "Ensure the configured secret file exists and is readable by the dnsweaver process"))
				continue
			}
		} else {
			value = getEnv(prefix + field.name)
			configured = value != ""
		}
		if configured {
			cfg.ProviderConfig[field.name] = value
		}
	}

	// Resolve the legacy INSECURE_SKIP_VERIFY alias. The new canonical name
	// is TLS_SKIP_VERIFY. If both are set, the new name wins and we log a
	// conflict; if only the legacy name is set, promote it and log a
	// one-line deprecation notice. The legacy key is always stripped from
	// the map so downstream consumers see only the canonical TLS_* keys.
	resolveLegacyTLSSkipVerify(cfg.ProviderConfig, instanceName)

	return errs
}

// validateProviderRequired runs after both file and environment values are known.
func validateProviderRequired(cfg *ProviderInstanceConfig) []*ConfigError {
	var errs []*ConfigError
	prefix := envPrefix(cfg.Name)
	if cfg.TypeName == "" {
		errs = append(errs, configErrFull(prefix+"TYPE", "required but not set", fmt.Sprintf("Provider %q needs a type (e.g., technitium, cloudflare, pihole)", cfg.Name), prefix+"TYPE=technitium"))
	}
	if cfg.Target == "" && cfg.TargetMode == "" {
		errs = append(errs, configErrFull(prefix+"TARGET", "required but not set", fmt.Sprintf("Provider %q needs a target IP or hostname for DNS records (or set %sTARGET_MODE for dynamic resolution)", cfg.Name, prefix), prefix+"TARGET=10.0.0.1"))
	}
	if len(cfg.Domains) == 0 && len(cfg.DomainsRegex) == 0 {
		errs = append(errs, configErrFull(prefix+"DOMAINS", "required but not set", fmt.Sprintf("Provider %q needs at least one domain pattern to match", cfg.Name), prefix+"DOMAINS=*.example.com"))
	}
	return errs
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
	{"PROXIED", false},     // Cloudflare-specific
	{"AUTH_HEADER", false}, // Webhook-specific
	{"AUTH_TOKEN", true},   // Webhook-specific
	{"TIMEOUT", false},     // Webhook-specific
	{"RETRIES", false},     // Webhook-specific
	{"RETRY_DELAY", false}, // Webhook-specific
	{"CONFIG_DIR", false},
	{"CONFIG_FILE", false},
	{"API_VERSION", false}, // Pi-hole: auto, v5, v6
	{"SSH_HOST", false},
	{"SSH_PORT", false},
	{"SSH_USER", false},
	{"SSH_KEY_FILE", false},
	{"SSH_PASSWORD", true},
	{"SSH_KNOWN_HOSTS_FILE", false},
	{"SSH_STRICT_HOST_KEY_CHECKING", false},
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
	// UniFi specific fields
	{"SITE", false}, // UniFi: site internalReference or UUID (default "default")
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
func mergeProviderEnvOverrides(cfg *ProviderInstanceConfig) []*ConfigError {
	return applyProviderEnv(cfg)
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
