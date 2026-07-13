package config

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/maxfield-allison/dnsweaver/internal/target"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// validateConfig performs cross-field validation on the complete configuration.
// Returns a list of structured validation errors.
func validateConfig(cfg *Config) []*ConfigError {
	var errs []*ConfigError

	// Validate enum fields that can come from file or env vars
	switch cfg.Global.LogLevel {
	case "debug", "info", "warn", "error":
		// Valid
	default:
		errs = append(errs, configErrFull(
			"log_level",
			fmt.Sprintf("invalid value %q", cfg.Global.LogLevel),
			"Must be one of: debug, info, warn, error",
			"DNSWEAVER_LOG_LEVEL=info",
		))
	}

	switch cfg.Global.LogFormat {
	case "json", "text":
		// Valid
	default:
		errs = append(errs, configErrFull(
			"log_format",
			fmt.Sprintf("invalid value %q", cfg.Global.LogFormat),
			"Must be one of: json, text",
			"DNSWEAVER_LOG_FORMAT=json",
		))
	}

	switch cfg.Global.DockerMode {
	case "auto", "swarm", "standalone":
		// Valid
	case "":
		// Will use default
	default:
		errs = append(errs, configErrFull(
			"docker_mode",
			fmt.Sprintf("invalid value %q", cfg.Global.DockerMode),
			"Must be one of: auto, swarm, standalone",
			"DNSWEAVER_DOCKER_MODE=auto",
		))
	}

	// Validate platform value
	switch cfg.Global.Platform {
	case "docker", "kubernetes", "both", "none":
		// Valid
	case "":
		// Will use default
	default:
		errs = append(errs, configErrFull(
			"platform",
			fmt.Sprintf("invalid value %q", cfg.Global.Platform),
			"Must be one of: docker, kubernetes, both, none",
			"DNSWEAVER_PLATFORM=docker",
		))
	}

	// Validate provider names are unique
	seen := make(map[string]bool)
	for _, inst := range cfg.ProviderInstances {
		if seen[inst.Name] {
			errs = append(errs, configErrHelp(
				"providers",
				fmt.Sprintf("duplicate provider instance name: %q", inst.Name),
				"Each provider instance must have a unique name",
			))
		}
		seen[inst.Name] = true
	}

	// Validate target matches record type for each provider
	for _, inst := range cfg.ProviderInstances {
		errs = append(errs, validateTargetRecordType(inst)...)
	}

	// Validate domain patterns for each provider
	for _, inst := range cfg.ProviderInstances {
		errs = append(errs, validateDomainPatterns(inst)...)
	}

	return errs
}

// validateTargetRecordType ensures the target is appropriate for the record type.
func validateTargetRecordType(inst *ProviderInstanceConfig) []*ConfigError {
	var errs []*ConfigError
	prefix := envPrefix(inst.Name)
	field := prefix + "TARGET"

	// When a dynamic target mode is configured, the literal TARGET is only an
	// optional fallback and is validated against the record type when present;
	// the resolved value's family is enforced at runtime. Dynamic modes resolve
	// IP addresses, so they are incompatible with CNAME records.
	if inst.TargetMode != "" {
		if _, err := target.Parse(inst.TargetMode, target.FamilyIPv4); err != nil {
			errs = append(errs, configErrFull(
				prefix+"TARGET_MODE",
				err.Error(),
				"Use 'public' or 'interface:<name>' (e.g. interface:eth0)",
				prefix+"TARGET_MODE=public",
			))
		}
		if inst.RecordType == provider.RecordTypeCNAME {
			errs = append(errs, configErrFull(
				prefix+"TARGET_MODE",
				fmt.Sprintf("TARGET_MODE %q cannot be used with CNAME records", inst.TargetMode),
				"Dynamic target modes resolve IP addresses; use an A or AAAA record type",
				prefix+"RECORD_TYPE=A",
			))
		}
		if inst.Target == "" {
			// No fallback to validate; runtime resolution enforces the family.
			return errs
		}
	}

	switch inst.RecordType {
	case provider.RecordTypeA:
		ip := net.ParseIP(inst.Target)
		if ip == nil {
			errs = append(errs, configErrFull(
				field,
				fmt.Sprintf("A records must point to an IP address, got %q", inst.Target),
				"Set TARGET to a valid IPv4 address for A records",
				prefix+"TARGET=10.0.0.1",
			))
		} else if ip.To4() == nil {
			errs = append(errs, configErrFull(
				field,
				fmt.Sprintf("A records must point to an IPv4 address, got IPv6 %q", inst.Target),
				"Use AAAA record type for IPv6 addresses",
				prefix+"RECORD_TYPE=AAAA",
			))
		}
	case provider.RecordTypeAAAA:
		ip := net.ParseIP(inst.Target)
		if ip == nil || ip.To4() != nil {
			errs = append(errs, configErrFull(
				field,
				fmt.Sprintf("AAAA records must point to an IPv6 address, got %q", inst.Target),
				"Set TARGET to a valid IPv6 address for AAAA records",
				prefix+"TARGET=2001:db8::1",
			))
		}
	case provider.RecordTypeCNAME:
		if net.ParseIP(inst.Target) != nil {
			errs = append(errs, configErrFull(
				field,
				fmt.Sprintf("CNAME records cannot point to IP addresses, got %q", inst.Target),
				"Use a hostname for CNAME targets, or change to an A/AAAA record type",
				prefix+"TARGET=host.example.com",
			))
		}
	case provider.RecordTypeTXT, provider.RecordTypeSRV:
		// Flexible targets, no validation needed
	case provider.RecordTypeHTTPS:
		// HTTPS records are managed automatically as companion records; no target validation needed
	}

	return errs
}

// validateDomainPatterns validates glob and regex domain patterns for a provider.
func validateDomainPatterns(inst *ProviderInstanceConfig) []*ConfigError {
	var errs []*ConfigError
	prefix := envPrefix(inst.Name)

	// Validate glob domain patterns
	for _, pattern := range inst.Domains {
		if err := validateGlobPattern(pattern); err != nil {
			errs = append(errs, configErrFull(
				prefix+"DOMAINS",
				fmt.Sprintf("invalid domain pattern %q: %s", pattern, err),
				"Domain patterns use glob syntax: * matches any characters within a label",
				prefix+"DOMAINS=*.example.com,internal.example.com",
			))
		}
	}

	// Validate glob exclude patterns
	for _, pattern := range inst.ExcludeDomains {
		if err := validateGlobPattern(pattern); err != nil {
			errs = append(errs, configErrFull(
				prefix+"EXCLUDE_DOMAINS",
				fmt.Sprintf("invalid exclude pattern %q: %s", pattern, err),
				"Exclude patterns use the same glob syntax as DOMAINS",
				prefix+"EXCLUDE_DOMAINS=test.*.example.com",
			))
		}
	}

	// Validate regex domain patterns compile correctly
	for _, pattern := range inst.DomainsRegex {
		if _, err := regexp.Compile(pattern); err != nil {
			errs = append(errs, configErrFull(
				prefix+"DOMAINS_REGEX",
				fmt.Sprintf("invalid regex pattern %q: %s", pattern, err),
				"Regex patterns must be valid Go regular expressions",
				prefix+`DOMAINS_REGEX=^.*\.example\.com$`,
			))
		}
	}

	// Validate regex exclude patterns compile correctly
	for _, pattern := range inst.ExcludeDomainsRegex {
		if _, err := regexp.Compile(pattern); err != nil {
			errs = append(errs, configErrFull(
				prefix+"EXCLUDE_DOMAINS_REGEX",
				fmt.Sprintf("invalid exclude regex %q: %s", pattern, err),
				"Regex patterns must be valid Go regular expressions",
				prefix+`EXCLUDE_DOMAINS_REGEX=^test\..*$`,
			))
		}
	}

	return errs
}

// validateGlobPattern checks that a glob domain pattern is syntactically valid.
func validateGlobPattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("empty pattern")
	}

	// Check for double wildcards (e.g., *.*.example.com)
	if strings.Contains(pattern, "*.*") {
		return fmt.Errorf("double wildcard (*.*..) is not supported; use a single * per label")
	}

	// Check for wildcard not at start of a label
	for _, label := range strings.Split(pattern, ".") {
		if strings.Contains(label, "*") && label != "*" && !strings.HasPrefix(label, "*") {
			return fmt.Errorf("wildcard must be at the start of a label, got %q", label)
		}
	}

	return nil
}

// validateProviderType checks that the provider type is known.
// This is called later when registering providers, not during config load.
func validateProviderType(typeName string, knownTypes []string) error {
	for _, known := range knownTypes {
		if typeName == known {
			return nil
		}
	}
	return fmt.Errorf("unknown provider type: %q (known types: %s)", typeName, strings.Join(knownTypes, ", "))
}
