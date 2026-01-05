// Package source defines the interface for hostname extraction from container labels.
package source

import "context"

// Hostname represents a hostname extracted from container labels.
type Hostname struct {
	Name        string            // The hostname (e.g., "app.example.com")
	ContainerID string            // Docker container/service ID
	Labels      map[string]string // Original labels for reference
}

// Source defines the interface for hostname extraction.
// Each source implementation (Traefik, Caddy, etc.) must satisfy this interface.
type Source interface {
	// Name returns the source name (e.g., "traefik").
	Name() string

	// Extract parses container labels and returns discovered hostnames.
	Extract(ctx context.Context, labels map[string]string) ([]Hostname, error)
}

// TODO: Implement in Issue #2 - Source interface
// - Full interface documentation
// - Error handling patterns
// - Label parsing helpers
