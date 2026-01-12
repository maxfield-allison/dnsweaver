// Package source defines the interface for hostname extraction from container labels
// and static configuration files.
//
// Sources are responsible for understanding how different reverse proxies
// (Traefik, Caddy, Nginx, etc.) store hostname information. Each source
// implementation knows how to parse its specific format from both:
//   - Docker container/service labels (Extract)
//   - Static configuration files (Discover)
//
// Example usage:
//
//	registry := source.NewRegistry(logger)
//	registry.Register(traefik.New())
//
//	// When a container event occurs (labels):
//	hostnames := registry.ExtractAll(ctx, container.Labels)
//	for _, h := range hostnames {
//	    log.Printf("Discovered from labels: %s from %s", h.Name, h.Source)
//	}
//
//	// For static file discovery:
//	hostnames := registry.DiscoverAll(ctx)
//	for _, h := range hostnames {
//	    log.Printf("Discovered from files: %s from %s", h.Name, h.Source)
//	}
package source

import "context"

// Source defines the interface for hostname extraction from container labels
// and optional file-based discovery.
//
// Each source implementation (Traefik, Caddy, etc.) must satisfy this interface.
// Sources may support one or both discovery methods:
//   - Extract: Parse hostnames from Docker container/service labels
//   - Discover: Parse hostnames from static configuration files
//
// Design principle: Presence implies intent. If FILE_PATHS is configured for a
// source, file discovery is enabled. No separate ENABLED flag needed.
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

	// Discover finds hostnames from configured file paths.
	//
	// This method is used for static configuration file discovery (e.g., Traefik
	// YAML rules, Caddyfile, nginx.conf). Sources that don't support file discovery
	// should return nil, nil.
	//
	// Returns:
	//   - Slice of discovered hostnames (may be empty if none found)
	//   - nil, nil if file discovery is not configured for this source
	//   - Error if configured paths cannot be read or parsed
	//
	// Implementation notes:
	//   - Only parse relevant sections (e.g., Traefik: http.routers.*.rule only)
	//   - Handle missing files gracefully (log warning, don't fail)
	//   - Support glob patterns in file paths
	Discover(ctx context.Context) ([]Hostname, error)

	// SupportsDiscovery returns true if this source has file paths configured.
	//
	// This is used by the reconciler to determine which sources need file-based
	// discovery in addition to label extraction. Sources without file paths
	// configured should return false.
	SupportsDiscovery() bool
}
