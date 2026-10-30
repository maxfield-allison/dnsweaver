package reconciler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
	"github.com/maxfield-allison/dnsweaver/providers/technitium"
	dnsweaversource "github.com/maxfield-allison/dnsweaver/sources/dnsweaver"
)

// fakeTechnitiumTXT is a local stand-in for the Technitium records API that
// stores TXT text verbatim, the way Technitium does.
type fakeTechnitiumTXT struct {
	mu      sync.Mutex
	records []fakeTechnitiumRecord
}

type fakeTechnitiumRecord struct {
	Name  string            `json:"name"`
	Type  string            `json:"type"`
	TTL   int               `json:"ttl"`
	RData map[string]string `json:"rData"`
}

func (f *fakeTechnitiumTXT) addTXT(name, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, fakeTechnitiumRecord{Name: name, Type: "TXT", TTL: 300, RData: map[string]string{"text": text}})
}

func (f *fakeTechnitiumTXT) texts(name string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var texts []string
	for _, record := range f.records {
		if record.Type == "TXT" && record.Name == name {
			texts = append(texts, record.RData["text"])
		}
	}
	return texts
}

func (f *fakeTechnitiumTXT) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		name, recordType, text := r.Form.Get("domain"), r.Form.Get("type"), r.Form.Get("text")
		switch r.URL.Path {
		case "/api/zones/records/get":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "response": map[string]any{"records": f.records}})
			return
		case "/api/zones/records/add":
			if recordType != "TXT" {
				t.Errorf("unexpected add of %s %s", recordType, name)
			}
			f.records = append(f.records, fakeTechnitiumRecord{Name: name, Type: recordType, TTL: 300, RData: map[string]string{"text": text}})
		case "/api/zones/records/delete":
			f.records = slices.DeleteFunc(f.records, func(record fakeTechnitiumRecord) bool {
				return record.Name == name && record.Type == recordType && record.RData["text"] == text
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
}

func TestReconcile_TechnitiumLegacyTXTMarkerKeepsItsValue(t *testing.T) {
	// A release before this one declared the data "hello" with its quotes and
	// marked it; a manual record holds plain hello at the same name. Technitium
	// stores TXT verbatim, so the marker owns only the quoted data: upgrading
	// must neither claim the manual record nor retire the owned one while it
	// is still declared.
	const host = "mail.example.com"
	plain := provider.Record{Type: provider.RecordTypeTXT, Target: "hello"}
	tests := []struct {
		declared      string
		afterUpgrade  []string
		afterWithdraw []string
	}{
		{`"hello"`, []string{`"hello"`, "hello"}, []string{"hello"}},
		{"hello", []string{"hello"}, []string{"hello"}},
	}
	for _, tt := range tests {
		t.Run(tt.declared, func(t *testing.T) {
			api := &fakeTechnitiumTXT{}
			quoted := provider.Record{Hostname: host, Type: provider.RecordTypeTXT, Target: `"hello"`}
			api.addTXT(host, quoted.Target)
			api.addTXT(host, plain.Target)
			marker := provider.MemberOwnershipRecord(host, 300, "", quoted, nil)
			api.addTXT(marker.Hostname, marker.Target)
			r, lister := newTechnitiumTXTFixture(t, api)
			lister.AddWorkload("app", map[string]string{
				"dnsweaver.records.txt.hostname": host,
				"dnsweaver.records.txt.type":     "TXT",
				"dnsweaver.records.txt.target":   tt.declared,
			})

			if _, err := r.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if got := api.texts(host); !slices.Equal(got, tt.afterUpgrade) {
				t.Fatalf("TXT after upgrade = %q, want %q", got, tt.afterUpgrade)
			}
			for _, text := range api.texts(marker.Hostname) {
				if provider.MatchesMemberOwnership(text, "", plain) {
					t.Fatalf("manual hello was claimed by marker %q", text)
				}
			}

			lister.workloads = nil
			if _, err := r.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile after withdrawal: %v", err)
			}
			if got := api.texts(host); !slices.Equal(got, tt.afterWithdraw) {
				t.Fatalf("TXT after withdrawal = %q, want %q", got, tt.afterWithdraw)
			}
		})
	}
}

func newTechnitiumTXTFixture(t *testing.T, api *fakeTechnitiumTXT) (*Reconciler, *testMockWorkloadLister) {
	t.Helper()
	server := httptest.NewServer(api.handler(t))
	t.Cleanup(server.Close)
	logger := quietLogger()
	dns, err := technitium.New("internal", &technitium.Config{URL: server.URL, Token: "test-token", Zone: "example.com", TTL: 300},
		technitium.WithProviderLogger(logger))
	if err != nil {
		t.Fatalf("technitium.New: %v", err)
	}
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("technitium-fake", func(_ provider.FactoryConfig) (provider.Provider, error) { return dns, nil })
	if err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name: "internal", TypeName: "technitium-fake", RecordType: provider.RecordTypeA,
		Target: "192.0.2.10", TTL: 300, Domains: []string{"*.example.com"},
	}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sources := source.NewRegistry(logger)
	sources.Register(dnsweaversource.New(dnsweaversource.WithLogger(logger)))
	lister := newTestMockWorkloadLister(workload.PlatformDocker)
	return New([]workload.Lister{lister}, sources, providers, WithConfig(DefaultConfig()), WithLogger(logger)), lister
}
