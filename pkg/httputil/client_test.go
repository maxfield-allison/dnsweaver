package httputil

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestNewClient_Defaults(t *testing.T) {
	client := NewClient(nil)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	if client.Timeout != DefaultTimeout {
		t.Errorf("expected timeout %v, got %v", DefaultTimeout, client.Timeout)
	}

	// Transport should be userAgentTransport wrapping default transport
	if client.Transport == nil {
		t.Fatal("expected non-nil transport")
	}

	uaTransport, ok := client.Transport.(*userAgentTransport)
	if !ok {
		t.Fatal("expected transport to be *userAgentTransport")
	}

	if uaTransport.userAgent != DefaultUserAgent {
		t.Errorf("expected userAgent %q, got %q", DefaultUserAgent, uaTransport.userAgent)
	}
}

func TestNewClient_CustomTimeout(t *testing.T) {
	cfg := &ClientConfig{
		Timeout: 60 * time.Second,
	}

	client := NewClient(cfg)

	if client.Timeout != 60*time.Second {
		t.Errorf("expected timeout 60s, got %v", client.Timeout)
	}
}

func TestNewClient_ZeroTimeout_UsesDefault(t *testing.T) {
	cfg := &ClientConfig{
		Timeout: 0,
	}

	client := NewClient(cfg)

	if client.Timeout != DefaultTimeout {
		t.Errorf("expected default timeout %v for zero value, got %v", DefaultTimeout, client.Timeout)
	}
}

func TestNewClient_NegativeTimeout_UsesDefault(t *testing.T) {
	cfg := &ClientConfig{
		Timeout: -1 * time.Second,
	}

	client := NewClient(cfg)

	if client.Timeout != DefaultTimeout {
		t.Errorf("expected default timeout %v for negative value, got %v", DefaultTimeout, client.Timeout)
	}
}

func TestNewClient_TLSSkipVerify(t *testing.T) {
	cfg := &ClientConfig{
		TLSSkipVerify: true,
	}

	client := NewClient(cfg)

	if client.Transport == nil {
		t.Fatal("expected non-nil transport when TLSSkipVerify is true")
	}

	uaTransport, ok := client.Transport.(*userAgentTransport)
	if !ok {
		t.Fatal("expected transport to be *userAgentTransport")
	}

	// The base transport should be *http.Transport with InsecureSkipVerify
	transport, ok := uaTransport.base.(*http.Transport)
	if !ok {
		t.Fatal("expected base transport to be *http.Transport")
	}

	if transport.TLSClientConfig == nil {
		t.Fatal("expected non-nil TLSClientConfig")
	}

	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true")
	}
}

func TestNewClient_TLSSkipVerifyFalse(t *testing.T) {
	cfg := &ClientConfig{
		TLSSkipVerify: false,
	}

	client := NewClient(cfg)

	// Transport should be userAgentTransport wrapping default transport
	if client.Transport == nil {
		t.Fatal("expected non-nil transport")
	}

	uaTransport, ok := client.Transport.(*userAgentTransport)
	if !ok {
		t.Fatal("expected transport to be *userAgentTransport")
	}

	// Base should be http.DefaultTransport (not a custom *http.Transport)
	if uaTransport.base != http.DefaultTransport {
		t.Error("expected base transport to be http.DefaultTransport when TLSSkipVerify is false")
	}
}

func TestNewClient_AllOptions(t *testing.T) {
	cfg := &ClientConfig{
		Timeout:       45 * time.Second,
		TLSSkipVerify: true,
		UserAgent:     "test-agent/1.0",
	}

	client := NewClient(cfg)

	if client.Timeout != 45*time.Second {
		t.Errorf("expected timeout 45s, got %v", client.Timeout)
	}

	uaTransport, ok := client.Transport.(*userAgentTransport)
	if !ok {
		t.Fatal("expected transport to be *userAgentTransport")
	}

	if uaTransport.userAgent != "test-agent/1.0" {
		t.Errorf("expected userAgent %q, got %q", "test-agent/1.0", uaTransport.userAgent)
	}

	transport, ok := uaTransport.base.(*http.Transport)
	if !ok {
		t.Fatal("expected base transport to be *http.Transport")
	}

	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true")
	}
}

func TestNewClient_CustomUserAgent(t *testing.T) {
	cfg := &ClientConfig{
		UserAgent: "custom-agent/2.0",
	}

	client := NewClient(cfg)

	uaTransport, ok := client.Transport.(*userAgentTransport)
	if !ok {
		t.Fatal("expected transport to be *userAgentTransport")
	}

	if uaTransport.userAgent != "custom-agent/2.0" {
		t.Errorf("expected userAgent %q, got %q", "custom-agent/2.0", uaTransport.userAgent)
	}
}

func TestNewClient_UserAgentAppliedToRequests(t *testing.T) {
	// Create a test server that echoes back the User-Agent header
	var receivedUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUserAgent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &ClientConfig{
		UserAgent: "test-dnsweaver/1.2.3",
	}
	client := NewClient(cfg)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if receivedUserAgent != "test-dnsweaver/1.2.3" {
		t.Errorf("expected User-Agent %q, got %q", "test-dnsweaver/1.2.3", receivedUserAgent)
	}
}

func TestNewClient_WithLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := &ClientConfig{
		Logger: logger,
	}

	client := NewClient(cfg)

	uaTransport, ok := client.Transport.(*userAgentTransport)
	if !ok {
		t.Fatal("expected transport to be *userAgentTransport")
	}

	if uaTransport.logger != logger {
		t.Error("expected logger to be set on transport")
	}
}

func TestDefaultClient(t *testing.T) {
	client := DefaultClient()

	if client == nil {
		t.Fatal("DefaultClient returned nil")
	}

	if client.Timeout != DefaultTimeout {
		t.Errorf("expected timeout %v, got %v", DefaultTimeout, client.Timeout)
	}

	// DefaultClient should have userAgentTransport with default user agent
	uaTransport, ok := client.Transport.(*userAgentTransport)
	if !ok {
		t.Fatal("expected transport to be *userAgentTransport")
	}

	if uaTransport.userAgent != DefaultUserAgent {
		t.Errorf("expected userAgent %q, got %q", DefaultUserAgent, uaTransport.userAgent)
	}
}

func TestNewClientWithTransport(t *testing.T) {
	customTransport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	client := NewClientWithTransport(15*time.Second, customTransport)

	if client.Timeout != 15*time.Second {
		t.Errorf("expected timeout 15s, got %v", client.Timeout)
	}

	if client.Transport != customTransport {
		t.Error("expected custom transport to be used")
	}
}

func TestNewClientWithTransport_ZeroTimeout(t *testing.T) {
	customTransport := &http.Transport{}

	client := NewClientWithTransport(0, customTransport)

	if client.Timeout != DefaultTimeout {
		t.Errorf("expected default timeout %v, got %v", DefaultTimeout, client.Timeout)
	}
}
