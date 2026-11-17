package opnsense

import "testing"

func TestSearchRejectsFailureInsteadOfEmptyInventory(t *testing.T) {
	for _, e := range []engine{unboundEngine{}, dnsmasqEngine{}} {
		t.Run(string(e.Name()), func(t *testing.T) {
			for _, body := range []string{`{"result":"failed"}`, `{"status":"failed"}`, `{}`, `null`, `{"rows":null}`} {
				if records, err := e.DecodeSearchResponse([]byte(body)); err == nil {
					t.Errorf("accepted incomplete search %s as %d records", body, len(records))
				}
			}
			if records, err := e.DecodeSearchResponse([]byte(`{"rows":[]}`)); err != nil || len(records) != 0 {
				t.Errorf("valid empty search rejected: %v", err)
			}
		})
	}
}
