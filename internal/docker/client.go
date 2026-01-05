// Package docker provides the Docker API client for container and service inspection.
package docker

// Client wraps the Docker SDK client with DNSWeaver-specific functionality.
type Client struct {
	// TODO: Add fields in Issue #5 - Docker client implementation
}

// Mode represents the Docker operation mode (standalone vs Swarm).
type Mode string

const (
	ModeStandalone Mode = "standalone"
	ModeSwarm      Mode = "swarm"
	ModeAuto       Mode = "auto"
)

// TODO: Implement in Issue #5
// - NewClient() constructor
// - Mode detection (auto, swarm, standalone)
// - Container/service listing
// - Label extraction
