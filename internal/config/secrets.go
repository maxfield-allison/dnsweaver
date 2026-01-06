// Package config handles loading and validation of DNSWeaver configuration.
package config

import (
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
// The file contents are trimmed of leading/trailing whitespace.
func getEnvOrFile(directKey, fileKey string) string {
	// Check for file-based secret first (Docker secrets pattern)
	if filePath := os.Getenv(fileKey); filePath != "" {
		content, err := os.ReadFile(filePath)
		if err == nil {
			return strings.TrimSpace(string(content))
		}
		// If file read fails, fall through to direct value
		// This could be logged as a warning in the future
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
