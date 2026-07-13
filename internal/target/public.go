package target

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// DefaultPublicEndpoints are the IP echo services queried, in order, until one
// returns a valid address. They each return the caller's public IP as a bare
// string.
var DefaultPublicEndpoints = []string{
	"https://checkip.amazonaws.com",
	"https://api.ipify.org",
	"https://ipinfo.io/ip",
	"https://ifconfig.me/ip",
}

// publicHTTPTimeout bounds each echo-endpoint request.
const publicHTTPTimeout = 10 * time.Second

// PublicResolver discovers the host's public IP by querying HTTP echo endpoints
// in order until one returns an address of the requested family.
type PublicResolver struct {
	family    Family
	endpoints []string
	client    *http.Client
}

// NewPublicResolver creates a PublicResolver for the given family. If endpoints
// is nil, DefaultPublicEndpoints is used. The resolver pins the HTTP request to
// the requested family's network (tcp4/tcp6) so the echo endpoint observes and
// returns an address of that family.
func NewPublicResolver(family Family, endpoints []string) *PublicResolver {
	if len(endpoints) == 0 {
		endpoints = DefaultPublicEndpoints
	}

	network := "tcp4"
	if family == FamilyIPv6 {
		network = "tcp6"
	}
	dialer := &net.Dialer{Timeout: publicHTTPTimeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
	}

	return &PublicResolver{
		family:    family,
		endpoints: endpoints,
		client:    &http.Client{Timeout: publicHTTPTimeout, Transport: transport},
	}
}

// Describe implements Resolver.
func (r *PublicResolver) Describe() string {
	return "public " + r.family.String()
}

// Resolve implements Resolver. It tries each endpoint in order and returns the
// first response that parses as an IP of the configured family. It returns an
// error only if every endpoint fails.
func (r *PublicResolver) Resolve(ctx context.Context) (string, error) {
	var errs []string
	for _, ep := range r.endpoints {
		ip, err := r.query(ctx, ep)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", ep, err))
			continue
		}
		return ip, nil
	}
	return "", fmt.Errorf("all public IP endpoints failed: %s", strings.Join(errs, "; "))
}

func (r *PublicResolver) query(ctx context.Context, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "dnsweaver")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	// Bound the read: echo endpoints return a short IP string.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return "", err
	}

	got := strings.TrimSpace(string(body))
	ip := net.ParseIP(got)
	if ip == nil {
		return "", fmt.Errorf("response %q is not an IP", got)
	}
	if !r.family.matches(ip) {
		return "", fmt.Errorf("response %q is not %s", got, r.family)
	}
	return ip.String(), nil
}
