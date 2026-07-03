package opnsense

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
)

// dnsmasqEngine implements engine for OPNsense's Dnsmasq resolver (24.7+).
//
// Endpoints:
//
//	POST /api/dnsmasq/settings/searchHost
//	POST /api/dnsmasq/settings/addHost
//	POST /api/dnsmasq/settings/delHost/{uuid}
//	POST /api/dnsmasq/service/reconfigure
//
// Add payload wraps the host in a top-level "host" key with dnsmasq's
// field names ("host", "ip", "descr" — not "hostname", "server",
// "description" like Unbound uses). Dnsmasq host entries do not carry an
// explicit record-type field: the resolver picks A or AAAA based on the
// IP address family.
type dnsmasqEngine struct{}

func (dnsmasqEngine) Name() Engine { return EngineDnsmasq }

func (dnsmasqEngine) SearchPath() string      { return "/api/dnsmasq/settings/searchHost" }
func (dnsmasqEngine) AddPath() string         { return "/api/dnsmasq/settings/addHost" }
func (dnsmasqEngine) ReconfigurePath() string { return "/api/dnsmasq/service/reconfigure" }

func (dnsmasqEngine) DelPath(uuid string) string {
	return "/api/dnsmasq/settings/delHost/" + uuid
}

type dnsmasqHostPayload struct {
	Host dnsmasqHostFields `json:"host"`
}

type dnsmasqHostFields struct {
	Enabled string `json:"enabled"`
	Host    string `json:"host"`
	Domain  string `json:"domain"`
	IP      string `json:"ip"`
	Descr   string `json:"descr"`
}

func (dnsmasqEngine) EncodeAddPayload(rec hostRecord) ([]byte, error) {
	enabled := "0"
	if rec.Enabled {
		enabled = "1"
	}
	// Dnsmasq infers the record type from the IP family, so we don't
	// serialize rec.Type. We do validate that Target parses as an IP so
	// we fail early instead of pushing garbage to OPNsense.
	if net.ParseIP(rec.Target) == nil {
		return nil, fmt.Errorf("dnsmasq engine requires an IP target, got %q", rec.Target)
	}
	return json.Marshal(dnsmasqHostPayload{
		Host: dnsmasqHostFields{
			Enabled: enabled,
			Host:    rec.Hostname,
			Domain:  rec.Domain,
			IP:      rec.Target,
			Descr:   rec.Description,
		},
	})
}

type dnsmasqSearchResponse struct {
	Rows []dnsmasqSearchRow `json:"rows"`
}

type dnsmasqSearchRow struct {
	UUID    string `json:"uuid"`
	Enabled string `json:"enabled"`
	Host    string `json:"host"`
	Domain  string `json:"domain"`
	IP      string `json:"ip"`
	Descr   string `json:"descr"`
}

func (dnsmasqEngine) DecodeSearchResponse(body []byte) ([]hostRecord, error) {
	var resp dnsmasqSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding dnsmasq search response: %w", err)
	}
	records := make([]hostRecord, 0, len(resp.Rows))
	for _, row := range resp.Rows {
		rrType := "A"
		if ip := net.ParseIP(strings.TrimSpace(row.IP)); ip != nil && ip.To4() == nil {
			rrType = "AAAA"
		}
		records = append(records, hostRecord{
			UUID:        row.UUID,
			Hostname:    row.Host,
			Domain:      row.Domain,
			Type:        rrType,
			Target:      row.IP,
			Description: row.Descr,
			Enabled:     row.Enabled == "1",
		})
	}
	return records, nil
}
