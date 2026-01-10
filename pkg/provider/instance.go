package provider

import (
	"context"
	"net"
	"time"

	"gitlab.bluewillows.net/root/dnsweaver/internal/matcher"
	"gitlab.bluewillows.net/root/dnsweaver/internal/metrics"
)

// isIPAddress returns true if the given string is a valid IPv4 or IPv6 address.
func isIPAddress(s string) bool {
	return net.ParseIP(s) != nil
}

// isIPv4Address returns true if the given string is a valid IPv4 address.
func isIPv4Address(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	// To4() returns nil for IPv6 addresses
	return ip.To4() != nil
}

// isIPv6Address returns true if the given string is a valid IPv6 address.
func isIPv6Address(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	// If To4() is nil, it's IPv6
	return ip.To4() == nil
}

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
	return pi.CreateRecordWithValues(ctx, hostname, pi.RecordType, pi.Target, pi.TTL)
}

// CreateRecordWithValues creates a DNS record with explicit type, target, and TTL.
// This is used when RecordHints override the provider instance defaults.
func (pi *ProviderInstance) CreateRecordWithValues(ctx context.Context, hostname string, recordType RecordType, target string, ttl int) error {
	record := Record{
		Hostname: hostname,
		Type:     recordType,
		Target:   target,
		TTL:      ttl,
	}

	start := time.Now()
	err := pi.Provider.Create(ctx, record)
	duration := time.Since(start).Seconds()

	status := "success"
	if err != nil {
		status = "error"
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "create", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "create").Observe(duration)

	return err
}

// DeleteRecord removes the DNS record for the given hostname.
func (pi *ProviderInstance) DeleteRecord(ctx context.Context, hostname string) error {
	record := Record{
		Hostname: hostname,
		Type:     pi.RecordType,
		Target:   pi.Target,
	}

	start := time.Now()
	err := pi.Provider.Delete(ctx, record)
	duration := time.Since(start).Seconds()

	status := "success"
	if err != nil {
		status = "error"
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "delete", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "delete").Observe(duration)

	return err
}

// GetExistingRecords returns all A/CNAME records that exist for a given hostname.
// This is used by the reconciler to detect if the target has changed or if there's
// a type conflict before creating a new record.
func (pi *ProviderInstance) GetExistingRecords(ctx context.Context, hostname string) ([]Record, error) {
	start := time.Now()
	allRecords, err := pi.Provider.List(ctx)
	duration := time.Since(start).Seconds()

	status := "success"
	if err != nil {
		status = "error"
		metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "list", status).Inc()
		metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "list").Observe(duration)
		return nil, err
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "list", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "list").Observe(duration)

	var matching []Record
	for _, r := range allRecords {
		// Only return A, AAAA, and CNAME records for the hostname (skip TXT, etc.)
		if r.Hostname == hostname && (r.Type == RecordTypeA || r.Type == RecordTypeAAAA || r.Type == RecordTypeCNAME) {
			matching = append(matching, r)
		}
	}

	return matching, nil
}

// DeleteRecordByTarget removes a specific DNS record by hostname and target.
// Unlike DeleteRecord, this allows specifying the target to delete (for cleanup
// of records with changed targets).
func (pi *ProviderInstance) DeleteRecordByTarget(ctx context.Context, hostname string, recordType RecordType, target string) error {
	record := Record{
		Hostname: hostname,
		Type:     recordType,
		Target:   target,
	}

	start := time.Now()
	err := pi.Provider.Delete(ctx, record)
	duration := time.Since(start).Seconds()

	status := "success"
	if err != nil {
		status = "error"
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "delete", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "delete").Observe(duration)

	return err
}

// CreateOwnershipRecord creates a TXT record to mark ownership of a hostname.
// The TXT record is named "_dnsweaver.{hostname}" with value "heritage=dnsweaver".
func (pi *ProviderInstance) CreateOwnershipRecord(ctx context.Context, hostname string) error {
	record := OwnershipRecord(hostname, pi.TTL)

	start := time.Now()
	err := pi.Provider.Create(ctx, record)
	duration := time.Since(start).Seconds()

	status := "success"
	if err != nil {
		// Ignore conflict errors - ownership record may already exist
		if IsConflict(err) {
			return nil
		}
		status = "error"
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "create_ownership", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "create_ownership").Observe(duration)

	return err
}

// DeleteOwnershipRecord removes the TXT ownership record for a hostname.
func (pi *ProviderInstance) DeleteOwnershipRecord(ctx context.Context, hostname string) error {
	record := OwnershipRecord(hostname, pi.TTL)

	start := time.Now()
	err := pi.Provider.Delete(ctx, record)
	duration := time.Since(start).Seconds()

	status := "success"
	if err != nil {
		status = "error"
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "delete_ownership", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "delete_ownership").Observe(duration)

	return err
}

// HasOwnershipRecord checks if an ownership TXT record exists for the given hostname.
func (pi *ProviderInstance) HasOwnershipRecord(ctx context.Context, hostname string) (bool, error) {
	ownershipName := OwnershipRecordName(hostname)

	start := time.Now()
	records, err := pi.Provider.List(ctx)
	duration := time.Since(start).Seconds()

	status := "success"
	if err != nil {
		status = "error"
		metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "list", status).Inc()
		metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "list").Observe(duration)
		return false, err
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "list", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "list").Observe(duration)

	for _, r := range records {
		if r.Hostname == ownershipName && r.Type == RecordTypeTXT && r.Target == OwnershipValue {
			return true, nil
		}
	}

	return false, nil
}

// RecoverOwnedHostnames scans the provider for ownership TXT records and returns
// the list of hostnames that dnsweaver previously created. This is used on startup
// to recover state and enable orphan cleanup for records created before a restart.
func (pi *ProviderInstance) RecoverOwnedHostnames(ctx context.Context) ([]string, error) {
	start := time.Now()
	records, err := pi.Provider.List(ctx)
	duration := time.Since(start).Seconds()

	status := "success"
	if err != nil {
		status = "error"
		metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "list", status).Inc()
		metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "list").Observe(duration)
		return nil, err
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "list", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "list").Observe(duration)

	var hostnames []string
	for _, r := range records {
		// Look for ownership TXT records with the correct value
		if r.Type == RecordTypeTXT && r.Target == OwnershipValue && IsOwnershipRecord(r.Hostname) {
			hostname := ExtractHostnameFromOwnership(r.Hostname)
			if hostname != "" {
				hostnames = append(hostnames, hostname)
			}
		}
	}

	return hostnames, nil
}

// Ping checks connectivity to the provider.
func (pi *ProviderInstance) Ping(ctx context.Context) error {
	start := time.Now()
	err := pi.Provider.Ping(ctx)
	duration := time.Since(start).Seconds()

	status := "success"
	healthy := float64(1)
	if err != nil {
		status = "error"
		healthy = 0
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "ping", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "ping").Observe(duration)
	metrics.ProviderHealthy.WithLabelValues(pi.Name()).Set(healthy)

	return err
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
	if c.RecordType != RecordTypeA && c.RecordType != RecordTypeAAAA && c.RecordType != RecordTypeCNAME {
		return ErrConfigInvalid("record_type", string(c.RecordType), "must be A, AAAA, or CNAME")
	}
	if c.Target == "" {
		return ErrConfigMissing("target")
	}

	// Validate target matches record type
	if c.RecordType == RecordTypeCNAME && isIPAddress(c.Target) {
		return ErrConfigInvalid("target", c.Target, "CNAME records cannot point to IP addresses; use record_type=A or AAAA for IP targets")
	}
	if c.RecordType == RecordTypeA && !isIPv4Address(c.Target) {
		return ErrConfigInvalid("target", c.Target, "A records must point to IPv4 addresses; use record_type=AAAA for IPv6 or CNAME for hostnames")
	}
	if c.RecordType == RecordTypeAAAA && !isIPv6Address(c.Target) {
		return ErrConfigInvalid("target", c.Target, "AAAA records must point to IPv6 addresses; use record_type=A for IPv4 or CNAME for hostnames")
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
