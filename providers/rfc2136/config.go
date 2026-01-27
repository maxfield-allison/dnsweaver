package rfc2136

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/dnsupdate"
)

// Default configuration values.
const (
	// DefaultTTL is the default TTL for DNS records.
	DefaultTTL = 300

	// DefaultTimeout is the default timeout for DNS operations.
	DefaultTimeout = 10
)

// Config holds RFC 2136 provider configuration.
// This wraps dnsupdate.Config and adds provider-specific settings.
type Config struct {
	// Server is the DNS server address in host:port format (required).
	// If port is omitted, defaults to 53.
	Server string

	// Zone is the DNS zone to update (required).
	// Must end with a dot (e.g., "example.com.").
	Zone string

	// TSIGKeyName is the TSIG key name for authentication (optional but recommended).
	// Must end with a dot (e.g., "dnsweaver.").
	TSIGKeyName string

	// TSIGSecret is the base64-encoded TSIG shared secret.
	TSIGSecret string

	// TSIGAlgorithm is the TSIG algorithm (default: hmac-sha256).
	// Supported: hmac-md5, hmac-sha256, hmac-sha512.
	TSIGAlgorithm string

	// Timeout is the timeout for DNS operations in seconds (default: 10).
	Timeout int

	// UseTCP forces TCP transport instead of UDP.
	// Required for large updates or when UDP is blocked.
	UseTCP bool

	// TTL is the default TTL for records (default: 300).
	TTL int
}

// Validate checks that all required configuration is present and valid.
func (c *Config) Validate() error {
	var errs []string

	if c.Server == "" {
		errs = append(errs, "SERVER is required")
	}

	if c.Zone == "" {
		errs = append(errs, "ZONE is required")
	} else if !strings.HasSuffix(c.Zone, ".") {
		errs = append(errs, "ZONE must end with a dot (e.g., 'example.com.')")
	}

	// If any TSIG field is set, require key name and secret
	if c.TSIGKeyName != "" || c.TSIGSecret != "" || c.TSIGAlgorithm != "" {
		if c.TSIGKeyName == "" {
			errs = append(errs, "TSIG_KEY_NAME is required when using TSIG authentication")
		} else if !strings.HasSuffix(c.TSIGKeyName, ".") {
			errs = append(errs, "TSIG_KEY_NAME must end with a dot (e.g., 'dnsweaver.')")
		}

		if c.TSIGSecret == "" {
			errs = append(errs, "TSIG_SECRET is required when using TSIG authentication")
		}
	}

	if c.TTL < 0 {
		errs = append(errs, "TTL must be non-negative")
	}

	if c.Timeout < 0 {
		errs = append(errs, "TIMEOUT must be non-negative")
	}

	if len(errs) > 0 {
		return fmt.Errorf("rfc2136 config validation failed: %s", strings.Join(errs, "; "))
	}

	return nil
}

// ToDNSUpdateConfig converts this config to dnsupdate.Config.
func (c *Config) ToDNSUpdateConfig() *dnsupdate.Config {
	return &dnsupdate.Config{
		Server:        c.Server,
		Zone:          c.Zone,
		TSIGKeyName:   c.TSIGKeyName,
		TSIGSecret:    c.TSIGSecret,
		TSIGAlgorithm: c.TSIGAlgorithm,
		Timeout:       time.Duration(c.Timeout) * time.Second,
		UseTCP:        c.UseTCP,
	}
}

// LoadConfig loads RFC 2136 configuration from environment variables.
// Environment variable pattern: DNSWEAVER_{INSTANCE_NAME}_{SETTING}
//
// Instance names are normalized: lowercase with hyphens becomes uppercase with underscores.
// Example: "internal-dns" looks for DNSWEAVER_INTERNAL_DNS_*
//
// Supported settings:
//   - SERVER: DNS server address (required)
//   - ZONE: Zone name (required)
//   - TSIG_KEY_NAME: TSIG key name (optional)
//   - TSIG_SECRET: TSIG secret (supports _FILE suffix for Docker secrets)
//   - TSIG_ALGORITHM: TSIG algorithm (default: hmac-sha256)
//   - TIMEOUT: Timeout in seconds (default: 10)
//   - USE_TCP: Force TCP transport (default: false)
//   - TTL: Default TTL for records (default: 300)
func LoadConfig(instanceName string) (*Config, error) {
	prefix := envPrefix(instanceName)

	config := &Config{
		Server:        getEnv(prefix + "SERVER"),
		Zone:          getEnv(prefix + "ZONE"),
		TSIGKeyName:   getEnv(prefix + "TSIG_KEY_NAME"),
		TSIGSecret:    getEnvOrFile(prefix+"TSIG_SECRET", prefix+"TSIG_SECRET_FILE"),
		TSIGAlgorithm: getEnv(prefix + "TSIG_ALGORITHM"),
		TTL:           DefaultTTL,
		Timeout:       DefaultTimeout,
	}

	// Parse timeout
	if timeoutStr := getEnv(prefix + "TIMEOUT"); timeoutStr != "" {
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid TIMEOUT value %q: %w", timeoutStr, err)
		}
		config.Timeout = timeout
	}

	// Parse use_tcp
	if tcpStr := getEnv(prefix + "USE_TCP"); tcpStr != "" {
		config.UseTCP = strings.EqualFold(tcpStr, "true") || tcpStr == "1"
	}

	// Parse TTL
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
// Required keys: SERVER, ZONE
// Optional keys: TSIG_KEY_NAME, TSIG_SECRET, TSIG_ALGORITHM, TIMEOUT, USE_TCP, TTL
func LoadConfigFromMap(instanceName string, configMap map[string]string) (*Config, error) {
	config := &Config{
		Server:        configMap["SERVER"],
		Zone:          configMap["ZONE"],
		TSIGKeyName:   configMap["TSIG_KEY_NAME"],
		TSIGSecret:    configMap["TSIG_SECRET"],
		TSIGAlgorithm: configMap["TSIG_ALGORITHM"],
		TTL:           DefaultTTL,
		Timeout:       DefaultTimeout,
	}

	// Parse timeout
	if timeoutStr, ok := configMap["TIMEOUT"]; ok && timeoutStr != "" {
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid TIMEOUT value %q: %w", timeoutStr, err)
		}
		config.Timeout = timeout
	}

	// Parse use_tcp
	if tcpStr, ok := configMap["USE_TCP"]; ok && tcpStr != "" {
		config.UseTCP = strings.EqualFold(tcpStr, "true") || tcpStr == "1"
	}

	// Parse TTL
	if ttlStr, ok := configMap["TTL"]; ok && ttlStr != "" {
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

// envPrefix converts an instance name to an environment variable prefix.
// Example: "internal-dns" → "DNSWEAVER_INTERNAL_DNS_"
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
