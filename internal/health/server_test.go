package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestServer_handleHealth(t *testing.T) {
	s := New(0)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
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

	if len(resp.Components) != 2 {
		t.Errorf("expected 2 components, got %d", len(resp.Components))
	}

	for _, c := range resp.Components {
		if !c.Healthy {
			t.Errorf("expected component %q to be healthy", c.Name)
		}
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

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
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

	// Check that one component is healthy and one is not
	healthyCount := 0
	unhealthyCount := 0
	for _, c := range resp.Components {
		if c.Healthy {
			healthyCount++
		} else {
			unhealthyCount++
			if c.Error != "connection refused" {
				t.Errorf("expected error 'connection refused', got %q", c.Error)
			}
		}
	}

	if healthyCount != 1 || unhealthyCount != 1 {
		t.Errorf("expected 1 healthy and 1 unhealthy, got %d healthy and %d unhealthy",
			healthyCount, unhealthyCount)
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

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
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

	if len(resp.Degraded) != 1 {
		t.Fatalf("expected 1 degraded component, got %d", len(resp.Degraded))
	}

	if resp.Degraded[0].Message != "some providers are initializing" {
		t.Errorf("expected degraded message, got %q", resp.Degraded[0].Message)
	}
}

func TestServer_handleReady_NoDegraded(t *testing.T) {
	s := New(0)

	// Register a degraded checker that returns not degraded
	s.RegisterDegradedChecker("pending-providers", func(ctx context.Context) (bool, string) {
		return false, "" // No pending providers
	})

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
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
