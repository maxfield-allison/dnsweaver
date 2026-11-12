package technitium

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

func TestCompanionDriftThroughProviderSnapshot(t *testing.T) {
	for _, recordType := range []provider.RecordType{provider.RecordTypeA, provider.RecordTypeAAAA} {
		t.Run(string(recordType), func(t *testing.T) {
			for _, tc := range []struct {
				name                                     string
				params                                   string
				priority                                 int
				oldTTL                                   int
				wantOldParams                            string
				target                                   string
				missing, disabled, duplicate, failUpdate bool
				wantUpdate, wantCreate                   bool
			}{
				{name: "changed ALPN", params: `"alpn|h2"`, priority: 1, target: ".", wantUpdate: true},
				{name: "object ALPN", params: `{"alpn":["h2"]}`, priority: 1, target: ".", wantUpdate: true},
				{name: "already current", params: `"alpn|h2,h3"`, priority: 1, target: "."},
				{name: "changed TTL only", params: `"alpn|h2,h3"`, priority: 1, target: ".", oldTTL: 60, wantOldParams: "alpn|h2,h3", wantUpdate: true},
				{name: "no parameters", params: `""`, priority: 1, target: "."},
				{name: "missing companion", missing: true, wantCreate: true},
				{name: "custom parameters", params: `"alpn|h2|port|8443"`, priority: 1, target: "."},
				{name: "numeric custom parameter", params: `{"alpn":"h2","port":8443}`, priority: 1, target: "."},
				{name: "custom priority", params: `"alpn|h2"`, priority: 2, target: "."},
				{name: "custom target", params: `"alpn|h2"`, priority: 1, target: "other.example.com"},
				{name: "disabled record", params: `"alpn|h2"`, priority: 1, target: ".", disabled: true},
				{name: "multiple HTTPS records", params: `"alpn|h2"`, priority: 1, target: ".", duplicate: true},
				{name: "failed update", params: `"alpn|h2"`, priority: 1, target: ".", wantUpdate: true, failUpdate: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					address := apiRecord{Name: "app.example.com", Type: string(recordType), TTL: 300, RData: apiRData{IPAddress: "192.0.2.10"}}
					if recordType == provider.RecordTypeAAAA {
						address.RData.IPAddress = "2001:db8::10"
					}
					records := []apiRecord{address}
					if !tc.missing {
						var params svcParamsValue
						if err := json.Unmarshal([]byte(tc.params), &params); err != nil {
							t.Fatal(err)
						}
						https := apiRecord{Name: "app.example.com", Type: "HTTPS", TTL: 300, Disabled: tc.disabled, RData: apiRData{SvcPriority: tc.priority, SvcTargetName: tc.target, SvcParams: params}}
						if tc.oldTTL != 0 {
							https.TTL = tc.oldTTL
						}
						records = append(records, https)
						if tc.duplicate {
							records = append(records, https)
						}
					}
					updates, creates := 0, 0
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
							wantOldParams := tc.wantOldParams
							if wantOldParams == "" {
								wantOldParams = "alpn|h2"
							}
							for key, want := range map[string]string{"type": "HTTPS", "svcParams": wantOldParams, "newSvcParams": "alpn|h2,h3", "svcPriority": "1", "newSvcPriority": "1", "svcTargetName": ".", "newSvcTargetName": ".", "ttl": "300"} {
								if got := r.Form.Get(key); got != want {
									t.Errorf("%s = %q, want %q", key, got, want)
								}
							}
							if tc.failUpdate {
								_, _ = w.Write([]byte(`{"status":"error","errorMessage":"denied"}`))
								return
							}
							records[1].RData.SvcParams = "alpn|h2,h3"
							records[1].TTL = 300
							_, _ = w.Write([]byte(`{"status":"ok"}`))
						case "/api/zones/records/add":
							creates++
							if err := r.ParseForm(); err != nil {
								t.Error(err)
							}
							if r.Form.Get("type") != "HTTPS" || r.Form.Get("svcParams") != "alpn|h2,h3" {
								t.Errorf("unexpected create: %v", r.Form)
							}
							records = append(records, apiRecord{Name: "app.example.com", Type: "HTTPS", TTL: 300, RData: apiRData{SvcPriority: 1, SvcTargetName: ".", SvcParams: "alpn|h2,h3"}})
							_, _ = w.Write([]byte(`{"status":"ok"}`))
						default:
							t.Errorf("unexpected mutation: %s", r.URL.Path)
							w.WriteHeader(http.StatusBadRequest)
						}
					}))
					defer server.Close()
					p, err := New("dns", &Config{URL: server.URL, Token: "test-token", Zone: "example.com", TTL: 300, AutoHTTPSRecords: true, AutoHTTPSALPN: "h2,h3"})
					if err != nil {
						t.Fatal(err)
					}
					listed, err := p.List(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					desired := provider.Record{Hostname: "app.example.com", Type: recordType, Target: address.RData.IPAddress, TTL: 300}
					wantDrift := tc.wantCreate || tc.wantUpdate
					if got := p.RecordNeedsUpdate(listed[0], desired); got != wantDrift {
						t.Fatalf("needs update = %v, want %v", got, wantDrift)
					}
					if wantDrift {
						err := p.Update(context.Background(), listed[0], desired)
						if (err != nil) != tc.failUpdate {
							t.Fatalf("Update error = %v", err)
						}
					}
					if (updates > 0) != tc.wantUpdate || (creates > 0) != tc.wantCreate {
						t.Fatalf("updates=%d creates=%d", updates, creates)
					}
					listed, err = p.List(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					if got := p.RecordNeedsUpdate(listed[0], desired); got != tc.failUpdate {
						t.Fatalf("subsequent drift=%v, want %v", got, tc.failUpdate)
					}
				})
			}
		})
	}
}

func TestCompanionDeletionUsesObservedALPN(t *testing.T) {
	for _, tc := range []struct {
		name, params string
		wantDeletes  int
	}{
		{name: "old ALPN", params: "alpn|h2", wantDeletes: 1},
		{name: "custom parameters", params: "alpn|h2|port|8443"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deletes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/zones/records/get":
					if err := json.NewEncoder(w).Encode(map[string]any{"status": "ok", "response": map[string]any{"records": []apiRecord{{Name: "app.example.com", Type: "HTTPS", TTL: 300, RData: apiRData{SvcPriority: 1, SvcTargetName: ".", SvcParams: svcParamsValue(tc.params)}}}}}); err != nil {
						t.Error(err)
					}
				case "/api/zones/records/delete":
					deletes++
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if got := r.Form.Get("svcParams"); got != "alpn|h2" {
						t.Errorf("deleted parameters = %q, want old ALPN", got)
					}
					_, _ = w.Write([]byte(`{"status":"ok"}`))
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
			}))
			defer server.Close()
			p, err := New("dns", &Config{URL: server.URL, Token: "test-token", Zone: "example.com", TTL: 300, AutoHTTPSRecords: true, AutoHTTPSALPN: "h2,h3"})
			if err != nil {
				t.Fatal(err)
			}
			if err := p.deleteCompanionHTTPS(context.Background(), "app.example.com", provider.RecordTypeA); err != nil {
				t.Fatal(err)
			}
			if deletes != tc.wantDeletes {
				t.Fatalf("deletes=%d, want %d", deletes, tc.wantDeletes)
			}
		})
	}
}
