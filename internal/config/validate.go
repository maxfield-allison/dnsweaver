package config

import (
	"fmt"
	"net"
	"strings"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	if len(e.Errors) == 1 {
		return fmt.Sprintf("configuration error: %s", e.Errors[0])
	}
	return fmt.Sprintf("configuration errors:\n  - %s", strings.Join(e.Errors, "\n  - "))
}

// validateConfig performs cross-field validation on the complete configuration.
// Returns a list of validation errors.
func validateConfig(cfg *Config) []string {
	var errs []string

	// Validate provider names are unique
	seen := make(map[string]bool)
	for _, inst := range cfg.ProviderInstances {
		if seen[inst.Name] {
			errs = append(errs, fmt.Sprintf("duplicate provider instance name: %q", inst.Name))
		}
		seen[inst.Name] = true
	}

	// Validate target matches record type for each provider
	for _, inst := range cfg.ProviderInstances {
		errs = append(errs, validateTargetRecordType(inst)...)
	}

	return errs
}

// validateTargetRecordType ensures the target is appropriate for the record type.
func validateTargetRecordType(inst *ProviderInstanceConfig) []string {
	var errs []string
	prefix := envPrefix(inst.Name)

	switch inst.RecordType {
	case provider.RecordTypeA:
		// A records must have an IP address as target
		if net.ParseIP(inst.Target) == nil {
			errs = append(errs, fmt.Sprintf("%sTARGET: A records must point to an IP address, got %q", prefix, inst.Target))
		}
	case provider.RecordTypeAAAA:
		// AAAA records must have an IPv6 address as target
		ip := net.ParseIP(inst.Target)
		if ip == nil || ip.To4() != nil {
			errs = append(errs, fmt.Sprintf("%sTARGET: AAAA records must point to an IPv6 address, got %q", prefix, inst.Target))
		}
	case provider.RecordTypeCNAME:
		// CNAME records must have a hostname, not an IP
		if net.ParseIP(inst.Target) != nil {
			errs = append(errs, fmt.Sprintf("%sTARGET: CNAME records cannot point to IP addresses, got %q", prefix, inst.Target))
		}
	case provider.RecordTypeTXT, provider.RecordTypeSRV:
		// TXT and SRV records have flexible targets, no validation needed
	}

	return errs
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
