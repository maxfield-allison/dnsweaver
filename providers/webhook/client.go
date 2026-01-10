// Package webhook implements the DNSWeaver provider interface for webhook-based DNS integrations.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Webhook API request/response types.
// These define the contract between DNSWeaver and webhook endpoints.

// RecordRequest is the request body for create operations.
type RecordRequest struct {
	Hostname string   `json:"hostname"`
	Type     string   `json:"type"`
	Value    string   `json:"value"`
	TTL      int      `json:"ttl"`
	SRV      *SRVData `json:"srv,omitempty"` // SRV-specific data (only for SRV records)
}

// SRVData contains SRV record-specific fields for webhook requests.
type SRVData struct {
	Priority uint16 `json:"priority"`
	Weight   uint16 `json:"weight"`
	Port     uint16 `json:"port"`
}

// DeleteRequest is the request body for delete operations.
type DeleteRequest struct {
	Hostname string `json:"hostname"`
	Type     string `json:"type,omitempty"`
}

// RecordResponse represents a single DNS record returned by the webhook.
type RecordResponse struct {
	Hostname string   `json:"hostname"`
	Type     string   `json:"type"`
	Value    string   `json:"value"`
	TTL      int      `json:"ttl,omitempty"`
	ID       string   `json:"id,omitempty"`
	SRV      *SRVData `json:"srv,omitempty"` // SRV-specific data (only for SRV records)
}

// ErrorResponse is the expected error response format from webhooks.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Code    int    `json:"code,omitempty"`
}

// Client is a webhook HTTP client.
type Client struct {
	baseURL    string
	authHeader string
	authToken  string
	httpClient *http.Client
	logger     *slog.Logger
	retries    int
	retryDelay time.Duration
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

// WithRetries sets the number of retry attempts for transient failures.
func WithRetries(retries int) ClientOption {
	return func(c *Client) {
		if retries >= 0 {
			c.retries = retries
		}
	}
}

// WithRetryDelay sets the base delay between retry attempts.
func WithRetryDelay(delay time.Duration) ClientOption {
	return func(c *Client) {
		if delay >= 0 {
			c.retryDelay = delay
		}
	}
}

// NewClient creates a new webhook client.
func NewClient(baseURL string, timeout time.Duration, authHeader, authToken string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		authHeader: authHeader,
		authToken:  authToken,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger:     slog.Default(),
		retries:    DefaultRetries,
		retryDelay: DefaultRetryDelay,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// isRetryable returns true if the status code indicates a transient failure.
func isRetryable(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// doRequest performs an HTTP request with retry logic.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, []byte, error) {
	reqURL := c.baseURL + path

	c.logger.Debug("making webhook request",
		slog.String("method", method),
		slog.String("url", reqURL),
	)

	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			// Wait before retry with exponential backoff
			delay := c.retryDelay * time.Duration(1<<(attempt-1))
			c.logger.Debug("retrying request",
				slog.Int("attempt", attempt),
				slog.Duration("delay", delay),
			)
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(delay):
			}

			// Re-read body if it was consumed (need to recreate for retry)
			// This is handled by the caller passing in a fresh reader
		}

		// Create a new body reader for this attempt if body is not nil
		var bodyReader io.Reader
		if body != nil {
			// For retries, we need to be able to re-read the body
			// The caller should pass a *bytes.Reader or similar
			if seeker, ok := body.(io.Seeker); ok {
				_, _ = seeker.Seek(0, io.SeekStart)
			}
			bodyReader = body
		}

		req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
		if err != nil {
			return nil, nil, fmt.Errorf("creating request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		// Add custom auth header if configured
		if c.authHeader != "" && c.authToken != "" {
			req.Header.Set(c.authHeader, c.authToken)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("executing request: %w", err)
			continue // Retry on network errors
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("reading response body: %w", err)
			continue
		}

		// Check if we should retry
		if isRetryable(resp.StatusCode) && attempt < c.retries {
			lastErr = fmt.Errorf("server returned %d", resp.StatusCode)
			continue
		}

		return resp, respBody, nil
	}

	return nil, nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// Ping checks connectivity to the webhook endpoint.
// Sends GET /ping and expects 200 OK.
func (c *Client) Ping(ctx context.Context) error {
	resp, _, err := c.doRequest(ctx, http.MethodGet, "/ping", nil)
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping failed: unexpected status %d", resp.StatusCode)
	}

	return nil
}

// List retrieves all DNS records from the webhook.
// Sends GET /list and expects a JSON array of RecordResponse.
func (c *Client) List(ctx context.Context) ([]RecordResponse, error) {
	resp, body, err := c.doRequest(ctx, http.MethodGet, "/list", nil)
	if err != nil {
		return nil, fmt.Errorf("list failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Try to parse error response
		var errResp ErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("list failed: %s", errResp.Error)
		}
		return nil, fmt.Errorf("list failed: unexpected status %d", resp.StatusCode)
	}

	var records []RecordResponse
	if err := json.Unmarshal(body, &records); err != nil {
		return nil, fmt.Errorf("parsing list response: %w", err)
	}

	c.logger.Debug("listed records from webhook",
		slog.Int("count", len(records)),
	)

	return records, nil
}

// Create sends a request to create a DNS record.
// Sends POST /create with RecordRequest body.
func (c *Client) Create(ctx context.Context, hostname, recordType, value string, ttl int) error {
	reqBody := RecordRequest{
		Hostname: hostname,
		Type:     recordType,
		Value:    value,
		TTL:      ttl,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	resp, body, err := c.doRequest(ctx, http.MethodPost, "/create", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create failed: %w", err)
	}

	// Accept 200 OK, 201 Created, or 204 No Content
	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusCreated &&
		resp.StatusCode != http.StatusNoContent {
		// Try to parse error response
		var errResp ErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("create failed: %s", errResp.Error)
		}
		return fmt.Errorf("create failed: unexpected status %d", resp.StatusCode)
	}

	c.logger.Info("created record via webhook",
		slog.String("hostname", hostname),
		slog.String("type", recordType),
		slog.String("value", value),
		slog.Int("ttl", ttl),
	)

	return nil
}

// CreateSRV sends a request to create an SRV record with SRV-specific data.
// Sends POST /create with RecordRequest body including SRV data.
func (c *Client) CreateSRV(ctx context.Context, hostname string, priority, weight, port uint16, target string, ttl int) error {
	reqBody := RecordRequest{
		Hostname: hostname,
		Type:     "SRV",
		Value:    target,
		TTL:      ttl,
		SRV: &SRVData{
			Priority: priority,
			Weight:   weight,
			Port:     port,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	resp, body, err := c.doRequest(ctx, http.MethodPost, "/create", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create SRV failed: %w", err)
	}

	// Accept 200 OK, 201 Created, or 204 No Content
	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusCreated &&
		resp.StatusCode != http.StatusNoContent {
		// Try to parse error response
		var errResp ErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("create SRV failed: %s", errResp.Error)
		}
		return fmt.Errorf("create SRV failed: unexpected status %d", resp.StatusCode)
	}

	c.logger.Info("created SRV record via webhook",
		slog.String("hostname", hostname),
		slog.Uint64("priority", uint64(priority)),
		slog.Uint64("weight", uint64(weight)),
		slog.Uint64("port", uint64(port)),
		slog.String("target", target),
		slog.Int("ttl", ttl),
	)

	return nil
}

// Delete sends a request to delete a DNS record.
// Sends DELETE /delete with DeleteRequest body.
func (c *Client) Delete(ctx context.Context, hostname, recordType string) error {
	reqBody := DeleteRequest{
		Hostname: hostname,
		Type:     recordType,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	resp, body, err := c.doRequest(ctx, http.MethodDelete, "/delete", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}

	// Accept 200 OK, 204 No Content, or 404 Not Found (idempotent delete)
	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusNoContent &&
		resp.StatusCode != http.StatusNotFound {
		// Try to parse error response
		var errResp ErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("delete failed: %s", errResp.Error)
		}
		return fmt.Errorf("delete failed: unexpected status %d", resp.StatusCode)
	}

	c.logger.Info("deleted record via webhook",
		slog.String("hostname", hostname),
		slog.String("type", recordType),
	)

	return nil
}
