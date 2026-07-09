package pfsense

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// client is a low-level HTTP client for the pfSense REST API, as provided by
// the community pfSense-pkg-RESTAPI package (https://pfrest.org), REST API v2.
// It knows about authentication, transport, and the pfrest response envelope,
// but not about DNS concepts — those live on the Provider, which composes this
// client with a resolver.
//
// Auth is a single API key sent in the X-API-Key header. Mutations use the
// resource-appropriate HTTP verb (POST create, PATCH update, DELETE remove),
// and every response is wrapped in pfrest's {code,status,response_id,message,
// data} envelope.
type client struct {
	baseURL    string
	apiKey     string
	res        resolver
	httpClient *http.Client
	logger     *slog.Logger
}

// clientOption configures a client at construction time.
type clientOption func(*client)

// withHTTPClient overrides the HTTP client (primarily for tests and to inject
// the framework-configured transport with unified TLS settings).
func withHTTPClient(h *http.Client) clientOption {
	return func(c *client) {
		if h != nil {
			c.httpClient = h
		}
	}
}

// withLogger sets a structured logger.
func withLogger(logger *slog.Logger) clientOption {
	return func(c *client) {
		if logger != nil {
			c.logger = logger
		}
	}
}

// newClient constructs a client for the given resolver. baseURL must be an
// absolute URL with scheme (http:// or https://); trailing slashes are trimmed
// so path concatenation is unambiguous.
func newClient(baseURL, apiKey string, res resolver, opts ...clientOption) *client {
	c := &client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		res:        res,
		httpClient: httputil.DefaultClient(),
		logger:     slog.Default(),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// envelope is the common pfrest v2 response wrapper. The HTTP status code
// mirrors Code, so transport-level status handling stays authoritative and the
// envelope is used mainly for surfacing Message on errors and reading Data on
// success.
type envelope struct {
	Code       int             `json:"code"`
	Status     string          `json:"status"`
	ResponseID string          `json:"response_id"`
	Message    string          `json:"message"`
	Data       json.RawMessage `json:"data"`
}

// wireOverride is the pfrest wire shape of a DNS host override. The ip field is
// an array on the DNS Resolver (Unbound) and a scalar on the DNS Forwarder
// (Dnsmasq), so it is decoded via RawMessage and normalized per resolver.
type wireOverride struct {
	ID     json.Number     `json:"id,omitempty"`
	Host   string          `json:"host"`
	Domain string          `json:"domain"`
	IP     json.RawMessage `json:"ip"`
	Descr  string          `json:"descr"`
}

// do issues a request against the pfrest API and returns the decoded envelope
// Data on success. Non-2xx responses are mapped to the framework's provider
// sentinels so callers can use errors.Is.
func (c *client) do(ctx context.Context, method, path, rawQuery string, body any) (json.RawMessage, error) {
	reqURL := c.baseURL + path
	if rawQuery != "" {
		reqURL += "?" + rawQuery
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encoding request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.logger.Debug("pfsense request",
		slog.String("method", method),
		slog.String("path", path),
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", provider.ErrProviderUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := httputil.ReadBody(resp, 0)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	var env envelope
	// A decode failure is tolerated for error statuses (pfSense can return a
	// bare HTML error page); it is only fatal when the status says success.
	decodeErr := json.Unmarshal(respBody, &env)

	if statusErr := mapStatusError(resp.StatusCode, env.Message, respBody); statusErr != nil {
		return nil, statusErr
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("decoding pfsense response: %w (body: %s)", decodeErr, truncate(string(respBody)))
	}
	return env.Data, nil
}

// list returns every host override for the configured resolver, including
// operator-managed ones (the provider filters by ownership).
func (c *client) list(ctx context.Context) ([]hostOverride, error) {
	data, err := c.do(ctx, http.MethodGet, c.res.plural, "", nil)
	if err != nil {
		return nil, err
	}
	var rows []wireOverride
	if len(data) > 0 {
		if err := json.Unmarshal(data, &rows); err != nil {
			return nil, fmt.Errorf("decoding pfsense host override list: %w", err)
		}
	}
	out := make([]hostOverride, 0, len(rows))
	for _, row := range rows {
		out = append(out, hostOverride{
			ID:     strings.TrimSpace(row.ID.String()),
			Host:   row.Host,
			Domain: row.Domain,
			IPs:    decodeIPs(row.IP),
			Descr:  row.Descr,
		})
	}
	return out, nil
}

// create adds a new host override.
func (c *client) create(ctx context.Context, ho hostOverride) error {
	payload, err := c.encodePayload(ho, false)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPost, c.res.single, "", payload)
	return err
}

// update replaces an existing host override identified by ho.ID.
func (c *client) update(ctx context.Context, ho hostOverride) error {
	if ho.ID == "" {
		return fmt.Errorf("update requires a host override id")
	}
	payload, err := c.encodePayload(ho, true)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPatch, c.res.single, "", payload)
	return err
}

// remove deletes a host override by its pfSense object id.
func (c *client) remove(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("remove requires a host override id")
	}
	q := url.Values{}
	q.Set("id", id)
	_, err := c.do(ctx, http.MethodDelete, c.res.single, q.Encode(), nil)
	return err
}

// apply reloads the resolver so pending changes take effect.
func (c *client) apply(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodPost, c.res.apply, "", map[string]any{})
	return err
}

// encodePayload builds the pfrest request body for a host override. The ip
// field is serialized as an array for multi-IP resolvers (Unbound) and as a
// scalar otherwise (Dnsmasq). When withID is set, the object id is included so
// PATCH targets the right row.
func (c *client) encodePayload(ho hostOverride, withID bool) (map[string]any, error) {
	if len(ho.IPs) == 0 {
		return nil, fmt.Errorf("host override %s.%s has no IP targets", ho.Host, ho.Domain)
	}
	payload := map[string]any{
		"host":   ho.Host,
		"domain": ho.Domain,
		"descr":  ho.Descr,
	}
	if c.res.multiIP {
		payload["ip"] = ho.IPs
	} else {
		if len(ho.IPs) > 1 {
			return nil, fmt.Errorf("the %s engine (DNS Forwarder) stores one IP per host override, got %d for %s.%s; use the unbound engine for dual-stack",
				c.res.name, len(ho.IPs), ho.Host, ho.Domain)
		}
		payload["ip"] = ho.IPs[0]
	}
	if withID {
		payload["id"] = ho.ID
	}
	return payload, nil
}

// decodeIPs normalizes the pfrest ip field, which is an array on the DNS
// Resolver and a scalar string on the DNS Forwarder.
func decodeIPs(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil && single != "" {
		return []string{single}
	}
	return nil
}

// mapStatusError translates common HTTP status codes into the framework's
// provider-level sentinel errors. message is the pfrest envelope message when
// available; body is the raw response for fallback context.
func mapStatusError(status int, message string, body []byte) error {
	detail := strings.TrimSpace(message)
	if detail == "" {
		detail = truncate(string(body))
	}
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return provider.ErrUnauthorized
	case status == http.StatusNotFound:
		return provider.ErrNotFound
	case status >= 500:
		return fmt.Errorf("%w: pfsense returned %d: %s", provider.ErrProviderUnavailable, status, detail)
	default:
		return fmt.Errorf("pfsense returned %d: %s", status, detail)
	}
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
