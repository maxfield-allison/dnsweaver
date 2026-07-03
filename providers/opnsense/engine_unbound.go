package opnsense

import (
	"encoding/json"
	"fmt"
	"strings"
)

// unboundEngine implements engine for OPNsense's Unbound resolver.
//
// Endpoints (as of OPNsense 24.x):
//
//	POST /api/unbound/settings/searchHostOverride
//	POST /api/unbound/settings/addHostOverride
//	POST /api/unbound/settings/delHostOverride/{uuid}
//	POST /api/unbound/service/reconfigure
//
// Add payload wraps the host in a top-level "host" key:
//
//	{"host": {"enabled":"1","hostname":"web","domain":"example.com",
//	          "rr":"A","server":"10.0.0.1","description":"dnsweaver:foo"}}
type unboundEngine struct{}

func (unboundEngine) Name() Engine { return EngineUnbound }

func (unboundEngine) SearchPath() string      { return "/api/unbound/settings/searchHostOverride" }
func (unboundEngine) AddPath() string         { return "/api/unbound/settings/addHostOverride" }
func (unboundEngine) ReconfigurePath() string { return "/api/unbound/service/reconfigure" }

func (unboundEngine) DelPath(uuid string) string {
	return "/api/unbound/settings/delHostOverride/" + uuid
}

// unboundHostPayload is the JSON body shape OPNsense's Unbound API expects.
// All fields are strings — OPNsense's API is stringly-typed for form-style
// endpoints, including the "enabled" boolean which is "0" or "1".
type unboundHostPayload struct {
	Host unboundHostFields `json:"host"`
}

type unboundHostFields struct {
	Enabled     string `json:"enabled"`
	Hostname    string `json:"hostname"`
	Domain      string `json:"domain"`
	RR          string `json:"rr"`
	Server      string `json:"server"`
	Description string `json:"description"`
}

func (unboundEngine) EncodeAddPayload(rec hostRecord) ([]byte, error) {
	enabled := "0"
	if rec.Enabled {
		enabled = "1"
	}
	return json.Marshal(unboundHostPayload{
		Host: unboundHostFields{
			Enabled:     enabled,
			Hostname:    rec.Hostname,
			Domain:      rec.Domain,
			RR:          rec.Type,
			Server:      rec.Target,
			Description: rec.Description,
		},
	})
}

// unboundSearchResponse mirrors the OPNsense grid response shape.
type unboundSearchResponse struct {
	Rows []unboundSearchRow `json:"rows"`
}

type unboundSearchRow struct {
	UUID        string `json:"uuid"`
	Enabled     string `json:"enabled"`
	Hostname    string `json:"hostname"`
	Domain      string `json:"domain"`
	RR          string `json:"rr"`
	Server      string `json:"server"`
	Description string `json:"description"`
}

func (unboundEngine) DecodeSearchResponse(body []byte) ([]hostRecord, error) {
	var resp unboundSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding unbound search response: %w", err)
	}
	records := make([]hostRecord, 0, len(resp.Rows))
	for _, row := range resp.Rows {
		// OPNsense sometimes returns the RR name with a trailing description,
		// e.g. "A (IPv4 Address)". Trim to the token to normalize.
		rrType := strings.TrimSpace(row.RR)
		if idx := strings.Index(rrType, " "); idx >= 0 {
			rrType = rrType[:idx]
		}
		records = append(records, hostRecord{
			UUID:        row.UUID,
			Hostname:    row.Hostname,
			Domain:      row.Domain,
			Type:        strings.ToUpper(rrType),
			Target:      row.Server,
			Description: row.Description,
			Enabled:     row.Enabled == "1",
		})
	}
	return records, nil
}
