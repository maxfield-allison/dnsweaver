package opnsense

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Engine identifies which DNS resolver on OPNsense this instance targets.
type Engine string

const (
	// EngineUnbound is OPNsense's default recursive resolver.
	EngineUnbound Engine = "unbound"
	// EngineDnsmasq is OPNsense's alternative resolver (24.7+).
	EngineDnsmasq Engine = "dnsmasq"
)

// ReconfigureMode controls when the provider calls the resolver's
// reconfigure endpoint (which fully reloads the daemon).
type ReconfigureMode string

const (
	// ReconfigureModePerWrite reloads after every Create/Delete. Correct
	// but expensive on large batches; acceptable for typical homelab scale.
	ReconfigureModePerWrite ReconfigureMode = "per_write"
	// ReconfigureModeNever disables automatic reload. Operators must
	// reload out of band (cron, manual, etc). Useful when many providers
	// share the same OPNsense host and reload is coordinated externally.
	ReconfigureModeNever ReconfigureMode = "never"
)

// DefaultTTL is informational only — OPNsense host overrides have no
// per-record TTL; the resolver uses its global TTL.
const DefaultTTL = 300

// Config holds OPNsense-specific configuration.
type Config struct {
	// URL is the OPNsense base URL (e.g. "https://opnsense.internal").
	URL string
	// APIKey is the OPNsense API key.
	APIKey string
	// APISecret is the OPNsense API secret.
	APISecret string
	// Engine selects the DNS resolver backend (unbound or dnsmasq).
	Engine Engine
	// Zone optionally filters records to a specific DNS zone.
	Zone string
	// TTL is informational only.
	TTL int
	// ReconfigureMode controls automatic resolver reload after mutations.
	ReconfigureMode ReconfigureMode
}

// Validate checks that all required configuration is present and correct.
func (c *Config) Validate() error {
	var errs []string

	if c.URL == "" {
		errs = append(errs, "URL is required")
	} else {
		parsed, err := url.Parse(c.URL)
		switch {
		case err != nil:
			errs = append(errs, fmt.Sprintf("invalid URL: %v", err))
		case parsed.Scheme != "http" && parsed.Scheme != "https":
			errs = append(errs, "URL must start with http:// or https://")
		case parsed.User != nil:
			errs = append(errs, "URL must not contain embedded credentials")
		}
	}

	if c.APIKey == "" {
		errs = append(errs, "API_KEY is required")
	}
	if c.APISecret == "" {
		errs = append(errs, "API_SECRET is required")
	}

	switch c.Engine {
	case EngineUnbound, EngineDnsmasq:
		// ok
	case "":
		errs = append(errs, "ENGINE is required (unbound or dnsmasq)")
	default:
		errs = append(errs, fmt.Sprintf("ENGINE must be %q or %q, got %q",
			EngineUnbound, EngineDnsmasq, c.Engine))
	}

	switch c.ReconfigureMode {
	case ReconfigureModePerWrite, ReconfigureModeNever:
		// ok
	default:
		errs = append(errs, fmt.Sprintf("RECONFIGURE_MODE must be %q or %q, got %q",
			ReconfigureModePerWrite, ReconfigureModeNever, c.ReconfigureMode))
	}

	if c.TTL < 0 {
		errs = append(errs, "TTL must be non-negative")
	}

	if len(errs) > 0 {
		return fmt.Errorf("opnsense config validation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

// LoadConfig loads OPNsense configuration from environment variables.
// Environment variable pattern: DNSWEAVER_{INSTANCE_NAME}_{SETTING}
//
// Instance names are normalized: lowercase with hyphens becomes uppercase
// with underscores. Example: "opnsense-fw" looks for DNSWEAVER_OPNSENSE_FW_*.
func LoadConfig(instanceName string) (*Config, error) {
	prefix := envPrefix(instanceName)

	config := &Config{
		URL:             getEnv(prefix + "URL"),
		APIKey:          getEnvOrFile(prefix+"API_KEY", prefix+"API_KEY_FILE"),
		APISecret:       getEnvOrFile(prefix+"API_SECRET", prefix+"API_SECRET_FILE"),
		Engine:          engineFromString(getEnv(prefix + "ENGINE")),
		Zone:            getEnv(prefix + "ZONE"),
		TTL:             DefaultTTL,
		ReconfigureMode: reconfigureModeFromString(getEnv(prefix + "RECONFIGURE_MODE")),
	}

	if ttlStr := getEnv(prefix + "TTL"); ttlStr != "" {
		ttl, err := strconv.Atoi(ttlStr)
		if err != nil {
			return nil, fmt.Errorf("invalid TTL value %q: %w", ttlStr, err)
		}
		config.TTL = ttl
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration for %s: %w", instanceName, err)
	}
	return config, nil
}

// LoadConfigFromMap creates a Config from a map of key-value pairs, as
// produced by the framework config loader.
func LoadConfigFromMap(name string, m map[string]string) (*Config, error) {
	config := &Config{
		URL:             getMapValue(m, "URL"),
		APIKey:          getMapValue(m, "API_KEY"),
		APISecret:       getMapValue(m, "API_SECRET"),
		Engine:          engineFromString(getMapValue(m, "ENGINE")),
		Zone:            getMapValue(m, "ZONE"),
		TTL:             DefaultTTL,
		ReconfigureMode: reconfigureModeFromString(getMapValue(m, "RECONFIGURE_MODE")),
	}

	if ttlStr := getMapValue(m, "TTL"); ttlStr != "" {
		ttl, err := strconv.Atoi(ttlStr)
		if err != nil {
			return nil, fmt.Errorf("invalid TTL value %q: %w", ttlStr, err)
		}
		config.TTL = ttl
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration for %s: %w", name, err)
	}
	return config, nil
}

// engineFromString parses an engine string, defaulting to unbound when empty
// so operators who omit the setting get OPNsense's default resolver.
func engineFromString(s string) Engine {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return EngineUnbound
	case string(EngineUnbound):
		return EngineUnbound
	case string(EngineDnsmasq):
		return EngineDnsmasq
	default:
		// Return the raw value so Validate() can report it accurately.
		return Engine(s)
	}
}

// reconfigureModeFromString parses a reconfigure mode string, defaulting
// to per_write when empty.
func reconfigureModeFromString(s string) ReconfigureMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return ReconfigureModePerWrite
	case string(ReconfigureModePerWrite):
		return ReconfigureModePerWrite
	case string(ReconfigureModeNever):
		return ReconfigureModeNever
	default:
		return ReconfigureMode(s)
	}
}

func envPrefix(instanceName string) string {
	normalized := strings.ToUpper(strings.ReplaceAll(instanceName, "-", "_"))
	return "DNSWEAVER_" + normalized + "_"
}

func getEnv(key string) string {
	return os.Getenv(key)
}

func getEnvOrFile(envKey, fileKey string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	if filePath := os.Getenv(fileKey); filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	}
	return ""
}

// getMapValue looks up a key case-insensitively.
func getMapValue(m map[string]string, key string) string {
	if v, ok := m[key]; ok {
		return v
	}
	if v, ok := m[strings.ToUpper(key)]; ok {
		return v
	}
	if v, ok := m[strings.ToLower(key)]; ok {
		return v
	}
	return ""
}
