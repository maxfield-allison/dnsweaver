package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

func TestClientOwnershipPaginationAndZoneIDResolution(t *testing.T) {
	for _, failedPage := range []bool{false, true} {
		t.Run(strconv.FormatBool(failedPage), func(t *testing.T) {
			pages := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/zones/zone-id" {
					_ = json.NewEncoder(w).Encode(successResponse(zoneResult{ID: "zone-id", Name: "example.com"}))
					return
				}
				pages++
				if r.URL.Query().Get("type") != "TXT" {
					t.Errorf("ownership reader requested type %q", r.URL.Query().Get("type"))
				}
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				if failedPage && page == 2 {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				var records []dnsRecord
				for i := 0; i < 2; i++ {
					records = append(records, dnsRecord{ID: fmt.Sprintf("%d-%d", page, i), Name: "txt.example.com", Type: "TXT", Content: fmt.Sprintf("record-%d-%d", page, i), TTL: 300})
				}
				body := successResponse(records)
				body["result_info"] = map[string]int{"page": page, "total_pages": 3}
				_ = json.NewEncoder(w).Encode(body)
			}))
			defer server.Close()
			p, err := New("cf", &Config{ZoneID: "zone-id", Token: "test-token", TTL: 300})
			if err != nil {
				t.Fatal(err)
			}
			p.client = NewClient("test-token", WithAPIEndpoint(server.URL))
			zone, err := p.OwnershipZone(context.Background())
			if err != nil || zone != "example.com" {
				t.Fatalf("ID-only zone resolution: %q %v", zone, err)
			}
			records, err := p.ListOwnershipState(context.Background())
			if failedPage {
				if err == nil || records != nil {
					t.Fatal("failed second page yielded usable partial ownership")
				}
				return
			}
			if err != nil || len(records) != 6 || pages != 3 {
				t.Fatalf("listing records=%d pages=%d err=%v", len(records), pages, err)
			}
		})
	}
}

func TestCloudflareCompactTXTQuotedWireStrings(t *testing.T) {
	value := "dnsweaver-page=v1:" + strings.Repeat("a", 700)
	quoted := strconv.Quote(value[:255]) + " " + strconv.Quote(value[255:510]) + " " + strconv.Quote(value[510:])
	decoded, err := compactTXTContent("_dw-p.owner.digest.example.com", quoted)
	if err != nil || decoded != value {
		t.Fatal("compact TXT wire chunks did not concatenate", err)
	}
	user := `"user text" "other text"`
	if decoded, err := compactTXTContent("app.example.com", user); err != nil || decoded != user {
		t.Fatal("changed ordinary user TXT")
	}
	if _, err := compactTXTContent(provider.OwnershipManifestName("example.com"), `"broken`); err == nil {
		t.Fatal("accepted malformed quoted state")
	}
	member := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.1"}
	legacy := provider.MakeMemberOwnershipValue("a", member, map[string]string{"note": "two words"})
	if decoded, err := compactTXTContent(provider.OwnershipRecordName(member.Hostname), strconv.Quote(legacy)); err != nil || decoded != legacy {
		t.Fatal("quoted legacy ownership did not preserve metadata", err)
	}
}
