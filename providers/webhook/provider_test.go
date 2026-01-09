package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

func TestProvider_Interface(t *testing.T) {
	// Verify Provider implements provider.Provider at compile time
	var _ provider.Provider = (*Provider)(nil)
}

func TestNew(t *testing.T) {
	t.Run("creates provider with valid config", func(t *testing.T) {
		config := &Config{
			URL:     "http://webhook.example.com",
			Timeout: 30 * time.Second,
		}

		p, err := New("test", config)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		if p.Name() != "test" {
			t.Errorf("Name() = %q, want %q", p.Name(), "test")
		}
		if p.Type() != "webhook" {
			t.Errorf("Type() = %q, want %q", p.Type(), "webhook")
		}
	})

	t.Run("returns error for nil config", func(t *testing.T) {
		_, err := New("test", nil)
		if err == nil {
			t.Error("New() expected error for nil config")
		}
	})

	t.Run("returns error for invalid config", func(t *testing.T) {
		config := &Config{
			URL: "", // Missing required URL
		}

		_, err := New("test", config)
		if err == nil {
			t.Error("New() expected error for invalid config")
		}
	})
}

func TestNewFromMap(t *testing.T) {
	t.Run("creates provider from map", func(t *testing.T) {
		config := map[string]string{
			"URL":         "http://webhook.example.com",
			"TIMEOUT":     "60s",
			"AUTH_HEADER": "X-API-Key",
			"AUTH_TOKEN":  "secret",
			"RETRIES":     "5",
			"RETRY_DELAY": "2s",
		}

		p, err := NewFromMap("test", config)
		if err != nil {
			t.Fatalf("NewFromMap() error = %v", err)
		}

		if p.Name() != "test" {
			t.Errorf("Name() = %q, want %q", p.Name(), "test")
		}
	})

	t.Run("uses defaults for missing optional fields", func(t *testing.T) {
		config := map[string]string{
			"URL": "http://webhook.example.com",
		}

		p, err := NewFromMap("test", config)
		if err != nil {
			t.Fatalf("NewFromMap() error = %v", err)
		}

		if p.Name() != "test" {
			t.Errorf("Name() = %q, want %q", p.Name(), "test")
		}
	})
}

func TestProvider_Ping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	config := &Config{
		URL:     server.URL,
		Timeout: 5 * time.Second,
		Retries: 0,
	}

	p, err := New("test", config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = p.Ping(context.Background())
	if err != nil {
		t.Errorf("Ping() unexpected error: %v", err)
	}
}

func TestProvider_List(t *testing.T) {
	t.Run("converts webhook records to provider records", func(t *testing.T) {
		webhookRecords := []RecordResponse{
			{Hostname: "app.example.com", Type: "A", Value: "10.0.0.1", TTL: 300, ID: "rec-1"},
			{Hostname: "www.example.com", Type: "CNAME", Value: "app.example.com", TTL: 300, ID: "rec-2"},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/list" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(webhookRecords)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		config := &Config{
			URL:     server.URL,
			Timeout: 5 * time.Second,
			Retries: 0,
		}

		p, err := New("test", config)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		records, err := p.List(context.Background())
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}

		if len(records) != 2 {
			t.Fatalf("List() returned %d records, want 2", len(records))
		}

		// Check A record
		if records[0].Hostname != "app.example.com" {
			t.Errorf("records[0].Hostname = %q, want %q", records[0].Hostname, "app.example.com")
		}
		if records[0].Type != provider.RecordTypeA {
			t.Errorf("records[0].Type = %q, want %q", records[0].Type, provider.RecordTypeA)
		}
		if records[0].Target != "10.0.0.1" {
			t.Errorf("records[0].Target = %q, want %q", records[0].Target, "10.0.0.1")
		}
		if records[0].ProviderID != "rec-1" {
			t.Errorf("records[0].ProviderID = %q, want %q", records[0].ProviderID, "rec-1")
		}

		// Check CNAME record
		if records[1].Type != provider.RecordTypeCNAME {
			t.Errorf("records[1].Type = %q, want %q", records[1].Type, provider.RecordTypeCNAME)
		}
	})

	t.Run("handles empty list", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]RecordResponse{})
		}))
		defer server.Close()

		config := &Config{
			URL:     server.URL,
			Timeout: 5 * time.Second,
			Retries: 0,
		}

		p, _ := New("test", config)
		records, err := p.List(context.Background())
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}

		if len(records) != 0 {
			t.Errorf("List() returned %d records, want 0", len(records))
		}
	})
}

func TestProvider_Create(t *testing.T) {
	t.Run("creates A record", func(t *testing.T) {
		var received RecordRequest

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/create" && r.Method == http.MethodPost {
				_ = json.NewDecoder(r.Body).Decode(&received)
				w.WriteHeader(http.StatusCreated)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		config := &Config{
			URL:     server.URL,
			Timeout: 5 * time.Second,
			Retries: 0,
		}

		p, _ := New("test", config)
		record := provider.Record{
			Hostname: "app.example.com",
			Type:     provider.RecordTypeA,
			Target:   "10.0.0.1",
			TTL:      300,
		}

		err := p.Create(context.Background(), record)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		if received.Hostname != "app.example.com" {
			t.Errorf("received.Hostname = %q, want %q", received.Hostname, "app.example.com")
		}
		if received.Type != "A" {
			t.Errorf("received.Type = %q, want %q", received.Type, "A")
		}
		if received.Value != "10.0.0.1" {
			t.Errorf("received.Value = %q, want %q", received.Value, "10.0.0.1")
		}
		if received.TTL != 300 {
			t.Errorf("received.TTL = %d, want %d", received.TTL, 300)
		}
	})

	t.Run("creates CNAME record", func(t *testing.T) {
		var received RecordRequest

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&received)
			w.WriteHeader(http.StatusCreated)
		}))
		defer server.Close()

		config := &Config{
			URL:     server.URL,
			Timeout: 5 * time.Second,
			Retries: 0,
		}

		p, _ := New("test", config)
		record := provider.Record{
			Hostname: "www.example.com",
			Type:     provider.RecordTypeCNAME,
			Target:   "app.example.com",
			TTL:      300,
		}

		err := p.Create(context.Background(), record)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		if received.Type != "CNAME" {
			t.Errorf("received.Type = %q, want %q", received.Type, "CNAME")
		}
	})
}

func TestProvider_Delete(t *testing.T) {
	t.Run("deletes record", func(t *testing.T) {
		var received DeleteRequest

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/delete" && r.Method == http.MethodDelete {
				_ = json.NewDecoder(r.Body).Decode(&received)
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		config := &Config{
			URL:     server.URL,
			Timeout: 5 * time.Second,
			Retries: 0,
		}

		p, _ := New("test", config)
		record := provider.Record{
			Hostname: "app.example.com",
			Type:     provider.RecordTypeA,
			Target:   "10.0.0.1",
		}

		err := p.Delete(context.Background(), record)
		if err != nil {
			t.Fatalf("Delete() error = %v", err)
		}

		if received.Hostname != "app.example.com" {
			t.Errorf("received.Hostname = %q, want %q", received.Hostname, "app.example.com")
		}
		if received.Type != "A" {
			t.Errorf("received.Type = %q, want %q", received.Type, "A")
		}
	})
}

func TestFactory(t *testing.T) {
	t.Run("returns working factory", func(t *testing.T) {
		factory := Factory()

		config := map[string]string{
			"URL": "http://webhook.example.com",
		}

		p, err := factory("test", config)
		if err != nil {
			t.Fatalf("Factory() error = %v", err)
		}

		if p.Name() != "test" {
			t.Errorf("Name() = %q, want %q", p.Name(), "test")
		}
		if p.Type() != "webhook" {
			t.Errorf("Type() = %q, want %q", p.Type(), "webhook")
		}
	})

	t.Run("factory returns error for invalid config", func(t *testing.T) {
		factory := Factory()

		config := map[string]string{
			"URL": "", // Missing URL
		}

		_, err := factory("test", config)
		if err == nil {
			t.Error("Factory() expected error for invalid config")
		}
	})
}
