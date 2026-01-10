package technitium

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

func newTestProvider(t *testing.T, serverURL string) *Provider {
	t.Helper()
	config := &Config{
		URL:   serverURL,
		Token: "test-token",
		Zone:  "example.com",
		TTL:   300,
	}
	p, err := New("test-provider", config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	return p
}

func TestProvider_Name(t *testing.T) {
	config := &Config{
		URL:   "http://localhost:5380",
		Token: "token",
		Zone:  "example.com",
		TTL:   300,
	}
	p, _ := New("my-instance", config)

	if p.Name() != "my-instance" {
		t.Errorf("expected name 'my-instance', got %s", p.Name())
	}
}

func TestProvider_Type(t *testing.T) {
	config := &Config{
		URL:   "http://localhost:5380",
		Token: "token",
		Zone:  "example.com",
		TTL:   300,
	}
	p, _ := New("test", config)

	if p.Type() != "technitium" {
		t.Errorf("expected type 'technitium', got %s", p.Type())
	}
}

func TestProvider_Zone(t *testing.T) {
	config := &Config{
		URL:   "http://localhost:5380",
		Token: "token",
		Zone:  "internal.example.com",
		TTL:   300,
	}
	p, _ := New("test", config)

	if p.Zone() != "internal.example.com" {
		t.Errorf("expected zone 'internal.example.com', got %s", p.Zone())
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "ok",
			"response": map[string]interface{}{},
		})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Ping(context.Background())

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProvider_Ping_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "error",
			"errorMessage": "Invalid token",
		})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Ping(context.Background())

	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestProvider_List_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"response": map[string]interface{}{
				"zone": map[string]interface{}{
					"name":     "example.com",
					"type":     "Primary",
					"disabled": false,
				},
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
						"ttl":      600,
						"disabled": false,
						"rData": map[string]interface{}{
							"cname": "app.example.com",
						},
					},
					{
						"name":     "example.com",
						"type":     "NS",
						"ttl":      3600,
						"disabled": false,
						"rData": map[string]interface{}{
							"value": "ns1.example.com",
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	records, err := p.List(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only return A and CNAME records, not NS
	if len(records) != 2 {
		t.Fatalf("expected 2 records (A and CNAME), got %d", len(records))
	}

	// Check A record
	if records[0].Type != provider.RecordTypeA {
		t.Errorf("expected first record type A, got %s", records[0].Type)
	}
	if records[0].Target != "10.0.0.1" {
		t.Errorf("expected first record target 10.0.0.1, got %s", records[0].Target)
	}

	// Check CNAME record
	if records[1].Type != provider.RecordTypeCNAME {
		t.Errorf("expected second record type CNAME, got %s", records[1].Type)
	}
	if records[1].Target != "app.example.com" {
		t.Errorf("expected second record target app.example.com, got %s", records[1].Target)
	}
}

func TestProvider_Create_ARecord(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		query := r.URL.Query()
		if query.Get("type") != "A" {
			t.Errorf("expected type A, got %s", query.Get("type"))
		}
		if query.Get("ipAddress") != "192.168.1.100" {
			t.Errorf("expected ipAddress 192.168.1.100, got %s", query.Get("ipAddress"))
		}
		if query.Get("ttl") != "300" {
			t.Errorf("expected ttl 300, got %s", query.Get("ttl"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
		})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Create(context.Background(), provider.Record{
		Hostname: "service.example.com",
		Type:     provider.RecordTypeA,
		Target:   "192.168.1.100",
		TTL:      300,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected API to be called")
	}
}

func TestProvider_Create_CNAMERecord(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		query := r.URL.Query()
		if query.Get("type") != "CNAME" {
			t.Errorf("expected type CNAME, got %s", query.Get("type"))
		}
		if query.Get("cname") != "target.example.com" {
			t.Errorf("expected cname target.example.com, got %s", query.Get("cname"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
		})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Create(context.Background(), provider.Record{
		Hostname: "alias.example.com",
		Type:     provider.RecordTypeCNAME,
		Target:   "target.example.com",
		TTL:      300,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected API to be called")
	}
}

func TestProvider_Create_DefaultTTL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		// Provider default TTL is 300
		if query.Get("ttl") != "300" {
			t.Errorf("expected default ttl 300, got %s", query.Get("ttl"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
		})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Create(context.Background(), provider.Record{
		Hostname: "service.example.com",
		Type:     provider.RecordTypeA,
		Target:   "192.168.1.100",
		TTL:      0, // No TTL specified, should use provider default
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProvider_Delete_ARecord(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/api/zones/records/delete" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("type") != "A" {
			t.Errorf("expected type A, got %s", query.Get("type"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
		})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Delete(context.Background(), provider.Record{
		Hostname: "service.example.com",
		Type:     provider.RecordTypeA,
		Target:   "192.168.1.100",
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected API to be called")
	}
}

func TestProvider_Delete_CNAMERecord(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		query := r.URL.Query()
		if query.Get("type") != "CNAME" {
			t.Errorf("expected type CNAME, got %s", query.Get("type"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
		})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Delete(context.Background(), provider.Record{
		Hostname: "alias.example.com",
		Type:     provider.RecordTypeCNAME,
		Target:   "target.example.com",
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected API to be called")
	}
}

func TestProvider_Create_SRVRecord(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		query := r.URL.Query()
		if query.Get("type") != "SRV" {
			t.Errorf("expected type SRV, got %s", query.Get("type"))
		}
		if query.Get("priority") != "10" {
			t.Errorf("expected priority 10, got %s", query.Get("priority"))
		}
		if query.Get("weight") != "5" {
			t.Errorf("expected weight 5, got %s", query.Get("weight"))
		}
		if query.Get("port") != "25565" {
			t.Errorf("expected port 25565, got %s", query.Get("port"))
		}
		if query.Get("target") != "mc.example.com" {
			t.Errorf("expected target mc.example.com, got %s", query.Get("target"))
		}
		if query.Get("ttl") != "300" {
			t.Errorf("expected ttl 300, got %s", query.Get("ttl"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
		})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Create(context.Background(), provider.Record{
		Hostname: "_minecraft._tcp.example.com",
		Type:     provider.RecordTypeSRV,
		Target:   "mc.example.com",
		TTL:      300,
		SRV: &provider.SRVData{
			Priority: 10,
			Weight:   5,
			Port:     25565,
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected API to be called")
	}
}

func TestProvider_Create_SRVRecord_MissingSRVData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("API should not be called when SRV data is missing")
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Create(context.Background(), provider.Record{
		Hostname: "_minecraft._tcp.example.com",
		Type:     provider.RecordTypeSRV,
		Target:   "mc.example.com",
		TTL:      300,
		SRV:      nil, // Missing SRV data
	})

	if err == nil {
		t.Error("expected error for missing SRV data, got nil")
	}
}

func TestProvider_Delete_SRVRecord(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/api/zones/records/delete" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("type") != "SRV" {
			t.Errorf("expected type SRV, got %s", query.Get("type"))
		}
		if query.Get("priority") != "10" {
			t.Errorf("expected priority 10, got %s", query.Get("priority"))
		}
		if query.Get("weight") != "5" {
			t.Errorf("expected weight 5, got %s", query.Get("weight"))
		}
		if query.Get("port") != "25565" {
			t.Errorf("expected port 25565, got %s", query.Get("port"))
		}
		if query.Get("target") != "mc.example.com" {
			t.Errorf("expected target mc.example.com, got %s", query.Get("target"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
		})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	err := p.Delete(context.Background(), provider.Record{
		Hostname: "_minecraft._tcp.example.com",
		Type:     provider.RecordTypeSRV,
		Target:   "mc.example.com",
		SRV: &provider.SRVData{
			Priority: 10,
			Weight:   5,
			Port:     25565,
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected API to be called")
	}
}

func TestProvider_List_WithSRVRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"response": map[string]interface{}{
				"zone": map[string]interface{}{
					"name":     "example.com",
					"type":     "Primary",
					"disabled": false,
				},
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
						"name":     "_minecraft._tcp.example.com",
						"type":     "SRV",
						"ttl":      3600,
						"disabled": false,
						"rData": map[string]interface{}{
							"priority": 10,
							"weight":   5,
							"port":     25565,
							"target":   "mc.example.com",
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	records, err := p.List(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records (A and SRV), got %d", len(records))
	}

	// Check A record
	if records[0].Type != provider.RecordTypeA {
		t.Errorf("expected first record type A, got %s", records[0].Type)
	}

	// Check SRV record
	if records[1].Type != provider.RecordTypeSRV {
		t.Errorf("expected second record type SRV, got %s", records[1].Type)
	}
	if records[1].Target != "mc.example.com" {
		t.Errorf("expected SRV target mc.example.com, got %s", records[1].Target)
	}
	if records[1].SRV == nil {
		t.Fatal("expected SRV data to be set")
	}
	if records[1].SRV.Priority != 10 {
		t.Errorf("expected SRV priority 10, got %d", records[1].SRV.Priority)
	}
	if records[1].SRV.Weight != 5 {
		t.Errorf("expected SRV weight 5, got %d", records[1].SRV.Weight)
	}
	if records[1].SRV.Port != 25565 {
		t.Errorf("expected SRV port 25565, got %d", records[1].SRV.Port)
	}
}

func TestProvider_ImplementsInterface(t *testing.T) {
	config := &Config{
		URL:   "http://localhost:5380",
		Token: "token",
		Zone:  "example.com",
		TTL:   300,
	}
	p, _ := New("test", config)

	// This is a compile-time check, but we can also verify at runtime
	var _ provider.Provider = p
}
