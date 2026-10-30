package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
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

// dnsOnlyZoneServer answers like Cloudflare for tokens holding only DNS
// permissions on zone-123: zone details succeed, listing zones needs Zone
// Read, and /user/tokens/verify accepts only user-owned tokens.
func dnsOnlyZoneServer(t *testing.T, userTokens ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones/zone-123":
			_ = json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{"id": "zone-123", "name": "example.com"}))
		case r.URL.Path == "/user/tokens/verify" && slices.Contains(userTokens, token):
			_ = json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{"id": "token-id", "status": "active"}))
		default:
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(errorResponse(10000, "Authentication error"))
		}
	}))
}

func TestProvider_Ping_ConfiguredZoneWithDNSOnlyTokens(t *testing.T) {
	server := dnsOnlyZoneServer(t, "user-token")
	defer server.Close()

	for _, token := range []string{"user-token", "cfat_account-token"} {
		t.Run(token, func(t *testing.T) {
			p, err := New("test-provider", &Config{Token: token, ZoneID: "zone-123", TTL: 300})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			p.client.apiEndpoint = server.URL
			if err := p.Ping(context.Background()); err != nil {
				t.Errorf("Ping() error = %v", err)
			}
		})
	}
}

func TestProvider_Ping_LooksUpZoneByName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/zones":
			_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{{"id": "zone-123", "name": "example.com"}}))
		case "/zones/zone-123":
			_ = json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{"id": "zone-123", "name": "example.com"}))
		default:
			t.Errorf("unexpected request: %s", r.URL.RequestURI())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	p, err := New("test-provider", &Config{Token: "test-token", Zone: "example.com", TTL: 300})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.client.apiEndpoint = server.URL
	if err := p.Ping(context.Background()); err != nil {
		t.Errorf("Ping() error = %v", err)
	}
}

func TestProvider_Ping_RejectedForZone(t *testing.T) {
	server := dnsOnlyZoneServer(t)
	defer server.Close()

	p, err := New("test-provider", &Config{Token: "test-token", ZoneID: "other-zone", TTL: 300})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.client.apiEndpoint = server.URL
	if err := p.Ping(context.Background()); err == nil {
		t.Error("Ping() succeeded for a zone the token cannot read")
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
			name:           "MX records never proxied even with metadata",
			providerConfig: true,
			record: provider.Record{
				Type:     provider.RecordTypeMX,
				Metadata: map[string]string{"proxied": "true"},
			},
			expected: false,
		},
		{
			name:           "NS records never proxied even with metadata",
			providerConfig: true,
			record: provider.Record{
				Type:     provider.RecordTypeNS,
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

func TestProvider_Create_MXAndNSRecords(t *testing.T) {
	tests := []struct {
		name         string
		record       provider.Record
		wantType     string
		wantContent  string
		wantPriority interface{} // nil when the request must omit "priority"
	}{
		{
			name: "MX record sends content and top-level priority",
			record: provider.Record{
				Hostname: "example.com",
				Type:     provider.RecordTypeMX,
				Target:   "mail.protonmail.ch",
				TTL:      3600,
				MX:       &provider.MXData{Preference: 10},
			},
			wantType:     "MX",
			wantContent:  "mail.protonmail.ch",
			wantPriority: float64(10),
		},
		{
			name: "MX record with preference zero still sends priority",
			record: provider.Record{
				Hostname: "example.com",
				Type:     provider.RecordTypeMX,
				Target:   "mail.protonmail.ch",
				TTL:      3600,
				MX:       &provider.MXData{Preference: 0},
			},
			wantType:     "MX",
			wantContent:  "mail.protonmail.ch",
			wantPriority: float64(0),
		},
		{
			name: "NS record sends content only",
			record: provider.Record{
				Hostname: "sub.example.com",
				Type:     provider.RecordTypeNS,
				Target:   "ns1.example.net",
				TTL:      3600,
			},
			wantType:    "NS",
			wantContent: "ns1.example.net",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedBody map[string]interface{}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					_ = json.NewDecoder(r.Body).Decode(&receivedBody)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{"id": "new-rec"}))
			}))
			defer server.Close()

			p := newTestProvider(t, server.URL)
			if err := p.Create(context.Background(), tt.record); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if receivedBody["type"] != tt.wantType {
				t.Errorf("type = %v, want %v", receivedBody["type"], tt.wantType)
			}
			if receivedBody["content"] != tt.wantContent {
				t.Errorf("content = %v, want %v", receivedBody["content"], tt.wantContent)
			}
			priority, hasPriority := receivedBody["priority"]
			if tt.wantPriority == nil {
				if hasPriority {
					t.Errorf("priority = %v, want the field to be omitted", priority)
				}
			} else if priority != tt.wantPriority {
				t.Errorf("priority = %v, want %v", priority, tt.wantPriority)
			}
			// Neither type is proxiable.
			if receivedBody["proxied"] != false {
				t.Errorf("proxied = %v, want false", receivedBody["proxied"])
			}
		})
	}
}

func TestProvider_Create_MXRecord_RequiresMXData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{"id": "new-rec"}))
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Create(context.Background(), provider.Record{
		Hostname: "example.com",
		Type:     provider.RecordTypeMX,
		Target:   "mail.protonmail.ch",
		TTL:      3600,
	})
	if err == nil {
		t.Fatal("expected an error when MX data is missing, got nil")
	}
	if !strings.Contains(err.Error(), "MX data is required") {
		t.Errorf("error = %q, want it to mention that MX data is required", err)
	}
}

func TestProvider_List_MapsMXAndNSRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Query().Get("type") {
		case "MX":
			_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
				{"id": "mx-1", "type": "MX", "name": "example.com", "content": "mail.protonmail.ch", "ttl": 3600, "priority": 10},
				{"id": "mx-2", "type": "MX", "name": "nopriority.example.com", "content": "mail.protonmail.ch", "ttl": 3600},
			}))
		case "NS":
			_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
				{"id": "ns-1", "type": "NS", "name": "sub.example.com", "content": "ns1.example.net", "ttl": 3600},
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

	byHostname := make(map[string]provider.Record, len(records))
	for _, rec := range records {
		byHostname[rec.Hostname] = rec
	}

	mx, ok := byHostname["example.com"]
	if !ok {
		t.Fatal("MX record for example.com missing from List()")
	}
	if mx.Type != provider.RecordTypeMX {
		t.Errorf("type = %q, want %q", mx.Type, provider.RecordTypeMX)
	}
	if mx.Target != "mail.protonmail.ch" {
		t.Errorf("target = %q, want %q", mx.Target, "mail.protonmail.ch")
	}
	if mx.MX == nil {
		t.Fatal("MX data is nil")
	}
	if mx.MX.Preference != 10 {
		t.Errorf("preference = %d, want %d", mx.MX.Preference, 10)
	}

	// A record the API returns without a priority still gets MX data, at the
	// zero preference, so comparisons never see a nil on one side only.
	noPriority, ok := byHostname["nopriority.example.com"]
	if !ok {
		t.Fatal("MX record for nopriority.example.com missing from List()")
	}
	if noPriority.MX == nil {
		t.Fatal("MX data is nil for a record without a priority field")
	}
	if noPriority.MX.Preference != 0 {
		t.Errorf("preference = %d, want %d", noPriority.MX.Preference, 0)
	}

	ns, ok := byHostname["sub.example.com"]
	if !ok {
		t.Fatal("NS record for sub.example.com missing from List()")
	}
	if ns.Type != provider.RecordTypeNS {
		t.Errorf("type = %q, want %q", ns.Type, provider.RecordTypeNS)
	}
	if ns.Target != "ns1.example.net" {
		t.Errorf("target = %q, want %q", ns.Target, "ns1.example.net")
	}
	if ns.MX != nil {
		t.Errorf("MX data = %+v, want nil on an NS record", ns.MX)
	}
}

func TestProvider_Update_MXRecordSendsNewPreference(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPatch:
			_ = json.NewDecoder(r.Body).Decode(&receivedBody)
			_ = json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{"id": "mx-1"}))
		default:
			_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
				{"id": "mx-1", "type": "MX", "name": "example.com", "content": "mail.protonmail.ch", "ttl": 3600, "priority": 10},
			}))
		}
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	existing := provider.Record{
		Hostname: "example.com",
		Type:     provider.RecordTypeMX,
		Target:   "mail.protonmail.ch",
		TTL:      3600,
		MX:       &provider.MXData{Preference: 10},
	}
	desired := existing
	desired.MX = &provider.MXData{Preference: 20}

	if err := p.Update(context.Background(), existing, desired); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedBody["type"] != "MX" {
		t.Errorf("type = %v, want MX", receivedBody["type"])
	}
	if receivedBody["priority"] != float64(20) {
		t.Errorf("priority = %v, want 20", receivedBody["priority"])
	}
	if receivedBody["content"] != "mail.protonmail.ch" {
		t.Errorf("content = %v, want mail.protonmail.ch", receivedBody["content"])
	}
}

func TestProvider_Update_MXRecord_RequiresMXData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(successProviderResponse([]map[string]interface{}{
				{"id": "mx-1", "type": "MX", "name": "example.com", "content": "mail.protonmail.ch", "ttl": 3600, "priority": 10},
			}))
			return
		}
		_ = json.NewEncoder(w).Encode(successProviderResponse(map[string]interface{}{"id": "mx-1"}))
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	existing := provider.Record{
		Hostname: "example.com",
		Type:     provider.RecordTypeMX,
		Target:   "mail.protonmail.ch",
		TTL:      3600,
		MX:       &provider.MXData{Preference: 10},
	}
	desired := existing
	desired.MX = nil

	err := p.Update(context.Background(), existing, desired)
	if err == nil {
		t.Fatal("expected an error when MX data is missing, got nil")
	}
	if !strings.Contains(err.Error(), "MX data is required") {
		t.Errorf("error = %q, want it to mention that MX data is required", err)
	}
}

func TestProvider_Capabilities_SupportsMXAndNS(t *testing.T) {
	config := &Config{Token: "token", ZoneID: "zone-123", TTL: 300}
	p, err := New("test", config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	caps := p.Capabilities()
	for _, rt := range []provider.RecordType{provider.RecordTypeMX, provider.RecordTypeNS} {
		if !caps.SupportsRecordType(rt) {
			t.Errorf("Capabilities() does not report support for %s", rt)
		}
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
			name:           "mx_never_proxied",
			defaultProxied: true,
			record:         provider.Record{Type: provider.RecordTypeMX, Target: "mail.example.com"},
			want:           false,
		},
		{
			name:           "ns_never_proxied",
			defaultProxied: true,
			record:         provider.Record{Type: provider.RecordTypeNS, Target: "ns1.example.net"},
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

func TestCloudflareRecordMatches_MXAndNS(t *testing.T) {
	preference := func(v uint16) *uint16 { return &v }
	mx := provider.Record{Hostname: "example.com", Type: provider.RecordTypeMX, Target: "mail.example.net", MX: &provider.MXData{Preference: 10}}
	ns := provider.Record{Hostname: "sub.example.com", Type: provider.RecordTypeNS, Target: "ns1.example.net"}

	tests := []struct {
		name   string
		api    dnsRecord
		record provider.Record
		want   bool
	}{
		{"MX with same exchanger and preference", dnsRecord{Name: "example.com", Type: "MX", Content: "Mail.Example.NET.", Priority: preference(10)}, mx, true},
		{"MX sibling with another preference", dnsRecord{Name: "example.com", Type: "MX", Content: "mail.example.net", Priority: preference(20)}, mx, false},
		{"MX without a priority", dnsRecord{Name: "example.com", Type: "MX", Content: "mail.example.net"}, mx, false},
		{"MX sibling with another exchanger", dnsRecord{Name: "example.com", Type: "MX", Content: "mail2.example.net", Priority: preference(10)}, mx, false},
		{"NS ignores case and trailing dot", dnsRecord{Name: "sub.example.com", Type: "NS", Content: "NS1.example.net."}, ns, true},
		{"NS sibling", dnsRecord{Name: "sub.example.com", Type: "NS", Content: "ns2.example.net"}, ns, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cloudflareRecordMatches(tt.api, tt.record); got != tt.want {
				t.Errorf("cloudflareRecordMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestListedTXTContent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"quoted value", `"protonmail-verification=abc123"`, "protonmail-verification=abc123"},
		{"unquoted value", "protonmail-verification=abc123", "protonmail-verification=abc123"},
		{"split long value", `"v=DKIM1; k=rsa; p=MIIB" "IjANBgkq"`, "v=DKIM1; k=rsa; p=MIIBIjANBgkq"},
		{"escaped quote", `"say \"hi\""`, `say "hi"`},
		{"unterminated", `"unterminated`, `"unterminated`},
		{"trailing quote only", `unterminated"`, `unterminated"`},
		{"a lone quote", `"`, `"`},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := listedTXTContent("example.com", tt.input)
			if err != nil || got != tt.want {
				t.Errorf("listedTXTContent(%q) = %q, %v; want %q", tt.input, got, err, tt.want)
			}
		})
	}
}

func TestCloudflareRecordMatches_QuotedTXT(t *testing.T) {
	record := provider.Record{Hostname: "example.com", Type: provider.RecordTypeTXT, Target: "v=spf1 -all"}
	if !cloudflareRecordMatches(dnsRecord{Name: "example.com", Type: "TXT", Content: `"v=spf1 -all"`}, record) {
		t.Error("quoted TXT read back did not match its declared value")
	}
	if cloudflareRecordMatches(dnsRecord{Name: "example.com", Type: "TXT", Content: `"v=spf1 ~all"`}, record) {
		t.Error("a different TXT value matched")
	}
}

func TestCloudflareRecordMatches_TXTDataContainingQuotes(t *testing.T) {
	quotedData := provider.Record{Hostname: "example.com", Type: provider.RecordTypeTXT, Target: `"hello"`}
	plain := provider.Record{Hostname: "example.com", Type: provider.RecordTypeTXT, Target: "hello"}
	stored := dnsRecord{Name: "example.com", Type: "TXT", Content: `"\"hello\""`}
	if !cloudflareRecordMatches(stored, quotedData) {
		t.Error("stored quoted data did not match its record")
	}
	if cloudflareRecordMatches(stored, plain) {
		t.Error("stored quoted data matched the plain value")
	}
	if cloudflareRecordMatches(dnsRecord{Name: "example.com", Type: "TXT", Content: `"hello"`}, quotedData) {
		t.Error("stored plain value matched the quoted data")
	}
	if got := apiContent(quotedData); got != `"\"hello\""` {
		t.Errorf("apiContent(%q) = %q, want quoted presentation", quotedData.Target, got)
	}
}

func TestTXTMemberMarkerPresentation(t *testing.T) {
	member := provider.Record{Hostname: "example.com", Type: provider.RecordTypeTXT, Target: `"hello"`}
	marker := provider.MemberOwnershipRecord(member.Hostname, 300, "x", member, nil)

	written := apiContent(marker)
	if !provider.MatchesMemberOwnership(written, "x", provider.Record{Type: provider.RecordTypeTXT, Target: `"\"hello\""`}) {
		t.Fatalf("written marker %q does not record the presentation text", written)
	}
	listed, err := listedTXTContent(marker.Hostname, written)
	if err != nil || listed != marker.Target {
		t.Fatalf("listed marker = %q, %v; want %q", listed, err, marker.Target)
	}
	// Earlier releases recorded the declared spelling, which on Cloudflare is
	// presentation text of the data.
	legacy := provider.MakeMemberOwnershipValue("x", provider.Record{Type: provider.RecordTypeTXT, Target: `"v=spf1 -all"`}, nil)
	listed, err = listedTXTContent(marker.Hostname, legacy)
	if err != nil || !provider.MatchesMemberOwnership(listed, "x", provider.Record{Type: provider.RecordTypeTXT, Target: "v=spf1 -all"}) {
		t.Fatalf("legacy marker listed as %q, %v; want ownership of v=spf1 -all", listed, err)
	}
}
