package sshutil

import (
	"os"
	"strings"
	"testing"
	"time"
)

// contains is a test helper to check if a string contains a substring.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with key file",
			config: Config{
				Host:    "example.com",
				User:    "admin",
				KeyFile: "/path/to/key",
			},
			wantErr: false,
		},
		{
			name: "valid config with key data",
			config: Config{
				Host:    "example.com",
				User:    "admin",
				KeyData: "-----BEGIN OPENSSH PRIVATE KEY-----\n...",
			},
			wantErr: false,
		},
		{
			name: "valid config with password",
			config: Config{
				Host:     "example.com",
				User:     "admin",
				Password: "secret",
			},
			wantErr: false,
		},
		{
			name: "missing host",
			config: Config{
				User:    "admin",
				KeyFile: "/path/to/key",
			},
			wantErr: true,
			errMsg:  "host is required",
		},
		{
			name: "missing user",
			config: Config{
				Host:    "example.com",
				KeyFile: "/path/to/key",
			},
			wantErr: true,
			errMsg:  "user is required",
		},
		{
			name: "no auth method",
			config: Config{
				Host: "example.com",
				User: "admin",
			},
			wantErr: true,
			errMsg:  "at least one authentication method required",
		},
		{
			name: "invalid port negative",
			config: Config{
				Host:    "example.com",
				User:    "admin",
				KeyFile: "/path/to/key",
				Port:    -1,
			},
			wantErr: true,
			errMsg:  "port must be between 0 and 65535",
		},
		{
			name: "invalid port too high",
			config: Config{
				Host:    "example.com",
				User:    "admin",
				KeyFile: "/path/to/key",
				Port:    65536,
			},
			wantErr: true,
			errMsg:  "port must be between 0 and 65535",
		},
		{
			name: "negative timeout",
			config: Config{
				Host:    "example.com",
				User:    "admin",
				KeyFile: "/path/to/key",
				Timeout: -1 * time.Second,
			},
			wantErr: true,
			errMsg:  "timeout must be non-negative",
		},
		{
			name: "negative keepalive",
			config: Config{
				Host:              "example.com",
				User:              "admin",
				KeyFile:           "/path/to/key",
				KeepaliveInterval: -1 * time.Second,
			},
			wantErr: true,
			errMsg:  "keepalive_interval must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestConfig_Address(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
		want string
	}{
		{
			name: "with explicit port",
			host: "example.com",
			port: 2222,
			want: "example.com:2222",
		},
		{
			name: "with default port (0)",
			host: "example.com",
			port: 0,
			want: "example.com:22",
		},
		{
			name: "ip address",
			host: "192.168.1.100",
			port: 22,
			want: "192.168.1.100:22",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{Host: tt.host, Port: tt.port}
			if got := c.Address(); got != tt.want {
				t.Errorf("Address() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_GetTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{
			name:    "explicit timeout",
			timeout: 60 * time.Second,
			want:    60 * time.Second,
		},
		{
			name:    "zero timeout returns default",
			timeout: 0,
			want:    DefaultSSHTimeout,
		},
		{
			name:    "negative timeout returns default",
			timeout: -1 * time.Second,
			want:    DefaultSSHTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{Timeout: tt.timeout}
			if got := c.GetTimeout(); got != tt.want {
				t.Errorf("GetTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_GetKeepaliveInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		want     time.Duration
	}{
		{
			name:     "explicit interval",
			interval: 30 * time.Second,
			want:     30 * time.Second,
		},
		{
			name:     "zero interval returns default",
			interval: 0,
			want:     DefaultKeepaliveInterval,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{KeepaliveInterval: tt.interval}
			if got := c.GetKeepaliveInterval(); got != tt.want {
				t.Errorf("GetKeepaliveInterval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	// Set up test environment variables
	prefix := "TEST_SSH_"

	// Clean up after test
	defer func() {
		os.Unsetenv(prefix + "HOST")
		os.Unsetenv(prefix + "PORT")
		os.Unsetenv(prefix + "USER")
		os.Unsetenv(prefix + "PASSWORD")
		os.Unsetenv(prefix + "TIMEOUT")
		os.Unsetenv(prefix + "KEEPALIVE_INTERVAL")
		os.Unsetenv(prefix + "STRICT_HOST_KEY_CHECKING")
	}()

	t.Run("valid config from env", func(t *testing.T) {
		os.Setenv(prefix+"HOST", "test.example.com")
		os.Setenv(prefix+"PORT", "2222")
		os.Setenv(prefix+"USER", "testuser")
		os.Setenv(prefix+"PASSWORD", "testpass")
		os.Setenv(prefix+"TIMEOUT", "60")
		os.Setenv(prefix+"KEEPALIVE_INTERVAL", "30")
		os.Setenv(prefix+"STRICT_HOST_KEY_CHECKING", "true")

		config, err := LoadConfig(prefix)
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}

		if config.Host != "test.example.com" {
			t.Errorf("Host = %v, want %v", config.Host, "test.example.com")
		}
		if config.Port != 2222 {
			t.Errorf("Port = %v, want %v", config.Port, 2222)
		}
		if config.User != "testuser" {
			t.Errorf("User = %v, want %v", config.User, "testuser")
		}
		if config.Password != "testpass" {
			t.Errorf("Password = %v, want %v", config.Password, "testpass")
		}
		if config.Timeout != 60*time.Second {
			t.Errorf("Timeout = %v, want %v", config.Timeout, 60*time.Second)
		}
		if config.KeepaliveInterval != 30*time.Second {
			t.Errorf("KeepaliveInterval = %v, want %v", config.KeepaliveInterval, 30*time.Second)
		}
		if !config.StrictHostKeyChecking {
			t.Errorf("StrictHostKeyChecking = %v, want %v", config.StrictHostKeyChecking, true)
		}
	})

	t.Run("default port when not set", func(t *testing.T) {
		os.Unsetenv(prefix + "PORT")
		os.Setenv(prefix+"HOST", "test.example.com")
		os.Setenv(prefix+"USER", "testuser")
		os.Setenv(prefix+"PASSWORD", "testpass")

		config, err := LoadConfig(prefix)
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}

		if config.Port != DefaultSSHPort {
			t.Errorf("Port = %v, want default %v", config.Port, DefaultSSHPort)
		}
	})

	t.Run("invalid port", func(t *testing.T) {
		os.Setenv(prefix+"HOST", "test.example.com")
		os.Setenv(prefix+"PORT", "not-a-number")
		os.Setenv(prefix+"USER", "testuser")
		os.Setenv(prefix+"PASSWORD", "testpass")

		_, err := LoadConfig(prefix)
		if err == nil {
			t.Fatal("LoadConfig() expected error for invalid port")
		}
		if !contains(err.Error(), "invalid PORT") {
			t.Errorf("LoadConfig() error = %v, want error containing 'invalid PORT'", err)
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		os.Unsetenv(prefix + "HOST")
		os.Unsetenv(prefix + "USER")
		os.Unsetenv(prefix + "PASSWORD")
		os.Unsetenv(prefix + "PORT")

		_, err := LoadConfig(prefix)
		if err == nil {
			t.Fatal("LoadConfig() expected error for missing required fields")
		}
	})
}

func TestLoadConfigFromMap(t *testing.T) {
	t.Run("valid config from map", func(t *testing.T) {
		configMap := map[string]string{
			"HOST":                     "test.example.com",
			"PORT":                     "2222",
			"USER":                     "testuser",
			"KEY_FILE":                 "/path/to/key",
			"TIMEOUT":                  "45",
			"KEEPALIVE_INTERVAL":       "20",
			"STRICT_HOST_KEY_CHECKING": "true",
		}

		config, err := LoadConfigFromMap(configMap)
		if err != nil {
			t.Fatalf("LoadConfigFromMap() error = %v", err)
		}

		if config.Host != "test.example.com" {
			t.Errorf("Host = %v, want %v", config.Host, "test.example.com")
		}
		if config.Port != 2222 {
			t.Errorf("Port = %v, want %v", config.Port, 2222)
		}
		if config.User != "testuser" {
			t.Errorf("User = %v, want %v", config.User, "testuser")
		}
		if config.KeyFile != "/path/to/key" {
			t.Errorf("KeyFile = %v, want %v", config.KeyFile, "/path/to/key")
		}
		if config.Timeout != 45*time.Second {
			t.Errorf("Timeout = %v, want %v", config.Timeout, 45*time.Second)
		}
		if config.KeepaliveInterval != 20*time.Second {
			t.Errorf("KeepaliveInterval = %v, want %v", config.KeepaliveInterval, 20*time.Second)
		}
		if !config.StrictHostKeyChecking {
			t.Errorf("StrictHostKeyChecking = %v, want %v", config.StrictHostKeyChecking, true)
		}
	})

	t.Run("defaults when optional fields missing", func(t *testing.T) {
		configMap := map[string]string{
			"HOST":     "test.example.com",
			"USER":     "testuser",
			"PASSWORD": "secret",
		}

		config, err := LoadConfigFromMap(configMap)
		if err != nil {
			t.Fatalf("LoadConfigFromMap() error = %v", err)
		}

		if config.Port != DefaultSSHPort {
			t.Errorf("Port = %v, want default %v", config.Port, DefaultSSHPort)
		}
		if config.Timeout != 0 {
			t.Errorf("Timeout = %v, want 0 (will use default at runtime)", config.Timeout)
		}
	})

	t.Run("invalid port in map", func(t *testing.T) {
		configMap := map[string]string{
			"HOST":     "test.example.com",
			"PORT":     "invalid",
			"USER":     "testuser",
			"PASSWORD": "secret",
		}

		_, err := LoadConfigFromMap(configMap)
		if err == nil {
			t.Fatal("LoadConfigFromMap() expected error for invalid port")
		}
	})
}

func TestGetEnvOrFile(t *testing.T) {
	// Create temp file for testing
	tmpFile, err := os.CreateTemp("", "ssh-test-secret-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	secretContent := "  secret-from-file  \n"
	if _, err := tmpFile.WriteString(secretContent); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpFile.Close()

	// Clean up env vars after test
	defer func() {
		os.Unsetenv("TEST_DIRECT")
		os.Unsetenv("TEST_DIRECT_FILE")
	}()

	t.Run("file takes precedence", func(t *testing.T) {
		os.Setenv("TEST_DIRECT", "direct-value")
		os.Setenv("TEST_DIRECT_FILE", tmpFile.Name())

		got := getEnvOrFile("TEST_DIRECT", "TEST_DIRECT_FILE")
		want := "secret-from-file" // Should be trimmed
		if got != want {
			t.Errorf("getEnvOrFile() = %q, want %q", got, want)
		}
	})

	t.Run("falls back to direct when file missing", func(t *testing.T) {
		os.Setenv("TEST_DIRECT", "direct-value")
		os.Setenv("TEST_DIRECT_FILE", "/nonexistent/path")

		got := getEnvOrFile("TEST_DIRECT", "TEST_DIRECT_FILE")
		want := "direct-value"
		if got != want {
			t.Errorf("getEnvOrFile() = %q, want %q", got, want)
		}
	})

	t.Run("direct value when no file env", func(t *testing.T) {
		os.Setenv("TEST_DIRECT", "direct-value")
		os.Unsetenv("TEST_DIRECT_FILE")

		got := getEnvOrFile("TEST_DIRECT", "TEST_DIRECT_FILE")
		want := "direct-value"
		if got != want {
			t.Errorf("getEnvOrFile() = %q, want %q", got, want)
		}
	})

	t.Run("empty when neither set", func(t *testing.T) {
		os.Unsetenv("TEST_DIRECT")
		os.Unsetenv("TEST_DIRECT_FILE")

		got := getEnvOrFile("TEST_DIRECT", "TEST_DIRECT_FILE")
		if got != "" {
			t.Errorf("getEnvOrFile() = %q, want empty string", got)
		}
	})
}
