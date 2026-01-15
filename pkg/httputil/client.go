// Package httputil provides shared HTTP client utilities for DNSWeaver providers.
package httputil

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"time"
)

// Default HTTP client configuration values.
const (
	// DefaultTimeout is the default HTTP client timeout.
	DefaultTimeout = 30 * time.Second

	// DefaultUserAgent is used when no custom user agent is specified.
	DefaultUserAgent = "dnsweaver/1.0"
)

// ClientConfig contains configuration for creating an HTTP client.
type ClientConfig struct {
	// Timeout is the HTTP client timeout. Defaults to 30 seconds.
	Timeout time.Duration

	// TLSSkipVerify controls whether to skip TLS certificate verification.
	// WARNING: This should only be used for testing or when connecting to
	// servers with self-signed certificates. It is insecure for production.
	TLSSkipVerify bool

	// UserAgent is the User-Agent header to set on requests.
	// Defaults to "dnsweaver/1.0" if not specified.
	UserAgent string

	// Logger enables debug logging for HTTP requests.
	// If nil, no debug logging is performed.
	Logger *slog.Logger
}

// userAgentTransport wraps an http.RoundTripper to add User-Agent header
// and optionally log requests at debug level.
type userAgentTransport struct {
	base      http.RoundTripper
	userAgent string
	logger    *slog.Logger
}

// RoundTrip implements http.RoundTripper.
func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Set User-Agent if not already set
	if req.Header.Get("User-Agent") == "" && t.userAgent != "" {
		req.Header.Set("User-Agent", t.userAgent)
	}

	// Debug log the request
	if t.logger != nil {
		t.logger.Debug("HTTP request",
			slog.String("method", req.Method),
			slog.String("url", req.URL.String()),
		)
	}

	resp, err := t.base.RoundTrip(req)

	// Debug log the response
	if t.logger != nil && resp != nil {
		t.logger.Debug("HTTP response",
			slog.String("method", req.Method),
			slog.String("url", req.URL.String()),
			slog.Int("status", resp.StatusCode),
		)
	}

	return resp, err
}

// NewClient creates an HTTP client with the specified configuration.
// If cfg is nil, defaults are used (30s timeout, TLS verification enabled).
func NewClient(cfg *ClientConfig) *http.Client {
	if cfg == nil {
		cfg = &ClientConfig{}
	}

	// Apply defaults
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = DefaultUserAgent
	}

	// Start with default transport
	baseTransport := http.DefaultTransport

	// Configure TLS if needed
	if cfg.TLSSkipVerify {
		baseTransport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // Intentional: user explicitly requested skip
			},
		}
	}

	// Wrap with User-Agent and logging transport
	transport := &userAgentTransport{
		base:      baseTransport,
		userAgent: userAgent,
		logger:    cfg.Logger,
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

// NewClientWithTransport creates an HTTP client with custom transport settings.
// This allows advanced configuration like custom TLS roots, proxies, etc.
func NewClientWithTransport(timeout time.Duration, transport *http.Transport) *http.Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

// DefaultClient returns a new HTTP client with default settings.
// Equivalent to NewClient(nil).
func DefaultClient() *http.Client {
	return NewClient(nil)
}
