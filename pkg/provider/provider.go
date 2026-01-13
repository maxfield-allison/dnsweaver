// Package provider defines the interface that all DNS providers must implement.
package provider

import "context"

// RecordType represents the type of DNS record.
type RecordType string

const (
	RecordTypeA     RecordType = "A"
	RecordTypeAAAA  RecordType = "AAAA"
	RecordTypeCNAME RecordType = "CNAME"
	RecordTypeTXT   RecordType = "TXT"
	RecordTypeSRV   RecordType = "SRV"
)

// OwnershipPrefix is the default prefix for ownership TXT records.
const OwnershipPrefix = "_dnsweaver"

// OwnershipValue is the content of ownership TXT records.
const OwnershipValue = "heritage=dnsweaver"

// SRVData contains SRV record-specific fields.
// Used when Type is RecordTypeSRV.
type SRVData struct {
	Priority uint16 // Lower values = higher priority (0-65535)
	Weight   uint16 // Load balancing among same-priority servers (0-65535)
	Port     uint16 // TCP/UDP port number (1-65535)
}

// Record represents a DNS record to be managed.
type Record struct {
	Hostname   string
	Type       RecordType
	Target     string // IP for A/AAAA, hostname for CNAME/SRV target
	TTL        int
	ProviderID string   // Provider-specific record identifier
	SRV        *SRVData // SRV-specific data (only set when Type is SRV)
}

// Capabilities describes a provider's feature support.
// Used by the reconciler to adapt behavior based on provider limitations.
type Capabilities struct {
	// SupportsOwnershipTXT indicates if the provider can create TXT records
	// for ownership tracking. File-based providers (dnsmasq) typically cannot.
	SupportsOwnershipTXT bool

	// SupportsNativeUpdate indicates if the provider has a native update operation.
	// If false, updates require delete+create. Providers with native update should
	// also implement the Updater interface.
	SupportsNativeUpdate bool

	// SupportedRecordTypes lists the DNS record types this provider can manage.
	// Used to filter operations in authoritative mode and validate requested records.
	SupportedRecordTypes []RecordType
}

// SupportsRecordType returns true if the provider supports the given record type.
func (c Capabilities) SupportsRecordType(rt RecordType) bool {
	for _, t := range c.SupportedRecordTypes {
		if t == rt {
			return true
		}
	}
	return false
}

// Provider defines the interface for DNS providers.
// Each provider implementation (Technitium, Cloudflare, etc.) must satisfy this interface.
type Provider interface {
	// Name returns the provider instance name (e.g., "internal-dns").
	Name() string

	// Type returns the provider type (e.g., "technitium", "cloudflare").
	Type() string

	// Ping checks connectivity to the provider.
	Ping(ctx context.Context) error

	// Capabilities returns the provider's feature support.
	// Used by the reconciler to adapt behavior based on provider limitations.
	Capabilities() Capabilities

	// List returns all managed records in the configured zone.
	List(ctx context.Context) ([]Record, error)

	// Create adds a new DNS record.
	Create(ctx context.Context, record Record) error

	// Delete removes a DNS record.
	Delete(ctx context.Context, record Record) error
}

// Updater is an optional interface that providers can implement to support
// native in-place record updates. This is more efficient than delete+create
// and avoids brief DNS gaps when changing record values.
//
// The reconciler will check if a provider implements Updater and use it when
// available. If not, the reconciler falls back to delete+create.
//
// Providers that implement Updater should also set Capabilities().SupportsNativeUpdate = true.
type Updater interface {
	// Update modifies an existing DNS record in place.
	// The existing record is identified by its current values (hostname, type, target).
	// The desired record contains the new values to apply.
	//
	// Implementations should:
	// - Only modify fields that differ between existing and desired
	// - Return ErrRecordNotFound if the existing record doesn't exist
	// - Be idempotent (calling with identical records is a no-op)
	Update(ctx context.Context, existing, desired Record) error
}

// RecordEquals returns true if two records are logically equal.
// Provider-specific IDs are not compared.
func RecordEquals(a, b Record) bool {
	if a.Hostname != b.Hostname || a.Type != b.Type || a.Target != b.Target || a.TTL != b.TTL {
		return false
	}

	// For SRV records, also compare SRV-specific data
	if a.Type == RecordTypeSRV {
		if a.SRV == nil && b.SRV == nil {
			return true
		}
		if a.SRV == nil || b.SRV == nil {
			return false
		}
		return a.SRV.Priority == b.SRV.Priority &&
			a.SRV.Weight == b.SRV.Weight &&
			a.SRV.Port == b.SRV.Port
	}

	return true
}

// OwnershipRecordName returns the TXT record name for ownership tracking.
// Example: "app.example.com" -> "_dnsweaver.app.example.com"
func OwnershipRecordName(hostname string) string {
	return OwnershipPrefix + "." + hostname
}

// IsOwnershipRecord returns true if the hostname is an ownership TXT record.
func IsOwnershipRecord(hostname string) bool {
	return len(hostname) > len(OwnershipPrefix)+1 &&
		hostname[:len(OwnershipPrefix)+1] == OwnershipPrefix+"."
}

// ExtractHostnameFromOwnership extracts the original hostname from an ownership record name.
// Example: "_dnsweaver.app.example.com" -> "app.example.com"
// Returns empty string if the hostname is not an ownership record.
func ExtractHostnameFromOwnership(ownershipName string) string {
	if !IsOwnershipRecord(ownershipName) {
		return ""
	}
	return ownershipName[len(OwnershipPrefix)+1:]
}

// OwnershipRecord creates a TXT record for ownership tracking.
func OwnershipRecord(hostname string, ttl int) Record {
	return Record{
		Hostname: OwnershipRecordName(hostname),
		Type:     RecordTypeTXT,
		Target:   OwnershipValue,
		TTL:      ttl,
	}
}
