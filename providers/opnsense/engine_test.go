package opnsense

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUnboundEngine_Paths(t *testing.T) {
	e := unboundEngine{}
	if got := e.SearchPath(); got != "/api/unbound/settings/searchHostOverride" {
		t.Errorf("SearchPath = %q", got)
	}
	if got := e.AddPath(); got != "/api/unbound/settings/addHostOverride" {
		t.Errorf("AddPath = %q", got)
	}
	if got := e.DelPath("uu-id"); got != "/api/unbound/settings/delHostOverride/uu-id" {
		t.Errorf("DelPath = %q", got)
	}
	if got := e.ReconfigurePath(); got != "/api/unbound/service/reconfigure" {
		t.Errorf("ReconfigurePath = %q", got)
	}
}

func TestUnboundEngine_EncodeAddPayload(t *testing.T) {
	e := unboundEngine{}
	body, err := e.EncodeAddPayload(hostRecord{
		Hostname: "web", Domain: "example.com", Type: "A",
		Target: "10.0.0.5", Description: "dnsweaver:opns", Enabled: true,
	})
	if err != nil {
		t.Fatalf("EncodeAddPayload error: %v", err)
	}

	var decoded map[string]map[string]string
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	host := decoded["host"]
	if host == nil {
		t.Fatalf("payload missing host key: %s", body)
	}
	// Boolean-as-string: OPNsense form endpoints require "1"/"0".
	if host["enabled"] != "1" {
		t.Errorf("enabled = %q, want %q", host["enabled"], "1")
	}
	if host["hostname"] != "web" || host["domain"] != "example.com" {
		t.Errorf("hostname/domain wrong: %+v", host)
	}
	if host["rr"] != "A" || host["server"] != "10.0.0.5" {
		t.Errorf("rr/server wrong: %+v", host)
	}
	if host["description"] != "dnsweaver:opns" {
		t.Errorf("description wrong: %+v", host)
	}
}

func TestUnboundEngine_DecodeSearchResponse(t *testing.T) {
	e := unboundEngine{}
	// OPNsense occasionally returns "A (IPv4 Address)" style RR labels;
	// the decoder should normalize to the token.
	body := []byte(`{"rows":[
	  {"uuid":"u1","enabled":"1","hostname":"a","domain":"example.com","rr":"A (IPv4 Address)","server":"10.0.0.1","description":"dnsweaver:opns"},
	  {"uuid":"u2","enabled":"0","hostname":"b","domain":"example.com","rr":"AAAA","server":"2001:db8::1","description":"manual"}
	],"rowCount":2,"total":2,"current":1}`)
	recs, err := e.DecodeSearchResponse(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].UUID != "u1" || recs[0].Type != "A" || !recs[0].Enabled {
		t.Errorf("row 0 unexpected: %+v", recs[0])
	}
	if recs[1].Type != "AAAA" || recs[1].Enabled {
		t.Errorf("row 1 unexpected: %+v", recs[1])
	}
}

func TestDnsmasqEngine_Paths(t *testing.T) {
	e := dnsmasqEngine{}
	if got := e.SearchPath(); got != "/api/dnsmasq/settings/searchHost" {
		t.Errorf("SearchPath = %q", got)
	}
	if got := e.AddPath(); got != "/api/dnsmasq/settings/addHost" {
		t.Errorf("AddPath = %q", got)
	}
	if got := e.DelPath("u"); got != "/api/dnsmasq/settings/delHost/u" {
		t.Errorf("DelPath = %q", got)
	}
}

func TestDnsmasqEngine_EncodeAddPayload_A(t *testing.T) {
	e := dnsmasqEngine{}
	body, err := e.EncodeAddPayload(hostRecord{
		Hostname: "web", Domain: "example.com", Type: "A",
		Target: "10.0.0.5", Description: "dnsweaver:opns", Enabled: true,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var decoded map[string]map[string]string
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	host := decoded["host"]
	// Dnsmasq uses different field names than Unbound.
	if host["host"] != "web" || host["domain"] != "example.com" {
		t.Errorf("host/domain wrong: %+v", host)
	}
	if host["ip"] != "10.0.0.5" {
		t.Errorf("ip = %q, want %q", host["ip"], "10.0.0.5")
	}
	if host["descr"] != "dnsweaver:opns" {
		t.Errorf("descr wrong: %+v", host)
	}
	if _, hasRR := host["rr"]; hasRR {
		t.Errorf("dnsmasq payload should not carry rr field: %+v", host)
	}
}

func TestDnsmasqEngine_EncodeAddPayload_RejectsNonIPTarget(t *testing.T) {
	e := dnsmasqEngine{}
	_, err := e.EncodeAddPayload(hostRecord{
		Hostname: "web", Domain: "example.com", Type: "A",
		Target: "not-an-ip", Description: "dnsweaver:opns", Enabled: true,
	})
	if err == nil {
		t.Fatal("expected error for non-IP target")
	}
	if !strings.Contains(err.Error(), "IP target") {
		t.Errorf("error wording: %v", err)
	}
}

func TestDnsmasqEngine_DecodeSearchResponse_InfersType(t *testing.T) {
	e := dnsmasqEngine{}
	body := []byte(`{"rows":[
	  {"uuid":"u1","enabled":"1","host":"a","domain":"example.com","ip":"10.0.0.1","descr":"dnsweaver:opns"},
	  {"uuid":"u2","enabled":"1","host":"b","domain":"example.com","ip":"2001:db8::1","descr":"dnsweaver:opns"}
	]}`)
	recs, err := e.DecodeSearchResponse(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if recs[0].Type != "A" {
		t.Errorf("row 0 type = %q, want A", recs[0].Type)
	}
	if recs[1].Type != "AAAA" {
		t.Errorf("row 1 type = %q, want AAAA", recs[1].Type)
	}
}

func TestNewEngine_Dispatch(t *testing.T) {
	if newEngine(EngineUnbound).Name() != EngineUnbound {
		t.Error("unbound dispatch")
	}
	if newEngine(EngineDnsmasq).Name() != EngineDnsmasq {
		t.Error("dnsmasq dispatch")
	}
	// Unknown engines default to unbound so Validate can still catch
	// misconfiguration without the engine dispatcher panicking.
	if newEngine(Engine("bogus")).Name() != EngineUnbound {
		t.Error("default dispatch")
	}
}
