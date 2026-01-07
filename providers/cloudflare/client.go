// Package cloudflare implements the DNSWeaver provider interface for Cloudflare DNS.
package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

const (
	// DefaultAPIEndpoint is the base URL for Cloudflare API v4.
	DefaultAPIEndpoint = "https://api.cloudflare.com/client/v4"

	// DefaultTimeout is the HTTP client timeout.
	DefaultTimeout = 30 * time.Second
)

// apiError represents an error from the Cloudflare API.
type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// apiResponse is the standard Cloudflare API response wrapper.
type apiResponse struct {
	Success  bool            `json:"success"`
	Errors   []apiError      `json:"errors"`
	Messages []string        `json:"messages"`
	Result   json.RawMessage `json:"result"`
}

// zoneResult represents a zone from the Cloudflare API.
type zoneResult struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// zonesResponse wraps the zones list response.
type zonesResponse struct {
	Result []zoneResult `json:"result"`
}

// dnsRecord represents a DNS record from the Cloudflare API.
type dnsRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
	ZoneID  string `json:"zone_id"`
}

// dnsRecordsResponse wraps the DNS records list response.
type dnsRecordsResponse struct {
	Result []dnsRecord `json:"result"`
}

// createRecordRequest is the request body for creating a DNS record.
type createRecordRequest struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

// Client is a Cloudflare DNS API client.
type Client struct {
	apiEndpoint string
	token       string
	httpClient  *http.Client
	logger      *slog.Logger
}

// ClientOption is a functional option for configuring the Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// WithLogger sets a custom logger.
func WithLogger(logger *slog.Logger) ClientOption {
	return func(c *Client) {
		if logger != nil {
			c.logger = logger
		}
	}
}

// WithAPIEndpoint sets a custom API endpoint (useful for testing).
func WithAPIEndpoint(endpoint string) ClientOption {
	return func(c *Client) {
		c.apiEndpoint = endpoint
	}
}

// NewClient creates a new Cloudflare API client.
func NewClient(token string, opts ...ClientOption) *Client {
	c := &Client{
		apiEndpoint: DefaultAPIEndpoint,
		token:       token,
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// doRequest performs an HTTP request to the Cloudflare API.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*apiResponse, error) {
	reqURL := fmt.Sprintf("%s%s", c.apiEndpoint, path)

	c.logger.Debug("making API request",
		slog.String("method", method),
		slog.String("path", path),
	)

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	// Handle non-2xx status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to parse as API response for error details
		var apiResp apiResponse
		if err := json.Unmarshal(respBody, &apiResp); err == nil && len(apiResp.Errors) > 0 {
			errCode := apiResp.Errors[0].Code
			errMsg := apiResp.Errors[0].Message
			// Error code 81053 = "record with that host already exists"
			// Error code 81058 = "An identical record already exists"
			if errCode == 81053 || errCode == 81058 {
				return nil, provider.ErrConflict
			}
			return nil, fmt.Errorf("API error: %s (code: %d)", errMsg, errCode)
		}
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp apiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("parsing response JSON: %w", err)
	}

	if !apiResp.Success {
		if len(apiResp.Errors) > 0 {
			return nil, fmt.Errorf("API error: %s (code: %d)", apiResp.Errors[0].Message, apiResp.Errors[0].Code)
		}
		return nil, fmt.Errorf("API request failed with unknown error")
	}

	return &apiResp, nil
}

// Ping checks connectivity to the Cloudflare API.
// Uses the /user/tokens/verify endpoint which is lightweight.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.doRequest(ctx, http.MethodGet, "/user/tokens/verify", nil)
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}
	return nil
}

// GetZoneID returns the zone ID for a given domain name.
// It looks up the zone by name using the Cloudflare API.
func (c *Client) GetZoneID(ctx context.Context, domain string) (string, error) {
	// Find the root zone for this domain by progressively stripping subdomains
	parts := strings.Split(domain, ".")
	for i := 0; i < len(parts)-1; i++ {
		zoneName := strings.Join(parts[i:], ".")
		params := url.Values{}
		params.Set("name", zoneName)
		params.Set("status", "active")

		resp, err := c.doRequest(ctx, http.MethodGet, "/zones?"+params.Encode(), nil)
		if err != nil {
			continue // Try next level
		}

		var zones zonesResponse
		if err := json.Unmarshal(resp.Result, &zones.Result); err != nil {
			continue
		}

		if len(zones.Result) > 0 {
			c.logger.Debug("found zone",
				slog.String("domain", domain),
				slog.String("zone", zoneName),
				slog.String("zone_id", zones.Result[0].ID),
			)
			return zones.Result[0].ID, nil
		}
	}

	return "", fmt.Errorf("no zone found for domain %s", domain)
}

// ListRecords returns all DNS records of the specified type in the given zone.
func (c *Client) ListRecords(ctx context.Context, zoneID string, recordType string) ([]dnsRecord, error) {
	params := url.Values{}
	params.Set("type", recordType)
	params.Set("per_page", "100") // Max per page

	path := fmt.Sprintf("/zones/%s/dns_records?%s", zoneID, params.Encode())
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("listing records: %w", err)
	}

	var records dnsRecordsResponse
	if err := json.Unmarshal(resp.Result, &records.Result); err != nil {
		return nil, fmt.Errorf("parsing records response: %w", err)
	}

	c.logger.Debug("listed records",
		slog.String("zone_id", zoneID),
		slog.String("type", recordType),
		slog.Int("count", len(records.Result)),
	)

	return records.Result, nil
}

// CreateRecord creates a new DNS record in the specified zone.
func (c *Client) CreateRecord(ctx context.Context, zoneID string, recordType, name, content string, ttl int, proxied bool) error {
	reqBody := createRecordRequest{
		Type:    recordType,
		Name:    name,
		Content: content,
		TTL:     ttl,
		Proxied: proxied,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	path := fmt.Sprintf("/zones/%s/dns_records", zoneID)
	_, err = c.doRequest(ctx, http.MethodPost, path, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return fmt.Errorf("creating record: %w", err)
	}

	c.logger.Info("created DNS record",
		slog.String("zone_id", zoneID),
		slog.String("type", recordType),
		slog.String("name", name),
		slog.String("content", content),
		slog.Int("ttl", ttl),
		slog.Bool("proxied", proxied),
	)

	return nil
}

// DeleteRecord deletes a DNS record by ID.
func (c *Client) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	path := fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID)
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("deleting record: %w", err)
	}

	c.logger.Info("deleted DNS record",
		slog.String("zone_id", zoneID),
		slog.String("record_id", recordID),
	)

	return nil
}

// FindRecord finds a DNS record by name and type in the given zone.
// Returns the record if found, nil otherwise.
func (c *Client) FindRecord(ctx context.Context, zoneID, recordType, name string) (*dnsRecord, error) {
	params := url.Values{}
	params.Set("type", recordType)
	params.Set("name", name)

	path := fmt.Sprintf("/zones/%s/dns_records?%s", zoneID, params.Encode())
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("finding record: %w", err)
	}

	var records dnsRecordsResponse
	if err := json.Unmarshal(resp.Result, &records.Result); err != nil {
		return nil, fmt.Errorf("parsing records response: %w", err)
	}

	if len(records.Result) == 0 {
		return nil, nil // Not found
	}

	return &records.Result[0], nil
}
