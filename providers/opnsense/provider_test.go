package opnsense

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// fakeOPNsense simulates the OPNsense host-override endpoints for a given
// engine. It supports search / add / delete / reconfigure and records every
// request so tests can assert on wire behavior. Auth is validated against
// the fixed key/secret pair below.
type fakeOPNsense struct {
	t         *testing.T
	engine    Engine
	apiKey    string
	apiSecret string

	mu           sync.Mutex
	rows         []map[string]string // simulated database rows
	nextUUID     int
	reloadCount  int
	requestPaths []string
	requestBody  map[string][]byte
	// override lets a test inject a canned response for a specific path.
	override map[string]func(w http.ResponseWriter, r *http.Request)
}

func newFakeOPNsense(t *testing.T, engine Engine) *fakeOPNsense {
	t.Helper()
	return &fakeOPNsense{
		t:           t,
		engine:      engine,
		apiKey:      "test-key",
		apiSecret:   "test-secret",
		rows:        []map[string]string{},
		requestBody: map[string][]byte{},
		override:    map[string]func(w http.ResponseWriter, r *http.Request){},
	}
}

func (f *fakeOPNsense) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != f.apiKey || pass != f.apiSecret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)

		f.mu.Lock()
		f.requestPaths = append(f.requestPaths, r.URL.Path)
		f.requestBody[r.URL.Path] = body
		override := f.override[r.URL.Path]
		f.mu.Unlock()

		if override != nil {
			// Re-attach body since override may want to inspect it.
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			override(w, r)
			return
		}

		switch {
		case f.isSearchPath(r.URL.Path):
			f.handleSearch(w)
		case f.isAddPath(r.URL.Path):
			f.handleAdd(w, body)
		case f.isDelPathPrefix(r.URL.Path):
			uuid := strings.TrimPrefix(r.URL.Path, f.delPathPrefix())
			f.handleDel(w, uuid)
		case r.URL.Path == f.reconfigurePath():
			f.mu.Lock()
			f.reloadCount++
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	})
}

func (f *fakeOPNsense) isSearchPath(path string) bool {
	switch f.engine {
	case EngineUnbound:
		return path == "/api/unbound/settings/searchHostOverride"
	default:
		return path == "/api/dnsmasq/settings/searchHost"
	}
}
func (f *fakeOPNsense) isAddPath(path string) bool {
	switch f.engine {
	case EngineUnbound:
		return path == "/api/unbound/settings/addHostOverride"
	default:
		return path == "/api/dnsmasq/settings/addHost"
	}
}
func (f *fakeOPNsense) delPathPrefix() string {
	switch f.engine {
	case EngineUnbound:
		return "/api/unbound/settings/delHostOverride/"
	default:
		return "/api/dnsmasq/settings/delHost/"
	}
}
func (f *fakeOPNsense) isDelPathPrefix(path string) bool {
	return strings.HasPrefix(path, f.delPathPrefix())
}
func (f *fakeOPNsense) reconfigurePath() string {
	switch f.engine {
	case EngineUnbound:
		return "/api/unbound/service/reconfigure"
	default:
		return "/api/dnsmasq/service/reconfigure"
	}
}

func (f *fakeOPNsense) handleSearch(w http.ResponseWriter) {
	f.mu.Lock()
	rowsCopy := make([]map[string]string, len(f.rows))
	copy(rowsCopy, f.rows)
	f.mu.Unlock()

	resp := map[string]interface{}{
		"rows":     rowsCopy,
		"rowCount": len(rowsCopy),
		"total":    len(rowsCopy),
		"current":  1,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (f *fakeOPNsense) handleAdd(w http.ResponseWriter, body []byte) {
	var wrapper map[string]map[string]string
	if err := json.Unmarshal(body, &wrapper); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	host := wrapper["host"]
	if host == nil {
		http.Error(w, "missing host", http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	f.nextUUID++
	uuid := fmt.Sprintf("uuid-%d", f.nextUUID)
	row := map[string]string{"uuid": uuid}
	for k, v := range host {
		row[k] = v
	}
	f.rows = append(f.rows, row)
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"result": "saved", "uuid": uuid})
}

func (f *fakeOPNsense) handleDel(w http.ResponseWriter, uuid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.rows {
		if r["uuid"] == uuid {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":"deleted"}`))
			return
		}
	}
	http.Error(w, "not found", http.StatusNotFound)
}

// seedUnbound inserts a row using Unbound field names.
func (f *fakeOPNsense) seedUnbound(uuid, hostname, domain, rr, server, description string, enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, map[string]string{
		"uuid": uuid, "hostname": hostname, "domain": domain,
		"rr": rr, "server": server, "description": description,
		"enabled": boolStr(enabled),
	})
}

// seedDnsmasq inserts a row using Dnsmasq field names.
func (f *fakeOPNsense) seedDnsmasq(uuid, host, domain, ip, descr string, enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, map[string]string{
		"uuid": uuid, "host": host, "domain": domain,
		"ip": ip, "descr": descr, "enabled": boolStr(enabled),
	})
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// newTestProvider spins up the fake OPNsense, wires up a real Provider
// pointed at it, and returns both plus a cleanup function.
func newTestProvider(t *testing.T, engine Engine, mode ReconfigureMode) (*Provider, *fakeOPNsense, func()) {
	t.Helper()
	f := newFakeOPNsense(t, engine)
	srv := httptest.NewServer(f.handler())

	cfg := &Config{
		URL:             srv.URL,
		APIKey:          f.apiKey,
		APISecret:       f.apiSecret,
		Engine:          engine,
		ReconfigureMode: mode,
	}
	p, err := New("opns-test", cfg)
	if err != nil {
		srv.Close()
		t.Fatalf("New: %v", err)
	}
	// The default httputil.DefaultClient() already talks http; nothing else needed.
	return p, f, srv.Close
}

func TestProvider_Type(t *testing.T) {
	p, _, cleanup := newTestProvider(t, EngineUnbound, ReconfigureModePerWrite)
	defer cleanup()

	if p.Type() != "opnsense" {
		t.Errorf("Type() = %q, want opnsense", p.Type())
	}
	id := p.Identity()
	if !strings.HasPrefix(id.Type, "opnsense/") {
		t.Errorf("Identity.Type = %q, want prefix opnsense/", id.Type)
	}

	caps := p.Capabilities()
	if caps.SupportsOwnershipTXT {
		t.Error("SupportsOwnershipTXT should be false (host overrides do not support TXT)")
	}
	if caps.SupportsNativeUpdate {
		t.Error("SupportsNativeUpdate should be false (v1)")
	}
}

func TestProvider_Ping_OK(t *testing.T) {
	p, _, cleanup := newTestProvider(t, EngineUnbound, ReconfigureModePerWrite)
	defer cleanup()

	if err := p.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestProvider_Ping_Unauthorized(t *testing.T) {
	f := newFakeOPNsense(t, EngineUnbound)
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	p, err := New("opns", &Config{
		URL:             srv.URL,
		APIKey:          "wrong",
		APISecret:       "wrong",
		Engine:          EngineUnbound,
		ReconfigureMode: ReconfigureModePerWrite,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = p.Ping(context.Background())
	if !errors.Is(err, provider.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestProvider_List_FiltersToDnsweaverOwned_Unbound(t *testing.T) {
	p, fake, cleanup := newTestProvider(t, EngineUnbound, ReconfigureModePerWrite)
	defer cleanup()

	// Two dnsweaver rows in-zone + one operator row in-zone + one dnsweaver row out-of-zone.
	fake.seedUnbound("u1", "web", "example.com", "A", "10.0.0.1", "dnsweaver:opns-test", true)
	fake.seedUnbound("u2", "api", "example.com", "AAAA", "2001:db8::1", "dnsweaver:opns-test | notes", true)
	fake.seedUnbound("u3", "manual", "example.com", "A", "10.0.0.99", "operator note", true)
	fake.seedUnbound("u4", "elsewhere", "other.tld", "A", "10.0.0.2", "dnsweaver:opns-test", true)

	// Zone filter should keep only the two dnsweaver-owned in example.com.
	p.zone = "example.com"

	recs, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2: %+v", len(recs), recs)
	}
	hosts := map[string]bool{}
	for _, r := range recs {
		hosts[r.Hostname] = true
	}
	if !hosts["web.example.com"] || !hosts["api.example.com"] {
		t.Errorf("missing expected hosts: %+v", hosts)
	}
}

func TestProvider_List_FiltersToDnsweaverOwned_Dnsmasq(t *testing.T) {
	p, fake, cleanup := newTestProvider(t, EngineDnsmasq, ReconfigureModePerWrite)
	defer cleanup()

	fake.seedDnsmasq("u1", "web", "example.com", "10.0.0.1", "dnsweaver:opns-test", true)
	fake.seedDnsmasq("u2", "manual", "example.com", "10.0.0.99", "operator", true)

	recs, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recs) != 1 || recs[0].Hostname != "web.example.com" {
		t.Fatalf("unexpected records: %+v", recs)
	}
	if recs[0].Type != provider.RecordTypeA {
		t.Errorf("type = %q, want A", recs[0].Type)
	}
}

func TestProvider_Create_Delete_Roundtrip_Unbound(t *testing.T) {
	p, fake, cleanup := newTestProvider(t, EngineUnbound, ReconfigureModePerWrite)
	defer cleanup()

	rec := provider.Record{
		Hostname: "web.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.5",
	}
	if err := p.Create(context.Background(), rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify wire: dnsweaver marker was written on the description.
	payload := fake.requestBody["/api/unbound/settings/addHostOverride"]
	if !strings.Contains(string(payload), "dnsweaver:opns-test") {
		t.Errorf("add payload missing ownership marker: %s", payload)
	}
	// Reconfigure was called after create (per_write mode).
	if fake.reloadCount != 1 {
		t.Errorf("reloadCount = %d, want 1", fake.reloadCount)
	}

	// List sees the row.
	listed, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("List after Create: got %d, want 1: %+v", len(listed), listed)
	}
	uuid := listed[0].ProviderID
	if uuid == "" {
		t.Fatal("ProviderID (UUID) missing from listed record")
	}

	// Delete using the UUID from List.
	if err := p.Delete(context.Background(), listed[0]); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if fake.reloadCount != 2 {
		t.Errorf("reloadCount after delete = %d, want 2", fake.reloadCount)
	}

	// Row is gone.
	listed, err = p.List(context.Background())
	if err != nil {
		t.Fatalf("List after Delete: %v", err)
	}
	if len(listed) != 0 {
		t.Errorf("expected empty list after delete, got %+v", listed)
	}
}

func TestProvider_Delete_LookupByFields_WhenUUIDMissing(t *testing.T) {
	p, fake, cleanup := newTestProvider(t, EngineUnbound, ReconfigureModePerWrite)
	defer cleanup()

	fake.seedUnbound("u-known", "web", "example.com", "A", "10.0.0.5", "dnsweaver:opns-test", true)

	err := p.Delete(context.Background(), provider.Record{
		Hostname: "web.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.5",
		// No ProviderID.
	})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(fake.rows) != 0 {
		t.Errorf("row not deleted: %+v", fake.rows)
	}
}

func TestProvider_Delete_DoesNotTouchOperatorRecords(t *testing.T) {
	p, fake, cleanup := newTestProvider(t, EngineUnbound, ReconfigureModePerWrite)
	defer cleanup()

	// Same hostname/target as what we'll try to delete, but no ownership marker.
	fake.seedUnbound("u-operator", "web", "example.com", "A", "10.0.0.5", "operator", true)

	err := p.Delete(context.Background(), provider.Record{
		Hostname: "web.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.5",
	})
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("expected ErrNotFound (operator row must be untouchable), got %v", err)
	}
	if len(fake.rows) != 1 {
		t.Errorf("operator row was deleted (BUG): %+v", fake.rows)
	}
}

func TestProvider_Create_SkipsTXT(t *testing.T) {
	p, fake, cleanup := newTestProvider(t, EngineUnbound, ReconfigureModePerWrite)
	defer cleanup()

	err := p.Create(context.Background(), provider.Record{
		Hostname: "_ownership.web.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
	})
	if err != nil {
		t.Fatalf("Create(TXT) should silently skip, got %v", err)
	}
	if len(fake.rows) != 0 {
		t.Errorf("TXT should not have created a row: %+v", fake.rows)
	}
}

func TestProvider_Create_RejectsInvalidHostname(t *testing.T) {
	p, _, cleanup := newTestProvider(t, EngineUnbound, ReconfigureModePerWrite)
	defer cleanup()

	err := p.Create(context.Background(), provider.Record{
		Hostname: "no-dot",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	if err == nil || !strings.Contains(err.Error(), "fully qualified") {
		t.Errorf("expected FQDN error, got %v", err)
	}
}

func TestProvider_Create_RejectsNonIPTarget(t *testing.T) {
	p, _, cleanup := newTestProvider(t, EngineUnbound, ReconfigureModePerWrite)
	defer cleanup()

	err := p.Create(context.Background(), provider.Record{
		Hostname: "web.example.com",
		Type:     provider.RecordTypeA,
		Target:   "not-an-ip",
	})
	if err == nil || !strings.Contains(err.Error(), "IP target") {
		t.Errorf("expected IP-target error, got %v", err)
	}
}

func TestProvider_ReconfigureMode_Never(t *testing.T) {
	p, fake, cleanup := newTestProvider(t, EngineUnbound, ReconfigureModeNever)
	defer cleanup()

	if err := p.Create(context.Background(), provider.Record{
		Hostname: "web.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.5",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if fake.reloadCount != 0 {
		t.Errorf("reloadCount = %d, want 0 in never mode", fake.reloadCount)
	}
}

func TestProvider_Create_FailedResponseSurfaces(t *testing.T) {
	p, fake, cleanup := newTestProvider(t, EngineUnbound, ReconfigureModePerWrite)
	defer cleanup()

	fake.mu.Lock()
	fake.override["/api/unbound/settings/addHostOverride"] = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"failed","validations":{"host.hostname":"invalid"}}`))
	}
	fake.mu.Unlock()

	err := p.Create(context.Background(), provider.Record{
		Hostname: "web.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.5",
	})
	if err == nil {
		t.Fatal("expected error on validation failure")
	}
	if !strings.Contains(err.Error(), "host.hostname") {
		t.Errorf("expected validation detail in error, got %v", err)
	}
	if fake.reloadCount != 0 {
		t.Errorf("reload should not have run after a failed create, got %d", fake.reloadCount)
	}
}

func TestJoinFQDN(t *testing.T) {
	cases := []struct {
		host, dom, want string
	}{
		{"web", "example.com", "web.example.com"},
		{"web", "example.com.", "web.example.com"},
		{"WEB", "Example.COM", "web.example.com"},
		{"", "example.com", "example.com"},
		{"web", "", "web"},
	}
	for _, tc := range cases {
		if got := joinFQDN(tc.host, tc.dom); got != tc.want {
			t.Errorf("joinFQDN(%q,%q) = %q, want %q", tc.host, tc.dom, got, tc.want)
		}
	}
}

func TestSplitFQDN(t *testing.T) {
	if h, d, ok := splitFQDN("web.example.com"); !ok || h != "web" || d != "example.com" {
		t.Errorf("normal split failed: %q %q %v", h, d, ok)
	}
	if _, _, ok := splitFQDN("host"); ok {
		t.Error("hostname without dot should fail")
	}
	if _, _, ok := splitFQDN("web.example.com."); !ok {
		t.Error("trailing dot should be ok")
	}
}

func TestInZone(t *testing.T) {
	if !inZone("web.example.com", "example.com") {
		t.Error("subdomain should match zone")
	}
	if !inZone("example.com", "example.com") {
		t.Error("apex should match zone")
	}
	if inZone("web.other.com", "example.com") {
		t.Error("unrelated FQDN must not match")
	}
	if inZone("badexample.com", "example.com") {
		t.Error("suffix without dot boundary must not match")
	}
}
