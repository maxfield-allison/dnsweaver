package sshutil

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// containsIgnoreCase is a test helper for case-insensitive substring check.
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func TestNewClient(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		config := &Config{
			Host:    "example.com",
			User:    "admin",
			KeyFile: "/path/to/key",
		}

		client, err := NewClient(config)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}

		if client == nil {
			t.Fatal("NewClient() returned nil client")
		}

		if client.config != config {
			t.Error("NewClient() config not set correctly")
		}
	})

	t.Run("nil config", func(t *testing.T) {
		_, err := NewClient(nil)
		if err == nil {
			t.Fatal("NewClient() expected error for nil config")
		}
		if !contains(err.Error(), "config is required") {
			t.Errorf("NewClient() error = %v, want error containing 'config is required'", err)
		}
	})

	t.Run("invalid config", func(t *testing.T) {
		config := &Config{
			Host: "example.com",
			// Missing User and auth method
		}

		_, err := NewClient(config)
		if err == nil {
			t.Fatal("NewClient() expected error for invalid config")
		}
	})

	t.Run("with logger option", func(t *testing.T) {
		config := &Config{
			Host:     "example.com",
			User:     "admin",
			Password: "secret",
		}

		logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
		client, err := NewClient(config, WithLogger(logger))
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}

		if client.logger != logger {
			t.Error("WithLogger() option not applied")
		}
	})

	t.Run("with nil logger option (should keep default)", func(t *testing.T) {
		config := &Config{
			Host:     "example.com",
			User:     "admin",
			Password: "secret",
		}

		client, err := NewClient(config, WithLogger(nil))
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}

		if client.logger == nil {
			t.Error("WithLogger(nil) removed default logger")
		}
	})
}

func TestClient_IsConnected(t *testing.T) {
	config := &Config{
		Host:     "example.com",
		User:     "admin",
		Password: "secret",
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	// Initially not connected
	if client.IsConnected() {
		t.Error("IsConnected() = true before Connect(), want false")
	}
}

func TestClient_GetConnection_NotConnected(t *testing.T) {
	config := &Config{
		Host:     "example.com",
		User:     "admin",
		Password: "secret",
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.GetConnection()
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("GetConnection() error = %v, want %v", err, ErrNotConnected)
	}
}

func TestClient_Close_NotConnected(t *testing.T) {
	config := &Config{
		Host:     "example.com",
		User:     "admin",
		Password: "secret",
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	// Close should be safe to call even when not connected
	err = client.Close()
	if err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

func TestClient_buildAuthMethods(t *testing.T) {
	t.Run("with key file", func(t *testing.T) {
		// Create a temp key file (not a real key, just for testing path handling)
		tmpFile, err := os.CreateTemp("", "ssh-test-key-*")
		if err != nil {
			t.Fatalf("Failed to create temp file: %v", err)
		}
		defer os.Remove(tmpFile.Name())

		// Write a fake key (won't parse, but tests path loading)
		tmpFile.WriteString("fake-key-content")
		tmpFile.Close()

		config := &Config{
			Host:    "example.com",
			User:    "admin",
			KeyFile: tmpFile.Name(),
		}

		client, _ := NewClient(config)

		// buildAuthMethods should fail because the key is invalid
		// but it should attempt to read the file
		_, err = client.buildAuthMethods()
		if err == nil {
			t.Error("buildAuthMethods() expected error for invalid key")
		}
		if !contains(err.Error(), "parsing key") {
			t.Errorf("buildAuthMethods() error = %v, want error containing 'parsing key'", err)
		}
	})

	t.Run("with nonexistent key file", func(t *testing.T) {
		config := &Config{
			Host:    "example.com",
			User:    "admin",
			KeyFile: "/nonexistent/path/to/key",
		}

		client, _ := NewClient(config)
		_, err := client.buildAuthMethods()
		if err == nil {
			t.Error("buildAuthMethods() expected error for nonexistent key file")
		}
		if !contains(err.Error(), "reading key file") {
			t.Errorf("buildAuthMethods() error = %v, want error containing 'reading key file'", err)
		}
	})

	t.Run("with invalid key data", func(t *testing.T) {
		config := &Config{
			Host:    "example.com",
			User:    "admin",
			KeyData: "not-a-valid-key",
		}

		client, _ := NewClient(config)
		_, err := client.buildAuthMethods()
		if err == nil {
			t.Error("buildAuthMethods() expected error for invalid key data")
		}
		if !contains(err.Error(), "parsing key data") {
			t.Errorf("buildAuthMethods() error = %v, want error containing 'parsing key data'", err)
		}
	})

	t.Run("with password only", func(t *testing.T) {
		config := &Config{
			Host:     "example.com",
			User:     "admin",
			Password: "secret",
		}

		client, _ := NewClient(config)
		methods, err := client.buildAuthMethods()
		if err != nil {
			t.Fatalf("buildAuthMethods() error = %v", err)
		}
		if len(methods) != 1 {
			t.Errorf("buildAuthMethods() returned %d methods, want 1", len(methods))
		}
	})

	t.Run("no auth methods", func(t *testing.T) {
		// Create config that bypasses validation (direct construction)
		config := &Config{
			Host: "example.com",
			User: "admin",
		}

		client := &Client{
			config: config,
			logger: slog.Default(),
		}

		_, err := client.buildAuthMethods()
		if err == nil {
			t.Error("buildAuthMethods() expected error for no auth methods")
		}
		if !contains(err.Error(), "no authentication methods") {
			t.Errorf("buildAuthMethods() error = %v, want error containing 'no authentication methods'", err)
		}
	})
}

func TestClient_buildHostKeyCallback(t *testing.T) {
	t.Run("no strict checking", func(t *testing.T) {
		config := &Config{
			Host:                  "example.com",
			User:                  "admin",
			Password:              "secret",
			StrictHostKeyChecking: false,
		}

		client, _ := NewClient(config)
		callback, err := client.buildHostKeyCallback()
		if err != nil {
			t.Fatalf("buildHostKeyCallback() error = %v", err)
		}
		if callback == nil {
			t.Error("buildHostKeyCallback() returned nil callback")
		}
	})

	t.Run("ignore callback", func(t *testing.T) {
		config := &Config{
			Host:                  "example.com",
			User:                  "admin",
			Password:              "secret",
			HostKeyCallback:       "ignore",
			StrictHostKeyChecking: false,
		}

		client, _ := NewClient(config)
		callback, err := client.buildHostKeyCallback()
		if err != nil {
			t.Fatalf("buildHostKeyCallback() error = %v", err)
		}
		if callback == nil {
			t.Error("buildHostKeyCallback() returned nil callback")
		}
	})

	t.Run("strict checking without known_hosts", func(t *testing.T) {
		config := &Config{
			Host:                  "example.com",
			User:                  "admin",
			Password:              "secret",
			StrictHostKeyChecking: true,
		}

		client, _ := NewClient(config)
		_, err := client.buildHostKeyCallback()
		if err == nil {
			t.Error("buildHostKeyCallback() expected error when strict checking enabled without known_hosts")
		}
	})
}

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "unable to authenticate",
			err:  errors.New("ssh: unable to authenticate"),
			want: true,
		},
		{
			name: "no supported methods",
			err:  errors.New("ssh: no supported methods remain"),
			want: true,
		},
		{
			name: "permission denied",
			err:  errors.New("permission denied"),
			want: true,
		},
		{
			name: "publickey error",
			err:  errors.New("ssh: publickey authentication failed"),
			want: true,
		},
		{
			name: "password error",
			err:  errors.New("ssh: password authentication failed"),
			want: true,
		},
		{
			name: "unrelated error",
			err:  errors.New("connection refused"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAuthError(tt.err); got != tt.want {
				t.Errorf("isAuthError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainsIgnoreCase(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		substr string
		want   bool
	}{
		{
			name:   "exact match",
			s:      "hello world",
			substr: "hello",
			want:   true,
		},
		{
			name:   "case insensitive",
			s:      "Hello World",
			substr: "hello",
			want:   true,
		},
		{
			name:   "not found",
			s:      "hello world",
			substr: "foo",
			want:   false,
		},
		{
			name:   "empty substring",
			s:      "hello",
			substr: "",
			want:   true,
		},
		{
			name:   "substring longer than string",
			s:      "hi",
			substr: "hello",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsIgnoreCase(tt.s, tt.substr); got != tt.want {
				t.Errorf("containsIgnoreCase(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
			}
		})
	}
}

// TestClient_Connect_InvalidHost tests connection to an invalid host
func TestClient_Connect_InvalidHost(t *testing.T) {
	config := &Config{
		Host:     "invalid.host.that.does.not.exist.local",
		User:     "admin",
		Password: "secret",
		Timeout:  1 * 1, // Very short timeout
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*1)
	defer cancel()

	err = client.Connect(ctx)
	if err == nil {
		client.Close()
		t.Fatal("Connect() expected error for invalid host")
	}
	// The error could be a DNS resolution failure or connection timeout
	// Either is acceptable for this test
}
