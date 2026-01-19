// Package pihole implements the DNSWeaver provider interface for Pi-hole DNS.
package pihole

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// APIVersion represents the detected Pi-hole API version.
type APIVersion int

const (
	// APIVersionUnknown indicates version detection failed or not yet performed.
	APIVersionUnknown APIVersion = iota
	// APIVersionV5 indicates Pi-hole v5.x with legacy API.
	APIVersionV5
	// APIVersionV6 indicates Pi-hole v6.x with new REST API.
	APIVersionV6
)

// String returns a human-readable version string.
func (v APIVersion) String() string {
	switch v {
	case APIVersionV5:
		return "v5"
	case APIVersionV6:
		return "v6"
	default:
		return "unknown"
	}
}

// VersionDetector probes a Pi-hole instance to determine its API version.
type VersionDetector struct {
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewVersionDetector creates a new version detector.
func NewVersionDetector(baseURL string, httpClient *http.Client, logger *slog.Logger) *VersionDetector {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Normalize URL - remove trailing slash
	baseURL = strings.TrimSuffix(baseURL, "/")

	return &VersionDetector{
		baseURL:    baseURL,
		httpClient: httpClient,
		logger:     logger,
	}
}

// Detect probes the Pi-hole instance and returns the detected API version.
// It tries v6 endpoints first (faster to detect if available), then falls back to v5.
func (d *VersionDetector) Detect(ctx context.Context) (APIVersion, string, error) {
	d.logger.Debug("starting Pi-hole version detection",
		slog.String("url", d.baseURL))

	// Try v6 first: GET /api/info/version (no auth required for version info)
	version, err := d.tryV6(ctx)
	if err == nil {
		d.logger.Info("detected Pi-hole v6 API",
			slog.String("version", version),
			slog.String("url", d.baseURL))
		return APIVersionV6, version, nil
	}
	d.logger.Debug("v6 probe failed, trying v5",
		slog.String("error", err.Error()))

	// Try v5: GET /admin/api.php?version
	version, err = d.tryV5(ctx)
	if err == nil {
		d.logger.Info("detected Pi-hole v5 API",
			slog.String("version", version),
			slog.String("url", d.baseURL))
		return APIVersionV5, version, nil
	}
	d.logger.Debug("v5 probe failed",
		slog.String("error", err.Error()))

	return APIVersionUnknown, "", fmt.Errorf("unable to detect Pi-hole version: both v5 and v6 probes failed")
}

// tryV6 probes for Pi-hole v6 API.
// The v6 API exposes /api/info/login without authentication, which returns
// basic status info that we can use to detect v6. Note: /api/info does NOT
// exist in Pi-hole v6 (it returns 404), so we must use /api/info/login.
func (d *VersionDetector) tryV6(ctx context.Context) (string, error) {
	// Pi-hole v6 exposes /api/info/login without auth
	// Response: {"dns":true,"https_port":443,"took":0.001...}
	url := d.baseURL + "/api/info/login"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	// V6 response structure from /api/info/login
	// This endpoint doesn't return version info, but its presence and
	// response structure confirms v6. We return "v6" as the version string.
	var result struct {
		DNS       bool    `json:"dns"`
		HTTPSPort int     `json:"https_port"`
		Took      float64 `json:"took"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	// If we got a valid response with the expected fields, this is v6
	// The HTTPSPort being present (non-zero) confirms the v6 API structure
	if result.HTTPSPort == 0 {
		return "", fmt.Errorf("unexpected response structure")
	}

	return "v6", nil
}

// tryV5 probes for Pi-hole v5 API.
// The v5 API exposes summary at /admin/api.php?version without authentication.
func (d *VersionDetector) tryV5(ctx context.Context) (string, error) {
	url := d.baseURL + "/admin/api.php?version"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	// V5 version response: just a string or {"version": true}
	// Pi-hole v5 returns {"version": X} where X is a number
	var result struct {
		Version int `json:"version"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		// Some v5 versions return just the version number as text
		version := strings.TrimSpace(string(body))
		if version != "" && version != "[]" {
			return version, nil
		}
		return "", fmt.Errorf("parsing response: %w", err)
	}

	if result.Version == 0 {
		return "", fmt.Errorf("no version in response")
	}

	return fmt.Sprintf("%d", result.Version), nil
}
