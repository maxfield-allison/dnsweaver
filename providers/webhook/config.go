package webhook

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// DefaultTimeout is the default HTTP client timeout for webhook requests.
const DefaultTimeout = 30 * time.Second

// DefaultRetries is the default number of retry attempts for transient failures.
const DefaultRetries = 3

// DefaultRetryDelay is the base delay between retry attempts.
const DefaultRetryDelay = time.Second

// Config holds webhook-specific configuration.
type Config struct {
	URL        string        // Base URL for the webhook endpoint (required)
	Timeout    time.Duration // HTTP client timeout (default: 30s)
	AuthHeader string        // Custom authentication header name (optional)
	AuthToken  string        // Authentication token value (optional)
	Retries    int           // Number of retry attempts (default: 3)
	RetryDelay time.Duration // Base delay between retries (default: 1s)
}

// Validate checks that all required configuration is present.
func (c *Config) Validate() error {
	var errs []string

	if c.URL == "" {
		errs = append(errs, "URL is required")
	} else {
		// Basic URL validation
		if !strings.HasPrefix(c.URL, "http://") && !strings.HasPrefix(c.URL, "https://") {
			errs = append(errs, "URL must start with http:// or https://")
		}
	}

	// AuthHeader requires AuthToken
	if c.AuthHeader != "" && c.AuthToken == "" {
		errs = append(errs, "AUTH_TOKEN is required when AUTH_HEADER is set")
	}

	if c.Timeout < 0 {
		errs = append(errs, "TIMEOUT must be non-negative")
	}

	if c.Retries < 0 {
		errs = append(errs, "RETRIES must be non-negative")
	}

	if c.RetryDelay < 0 {
		errs = append(errs, "RETRY_DELAY must be non-negative")
	}

	if len(errs) > 0 {
		return fmt.Errorf("webhook config validation failed: %s", strings.Join(errs, "; "))
	}

	return nil
}

// LoadConfig loads webhook configuration from environment variables.
// Environment variable pattern: DNSWEAVER_{INSTANCE_NAME}_{SETTING}
//
// Instance names are normalized: lowercase with hyphens becomes uppercase with underscores.
// Example: "custom-dns" looks for DNSWEAVER_CUSTOM_DNS_*
//
// Supported settings:
//   - URL: Base webhook URL (required)
//   - TIMEOUT: HTTP timeout duration (optional, default: 30s)
//   - AUTH_HEADER: Custom auth header name (optional, e.g., "X-API-Key")
//   - AUTH_TOKEN: Auth token value (required if AUTH_HEADER set, supports _FILE)
//   - RETRIES: Number of retry attempts (optional, default: 3)
//   - RETRY_DELAY: Base delay between retries (optional, default: 1s)
func LoadConfig(instanceName string) (*Config, error) {
	prefix := envPrefix(instanceName)

	config := &Config{
		URL:        getEnv(prefix + "URL"),
		Timeout:    DefaultTimeout,
		AuthHeader: getEnv(prefix + "AUTH_HEADER"),
		AuthToken:  getEnvOrFile(prefix+"AUTH_TOKEN", prefix+"AUTH_TOKEN_FILE"),
		Retries:    DefaultRetries,
		RetryDelay: DefaultRetryDelay,
	}

	// Parse optional TIMEOUT
	if timeoutStr := getEnv(prefix + "TIMEOUT"); timeoutStr != "" {
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid TIMEOUT value %q: %w", timeoutStr, err)
		}
		config.Timeout = timeout
	}

	// Parse optional RETRIES
	if retriesStr := getEnv(prefix + "RETRIES"); retriesStr != "" {
		var retries int
		if _, err := fmt.Sscanf(retriesStr, "%d", &retries); err != nil {
			return nil, fmt.Errorf("invalid RETRIES value %q: %w", retriesStr, err)
		}
		config.Retries = retries
	}

	// Parse optional RETRY_DELAY
	if delayStr := getEnv(prefix + "RETRY_DELAY"); delayStr != "" {
		delay, err := time.ParseDuration(delayStr)
		if err != nil {
			return nil, fmt.Errorf("invalid RETRY_DELAY value %q: %w", delayStr, err)
		}
		config.RetryDelay = delay
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration for %s: %w", instanceName, err)
	}

	return config, nil
}

// envPrefix converts an instance name to an environment variable prefix.
// Example: "custom-dns" → "DNSWEAVER_CUSTOM_DNS_"
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
