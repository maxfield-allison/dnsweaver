package reconciler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/providers/technitium"
)

// Exercise the real provider's snapshot/comparer/update path under the
// reconciler's ownership and dry-run gates. The address never changes.
func TestReconcileCompanionDriftRequiresParentAuthority(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		owned, adopt, dryRun, disabled bool
		wantUpdates                    int
	}{
		{name: "owned parent", owned: true, wantUpdates: 1},
		{name: "unowned parent"},
		{name: "explicit adoption", adopt: true, wantUpdates: 1},
		{name: "dry run", owned: true, dryRun: true},
		{name: "companions disabled", owned: true, disabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			address := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.10", TTL: 300}
			marker := provider.MemberOwnershipRecord(address.Hostname, 300, "", address, nil)
			alpn := "h2"
			updates := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if err := req.ParseForm(); err != nil {
					t.Error(err)
				}
				switch req.URL.Path {
				case "/api/zones/records/get":
					records := []map[string]any{
						{"name": address.Hostname, "type": "A", "ttl": 300, "rData": map[string]any{"ipAddress": address.Target}},
						{"name": address.Hostname, "type": "HTTPS", "ttl": 300, "rData": map[string]any{"svcPriority": 1, "svcTargetName": ".", "svcParams": map[string]string{"alpn": alpn}}},
					}
					if tc.owned {
						records = append(records, map[string]any{"name": marker.Hostname, "type": "TXT", "ttl": 300, "rData": map[string]string{"text": marker.Target}})
					}
					if err := json.NewEncoder(w).Encode(map[string]any{"status": "ok", "response": map[string]any{"records": records}}); err != nil {
						t.Error(err)
					}
				case "/api/zones/records/update":
					if req.Form.Get("type") != "HTTPS" || req.Form.Get("newSvcParams") != "alpn|h2,h3" {
						t.Errorf("unexpected update: %v", req.Form)
					}
					updates++
					alpn = "h2,h3"
					_, _ = w.Write([]byte(`{"status":"ok"}`))
				case "/api/zones/records/add":
					if req.Form.Get("type") != "TXT" || !tc.adopt {
						t.Errorf("unexpected create: %v", req.Form)
					}
					_, _ = w.Write([]byte(`{"status":"ok"}`))
				default:
					t.Errorf("unexpected API call: %s", req.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			backend, err := technitium.New("dns", &technitium.Config{URL: server.URL, Token: "test-token", Zone: "example.com", TTL: 300, AutoHTTPSRecords: !tc.disabled, AutoHTTPSALPN: "h2,h3"}, technitium.WithProviderLogger(quietLogger()))
			if err != nil {
				t.Fatal(err)
			}
			providers := provider.NewRegistry(quietLogger())
			providers.RegisterFactory("test-technitium", func(_ provider.FactoryConfig) (provider.Provider, error) { return backend, nil })
			if err := providers.CreateInstance(provider.ProviderInstanceConfig{Name: "dns", TypeName: "test-technitium", RecordType: provider.RecordTypeA, Target: address.Target, TTL: 300, Domains: []string{"*.example.com"}}); err != nil {
				t.Fatal(err)
			}
			cfg := DefaultConfig()
			cfg.AdoptExisting, cfg.DryRun = tc.adopt, tc.dryRun
			r := New(nil, source.NewRegistry(quietLogger()), providers, WithConfig(cfg), WithLogger(quietLogger()))
			compiled := r.compileDesiredRecordSets([]*source.Hostname{{Name: address.Hostname, Source: "test"}})
			cache := newRecordCache(context.Background(), providers, quietLogger())
			actions := r.reconcileDesiredSet(context.Background(), compiled.Sets[0], cache, nil)
			for _, action := range actions {
				if action.Status == StatusFailed {
					t.Fatalf("reconcile failed: %+v", actions)
				}
			}
			if updates != tc.wantUpdates {
				t.Fatalf("HTTPS updates=%d, want %d; actions=%+v", updates, tc.wantUpdates, actions)
			}
			// A fresh provider snapshot after successful reconciliation is stable.
			cache = newRecordCache(context.Background(), providers, quietLogger())
			r.reconcileDesiredSet(context.Background(), compiled.Sets[0], cache, nil)
			if updates != tc.wantUpdates {
				t.Fatalf("unchanged cycle repeated HTTPS writes: %d", updates)
			}
		})
	}
}
