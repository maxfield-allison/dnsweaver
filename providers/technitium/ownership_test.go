package technitium

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/maxfield-allison/dnsweaver/internal/testutil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

func TestTechnitiumOwnershipMigrationConformance(t *testing.T) {
	var mu sync.Mutex
	records := []apiRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/api/zones/records/get" {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "response": zoneRecordsResponse{Records: records}})
			return
		}
		ttl, _ := strconv.Atoi(r.Form.Get("ttl"))
		priority, _ := strconv.Atoi(r.Form.Get("priority"))
		weight, _ := strconv.Atoi(r.Form.Get("weight"))
		port, _ := strconv.Atoi(r.Form.Get("port"))
		record := apiRecord{Name: r.Form.Get("domain"), Type: r.Form.Get("type"), TTL: ttl,
			RData: apiRData{IPAddress: r.Form.Get("ipAddress"), CName: r.Form.Get("cname"), Text: r.Form.Get("text"),
				Priority: priority, Weight: weight, Port: port, SrvTarget: r.Form.Get("target")}}
		switch r.URL.Path {
		case "/api/zones/records/add":
			records = append(records, record)
		case "/api/zones/records/delete":
			records = slices.DeleteFunc(records, func(existing apiRecord) bool {
				return existing.Name == record.Name && existing.Type == record.Type && existing.RData == record.RData
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()
	testutil.RunOwnershipMigrationConformance(t, func(t *testing.T, owner, storage string) provider.Provider {
		return testutil.OwnershipRegistryProvider(t, owner, storage, "technitium", Factory(),
			map[string]string{"URL": server.URL, "TOKEN": "test-token", "ZONE": "example.com"})
	})
}
