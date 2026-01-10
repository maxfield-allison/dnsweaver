// Package pihole implements the DNSWeaver provider interface for Pi-hole DNS.
package pihole

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

const (
	// DefaultTimeout is the HTTP client timeout.
	DefaultTimeout = 30 * time.Second
)

// piholeRecord represents a DNS record from Pi-hole's API.
type piholeRecord struct {
	Hostname string
	Type     provider.RecordType
	Target   string
}

// customDNSResponse represents Pi-hole's custom DNS list response.
// The API returns: {"data": [["10.0.0.1", "host1.local"], ["10.0.0.2", "host2.local"]]}
type customDNSResponse struct {
	Data [][]string `json:"data"`
}

// cnameResponse represents Pi-hole's CNAME list response.
// The API returns: {"data": [["alias.local", "target.local"], ...]}
type cnameResponse struct {
	Data [][]string `json:"data"`
}

// APIClient handles HTTP communication with Pi-hole's Admin API.
type APIClient struct {
	baseURL    string
	password   string
	httpClient *http.Client
	logger     *slog.Logger
	zone       string
}

// APIClientOption is a functional option for configuring the APIClient.
type APIClientOption func(*APIClient)

// WithAPILogger sets a custom logger.
func WithAPILogger(logger *slog.Logger) APIClientOption {
	return func(c *APIClient) {
		if logger != nil {
			c.logger = logger
		}
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) APIClientOption {
	return func(c *APIClient) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// NewAPIClient creates a new Pi-hole API client.
func NewAPIClient(baseURL, password, zone string, opts ...APIClientOption) *APIClient {
	c := &APIClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		password: password,
		zone:     zone,
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

// hashPassword creates a double-SHA256 hash of the password.
// Pi-hole uses this format for API authentication.
func (c *APIClient) hashPassword() string {
	// Pi-hole v5.x uses double SHA256 hash
	first := sha256.Sum256([]byte(c.password))
	second := sha256.Sum256(first[:])
	return hex.EncodeToString(second[:])
}

// buildURL constructs an API URL with authentication.
func (c *APIClient) buildURL(params url.Values) string {
	params.Set("auth", c.hashPassword())
	return fmt.Sprintf("%s%s?%s", c.baseURL, DefaultAPIPath, params.Encode())
}

// Ping checks connectivity to Pi-hole's API.
func (c *APIClient) Ping(ctx context.Context) error {
	// Use the summary endpoint as a health check
	params := url.Values{}
	params.Set("summary", "")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURL(params), nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to Pi-hole: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Pi-hole returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// List returns all custom DNS records from Pi-hole.
func (c *APIClient) List(ctx context.Context) ([]piholeRecord, error) {
	var records []piholeRecord

	// Get A/AAAA records from customdns
	aRecords, err := c.listCustomDNS(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing custom DNS: %w", err)
	}
	records = append(records, aRecords...)

	// Get CNAME records
	cnameRecords, err := c.listCNAME(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing CNAME: %w", err)
	}
	records = append(records, cnameRecords...)

	// Filter by zone if configured
	if c.zone != "" {
		var filtered []piholeRecord
		for _, r := range records {
			if strings.HasSuffix(r.Hostname, "."+c.zone) || r.Hostname == c.zone {
				filtered = append(filtered, r)
			}
		}
		records = filtered
	}

	c.logger.Debug("listed records",
		slog.Int("count", len(records)),
		slog.String("zone", c.zone))

	return records, nil
}

// listCustomDNS retrieves custom DNS (A/AAAA) records.
func (c *APIClient) listCustomDNS(ctx context.Context) ([]piholeRecord, error) {
	params := url.Values{}
	params.Set("customdns", "")
	params.Set("action", "get")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURL(params), nil)
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
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result customDNSResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	var records []piholeRecord
	for _, entry := range result.Data {
		if len(entry) < 2 {
			continue
		}
		ip := entry[0]
		hostname := entry[1]

		// Determine record type from IP format
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			c.logger.Warn("skipping invalid IP",
				slog.String("ip", ip),
				slog.String("hostname", hostname))
			continue
		}

		recordType := provider.RecordTypeA
		if parsedIP.To4() == nil && parsedIP.To16() != nil {
			recordType = provider.RecordTypeAAAA
		}

		records = append(records, piholeRecord{
			Hostname: hostname,
			Type:     recordType,
			Target:   ip,
		})
	}

	return records, nil
}

// listCNAME retrieves CNAME records.
func (c *APIClient) listCNAME(ctx context.Context) ([]piholeRecord, error) {
	params := url.Values{}
	params.Set("customcname", "")
	params.Set("action", "get")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURL(params), nil)
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
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result cnameResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	var records []piholeRecord
	for _, entry := range result.Data {
		if len(entry) < 2 {
			continue
		}
		hostname := entry[0]
		target := entry[1]

		records = append(records, piholeRecord{
			Hostname: hostname,
			Type:     provider.RecordTypeCNAME,
			Target:   target,
		})
	}

	return records, nil
}

// Create adds a DNS record via Pi-hole's API.
func (c *APIClient) Create(ctx context.Context, record piholeRecord) error {
	switch record.Type {
	case provider.RecordTypeA, provider.RecordTypeAAAA:
		return c.createCustomDNS(ctx, record)
	case provider.RecordTypeCNAME:
		return c.createCNAME(ctx, record)
	default:
		return fmt.Errorf("unsupported record type: %s", record.Type)
	}
}

// createCustomDNS creates an A or AAAA record.
func (c *APIClient) createCustomDNS(ctx context.Context, record piholeRecord) error {
	params := url.Values{}
	params.Set("customdns", "")
	params.Set("action", "add")
	params.Set("ip", record.Target)
	params.Set("domain", record.Hostname)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURL(params), nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	// Pi-hole returns {"success": true} on success
	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message,omitempty"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		// Some versions return plain text
		if resp.StatusCode == http.StatusOK {
			c.logger.Debug("created A/AAAA record",
				slog.String("hostname", record.Hostname),
				slog.String("target", record.Target))
			return nil
		}
		return fmt.Errorf("parsing response: %w", err)
	}

	if !result.Success {
		// Check if it's a duplicate (not an error)
		if strings.Contains(result.Message, "already exists") {
			c.logger.Debug("record already exists",
				slog.String("hostname", record.Hostname))
			return nil
		}
		return fmt.Errorf("API error: %s", result.Message)
	}

	c.logger.Debug("created A/AAAA record",
		slog.String("hostname", record.Hostname),
		slog.String("target", record.Target))

	return nil
}

// createCNAME creates a CNAME record.
func (c *APIClient) createCNAME(ctx context.Context, record piholeRecord) error {
	params := url.Values{}
	params.Set("customcname", "")
	params.Set("action", "add")
	params.Set("domain", record.Hostname)
	params.Set("target", record.Target)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURL(params), nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message,omitempty"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		if resp.StatusCode == http.StatusOK {
			c.logger.Debug("created CNAME record",
				slog.String("hostname", record.Hostname),
				slog.String("target", record.Target))
			return nil
		}
		return fmt.Errorf("parsing response: %w", err)
	}

	if !result.Success {
		if strings.Contains(result.Message, "already exists") {
			c.logger.Debug("CNAME already exists",
				slog.String("hostname", record.Hostname))
			return nil
		}
		return fmt.Errorf("API error: %s", result.Message)
	}

	c.logger.Debug("created CNAME record",
		slog.String("hostname", record.Hostname),
		slog.String("target", record.Target))

	return nil
}

// Delete removes a DNS record via Pi-hole's API.
func (c *APIClient) Delete(ctx context.Context, record piholeRecord) error {
	switch record.Type {
	case provider.RecordTypeA, provider.RecordTypeAAAA:
		return c.deleteCustomDNS(ctx, record)
	case provider.RecordTypeCNAME:
		return c.deleteCNAME(ctx, record)
	default:
		return fmt.Errorf("unsupported record type: %s", record.Type)
	}
}

// deleteCustomDNS deletes an A or AAAA record.
func (c *APIClient) deleteCustomDNS(ctx context.Context, record piholeRecord) error {
	params := url.Values{}
	params.Set("customdns", "")
	params.Set("action", "delete")
	params.Set("ip", record.Target)
	params.Set("domain", record.Hostname)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURL(params), nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message,omitempty"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		if resp.StatusCode == http.StatusOK {
			c.logger.Debug("deleted A/AAAA record",
				slog.String("hostname", record.Hostname))
			return nil
		}
		return fmt.Errorf("parsing response: %w", err)
	}

	if !result.Success {
		// "not found" is not an error for delete
		if strings.Contains(result.Message, "not found") ||
			strings.Contains(result.Message, "does not exist") {
			c.logger.Debug("record not found for deletion",
				slog.String("hostname", record.Hostname))
			return nil
		}
		return fmt.Errorf("API error: %s", result.Message)
	}

	c.logger.Debug("deleted A/AAAA record",
		slog.String("hostname", record.Hostname))

	return nil
}

// deleteCNAME deletes a CNAME record.
func (c *APIClient) deleteCNAME(ctx context.Context, record piholeRecord) error {
	params := url.Values{}
	params.Set("customcname", "")
	params.Set("action", "delete")
	params.Set("domain", record.Hostname)
	params.Set("target", record.Target)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURL(params), nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message,omitempty"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		if resp.StatusCode == http.StatusOK {
			c.logger.Debug("deleted CNAME record",
				slog.String("hostname", record.Hostname))
			return nil
		}
		return fmt.Errorf("parsing response: %w", err)
	}

	if !result.Success {
		if strings.Contains(result.Message, "not found") ||
			strings.Contains(result.Message, "does not exist") {
			c.logger.Debug("CNAME not found for deletion",
				slog.String("hostname", record.Hostname))
			return nil
		}
		return fmt.Errorf("API error: %s", result.Message)
	}

	c.logger.Debug("deleted CNAME record",
		slog.String("hostname", record.Hostname))

	return nil
}
