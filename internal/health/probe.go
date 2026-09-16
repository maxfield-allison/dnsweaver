package health

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
)

// Probe checks the local liveness endpoint on port.
func Probe(ctx context.Context, port int) error {
	return ProbePath(ctx, port, "/health")
}

// ProbePath checks a local management endpoint. It always targets loopback so
// container and exec probes remain compatible with the secure listener default.
func ProbePath(ctx context.Context, port int, path string) error {
	return ProbeAddressPath(ctx, "127.0.0.1", port, path)
}

// ProbeAddressPath checks a management endpoint through the configured local
// listener. Wildcard listeners are reached through loopback; no proxy is used.
func ProbeAddressPath(ctx context.Context, address string, port int, path string) error {
	if path != "/health" && path != "/ready" {
		return fmt.Errorf("unsupported management probe path %q", path)
	}
	ip := net.ParseIP(address)
	if ip == nil {
		return fmt.Errorf("invalid management listener address %q", address)
	}
	if ip.IsUnspecified() {
		if ip.To4() == nil {
			address = "::1"
		} else {
			address = "127.0.0.1"
		}
	}
	url := "http://" + net.JoinHostPort(address, fmt.Sprintf("%d", port)) + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating health request: %w", err)
	}

	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("probing %s: %w", url, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("probing %s: received %s", url, resp.Status)
	}

	return nil
}
