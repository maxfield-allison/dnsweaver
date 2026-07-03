package opnsense

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// Client is a low-level HTTP client for the OPNsense REST API. It knows about
// authentication, transport, and response envelopes, but not about DNS
// concepts — those live on the Provider, which composes Client with an engine.
type Client struct {
	baseURL    string
	apiKey     string
	apiSecret  string
	httpClient *http.Client
	logger     *slog.Logger
}

// ClientOption configures a Client at construction time.
type ClientOption func(*Client)

// WithHTTPClient overrides the HTTP client (primarily for tests and to inject
// the framework-configured transport with unified TLS settings).
func WithHTTPClient(h *http.Client) ClientOption {
	return func(c *Client) { c.httpClient = h }
}

// WithLogger sets a structured logger.
func WithLogger(logger *slog.Logger) ClientOption {
	return func(c *Client) {
		if logger != nil {
			c.logger = logger
		}
	}
}

// NewClient constructs a Client. baseURL must be an absolute URL with scheme
// (http:// or https://). Trailing slashes are trimmed so path concatenation
// is unambiguous.
func NewClient(baseURL, apiKey, apiSecret string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		httpClient: httputil.DefaultClient(),
		logger:     slog.Default(),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// do POSTs a JSON request against the OPNsense API and returns the raw
// response body plus status code. Callers own status-code interpretation
// because "success" is endpoint-specific (200 with {"result":"failed"} is
// a common OPNsense failure mode). All OPNsense API mutations use POST;
// GET is never appropriate.
func (c *Client) do(ctx context.Context, path string, body []byte) ([]byte, int, error) {
	reqURL := c.baseURL + path

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, reader)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}

	req.SetBasicAuth(c.apiKey, c.apiSecret)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.logger.Debug("opnsense request",
		slog.String("path", path),
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := httputil.ReadBody(resp, 0)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response body: %w", err)
	}
	return respBody, resp.StatusCode, nil
}

// mapStatusError translates common HTTP status codes into the framework's
// provider-level sentinel errors so callers can use errors.Is checks.
func mapStatusError(status int, body []byte) error {
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return provider.ErrUnauthorized
	case status == http.StatusNotFound:
		return provider.ErrNotFound
	case status >= 500:
		return fmt.Errorf("%w: opnsense returned %d: %s",
			provider.ErrProviderUnavailable, status, truncate(string(body)))
	case status >= 400:
		return fmt.Errorf("opnsense returned %d: %s", status, truncate(string(body)))
	}
	return nil
}

// resultEnvelope is the common OPNsense response wrapper for mutation
// endpoints. "saved" and "deleted" indicate success; anything else is an
// error the caller should surface.
type resultEnvelope struct {
	Result string `json:"result"`
	UUID   string `json:"uuid"`
	// Validations is set when OPNsense rejects a request due to field-level
	// validation errors. Keys are dotted field paths (e.g. "host.hostname").
	Validations map[string]string `json:"validations"`
}

// parseMutationResponse handles the common shape of add/delete/set responses.
// Returns nil on success; a descriptive error otherwise. dnsweaver looks up
// UUIDs via the search endpoint, so the assigned UUID from add responses is
// deliberately discarded here.
func parseMutationResponse(body []byte) error {
	var env resultEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("decoding opnsense response: %w (body: %s)",
			err, truncate(string(body)))
	}
	switch strings.ToLower(env.Result) {
	case "saved", "deleted", "ok":
		return nil
	case "failed", "":
		if len(env.Validations) > 0 {
			return fmt.Errorf("opnsense rejected request: %s",
				formatValidations(env.Validations))
		}
		return fmt.Errorf("opnsense request failed: %s", truncate(string(body)))
	default:
		// Unknown result value — surface it verbatim.
		return fmt.Errorf("opnsense returned unexpected result %q", env.Result)
	}
}

// searchRequest is the standard OPNsense grid/search request body. rowCount
// = -1 asks for all rows so the caller doesn't need to paginate.
type searchRequest struct {
	Current      int    `json:"current"`
	RowCount     int    `json:"rowCount"`
	SearchPhrase string `json:"searchPhrase"`
}

func newSearchRequest() []byte {
	// Ignore error: struct only contains primitives, encoding cannot fail.
	body, _ := json.Marshal(searchRequest{Current: 1, RowCount: -1})
	return body
}

func formatValidations(v map[string]string) string {
	parts := make([]string, 0, len(v))
	for k, msg := range v {
		parts = append(parts, fmt.Sprintf("%s: %s", k, msg))
	}
	return strings.Join(parts, "; ")
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
