package provider

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/maxfield-allison/dnsweaver/internal/matcher"
	"github.com/maxfield-allison/dnsweaver/internal/metrics"
)

// Metrics status values.
const (
	statusSuccess = "success"
	statusError   = "error"
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
//   - Operational mode (managed, authoritative, additive)
type ProviderInstance struct {
	// Provider is the underlying DNS provider implementation.
	Provider Provider

	// Matcher determines which hostnames this instance handles.
	Matcher *matcher.DomainMatcher

	// RecordType is the type of DNS record to create (A or CNAME).
	RecordType RecordType

	// Target is the value for DNS records:
	// - For A records: an IP address (e.g., "192.0.2.10")
	// - For CNAME records: a target hostname (e.g., "example.com")
	//
	// Target holds the statically configured value. When a dynamic target
	// resolver is active (DNSWEAVER_{NAME}_TARGET_MODE), the resolved value is
	// stored separately via SetDynamicTarget and takes precedence; read the
	// effective value with EffectiveTarget(). Target itself remains the
	// last-resort fallback used until the first successful resolution.
	Target string

	// dynamicTarget holds a resolver-provided target that overrides the static
	// Target. It is read on the reconcile hot path and written by the target
	// refresh goroutine, so access is lock-free via atomic. A nil pointer means
	// no dynamic target has been resolved yet (fall back to Target).
	dynamicTarget atomic.Pointer[string]

	// TTL is the time-to-live for DNS records in seconds.
	TTL int

	// Mode is the operational mode for this instance.
	// Defaults to ModeManaged if not set.
	Mode OperationalMode

	// InstanceID is the unique identifier for the dnsweaver instance.
	// Used for multi-instance coordination to scope ownership records.
	// Empty string means single-instance mode (legacy behavior).
	InstanceID string

	// MetadataFilters scopes this instance to hostnames whose Metadata map
	// satisfies every key in this map. For each key, the hostname's value
	// must appear in the configured allowlist. A hostname missing the key
	// entirely is treated as a wildcard and matches any filter (mirrors
	// Traefik's "no entrypoints declared = bound to all entrypoints"
	// semantics). nil/empty means no metadata filtering — domain match alone
	// decides ownership (full backward compatibility).
	MetadataFilters map[string][]string

	// Identity is the backend identity reported by the underlying Provider
	// (or a conservative fallback for providers that don't implement
	// Identifiable). The reconciler uses this together with RecordType to
	// group overlapping providers so each distinct backend receives exactly
	// one write per hostname per reconciliation. See identity.go and #88.
	Identity ProviderIdentity
}

// Name returns the provider instance name (delegates to Provider).
func (pi *ProviderInstance) Name() string {
	return pi.Provider.Name()
}

// EffectiveTarget returns the target to use for DNS records: the dynamically
// resolved target if one has been set (see SetDynamicTarget), otherwise the
// statically configured Target. Safe for concurrent use.
func (pi *ProviderInstance) EffectiveTarget() string {
	if v := pi.dynamicTarget.Load(); v != nil {
		return *v
	}
	return pi.Target
}

// SetDynamicTarget stores a resolver-provided target that overrides the static
// Target. Callers (the target refresh loop) should only pass non-empty,
// validated values; passing "" clears the override and falls back to Target.
// Safe for concurrent use.
func (pi *ProviderInstance) SetDynamicTarget(target string) {
	if target == "" {
		pi.dynamicTarget.Store(nil)
		return
	}
	pi.dynamicTarget.Store(&target)
}

// Type returns the provider type (delegates to Provider).
func (pi *ProviderInstance) Type() string {
	return pi.Provider.Type()
}

// Matches returns true if this instance should handle the given hostname.
//
// This is the legacy domain-only matcher. Metadata filters are NOT consulted
// — callers that have a full source.Hostname in hand should prefer
// MatchesWithMetadata so that DNSWEAVER_{NAME}_ENTRYPOINTS-style filters
// take effect.
func (pi *ProviderInstance) Matches(hostname string) bool {
	return pi.Matcher.Matches(hostname)
}

// MatchesWithMetadata returns true if this instance should handle the given
// hostname, AND the supplied metadata satisfies every configured
// MetadataFilter. A hostname missing a filtered key entirely is treated as a
// wildcard and matches any filter value.
func (pi *ProviderInstance) MatchesWithMetadata(hostname string, metadata map[string]string) bool {
	if !pi.Matcher.Matches(hostname) {
		return false
	}
	return matchesMetadataFilters(metadata, pi.MetadataFilters)
}

// matchesMetadataFilters checks the AND-of-OR predicate: for every filter
// key, either the hostname omits the key (wildcard) or its value is in the
// allowlist. nil/empty filters always match.
func matchesMetadataFilters(metadata map[string]string, filters map[string][]string) bool {
	if len(filters) == 0 {
		return true
	}
	for key, allowed := range filters {
		value, present := metadata[key]
		if !present {
			// Missing key = wildcard — matches any filter.
			continue
		}
		if !sliceContainsString(allowed, value) {
			return false
		}
	}
	return true
}

func sliceContainsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// CreateRecord creates a DNS record for the given hostname using this instance's
// record type and target configuration.
func (pi *ProviderInstance) CreateRecord(ctx context.Context, hostname string) error {
	return pi.CreateRecordWithValues(ctx, hostname, pi.RecordType, pi.EffectiveTarget(), pi.TTL, nil, nil)
}

// CreateRecordWithValues creates a DNS record with explicit type, target, TTL, optional SRV data,
// and optional metadata. Metadata is passed through to the provider for provider-specific behavior
// (e.g., Cloudflare proxied state). A nil metadata map is valid and means no metadata.
func (pi *ProviderInstance) CreateRecordWithValues(ctx context.Context, hostname string, recordType RecordType, target string, ttl int, srvData *SRVData, metadata map[string]string) error {
	record := Record{
		Hostname: hostname,
		Type:     recordType,
		Target:   target,
		TTL:      ttl,
		SRV:      srvData,
		Metadata: metadata,
	}

	start := time.Now()
	err := pi.Provider.Create(ctx, record)
	duration := time.Since(start).Seconds()

	status := statusSuccess
	if err != nil {
		status = statusError
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
		Target:   pi.EffectiveTarget(),
	}

	start := time.Now()
	err := pi.Provider.Delete(ctx, record)
	duration := time.Since(start).Seconds()

	status := statusSuccess
	if err != nil {
		status = statusError
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "delete", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "delete").Observe(duration)

	return err
}

// UpdateRecord updates an existing DNS record in place if the provider supports
// native updates. If the provider doesn't implement the Updater interface, this
// method falls back to delete+create.
//
// This should be used when only the target, TTL, or SRV data has changed and
// we want to avoid the brief DNS gap that delete+create would cause.
func (pi *ProviderInstance) UpdateRecord(ctx context.Context, existing, desired Record) error {
	// Check if provider implements native update
	if updater, ok := pi.Provider.(Updater); ok {
		start := time.Now()
		err := updater.Update(ctx, existing, desired)
		duration := time.Since(start).Seconds()

		status := statusSuccess
		if err != nil {
			status = statusError
		}

		metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "update", status).Inc()
		metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "update").Observe(duration)

		return err
	}

	// Fallback: delete + create
	// Delete the existing record
	start := time.Now()
	if err := pi.Provider.Delete(ctx, existing); err != nil {
		metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "delete", statusError).Inc()
		metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "delete").Observe(time.Since(start).Seconds())
		// If delete fails with not found, continue to create (record may have been manually deleted)
		if !errors.Is(err, ErrNotFound) {
			return err
		}
	} else {
		metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "delete", statusSuccess).Inc()
		metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "delete").Observe(time.Since(start).Seconds())
	}

	// Create the new record
	start = time.Now()
	err := pi.Provider.Create(ctx, desired)
	duration := time.Since(start).Seconds()

	status := statusSuccess
	if err != nil {
		status = statusError
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "create", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "create").Observe(duration)

	return err
}

// GetExistingRecords returns all A/CNAME records that exist for a given hostname.
// This is used by the reconciler to detect if the target has changed or if there's
// a type conflict before creating a new record.
func (pi *ProviderInstance) GetExistingRecords(ctx context.Context, hostname string) ([]Record, error) {
	start := time.Now()
	allRecords, err := pi.Provider.List(ctx)
	duration := time.Since(start).Seconds()

	status := statusSuccess
	if err != nil {
		status = statusError
		metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "list", status).Inc()
		metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "list").Observe(duration)
		return nil, err
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "list", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "list").Observe(duration)

	var matching []Record
	for _, r := range allRecords {
		// Case-insensitive hostname comparison per RFC 1035 Section 2.3.3
		if strings.EqualFold(r.Hostname, hostname) {
			switch r.Type {
			case RecordTypeA, RecordTypeAAAA, RecordTypeCNAME, RecordTypeSRV, RecordTypeHTTPS:
				matching = append(matching, r)
			case RecordTypeTXT:
				// Skip TXT records (ownership markers)
			}
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

	status := statusSuccess
	if err != nil {
		status = statusError
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "delete", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "delete").Observe(duration)

	return err
}

// DeleteSRVRecord removes a specific SRV record by hostname, target, and SRV data.
// This is needed because multiple SRV records can have the same target but different
// priority/weight/port values.
func (pi *ProviderInstance) DeleteSRVRecord(ctx context.Context, hostname string, target string, srvData *SRVData) error {
	record := Record{
		Hostname: hostname,
		Type:     RecordTypeSRV,
		Target:   target,
		SRV:      srvData,
	}

	start := time.Now()
	err := pi.Provider.Delete(ctx, record)
	duration := time.Since(start).Seconds()

	status := statusSuccess
	if err != nil {
		status = statusError
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "delete", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "delete").Observe(duration)

	return err
}

// CreateOwnershipRecord creates a TXT record to mark ownership of a hostname.
// The TXT record is named "_dnsweaver.{hostname}" with a value that includes
// the instance ID when configured for multi-instance coordination.
// If metadata is non-nil, it is serialized into the TXT value for persistence.
func (pi *ProviderInstance) CreateOwnershipRecord(ctx context.Context, hostname string, metadata map[string]string) error {
	record := OwnershipRecord(hostname, pi.TTL, pi.InstanceID, metadata)

	start := time.Now()
	err := pi.Provider.Create(ctx, record)
	duration := time.Since(start).Seconds()

	status := statusSuccess
	if err != nil {
		// Ignore conflict errors - ownership record may already exist
		if IsConflict(err) {
			return nil
		}
		status = statusError
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "create_ownership", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "create_ownership").Observe(duration)

	return err
}

// DeleteOwnershipRecord removes the TXT ownership record for a hostname.
func (pi *ProviderInstance) DeleteOwnershipRecord(ctx context.Context, hostname string) error {
	record := OwnershipRecord(hostname, pi.TTL, pi.InstanceID, nil)

	start := time.Now()
	err := pi.Provider.Delete(ctx, record)
	duration := time.Since(start).Seconds()

	status := statusSuccess
	if err != nil {
		status = statusError
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "delete_ownership", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "delete_ownership").Observe(duration)

	return err
}

// HasOwnershipRecord checks if an ownership TXT record exists for the given hostname
// that matches this instance's ID. In multi-instance mode, only records with the
// matching instance ID are considered owned by this instance.
func (pi *ProviderInstance) HasOwnershipRecord(ctx context.Context, hostname string) (bool, error) {
	ownershipName := OwnershipRecordName(hostname)

	start := time.Now()
	records, err := pi.Provider.List(ctx)
	duration := time.Since(start).Seconds()

	status := statusSuccess
	if err != nil {
		status = statusError
		metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "list", status).Inc()
		metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "list").Observe(duration)
		return false, err
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "list", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "list").Observe(duration)

	for _, r := range records {
		if r.Hostname == ownershipName && r.Type == RecordTypeTXT && MatchesOwnership(r.Target, pi.InstanceID) {
			return true, nil
		}
	}

	return false, nil
}

// RecoveredHostname represents a hostname recovered from ownership TXT records,
// along with any metadata that was persisted in the ownership value.
type RecoveredHostname struct {
	// Hostname is the DNS hostname extracted from the ownership record name.
	Hostname string

	// Metadata contains key-value pairs recovered from the ownership TXT value.
	// For old-format records (no metadata), this will be nil.
	Metadata map[string]string
}

// RecoverOwnedHostnames scans the provider for ownership TXT records and returns
// the list of hostnames (with metadata) that this dnsweaver instance previously
// created. In multi-instance mode, only records matching this instance's ID are
// recovered. This is used on startup to recover state and enable orphan cleanup.
func (pi *ProviderInstance) RecoverOwnedHostnames(ctx context.Context) ([]RecoveredHostname, error) {
	start := time.Now()
	records, err := pi.Provider.List(ctx)
	duration := time.Since(start).Seconds()

	status := statusSuccess
	if err != nil {
		status = statusError
		metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "list", status).Inc()
		metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "list").Observe(duration)
		return nil, err
	}

	metrics.ProviderAPIRequestsTotal.WithLabelValues(pi.Name(), "list", status).Inc()
	metrics.ProviderAPIDuration.WithLabelValues(pi.Name(), "list").Observe(duration)

	var hostnames []RecoveredHostname
	for _, r := range records {
		// Look for ownership TXT records that match our instance
		if r.Type == RecordTypeTXT && IsOwnershipRecord(r.Hostname) {
			isOwned, _, metadata := ParseOwnershipValue(r.Target)
			if !isOwned {
				continue
			}
			// In multi-instance mode, verify instance ID matches
			if !MatchesOwnership(r.Target, pi.InstanceID) {
				continue
			}
			hostname := ExtractHostnameFromOwnership(r.Hostname)
			if hostname != "" {
				hostnames = append(hostnames, RecoveredHostname{
					Hostname: hostname,
					Metadata: metadata,
				})
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

	status := statusSuccess
	healthy := float64(1)
	if err != nil {
		status = statusError
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

	// Mode is the operational mode (managed, authoritative, additive).
	// Defaults to "managed" if not set.
	Mode OperationalMode

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

	// MetadataFilters scopes this instance to hostnames whose Metadata map
	// satisfies every key in this map. See ProviderInstance.MetadataFilters
	// for the full semantics. nil/empty means no metadata filtering.
	MetadataFilters map[string][]string

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
