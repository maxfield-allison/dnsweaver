// Package sshutil provides shared SSH/SFTP client utilities for DNSWeaver providers.
//
// This package enables providers to manage remote file-based DNS configurations
// (e.g., dnsmasq, Pi-hole, hosts files) via SSH/SFTP, and execute remote commands
// for reloading configurations.
//
// Key features:
//   - SSH connection pooling and reuse
//   - SFTP-based FileSystem interface implementation
//   - SSH exec-based CommandRunner interface implementation
//   - Docker secrets support (_FILE suffix pattern)
//   - Multiple authentication methods (key file, key content, password)
package sshutil

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Default SSH client configuration values.
const (
	// DefaultSSHPort is the standard SSH port.
	DefaultSSHPort = 22

	// DefaultSSHTimeout is the default connection timeout.
	DefaultSSHTimeout = 30 * time.Second

	// DefaultKeepaliveInterval is the default SSH keepalive interval.
	DefaultKeepaliveInterval = 15 * time.Second
)

// Config holds SSH connection configuration.
type Config struct {
	// Host is the SSH server hostname or IP address (required).
	Host string

	// Port is the SSH server port (default: 22).
	Port int

	// User is the SSH username (required).
	User string

	// KeyFile is the path to the SSH private key file.
	// Either KeyFile, KeyData, or Password must be provided.
	KeyFile string

	// KeyData is the SSH private key content directly.
	// Useful when the key is provided via environment variable or Docker secret.
	// Either KeyFile, KeyData, or Password must be provided.
	KeyData string

	// KeyPassphrase is the passphrase for encrypted SSH keys (optional).
	KeyPassphrase string

	// Password is the SSH password for password authentication.
	// Key-based authentication is recommended over password.
	// Either KeyFile, KeyData, or Password must be provided.
	Password string

	// Timeout is the SSH connection timeout (default: 30s).
	Timeout time.Duration

	// KeepaliveInterval is the interval for SSH keepalive messages (default: 15s).
	// Set to 0 to disable keepalives.
	KeepaliveInterval time.Duration

	// HostKeyCallback controls host key verification.
	// If empty, host keys are not verified (InsecureIgnoreHostKey).
	// Supported values: "ignore" (insecure), or path to known_hosts file.
	HostKeyCallback string

	// StrictHostKeyChecking controls whether to verify host keys.
	// If false (default when HostKeyCallback is empty), host keys are not verified.
	// WARNING: Disabling host key checking is insecure and should only be used
	// for testing or when connecting to trusted internal networks.
	StrictHostKeyChecking bool
}

// Validate checks that all required configuration is present and valid.
func (c *Config) Validate() error {
	var errs []string

	if c.Host == "" {
		errs = append(errs, "host is required")
	}

	if c.User == "" {
		errs = append(errs, "user is required")
	}

	// At least one authentication method required
	if c.KeyFile == "" && c.KeyData == "" && c.Password == "" {
		errs = append(errs, "at least one authentication method required (key_file, key_data, or password)")
	}

	if c.Port < 0 || c.Port > 65535 {
		errs = append(errs, "port must be between 0 and 65535")
	}

	if c.Timeout < 0 {
		errs = append(errs, "timeout must be non-negative")
	}

	if c.KeepaliveInterval < 0 {
		errs = append(errs, "keepalive_interval must be non-negative")
	}

	if len(errs) > 0 {
		return fmt.Errorf("ssh config validation failed: %s", strings.Join(errs, "; "))
	}

	return nil
}

// Address returns the SSH server address in host:port format.
func (c *Config) Address() string {
	port := c.Port
	if port == 0 {
		port = DefaultSSHPort
	}
	return fmt.Sprintf("%s:%d", c.Host, port)
}

// GetTimeout returns the configured timeout or the default.
func (c *Config) GetTimeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultSSHTimeout
}

// GetKeepaliveInterval returns the configured keepalive interval or the default.
func (c *Config) GetKeepaliveInterval() time.Duration {
	if c.KeepaliveInterval > 0 {
		return c.KeepaliveInterval
	}
	return DefaultKeepaliveInterval
}

// LoadConfig loads SSH configuration from environment variables.
// Environment variable pattern: {prefix}_{setting}
//
// Supported settings:
//   - HOST: SSH server hostname or IP (required)
//   - PORT: SSH server port (default: 22)
//   - USER: SSH username (required)
//   - KEY_FILE: Path to SSH private key file (supports _FILE suffix for Docker secrets)
//   - KEY_DATA: SSH private key content directly (supports _FILE suffix for Docker secrets)
//   - KEY_PASSPHRASE: Passphrase for encrypted keys (supports _FILE suffix for Docker secrets)
//   - PASSWORD: SSH password (supports _FILE suffix for Docker secrets)
//   - TIMEOUT: Connection timeout in seconds (default: 30)
//   - KEEPALIVE_INTERVAL: Keepalive interval in seconds (default: 15, 0 to disable)
//   - HOST_KEY_CALLBACK: "ignore" or path to known_hosts file
//   - STRICT_HOST_KEY_CHECKING: "true" or "false" (default: false)
func LoadConfig(prefix string) (*Config, error) {
	config := &Config{
		Host:                  getEnv(prefix + "HOST"),
		User:                  getEnv(prefix + "USER"),
		KeyFile:               getEnvOrFile(prefix+"KEY_FILE", prefix+"KEY_FILE_FILE"),
		KeyData:               getEnvOrFile(prefix+"KEY_DATA", prefix+"KEY_DATA_FILE"),
		KeyPassphrase:         getEnvOrFile(prefix+"KEY_PASSPHRASE", prefix+"KEY_PASSPHRASE_FILE"),
		Password:              getEnvOrFile(prefix+"PASSWORD", prefix+"PASSWORD_FILE"),
		HostKeyCallback:       getEnv(prefix + "HOST_KEY_CALLBACK"),
		StrictHostKeyChecking: false,
	}

	// Parse port
	if portStr := getEnv(prefix + "PORT"); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid PORT value %q: %w", portStr, err)
		}
		config.Port = port
	} else {
		config.Port = DefaultSSHPort
	}

	// Parse timeout
	if timeoutStr := getEnv(prefix + "TIMEOUT"); timeoutStr != "" {
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid TIMEOUT value %q: %w", timeoutStr, err)
		}
		config.Timeout = time.Duration(timeout) * time.Second
	}

	// Parse keepalive interval
	if keepaliveStr := getEnv(prefix + "KEEPALIVE_INTERVAL"); keepaliveStr != "" {
		keepalive, err := strconv.Atoi(keepaliveStr)
		if err != nil {
			return nil, fmt.Errorf("invalid KEEPALIVE_INTERVAL value %q: %w", keepaliveStr, err)
		}
		config.KeepaliveInterval = time.Duration(keepalive) * time.Second
	}

	// Parse strict host key checking
	if strictStr := getEnv(prefix + "STRICT_HOST_KEY_CHECKING"); strictStr != "" {
		config.StrictHostKeyChecking = strings.EqualFold(strictStr, "true")
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

// LoadConfigFromMap creates a Config from a map of key-value pairs.
// This is used by provider registries to create SSH configurations from
// configuration that was already parsed from environment variables.
//
// Required keys: HOST, USER, and at least one of KEY_FILE/KEY_DATA/PASSWORD
// Optional keys: PORT, TIMEOUT, KEEPALIVE_INTERVAL, KEY_PASSPHRASE, HOST_KEY_CALLBACK, STRICT_HOST_KEY_CHECKING
func LoadConfigFromMap(configMap map[string]string) (*Config, error) {
	config := &Config{
		Host:                  configMap["HOST"],
		User:                  configMap["USER"],
		KeyFile:               configMap["KEY_FILE"],
		KeyData:               configMap["KEY_DATA"],
		KeyPassphrase:         configMap["KEY_PASSPHRASE"],
		Password:              configMap["PASSWORD"],
		HostKeyCallback:       configMap["HOST_KEY_CALLBACK"],
		StrictHostKeyChecking: false,
		Port:                  DefaultSSHPort,
	}

	// Parse port
	if portStr, ok := configMap["PORT"]; ok && portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid PORT value %q: %w", portStr, err)
		}
		config.Port = port
	}

	// Parse timeout
	if timeoutStr, ok := configMap["TIMEOUT"]; ok && timeoutStr != "" {
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid TIMEOUT value %q: %w", timeoutStr, err)
		}
		config.Timeout = time.Duration(timeout) * time.Second
	}

	// Parse keepalive interval
	if keepaliveStr, ok := configMap["KEEPALIVE_INTERVAL"]; ok && keepaliveStr != "" {
		keepalive, err := strconv.Atoi(keepaliveStr)
		if err != nil {
			return nil, fmt.Errorf("invalid KEEPALIVE_INTERVAL value %q: %w", keepaliveStr, err)
		}
		config.KeepaliveInterval = time.Duration(keepalive) * time.Second
	}

	// Parse strict host key checking
	if strictStr, ok := configMap["STRICT_HOST_KEY_CHECKING"]; ok && strictStr != "" {
		config.StrictHostKeyChecking = strings.EqualFold(strictStr, "true")
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
