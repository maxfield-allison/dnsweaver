//go:build integration

package unifi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// TestIntegration_FullCRUD runs a full create/read/update/delete cycle against a live
// UniFi OS console or UniFi OS Server running Network 10.6 or later.
//
// Every record name carries a random per-run suffix, and cleanup deletes only
// the policies this run created, by id, so the test never touches records that
// already exist on the console.
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
	if err := p.Ping(ctx); err != nil {
		t.Fatalf("Ping() failed: %v", err)
	}

	suffix := randomSuffix(t)
	name := func(label string) string { return "inttest-" + suffix + "-" + label + ".dnsweaver.test" }

	a := provider.Record{Hostname: name("a"), Type: provider.RecordTypeA, Target: "10.99.99.1"}
	testRecords := []provider.Record{
		a,
		{Hostname: name("aaaa"), Type: provider.RecordTypeAAAA, Target: "2001:db8:99::1"},
		{Hostname: name("cname"), Type: provider.RecordTypeCNAME, Target: a.Hostname},
		{Hostname: "_inttest-" + suffix + "._tcp.dnsweaver.test", Type: provider.RecordTypeSRV, Target: a.Hostname, SRV: &provider.SRVData{Priority: 10, Weight: 5, Port: 8080}},
		// A real v3 per-member ownership marker: comma-delimited, so it
		// exercises TXT quoting and the console's length limit.
		provider.MemberOwnershipRecord(a.Hostname, 300, "inttest-"+suffix, a, nil),
		// A second, unrelated TXT value on the same name as the marker.
		{Hostname: provider.OwnershipRecordName(a.Hostname), Type: provider.RecordTypeTXT, Target: "note=sibling,run=" + suffix},
	}

	// created holds the id of every policy this run created. Cleanup deletes
	// exactly these, by id, even if a step fails part way.
	var mu sync.Mutex
	created := map[string]provider.Record{}
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for id, rec := range created {
			rec.ProviderID = id
			if err := p.Delete(context.Background(), rec); err != nil {
				t.Errorf("cleanup: Delete(%s %s, id %s) failed: %v", rec.Hostname, rec.Type, id, err)
			}
		}
	})

	// 2. Create records, recording the id of each one that succeeded.
	t.Run("Create", func(t *testing.T) {
		for _, rec := range testRecords {
			if err := p.Create(ctx, rec); err != nil {
				t.Errorf("Create(%s %s) failed: %v", rec.Hostname, rec.Type, err)
				continue
			}
			listed, ok := findRecord(t, p, rec)
			if !ok {
				t.Errorf("Create(%s %s) succeeded but the record is not listed", rec.Hostname, rec.Type)
				continue
			}
			mu.Lock()
			created[listed.ProviderID] = rec
			mu.Unlock()
		}
	})

	// 3. Created policies report the origin isManaged relies on.
	t.Run("Origin", func(t *testing.T) {
		siteID, err := p.resolveSiteID(ctx)
		if err != nil {
			t.Fatalf("resolveSiteID() failed: %v", err)
		}
		policies, err := p.client.ListDNSPolicies(ctx, siteID)
		if err != nil {
			t.Fatalf("ListDNSPolicies() failed: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		for _, pol := range policies {
			if _, ours := created[pol.ID]; !ours {
				continue
			}
			if pol.Metadata == nil || pol.Metadata.Origin != originUserDefined {
				t.Errorf("policy %s (%s %s) metadata = %+v, want origin %s", pol.ID, pol.Type, pol.Domain, pol.Metadata, originUserDefined)
			}
		}
	})

	// 4. Duplicate create is reported as a conflict and creates nothing.
	t.Run("Create_Duplicate", func(t *testing.T) {
		err := p.Create(ctx, testRecords[0])
		if !errors.Is(err, provider.ErrConflict) {
			t.Errorf("Create() of an existing record = %v, want ErrConflict", err)
		}
	})

	// 5. List returns every record exactly as written.
	t.Run("List", func(t *testing.T) {
		for _, expected := range testRecords {
			if _, ok := findRecord(t, p, expected); !ok {
				t.Errorf("List() missing record: %s %s %s", expected.Hostname, expected.Type, expected.Target)
			}
		}
	})

	// 6. Update in place keeps the policy id.
	t.Run("Update", func(t *testing.T) {
		existing, ok := findRecord(t, p, testRecords[0])
		if !ok {
			t.Fatal("record to update is not listed")
		}
		desired := testRecords[0]
		desired.Target = "10.99.99.2"

		if err := p.Update(ctx, existing, desired); err != nil {
			t.Fatalf("Update() failed: %v", err)
		}

		updated, ok := findRecord(t, p, desired)
		if !ok {
			t.Fatal("updated record not found in List()")
		}
		if updated.ProviderID != existing.ProviderID {
			t.Errorf("Update() changed the policy id from %s to %s", existing.ProviderID, updated.ProviderID)
		}

		mu.Lock()
		created[existing.ProviderID] = desired
		mu.Unlock()
		testRecords[0] = desired
	})

	// 7. Delete every record this run created.
	t.Run("Delete", func(t *testing.T) {
		mu.Lock()
		defer mu.Unlock()
		for id, rec := range created {
			rec.ProviderID = id
			if err := p.Delete(ctx, rec); err != nil {
				t.Errorf("Delete(%s %s) failed: %v", rec.Hostname, rec.Type, err)
				continue
			}
			delete(created, id)
		}

		for _, rec := range testRecords {
			if _, ok := findRecord(t, p, rec); ok {
				t.Errorf("record %s %s should have been deleted", rec.Hostname, rec.Type)
			}
		}
	})
}

// findRecord lists the provider and returns the record matching want by
// hostname, type and member value, with its ProviderID.
func findRecord(t *testing.T, p *Provider, want provider.Record) (provider.Record, bool) {
	t.Helper()
	records, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	for _, r := range records {
		if sameHostname(r.Hostname, want.Hostname) && provider.SameRecordMember(r, want) {
			return r, true
		}
	}
	return provider.Record{}, false
}

// randomSuffix returns a short random hex string that keeps each run's
// record names distinct from existing records and from other runs.
func randomSuffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generating run suffix: %v", err)
	}
	return hex.EncodeToString(b)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
