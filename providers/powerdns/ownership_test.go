package powerdns

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/maxfield-allison/dnsweaver/internal/testutil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

func TestPowerDNSOwnershipMigrationConformance(t *testing.T) {
	var mu sync.Mutex
	zone := zoneResponse{Name: "example.com.", RRsets: []rrset{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(zone)
		case http.MethodPatch:
			var request patchRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			for _, incoming := range request.RRsets {
				zone.RRsets = slices.DeleteFunc(zone.RRsets, func(existing rrset) bool {
					return existing.Name == incoming.Name && existing.Type == incoming.Type
				})
				if incoming.ChangeType == "REPLACE" {
					incoming.ChangeType = ""
					zone.RRsets = append(zone.RRsets, incoming)
				}
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	testutil.RunOwnershipMigrationConformance(t, func(t *testing.T, owner, storage string) provider.Provider {
		return testutil.OwnershipRegistryProvider(t, owner, storage, "powerdns", Factory(),
			map[string]string{"URL": server.URL, "API_KEY": "test-key", "ZONE": "example.com"})
	})
}
