package source

import "time"

// FileDiscoveryConfig holds configuration for file-based hostname discovery.
//
// Design principle: Presence implies intent. Setting FilePaths enables
// file discovery - no separate Enabled flag needed.
type FileDiscoveryConfig struct {
	// FilePaths is a list of paths to scan for hostnames.
	// Supports individual files or directories.
	// Empty means file discovery is disabled.
	FilePaths []string

	// FilePattern is a glob pattern for files to include.
	// Default depends on source type (e.g., "*.yml" for Traefik).
	FilePattern string

	// PollInterval is how often to check files for changes.
	// Default is 60s. Can be safely lowered to 5s.
	// Zero means polling is disabled (inotify only).
	PollInterval time.Duration

	// WatchMethod controls how file changes are detected.
	// Values: "auto", "inotify", "poll"
	// Default is "auto" (tries inotify, falls back to poll for network mounts).
	WatchMethod string
}

// DefaultFileDiscoveryConfig returns a config with sensible defaults.
func DefaultFileDiscoveryConfig() FileDiscoveryConfig {
	return FileDiscoveryConfig{
		FilePaths:    nil, // Disabled by default
		FilePattern:  "",  // Source-specific default
		PollInterval: 60 * time.Second,
		WatchMethod:  "auto",
	}
}

// IsEnabled returns true if file discovery is configured.
// Per design: presence of file paths implies enablement.
func (c FileDiscoveryConfig) IsEnabled() bool {
	return len(c.FilePaths) > 0
}

// WatchMethodType represents the method used to detect file changes.
type WatchMethodType string

const (
	// WatchMethodAuto tries inotify first, falls back to poll.
	WatchMethodAuto WatchMethodType = "auto"

	// WatchMethodInotify uses Linux inotify for instant change detection.
	// Fails on network mounts (NFS, CIFS, Ceph).
	WatchMethodInotify WatchMethodType = "inotify"

	// WatchMethodPoll periodically checks file mtimes.
	// Works everywhere, including network mounts.
	WatchMethodPoll WatchMethodType = "poll"
)

// ParseWatchMethod parses a watch method string.
func ParseWatchMethod(s string) WatchMethodType {
	switch s {
	case "inotify":
		return WatchMethodInotify
	case "poll":
		return WatchMethodPoll
	default:
		return WatchMethodAuto
	}
}
