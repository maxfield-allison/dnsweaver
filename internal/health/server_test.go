package health

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestServer_handleHealth(t *testing.T) {
	s := New(0)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	s.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("expected status 'healthy', got %q", resp.Status)
	}
}

func TestServer_handleReady_NoCheckers(t *testing.T) {
	s := New(0)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	s.handleReady(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "ready" {
		t.Errorf("expected status 'ready', got %q", resp.Status)
	}
}

func TestServer_handleReady_AllHealthy(t *testing.T) {
	s := New(0)

	s.RegisterChecker("provider:test1", func(ctx context.Context) error {
		return nil
	})
	s.RegisterChecker("provider:test2", func(ctx context.Context) error {
		return nil
	})
	s.refreshReadiness(t.Context())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	s.handleReady(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "ready" {
		t.Errorf("expected status 'ready', got %q", resp.Status)
	}

	if len(resp.Components) != 0 {
		t.Errorf("readiness response exposed components: %+v", resp.Components)
	}
}

func TestServer_handleReady_SomeUnhealthy(t *testing.T) {
	s := New(0)

	s.RegisterChecker("provider:healthy", func(ctx context.Context) error {
		return nil
	})
	s.RegisterChecker("provider:unhealthy", func(ctx context.Context) error {
		return errors.New("connection refused")
	})
	s.refreshReadiness(t.Context())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	s.handleReady(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "not_ready" {
		t.Errorf("expected status 'not_ready', got %q", resp.Status)
	}

	if len(resp.Components) != 0 {
		t.Errorf("readiness response exposed components or upstream errors: %+v", resp.Components)
	}
}

func TestServer_handleReady_Timeout(t *testing.T) {
	s := New(0, WithTimeout(50*time.Millisecond))

	s.RegisterChecker("provider:slow", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return nil
		}
	})
	s.refreshReadiness(t.Context())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	s.handleReady(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "not_ready" {
		t.Errorf("expected status 'not_ready', got %q", resp.Status)
	}
}

func TestServer_RegisterChecker(t *testing.T) {
	s := New(0)

	s.RegisterChecker("test", func(ctx context.Context) error { return nil })

	if len(s.checkers) != 1 {
		t.Errorf("expected 1 checker, got %d", len(s.checkers))
	}

	if _, ok := s.checkers["test"]; !ok {
		t.Error("expected checker 'test' to be registered")
	}
}

func TestServer_handleReady_Degraded(t *testing.T) {
	s := New(0)

	// Register a degraded checker that reports degraded status
	s.RegisterDegradedChecker("pending-providers", func(ctx context.Context) (bool, string) {
		return true, "some providers are initializing"
	})
	s.refreshReadiness(t.Context())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	s.handleReady(w, req)

	// Degraded should still return 200 OK (service is usable)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != StatusDegraded {
		t.Errorf("expected status 'degraded', got %q", resp.Status)
	}

	if len(resp.Degraded) != 0 {
		t.Fatalf("readiness response exposed degraded details: %+v", resp.Degraded)
	}
}

func TestServer_handleReady_NoDegraded(t *testing.T) {
	s := New(0)

	// Register a degraded checker that returns not degraded
	s.RegisterDegradedChecker("pending-providers", func(ctx context.Context) (bool, string) {
		return false, "" // No pending providers
	})
	s.refreshReadiness(t.Context())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	s.handleReady(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "ready" {
		t.Errorf("expected status 'ready', got %q", resp.Status)
	}
}

func TestServer_handleReady_DegradedWithUnhealthyChecker(t *testing.T) {
	s := New(0)

	// Register a failing health checker
	s.RegisterChecker("failing-checker", func(ctx context.Context) error {
		return errors.New("checker failed")
	})

	// Register a degraded checker
	s.RegisterDegradedChecker("pending-providers", func(ctx context.Context) (bool, string) {
		return true, "providers pending"
	})
	s.refreshReadiness(t.Context())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	s.handleReady(w, req)

	// Unhealthy takes precedence over degraded - should return 503
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 (unhealthy), got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "not_ready" {
		t.Errorf("expected status 'not_ready', got %q", resp.Status)
	}
}

func TestServer_RegisterDegradedChecker(t *testing.T) {
	s := New(0)

	s.RegisterDegradedChecker("test-degraded", func(ctx context.Context) (bool, string) {
		return false, ""
	})

	if len(s.degradedCheckers) != 1 {
		t.Errorf("expected 1 degraded checker, got %d", len(s.degradedCheckers))
	}

	if _, ok := s.degradedCheckers["test-degraded"]; !ok {
		t.Error("expected degraded checker 'test-degraded' to be registered")
	}
}

func TestServer_handleReady_ShuttingDown(t *testing.T) {
	s := New(0)

	// Register a healthy checker to prove we skip it during shutdown
	s.RegisterChecker("test", func(ctx context.Context) error {
		return nil
	})
	s.refreshReadiness(t.Context())

	// Before shutdown — should be ready
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", nil)
	rr := httptest.NewRecorder()
	s.handleReady(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("before shutdown: status = %d, want %d", rr.Code, http.StatusOK)
	}

	// Mark as shutting down
	s.SetShuttingDown()

	// After shutdown — should be 503
	req = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", nil)
	rr = httptest.NewRecorder()
	s.handleReady(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("during shutdown: status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}

	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != "shutting_down" {
		t.Errorf("status = %q, want %q", resp.Status, "shutting_down")
	}
}

func TestServer_handleReady_DoesNotInvokeBackendChecker(t *testing.T) {
	s := New(0)
	calls := 0
	s.RegisterChecker("provider:fixture", func(context.Context) error {
		calls++
		return nil
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", nil)
	rr := httptest.NewRecorder()
	s.handleReady(rr, req)
	if calls != 0 {
		t.Fatalf("HTTP readiness invoked backend checker %d times", calls)
	}
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("uncached readiness status = %d, want 503", rr.Code)
	}

	s.refreshReadiness(t.Context())
	rr = httptest.NewRecorder()
	s.handleReady(rr, req)
	if calls != 1 {
		t.Fatalf("cached HTTP readiness changed checker count to %d, want 1", calls)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("refreshed readiness status = %d, want 200", rr.Code)
	}
}

func TestServer_refreshReadiness_DoesNotPublishStaleSnapshot(t *testing.T) {
	s := New(0)
	s.RegisterChecker("provider:first", func(context.Context) error {
		s.RegisterChecker("provider:late", func(context.Context) error { return nil })
		return nil
	})

	s.refreshReadiness(t.Context())
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", nil)
	rr := httptest.NewRecorder()
	s.handleReady(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("stale readiness status = %d, want 503 until late checker runs", rr.Code)
	}
}

func TestServer_DefaultListenerIsLoopback(t *testing.T) {
	s := New(0)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})

	host, portString, err := net.SplitHostPort(s.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		t.Fatalf("default listener address = %q, want loopback", host)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatal(err)
	}
	if err := Probe(t.Context(), port); err != nil {
		t.Fatalf("local health probe failed: %v", err)
	}

	if externalIP := localNonLoopbackIP(t); externalIP != nil {
		dialer := net.Dialer{Timeout: 250 * time.Millisecond}
		conn, err := dialer.DialContext(t.Context(), "tcp", net.JoinHostPort(externalIP.String(), portString))
		if err == nil {
			_ = conn.Close()
			t.Fatalf("default loopback listener accepted non-loopback address %s", externalIP)
		}
	}
}

func TestServer_ExplicitNetworkListener(t *testing.T) {
	externalIP := localNonLoopbackIP(t)
	if externalIP == nil {
		t.Skip("no non-loopback interface available")
	}
	s := New(0, WithAddress("0.0.0.0"))
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})

	host, port, err := net.SplitHostPort(s.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsUnspecified() {
		t.Fatalf("explicit network listener address = %q, want wildcard", host)
	}

	client := &http.Client{
		Timeout:   time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+net.JoinHostPort(externalIP.String(), port)+"/health", nil)
	if err != nil {
		t.Fatalf("create explicit network health request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("explicit network health request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("explicit network health status = %d, want 200", resp.StatusCode)
	}
}

func TestServer_StartReturnsBindFailure(t *testing.T) {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer listener.Close()

	port := serverPort(t, listener.Addr())
	s := New(port)
	if err := s.Start(); err == nil {
		t.Fatal("Start() error = nil, want occupied-port bind failure")
	}
}

func localNonLoopbackIP(t *testing.T) net.IP {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatalf("InterfaceAddrs() error = %v", err)
	}
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err == nil && ip.IsGlobalUnicast() && !ip.IsLoopback() {
			return ip
		}
	}
	return nil
}
