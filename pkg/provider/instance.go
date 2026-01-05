package provider

import (
	"context"

	"gitlab.bluewillows.net/root/dnsweaver/internal/matcher"
)

// ProviderInstance combines a Provider with its domain matcher and record configuration.
// This allows each provider instance to have its own:
//   - Domain patterns (which hostnames it handles)
//   - Record type (A or CNAME)
//   - Target (IP for A, hostname for CNAME)
//   - TTL
type ProviderInstance struct {
	// Provider is the underlying DNS provider implementation.
	Provider Provider

	// Matcher determines which hostnames this instance handles.
	Matcher *matcher.DomainMatcher

	// RecordType is the type of DNS record to create (A or CNAME).
	RecordType RecordType

	// Target is the value for DNS records:
	// - For A records: an IP address (e.g., "10.1.20.210")
	// - For CNAME records: a target hostname (e.g., "bluewillows.net")
	Target string

	// TTL is the time-to-live for DNS records in seconds.
	TTL int
}

// Name returns the provider instance name (delegates to Provider).
func (pi *ProviderInstance) Name() string {
	return pi.Provider.Name()
}

// Type returns the provider type (delegates to Provider).
func (pi *ProviderInstance) Type() string {
	return pi.Provider.Type()
}

// Matches returns true if this instance should handle the given hostname.
func (pi *ProviderInstance) Matches(hostname string) bool {
	return pi.Matcher.Matches(hostname)
}

// CreateRecord creates a DNS record for the given hostname using this instance's
// record type and target configuration.
func (pi *ProviderInstance) CreateRecord(ctx context.Context, hostname string) error {
	record := Record{
		Hostname: hostname,
		Type:     pi.RecordType,
		Target:   pi.Target,
		TTL:      pi.TTL,
	}
	return pi.Provider.Create(ctx, record)
}

// DeleteRecord removes the DNS record for the given hostname.
func (pi *ProviderInstance) DeleteRecord(ctx context.Context, hostname string) error {
	record := Record{
		Hostname: hostname,
		Type:     pi.RecordType,
		Target:   pi.Target,
	}
	return pi.Provider.Delete(ctx, record)
}

// Ping checks connectivity to the provider.
func (pi *ProviderInstance) Ping(ctx context.Context) error {
	return pi.Provider.Ping(ctx)
}

// ProviderInstanceConfig holds configuration for creating a ProviderInstance.
type ProviderInstanceConfig struct {
	// Name is the instance name (e.g., "internal-dns").
	Name string

	// TypeName is the provider type (e.g., "technitium", "cloudflare").
	TypeName string

	// RecordType is "A" or "CNAME".
	RecordType RecordType

	// Target is the IP or hostname target for records.
	Target string

	// TTL is the record TTL in seconds.
	TTL int

	// Domains is a list of glob patterns for matching hostnames.
	// At least one is required.
	Domains []string

	// ExcludeDomains is an optional list of glob patterns to exclude.
	ExcludeDomains []string

	// DomainsRegex is a list of regex patterns (alternative to Domains).
	// If set, Domains must be empty.
	DomainsRegex []string

	// ExcludeDomainsRegex is an optional list of regex patterns to exclude.
	ExcludeDomainsRegex []string

	// ProviderConfig holds provider-specific settings (URL, token, zone, etc.).
	ProviderConfig map[string]string
}

// Validate checks that the configuration is valid.
func (c *ProviderInstanceConfig) Validate() error {
	if c.Name == "" {
		return ErrConfigMissing("name")
	}
	if c.TypeName == "" {
		return ErrConfigMissing("type")
	}
	if c.RecordType != RecordTypeA && c.RecordType != RecordTypeCNAME {
		return ErrConfigInvalid("record_type", string(c.RecordType), "must be A or CNAME")
	}
	if c.Target == "" {
		return ErrConfigMissing("target")
	}
	if c.TTL < 1 {
		return ErrConfigInvalid("ttl", "", "must be at least 1")
	}

	// Domains validation: must have either Domains or DomainsRegex, but not both
	hasGlob := len(c.Domains) > 0
	hasRegex := len(c.DomainsRegex) > 0

	if !hasGlob && !hasRegex {
		return ErrConfigMissing("domains (or domains_regex)")
	}
	if hasGlob && hasRegex {
		return ErrConfigInvalid("domains", "", "cannot specify both DOMAINS and DOMAINS_REGEX")
	}

	return nil
}

// UseRegex returns true if regex patterns should be used instead of glob.
func (c *ProviderInstanceConfig) UseRegex() bool {
	return len(c.DomainsRegex) > 0
}

// GetIncludes returns the include patterns (either glob or regex).
func (c *ProviderInstanceConfig) GetIncludes() []string {
	if c.UseRegex() {
		return c.DomainsRegex
	}
	return c.Domains
}

// GetExcludes returns the exclude patterns (either glob or regex).
func (c *ProviderInstanceConfig) GetExcludes() []string {
	if c.UseRegex() {
		return c.ExcludeDomainsRegex
	}
	return c.ExcludeDomains
}
