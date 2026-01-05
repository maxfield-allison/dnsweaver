// Package source defines the interface for hostname extraction from container labels.
//
// Sources are responsible for understanding how different reverse proxies
// (Traefik, Caddy, Nginx, etc.) store hostname information in container labels.
// Each source implementation knows how to parse its specific label format.
//
// Example usage:
//
//	registry := source.NewRegistry(logger)
//	registry.Register(traefik.New())
//
//	// When a container event occurs:
//	hostnames := registry.ExtractAll(ctx, container.Labels)
//	for _, h := range hostnames {
//	    log.Printf("Discovered: %s from %s", h.Name, h.Source)
//	}
package source

import "context"

// Source defines the interface for hostname extraction from container labels.
// Each source implementation (Traefik, Caddy, etc.) must satisfy this interface.
//
// Sources should:
//   - Be stateless and safe for concurrent use
//   - Return empty slice (not error) if no hostnames found
//   - Only return error for parsing failures that indicate misconfiguration
type Source interface {
	// Name returns the source identifier (e.g., "traefik", "caddy").
	// This is used for logging and metrics.
	Name() string

	// Extract parses container labels and returns discovered hostnames.
	//
	// The labels map contains all labels from a Docker container or Swarm service.
	// Implementations should only look at labels relevant to their format and
	// ignore unrecognized labels.
	//
	// Returns:
	//   - Slice of discovered hostnames (may be empty if none found)
	//   - Error only if labels indicate malformed configuration
	//
	// Context is provided for future extensibility (e.g., label expansion
	// that requires external lookups).
	Extract(ctx context.Context, labels map[string]string) ([]Hostname, error)
}
