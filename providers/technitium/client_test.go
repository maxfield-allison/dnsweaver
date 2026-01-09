package technitium

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockZoneInfo creates a zone info object matching the Technitium API response format.
func mockZoneInfo(name string) map[string]interface{} {
	return map[string]interface{}{
		"name":     name,
		"type":     "Primary",
		"disabled": false,
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient("http://localhost:5380", "test-token")

	if client.baseURL != "http://localhost:5380" {
		t.Errorf("expected baseURL http://localhost:5380, got %s", client.baseURL)
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

func TestClient_Ping_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/session/get" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		query := r.URL.Query()
		if query.Get("token") != "test-token" {
			t.Errorf("unexpected token: %s", query.Get("token"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"response": map[string]interface{}{
				"username": "admin",
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.Ping(context.Background())

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClient_Ping_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "error",
			"errorMessage": "Invalid token",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "bad-token")
	err := client.Ping(context.Background())

	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestClient_AddARecord_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/zones/records/add" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		query := r.URL.Query()
		if query.Get("token") != "test-token" {
			t.Errorf("unexpected token: %s", query.Get("token"))
		}
		if query.Get("zone") != "example.com" {
			t.Errorf("unexpected zone: %s", query.Get("zone"))
		}
		if query.Get("domain") != "test.example.com" {
			t.Errorf("unexpected domain: %s", query.Get("domain"))
		}
		if query.Get("type") != "A" {
			t.Errorf("unexpected type: %s", query.Get("type"))
		}
		if query.Get("ipAddress") != "10.0.0.1" {
			t.Errorf("unexpected ipAddress: %s", query.Get("ipAddress"))
		}
		if query.Get("ttl") != "300" {
			t.Errorf("unexpected ttl: %s", query.Get("ttl"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"response": map[string]interface{}{
				"zone": mockZoneInfo("example.com"),
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.AddARecord(context.Background(), "example.com", "test.example.com", "10.0.0.1", 300)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClient_AddARecord_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "error",
			"errorMessage": "Zone does not exist",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.AddARecord(context.Background(), "nonexistent.com", "test.nonexistent.com", "10.0.0.1", 300)

	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestClient_AddCNAMERecord_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/zones/records/add" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		query := r.URL.Query()
		if query.Get("type") != "CNAME" {
			t.Errorf("unexpected type: %s", query.Get("type"))
		}
		if query.Get("cname") != "target.example.com" {
			t.Errorf("unexpected cname: %s", query.Get("cname"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"response": map[string]interface{}{
				"zone": mockZoneInfo("example.com"),
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.AddCNAMERecord(context.Background(), "example.com", "alias.example.com", "target.example.com", 300)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClient_DeleteARecord_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/zones/records/delete" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		query := r.URL.Query()
		if query.Get("type") != "A" {
			t.Errorf("unexpected type: %s", query.Get("type"))
		}
		if query.Get("ipAddress") != "10.0.0.1" {
			t.Errorf("unexpected ipAddress: %s", query.Get("ipAddress"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.DeleteARecord(context.Background(), "example.com", "test.example.com", "10.0.0.1")

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClient_DeleteCNAMERecord_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/zones/records/delete" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		query := r.URL.Query()
		if query.Get("type") != "CNAME" {
			t.Errorf("unexpected type: %s", query.Get("type"))
		}
		if query.Get("cname") != "target.example.com" {
			t.Errorf("unexpected cname: %s", query.Get("cname"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.DeleteCNAMERecord(context.Background(), "example.com", "alias.example.com", "target.example.com")

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClient_GetRecords_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/zones/records/get" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"response": map[string]interface{}{
				"zone": mockZoneInfo("example.com"),
				"name": "test.example.com",
				"records": []map[string]interface{}{
					{
						"name":     "test.example.com",
						"type":     "A",
						"ttl":      300,
						"disabled": false,
						"rData": map[string]interface{}{
							"ipAddress": "10.0.0.1",
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	records, err := client.GetRecords(context.Background(), "example.com", "test.example.com")

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record, got %d", len(records))
	}
	if records[0].RData.IPAddress != "10.0.0.1" {
		t.Errorf("unexpected IP: %s", records[0].RData.IPAddress)
	}
}

func TestClient_ListZoneRecords_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("listZone") != "true" {
			t.Errorf("expected listZone=true, got %s", query.Get("listZone"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"response": map[string]interface{}{
				"zone": mockZoneInfo("example.com"),
				"records": []map[string]interface{}{
					{
						"name":     "app.example.com",
						"type":     "A",
						"ttl":      300,
						"disabled": false,
						"rData": map[string]interface{}{
							"ipAddress": "10.0.0.1",
						},
					},
					{
						"name":     "www.example.com",
						"type":     "CNAME",
						"ttl":      300,
						"disabled": false,
						"rData": map[string]interface{}{
							"cname": "app.example.com",
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	records, err := client.ListZoneRecords(context.Background(), "example.com")

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 records, got %d", len(records))
	}
}

func TestClient_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.Ping(context.Background())

	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestClient_NetworkError(t *testing.T) {
	client := NewClient("http://localhost:99999", "test-token")
	err := client.Ping(context.Background())

	if err == nil {
		t.Error("expected error, got nil")
	}
}
