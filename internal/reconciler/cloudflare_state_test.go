package reconciler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
	"github.com/maxfield-allison/dnsweaver/providers/cloudflare"
	dnsweaversource "github.com/maxfield-allison/dnsweaver/sources/dnsweaver"
)

// =============================================================================
// Issue #170 against the real Cloudflare provider
//
// The Cloudflare provider is wired to an in-memory stand-in for the DNS
// records API so the reconciler, the provider's proxied resolution and its
// List/Update round trip are all exercised together. The fake applies the one
// Cloudflare rule that matters for churn: a proxied record is stored and
// listed with TTL 1 ("automatic"), whatever TTL was sent.
// =============================================================================

type fakeCFRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

type fakeCloudflareAPI struct {
	mu      sync.Mutex
	nextID  int
	records []fakeCFRecord
	patches []fakeCFRecord // bodies of PATCH requests, in order
	posts   int
	deletes int
}

func (f *fakeCloudflareAPI) add(recordType, name, content string, ttl int, proxied bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	if proxied {
		ttl = 1
	}
	f.records = append(f.records, fakeCFRecord{
		ID: "rec-" + strconv.Itoa(f.nextID), Type: recordType, Name: name, Content: content, TTL: ttl, Proxied: proxied,
	})
}

func (f *fakeCloudflareAPI) find(recordType, name string) (fakeCFRecord, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.records {
		if r.Type == recordType && r.Name == name {
			return r, true
		}
	}
	return fakeCFRecord{}, false
}

func (f *fakeCloudflareAPI) patchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.patches)
}

func writeCFResult(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true, "errors": []any{}, "messages": []any{}, "result": result,
	})
}

func (f *fakeCloudflareAPI) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /zones/{zone}/dns_records", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f.mu.Lock()
		defer f.mu.Unlock()
		out := make([]fakeCFRecord, 0)
		for _, rec := range f.records {
			if t := q.Get("type"); t != "" && rec.Type != t {
				continue
			}
			if n := q.Get("name"); n != "" && !strings.EqualFold(rec.Name, n) {
				continue
			}
			out = append(out, rec)
		}
		writeCFResult(w, out)
	})

	mux.HandleFunc("POST /zones/{zone}/dns_records", func(w http.ResponseWriter, r *http.Request) {
		var body fakeCFRecord
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.posts++
		f.mu.Unlock()
		f.add(body.Type, body.Name, body.Content, body.TTL, body.Proxied)
		rec, _ := f.find(body.Type, body.Name)
		writeCFResult(w, rec)
	})

	mux.HandleFunc("PATCH /zones/{zone}/dns_records/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body fakeCFRecord
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.patches = append(f.patches, body)
		id := r.PathValue("id")
		for i := range f.records {
			if f.records[i].ID != id {
				continue
			}
			f.records[i].Content = body.Content
			f.records[i].Proxied = body.Proxied
			f.records[i].TTL = body.TTL
			if body.Proxied {
				f.records[i].TTL = 1
			}
			writeCFResult(w, f.records[i])
			return
		}
		w.WriteHeader(http.StatusNotFound)
		writeCFResult(w, nil)
	})

	mux.HandleFunc("DELETE /zones/{zone}/dns_records/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.deletes++
		id := r.PathValue("id")
		kept := f.records[:0]
		for _, rec := range f.records {
			if rec.ID != id {
				kept = append(kept, rec)
			}
		}
		f.records = kept
		writeCFResult(w, map[string]string{"id": id})
	})

	return mux
}

// rewriteTransport sends every request to base instead of the Cloudflare API,
// dropping the /client/v4 prefix the provider's client adds.
type rewriteTransport struct {
	base *url.URL
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = t.base.Scheme
	req.URL.Host = t.base.Host
	req.URL.Path = strings.TrimPrefix(req.URL.Path, "/client/v4")
	return http.DefaultTransport.RoundTrip(req)
}

// newCloudflareStateFixture returns a reconciler whose single instance is a real
// Cloudflare provider (PROXIED as given, TTL 300, A records at target) backed
// by the fake API, plus the workload lister feeding it.
func newCloudflareStateFixture(t *testing.T, api *fakeCloudflareAPI, proxiedDefault bool, target string) (*Reconciler, *testMockWorkloadLister) {
	t.Helper()

	server := httptest.NewServer(api.handler())
	t.Cleanup(server.Close)
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	logger := quietLogger()
	cf, err := cloudflare.New("external",
		&cloudflare.Config{Token: "test-token", ZoneID: "zone-123", TTL: 300, Proxied: proxiedDefault},
		cloudflare.WithProviderHTTPClient(&http.Client{Transport: rewriteTransport{base: base}}),
		cloudflare.WithProviderLogger(logger),
	)
	if err != nil {
		t.Fatalf("cloudflare.New: %v", err)
	}

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("cloudflare-fake", func(_ provider.FactoryConfig) (provider.Provider, error) {
		return cf, nil
	})
	if err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "external",
		TypeName:   "cloudflare-fake",
		RecordType: provider.RecordTypeA,
		Target:     target,
		TTL:        300,
		Domains:    []string{"*.example.com"},
	}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	sources := source.NewRegistry(logger)
	sources.Register(dnsweaversource.New(dnsweaversource.WithLogger(logger)))
	lister := newTestMockWorkloadLister(workload.PlatformDocker)

	r := New([]workload.Lister{lister}, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)
	return r, lister
}

func TestReconcile_CloudflareProxiedLabelUpdatesExistingRecord(t *testing.T) {
	// The reporter's scenario from #170: PROXIED=true, records created
	// proxied, then dnsweaver.proxied=false added to the container.
	const target = "203.0.113.10"
	hosts := []string{"mail.example.com", "autodiscover.example.com"}

	api := &fakeCloudflareAPI{}
	for _, h := range hosts {
		api.add("A", h, target, 300, true)
		api.add("TXT", provider.OwnershipRecordName(h), provider.OwnershipValue, 300, false)
	}

	r, lister := newCloudflareStateFixture(t, api, true, target)
	lister.AddWorkload("mail", map[string]string{
		"dnsweaver.hostnames": strings.Join(hosts, ","),
		"dnsweaver.proxied":   "false",
	})

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := len(result.Updated()); got != len(hosts) {
		t.Errorf("first pass: updated = %d, want %d", got, len(hosts))
	}
	if got := api.patchCount(); got != len(hosts) {
		t.Errorf("first pass: PATCH requests = %d, want %d", got, len(hosts))
	}
	for _, h := range hosts {
		rec, ok := api.find("A", h)
		if !ok {
			t.Fatalf("record for %s disappeared", h)
		}
		if rec.Proxied {
			t.Errorf("%s is still proxied after the label flip", h)
		}
		if rec.TTL != 300 {
			t.Errorf("%s TTL = %d after unproxying, want 300", h, rec.TTL)
		}
	}

	result, err = r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := len(result.Updated()); got != 0 {
		t.Errorf("second pass: updated = %d, want 0", got)
	}
	if got := api.patchCount(); got != len(hosts) {
		t.Errorf("second pass issued %d extra PATCH requests", got-len(hosts))
	}
	if api.posts != 0 || api.deletes != 0 {
		t.Errorf("expected no creates or deletes, got %d POST and %d DELETE", api.posts, api.deletes)
	}
}

func TestReconcile_CloudflareProxiedSteadyStateIsNotChurn(t *testing.T) {
	// A proxied record lists with TTL 1 while the instance is configured with
	// TTL 300. Matching proxied state must not produce a write, pass after pass.
	const target = "203.0.113.10"
	const host = "app.example.com"

	tests := []struct {
		name           string
		proxiedDefault bool
		recordProxied  bool
		labels         map[string]string
	}{
		{
			name:           "default true, record proxied, no label",
			proxiedDefault: true,
			recordProxied:  true,
			labels:         map[string]string{"dnsweaver.hostname": host},
		},
		{
			name:           "default true, record DNS-only, label false",
			proxiedDefault: true,
			recordProxied:  false,
			labels:         map[string]string{"dnsweaver.hostname": host, "dnsweaver.proxied": "false"},
		},
		{
			name:           "default false, record proxied, label true",
			proxiedDefault: false,
			recordProxied:  true,
			labels:         map[string]string{"dnsweaver.hostname": host, "dnsweaver.proxied": "true"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeCloudflareAPI{}
			api.add("A", host, target, 300, tt.recordProxied)
			api.add("TXT", provider.OwnershipRecordName(host), provider.OwnershipValue, 300, false)

			r, lister := newCloudflareStateFixture(t, api, tt.proxiedDefault, target)
			lister.AddWorkload("app", tt.labels)

			for pass := 1; pass <= 2; pass++ {
				result, err := r.Reconcile(context.Background())
				if err != nil {
					t.Fatalf("pass %d: Reconcile: %v", pass, err)
				}
				if got := len(result.Updated()); got != 0 {
					t.Errorf("pass %d: updated = %d, want 0", pass, got)
				}
			}
			if got := api.patchCount(); got != 0 {
				t.Errorf("PATCH requests = %d, want 0", got)
			}
			if api.posts != 0 || api.deletes != 0 {
				t.Errorf("expected no creates or deletes, got %d POST and %d DELETE", api.posts, api.deletes)
			}
		})
	}
}

func TestReconcile_CloudflareProxiedDefaultFlipUpdatesExistingRecord(t *testing.T) {
	// Operator changes DNSWEAVER_EXTERNAL_PROXIED from true to false with no
	// per-record label: every existing proxied record is updated once.
	const target = "203.0.113.10"
	const host = "app.example.com"

	api := &fakeCloudflareAPI{}
	api.add("A", host, target, 300, true)
	api.add("TXT", provider.OwnershipRecordName(host), provider.OwnershipValue, 300, false)

	r, lister := newCloudflareStateFixture(t, api, false, target)
	lister.AddWorkload("app", map[string]string{"dnsweaver.hostname": host})

	for pass, wantUpdates := 1, 1; pass <= 2; pass, wantUpdates = pass+1, 0 {
		result, err := r.Reconcile(context.Background())
		if err != nil {
			t.Fatalf("pass %d: Reconcile: %v", pass, err)
		}
		if got := len(result.Updated()); got != wantUpdates {
			t.Errorf("pass %d: updated = %d, want %d", pass, got, wantUpdates)
		}
	}
	if got := api.patchCount(); got != 1 {
		t.Errorf("PATCH requests = %d, want 1", got)
	}
	rec, _ := api.find("A", host)
	if rec.Proxied || rec.TTL != 300 {
		t.Errorf("record after default flip = proxied %v TTL %d, want DNS-only TTL 300", rec.Proxied, rec.TTL)
	}
}

func TestReconcile_CloudflareNonRoutableTargetIsNotChurn(t *testing.T) {
	// PROXIED=true with a private target: the provider demotes the record to
	// DNS-only on every write, and the comparison must apply the same rule or
	// it would request a proxied record each pass and never settle.
	const target = "10.0.0.5"
	const host = "app.example.com"

	api := &fakeCloudflareAPI{}
	api.add("A", host, target, 300, false)
	api.add("TXT", provider.OwnershipRecordName(host), provider.OwnershipValue, 300, false)

	r, lister := newCloudflareStateFixture(t, api, true, target)
	lister.AddWorkload("app", map[string]string{"dnsweaver.hostname": host})

	for pass := 1; pass <= 2; pass++ {
		result, err := r.Reconcile(context.Background())
		if err != nil {
			t.Fatalf("pass %d: Reconcile: %v", pass, err)
		}
		if got := len(result.Updated()); got != 0 {
			t.Errorf("pass %d: updated = %d, want 0", pass, got)
		}
	}
	if got := api.patchCount(); got != 0 {
		t.Errorf("PATCH requests = %d, want 0", got)
	}
}
