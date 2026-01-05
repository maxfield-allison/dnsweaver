// Package provider defines the interface that all DNS providers must implement.
package provider

import "context"

// RecordType represents the type of DNS record.
type RecordType string

const (
	RecordTypeA     RecordType = "A"
	RecordTypeCNAME RecordType = "CNAME"
)

// Record represents a DNS record to be managed.
type Record struct {
	Hostname   string
	Type       RecordType
	Target     string // IP for A records, hostname for CNAME
	TTL        int
	ProviderID string // Provider-specific record identifier
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

// TODO: Implement in Issue #3 - Provider interface
// - Full interface documentation
// - Error types (ErrNotFound, ErrConflict, etc.)
// - Record comparison helpers
