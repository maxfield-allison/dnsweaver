package cloudflare

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// DefaultTTL is the default TTL for Cloudflare DNS records.
// Cloudflare's minimum TTL is 60 seconds (1 = "automatic" which is 300).
const DefaultTTL = 300

// Config holds Cloudflare-specific configuration.
type Config struct {
	Token   string // API token (Bearer authentication)
	ZoneID  string // Zone ID (optional if Zone is set)
	Zone    string // Zone name for lookup (used if ZoneID is empty)
	TTL     int    // Record TTL (defaults to DefaultTTL)
	Proxied bool   // Whether to proxy records through Cloudflare (default: false)
}

// Validate checks that all required configuration is present.
func (c *Config) Validate() error {
	var errs []string

	if c.Token == "" {
		errs = append(errs, "TOKEN is required")
	}
	// Either ZoneID or Zone must be set
	if c.ZoneID == "" && c.Zone == "" {
		errs = append(errs, "ZONE_ID or ZONE is required")
	}
	if c.TTL < 0 {
		errs = append(errs, "TTL must be non-negative")
	}
	// Cloudflare minimum TTL is 60 seconds (1 = automatic)
	if c.TTL > 0 && c.TTL < 60 && c.TTL != 1 {
		errs = append(errs, "TTL must be at least 60 seconds (or 1 for automatic)")
	}

	if len(errs) > 0 {
		return fmt.Errorf("cloudflare config validation failed: %s", strings.Join(errs, "; "))
	}

	return nil
}

// LoadConfig loads Cloudflare configuration from environment variables.
// Environment variable pattern: DNSWEAVER_{INSTANCE_NAME}_{SETTING}
//
// Instance names are normalized: lowercase with hyphens becomes uppercase with underscores.
// Example: "public-dns" looks for DNSWEAVER_PUBLIC_DNS_*
//
// Supported settings:
//   - TOKEN: API token (required, supports _FILE suffix for Docker secrets)
//   - ZONE_ID: Zone ID (optional if ZONE is set)
//   - ZONE: Zone name for lookup (optional if ZONE_ID is set)
//   - TTL: Record TTL (optional, defaults to 300)
//   - PROXIED: Enable Cloudflare proxy (optional, defaults to false)
func LoadConfig(instanceName string) (*Config, error) {
	prefix := envPrefix(instanceName)

	config := &Config{
		Token:   getEnvOrFile(prefix+"TOKEN", prefix+"TOKEN_FILE"),
		ZoneID:  getEnv(prefix + "ZONE_ID"),
		Zone:    getEnv(prefix + "ZONE"),
		TTL:     DefaultTTL,
		Proxied: false,
	}

	// Parse optional TTL
	if ttlStr := getEnv(prefix + "TTL"); ttlStr != "" {
		ttl, err := strconv.Atoi(ttlStr)
		if err != nil {
			return nil, fmt.Errorf("invalid TTL value %q: %w", ttlStr, err)
		}
		config.TTL = ttl
	}

	// Parse optional PROXIED flag
	if proxiedStr := getEnv(prefix + "PROXIED"); proxiedStr != "" {
		config.Proxied = parseBool(proxiedStr)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration for %s: %w", instanceName, err)
	}

	return config, nil
}

// envPrefix converts an instance name to an environment variable prefix.
// Example: "public-dns" → "DNSWEAVER_PUBLIC_DNS_"
func envPrefix(instanceName string) string {
	normalized := strings.ToUpper(instanceName)
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return "DNSWEAVER_" + normalized + "_"
}

// getEnv retrieves an environment variable value.
func getEnv(key string) string {
	return os.Getenv(key)
}

// getEnvOrFile retrieves a value from either a direct environment variable
// or a file path specified by the file key (Docker secrets pattern).
//
// If both are set, the file takes precedence.
// The file contents are trimmed of leading/trailing whitespace.
func getEnvOrFile(directKey, fileKey string) string {
	// Check for file-based secret first (Docker secrets pattern)
	if filePath := os.Getenv(fileKey); filePath != "" {
		content, err := os.ReadFile(filePath)
		if err == nil {
			return strings.TrimSpace(string(content))
		}
		// If file read fails, fall through to direct value
	}

	return os.Getenv(directKey)
}

// parseBool parses a boolean string.
// Accepts: true/false, 1/0, yes/no (case-insensitive).
func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}
