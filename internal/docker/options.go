package docker

import "log/slog"

// Option is a functional option for configuring the Client.
type Option func(*Client)

// WithHost sets the Docker host address.
// Examples:
//   - "unix:///var/run/docker.sock" (default Unix socket)
//   - "tcp://localhost:2375" (unencrypted TCP)
//   - "tcp://docker.example.com:2376" (TLS)
//
// If not set, the client uses the DOCKER_HOST environment variable
// or falls back to the default socket.
func WithHost(host string) Option {
	return func(c *Client) {
		c.host = host
	}
}

// WithMode sets the Docker operation mode.
//
// Modes:
//   - ModeAuto: Auto-detect based on Docker daemon state (default)
//   - ModeSwarm: Force Swarm mode (fails if Swarm is not active or node is not a manager)
//   - ModeStandalone: Force standalone mode (ignores Swarm state)
//
// Use ModeSwarm when you want to fail fast if Swarm is not available.
// Use ModeStandalone to explicitly ignore Swarm even if available.
func WithMode(mode Mode) Option {
	return func(c *Client) {
		c.mode = mode
	}
}

// WithLogger sets a custom slog.Logger for the client.
// If not set, slog.Default() is used.
func WithLogger(logger *slog.Logger) Option {
	return func(c *Client) {
		if logger != nil {
			c.logger = logger
		}
	}
}

// WithCleanupOnStop controls whether stopped containers are considered orphans.
//
// When true (default): Only running containers are discovered. Stopped containers
// are treated as orphans and their DNS records are cleaned up.
//
// When false: Both running and stopped containers are discovered. DNS records
// are only cleaned up when containers are removed, not when they're stopped.
// This is useful for maintenance windows or brief restarts.
func WithCleanupOnStop(cleanup bool) Option {
	return func(c *Client) {
		c.cleanupOnStop = cleanup
	}
}
