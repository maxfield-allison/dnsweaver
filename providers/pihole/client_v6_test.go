package pihole

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

func TestV6APIClient_Authentication(t *testing.T) {
	authCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" && r.Method == http.MethodPost {
			authCalled = true
			var req struct {
				Password string `json:"password"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)

			if req.Password != "correctpassword" {
				w.WriteHeader(http.StatusUnauthorized)
				resp := map[string]any{
					"session": map[string]any{
						"valid":   false,
						"message": "Invalid password",
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}

			resp := map[string]any{
				"session": map[string]any{
					"valid":    true,
					"sid":      "test-session-id",
					"validity": 300,
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Check for SID in request
		sid := r.Header.Get("X-FTL-SID")
		if sid != "test-session-id" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if r.URL.Path == "/api/config/dns" {
			resp := map[string]any{
				"config": map[string]any{
					"hosts":        []string{},
					"cnameRecords": []string{},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewV6APIClient(server.URL, "correctpassword", "")
	records, err := client.List(context.Background())

	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if !authCalled {
		t.Error("Authentication was not called")
	}
	if len(records) != 0 {
		t.Errorf("List() returned %d records, want 0", len(records))
	}
}

func TestV6APIClient_AuthenticationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			w.WriteHeader(http.StatusOK)
			resp := map[string]any{
				"session": map[string]any{
					"valid":   false,
					"message": "Invalid password",
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewV6APIClient(server.URL, "wrongpassword", "")
	_, err := client.List(context.Background())

	if err == nil {
		t.Fatal("List() expected error for invalid password, got nil")
	}
}

func TestV6APIClient_List(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			resp := map[string]any{
				"session": map[string]any{
					"valid":    true,
					"sid":      "test-sid",
					"validity": 300,
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if r.URL.Path == "/api/config/dns" {
			// Pi-hole v6 returns: { "config": { "dns": { "hosts": [...], "cnameRecords": [...] } } }
			resp := map[string]any{
				"config": map[string]any{
					"dns": map[string]any{
						"hosts": []string{
							"192.168.1.100 server.local",
							"192.168.1.101 db.local cache.local",
							"2001:db8::1 ipv6host.local",
						},
						"cnameRecords": []string{
							"www.local,server.local",
							"api.local,server.local,3600",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewV6APIClient(server.URL, "password", "")
	records, err := client.List(context.Background())

	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	// Expected records:
	// - server.local -> 192.168.1.100 (A)
	// - db.local -> 192.168.1.101 (A)
	// - cache.local -> 192.168.1.101 (A)
	// - ipv6host.local -> 2001:db8::1 (AAAA)
	// - www.local -> server.local (CNAME)
	// - api.local -> server.local (CNAME)
	expectedCount := 6
	if len(records) != expectedCount {
		t.Errorf("List() returned %d records, want %d", len(records), expectedCount)
		for i, r := range records {
			t.Logf("  [%d] %s %s -> %s", i, r.Type, r.Hostname, r.Target)
		}
	}

	// Verify specific records exist
	hasRecord := func(hostname string, recordType provider.RecordType, target string) bool {
		for _, r := range records {
			if r.Hostname == hostname && r.Type == recordType && r.Target == target {
				return true
			}
		}
		return false
	}

	if !hasRecord("server.local", provider.RecordTypeA, "192.168.1.100") {
		t.Error("Missing A record: server.local -> 192.168.1.100")
	}
	if !hasRecord("ipv6host.local", provider.RecordTypeAAAA, "2001:db8::1") {
		t.Error("Missing AAAA record: ipv6host.local -> 2001:db8::1")
	}
	if !hasRecord("www.local", provider.RecordTypeCNAME, "server.local") {
		t.Error("Missing CNAME record: www.local -> server.local")
	}
}

func TestV6APIClient_Create(t *testing.T) {
	var createdPaths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			resp := map[string]any{
				"session": map[string]any{
					"valid":    true,
					"sid":      "test-sid",
					"validity": 300,
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if r.Method == http.MethodPut {
			createdPaths = append(createdPaths, r.URL.Path)
			w.WriteHeader(http.StatusCreated)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewV6APIClient(server.URL, "password", "")

	// Create A record
	err := client.Create(context.Background(), piholeRecord{
		Hostname: "test.local",
		Target:   "192.168.1.200",
		Type:     provider.RecordTypeA,
	})
	if err != nil {
		t.Fatalf("Create(A) error = %v", err)
	}

	// Create CNAME record
	err = client.Create(context.Background(), piholeRecord{
		Hostname: "alias.local",
		Target:   "test.local",
		Type:     provider.RecordTypeCNAME,
	})
	if err != nil {
		t.Fatalf("Create(CNAME) error = %v", err)
	}

	if len(createdPaths) != 2 {
		t.Errorf("Expected 2 PUT requests, got %d", len(createdPaths))
	}
}

func TestV6APIClient_Delete(t *testing.T) {
	var deletedPaths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth" {
			resp := map[string]any{
				"session": map[string]any{
					"valid":    true,
					"sid":      "test-sid",
					"validity": 300,
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if r.Method == http.MethodDelete {
			deletedPaths = append(deletedPaths, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewV6APIClient(server.URL, "password", "")

	// Delete A record
	err := client.Delete(context.Background(), piholeRecord{
		Hostname: "test.local",
		Target:   "192.168.1.200",
		Type:     provider.RecordTypeA,
	})
	if err != nil {
		t.Fatalf("Delete(A) error = %v", err)
	}

	// Delete CNAME record
	err = client.Delete(context.Background(), piholeRecord{
		Hostname: "alias.local",
		Target:   "test.local",
		Type:     provider.RecordTypeCNAME,
	})
	if err != nil {
		t.Fatalf("Delete(CNAME) error = %v", err)
	}

	if len(deletedPaths) != 2 {
		t.Errorf("Expected 2 DELETE requests, got %d", len(deletedPaths))
	}
}
