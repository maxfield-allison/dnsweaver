// Package incus provides a client for interacting with the Incus REST API.
//
// Incus (https://linuxcontainers.org/incus/) is the LinuxContainers fork of LXD
// and manages system containers and virtual machines over a RESTful API. The
// API is exposed two ways:
//
//   - A local Unix socket (e.g. /var/lib/incus/unix.socket) for unauthenticated
//     local access. This is the simplest mode when dnsweaver runs on the Incus
//     host itself.
//   - A remote HTTPS endpoint (default port 8443) authenticated with a TLS
//     client certificate/key pair. Use this when dnsweaver runs off-host.
//
// The client only performs read-only GETs against the instances collection, so
// it deliberately avoids the official Incus Go SDK (and its large transitive
// dependency tree) in favor of the standard library plus the shared httputil
// TLS helpers.
//
// Example usage:
//
//	client, err := incus.NewClient(incus.ClientConfig{
//	    SocketPath: "/var/lib/incus/unix.socket",
//	    Project:    "default",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	instances, err := client.ListInstances(ctx)
package incus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
)

// socketBaseURL is the placeholder base URL used in Unix-socket mode. The host
// is irrelevant because the custom dialer ignores it and always connects to the
// configured socket path; only the request path matters.
const socketBaseURL = "http://incus"

// ClientConfig holds configuration for the Incus API client.
type ClientConfig struct {
	// BaseURL is the remote Incus API base URL (e.g. "https://incus.example.com:8443").
	// Must not have a trailing slash. Mutually exclusive with SocketPath.
	BaseURL string

	// SocketPath is the local Incus Unix socket path
	// (e.g. "/var/lib/incus/unix.socket"). Mutually exclusive with BaseURL.
	SocketPath string

	// Project is the Incus project to query. Empty string uses the Incus
	// default project ("default"). Ignored when AllProjects is true.
	Project string

	// AllProjects queries every project the client's credentials can see, via
	// the API's all-projects mode, instead of a single project. When true,
	// Project is ignored. Requires server-wide (unrestricted) credentials.
	AllProjects bool

	// TLS is the unified TLS configuration for remote HTTPS endpoints. Incus
	// remote authentication uses a client certificate/key pair (CertFile and
	// KeyFile). Ignored in socket mode.
	TLS *httputil.TLSConfig

	// HTTPTimeout is the HTTP client timeout. Defaults to 15 seconds if zero.
	HTTPTimeout time.Duration

	// Logger is the slog logger. Defaults to slog.Default() if nil.
	Logger *slog.Logger
}

// Client is an Incus REST API client.
type Client struct {
	baseURL     string
	project     string
	allProjects bool
	http        *http.Client
	logger      *slog.Logger
}

// NewClient creates a new Incus API client from the given config. Exactly one
// of BaseURL or SocketPath must be set. Returns an error otherwise.
func NewClient(cfg ClientConfig) (*Client, error) {
	switch {
	case cfg.BaseURL == "" && cfg.SocketPath == "":
		return nil, fmt.Errorf("incus: either BaseURL or SocketPath is required")
	case cfg.BaseURL != "" && cfg.SocketPath != "":
		return nil, fmt.Errorf("incus: set exactly one of BaseURL or SocketPath, not both")
	}

	timeout := cfg.HTTPTimeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	var (
		httpClient *http.Client
		baseURL    string
	)

	if cfg.SocketPath != "" {
		socket := cfg.SocketPath
		transport := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
		}
		httpClient = &http.Client{Timeout: timeout, Transport: transport}
		baseURL = socketBaseURL
	} else {
		httpClient = httputil.NewClient(&httputil.ClientConfig{
			Timeout:   timeout,
			TLS:       cfg.TLS,
			UserAgent: httputil.DefaultUserAgent,
			Logger:    logger,
		})
		baseURL = strings.TrimRight(cfg.BaseURL, "/")
	}

	return &Client{
		baseURL:     baseURL,
		project:     cfg.Project,
		allProjects: cfg.AllProjects,
		http:        httpClient,
		logger:      logger,
	}, nil
}

// apiResponse is the standard Incus REST API response envelope. Synchronous
// reads return their payload in Metadata; errors set Type to "error" and
// populate Error.
type apiResponse struct {
	Type       string          `json:"type"`
	StatusCode int             `json:"status_code"`
	Error      string          `json:"error"`
	Metadata   json.RawMessage `json:"metadata"`
}

// Instance represents a single Incus instance (system container or VM) as
// returned by GET /1.0/instances with recursion. Only fields relevant to DNS
// hostname extraction are modeled.
type Instance struct {
	// Name is the instance name (e.g. "web-server").
	Name string `json:"name"`

	// Type is "container" for system containers or "virtual-machine" for VMs.
	Type string `json:"type"`

	// Status is the human-readable status (e.g. "Running", "Stopped").
	Status string `json:"status"`

	// StatusCode is the numeric status code (103 == Running).
	StatusCode int `json:"status_code"`

	// Project is the Incus project the instance belongs to.
	Project string `json:"project"`

	// Config holds the instance configuration keys (including user.* keys).
	Config map[string]string `json:"config"`

	// State holds runtime state, including resolved network addresses. Present
	// only when the instance list is requested with recursion>=2.
	State *InstanceState `json:"state"`
}

// InstanceState holds the runtime state of an instance.
type InstanceState struct {
	// Network maps interface names (e.g. "eth0", "lo") to their network state.
	Network map[string]InstanceNetwork `json:"network"`
}

// InstanceNetwork holds the network state of a single interface.
type InstanceNetwork struct {
	// Addresses are the IP addresses assigned to this interface.
	Addresses []InstanceAddress `json:"addresses"`
}

// InstanceAddress represents a single IP address on an interface.
type InstanceAddress struct {
	// Family is "inet" (IPv4) or "inet6" (IPv6).
	Family string `json:"family"`

	// Address is the IP address string (without prefix length).
	Address string `json:"address"`

	// Netmask is the network prefix length as a string (e.g. "24").
	Netmask string `json:"netmask"`

	// Scope is "global", "link", or "local".
	Scope string `json:"scope"`
}

// ListInstances returns all instances (containers and VMs) in the configured
// project (or across all projects when AllProjects is set), including their
// runtime network state. The state is required for IP resolution, so
// recursion=2 is always requested.
func (c *Client) ListInstances(ctx context.Context) ([]Instance, error) {
	q := url.Values{}
	q.Set("recursion", "2")
	switch {
	case c.allProjects:
		q.Set("all-projects", "true")
	case c.project != "":
		q.Set("project", c.project)
	}

	var instances []Instance
	if err := c.get(ctx, "/1.0/instances?"+q.Encode(), &instances); err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}

	return instances, nil
}

// get performs a GET request to the given API path, unwraps the Incus response
// envelope, and decodes the metadata payload into dst. The path must start
// with "/".
func (c *Client) get(ctx context.Context, path string, dst any) error {
	reqURL := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", httputil.DefaultUserAgent)

	c.logger.Debug("incus api request", slog.String("path", path))

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("performing request to %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body from %s: %w", path, err)
	}

	var env apiResponse
	// Always attempt to decode the envelope; Incus returns it for both success
	// and error responses, and it carries a more useful message than the raw
	// status line.
	decodeErr := json.Unmarshal(body, &env)

	if resp.StatusCode != http.StatusOK {
		if decodeErr == nil && env.Error != "" {
			return fmt.Errorf("incus api error: status %d for %s: %s", resp.StatusCode, path, env.Error)
		}
		return fmt.Errorf("incus api error: status %d for %s: %s", resp.StatusCode, path, truncate(string(body), 200))
	}

	if decodeErr != nil {
		return fmt.Errorf("decoding response envelope from %s: %w", path, decodeErr)
	}
	if env.Type == "error" {
		return fmt.Errorf("incus api error for %s: %s", path, env.Error)
	}

	if err := json.Unmarshal(env.Metadata, dst); err != nil {
		return fmt.Errorf("decoding metadata from %s: %w", path, err)
	}

	return nil
}

// truncate returns s truncated to at most n bytes, appending an ellipsis when
// truncation occurs.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
