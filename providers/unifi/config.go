package unifi

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// DefaultTTL is the default TTL for UniFi A, AAAA and CNAME records.
// TXT and SRV policies have no TTL on the console; the value is informational for them.
const DefaultTTL = 300

// DefaultSite is the internalReference of the site every UniFi console ships with.
const DefaultSite = "default"

// Config holds UniFi-specific configuration.
type Config struct {
	// URL is the console base URL (e.g. "https://192.168.1.1").
	URL string

	// APIKey is the Integration API key. It is sent as the X-API-KEY header.
	APIKey string

	// Site is the site internalReference (e.g. "default") or UUID.
	Site string

	// Zone is the DNS zone for record filtering (optional).
	// When set, only records matching this zone suffix are returned by List.
	Zone string

	// TTL is the record TTL for A, AAAA and CNAME records.
	TTL int
}

// Validate checks that all required configuration is present.
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

	if c.Site == "" {
		errs = append(errs, "SITE must not be empty")
	}

	if c.TTL < 0 {
		errs = append(errs, "TTL must be non-negative")
	}

	if len(errs) > 0 {
		return fmt.Errorf("unifi config validation failed: %s", strings.Join(errs, "; "))
	}

	return nil
}

// LoadConfig loads UniFi configuration from environment variables.
// Environment variable pattern: DNSWEAVER_{INSTANCE_NAME}_{SETTING}
//
// Instance names are normalized: lowercase with hyphens becomes uppercase with underscores.
// Example: "unifi-dns" looks for DNSWEAVER_UNIFI_DNS_*
//
// Supported settings:
//   - URL: Console base URL (required)
//   - API_KEY: Integration API key (required, supports _FILE suffix for Docker secrets)
//   - SITE: Site internalReference or UUID (optional, default "default")
//   - ZONE: DNS zone for record filtering (optional)
//   - TTL: Record TTL (optional, default 300)
func LoadConfig(instanceName string) (*Config, error) {
	prefix := envPrefix(instanceName)

	config := &Config{
		URL:    getEnv(prefix + "URL"),
		APIKey: getEnvOrFile(prefix+"API_KEY", prefix+"API_KEY_FILE"),
		Site:   siteOrDefault(getEnv(prefix + "SITE")),
		Zone:   getEnv(prefix + "ZONE"),
		TTL:    DefaultTTL,
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

// LoadConfigFromMap creates a Config from a map of key-value pairs.
// This is used by the provider registry to create instances from
// configuration that was already parsed from environment variables.
//
// Required keys: URL, API_KEY
// Optional keys: SITE, ZONE, TTL
func LoadConfigFromMap(name string, m map[string]string) (*Config, error) {
	config := &Config{
		URL:    getMapValue(m, "URL"),
		APIKey: getMapValue(m, "API_KEY"),
		Site:   siteOrDefault(getMapValue(m, "SITE")),
		Zone:   getMapValue(m, "ZONE"),
		TTL:    DefaultTTL,
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

// siteOrDefault returns the trimmed site name, falling back to DefaultSite.
func siteOrDefault(s string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return DefaultSite
}

// envPrefix returns the environment variable prefix for a provider instance.
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

// getMapValue returns a value from a map, case-insensitively.
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
