package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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
		_ = json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{
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
				_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
					{"id": "looked-up-zone-id", "name": "example.com", "status": "active"},
				}))
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{}))
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
			_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
				{"id": "rec-1", "type": "A", "name": "app.example.com", "content": "10.0.0.1", "ttl": 300},
			}))
		case "CNAME":
			_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
				{"id": "rec-2", "type": "CNAME", "name": "www.example.com", "content": "app.example.com", "ttl": 300},
			}))
		default:
			_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{}))
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
			_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{
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
			_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{
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
			_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{
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
		Target:   "203.0.113.10",
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
			_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
				{"id": "rec-to-delete", "type": "A", "name": "delete.example.com", "content": "10.0.0.1"},
			}))
			return
		}

		if r.Method == http.MethodDelete && r.URL.Path == "/zones/zone-123/dns_records/rec-to-delete" {
			deleteCalled = true
			_ = json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{
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

func TestProvider_Delete_SelectsMatchingTarget(t *testing.T) {
	var deletedID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones/zone-123/dns_records":
			_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
				{"id": "sibling", "type": "A", "name": "app.example.com", "content": "10.0.0.2"},
				{"id": "wanted", "type": "A", "name": "app.example.com", "content": "10.0.0.1"},
			}))
		case r.Method == http.MethodDelete:
			deletedID = strings.TrimPrefix(r.URL.Path, "/zones/zone-123/dns_records/")
			_ = json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{"id": deletedID}))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Delete(context.Background(), provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if deletedID != "wanted" {
		t.Fatalf("deleted record %q, want exact target record %q", deletedID, "wanted")
	}
}

func TestProvider_Delete_DoesNotDeleteWhenTargetMissing(t *testing.T) {
	deleteCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			deleteCalled = true
		}
		_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
			{"id": "sibling", "type": "A", "name": "app.example.com", "content": "10.0.0.2"},
		}))
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Delete(context.Background(), provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if deleteCalled {
		t.Fatal("Delete() removed a sibling when the requested target was absent")
	}
}

func TestProvider_Delete_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return empty result - record not found
		_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{}))
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

	p, err := factory(provider.FactoryConfig{
		Name:           "factory-test",
		ProviderConfig: config,
	})
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

func TestResolveProxied(t *testing.T) {
	tests := []struct {
		name           string
		providerConfig bool // provider-level proxied default
		record         provider.Record
		expected       bool
	}{
		{
			name:           "provider default true, no metadata",
			providerConfig: true,
			record: provider.Record{
				Type: provider.RecordTypeA,
			},
			expected: true,
		},
		{
			name:           "provider default false, no metadata",
			providerConfig: false,
			record: provider.Record{
				Type: provider.RecordTypeA,
			},
			expected: false,
		},
		{
			name:           "metadata overrides provider default to false",
			providerConfig: true,
			record: provider.Record{
				Type:     provider.RecordTypeA,
				Metadata: map[string]string{"proxied": "false"},
			},
			expected: false,
		},
		{
			name:           "metadata overrides provider default to true",
			providerConfig: false,
			record: provider.Record{
				Type:     provider.RecordTypeA,
				Metadata: map[string]string{"proxied": "true"},
			},
			expected: true,
		},
		{
			name:           "TXT records never proxied even with metadata",
			providerConfig: true,
			record: provider.Record{
				Type:     provider.RecordTypeTXT,
				Metadata: map[string]string{"proxied": "true"},
			},
			expected: false,
		},
		{
			name:           "SRV records never proxied even with metadata",
			providerConfig: true,
			record: provider.Record{
				Type:     provider.RecordTypeSRV,
				Metadata: map[string]string{"proxied": "true"},
			},
			expected: false,
		},
		{
			name:           "CNAME respects metadata override",
			providerConfig: false,
			record: provider.Record{
				Type:     provider.RecordTypeCNAME,
				Metadata: map[string]string{"proxied": "true"},
			},
			expected: true,
		},
		{
			name:           "AAAA respects metadata override",
			providerConfig: true,
			record: provider.Record{
				Type:     provider.RecordTypeAAAA,
				Metadata: map[string]string{"proxied": "false"},
			},
			expected: false,
		},
		{
			name:           "nil metadata uses provider default",
			providerConfig: true,
			record: provider.Record{
				Type:     provider.RecordTypeA,
				Metadata: nil,
			},
			expected: true,
		},
		{
			name:           "empty metadata map uses provider default",
			providerConfig: false,
			record: provider.Record{
				Type:     provider.RecordTypeA,
				Metadata: map[string]string{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				Token:   "test-token",
				ZoneID:  "zone-123",
				TTL:     300,
				Proxied: tt.providerConfig,
			}
			p, err := New("test", config)
			if err != nil {
				t.Fatalf("failed to create provider: %v", err)
			}

			result := p.resolveProxied(tt.record)
			if result != tt.expected {
				t.Errorf("resolveProxied() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestProvider_Create_WithMetadataProxiedOverride(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{
			"id": "new-rec",
		}))
	}))
	defer server.Close()

	// Provider default is proxied=false, but metadata overrides to true
	config := &Config{
		Token:   "test-token",
		ZoneID:  "zone-123",
		TTL:     300,
		Proxied: false,
	}
	p, _ := New("test", config)
	p.client.apiEndpoint = server.URL

	record := provider.Record{
		Hostname: "override.example.com",
		Type:     provider.RecordTypeA,
		Target:   "203.0.113.10",
		TTL:      300,
		Metadata: map[string]string{"proxied": "true"},
	}

	err := p.Create(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedBody["proxied"] != true {
		t.Errorf("expected proxied true (metadata override), got %v", receivedBody["proxied"])
	}
}

func TestProvider_Create_MetadataProxiedFalseOverridesDefault(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{
			"id": "new-rec",
		}))
	}))
	defer server.Close()

	// Provider default is proxied=true, but metadata overrides to false
	config := &Config{
		Token:   "test-token",
		ZoneID:  "zone-123",
		TTL:     300,
		Proxied: true,
	}
	p, _ := New("test", config)
	p.client.apiEndpoint = server.URL

	record := provider.Record{
		Hostname: "dns-only.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
		TTL:      300,
		Metadata: map[string]string{"proxied": "false"},
	}

	err := p.Create(context.Background(), record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedBody["proxied"] != false {
		t.Errorf("expected proxied false (metadata override), got %v", receivedBody["proxied"])
	}
}

func TestProvider_List_PopulatesProxiedMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		query := r.URL.Query()
		recordType := query.Get("type")

		switch recordType {
		case "A":
			_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
				{"id": "rec-1", "type": "A", "name": "proxied.example.com", "content": "10.0.0.1", "ttl": 1, "proxied": true},
				{"id": "rec-2", "type": "A", "name": "dnsonly.example.com", "content": "10.0.0.2", "ttl": 300, "proxied": false},
			}))
		case "AAAA":
			_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{}))
		case "CNAME":
			_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
				{"id": "rec-3", "type": "CNAME", "name": "www.example.com", "content": "example.com", "ttl": 300, "proxied": true},
			}))
		case "TXT":
			_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
				{"id": "rec-4", "type": "TXT", "name": "example.com", "content": "v=spf1", "ttl": 300, "proxied": false},
			}))
		case "SRV":
			_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{}))
		default:
			_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{}))
		}
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	records, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check A records have proxied metadata
	for _, rec := range records {
		switch rec.Hostname {
		case "proxied.example.com":
			if rec.Metadata == nil || rec.Metadata["proxied"] != "true" {
				t.Errorf("expected proxied.example.com Metadata[\"proxied\"]=\"true\", got %v", rec.Metadata)
			}
		case "dnsonly.example.com":
			if rec.Metadata == nil || rec.Metadata["proxied"] != "false" {
				t.Errorf("expected dnsonly.example.com Metadata[\"proxied\"]=\"false\", got %v", rec.Metadata)
			}
		case "www.example.com":
			if rec.Metadata == nil || rec.Metadata["proxied"] != "true" {
				t.Errorf("expected www.example.com Metadata[\"proxied\"]=\"true\", got %v", rec.Metadata)
			}
		case "example.com":
			// TXT records should NOT have proxied metadata
			if rec.Metadata != nil {
				if _, hasProxied := rec.Metadata["proxied"]; hasProxied {
					t.Errorf("TXT record should not have proxied metadata, got %v", rec.Metadata)
				}
			}
		}
	}
}

// newResolveProxiedProvider builds a minimal provider for resolveProxied unit
// tests, with a given provider-level default and a discarding logger.
func newResolveProxiedProvider(defaultProxied bool) *Provider {
	return &Provider{
		name:    "test-cf",
		proxied: defaultProxied,
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestResolveProxied_NonRoutableTargetsDemoted(t *testing.T) {
	// A/AAAA records whose target cannot be proxied by Cloudflare should be
	// forced unproxied regardless of the provider default or an explicit hint.
	nonRoutable := []struct {
		name   string
		typ    provider.RecordType
		target string
	}{
		{"rfc1918_10", provider.RecordTypeA, "10.0.0.1"},
		{"rfc1918_172", provider.RecordTypeA, "172.16.5.9"},
		{"rfc1918_192", provider.RecordTypeA, "192.168.0.144"},
		{"loopback_v4", provider.RecordTypeA, "127.0.0.1"},
		{"linklocal_v4", provider.RecordTypeA, "169.254.1.1"},
		{"unspecified_v4", provider.RecordTypeA, "0.0.0.0"},
		{"cgnat_low", provider.RecordTypeA, "100.64.0.1"},
		{"cgnat_high", provider.RecordTypeA, "100.127.255.254"},
		{"ula_v6", provider.RecordTypeAAAA, "fd00::1"},
		{"loopback_v6", provider.RecordTypeAAAA, "::1"},
		{"linklocal_v6", provider.RecordTypeAAAA, "fe80::1"},
	}

	for _, tc := range nonRoutable {
		t.Run(tc.name+"_default_true", func(t *testing.T) {
			p := newResolveProxiedProvider(true)
			rec := provider.Record{Hostname: "app.example.com", Type: tc.typ, Target: tc.target}
			if p.resolveProxied(rec) {
				t.Errorf("resolveProxied(%s %s) = true, want false (non-routable, default on)", tc.typ, tc.target)
			}
		})
		t.Run(tc.name+"_explicit_true", func(t *testing.T) {
			p := newResolveProxiedProvider(false)
			rec := provider.Record{
				Hostname: "app.example.com",
				Type:     tc.typ,
				Target:   tc.target,
				Metadata: map[string]string{"proxied": "true"},
			}
			if p.resolveProxied(rec) {
				t.Errorf("resolveProxied(%s %s, explicit true) = true, want false (non-routable overrides explicit)", tc.typ, tc.target)
			}
		})
	}
}

func TestResolveProxied_RoutableAndOtherTypesUnchanged(t *testing.T) {
	tests := []struct {
		name           string
		defaultProxied bool
		record         provider.Record
		want           bool
	}{
		{
			name:           "public_v4_default_true",
			defaultProxied: true,
			record:         provider.Record{Type: provider.RecordTypeA, Target: "1.1.1.1"},
			want:           true,
		},
		{
			name:           "public_v4_default_false",
			defaultProxied: false,
			record:         provider.Record{Type: provider.RecordTypeA, Target: "1.1.1.1"},
			want:           false,
		},
		{
			name:           "public_v4_explicit_true",
			defaultProxied: false,
			record:         provider.Record{Type: provider.RecordTypeA, Target: "8.8.8.8", Metadata: map[string]string{"proxied": "true"}},
			want:           true,
		},
		{
			name:           "public_v6_default_true",
			defaultProxied: true,
			record:         provider.Record{Type: provider.RecordTypeAAAA, Target: "2606:4700:4700::1111"},
			want:           true,
		},
		{
			// CNAME targets are names, not IPs — never inferred, honor default/hint.
			name:           "cname_private_looking_default_true",
			defaultProxied: true,
			record:         provider.Record{Type: provider.RecordTypeCNAME, Target: "internal.example.com"},
			want:           true,
		},
		{
			name:           "cname_explicit_false",
			defaultProxied: true,
			record:         provider.Record{Type: provider.RecordTypeCNAME, Target: "internal.example.com", Metadata: map[string]string{"proxied": "false"}},
			want:           false,
		},
		{
			// A record with a non-IP target (shouldn't happen, but must not panic).
			name:           "a_record_non_ip_target",
			defaultProxied: true,
			record:         provider.Record{Type: provider.RecordTypeA, Target: "not-an-ip"},
			want:           true,
		},
		{
			name:           "txt_never_proxied",
			defaultProxied: true,
			record:         provider.Record{Type: provider.RecordTypeTXT, Target: "some text"},
			want:           false,
		},
		{
			name:           "srv_never_proxied",
			defaultProxied: true,
			record:         provider.Record{Type: provider.RecordTypeSRV, Target: "srv.example.com"},
			want:           false,
		},
		{
			name:           "explicit_false_public_target",
			defaultProxied: true,
			record:         provider.Record{Type: provider.RecordTypeA, Target: "1.1.1.1", Metadata: map[string]string{"proxied": "false"}},
			want:           false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newResolveProxiedProvider(tc.defaultProxied)
			if got := p.resolveProxied(tc.record); got != tc.want {
				t.Errorf("resolveProxied() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProvider_RecordNeedsUpdate(t *testing.T) {
	// existing is what List reports; desired is what the reconciler wants,
	// carrying the per-record hint (if any) in Metadata and the configured TTL.
	listed := func(typ provider.RecordType, target string, proxied bool) provider.Record {
		rec := provider.Record{Hostname: "app.example.com", Type: typ, Target: target, TTL: 300, Metadata: proxiedMetadata(proxied)}
		if proxied {
			rec.TTL = 1
		}
		return rec
	}
	wanted := func(typ provider.RecordType, target string, metadata map[string]string) provider.Record {
		return provider.Record{Hostname: "app.example.com", Type: typ, Target: target, TTL: 300, Metadata: metadata}
	}

	tests := []struct {
		name           string
		defaultProxied bool
		existing       provider.Record
		desired        provider.Record
		want           bool
	}{
		{
			name:           "label false on a proxied record",
			defaultProxied: true,
			existing:       listed(provider.RecordTypeA, "1.1.1.1", true),
			desired:        wanted(provider.RecordTypeA, "1.1.1.1", map[string]string{"proxied": "false"}),
			want:           true,
		},
		{
			name:           "label true on a DNS-only record",
			defaultProxied: false,
			existing:       listed(provider.RecordTypeA, "1.1.1.1", false),
			desired:        wanted(provider.RecordTypeA, "1.1.1.1", map[string]string{"proxied": "true"}),
			want:           true,
		},
		{
			name:           "label matches existing state",
			defaultProxied: true,
			existing:       listed(provider.RecordTypeA, "1.1.1.1", false),
			desired:        wanted(provider.RecordTypeA, "1.1.1.1", map[string]string{"proxied": "false"}),
			want:           false,
		},
		{
			name:           "default flipped to false, no label",
			defaultProxied: false,
			existing:       listed(provider.RecordTypeA, "1.1.1.1", true),
			desired:        wanted(provider.RecordTypeA, "1.1.1.1", nil),
			want:           true,
		},
		{
			name:           "default flipped to true, no label",
			defaultProxied: true,
			existing:       listed(provider.RecordTypeA, "1.1.1.1", false),
			desired:        wanted(provider.RecordTypeA, "1.1.1.1", nil),
			want:           true,
		},
		{
			name:           "default matches, proxied TTL 1 against configured TTL 300 is not a change",
			defaultProxied: true,
			existing:       listed(provider.RecordTypeA, "1.1.1.1", true),
			desired:        wanted(provider.RecordTypeA, "1.1.1.1", nil),
			want:           false,
		},
		{
			name:           "CNAME honors the label",
			defaultProxied: true,
			existing:       listed(provider.RecordTypeCNAME, "origin.example.com", true),
			desired:        wanted(provider.RecordTypeCNAME, "origin.example.com", map[string]string{"proxied": "false"}),
			want:           true,
		},
		{
			name:           "non-routable target demoted in both views",
			defaultProxied: true,
			existing:       listed(provider.RecordTypeA, "10.0.0.5", false),
			desired:        wanted(provider.RecordTypeA, "10.0.0.5", nil),
			want:           false,
		},
		{
			name:           "non-routable target with explicit proxied=true demoted in both views",
			defaultProxied: false,
			existing:       listed(provider.RecordTypeA, "192.168.1.20", false),
			desired:        wanted(provider.RecordTypeA, "192.168.1.20", map[string]string{"proxied": "true"}),
			want:           false,
		},
		{
			name:           "existing record without proxied metadata is never updated",
			defaultProxied: true,
			existing:       provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "1.1.1.1", TTL: 300},
			desired:        wanted(provider.RecordTypeA, "1.1.1.1", map[string]string{"proxied": "false"}),
			want:           false,
		},
		{
			name:           "TXT records carry no proxied state",
			defaultProxied: true,
			existing:       provider.Record{Hostname: "_dnsweaver.app.example.com", Type: provider.RecordTypeTXT, Target: "heritage=dnsweaver", TTL: 300},
			desired:        provider.Record{Hostname: "_dnsweaver.app.example.com", Type: provider.RecordTypeTXT, Target: "heritage=dnsweaver", TTL: 300},
			want:           false,
		},
		{
			name:           "SRV records carry no proxied state",
			defaultProxied: true,
			existing:       provider.Record{Hostname: "_sip._tcp.example.com", Type: provider.RecordTypeSRV, Target: "sip.example.com", TTL: 300, SRV: &provider.SRVData{Priority: 10, Weight: 5, Port: 5060}},
			desired:        provider.Record{Hostname: "_sip._tcp.example.com", Type: provider.RecordTypeSRV, Target: "sip.example.com", TTL: 300, SRV: &provider.SRVData{Priority: 10, Weight: 5, Port: 5060}},
			want:           false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newResolveProxiedProvider(tc.defaultProxied)
			if got := p.RecordNeedsUpdate(tc.existing, tc.desired); got != tc.want {
				t.Errorf("RecordNeedsUpdate() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProvider_RecordNeedsUpdate_DoesNotLogDemotion(t *testing.T) {
	// The comparison runs every reconcile pass; the non-routable demotion
	// warning belongs to writes only, or it would repeat every interval.
	var buf bytes.Buffer
	p := &Provider{
		name:    "test-cf",
		proxied: true,
		logger:  slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	existing := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.5", TTL: 300, Metadata: proxiedMetadata(false)}
	desired := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.5", TTL: 300}

	if p.RecordNeedsUpdate(existing, desired) {
		t.Error("RecordNeedsUpdate() = true for a demoted target that is already DNS-only, want false")
	}
	if buf.Len() != 0 {
		t.Errorf("comparison logged: %s", buf.String())
	}

	// The write path still warns.
	if p.resolveProxied(desired) {
		t.Error("resolveProxied() = true for a non-routable target, want false")
	}
	if !strings.Contains(buf.String(), "auto-disabling Cloudflare proxy") {
		t.Errorf("resolveProxied did not log the demotion warning: %s", buf.String())
	}
}
