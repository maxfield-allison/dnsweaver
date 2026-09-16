// Package config handles loading and validation of DNSWeaver configuration.
package config

import (
	"fmt"
	"os"
	"strings"
)

// getEnv retrieves an environment variable value.
func getEnv(key string) string {
	return os.Getenv(key)
}

// readEnvOrFile returns the selected value, whether an override was explicitly
// configured, and any file-read error. The configured bit distinguishes an
// intentionally empty secret file from no override at all.
func readEnvOrFile(directKey, fileKey string) (string, bool, error) {
	if filePath := os.Getenv(fileKey); filePath != "" {
		content, err := os.ReadFile(filePath)
		if err != nil {
			return "", true, fmt.Errorf("reading %s path %q: %w", fileKey, filePath, err)
		}
		return strings.TrimSpace(string(content)), true, nil
	}
	value, configured := os.LookupEnv(directKey)
	return value, configured, nil
}

// getEnvWithFileFallback retrieves a value supporting the _FILE suffix pattern.
// Given a base key like "TOKEN", it checks:
//  1. TOKEN_FILE - reads file contents if set
//  2. TOKEN - returns direct value if set
func getEnvWithFileFallback(prefix, key string) (string, bool, error) {
	return readEnvOrFile(prefix+key, prefix+key+"_FILE")
}

// parseBool parses a boolean string, returning defaultValue on parse failure.
// Accepts: true/false, 1/0, yes/no, on/off (case-insensitive).
func parseBool(s string, defaultValue bool) bool {
	if value, ok := parseBoolValue(s); ok {
		return value
	}
	return defaultValue
}

// parseBoolValue parses the accepted boolean spellings and reports whether
// the input was valid. Callers for new safety-sensitive settings use the ok
// result to fail fast instead of silently falling back.
func parseBoolValue(s string) (bool, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	default:
		return false, false
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
