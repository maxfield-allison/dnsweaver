// Package technitium implements the DNSWeaver provider interface for Technitium DNS Server.
package technitium

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// apiRecord represents a DNS record from the Technitium API.
type apiRecord struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	TTL      int      `json:"ttl"`
	RData    apiRData `json:"rData"`
	Disabled bool     `json:"disabled"`
}

// svcParamsValue handles Technitium's dual representation of svcParams.
// Older API versions return a pipe-delimited string ("alpn|h2"),
// while newer versions return a JSON object ({"alpn": "h2"}).
type svcParamsValue string

// UnmarshalJSON implements json.Unmarshaler to accept both string and object forms.
func (v *svcParamsValue) UnmarshalJSON(data []byte) error {
	// Try string form first (write-path format: "alpn|h2")
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*v = svcParamsValue(s)
		return nil
	}
	// Try object form (read-path format: {"alpn": "h2"} or {"alpn": ["h2","h3"]})
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("svcParams: cannot parse as string or object: %s", data)
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic ordering
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		var valStr string
		if err := json.Unmarshal(obj[k], &valStr); err == nil {
			parts = append(parts, k+"|"+valStr)
			continue
		}
		var valArr []string
		if err := json.Unmarshal(obj[k], &valArr); err == nil {
			parts = append(parts, k+"|"+strings.Join(valArr, ","))
		}
	}
	*v = svcParamsValue(strings.Join(parts, " "))
	return nil
}

// apiRData contains the record-specific data from Technitium.
type apiRData struct {
	IPAddress string `json:"ipAddress,omitempty"` // For A/AAAA records
	CName     string `json:"cname,omitempty"`     // For CNAME records
	Text      string `json:"text,omitempty"`      // For TXT records
	// SRV record fields
	Priority  int    `json:"priority,omitempty"` // For SRV records
	Weight    int    `json:"weight,omitempty"`   // For SRV records
	Port      int    `json:"port,omitempty"`     // For SRV records
	SrvTarget string `json:"target,omitempty"`   // For SRV records
	// HTTPS/SVCB record fields
	SvcPriority   int            `json:"svcPriority,omitempty"`   // For HTTPS/SVCB records
	SvcTargetName string         `json:"svcTargetName,omitempty"` // For HTTPS/SVCB records ("." = self)
	SvcParams     svcParamsValue `json:"svcParams,omitempty"`     // For HTTPS/SVCB records (e.g., "alpn|h2")
}

// Note: older Technitium versions might represent svcParams differently; keep parsing flexible.

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

// WithInsecureSkipVerify configures the client to skip TLS certificate verification.
// WARNING: This should only be used for testing or when connecting to servers with
// self-signed certificates. It is insecure and should not be used in production.
//
// Deprecated: TLS settings are now configured framework-wide via the unified
// httputil.TLSConfig wired through FactoryConfig.HTTP.TLS. This option remains
// for backward compatibility with callers that construct Client directly; when
// used, it replaces only the transport's TLS config rather than the entire
// http.Client, so it composes with prior WithHTTPClient(…) calls.
func WithInsecureSkipVerify(skip bool) ClientOption {
	return func(c *Client) {
		if !skip {
			return
		}
		// Build a fresh client that inherits stdlib transport defaults
		// (HTTP/2, proxy env, connection pool) and only flips the
		// skip-verify bit. This intentionally REPLACES c.httpClient
		// rather than mutating its transport in place because the
		// previously-set client may share its transport with other
		// callers — mutating it would leak the change globally.
		c.httpClient = httputil.NewClient(&httputil.ClientConfig{
			TLS: &httputil.TLSConfig{InsecureSkip: true},
		})
	}
}

// NewClient creates a new Technitium API client.
func NewClient(baseURL, token string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL:    baseURL,
		token:      token,
		httpClient: httputil.DefaultClient(),
		logger:     slog.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// doRequest performs an HTTP request to the Technitium API.
func (c *Client) doRequest(ctx context.Context, endpoint string, params url.Values) (result *apiResponse, err error) {
	// Older supported servers require query-string authentication. Keep that
	// contract, but protect every returned error before callers can log it.
	defer func() { err = redactRequestError(err, c.token) }()

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

	body, err := httputil.ReadBody(resp, 0)
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
		// Detect "record already exists" errors and return ErrConflict.
		// Technitium returns different messages:
		// - "Record already exists" (general)
		// - "Identical record already exists" (error code 81058)
		errLower := strings.ToLower(apiResp.ErrorMessage)
		if strings.Contains(errLower, "record already exists") ||
			strings.Contains(errLower, "identical record") {
			return nil, fmt.Errorf("API error: %s: %w", apiResp.ErrorMessage, provider.ErrConflict)
		}
		// Detect CNAME type conflicts.
		// Technitium returns: "a CNAME record cannot exists with other record types"
		if strings.Contains(errLower, "cname record cannot") {
			return nil, fmt.Errorf("API error: %s: %w", apiResp.ErrorMessage, provider.ErrTypeConflict)
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

// AddAAAARecord creates an AAAA (IPv6) record in the specified zone.
func (c *Client) AddAAAARecord(ctx context.Context, zone, hostname, ip string, ttl int) error {
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("domain", hostname)
	params.Set("type", "AAAA")
	params.Set("ipAddress", ip)
	params.Set("ttl", strconv.Itoa(ttl))

	_, err := c.doRequest(ctx, "/api/zones/records/add", params)
	if err != nil {
		return fmt.Errorf("adding AAAA record for %s: %w", hostname, err)
	}

	c.logger.Info("added AAAA record",
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

// DeleteAAAARecord removes an AAAA (IPv6) record from the specified zone.
func (c *Client) DeleteAAAARecord(ctx context.Context, zone, hostname, ip string) error {
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("domain", hostname)
	params.Set("type", "AAAA")
	params.Set("ipAddress", ip)

	_, err := c.doRequest(ctx, "/api/zones/records/delete", params)
	if err != nil {
		return fmt.Errorf("deleting AAAA record for %s: %w", hostname, err)
	}

	c.logger.Info("deleted AAAA record",
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

// AddSRVRecord creates an SRV record in the specified zone.
func (c *Client) AddSRVRecord(ctx context.Context, zone, hostname string, priority, weight, port int, target string, ttl int) error {
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("domain", hostname)
	params.Set("type", "SRV")
	params.Set("priority", strconv.Itoa(priority))
	params.Set("weight", strconv.Itoa(weight))
	params.Set("port", strconv.Itoa(port))
	params.Set("target", target)
	params.Set("ttl", strconv.Itoa(ttl))

	_, err := c.doRequest(ctx, "/api/zones/records/add", params)
	if err != nil {
		return fmt.Errorf("adding SRV record for %s: %w", hostname, err)
	}

	c.logger.Info("added SRV record",
		slog.String("hostname", hostname),
		slog.Int("priority", priority),
		slog.Int("weight", weight),
		slog.Int("port", port),
		slog.String("target", target),
		slog.String("zone", zone),
		slog.Int("ttl", ttl),
	)

	return nil
}

// DeleteSRVRecord removes an SRV record from the specified zone.
func (c *Client) DeleteSRVRecord(ctx context.Context, zone, hostname string, priority, weight, port int, target string) error {
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("domain", hostname)
	params.Set("type", "SRV")
	params.Set("priority", strconv.Itoa(priority))
	params.Set("weight", strconv.Itoa(weight))
	params.Set("port", strconv.Itoa(port))
	params.Set("target", target)

	_, err := c.doRequest(ctx, "/api/zones/records/delete", params)
	if err != nil {
		return fmt.Errorf("deleting SRV record for %s: %w", hostname, err)
	}

	c.logger.Info("deleted SRV record",
		slog.String("hostname", hostname),
		slog.Int("priority", priority),
		slog.Int("weight", weight),
		slog.Int("port", port),
		slog.String("target", target),
		slog.String("zone", zone),
	)

	return nil
}

// AddHTTPSRecord creates an HTTPS (SVCB Type 65) record in the specified zone.
// The svcPriority controls record mode: 0 = AliasMode, 1+ = ServiceMode.
// The svcTargetName uses "." to indicate the record's owner name (self-referential).
// The svcParams is in Technitium's pipe-separated format (e.g., "alpn|h2").
func (c *Client) AddHTTPSRecord(ctx context.Context, zone, hostname string, svcPriority int, svcTargetName, svcParams string, ttl int) error {
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("domain", hostname)
	params.Set("type", "HTTPS")
	params.Set("svcPriority", strconv.Itoa(svcPriority))
	params.Set("svcTargetName", svcTargetName)
	params.Set("svcParams", svcParams)
	params.Set("ttl", strconv.Itoa(ttl))

	_, err := c.doRequest(ctx, "/api/zones/records/add", params)
	if err != nil {
		return fmt.Errorf("adding HTTPS record for %s: %w", hostname, err)
	}

	c.logger.Info("added HTTPS record",
		slog.String("hostname", hostname),
		slog.Int("svc_priority", svcPriority),
		slog.String("svc_target", svcTargetName),
		slog.String("svc_params", svcParams),
		slog.String("zone", zone),
		slog.Int("ttl", ttl),
	)

	return nil
}

// DeleteHTTPSRecord removes an HTTPS (SVCB Type 65) record from the specified zone.
// Note: implemented earlier; duplicate guard to ensure compile stability when patching.

// parseSvcParams splits svcParams into key/value pairs for future use.
// Input: "alpn|h2" → map{"alpn":"h2"}
func parseSvcParams(s string) map[string]string {
	out := make(map[string]string)
	for _, part := range strings.Split(s, " ") {
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "|", 2)
		if len(kv) == 2 {
			out[kv[0]] = kv[1]
		}
	}
	return out
}

// DeleteHTTPSRecord removes an HTTPS (SVCB Type 65) record from the specified zone.
func (c *Client) DeleteHTTPSRecord(ctx context.Context, zone, hostname string, svcPriority int, svcTargetName, svcParams string) error {
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("domain", hostname)
	params.Set("type", "HTTPS")
	params.Set("svcPriority", strconv.Itoa(svcPriority))
	params.Set("svcTargetName", svcTargetName)
	params.Set("svcParams", svcParams)

	_, err := c.doRequest(ctx, "/api/zones/records/delete", params)
	if err != nil {
		return fmt.Errorf("deleting HTTPS record for %s: %w", hostname, err)
	}

	c.logger.Info("deleted HTTPS record",
		slog.String("hostname", hostname),
		slog.Int("svc_priority", svcPriority),
		slog.String("svc_target", svcTargetName),
		slog.String("svc_params", svcParams),
		slog.String("zone", zone),
	)

	return nil
}

// UpdateARecord updates an A record's target IP address in the specified zone.
// The Technitium API requires the old IP to identify the record.
func (c *Client) UpdateARecord(ctx context.Context, zone, hostname, oldIP, newIP string, ttl int) error {
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("domain", hostname)
	params.Set("type", "A")
	params.Set("ipAddress", oldIP)
	params.Set("newIpAddress", newIP)
	params.Set("ttl", strconv.Itoa(ttl))

	_, err := c.doRequest(ctx, "/api/zones/records/update", params)
	if err != nil {
		return fmt.Errorf("updating A record for %s: %w", hostname, err)
	}

	c.logger.Info("updated A record",
		slog.String("hostname", hostname),
		slog.String("old_ip", oldIP),
		slog.String("new_ip", newIP),
		slog.String("zone", zone),
		slog.Int("ttl", ttl),
	)

	return nil
}

// UpdateAAAARecord updates an AAAA (IPv6) record's target IP address in the specified zone.
func (c *Client) UpdateAAAARecord(ctx context.Context, zone, hostname, oldIP, newIP string, ttl int) error {
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("domain", hostname)
	params.Set("type", "AAAA")
	params.Set("ipAddress", oldIP)
	params.Set("newIpAddress", newIP)
	params.Set("ttl", strconv.Itoa(ttl))

	_, err := c.doRequest(ctx, "/api/zones/records/update", params)
	if err != nil {
		return fmt.Errorf("updating AAAA record for %s: %w", hostname, err)
	}

	c.logger.Info("updated AAAA record",
		slog.String("hostname", hostname),
		slog.String("old_ip", oldIP),
		slog.String("new_ip", newIP),
		slog.String("zone", zone),
		slog.Int("ttl", ttl),
	)

	return nil
}

// UpdateCNAMERecord updates a CNAME record's target in the specified zone.
//
// CNAME records are unique per domain name (only one CNAME may exist at a name),
// so the Technitium API's record-update endpoint identifies the record by
// (zone, domain, type) alone. The new target is sent as the `cname` parameter.
// The Technitium API does not define a `newCname` parameter; the previous use of
// `cname=oldTarget` + `newCname=newTarget` was a silent no-op (the API matched
// the existing record by `cname` and saw no change to apply). See issue #84.
//
// The oldTarget argument is retained for logging and call-site symmetry with
// UpdateARecord/UpdateAAAARecord; it is not sent to the Technitium API.
func (c *Client) UpdateCNAMERecord(ctx context.Context, zone, hostname, oldTarget, newTarget string, ttl int) error {
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("domain", hostname)
	params.Set("type", "CNAME")
	params.Set("cname", newTarget)
	params.Set("ttl", strconv.Itoa(ttl))

	_, err := c.doRequest(ctx, "/api/zones/records/update", params)
	if err != nil {
		return fmt.Errorf("updating CNAME record for %s: %w", hostname, err)
	}

	c.logger.Info("updated CNAME record",
		slog.String("hostname", hostname),
		slog.String("old_target", oldTarget),
		slog.String("new_target", newTarget),
		slog.String("zone", zone),
		slog.Int("ttl", ttl),
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
	// The domain parameter is required by the API even with listZone=true
	// Setting it to the zone apex returns all records in the zone
	params.Set("domain", zone)
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
