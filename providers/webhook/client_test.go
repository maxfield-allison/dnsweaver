package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_Ping(t *testing.T) {
	t.Run("successful ping", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/ping" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Method != http.MethodGet {
				t.Errorf("unexpected method: %s", r.Method)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "", WithRetries(0))
		err := client.Ping(context.Background())
		if err != nil {
			t.Errorf("Ping() unexpected error: %v", err)
		}
	})

	t.Run("ping failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "", WithRetries(0))
		err := client.Ping(context.Background())
		if err == nil {
			t.Error("Ping() expected error, got nil")
		}
	})

	t.Run("ping with auth header", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authValue := r.Header.Get("X-API-Key")
			if authValue != "secret123" {
				t.Errorf("auth header = %q, want %q", authValue, "secret123")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "X-API-Key", "secret123", WithRetries(0))
		err := client.Ping(context.Background())
		if err != nil {
			t.Errorf("Ping() unexpected error: %v", err)
		}
	})
}

func TestClient_List(t *testing.T) {
	t.Run("successful list", func(t *testing.T) {
		records := []RecordResponse{
			{Hostname: "app.example.com", Type: "A", Value: "10.0.0.1", TTL: 300},
			{Hostname: "www.example.com", Type: "CNAME", Value: "app.example.com", TTL: 300},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/list" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Method != http.MethodGet {
				t.Errorf("unexpected method: %s", r.Method)
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(records)
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "", WithRetries(0))
		result, err := client.List(context.Background())
		if err != nil {
			t.Fatalf("List() unexpected error: %v", err)
		}

		if len(result) != 2 {
			t.Errorf("List() returned %d records, want 2", len(result))
		}
		if result[0].Hostname != "app.example.com" {
			t.Errorf("result[0].Hostname = %q, want %q", result[0].Hostname, "app.example.com")
		}
	})

	t.Run("empty list", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]RecordResponse{})
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "", WithRetries(0))
		result, err := client.List(context.Background())
		if err != nil {
			t.Fatalf("List() unexpected error: %v", err)
		}

		if len(result) != 0 {
			t.Errorf("List() returned %d records, want 0", len(result))
		}
	})

	t.Run("list error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(ErrorResponse{
				Error:   "internal error",
				Message: "database connection failed",
			})
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "", WithRetries(0))
		_, err := client.List(context.Background())
		if err == nil {
			t.Error("List() expected error, got nil")
		}
	})
}

func TestClient_Create(t *testing.T) {
	t.Run("successful create", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/create" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Method != http.MethodPost {
				t.Errorf("unexpected method: %s", r.Method)
			}

			var req RecordRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("failed to decode request body: %v", err)
			}

			if req.Hostname != "app.example.com" {
				t.Errorf("hostname = %q, want %q", req.Hostname, "app.example.com")
			}
			if req.Type != "A" {
				t.Errorf("type = %q, want %q", req.Type, "A")
			}
			if req.Value != "10.0.0.1" {
				t.Errorf("value = %q, want %q", req.Value, "10.0.0.1")
			}
			if req.TTL != 300 {
				t.Errorf("ttl = %d, want %d", req.TTL, 300)
			}

			w.WriteHeader(http.StatusCreated)
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "", WithRetries(0))
		err := client.Create(context.Background(), "app.example.com", "A", "10.0.0.1", 300)
		if err != nil {
			t.Errorf("Create() unexpected error: %v", err)
		}
	})

	t.Run("create returns 200 OK", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "", WithRetries(0))
		err := client.Create(context.Background(), "app.example.com", "A", "10.0.0.1", 300)
		if err != nil {
			t.Errorf("Create() unexpected error: %v", err)
		}
	})

	t.Run("create returns 204 No Content", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "", WithRetries(0))
		err := client.Create(context.Background(), "app.example.com", "A", "10.0.0.1", 300)
		if err != nil {
			t.Errorf("Create() unexpected error: %v", err)
		}
	})

	t.Run("create error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ErrorResponse{
				Error: "invalid hostname",
			})
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "", WithRetries(0))
		err := client.Create(context.Background(), "", "A", "10.0.0.1", 300)
		if err == nil {
			t.Error("Create() expected error, got nil")
		}
	})
}

func TestClient_Delete(t *testing.T) {
	t.Run("successful delete", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/delete" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Method != http.MethodDelete {
				t.Errorf("unexpected method: %s", r.Method)
			}

			var req DeleteRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("failed to decode request body: %v", err)
			}

			if req.Hostname != "app.example.com" {
				t.Errorf("hostname = %q, want %q", req.Hostname, "app.example.com")
			}

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "", WithRetries(0))
		err := client.Delete(context.Background(), "app.example.com", "A")
		if err != nil {
			t.Errorf("Delete() unexpected error: %v", err)
		}
	})

	t.Run("delete returns 204", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "", WithRetries(0))
		err := client.Delete(context.Background(), "app.example.com", "A")
		if err != nil {
			t.Errorf("Delete() unexpected error: %v", err)
		}
	})

	t.Run("delete returns 404 (idempotent)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "", WithRetries(0))
		err := client.Delete(context.Background(), "nonexistent.example.com", "A")
		if err != nil {
			t.Errorf("Delete() unexpected error for 404: %v", err)
		}
	})

	t.Run("delete error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(ErrorResponse{
				Error: "permission denied",
			})
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "", WithRetries(0))
		err := client.Delete(context.Background(), "app.example.com", "A")
		if err == nil {
			t.Error("Delete() expected error, got nil")
		}
	})
}

func TestClient_Retry(t *testing.T) {
	t.Run("retries on 503", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			if attempts < 3 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "",
			WithRetries(3),
			WithRetryDelay(1*time.Millisecond),
		)
		err := client.Ping(context.Background())
		if err != nil {
			t.Errorf("Ping() unexpected error after retries: %v", err)
		}
		if attempts != 3 {
			t.Errorf("expected 3 attempts, got %d", attempts)
		}
	})

	t.Run("retries on 429 Too Many Requests", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			if attempts == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "",
			WithRetries(2),
			WithRetryDelay(1*time.Millisecond),
		)
		err := client.Ping(context.Background())
		if err != nil {
			t.Errorf("Ping() unexpected error after retry: %v", err)
		}
		if attempts != 2 {
			t.Errorf("expected 2 attempts, got %d", attempts)
		}
	})

	t.Run("does not retry on 400 Bad Request", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "",
			WithRetries(3),
			WithRetryDelay(1*time.Millisecond),
		)
		_ = client.Ping(context.Background())
		if attempts != 1 {
			t.Errorf("expected 1 attempt (no retry for 400), got %d", attempts)
		}
	})

	t.Run("max retries exceeded", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "",
			WithRetries(2),
			WithRetryDelay(1*time.Millisecond),
		)
		err := client.Ping(context.Background())
		if err == nil {
			t.Error("Ping() expected error after max retries")
		}
		if attempts != 3 { // initial + 2 retries
			t.Errorf("expected 3 attempts, got %d", attempts)
		}
	})
}

func TestClient_ContentType(t *testing.T) {
	t.Run("sets correct headers", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			contentType := r.Header.Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
			}

			accept := r.Header.Get("Accept")
			if accept != "application/json" {
				t.Errorf("Accept = %q, want %q", accept, "application/json")
			}

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewClient(server.URL, 5*time.Second, "", "", WithRetries(0))
		_ = client.Ping(context.Background())
	})
}

func TestClient_BaseURLNormalization(t *testing.T) {
	t.Run("strips trailing slash", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/ping" {
				t.Errorf("path = %q, want %q", r.URL.Path, "/ping")
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Add trailing slash to URL
		client := NewClient(server.URL+"/", 5*time.Second, "", "", WithRetries(0))
		err := client.Ping(context.Background())
		if err != nil {
			t.Errorf("Ping() unexpected error: %v", err)
		}
	})
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		statusCode int
		want       bool
	}{
		{http.StatusOK, false},
		{http.StatusCreated, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusNotFound, false},
		{http.StatusInternalServerError, false},
		{http.StatusTooManyRequests, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.statusCode), func(t *testing.T) {
			if got := isRetryable(tt.statusCode); got != tt.want {
				t.Errorf("isRetryable(%d) = %v, want %v", tt.statusCode, got, tt.want)
			}
		})
	}
}
