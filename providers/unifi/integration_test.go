//go:build integration

package unifi

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// TestIntegration_FullCRUD runs a full create/read/update/delete cycle against a live
// UniFi OS console or UniFi OS Server running Network 10.1 or later.
//
// To run: go test -tags=integration -run TestIntegration ./providers/unifi/ -v
//
// Expects:
//   - UNIFI_TEST_URL (default: https://192.168.1.1)
//   - UNIFI_TEST_API_KEY (required; the test is skipped when unset)
//   - UNIFI_TEST_SITE (default: default)
//   - UNIFI_TEST_TLS_SKIP_VERIFY (default: true; consoles use self-signed certificates)
func TestIntegration_FullCRUD(t *testing.T) {
	apiKey := os.Getenv("UNIFI_TEST_API_KEY")
	if apiKey == "" {
		t.Skip("UNIFI_TEST_API_KEY not set")
	}

	cfg := &Config{
		URL:    envOr("UNIFI_TEST_URL", "https://192.168.1.1"),
		APIKey: apiKey,
		Site:   envOr("UNIFI_TEST_SITE", DefaultSite),
		TTL:    300,
	}

	skipVerify := !strings.EqualFold(envOr("UNIFI_TEST_TLS_SKIP_VERIFY", "true"), "false")
	httpClient := httputil.NewClient(&httputil.ClientConfig{
		TLS: &httputil.TLSConfig{InsecureSkip: skipVerify},
	})

	p, err := New("integration-test", cfg, WithProviderHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	ctx := context.Background()

	// 1. Ping
	t.Run("Ping", func(t *testing.T) {
		if err := p.Ping(ctx); err != nil {
			t.Fatalf("Ping() failed: %v", err)
		}
	})

	// 2. Create records
	testRecords := []provider.Record{
		{Hostname: "inttest-a.dnsweaver.test", Type: provider.RecordTypeA, Target: "10.99.99.1"},
		{Hostname: "inttest-aaaa.dnsweaver.test", Type: provider.RecordTypeAAAA, Target: "2001:db8:99::1"},
		{Hostname: "inttest-cname.dnsweaver.test", Type: provider.RecordTypeCNAME, Target: "inttest-a.dnsweaver.test"},
		{Hostname: "_dnsweaver.inttest-a.dnsweaver.test", Type: provider.RecordTypeTXT, Target: "heritage=dnsweaver,instance=inttest"},
		{Hostname: "_inttest._tcp.dnsweaver.test", Type: provider.RecordTypeSRV, Target: "inttest-a.dnsweaver.test", SRV: &provider.SRVData{Priority: 10, Weight: 5, Port: 8080}},
	}

	// Remove every test record even if a step fails part way, so nothing is
	// left behind on the operator's console. Delete is a no-op for records
	// that were never created or were already removed.
	t.Cleanup(func() {
		for _, rec := range testRecords {
			if err := p.Delete(context.Background(), rec); err != nil {
				t.Logf("cleanup: Delete(%s %s) failed: %v", rec.Hostname, rec.Type, err)
			}
		}
	})

	t.Run("Create", func(t *testing.T) {
		for _, rec := range testRecords {
			if err := p.Create(ctx, rec); err != nil {
				t.Errorf("Create(%s %s) failed: %v", rec.Hostname, rec.Type, err)
			}
		}
	})

	// 3. Duplicate create is reported as a conflict
	t.Run("Create_Duplicate", func(t *testing.T) {
		err := p.Create(ctx, testRecords[0])
		if !errors.Is(err, provider.ErrConflict) {
			t.Errorf("Create() of an existing record = %v, want ErrConflict", err)
		}
	})

	// 4. List and verify
	t.Run("List", func(t *testing.T) {
		records, err := p.List(ctx)
		if err != nil {
			t.Fatalf("List() failed: %v", err)
		}

		found := map[string]bool{}
		for _, r := range records {
			found[r.Hostname+":"+string(r.Type)+":"+r.Target] = true
		}

		for _, expected := range testRecords {
			key := expected.Hostname + ":" + string(expected.Type) + ":" + expected.Target
			if !found[key] {
				t.Errorf("List() missing record: %s", key)
			}
		}
	})

	// 5. Update
	t.Run("Update", func(t *testing.T) {
		existing := testRecords[0]
		desired := provider.Record{
			Hostname: "inttest-a.dnsweaver.test",
			Type:     provider.RecordTypeA,
			Target:   "10.99.99.2",
		}

		if err := p.Update(ctx, existing, desired); err != nil {
			t.Errorf("Update() failed: %v", err)
		}

		records, err := p.List(ctx)
		if err != nil {
			t.Fatalf("List() after update failed: %v", err)
		}

		found := false
		for _, r := range records {
			if r.Hostname == "inttest-a.dnsweaver.test" && r.Target == "10.99.99.2" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Updated record not found in List()")
		}

		testRecords[0].Target = "10.99.99.2"
	})

	// 6. Delete all test records
	t.Run("Delete", func(t *testing.T) {
		for _, rec := range testRecords {
			if err := p.Delete(ctx, rec); err != nil {
				t.Errorf("Delete(%s %s) failed: %v", rec.Hostname, rec.Type, err)
			}
		}

		records, err := p.List(ctx)
		if err != nil {
			t.Fatalf("List() after delete failed: %v", err)
		}

		for _, r := range records {
			for _, expected := range testRecords {
				if r.Hostname == expected.Hostname && r.Type == expected.Type {
					t.Errorf("Record %s %s should have been deleted", r.Hostname, r.Type)
				}
			}
		}
	})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
