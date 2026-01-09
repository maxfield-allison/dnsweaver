package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// successResponse creates a successful Cloudflare API response.
func successResponse(result interface{}) map[string]interface{} {
	return map[string]interface{}{
		"success":  true,
		"errors":   []interface{}{},
		"messages": []interface{}{},
		"result":   result,
	}
}

// errorResponse creates an error Cloudflare API response.
func errorResponse(code int, message string) map[string]interface{} {
	return map[string]interface{}{
		"success": false,
		"errors": []map[string]interface{}{
			{"code": code, "message": message},
		},
		"messages": []interface{}{},
		"result":   nil,
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient("test-token")

	if client.apiEndpoint != DefaultAPIEndpoint {
		t.Errorf("expected apiEndpoint %s, got %s", DefaultAPIEndpoint, client.apiEndpoint)
	}
	if client.token != "test-token" {
		t.Errorf("expected token test-token, got %s", client.token)
	}
	if client.httpClient == nil {
		t.Error("expected httpClient to be initialized")
	}
	if client.logger == nil {
		t.Error("expected logger to be initialized")
	}
}

func TestClient_WithAPIEndpoint(t *testing.T) {
	client := NewClient("test-token", WithAPIEndpoint("http://custom-endpoint"))

	if client.apiEndpoint != "http://custom-endpoint" {
		t.Errorf("expected apiEndpoint http://custom-endpoint, got %s", client.apiEndpoint)
	}
}

func TestClient_Ping_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/tokens/verify" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-token" {
			t.Errorf("unexpected Authorization header: %s", authHeader)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(successResponse(map[string]interface{}{
			"id":     "token-id",
			"status": "active",
		}))
	}))
	defer server.Close()

	client := NewClient("test-token", WithAPIEndpoint(server.URL))
	err := client.Ping(context.Background())

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClient_Ping_InvalidToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorResponse(1000, "Invalid API token"))
	}))
	defer server.Close()

	client := NewClient("bad-token", WithAPIEndpoint(server.URL))
	err := client.Ping(context.Background())

	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestClient_GetZoneID_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zones" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		query := r.URL.Query()
		zoneName := query.Get("name")

		w.Header().Set("Content-Type", "application/json")

		// Return zone if querying for example.com
		if zoneName == "example.com" {
			_ = json.NewEncoder(w).Encode(successResponse([]map[string]interface{}{
				{"id": "zone-123", "name": "example.com", "status": "active"},
			}))
		} else {
			_ = json.NewEncoder(w).Encode(successResponse([]map[string]interface{}{}))
		}
	}))
	defer server.Close()

	client := NewClient("test-token", WithAPIEndpoint(server.URL))
	zoneID, err := client.GetZoneID(context.Background(), "app.example.com")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if zoneID != "zone-123" {
		t.Errorf("expected zone ID zone-123, got %s", zoneID)
	}
}

func TestClient_GetZoneID_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(successResponse([]map[string]interface{}{}))
	}))
	defer server.Close()

	client := NewClient("test-token", WithAPIEndpoint(server.URL))
	_, err := client.GetZoneID(context.Background(), "nonexistent.example.com")

	if err == nil {
		t.Error("expected error for missing zone, got nil")
	}
}

func TestClient_ListRecords_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zones/zone-123/dns_records" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		query := r.URL.Query()
		recordType := query.Get("type")

		w.Header().Set("Content-Type", "application/json")

		if recordType == "A" {
			_ = json.NewEncoder(w).Encode(successResponse([]map[string]interface{}{
				{"id": "rec-1", "type": "A", "name": "app.example.com", "content": "10.0.0.1", "ttl": 300, "proxied": false},
				{"id": "rec-2", "type": "A", "name": "api.example.com", "content": "10.0.0.2", "ttl": 300, "proxied": true},
			}))
		} else {
			_ = json.NewEncoder(w).Encode(successResponse([]map[string]interface{}{}))
		}
	}))
	defer server.Close()

	client := NewClient("test-token", WithAPIEndpoint(server.URL))
	records, err := client.ListRecords(context.Background(), "zone-123", "A")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 records, got %d", len(records))
	}
	if records[0].Name != "app.example.com" {
		t.Errorf("expected first record name app.example.com, got %s", records[0].Name)
	}
}

func TestClient_CreateRecord_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if r.URL.Path != "/zones/zone-123/dns_records" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}

		if body["type"] != "A" {
			t.Errorf("expected type A, got %v", body["type"])
		}
		if body["name"] != "test.example.com" {
			t.Errorf("expected name test.example.com, got %v", body["name"])
		}
		if body["content"] != "10.0.0.1" {
			t.Errorf("expected content 10.0.0.1, got %v", body["content"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(successResponse(map[string]interface{}{
			"id":      "rec-new",
			"type":    "A",
			"name":    "test.example.com",
			"content": "10.0.0.1",
			"ttl":     300,
		}))
	}))
	defer server.Close()

	client := NewClient("test-token", WithAPIEndpoint(server.URL))
	err := client.CreateRecord(context.Background(), "zone-123", "A", "test.example.com", "10.0.0.1", 300, false)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClient_CreateRecord_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorResponse(1004, "DNS Validation Error"))
	}))
	defer server.Close()

	client := NewClient("test-token", WithAPIEndpoint(server.URL))
	err := client.CreateRecord(context.Background(), "zone-123", "A", "invalid", "not-an-ip", 300, false)

	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestClient_DeleteRecord_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE method, got %s", r.Method)
		}
		if r.URL.Path != "/zones/zone-123/dns_records/rec-1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(successResponse(map[string]interface{}{
			"id": "rec-1",
		}))
	}))
	defer server.Close()

	client := NewClient("test-token", WithAPIEndpoint(server.URL))
	err := client.DeleteRecord(context.Background(), "zone-123", "rec-1")

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClient_FindRecord_Found(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("name") != "app.example.com" || query.Get("type") != "A" {
			t.Errorf("unexpected query params: %v", query)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(successResponse([]map[string]interface{}{
			{"id": "rec-1", "type": "A", "name": "app.example.com", "content": "10.0.0.1", "ttl": 300},
		}))
	}))
	defer server.Close()

	client := NewClient("test-token", WithAPIEndpoint(server.URL))
	record, err := client.FindRecord(context.Background(), "zone-123", "A", "app.example.com")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if record == nil {
		t.Fatal("expected record, got nil")
	}
	if record.ID != "rec-1" {
		t.Errorf("expected record ID rec-1, got %s", record.ID)
	}
}

func TestClient_FindRecord_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(successResponse([]map[string]interface{}{}))
	}))
	defer server.Close()

	client := NewClient("test-token", WithAPIEndpoint(server.URL))
	record, err := client.FindRecord(context.Background(), "zone-123", "A", "nonexistent.example.com")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if record != nil {
		t.Errorf("expected nil record, got %+v", record)
	}
}

func TestClient_RateLimiting(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(errorResponse(10000, "Rate limit exceeded"))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(successResponse(map[string]interface{}{}))
	}))
	defer server.Close()

	client := NewClient("test-token", WithAPIEndpoint(server.URL))
	err := client.Ping(context.Background())

	// First call should fail with rate limit error
	if err == nil {
		t.Error("expected rate limit error, got nil")
	}
}
