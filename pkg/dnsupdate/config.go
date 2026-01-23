package dnsupdate

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// Default configuration values.
const (
	// DefaultPort is the standard DNS port.
	DefaultPort = 53

	// DefaultTimeout is the default timeout for DNS operations.
	DefaultTimeout = 10 * time.Second

	// DefaultTSIGAlgorithm is the default TSIG algorithm if none specified.
	DefaultTSIGAlgorithm = TSIGAlgorithmSHA256
)

// TSIG algorithm constants matching miekg/dns format.
const (
	// TSIGAlgorithmMD5 is HMAC-MD5 (legacy, not recommended).
	TSIGAlgorithmMD5 = dns.HmacMD5

	// TSIGAlgorithmSHA256 is HMAC-SHA256 (recommended).
	TSIGAlgorithmSHA256 = dns.HmacSHA256

	// TSIGAlgorithmSHA512 is HMAC-SHA512 (strongest).
	TSIGAlgorithmSHA512 = dns.HmacSHA512
)

// Algorithm name constants for user-facing configuration.
const (
	AlgNameSHA256 = "hmac-sha256"
	AlgNameSHA512 = "hmac-sha512"
	AlgNameMD5    = "hmac-md5"
)

// Config holds RFC 2136 Dynamic DNS client configuration.
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

	// Timeout is the timeout for DNS operations (default: 10s).
	Timeout time.Duration

	// UseTCP forces TCP transport instead of UDP.
	// Required for large updates or when UDP is blocked.
	UseTCP bool
}

// Validate checks that all required configuration is present and valid.
func (c *Config) Validate() error {
	var errs []string

	if c.Server == "" {
		errs = append(errs, "server is required")
	}

	if c.Zone == "" {
		errs = append(errs, "zone is required")
	} else if !strings.HasSuffix(c.Zone, ".") {
		errs = append(errs, "zone must end with a dot (e.g., 'example.com.')")
	}

	// If any TSIG field is set, require key name and secret
	if c.TSIGKeyName != "" || c.TSIGSecret != "" || c.TSIGAlgorithm != "" {
		if c.TSIGKeyName == "" {
			errs = append(errs, "tsig_key_name is required when using TSIG authentication")
		} else if !strings.HasSuffix(c.TSIGKeyName, ".") {
			errs = append(errs, "tsig_key_name must end with a dot (e.g., 'dnsweaver.')")
		}

		if c.TSIGSecret == "" {
			errs = append(errs, "tsig_secret is required when using TSIG authentication")
		}

		if c.TSIGAlgorithm != "" {
			alg := c.GetTSIGAlgorithm()
			if alg != TSIGAlgorithmMD5 && alg != TSIGAlgorithmSHA256 && alg != TSIGAlgorithmSHA512 {
				errs = append(errs, fmt.Sprintf("unsupported tsig_algorithm: %s (supported: hmac-md5, hmac-sha256, hmac-sha512)", c.TSIGAlgorithm))
			}
		}
	}

	if c.Timeout < 0 {
		errs = append(errs, "timeout must be non-negative")
	}

	if len(errs) > 0 {
		return fmt.Errorf("dnsupdate config validation failed: %s", strings.Join(errs, "; "))
	}

	return nil
}

// GetServer returns the server address with port.
// If no port is specified, appends the default DNS port (53).
func (c *Config) GetServer() string {
	if c.Server == "" {
		return ""
	}

	// Check if port is already included
	if strings.Contains(c.Server, ":") {
		return c.Server
	}

	return fmt.Sprintf("%s:%d", c.Server, DefaultPort)
}

// GetTimeout returns the configured timeout or the default.
func (c *Config) GetTimeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

// GetTSIGAlgorithm returns the TSIG algorithm in miekg/dns format.
func (c *Config) GetTSIGAlgorithm() string {
	if c.TSIGAlgorithm == "" {
		return DefaultTSIGAlgorithm
	}

	// Normalize the algorithm string
	alg := strings.ToLower(strings.TrimSpace(c.TSIGAlgorithm))

	switch alg {
	case AlgNameMD5, "md5", "hmac-md5.sig-alg.reg.int.":
		return TSIGAlgorithmMD5
	case AlgNameSHA256, "sha256":
		return TSIGAlgorithmSHA256
	case AlgNameSHA512, "sha512":
		return TSIGAlgorithmSHA512
	default:
		return alg // Return as-is, validation will catch invalid values
	}
}

// HasTSIG returns true if TSIG authentication is configured.
func (c *Config) HasTSIG() bool {
	return c.TSIGKeyName != "" && c.TSIGSecret != ""
}

// LoadConfig loads RFC 2136 configuration from environment variables.
// Environment variable pattern: {prefix}{setting}
//
// Supported settings:
//   - SERVER: DNS server address (required)
//   - ZONE: Zone name (required)
//   - TSIG_KEY_NAME: TSIG key name (optional)
//   - TSIG_SECRET: TSIG secret (supports _FILE suffix for Docker secrets)
//   - TSIG_ALGORITHM: TSIG algorithm (default: hmac-sha256)
//   - TIMEOUT: Timeout in seconds (default: 10)
//   - USE_TCP: Force TCP transport (default: false)
func LoadConfig(prefix string) (*Config, error) {
	config := &Config{
		Server:        getEnv(prefix + "SERVER"),
		Zone:          getEnv(prefix + "ZONE"),
		TSIGKeyName:   getEnv(prefix + "TSIG_KEY_NAME"),
		TSIGSecret:    getEnvOrFile(prefix+"TSIG_SECRET", prefix+"TSIG_SECRET_FILE"),
		TSIGAlgorithm: getEnv(prefix + "TSIG_ALGORITHM"),
	}

	// Parse timeout
	if timeoutStr := getEnv(prefix + "TIMEOUT"); timeoutStr != "" {
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid TIMEOUT value %q: %w", timeoutStr, err)
		}
		config.Timeout = time.Duration(timeout) * time.Second
	}

	// Parse use_tcp
	if tcpStr := getEnv(prefix + "USE_TCP"); tcpStr != "" {
		config.UseTCP = strings.EqualFold(tcpStr, "true") || tcpStr == "1"
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

// LoadConfigFromMap creates a Config from a map of key-value pairs.
// This is used by provider registries to create RFC 2136 configurations from
// configuration that was already parsed from environment variables.
//
// Required keys: SERVER, ZONE
// Optional keys: TSIG_KEY_NAME, TSIG_SECRET, TSIG_ALGORITHM, TIMEOUT, USE_TCP
func LoadConfigFromMap(configMap map[string]string) (*Config, error) {
	config := &Config{
		Server:        configMap["SERVER"],
		Zone:          configMap["ZONE"],
		TSIGKeyName:   configMap["TSIG_KEY_NAME"],
		TSIGSecret:    configMap["TSIG_SECRET"],
		TSIGAlgorithm: configMap["TSIG_ALGORITHM"],
	}

	// Parse timeout
	if timeoutStr, ok := configMap["TIMEOUT"]; ok && timeoutStr != "" {
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid TIMEOUT value %q: %w", timeoutStr, err)
		}
		config.Timeout = time.Duration(timeout) * time.Second
	}

	// Parse use_tcp
	if tcpStr, ok := configMap["USE_TCP"]; ok && tcpStr != "" {
		config.UseTCP = strings.EqualFold(tcpStr, "true") || tcpStr == "1"
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
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
