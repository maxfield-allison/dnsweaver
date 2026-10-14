package ovh

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/maxfield-allison/dnsweaver/internal/testutil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

func TestOVHOwnershipMigrationConformance(t *testing.T) {
	var mu sync.Mutex
	records := make(map[int64]ovhRecord)
	var nextID int64
	server := ovhMux(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path := "/domain/zone/example.com/record"
		switch {
		case strings.HasSuffix(r.URL.Path, "/refresh"):
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == path && r.Method == http.MethodPost:
			var request recordCreateRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			nextID++
			target := request.Target
			if request.FieldType == "TXT" {
				target = `"` + target + `"`
			}
			record := ovhRecord{ID: nextID, Zone: "example.com", SubDomain: request.SubDomain, FieldType: request.FieldType, Target: target, TTL: request.TTL}
			records[nextID] = record
			_ = json.NewEncoder(w).Encode(record)
		case r.URL.Path == path && r.Method == http.MethodGet:
			ids := []int64{}
			for id, record := range records {
				if kind := r.URL.Query().Get("fieldType"); kind != "" && kind != record.FieldType {
					continue
				}
				if name := r.URL.Query().Get("subDomain"); name != "" && name != record.SubDomain {
					continue
				}
				ids = append(ids, id)
			}
			_ = json.NewEncoder(w).Encode(ids)
		case strings.HasPrefix(r.URL.Path, path+"/"):
			id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, path+"/"), 10, 64)
			record, found := records[id]
			if !found {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if r.Method == http.MethodDelete {
				delete(records, id)
				w.WriteHeader(http.StatusOK)
				return
			}
			_ = json.NewEncoder(w).Encode(record)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer server.Close()
	testutil.RunOwnershipMigrationConformance(t, func(t *testing.T, owner, storage string) provider.Provider {
		factory := func(provider.FactoryConfig) (provider.Provider, error) {
			p := testProvider(t, server.URL)
			p.logger = testutil.QuietLogger()
			p.client.logger = p.logger
			return p, nil
		}
		return testutil.OwnershipRegistryProvider(t, owner, storage, "ovh", factory, map[string]string{"ZONE": "example.com"})
	})
}
