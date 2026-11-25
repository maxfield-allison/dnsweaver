package technitium

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// TestTTLDriftThroughProviderSnapshot verifies that a changed TTL on an
// existing A/AAAA/CNAME record is reported as drift and applied in place via
// the native update API, and that the record is current afterwards.
func TestTTLDriftThroughProviderSnapshot(t *testing.T) {
	tests := []struct {
		name       string
		apiType    string
		recordType provider.RecordType
		rdata      apiRData
		target     string
		listedTTL  int
		desiredTTL int
		wantDrift  bool
	}{
		{name: "CNAME lowered", apiType: "CNAME", recordType: provider.RecordTypeCNAME,
			rdata: apiRData{CName: "proxy.example.com"}, target: "proxy.example.com", listedTTL: 300, desiredTTL: 60, wantDrift: true},
		{name: "CNAME raised", apiType: "CNAME", recordType: provider.RecordTypeCNAME,
			rdata: apiRData{CName: "proxy.example.com"}, target: "proxy.example.com", listedTTL: 60, desiredTTL: 3600, wantDrift: true},
		{name: "CNAME current", apiType: "CNAME", recordType: provider.RecordTypeCNAME,
			rdata: apiRData{CName: "proxy.example.com"}, target: "proxy.example.com", listedTTL: 60, desiredTTL: 60},
		{name: "CNAME falls back to provider TTL", apiType: "CNAME", recordType: provider.RecordTypeCNAME,
			rdata: apiRData{CName: "proxy.example.com"}, target: "proxy.example.com", listedTTL: 300, desiredTTL: 0},
		{name: "A lowered", apiType: "A", recordType: provider.RecordTypeA,
			rdata: apiRData{IPAddress: "192.0.2.10"}, target: "192.0.2.10", listedTTL: 300, desiredTTL: 60, wantDrift: true},
		{name: "AAAA lowered", apiType: "AAAA", recordType: provider.RecordTypeAAAA,
			rdata: apiRData{IPAddress: "2001:db8::10"}, target: "2001:db8::10", listedTTL: 300, desiredTTL: 60, wantDrift: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			records := []apiRecord{{Name: "app.example.com", Type: tc.apiType, TTL: tc.listedTTL, RData: tc.rdata}}
			updates := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/zones/records/get":
					if err := json.NewEncoder(w).Encode(map[string]any{"status": "ok", "response": map[string]any{"records": records}}); err != nil {
						t.Error(err)
					}
				case "/api/zones/records/update":
					updates++
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if got := r.Form.Get("type"); got != tc.apiType {
						t.Errorf("update type = %q, want %q", got, tc.apiType)
					}
					if got, want := r.Form.Get("ttl"), strconv.Itoa(tc.desiredTTL); got != want {
						t.Errorf("update ttl = %q, want %q", got, want)
					}
					records[0].TTL = tc.desiredTTL
					_, _ = w.Write([]byte(`{"status":"ok"}`))
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()

			// Companion HTTPS records are disabled to isolate TTL drift.
			p, err := New("dns", &Config{URL: server.URL, Token: "test-token", Zone: "example.com", TTL: 300})
			if err != nil {
				t.Fatal(err)
			}
			listed, err := p.List(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			desired := provider.Record{Hostname: "app.example.com", Type: tc.recordType, Target: tc.target, TTL: tc.desiredTTL}

			if got := p.RecordNeedsUpdate(listed[0], desired); got != tc.wantDrift {
				t.Fatalf("RecordNeedsUpdate = %v, want %v", got, tc.wantDrift)
			}
			if !tc.wantDrift {
				return
			}
			if err := p.Update(context.Background(), listed[0], desired); err != nil {
				t.Fatalf("Update: %v", err)
			}
			if updates != 1 {
				t.Fatalf("native updates = %d, want 1", updates)
			}
			listed, err = p.List(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if p.RecordNeedsUpdate(listed[0], desired) {
				t.Fatal("record still reported as drifted after update")
			}
		})
	}
}

func TestTTLDriftIgnoresUnknownTTLAndOtherTypes(t *testing.T) {
	p := &Provider{ttl: 300}
	cases := []struct {
		name     string
		existing provider.Record
		desired  provider.Record
	}{
		{name: "listed TTL unknown", existing: provider.Record{Type: provider.RecordTypeCNAME, TTL: 0}, desired: provider.Record{Type: provider.RecordTypeCNAME, TTL: 60}},
		{name: "TXT is not compared", existing: provider.Record{Type: provider.RecordTypeTXT, TTL: 300}, desired: provider.Record{Type: provider.RecordTypeTXT, TTL: 60}},
		{name: "SRV is not compared", existing: provider.Record{Type: provider.RecordTypeSRV, TTL: 300}, desired: provider.Record{Type: provider.RecordTypeSRV, TTL: 60}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if p.RecordNeedsUpdate(tc.existing, tc.desired) {
				t.Fatal("unexpected drift")
			}
		})
	}
}
