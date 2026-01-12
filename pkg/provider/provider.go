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

// Provider defines the interface for DNS providers.
// Each provider implementation (Technitium, Cloudflare, etc.) must satisfy this interface.
type Provider interface {
	// Name returns the provider instance name (e.g., "internal-dns").
	Name() string

	// Type returns the provider type (e.g., "technitium", "cloudflare").
	Type() string

	// Ping checks connectivity to the provider.
	Ping(ctx context.Context) error

	// List returns all managed records in the configured zone.
	List(ctx context.Context) ([]Record, error)

	// Create adds a new DNS record.
	Create(ctx context.Context, record Record) error

	// Delete removes a DNS record.
	Delete(ctx context.Context, record Record) error
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
