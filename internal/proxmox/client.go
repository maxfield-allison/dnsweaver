// Package proxmox provides a client for interacting with the Proxmox VE REST API.
//
// The client authenticates using PVE API tokens (the recommended method for
// automation) and supports listing QEMU/KVM virtual machines and LXC containers
// across a Proxmox cluster or standalone node.
//
// Authentication uses the API token header format:
//
//	Authorization: PVEAPIToken=<tokenid>=<secret>
//
// where tokenid is in the form USER@REALM!TOKENNAME (e.g., "root@pam!dnsweaver").
//
// Example usage:
//
//	client, err := proxmox.NewClient(proxmox.Config{
//	    BaseURL:      "https://pve.example.com:8006",
//	    TokenID:      "root@pam!dnsweaver",
//	    TokenSecret:  "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
//	    VerifyTLS:    true,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	resources, err := client.ListClusterResources(ctx)
package proxmox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
)

// ClientConfig holds configuration for the Proxmox API client.
type ClientConfig struct {
	// BaseURL is the Proxmox API base URL (e.g., "https://pve.example.com:8006").
	// Must not have a trailing slash.
	BaseURL string

	// TokenID is the API token identifier in USER@REALM!TOKENNAME format.
	// Example: "root@pam!dnsweaver"
	TokenID string

	// TokenSecret is the API token secret (UUID format).
	TokenSecret string

	// VerifyTLS controls TLS certificate verification.
	//
	// Deprecated: use TLS for full control (CA bundle, mTLS, SNI, min version,
	// skip-verify). When TLS is non-nil, VerifyTLS is ignored. Retained so the
	// legacy DNSWEAVER_PROXMOX_VERIFY_TLS env var path still works; will be
	// removed in a future major release.
	VerifyTLS bool

	// TLS is the unified TLS configuration. When non-nil, it fully describes
	// the TLS behavior (custom CA, mTLS, SNI, min version, skip-verify). When
	// nil and VerifyTLS is false, the client falls back to skip-verify for
	// back-compat with the old behavior; when nil and VerifyTLS is true the
	// client uses stdlib defaults (system roots, TLS 1.2 floor).
	TLS *httputil.TLSConfig

	// HTTPTimeout is the HTTP client timeout. Defaults to 15 seconds if zero.
	HTTPTimeout time.Duration

	// Logger is the slog logger. Defaults to slog.Default() if nil.
	Logger *slog.Logger
}

// Client is a Proxmox VE API client.
type Client struct {
	baseURL     string
	tokenHeader string
	http        *http.Client
	logger      *slog.Logger
}

// NewClient creates a new Proxmox API client from the given config.
// Returns an error if required fields are missing.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("proxmox: BaseURL is required")
	}
	if cfg.TokenID == "" {
		return nil, fmt.Errorf("proxmox: TokenID is required")
	}
	if cfg.TokenSecret == "" {
		return nil, fmt.Errorf("proxmox: TokenSecret is required")
	}

	timeout := cfg.HTTPTimeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Resolve the effective TLS configuration. Precedence:
	//   1. cfg.TLS (the unified path used by v1.5+ callers)
	//   2. legacy cfg.VerifyTLS=false → InsecureSkip=true
	//   3. otherwise stdlib defaults (TLS=nil)
	tlsCfg := cfg.TLS
	if tlsCfg == nil && !cfg.VerifyTLS {
		tlsCfg = &httputil.TLSConfig{InsecureSkip: true}
	}

	httpClient := httputil.NewClient(&httputil.ClientConfig{
		Timeout:   timeout,
		TLS:       tlsCfg,
		UserAgent: httputil.DefaultUserAgent,
		Logger:    logger,
	})

	return &Client{
		baseURL:     cfg.BaseURL,
		tokenHeader: "PVEAPIToken=" + cfg.TokenID + "=" + cfg.TokenSecret,
		http:        httpClient,
		logger:      logger,
	}, nil
}

// ClusterResource represents a single entry from the PVE cluster resources API.
// This covers both QEMU VMs and LXC containers.
type ClusterResource struct {
	// VMID is the unique VM/container ID within the cluster.
	VMID int `json:"vmid"`

	// Name is the human-readable name of the VM or container.
	Name string `json:"name"`

	// Node is the Proxmox node that hosts this VM/container.
	Node string `json:"node"`

	// Type is "qemu" for VMs or "lxc" for containers.
	Type string `json:"type"`

	// Status is the current state (e.g., "running", "stopped").
	Status string `json:"status"`

	// Tags is the semicolon-delimited list of tags assigned to the resource.
	// Example: "dns;web;production"
	Tags string `json:"tags"`
}

// LXCConfig holds the parsed configuration of a single LXC container.
// Only fields relevant to IP resolution are populated.
type LXCConfig struct {
	// Net0 is the raw value of the net0 config field.
	// Example: "name=eth0,bridge=vmbr0,hwaddr=AA:BB:CC:DD:EE:FF,ip=192.0.2.50/24,ip6=auto"
	Net0 string `json:"net0"`
}

// AgentNetworkInterface represents a single network interface from
// the qemu-guest-agent network-get-interfaces response.
type AgentNetworkInterface struct {
	// Name is the interface name (e.g., "eth0").
	Name string `json:"name"`

	// IPAddresses contains the IP addresses assigned to this interface.
	IPAddresses []AgentIPAddress `json:"ip-addresses"`
}

// AgentIPAddress represents a single IP address from the guest agent response.
type AgentIPAddress struct {
	// IPAddressType is "ipv4" or "ipv6".
	IPAddressType string `json:"ip-address-type"`

	// IPAddress is the IP address string (without prefix length).
	IPAddress string `json:"ip-address"`
}

// ListClusterResources returns all QEMU VMs and LXC containers in the cluster.
// Works on both standalone nodes and full cluster configurations.
func (c *Client) ListClusterResources(ctx context.Context) ([]ClusterResource, error) {
	var result struct {
		Data []ClusterResource `json:"data"`
	}

	if err := c.get(ctx, "/api2/json/cluster/resources?type=vm", &result); err != nil {
		return nil, fmt.Errorf("listing cluster resources: %w", err)
	}

	return result.Data, nil
}

// GetLXCConfig returns the configuration of an LXC container.
// node is the Proxmox node name, vmid is the container ID.
func (c *Client) GetLXCConfig(ctx context.Context, node string, vmid int) (*LXCConfig, error) {
	var result struct {
		Data LXCConfig `json:"data"`
	}

	path := fmt.Sprintf("/api2/json/nodes/%s/lxc/%d/config", node, vmid)
	if err := c.get(ctx, path, &result); err != nil {
		return nil, fmt.Errorf("getting LXC config for %s/%d: %w", node, vmid, err)
	}

	return &result.Data, nil
}

// GetVMAgentNetworks returns network interface information from the qemu-guest-agent.
// Returns ErrAgentNotRunning if the guest agent is not available.
func (c *Client) GetVMAgentNetworks(ctx context.Context, node string, vmid int) ([]AgentNetworkInterface, error) {
	var result struct {
		Data struct {
			Result []AgentNetworkInterface `json:"result"`
		} `json:"data"`
	}

	path := fmt.Sprintf("/api2/json/nodes/%s/qemu/%d/agent/network-get-interfaces", node, vmid)
	if err := c.get(ctx, path, &result); err != nil {
		return nil, err
	}

	return result.Data.Result, nil
}

// ErrAgentNotRunning is returned when the qemu-guest-agent is not available on a VM.
type ErrAgentNotRunning struct {
	Node string
	VMID int
}

func (e *ErrAgentNotRunning) Error() string {
	return fmt.Sprintf("proxmox: qemu-guest-agent not running on %s/%d", e.Node, e.VMID)
}

// get performs a GET request to the given API path and decodes the JSON response
// into dst. The path must start with "/".
func (c *Client) get(ctx context.Context, path string, dst any) error {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", c.tokenHeader)
	req.Header.Set("Accept", "application/json")

	c.logger.Debug("proxmox api request", slog.String("path", path))

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("performing request to %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body from %s: %w", path, err)
	}

	if resp.StatusCode == http.StatusInternalServerError {
		// PVE returns 500 when the guest agent is not running.
		// Attempt to detect this specific case from the error message.
		var errResp struct {
			Errors map[string]string `json:"errors"`
		}
		if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil {
			for _, msg := range errResp.Errors {
				if containsAgentError(msg) {
					return parseAgentNotRunning(path)
				}
			}
		}
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("proxmox api error: status %d for %s: %s", resp.StatusCode, path, truncate(string(body), 200))
	}

	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("decoding response from %s: %w", path, err)
	}

	return nil
}

// containsAgentError returns true if the PVE error message indicates the
// guest agent is not running or not enabled.
func containsAgentError(msg string) bool {
	agentErrors := []string{
		"QEMU guest agent is not running",
		"guest agent is not running",
		"No QEMU guest agent",
	}
	for _, e := range agentErrors {
		if len(msg) >= len(e) {
			for i := 0; i <= len(msg)-len(e); i++ {
				if msg[i:i+len(e)] == e {
					return true
				}
			}
		}
	}
	return false
}

// parseAgentNotRunning extracts node and vmid from an agent API path and
// returns ErrAgentNotRunning. Falls back to a generic error if parsing fails.
func parseAgentNotRunning(path string) error {
	var node string
	var vmid int
	//nolint:errcheck // best-effort parse; fallback to generic message
	_, _ = fmt.Sscanf(path, "/api2/json/nodes/%s/qemu/%d/agent/", &node, &vmid)
	return &ErrAgentNotRunning{Node: node, VMID: vmid}
}

// truncate returns s truncated to at most n bytes.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
