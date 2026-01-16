// Package pihole implements the DNSWeaver provider interface for Pi-hole DNS.
package pihole

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Mode defines how the provider interacts with Pi-hole.
type Mode string

const (
	// ModeAPI uses Pi-hole's Admin API (default for Pi-hole v5+).
	ModeAPI Mode = "api"

	// ModeFile uses dnsmasq-style config files (for containerized Pi-hole).
	ModeFile Mode = "file"
)

// DefaultTTL is the default TTL for Pi-hole DNS records.
// Note: Pi-hole doesn't use TTL for local records, but we track it for consistency.
const DefaultTTL = 300

// DefaultAPIPath is the default path for Pi-hole's admin API.
const DefaultAPIPath = "/admin/api.php"

// DefaultConfigDir is the default directory for Pi-hole custom DNS files.
const DefaultConfigDir = "/etc/pihole"

// DefaultConfigFile is the default filename for dnsweaver-managed records.
const DefaultConfigFile = "custom.list"

// DefaultReloadCommand is the default command to reload Pi-hole DNS.
const DefaultReloadCommand = "pihole restartdns reload-lists"

// Config holds Pi-hole-specific configuration.
type Config struct {
	// Mode determines how to interact with Pi-hole: "api" or "file"
	Mode Mode

	// API mode settings
	URL      string // Pi-hole admin URL (e.g., "http://pihole.local")
	Password string // Admin password for authentication

	// APIVersion allows forcing a specific API version (v5 or v6).
	// If empty (default), the version is auto-detected by probing the Pi-hole instance.
	// Valid values: "v5", "v6", "auto" (default), or empty (same as "auto")
	APIVersion string

	// File mode settings (reuses dnsmasq-style config)
	ConfigDir     string // Directory for config files (e.g., /etc/pihole)
	ConfigFile    string // Filename for custom DNS records (e.g., custom.list)
	ReloadCommand string // Command to reload Pi-hole (e.g., "pihole restartdns reload-lists")

	// Common settings
	Zone string // DNS zone for record filtering (optional)
	TTL  int    // Record TTL (for consistency with other providers)
}

// Validate checks that all required configuration is present.
func (c *Config) Validate() error {
	var errs []string

	switch c.Mode {
	case ModeAPI:
		if c.URL == "" {
			errs = append(errs, "URL is required for API mode")
		}
		if c.Password == "" {
			errs = append(errs, "PASSWORD is required for API mode")
		}
		// Validate API version if specified
		if c.APIVersion != "" && c.APIVersion != "auto" {
			v := strings.ToLower(c.APIVersion)
			if v != "v5" && v != "v6" {
				errs = append(errs, fmt.Sprintf("invalid API_VERSION %q: must be 'v5', 'v6', or 'auto'", c.APIVersion))
			}
		}
	case ModeFile:
		if c.ConfigDir == "" {
			errs = append(errs, "CONFIG_DIR is required for file mode")
		}
		if c.ConfigFile == "" {
			errs = append(errs, "CONFIG_FILE is required for file mode")
		}
		if c.ReloadCommand == "" {
			errs = append(errs, "RELOAD_COMMAND is required for file mode")
		}
	case "":
		errs = append(errs, "MODE is required (api or file)")
	default:
		errs = append(errs, fmt.Sprintf("invalid MODE %q: must be 'api' or 'file'", c.Mode))
	}

	if c.TTL < 0 {
		errs = append(errs, "TTL must be non-negative")
	}

	if len(errs) > 0 {
		return fmt.Errorf("pihole config validation failed: %s", strings.Join(errs, "; "))
	}

	return nil
}

// ConfigFilePath returns the full path to the custom DNS file.
func (c *Config) ConfigFilePath() string {
	return c.ConfigDir + "/" + c.ConfigFile
}

// LoadConfig loads Pi-hole configuration from environment variables.
// Environment variable pattern: DNSWEAVER_{INSTANCE_NAME}_{SETTING}
//
// Instance names are normalized: lowercase with hyphens becomes uppercase with underscores.
// Example: "pihole-dns" looks for DNSWEAVER_PIHOLE_DNS_*
//
// Supported settings:
//   - MODE: Operation mode - "api" (default) or "file"
//
// API mode settings:
//   - URL: Pi-hole admin URL (e.g., "http://pihole.local")
//   - PASSWORD: Admin password (supports _FILE suffix for Docker secrets)
//
// File mode settings:
//   - CONFIG_DIR: Directory for config files (default: /etc/pihole)
//   - CONFIG_FILE: Filename for custom DNS (default: custom.list)
//   - RELOAD_COMMAND: Command to reload Pi-hole (default: pihole restartdns reload-lists)
//
// Common settings:
//   - ZONE: DNS zone for record filtering (optional)
//   - TTL: Record TTL (optional, default: 300)
func LoadConfig(instanceName string) (*Config, error) {
	prefix := envPrefix(instanceName)

	modeStr := getEnvWithDefault(prefix+"MODE", string(ModeAPI))
	mode := Mode(strings.ToLower(modeStr))

	config := &Config{
		Mode:          mode,
		URL:           getEnv(prefix + "URL"),
		Password:      getEnvOrFile(prefix+"PASSWORD", prefix+"PASSWORD_FILE"),
		APIVersion:    getEnvWithDefault(prefix+"API_VERSION", "auto"),
		ConfigDir:     getEnvWithDefault(prefix+"CONFIG_DIR", DefaultConfigDir),
		ConfigFile:    getEnvWithDefault(prefix+"CONFIG_FILE", DefaultConfigFile),
		ReloadCommand: getEnvWithDefault(prefix+"RELOAD_COMMAND", DefaultReloadCommand),
		Zone:          getEnv(prefix + "ZONE"),
		TTL:           DefaultTTL,
	}

	// Parse optional TTL
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

// LoadConfigFromMap creates a Config from a map of key-value pairs.
// This is used by the provider registry to create instances from
// configuration that was already parsed from environment variables.
//
// Expected keys (case-insensitive):
//   - mode: "api" or "file"
//   - url: Pi-hole admin URL (API mode)
//   - password: Admin password (API mode)
//   - config_dir: Config directory (file mode)
//   - config_file: Config filename (file mode)
//   - reload_command: Reload command (file mode)
//   - zone: DNS zone
//   - ttl: Record TTL
func LoadConfigFromMap(name string, m map[string]string) (*Config, error) {
	modeStr := getMapValueWithDefault(m, "mode", string(ModeAPI))
	mode := Mode(strings.ToLower(modeStr))

	config := &Config{
		Mode:          mode,
		URL:           getMapValue(m, "url"),
		Password:      getMapValue(m, "password"),
		APIVersion:    getMapValueWithDefault(m, "api_version", "auto"),
		ConfigDir:     getMapValueWithDefault(m, "config_dir", DefaultConfigDir),
		ConfigFile:    getMapValueWithDefault(m, "config_file", DefaultConfigFile),
		ReloadCommand: getMapValueWithDefault(m, "reload_command", DefaultReloadCommand),
		Zone:          getMapValue(m, "zone"),
		TTL:           DefaultTTL,
	}

	// Parse optional TTL
	if ttlStr := getMapValue(m, "ttl"); ttlStr != "" {
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

// Helper functions for loading configuration

// envPrefix returns the environment variable prefix for a provider instance.
func envPrefix(instanceName string) string {
	// Normalize: replace hyphens with underscores, uppercase
	normalized := strings.ToUpper(strings.ReplaceAll(instanceName, "-", "_"))
	return "DNSWEAVER_" + normalized + "_"
}

// getEnv returns an environment variable value or empty string.
func getEnv(key string) string {
	return os.Getenv(key)
}

// getEnvWithDefault returns an environment variable value or a default.
func getEnvWithDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// getEnvOrFile tries to read from env var, then from a file specified by fileKey.
// This supports Docker secrets pattern: VAR vs VAR_FILE.
func getEnvOrFile(envKey, fileKey string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	if filePath := os.Getenv(fileKey); filePath != "" {
		data, err := os.ReadFile(filePath)
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

// getMapValue returns a value from a map, trying both lowercase and uppercase keys.
func getMapValue(m map[string]string, key string) string {
	if v, ok := m[key]; ok {
		return v
	}
	if v, ok := m[strings.ToUpper(key)]; ok {
		return v
	}
	return ""
}

// getMapValueWithDefault returns a value from a map or a default value.
func getMapValueWithDefault(m map[string]string, key, defaultValue string) string {
	if v := getMapValue(m, key); v != "" {
		return v
	}
	return defaultValue
}
