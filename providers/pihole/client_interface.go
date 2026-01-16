// Package pihole implements the DNSWeaver provider interface for Pi-hole DNS.
package pihole

import (
	"context"
)

// DNSClient defines the interface for Pi-hole DNS operations.
// Both V5 (legacy) and V6 (new) API clients implement this interface,
// allowing the provider to work with either version transparently.
type DNSClient interface {
	// List retrieves all DNS records from Pi-hole.
	// Returns custom DNS entries (A/AAAA) and CNAME records.
	List(ctx context.Context) ([]piholeRecord, error)

	// Create adds a new DNS record to Pi-hole.
	// Supported types: A, AAAA, CNAME.
	Create(ctx context.Context, record piholeRecord) error

	// Delete removes a DNS record from Pi-hole.
	// Returns nil if the record doesn't exist (idempotent).
	Delete(ctx context.Context, record piholeRecord) error
}

// Ensure APIClient implements DNSClient.
var _ DNSClient = (*APIClient)(nil)
