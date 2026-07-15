// Package httputil provides shared HTTP client utilities for DNSWeaver providers.
package httputil

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Default HTTP client configuration values.
const (
	// DefaultTimeout is the default HTTP client timeout.
	DefaultTimeout = 30 * time.Second

	// DefaultUserAgent is used when no custom user agent is specified.
	DefaultUserAgent = "dnsweaver/1.0"

	// DefaultTLSMinVersion is the minimum TLS protocol version negotiated by
	// default. TLS 1.2 is pinned explicitly rather than relying on the Go
	// stdlib floor so the choice is auditable and stable across toolchain
	// upgrades.
	DefaultTLSMinVersion uint16 = tls.VersionTLS12
)

// TLSConfig holds shared TLS settings used by every HTTP-based provider and
// source. It is consumed by NewClient and may be reused unchanged by future
// non-HTTP transports (e.g. gRPC).
//
// Zero-value semantics: an empty TLSConfig produces a nil *tls.Config from
// Build(), which means "use stdlib defaults". This makes it safe to embed in
// configuration structs without forcing every caller to populate it.
type TLSConfig struct {
	// CAFile is the path to a PEM-encoded CA bundle that will be APPENDED to
	// the system root pool. Leave empty to trust only the system roots.
	// Use this when the upstream server presents a certificate issued by a
	// private CA that is not in the container image's trust store.
	CAFile string

	// CertFile is the path to a PEM-encoded client certificate used for
	// mutual TLS authentication. Must be set together with KeyFile.
	CertFile string

	// KeyFile is the path to the PEM-encoded private key for the client
	// certificate. Must be set together with CertFile.
	KeyFile string

	// ServerName overrides the hostname used for SNI and certificate
	// verification. Leave empty to derive it from the request URL (the
	// normal Go behavior).
	ServerName string

	// InsecureSkip bypasses certificate verification entirely. WARNING:
	// only use this for testing or against self-signed certificates whose
	// chain cannot be supplied via CAFile. NewClient logs a WARN when this
	// is enabled.
	InsecureSkip bool

	// PinnedSHA256 pins the server's leaf certificate to a specific SHA-256
	// fingerprint (hex, colons and case optional). When set, the server's
	// presented leaf certificate is verified against this fingerprint instead
	// of the default chain/hostname check. This is used to trust a self-signed
	// server whose certificate cannot be validated by hostname (e.g. Incus,
	// whose default certificate only carries loopback SANs) while remaining
	// cryptographically anchored. An explicit InsecureSkip takes precedence.
	PinnedSHA256 string

	// MinVersion is the minimum TLS protocol version. Defaults to
	// DefaultTLSMinVersion (TLS 1.2) when zero. Accepts tls.VersionTLS12
	// and tls.VersionTLS13.
	MinVersion uint16
}

// IsZero reports whether the TLSConfig has any non-default settings.
// A zero TLSConfig contributes no *tls.Config to the resulting client.
func (t TLSConfig) IsZero() bool {
	return t.CAFile == "" &&
		t.CertFile == "" &&
		t.KeyFile == "" &&
		t.ServerName == "" &&
		!t.InsecureSkip &&
		t.PinnedSHA256 == "" &&
		t.MinVersion == 0
}

// Build materializes the TLSConfig into a *tls.Config.
// Returns (nil, nil) when no settings are configured, signaling to callers
// that stdlib defaults should be used. Returns an error when CAFile cannot
// be read or parsed, or when CertFile/KeyFile do not load as a valid
// X.509 keypair.
//
// When CertFile or KeyFile is set, both must be set.
func (t TLSConfig) Build() (*tls.Config, error) {
	if t.IsZero() {
		return nil, nil
	}

	minVersion := t.MinVersion
	if minVersion == 0 {
		minVersion = DefaultTLSMinVersion
	}

	out := &tls.Config{
		MinVersion:         minVersion,
		ServerName:         t.ServerName,
		InsecureSkipVerify: t.InsecureSkip, //nolint:gosec // operator-controlled; warning logged by caller
	}

	if t.CAFile != "" {
		pem, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, permissionHint(fmt.Errorf("reading TLS CA file %q: %w", t.CAFile, err))
		}
		// Clone the system pool so we add to system trust rather than
		// replacing it. SystemCertPool may fail on platforms without a
		// usable system pool (e.g. minimal scratch images); fall back to
		// an empty pool in that case so the CAFile still takes effect.
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("TLS CA file %q contained no valid PEM certificates", t.CAFile)
		}
		out.RootCAs = pool
	}

	// Client certificate (mTLS). Both halves required together.
	certSet := t.CertFile != ""
	keySet := t.KeyFile != ""
	if certSet != keySet {
		return nil, fmt.Errorf("TLS CertFile and KeyFile must both be set for mTLS (got CertFile=%q KeyFile=%q)", t.CertFile, t.KeyFile)
	}
	if certSet {
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, permissionHint(fmt.Errorf("loading TLS client keypair (cert=%q key=%q): %w", t.CertFile, t.KeyFile, err))
		}
		out.Certificates = []tls.Certificate{cert}
	}

	// Certificate pinning. When a leaf fingerprint is pinned and the operator
	// has not explicitly disabled verification, replace the default
	// chain/hostname check with an exact match against the pinned SHA-256.
	// This trusts a specific self-signed server (e.g. Incus, whose default
	// certificate only has loopback SANs) without weakening to a blanket skip.
	if t.PinnedSHA256 != "" && !t.InsecureSkip {
		pin := normalizeFingerprint(t.PinnedSHA256)
		// The default verifier is disabled because it would reject the
		// self-signed / hostname-mismatched certificate before VerifyConnection
		// runs; the pin below is the replacement, not a downgrade.
		out.InsecureSkipVerify = true //nolint:gosec // verification replaced by the certificate pin in VerifyConnection
		out.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("tls: server presented no certificate for pinned verification")
			}
			sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
			got := hex.EncodeToString(sum[:])
			if !strings.EqualFold(got, pin) {
				return fmt.Errorf("tls: server certificate SHA-256 %s does not match pinned fingerprint %s", got, pin)
			}
			return nil
		}
	}

	return out, nil
}

// normalizeFingerprint lowercases a hex certificate fingerprint and strips
// colon and whitespace separators so pins may be supplied in any common form
// ("AA:BB:...", "aabb...", with or without spaces).
func normalizeFingerprint(fp string) string {
	replacer := strings.NewReplacer(":", "", " ", "", "\t", "", "\n", "")
	return strings.ToLower(replacer.Replace(fp))
}

// permissionHint augments a file-access error with the uid/gid the process is
// actually running as. The container entrypoint drops privileges to the
// unprivileged "dnsweaver" user (uid/gid 1000) via su-exec even when the
// container is started as root, so a "permission denied" on a cert/key mounted
// root:root 0600 is a common and confusing failure: the operator sees root on
// the host but the binary is not root inside the container. The hint points at
// the actual runtime uid/gid and the docs so the fix (chown to that uid/gid, or
// make the file group-readable, or use Docker secrets) is obvious.
//
// Returns the error unchanged when it is nil or not a permission error.
func permissionHint(err error) error {
	if err == nil || !errors.Is(err, fs.ErrPermission) {
		return err
	}
	return fmt.Errorf("%w (dnsweaver runs as uid=%d gid=%d after dropping privileges; "+
		"the file must be readable by that user — chown it to that uid/gid, make it group-readable, "+
		"or mount it as a Docker secret: "+
		"https://maxfield-allison.github.io/dnsweaver/configuration/environment/#tls-certificate-file-permissions)",
		err, os.Getuid(), os.Getgid())
}

// ParseTLSMinVersion converts a user-facing version string (e.g. "1.2", "1.3",
// "TLS1.2", "tls1.3") into the matching tls.VersionTLS* constant. Returns 0
// and an error for unrecognized inputs. An empty string returns (0, nil) so
// callers can apply their own default.
func ParseTLSMinVersion(s string) (uint16, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "tls")
	s = strings.TrimPrefix(s, "v")
	switch s {
	case "":
		return 0, nil
	case "1.2", "12":
		return tls.VersionTLS12, nil
	case "1.3", "13":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unsupported TLS min version %q (use \"1.2\" or \"1.3\")", s)
	}
}

// ClientConfig contains configuration for creating an HTTP client.
type ClientConfig struct {
	// Timeout is the HTTP client timeout. Defaults to 30 seconds.
	Timeout time.Duration

	// TLS is the unified TLS configuration. When nil (or zero-valued),
	// stdlib defaults are used. When non-nil, the configured settings are
	// merged into a transport CLONED from http.DefaultTransport so that
	// HTTP/2 negotiation, proxy env vars, dial/idle timeouts, and the
	// shared connection pool are preserved.
	TLS *TLSConfig

	// TLSSkipVerify is a legacy shortcut for TLS.InsecureSkip. When TLS is
	// nil and this is true, NewClient internally promotes it to a
	// TLSConfig{InsecureSkip: true}. Deprecated: prefer TLS.InsecureSkip.
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

// sensitiveQueryParams lists query parameter names that may contain credentials.
// These are redacted from URLs before logging to prevent secret leakage.
var sensitiveQueryParams = map[string]bool{
	"token":    true,
	"auth":     true,
	"password": true,
	"key":      true,
	"secret":   true,
	"sid":      true,
	"apikey":   true,
}

// sanitizeURL returns a URL string with sensitive query parameters redacted.
// This prevents credentials from appearing in debug logs.
func sanitizeURL(u *url.URL) string {
	if u == nil {
		return ""
	}

	query := u.Query()
	redacted := false
	for param := range query {
		if sensitiveQueryParams[param] {
			query.Set(param, "REDACTED")
			redacted = true
		}
	}

	if !redacted {
		return u.String()
	}

	// Rebuild URL with redacted query
	sanitized := *u
	sanitized.RawQuery = query.Encode()
	return sanitized.String()
}

// RoundTrip implements http.RoundTripper.
func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Set User-Agent if not already set
	if req.Header.Get("User-Agent") == "" && t.userAgent != "" {
		req.Header.Set("User-Agent", t.userAgent)
	}

	// Debug log the request (with sensitive query params redacted)
	if t.logger != nil {
		t.logger.Debug("HTTP request",
			slog.String("method", req.Method),
			slog.String("url", sanitizeURL(req.URL)),
		)
	}

	resp, err := t.base.RoundTrip(req)

	// Debug log the response (with sensitive query params redacted)
	if t.logger != nil && resp != nil {
		t.logger.Debug("HTTP response",
			slog.String("method", req.Method),
			slog.String("url", sanitizeURL(req.URL)),
			slog.Int("status", resp.StatusCode),
		)
	}

	return resp, err
}

// NewClient creates an HTTP client with the specified configuration.
// If cfg is nil, defaults are used (30s timeout, TLS verification enabled).
//
// When cfg.TLS is non-nil and not zero-valued, the client's transport is
// cloned from http.DefaultTransport and the configured *tls.Config is
// applied to the clone. This preserves HTTP/2, proxy environment handling,
// idle timeouts, and the stdlib connection pool — all of which a bare
// &http.Transport{TLSClientConfig: …} would silently discard.
//
// On TLS construction errors NewClient logs the error and returns a client
// using stdlib defaults; it does not return an error so that the existing
// signature stays back-compatible. Providers that need fail-fast behavior
// should call TLSConfig.Build() themselves before constructing the client.
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

	// Promote the legacy TLSSkipVerify shortcut into a TLSConfig so all
	// downstream logic operates on the single unified type.
	tlsCfg := cfg.TLS
	if tlsCfg == nil && cfg.TLSSkipVerify {
		tlsCfg = &TLSConfig{InsecureSkip: true}
	}

	baseTransport := buildTransport(tlsCfg, cfg.Logger)

	// Warn loudly when verification is skipped — operators frequently set
	// this once for debugging and forget to remove it.
	if tlsCfg != nil && tlsCfg.InsecureSkip && cfg.Logger != nil {
		cfg.Logger.Warn("TLS certificate verification disabled — connections are vulnerable to MITM")
	}

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

// buildTransport returns the base RoundTripper for NewClient. When tlsCfg is
// nil or zero, http.DefaultTransport is returned unchanged so we share the
// stdlib's global connection pool. Otherwise DefaultTransport is CLONED and
// the materialized *tls.Config is applied to the clone.
func buildTransport(tlsCfg *TLSConfig, logger *slog.Logger) http.RoundTripper {
	if tlsCfg == nil || tlsCfg.IsZero() {
		return http.DefaultTransport
	}

	built, err := tlsCfg.Build()
	if err != nil {
		// We can't fail the constructor without breaking the existing
		// signature, but we MUST surface the misconfiguration. Log loudly
		// and fall back to stdlib defaults — the request will then fail
		// with a clear x509 error rather than silently bypassing TLS.
		if logger != nil {
			logger.Error("TLS configuration failed to build, falling back to stdlib defaults",
				slog.String("error", err.Error()),
			)
		}
		return http.DefaultTransport
	}

	// Clone the default transport to inherit HTTP/2, proxy, dial, idle, and
	// pool defaults. Fall back to a fresh &http.Transport{} only if the
	// stdlib's default ever changes type (shouldn't happen in practice).
	defaultHTTPTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{TLSClientConfig: built}
	}
	cloned := defaultHTTPTransport.Clone()
	cloned.TLSClientConfig = built
	return cloned
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

// Response size limits for protection against OOM from oversized responses.
const (
	// MaxResponseSize is the default maximum response body size (10 MB).
	// This prevents OOM if a server sends an unexpectedly large response.
	MaxResponseSize int64 = 10 * 1024 * 1024
)

// ReadBody reads and returns the response body, limiting the size to prevent OOM.
// If maxBytes is 0, MaxResponseSize is used.
// Returns an error if the response exceeds the limit.
func ReadBody(resp *http.Response, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = MaxResponseSize
	}

	// Read up to maxBytes+1 to detect if the response exceeds the limit
	limited := io.LimitReader(resp.Body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("response body exceeds maximum size of %d bytes", maxBytes)
	}

	return data, nil
}
