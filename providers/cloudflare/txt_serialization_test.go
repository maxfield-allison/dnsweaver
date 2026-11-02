package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

func TestProvider_CreateTXTSerializesPresentation(t *testing.T) {
	for _, tt := range []struct {
		name     string
		declared string
		content  string
	}{
		{"decimal escape", `"a\065b"`, "aAb"},
		{"leading literal quote", `"\"open"`, `"\"open"`},
		{"empty payload", `""`, `""`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(successProviderResponse(map[string]string{"id": "txt-record"}))
			}))
			defer server.Close()
			p := newTestProvider(t, server.URL)
			record := provider.Record{Hostname: "txt.example.com", Type: provider.RecordTypeTXT, Target: provider.DeclaredTXT(p, tt.declared), TTL: 300}
			if err := p.Create(context.Background(), record); err != nil {
				t.Fatal(err)
			}
			if content, present := body["content"]; !present || content != tt.content {
				t.Fatalf("serialized content = %q, present=%v; want %q", content, present, tt.content)
			}
		})
	}
}
