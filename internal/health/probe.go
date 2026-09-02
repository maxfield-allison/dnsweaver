package health

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// Probe checks the local liveness endpoint on port.
func Probe(ctx context.Context, port int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating health request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
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
