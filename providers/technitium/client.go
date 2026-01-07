// Package technitium implements the DNSWeaver provider interface for Technitium DNS Server.
package technitium

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// apiRecord represents a DNS record from the Technitium API.
type apiRecord struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	TTL      int      `json:"ttl"`
	RData    apiRData `json:"rData"`
	Disabled bool     `json:"disabled"`
}

// apiRData contains the record-specific data from Technitium.
type apiRData struct {
	IPAddress string `json:"ipAddress,omitempty"` // For A records
	CName     string `json:"cname,omitempty"`     // For CNAME records
	Text      string `json:"text,omitempty"`      // For TXT records
}

// apiResponse is the standard Technitium API response wrapper.
type apiResponse struct {
	Status       string          `json:"status"`
	ErrorMessage string          `json:"errorMessage,omitempty"`
	Response     json.RawMessage `json:"response,omitempty"`
}

// zoneRecordsResponse is the response from the zones/records/get endpoint.
type zoneRecordsResponse struct {
	Zone    zoneInfo    `json:"zone"`
	Name    string      `json:"name"`
	Records []apiRecord `json:"records"`
}

// zoneInfo contains zone metadata from the API response.
type zoneInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Disabled bool   `json:"disabled"`
}

// Client is a Technitium DNS Server API client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	logger     *slog.Logger
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

// NewClient creates a new Technitium API client.
func NewClient(baseURL, token string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// doRequest performs an HTTP request to the Technitium API.
func (c *Client) doRequest(ctx context.Context, endpoint string, params url.Values) (*apiResponse, error) {
	// Add token to params
	if params == nil {
		params = url.Values{}
	}
	params.Set("token", c.token)

	reqURL := fmt.Sprintf("%s%s?%s", c.baseURL, endpoint, params.Encode())

	c.logger.Debug("making API request",
		slog.String("endpoint", endpoint),
		slog.String("url", c.baseURL+endpoint),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var apiResp apiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parsing response JSON: %w", err)
	}

	if apiResp.Status == "error" {
		// Detect "record already exists" error and return ErrConflict
		if strings.Contains(strings.ToLower(apiResp.ErrorMessage), "record already exists") {
			return nil, fmt.Errorf("API error: %s: %w", apiResp.ErrorMessage, provider.ErrConflict)
		}
		return nil, fmt.Errorf("API error: %s", apiResp.ErrorMessage)
	}

	return &apiResp, nil
}

// Ping checks connectivity to the Technitium server.
// Uses the /api/user/session/get endpoint which is lightweight.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.doRequest(ctx, "/api/user/session/get", nil)
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}
	return nil
}

// AddARecord creates an A record in the specified zone.
func (c *Client) AddARecord(ctx context.Context, zone, hostname, ip string, ttl int) error {
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("domain", hostname)
	params.Set("type", "A")
	params.Set("ipAddress", ip)
	params.Set("ttl", strconv.Itoa(ttl))

	_, err := c.doRequest(ctx, "/api/zones/records/add", params)
	if err != nil {
		return fmt.Errorf("adding A record for %s: %w", hostname, err)
	}

	c.logger.Info("added A record",
		slog.String("hostname", hostname),
		slog.String("ip", ip),
		slog.String("zone", zone),
		slog.Int("ttl", ttl),
	)

	return nil
}

// AddCNAMERecord creates a CNAME record in the specified zone.
func (c *Client) AddCNAMERecord(ctx context.Context, zone, hostname, target string, ttl int) error {
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("domain", hostname)
	params.Set("type", "CNAME")
	params.Set("cname", target)
	params.Set("ttl", strconv.Itoa(ttl))

	_, err := c.doRequest(ctx, "/api/zones/records/add", params)
	if err != nil {
		return fmt.Errorf("adding CNAME record for %s: %w", hostname, err)
	}

	c.logger.Info("added CNAME record",
		slog.String("hostname", hostname),
		slog.String("target", target),
		slog.String("zone", zone),
		slog.Int("ttl", ttl),
	)

	return nil
}

// DeleteARecord removes an A record from the specified zone.
func (c *Client) DeleteARecord(ctx context.Context, zone, hostname, ip string) error {
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("domain", hostname)
	params.Set("type", "A")
	params.Set("ipAddress", ip)

	_, err := c.doRequest(ctx, "/api/zones/records/delete", params)
	if err != nil {
		return fmt.Errorf("deleting A record for %s: %w", hostname, err)
	}

	c.logger.Info("deleted A record",
		slog.String("hostname", hostname),
		slog.String("ip", ip),
		slog.String("zone", zone),
	)

	return nil
}

// DeleteCNAMERecord removes a CNAME record from the specified zone.
func (c *Client) DeleteCNAMERecord(ctx context.Context, zone, hostname, target string) error {
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("domain", hostname)
	params.Set("type", "CNAME")
	params.Set("cname", target)

	_, err := c.doRequest(ctx, "/api/zones/records/delete", params)
	if err != nil {
		return fmt.Errorf("deleting CNAME record for %s: %w", hostname, err)
	}

	c.logger.Info("deleted CNAME record",
		slog.String("hostname", hostname),
		slog.String("target", target),
		slog.String("zone", zone),
	)

	return nil
}

// AddTXTRecord creates a TXT record in the specified zone.
func (c *Client) AddTXTRecord(ctx context.Context, zone, hostname, text string, ttl int) error {
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("domain", hostname)
	params.Set("type", "TXT")
	params.Set("text", text)
	params.Set("ttl", strconv.Itoa(ttl))

	_, err := c.doRequest(ctx, "/api/zones/records/add", params)
	if err != nil {
		return fmt.Errorf("adding TXT record for %s: %w", hostname, err)
	}

	c.logger.Info("added TXT record",
		slog.String("hostname", hostname),
		slog.String("text", text),
		slog.String("zone", zone),
		slog.Int("ttl", ttl),
	)

	return nil
}

// DeleteTXTRecord removes a TXT record from the specified zone.
func (c *Client) DeleteTXTRecord(ctx context.Context, zone, hostname, text string) error {
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("domain", hostname)
	params.Set("type", "TXT")
	params.Set("text", text)

	_, err := c.doRequest(ctx, "/api/zones/records/delete", params)
	if err != nil {
		return fmt.Errorf("deleting TXT record for %s: %w", hostname, err)
	}

	c.logger.Info("deleted TXT record",
		slog.String("hostname", hostname),
		slog.String("text", text),
		slog.String("zone", zone),
	)

	return nil
}

// GetRecords retrieves all records for a given hostname in the specified zone.
func (c *Client) GetRecords(ctx context.Context, zone, hostname string) ([]apiRecord, error) {
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("domain", hostname)

	apiResp, err := c.doRequest(ctx, "/api/zones/records/get", params)
	if err != nil {
		return nil, fmt.Errorf("getting records for %s: %w", hostname, err)
	}

	var recordsResp zoneRecordsResponse
	if err := json.Unmarshal(apiResp.Response, &recordsResp); err != nil {
		return nil, fmt.Errorf("parsing records response: %w", err)
	}

	c.logger.Debug("retrieved records",
		slog.String("hostname", hostname),
		slog.String("zone", zone),
		slog.Int("count", len(recordsResp.Records)),
	)

	return recordsResp.Records, nil
}

// ListZoneRecords retrieves all records in a zone.
// This is used for listing all managed records.
func (c *Client) ListZoneRecords(ctx context.Context, zone string) ([]apiRecord, error) {
	params := url.Values{}
	params.Set("zone", zone)
	// Omit domain to get all records in the zone
	params.Set("listZone", "true")

	apiResp, err := c.doRequest(ctx, "/api/zones/records/get", params)
	if err != nil {
		return nil, fmt.Errorf("listing zone %s: %w", zone, err)
	}

	// The listZone response has a slightly different format
	var result struct {
		Zone    zoneInfo    `json:"zone"`
		Records []apiRecord `json:"records"`
	}
	if err := json.Unmarshal(apiResp.Response, &result); err != nil {
		return nil, fmt.Errorf("parsing zone records response: %w", err)
	}

	c.logger.Debug("listed zone records",
		slog.String("zone", zone),
		slog.Int("count", len(result.Records)),
	)

	return result.Records, nil
}
