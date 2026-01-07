package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// successResponse creates a successful Cloudflare API response.
func successProviderResponse(result interface{}) map[string]interface{} {
	return map[string]interface{}{
		"success":  true,
		"errors":   []interface{}{},
		"messages": []interface{}{},
		"result":   result,
	}
}

func newTestProvider(t *testing.T, serverURL string) *Provider {
	t.Helper()
	config := &Config{
		Token:   "test-token",
		ZoneID:  "zone-123",
		TTL:     300,
		Proxied: false,
	}
	p, err := New("test-provider", config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	// Override API endpoint to use test server
	p.client.apiEndpoint = serverURL
	return p
}

func TestProvider_Name(t *testing.T) {
	config := &Config{
		Token:  "token",
		ZoneID: "zone-123",
		TTL:    300,
	}
	p, _ := New("my-instance", config)

	if p.Name() != "my-instance" {
		t.Errorf("expected name 'my-instance', got %s", p.Name())
	}
}

func TestProvider_Type(t *testing.T) {
	config := &Config{
		Token:  "token",
		ZoneID: "zone-123",
		TTL:    300,
	}
	p, _ := New("test", config)

	if p.Type() != "cloudflare" {
		t.Errorf("expected type 'cloudflare', got %s", p.Type())
	}
}

func TestProvider_Zone(t *testing.T) {
	config := &Config{
		Token:  "token",
		Zone:   "example.com",
		ZoneID: "zone-123",
		TTL:    300,
	}
	p, _ := New("test", config)

	if p.Zone() != "example.com" {
		t.Errorf("expected zone 'example.com', got %s", p.Zone())
	}
}

func TestProvider_New_NilConfig(t *testing.T) {
	_, err := New("test", nil)
	if err == nil {
		t.Error("expected error for nil config, got nil")
	}
}

func TestProvider_New_InvalidConfig(t *testing.T) {
	config := &Config{} // All fields missing
	_, err := New("test", config)
	if err == nil {
		t.Error("expected error for invalid config, got nil")
	}
}

func TestProvider_Ping_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{
			"id":     "token-id",
			"status": "active",
		}))
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Ping(context.Background())

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProvider_ZoneID_FromConfig(t *testing.T) {
	config := &Config{
		Token:  "token",
		ZoneID: "configured-zone-id",
		TTL:    300,
	}
	p, _ := New("test", config)

	zoneID, err := p.ZoneID(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if zoneID != "configured-zone-id" {
		t.Errorf("expected zone ID 'configured-zone-id', got %s", zoneID)
	}
}

func TestProvider_ZoneID_Lookup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/zones" {
			query := r.URL.Query()
			if query.Get("name") == "example.com" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
					{"id": "looked-up-zone-id", "name": "example.com", "status": "active"},
				}))
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{}))
	}))
	defer server.Close()

	config := &Config{
		Token: "token",
		Zone:  "example.com", // No ZoneID, should trigger lookup
		TTL:   300,
	}
	p, _ := New("test", config)
	p.client.apiEndpoint = server.URL

	zoneID, err := p.ZoneID(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if zoneID != "looked-up-zone-id" {
		t.Errorf("expected zone ID 'looked-up-zone-id', got %s", zoneID)
	}
}

func TestProvider_List_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		recordType := query.Get("type")

		w.Header().Set("Content-Type", "application/json")

		switch recordType {
		case "A":
			json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
				{"id": "rec-1", "type": "A", "name": "app.example.com", "content": "10.0.0.1", "ttl": 300},
			}))
		case "CNAME":
			json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
				{"id": "rec-2", "type": "CNAME", "name": "www.example.com", "content": "app.example.com", "ttl": 300},
			}))
		default:
			json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{}))
		}
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	records, err := p.List(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 records, got %d", len(records))
	}

	// Verify A record
	found := false
	for _, r := range records {
		if r.Type == provider.RecordTypeA && r.Hostname == "app.example.com" {
			found = true
			if r.Target != "10.0.0.1" {
				t.Errorf("expected A record target 10.0.0.1, got %s", r.Target)
			}
		}
	}
	if !found {
		t.Error("expected to find A record for app.example.com")
	}

	// Verify CNAME record
	found = false
	for _, r := range records {
		if r.Type == provider.RecordTypeCNAME && r.Hostname == "www.example.com" {
			found = true
			if r.Target != "app.example.com" {
				t.Errorf("expected CNAME record target app.example.com, got %s", r.Target)
			}
		}
	}
	if !found {
		t.Error("expected to find CNAME record for www.example.com")
	}
}

func TestProvider_Create_ARecord(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			json.NewDecoder(r.Body).Decode(&receivedBody)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{
			"id": "new-rec",
		}))
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	record := provider.Record{
		Hostname: "test.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
		TTL:      600,
	}

	err := p.Create(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedBody["type"] != "A" {
		t.Errorf("expected type A, got %v", receivedBody["type"])
	}
	if receivedBody["name"] != "test.example.com" {
		t.Errorf("expected name test.example.com, got %v", receivedBody["name"])
	}
	if receivedBody["content"] != "10.0.0.1" {
		t.Errorf("expected content 10.0.0.1, got %v", receivedBody["content"])
	}
}

func TestProvider_Create_CNAMERecord(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			json.NewDecoder(r.Body).Decode(&receivedBody)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{
			"id": "new-rec",
		}))
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	record := provider.Record{
		Hostname: "www.example.com",
		Type:     provider.RecordTypeCNAME,
		Target:   "app.example.com",
		TTL:      300,
	}

	err := p.Create(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedBody["type"] != "CNAME" {
		t.Errorf("expected type CNAME, got %v", receivedBody["type"])
	}
	if receivedBody["content"] != "app.example.com" {
		t.Errorf("expected content app.example.com, got %v", receivedBody["content"])
	}
}

func TestProvider_Create_WithProxied(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			json.NewDecoder(r.Body).Decode(&receivedBody)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{
			"id": "new-rec",
		}))
	}))
	defer server.Close()

	config := &Config{
		Token:   "test-token",
		ZoneID:  "zone-123",
		TTL:     300,
		Proxied: true, // Enable proxying
	}
	p, _ := New("proxied-provider", config)
	p.client.apiEndpoint = server.URL

	record := provider.Record{
		Hostname: "proxy.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	}

	err := p.Create(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedBody["proxied"] != true {
		t.Errorf("expected proxied true, got %v", receivedBody["proxied"])
	}
}

func TestProvider_Delete_Success(t *testing.T) {
	deleteCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodGet && r.URL.Path == "/zones/zone-123/dns_records" {
			// FindRecord call
			json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
				{"id": "rec-to-delete", "type": "A", "name": "delete.example.com", "content": "10.0.0.1"},
			}))
			return
		}

		if r.Method == http.MethodDelete && r.URL.Path == "/zones/zone-123/dns_records/rec-to-delete" {
			deleteCalled = true
			json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{
				"id": "rec-to-delete",
			}))
			return
		}

		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	record := provider.Record{
		Hostname: "delete.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	}

	err := p.Delete(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !deleteCalled {
		t.Error("expected delete endpoint to be called")
	}
}

func TestProvider_Delete_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return empty result - record not found
		json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{}))
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	record := provider.Record{
		Hostname: "nonexistent.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	}

	// Should not error when record doesn't exist
	err := p.Delete(context.Background(), record)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProvider_Factory(t *testing.T) {
	factory := Factory()

	config := map[string]string{
		"TOKEN":   "test-token",
		"ZONE_ID": "zone-123",
		"TTL":     "600",
		"PROXIED": "true",
	}

	p, err := factory("factory-test", config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Name() != "factory-test" {
		t.Errorf("expected name factory-test, got %s", p.Name())
	}
	if p.Type() != "cloudflare" {
		t.Errorf("expected type cloudflare, got %s", p.Type())
	}

	// Verify the cast works and check proxied setting
	cfProvider, ok := p.(*Provider)
	if !ok {
		t.Fatal("expected *Provider type")
	}
	if !cfProvider.proxied {
		t.Error("expected proxied true")
	}
	if cfProvider.ttl != 600 {
		t.Errorf("expected TTL 600, got %d", cfProvider.ttl)
	}
}

func TestProvider_NewFromMap_MissingToken(t *testing.T) {
	config := map[string]string{
		"ZONE_ID": "zone-123",
	}

	_, err := NewFromMap("test", config)
	if err == nil {
		t.Error("expected error for missing token, got nil")
	}
}

func TestProvider_ImplementsInterface(t *testing.T) {
	config := &Config{
		Token:  "token",
		ZoneID: "zone-123",
		TTL:    300,
	}
	p, _ := New("test", config)

	// Verify it implements provider.Provider
	var _ provider.Provider = p
}
