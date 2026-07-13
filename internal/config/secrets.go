// Package config handles loading and validation of DNSWeaver configuration.
package config

import (
	"log/slog"
	"os"
	"strings"
)

// getEnv retrieves an environment variable value.
func getEnv(key string) string {
	return os.Getenv(key)
}

// getEnvOrFile retrieves a value from either a direct environment variable
// or a file path specified by the file key (Docker secrets pattern).
//
// If both are set, the file takes precedence. This allows local development
// with direct values while production uses Docker secrets.
//
// If the file key is set but the file cannot be read, this returns an empty
// string (hard failure) rather than silently falling through to the direct
// env var. This prevents silent misconfiguration when a secret file path is
// explicitly configured but points to an unreadable location.
//
// The file contents are trimmed of leading/trailing whitespace.
func getEnvOrFile(directKey, fileKey string) string {
	// Check for file-based secret first (Docker secrets pattern)
	if filePath := os.Getenv(fileKey); filePath != "" {
		content, err := os.ReadFile(filePath)
		if err == nil {
			return strings.TrimSpace(string(content))
		}
		// File key was explicitly set but file can't be read — this is a
		// configuration error. Return empty rather than silently falling
		// through to the direct env var, which would mask the problem.
		slog.Warn("secret file specified but unreadable, ignoring direct env var",
			slog.String("file_key", fileKey),
			slog.String("file_path", filePath),
			slog.String("error", err.Error()),
		)
		return ""
	}

	return os.Getenv(directKey)
}

// getEnvWithFileFallback retrieves a value supporting the _FILE suffix pattern.
// Given a base key like "TOKEN", it checks:
//  1. TOKEN_FILE - reads file contents if set
//  2. TOKEN - returns direct value if set
func getEnvWithFileFallback(prefix, key string) string {
	return getEnvOrFile(prefix+key, prefix+key+"_FILE")
}

// parseBool parses a boolean string, returning defaultValue on parse failure.
// Accepts: true/false, 1/0, yes/no, on/off (case-insensitive).
func parseBool(s string, defaultValue bool) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return defaultValue
	}
}

// splitCommaList splits a comma-separated string into a trimmed, non-empty
// slice. Returns nil for an empty or all-whitespace input.
func splitCommaList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// normalizeInstanceName converts an instance name to environment variable format.
// Example: "internal-dns" → "INTERNAL_DNS"
func normalizeInstanceName(name string) string {
	normalized := strings.ToUpper(name)
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return normalized
}

// envPrefix creates the full environment variable prefix for a provider instance.
// Example: "internal-dns" → "DNSWEAVER_INTERNAL_DNS_"
func envPrefix(instanceName string) string {
	return "DNSWEAVER_" + normalizeInstanceName(instanceName) + "_"
}
