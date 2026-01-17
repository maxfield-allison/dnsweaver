// Package sshutil provides shared SSH/SFTP client utilities for DNSWeaver providers.
package sshutil

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Sentinel errors for SSH operations.
var (
	// ErrNotConnected is returned when an operation is attempted on a disconnected client.
	ErrNotConnected = errors.New("ssh client is not connected")

	// ErrAlreadyConnected is returned when Connect is called on an already connected client.
	ErrAlreadyConnected = errors.New("ssh client is already connected")

	// ErrAuthenticationFailed is returned when SSH authentication fails.
	ErrAuthenticationFailed = errors.New("ssh authentication failed")

	// ErrConnectionTimeout is returned when the connection times out.
	ErrConnectionTimeout = errors.New("ssh connection timed out")
)

// Client manages SSH connections with connection pooling and automatic reconnection.
type Client struct {
	config *Config
	logger *slog.Logger

	mu      sync.RWMutex
	conn    *ssh.Client
	connCtx context.Context    //nolint:containedctx // connection lifetime context
	cancel  context.CancelFunc // cancel function for connection context
}

// ClientOption is a functional option for configuring the Client.
type ClientOption func(*Client)

// WithLogger sets a custom logger for the SSH client.
func WithLogger(logger *slog.Logger) ClientOption {
	return func(c *Client) {
		if logger != nil {
			c.logger = logger
		}
	}
}

// NewClient creates a new SSH client with the given configuration.
// The client is not connected until Connect() is called.
func NewClient(config *Config, opts ...ClientOption) (*Client, error) {
	if config == nil {
		return nil, errors.New("config is required")
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	c := &Client{
		config: config,
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

// Connect establishes an SSH connection to the configured server.
// If already connected, returns ErrAlreadyConnected.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return ErrAlreadyConnected
	}

	sshConfig, err := c.buildSSHConfig()
	if err != nil {
		return fmt.Errorf("building SSH config: %w", err)
	}

	c.logger.Debug("connecting to SSH server",
		slog.String("host", c.config.Host),
		slog.Int("port", c.config.Port),
		slog.String("user", c.config.User),
	)

	// Create a context for the connection attempt with timeout
	timeout := c.config.GetTimeout()
	dialCtx, dialCancel := context.WithTimeout(ctx, timeout)
	defer dialCancel()

	// Dial with context-aware connection
	dialer := &net.Dialer{
		Timeout: timeout,
	}

	netConn, err := dialer.DialContext(dialCtx, "tcp", c.config.Address())
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ErrConnectionTimeout
		}
		return fmt.Errorf("dialing %s: %w", c.config.Address(), err)
	}

	// Perform SSH handshake
	sshConn, chans, reqs, err := ssh.NewClientConn(netConn, c.config.Address(), sshConfig)
	if err != nil {
		_ = netConn.Close() // Best effort cleanup
		// Check for authentication failures
		if isAuthError(err) {
			return fmt.Errorf("%w: %w", ErrAuthenticationFailed, err)
		}
		return fmt.Errorf("SSH handshake failed: %w", err)
	}

	c.conn = ssh.NewClient(sshConn, chans, reqs)

	// Create connection context for keepalive management
	c.connCtx, c.cancel = context.WithCancel(context.Background())

	// Start keepalive goroutine if configured
	if interval := c.config.GetKeepaliveInterval(); interval > 0 {
		go c.keepalive(c.connCtx, interval)
	}

	c.logger.Info("SSH connection established",
		slog.String("host", c.config.Host),
		slog.Int("port", c.config.Port),
	)

	return nil
}

// Close closes the SSH connection.
// Safe to call multiple times.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil

	c.logger.Debug("SSH connection closed",
		slog.String("host", c.config.Host),
	)

	return err
}

// IsConnected returns true if the client has an active connection.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn != nil
}

// GetConnection returns the underlying SSH client connection.
// Returns ErrNotConnected if not connected.
// The connection should not be closed directly; use Client.Close() instead.
func (c *Client) GetConnection() (*ssh.Client, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conn == nil {
		return nil, ErrNotConnected
	}

	return c.conn, nil
}

// Reconnect closes any existing connection and establishes a new one.
func (c *Client) Reconnect(ctx context.Context) error {
	if err := c.Close(); err != nil {
		c.logger.Warn("error closing existing connection during reconnect",
			slog.String("error", err.Error()),
		)
	}

	return c.Connect(ctx)
}

// buildSSHConfig creates the ssh.ClientConfig from our Config.
func (c *Client) buildSSHConfig() (*ssh.ClientConfig, error) {
	authMethods, err := c.buildAuthMethods()
	if err != nil {
		return nil, fmt.Errorf("building auth methods: %w", err)
	}

	hostKeyCallback, err := c.buildHostKeyCallback()
	if err != nil {
		return nil, fmt.Errorf("building host key callback: %w", err)
	}

	return &ssh.ClientConfig{
		User:            c.config.User,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         c.config.GetTimeout(),
	}, nil
}

// buildAuthMethods creates authentication methods from the config.
func (c *Client) buildAuthMethods() ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	// Try key-based authentication first (preferred)
	if c.config.KeyFile != "" {
		keyData, err := os.ReadFile(c.config.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("reading key file %s: %w", c.config.KeyFile, err)
		}

		signer, err := c.parsePrivateKey(keyData)
		if err != nil {
			return nil, fmt.Errorf("parsing key from file: %w", err)
		}

		methods = append(methods, ssh.PublicKeys(signer))
		c.logger.Debug("added key file authentication",
			slog.String("key_file", c.config.KeyFile),
		)
	}

	// Try key data (inline key)
	if c.config.KeyData != "" {
		signer, err := c.parsePrivateKey([]byte(c.config.KeyData))
		if err != nil {
			return nil, fmt.Errorf("parsing key data: %w", err)
		}

		methods = append(methods, ssh.PublicKeys(signer))
		c.logger.Debug("added key data authentication")
	}

	// Fall back to password authentication
	if c.config.Password != "" {
		methods = append(methods, ssh.Password(c.config.Password))
		c.logger.Debug("added password authentication")
	}

	if len(methods) == 0 {
		return nil, errors.New("no authentication methods configured")
	}

	return methods, nil
}

// parsePrivateKey parses a private key, handling encrypted keys if a passphrase is provided.
func (c *Client) parsePrivateKey(keyData []byte) (ssh.Signer, error) {
	if c.config.KeyPassphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(c.config.KeyPassphrase))
	}
	return ssh.ParsePrivateKey(keyData)
}

// buildHostKeyCallback creates the host key callback based on config.
func (c *Client) buildHostKeyCallback() (ssh.HostKeyCallback, error) {
	// If strict host key checking is enabled, require a valid host key callback configuration
	if c.config.StrictHostKeyChecking {
		if c.config.HostKeyCallback == "" {
			// TODO: Add support for loading from known_hosts file
			return nil, errors.New("strict host key checking enabled but no known_hosts file configured - set HOST_KEY_CALLBACK to a known_hosts file path")
		}
		if c.config.HostKeyCallback == "ignore" {
			return nil, errors.New("strict host key checking enabled but HOST_KEY_CALLBACK is set to 'ignore' - these settings conflict")
		}
		// TODO: Load from known_hosts file at c.config.HostKeyCallback path
		return nil, errors.New("strict host key checking enabled but known_hosts loading not yet implemented")
	}

	// Strict checking disabled - use insecure mode
	c.logger.Warn("host key verification disabled - this is insecure",
		slog.String("host", c.config.Host),
	)
	return ssh.InsecureIgnoreHostKey(), nil //nolint:gosec // User explicitly requested skip
}

// keepalive sends periodic keepalive messages to maintain the connection.
func (c *Client) keepalive(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.RLock()
			conn := c.conn
			c.mu.RUnlock()

			if conn == nil {
				return
			}

			// Send a global request as keepalive
			_, _, err := conn.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				c.logger.Warn("keepalive failed",
					slog.String("host", c.config.Host),
					slog.String("error", err.Error()),
				)
				// Don't close here - let the next operation discover the failure
			}
		}
	}
}

// isAuthError checks if an error is an authentication-related error.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "unable to authenticate") ||
		strings.Contains(errStr, "no supported methods") ||
		strings.Contains(errStr, "permission denied") ||
		strings.Contains(errStr, "publickey") ||
		strings.Contains(errStr, "password")
}
