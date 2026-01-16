// Package pihole implements the DNSWeaver provider interface for Pi-hole DNS.
package pihole

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// V6APIClient implements DNSClient for Pi-hole v6's REST API.
// Pi-hole v6 uses session-based authentication via SID tokens.
type V6APIClient struct {
	baseURL    string
	password   string
	zone       string
	httpClient *http.Client
	logger     *slog.Logger

	// Session management
	mu             sync.RWMutex
	sid            string
	sessionExpires time.Time
}

// V6APIClientOption is a functional option for configuring V6APIClient.
type V6APIClientOption func(*V6APIClient)

// WithV6Logger sets a custom logger.
func WithV6Logger(logger *slog.Logger) V6APIClientOption {
	return func(c *V6APIClient) {
		if logger != nil {
			c.logger = logger
		}
	}
}

// WithV6HTTPClient sets a custom HTTP client.
func WithV6HTTPClient(client *http.Client) V6APIClientOption {
	return func(c *V6APIClient) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// NewV6APIClient creates a new Pi-hole v6 API client.
func NewV6APIClient(baseURL, password, zone string, opts ...V6APIClientOption) *V6APIClient {
	c := &V6APIClient{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		password:   password,
		zone:       zone,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     slog.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Ensure V6APIClient implements DNSClient.
var _ DNSClient = (*V6APIClient)(nil)

// sessionResponse represents the auth response from Pi-hole v6.
type sessionResponse struct {
	Session struct {
		Valid    bool   `json:"valid"`
		SID      string `json:"sid"`
		CSRF     string `json:"csrf"`
		Validity int    `json:"validity"` // Seconds until expiration
		Message  string `json:"message"`
	} `json:"session"`
}

// authenticate obtains a session ID from Pi-hole v6.
func (c *V6APIClient) authenticate(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we have a valid session
	if c.sid != "" && time.Now().Before(c.sessionExpires) {
		return nil
	}

	url := c.baseURL + "/api/auth"

	payload := struct {
		Password string `json:"password"`
	}{
		Password: c.password,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling auth request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing auth request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading auth response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var session sessionResponse
	if err := json.Unmarshal(respBody, &session); err != nil {
		return fmt.Errorf("parsing auth response: %w", err)
	}

	if !session.Session.Valid {
		msg := session.Session.Message
		if msg == "" {
			msg = "invalid credentials"
		}
		return fmt.Errorf("authentication failed: %s", msg)
	}

	c.sid = session.Session.SID
	// Expire 30 seconds early to avoid race conditions
	validity := time.Duration(session.Session.Validity-30) * time.Second
	if validity < 30*time.Second {
		validity = 30 * time.Second
	}
	c.sessionExpires = time.Now().Add(validity)

	c.logger.Debug("authenticated with Pi-hole v6",
		slog.Duration("validity", validity))

	return nil
}

// getSID returns the current SID, refreshing if necessary.
func (c *V6APIClient) getSID(ctx context.Context) (string, error) {
	if err := c.authenticate(ctx); err != nil {
		return "", err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sid, nil
}

// doRequest performs an authenticated request to the Pi-hole v6 API.
// nolint:unparam // reqBody is kept for API consistency and future use with PATCH requests
func (c *V6APIClient) doRequest(ctx context.Context, method, path string, reqBody any) ([]byte, error) {
	sid, err := c.getSID(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting session: %w", err)
	}

	url := c.baseURL + path

	var bodyReader io.Reader
	if reqBody != nil {
		reqBodyBytes, marshalErr := json.Marshal(reqBody)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshaling request: %w", marshalErr)
		}
		bodyReader = bytes.NewReader(reqBodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("X-FTL-SID", sid)
	req.Header.Set("Accept", "application/json")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	// Handle auth errors - session may have expired
	if resp.StatusCode == http.StatusUnauthorized {
		// Clear session and try once more
		c.mu.Lock()
		c.sid = ""
		c.sessionExpires = time.Time{}
		c.mu.Unlock()

		return nil, fmt.Errorf("session expired, retry required")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// List retrieves all DNS records from Pi-hole v6.
func (c *V6APIClient) List(ctx context.Context) ([]piholeRecord, error) {
	// Fetch configuration which contains dns.hosts and dns.cnameRecords
	body, err := c.doRequest(ctx, http.MethodGet, "/api/config/dns", nil)
	if err != nil {
		return nil, fmt.Errorf("fetching config: %w", err)
	}

	// Response structure for dns config subset
	var result struct {
		Config struct {
			Hosts        []string `json:"hosts"`
			CnameRecords []string `json:"cnameRecords"`
		} `json:"config"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	var records []piholeRecord

	// Parse A/AAAA records from dns.hosts
	// Format: "IP HOSTNAME [HOSTNAME ...]"
	for _, entry := range result.Config.Hosts {
		hostRecords := c.parseHostEntry(entry)
		records = append(records, hostRecords...)
	}

	// Parse CNAME records from dns.cnameRecords
	// Format: "<alias>,<target>[,<ttl>]"
	for _, entry := range result.Config.CnameRecords {
		if record := c.parseCNAMEEntry(entry); record != nil {
			records = append(records, *record)
		}
	}

	c.logger.Debug("listed records from Pi-hole v6",
		slog.Int("count", len(records)))

	return records, nil
}

// parseHostEntry parses a dns.hosts entry and returns records.
// Format: "IP HOSTNAME [HOSTNAME ...]"
func (c *V6APIClient) parseHostEntry(entry string) []piholeRecord {
	parts := strings.Fields(entry)
	if len(parts) < 2 {
		return nil
	}

	ip := parts[0]
	recordType := provider.RecordTypeA
	if net.ParseIP(ip) != nil && strings.Contains(ip, ":") {
		recordType = provider.RecordTypeAAAA
	}

	var records []piholeRecord
	for _, hostname := range parts[1:] {
		records = append(records, piholeRecord{
			Hostname: hostname,
			Target:   ip,
			Type:     recordType,
		})
	}

	return records
}

// parseCNAMEEntry parses a dns.cnameRecords entry.
// Format: "<alias>,<target>[,<ttl>]"
func (c *V6APIClient) parseCNAMEEntry(entry string) *piholeRecord {
	parts := strings.Split(entry, ",")
	if len(parts) < 2 {
		return nil
	}

	return &piholeRecord{
		Hostname: strings.TrimSpace(parts[0]),
		Target:   strings.TrimSpace(parts[1]),
		Type:     provider.RecordTypeCNAME,
	}
}

// Create adds a new DNS record to Pi-hole v6.
func (c *V6APIClient) Create(ctx context.Context, record piholeRecord) error {
	switch record.Type {
	case provider.RecordTypeA, provider.RecordTypeAAAA:
		return c.createHostEntry(ctx, record)
	case provider.RecordTypeCNAME:
		return c.createCNAMEEntry(ctx, record)
	default:
		return fmt.Errorf("unsupported record type: %s", record.Type)
	}
}

// createHostEntry adds an A/AAAA record via dns.hosts.
func (c *V6APIClient) createHostEntry(ctx context.Context, record piholeRecord) error {
	// Pi-hole v6 uses PUT /api/config/dns/hosts/{value} to add to the array
	// Format: "IP HOSTNAME"
	value := fmt.Sprintf("%s %s", record.Target, record.Hostname)

	// URL-encode the value for the path
	path := fmt.Sprintf("/api/config/dns/hosts/%s", value)

	_, err := c.doRequest(ctx, http.MethodPut, path, nil)
	if err != nil {
		// Check if already exists (not an error for create)
		if strings.Contains(err.Error(), "already exists") ||
			strings.Contains(err.Error(), "duplicate") {
			c.logger.Debug("host entry already exists",
				slog.String("hostname", record.Hostname),
				slog.String("ip", record.Target))
			return nil
		}
		return fmt.Errorf("creating host entry: %w", err)
	}

	c.logger.Debug("created A/AAAA record",
		slog.String("hostname", record.Hostname),
		slog.String("ip", record.Target))

	return nil
}

// createCNAMEEntry adds a CNAME record via dns.cnameRecords.
func (c *V6APIClient) createCNAMEEntry(ctx context.Context, record piholeRecord) error {
	// Format: "alias,target"
	value := fmt.Sprintf("%s,%s", record.Hostname, record.Target)

	path := fmt.Sprintf("/api/config/dns/cnameRecords/%s", value)

	_, err := c.doRequest(ctx, http.MethodPut, path, nil)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") ||
			strings.Contains(err.Error(), "duplicate") {
			c.logger.Debug("CNAME already exists",
				slog.String("hostname", record.Hostname),
				slog.String("target", record.Target))
			return nil
		}
		return fmt.Errorf("creating CNAME entry: %w", err)
	}

	c.logger.Debug("created CNAME record",
		slog.String("hostname", record.Hostname),
		slog.String("target", record.Target))

	return nil
}

// Delete removes a DNS record from Pi-hole v6.
func (c *V6APIClient) Delete(ctx context.Context, record piholeRecord) error {
	switch record.Type {
	case provider.RecordTypeA, provider.RecordTypeAAAA:
		return c.deleteHostEntry(ctx, record)
	case provider.RecordTypeCNAME:
		return c.deleteCNAMEEntry(ctx, record)
	default:
		return fmt.Errorf("unsupported record type: %s", record.Type)
	}
}

// deleteHostEntry removes an A/AAAA record from dns.hosts.
func (c *V6APIClient) deleteHostEntry(ctx context.Context, record piholeRecord) error {
	// Format: "IP HOSTNAME"
	value := fmt.Sprintf("%s %s", record.Target, record.Hostname)

	path := fmt.Sprintf("/api/config/dns/hosts/%s", value)

	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		// Not found is not an error for delete (idempotent)
		if strings.Contains(err.Error(), "not found") ||
			strings.Contains(err.Error(), "does not exist") ||
			strings.Contains(err.Error(), "404") {
			c.logger.Debug("host entry not found for deletion",
				slog.String("hostname", record.Hostname))
			return nil
		}
		return fmt.Errorf("deleting host entry: %w", err)
	}

	c.logger.Debug("deleted A/AAAA record",
		slog.String("hostname", record.Hostname))

	return nil
}

// deleteCNAMEEntry removes a CNAME record from dns.cnameRecords.
func (c *V6APIClient) deleteCNAMEEntry(ctx context.Context, record piholeRecord) error {
	// Format: "alias,target"
	value := fmt.Sprintf("%s,%s", record.Hostname, record.Target)

	path := fmt.Sprintf("/api/config/dns/cnameRecords/%s", value)

	_, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		if strings.Contains(err.Error(), "not found") ||
			strings.Contains(err.Error(), "does not exist") ||
			strings.Contains(err.Error(), "404") {
			c.logger.Debug("CNAME not found for deletion",
				slog.String("hostname", record.Hostname))
			return nil
		}
		return fmt.Errorf("deleting CNAME entry: %w", err)
	}

	c.logger.Debug("deleted CNAME record",
		slog.String("hostname", record.Hostname))

	return nil
}
