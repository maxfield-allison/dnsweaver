package unifi

import (
	"bytes"
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

	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// DNS policy type discriminators used by the Integration API.
const (
	policyTypeA             = "A_RECORD"
	policyTypeAAAA          = "AAAA_RECORD"
	policyTypeCNAME         = "CNAME_RECORD"
	policyTypeTXT           = "TXT_RECORD"
	policyTypeSRV           = "SRV_RECORD"
	policyTypeMX            = "MX_RECORD"
	policyTypeForwardDomain = "FORWARD_DOMAIN"
)

// integrationAPIPath is where UniFi OS consoles and UniFi OS Server serve the
// Network Integration API, relative to the console URL.
const integrationAPIPath = "/proxy/network/integration/v1"

// originUserDefined marks policies created through the UI or API, as opposed
// to entries the console derives from clients, devices or other settings.
const originUserDefined = "USER_DEFINED"

// errorCodeAlreadyExists is the API error code returned (with HTTP 400) when a
// policy with the same type and properties already exists.
const errorCodeAlreadyExists = "api.dns.policy.validation.record-already-exists"

// pageLimit is the maximum page size the Integration API accepts.
const pageLimit = 200

// maxRetries bounds the number of attempts for rate-limited GET requests.
const maxRetries = 3

// maxRetryDelay caps how long a Retry-After header may hold a request.
const maxRetryDelay = 5 * time.Second

// dnsPolicy is the wire shape of a DNS policy. The API uses one polymorphic
// object keyed on Type, so every type-specific field is optional here and the
// conversion helpers on the Provider decide which ones are meaningful.
type dnsPolicy struct {
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type"`
	Enabled  bool            `json:"enabled"`
	Metadata *policyMetadata `json:"metadata,omitempty"`

	Domain     string `json:"domain,omitempty"`
	TTLSeconds *int   `json:"ttlSeconds,omitempty"`

	IPv4Address  string `json:"ipv4Address,omitempty"`  // A_RECORD
	IPv6Address  string `json:"ipv6Address,omitempty"`  // AAAA_RECORD
	TargetDomain string `json:"targetDomain,omitempty"` // CNAME_RECORD
	Text         string `json:"text,omitempty"`         // TXT_RECORD

	// SRV_RECORD fields. Service and Protocol include their leading underscore
	// (e.g. "_ldap", "_tcp").
	Service      string `json:"service,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	ServerDomain string `json:"serverDomain,omitempty"`
	Port         *int   `json:"port,omitempty"`
	Priority     *int   `json:"priority,omitempty"`
	Weight       *int   `json:"weight,omitempty"`
}

// policyMetadata carries the console's provenance for a policy.
type policyMetadata struct {
	Origin string `json:"origin"`
}

// page is the Integration API's list envelope.
type page[T any] struct {
	Offset     int `json:"offset"`
	Limit      int `json:"limit"`
	Count      int `json:"count"`
	TotalCount int `json:"totalCount"`
	Data       []T `json:"data"`
}

// site is a UniFi Network site as returned by GET /sites.
type site struct {
	ID                string `json:"id"`
	InternalReference string `json:"internalReference"`
	Name              string `json:"name"`
}

// applicationInfo is the GET /info response.
type applicationInfo struct {
	ApplicationVersion string `json:"applicationVersion"`
}

// apiError is the Integration API's error body.
type apiError struct {
	StatusCode int    `json:"statusCode"`
	StatusName string `json:"statusName"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

// proxyError is the error body the UniFi OS proxy layer returns in front of
// the Network application, for example on a rejected API key.
type proxyError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Client is a UniFi Network Integration API client.
type Client struct {
	baseURL    string // console URL + integrationAPIPath, no trailing slash
	apiKey     string
	httpClient *http.Client
	logger     *slog.Logger
}

// ClientOption is a functional option for configuring the Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
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

// NewClient creates a new Integration API client for the console at consoleURL.
func NewClient(consoleURL, apiKey string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(consoleURL, "/") + integrationAPIPath,
		apiKey:     apiKey,
		httpClient: httputil.DefaultClient(),
		logger:     slog.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// doRequest performs an HTTP request against the Integration API and returns
// the response body on success. Non-2xx responses are mapped to the
// framework's provider sentinels so callers can use errors.Is.
//
// Rate-limited GET requests (429) are retried honoring Retry-After. Mutating
// requests are never retried: a create that timed out may already have
// succeeded, and retrying it would produce a duplicate policy.
func (c *Client) doRequest(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	reqURL := c.baseURL + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}

	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encoding request body: %w", err)
		}
		payload = encoded
	}

	c.logger.Debug("making UniFi API request",
		slog.String("method", method),
		slog.String("path", path),
	)

	for attempt := 1; ; attempt++ {
		respBody, statusCode, retryAfter, err := c.once(ctx, method, reqURL, payload)
		if err != nil {
			return nil, err
		}

		if statusCode == http.StatusTooManyRequests && method == http.MethodGet && attempt < maxRetries {
			c.logger.Debug("UniFi API rate limited, retrying",
				slog.String("path", path),
				slog.Int("attempt", attempt),
				slog.Duration("delay", retryAfter),
			)
			select {
			case <-time.After(retryAfter):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		if statusErr := mapStatusError(statusCode, respBody); statusErr != nil {
			return nil, statusErr
		}
		return respBody, nil
	}
}

// once issues a single HTTP request and returns the body, status and the
// Retry-After delay (zero when absent).
func (c *Client) once(ctx context.Context, method, reqURL string, payload []byte) ([]byte, int, time.Duration, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("X-API-KEY", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("%w: %w", provider.ErrProviderUnavailable, err)
	}
	defer resp.Body.Close()

	respBody, err := httputil.ReadBody(resp, 0)
	if err != nil {
		return nil, resp.StatusCode, 0, fmt.Errorf("reading response body: %w", err)
	}

	return respBody, resp.StatusCode, parseRetryAfter(resp.Header.Get("Retry-After")), nil
}

// Ping checks connectivity and authentication by calling GET /info and
// returns the Network application version the console reports.
func (c *Client) Ping(ctx context.Context) (string, error) {
	body, err := c.doRequest(ctx, http.MethodGet, "/info", nil, nil)
	if err != nil {
		return "", fmt.Errorf("ping failed: %w", err)
	}

	var info applicationInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return "", fmt.Errorf("parsing info response: %w", err)
	}

	c.logger.Debug("UniFi ping successful",
		slog.String("version", info.ApplicationVersion),
	)

	return info.ApplicationVersion, nil
}

// ResolveSiteID returns the UUID of the site whose internalReference or id
// matches the given name. The error for an unknown site lists the sites the
// API key can see so a typo is easy to spot.
func (c *Client) ResolveSiteID(ctx context.Context, name string) (string, error) {
	sites, err := listAll[site](ctx, c, "/sites")
	if err != nil {
		return "", fmt.Errorf("listing sites: %w", err)
	}

	available := make([]string, 0, len(sites))
	for _, s := range sites {
		if s.ID == name || s.InternalReference == name {
			return s.ID, nil
		}
		available = append(available, s.InternalReference)
	}

	return "", fmt.Errorf("site %q not found (available: %s)", name, strings.Join(available, ", "))
}

// ListDNSPolicies returns every DNS policy in the site, across all pages.
func (c *Client) ListDNSPolicies(ctx context.Context, siteID string) ([]dnsPolicy, error) {
	policies, err := listAll[dnsPolicy](ctx, c, "/sites/"+siteID+"/dns/policies")
	if err != nil {
		return nil, fmt.Errorf("listing DNS policies: %w", err)
	}

	c.logger.Debug("listed DNS policies",
		slog.String("site_id", siteID),
		slog.Int("count", len(policies)),
	)

	return policies, nil
}

// CreateDNSPolicy creates a DNS policy and returns it as stored, including its id.
func (c *Client) CreateDNSPolicy(ctx context.Context, siteID string, policy dnsPolicy) (dnsPolicy, error) {
	body, err := c.doRequest(ctx, http.MethodPost, "/sites/"+siteID+"/dns/policies", nil, policy)
	if err != nil {
		return dnsPolicy{}, fmt.Errorf("creating %s policy for %s: %w", policy.Type, policy.Domain, err)
	}

	var created dnsPolicy
	if err := json.Unmarshal(body, &created); err != nil {
		return dnsPolicy{}, fmt.Errorf("parsing created policy: %w", err)
	}

	return created, nil
}

// UpdateDNSPolicy replaces the DNS policy with the given id.
func (c *Client) UpdateDNSPolicy(ctx context.Context, siteID, policyID string, policy dnsPolicy) error {
	if policyID == "" {
		return fmt.Errorf("updating %s policy for %s: policy id is required", policy.Type, policy.Domain)
	}

	if _, err := c.doRequest(ctx, http.MethodPut, "/sites/"+siteID+"/dns/policies/"+policyID, nil, policy); err != nil {
		return fmt.Errorf("updating %s policy for %s: %w", policy.Type, policy.Domain, err)
	}

	return nil
}

// DeleteDNSPolicy removes the DNS policy with the given id.
// A policy that no longer exists yields provider.ErrNotFound.
func (c *Client) DeleteDNSPolicy(ctx context.Context, siteID, policyID string) error {
	if policyID == "" {
		return fmt.Errorf("deleting policy: policy id is required")
	}

	if _, err := c.doRequest(ctx, http.MethodDelete, "/sites/"+siteID+"/dns/policies/"+policyID, nil, nil); err != nil {
		return fmt.Errorf("deleting policy %s: %w", policyID, err)
	}

	return nil
}

// listAll pages through a list endpoint until every item has been fetched.
func listAll[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var items []T
	offset := 0

	for {
		query := url.Values{}
		query.Set("offset", strconv.Itoa(offset))
		query.Set("limit", strconv.Itoa(pageLimit))

		body, err := c.doRequest(ctx, http.MethodGet, path, query, nil)
		if err != nil {
			return nil, err
		}

		var p page[T]
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, fmt.Errorf("parsing page response: %w", err)
		}

		items = append(items, p.Data...)
		offset += len(p.Data)

		if len(p.Data) == 0 || offset >= p.TotalCount {
			return items, nil
		}
	}
}

// mapStatusError translates HTTP status codes into the framework's
// provider-level sentinel errors. The error body is used for detail when it
// decodes as either the Integration API or UniFi OS proxy error shape;
// otherwise the raw body is included (UniFi OS occasionally answers with an
// HTML error page). A duplicate policy is reported as HTTP 400 with a
// dedicated error code rather than 409, so both are mapped to ErrConflict.
func mapStatusError(status int, body []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}

	code, detail := parseErrorBody(body)

	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return fmt.Errorf("%w: unifi returned %d: %s", provider.ErrUnauthorized, status, detail)
	case status == http.StatusNotFound:
		return fmt.Errorf("%w: unifi returned %d: %s", provider.ErrNotFound, status, detail)
	case status == http.StatusConflict, code == errorCodeAlreadyExists:
		return fmt.Errorf("%w: unifi returned %d: %s", provider.ErrConflict, status, detail)
	case status >= 500:
		return fmt.Errorf("%w: unifi returned %d: %s", provider.ErrProviderUnavailable, status, detail)
	default:
		return fmt.Errorf("unifi returned %d: %s", status, detail)
	}
}

// parseErrorBody extracts the error code and a human-readable detail string
// from an error response body. It understands the Integration API shape
// ({statusCode, code, message, ...}) and the UniFi OS proxy shape
// ({error: {code, message}}), and falls back to the truncated raw body.
func parseErrorBody(body []byte) (code, detail string) {
	var apiErr apiError
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Message != "" {
		if apiErr.Code != "" {
			return apiErr.Code, truncate(apiErr.Code + ": " + apiErr.Message)
		}
		return "", truncate(apiErr.Message)
	}

	var proxyErr proxyError
	if err := json.Unmarshal(body, &proxyErr); err == nil && proxyErr.Error.Message != "" {
		return "", truncate(proxyErr.Error.Message)
	}

	return "", truncate(strings.TrimSpace(string(body)))
}

// parseRetryAfter reads a Retry-After header given in seconds, capped at
// maxRetryDelay. Missing or unparsable values yield a short default so a
// retry still backs off.
func parseRetryAfter(header string) time.Duration {
	const fallback = time.Second

	seconds, err := strconv.Atoi(strings.TrimSpace(header))
	if err != nil || seconds < 0 {
		return fallback
	}
	return min(time.Duration(seconds)*time.Second, maxRetryDelay)
}

// truncate caps a string for inclusion in error messages so a runaway HTML
// error page or huge JSON body doesn't drown the logs.
func truncate(s string) string {
	const n = 200
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
