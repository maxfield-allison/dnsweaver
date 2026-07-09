package pfsense

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"sync"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// fakePfrest simulates the community pfSense-pkg-RESTAPI (pfrest) v2 host
// override endpoints for a given engine. It enforces X-API-Key auth and the
// unique (host, domain) constraint, and renders the ip field as an array
// (Unbound) or scalar (Dnsmasq) so tests exercise both wire shapes.
type fakePfrest struct {
	t      *testing.T
	res    resolver
	apiKey string

	mu         sync.Mutex
	rows       []fakeRow
	nextID     int
	applyCount int
}

type fakeRow struct {
	id     int
	host   string
	domain string
	ips    []string
	descr  string
}

func newFakePfrest(t *testing.T, engine Engine) *fakePfrest {
	t.Helper()
	return &fakePfrest{
		t:      t,
		res:    resolverFor(engine),
		apiKey: "test-key",
	}
}

func (f *fakePfrest) seed(row fakeRow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	row.id = f.nextID
	f.rows = append(f.rows, row)
}

func (f *fakePfrest) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != f.apiKey {
			f.writeEnvelope(w, http.StatusUnauthorized, "error", "invalid API key", nil)
			return
		}
		switch {
		case r.URL.Path == f.res.plural && r.Method == http.MethodGet:
			f.handleList(w)
		case r.URL.Path == f.res.single && r.Method == http.MethodPost:
			f.handleCreate(w, r)
		case r.URL.Path == f.res.single && r.Method == http.MethodPatch:
			f.handleUpdate(w, r)
		case r.URL.Path == f.res.single && r.Method == http.MethodDelete:
			f.handleDelete(w, r)
		case r.URL.Path == f.res.apply && r.Method == http.MethodPost:
			f.mu.Lock()
			f.applyCount++
			f.mu.Unlock()
			f.writeEnvelope(w, http.StatusOK, "ok", "", map[string]any{"applied": true})
		default:
			f.writeEnvelope(w, http.StatusNotFound, "error", "endpoint not found", nil)
		}
	})
}

func (f *fakePfrest) renderIP(ips []string) any {
	if f.res.multiIP {
		return ips
	}
	if len(ips) == 0 {
		return ""
	}
	return ips[0]
}

func (f *fakePfrest) rowJSON(row fakeRow) map[string]any {
	return map[string]any{
		"id":     row.id,
		"host":   row.host,
		"domain": row.domain,
		"ip":     f.renderIP(row.ips),
		"descr":  row.descr,
	}
}

func (f *fakePfrest) handleList(w http.ResponseWriter) {
	f.mu.Lock()
	data := make([]map[string]any, 0, len(f.rows))
	for _, row := range f.rows {
		data = append(data, f.rowJSON(row))
	}
	f.mu.Unlock()
	f.writeEnvelope(w, http.StatusOK, "ok", "", data)
}

type reqOverride struct {
	ID     json.Number     `json:"id"`
	Host   string          `json:"host"`
	Domain string          `json:"domain"`
	IP     json.RawMessage `json:"ip"`
	Descr  string          `json:"descr"`
}

func (f *fakePfrest) decodeBody(r *http.Request) reqOverride {
	var body reqOverride
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &body)
	return body
}

func (f *fakePfrest) handleCreate(w http.ResponseWriter, r *http.Request) {
	body := f.decodeBody(r)
	ips := decodeIPs(body.IP)

	f.mu.Lock()
	defer f.mu.Unlock()
	for _, row := range f.rows {
		if row.host == body.Host && row.domain == body.Domain {
			f.writeEnvelopeLocked(w, http.StatusBadRequest, "error",
				"A host override with this host and domain already exists.", nil)
			return
		}
	}
	f.nextID++
	row := fakeRow{id: f.nextID, host: body.Host, domain: body.Domain, ips: ips, descr: body.Descr}
	f.rows = append(f.rows, row)
	f.writeEnvelopeLocked(w, http.StatusOK, "ok", "", f.rowJSON(row))
}

func (f *fakePfrest) handleUpdate(w http.ResponseWriter, r *http.Request) {
	body := f.decodeBody(r)
	ips := decodeIPs(body.IP)
	id, _ := strconv.Atoi(body.ID.String())

	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].id == id {
			f.rows[i].host = body.Host
			f.rows[i].domain = body.Domain
			f.rows[i].ips = ips
			f.rows[i].descr = body.Descr
			f.writeEnvelopeLocked(w, http.StatusOK, "ok", "", f.rowJSON(f.rows[i]))
			return
		}
	}
	f.writeEnvelopeLocked(w, http.StatusNotFound, "error", "object not found", nil)
}

func (f *fakePfrest) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].id == id {
			removed := f.rows[i]
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			f.writeEnvelopeLocked(w, http.StatusOK, "ok", "", f.rowJSON(removed))
			return
		}
	}
	f.writeEnvelopeLocked(w, http.StatusNotFound, "error", "object not found", nil)
}

func (f *fakePfrest) writeEnvelope(w http.ResponseWriter, code int, status, message string, data any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeEnvelopeLocked(w, code, status, message, data)
}

func (f *fakePfrest) writeEnvelopeLocked(w http.ResponseWriter, code int, status, message string, data any) {
	rawData, _ := json.Marshal(data)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":        code,
		"status":      status,
		"response_id": "TEST",
		"message":     message,
		"data":        json.RawMessage(rawData),
	})
}

func (f *fakePfrest) applies() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.applyCount
}

// newTestProvider wires a Provider to a fake pfrest server for the given engine.
func newTestProvider(t *testing.T, fake *fakePfrest, engine Engine, mode ReconfigureMode) (*Provider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	cfg := &Config{
		URL:             srv.URL,
		APIKey:          fake.apiKey,
		Engine:          engine,
		ReconfigureMode: mode,
		TTL:             DefaultTTL,
	}
	p, err := New("edge-fw", cfg, WithProviderHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p, srv
}

func TestUnboundCreateAndList(t *testing.T) {
	fake := newFakePfrest(t, EngineUnbound)
	p, _ := newTestProvider(t, fake, EngineUnbound, ReconfigureModePerWrite)
	ctx := context.Background()

	if err := p.Create(ctx, provider.Record{Hostname: "web.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if fake.applies() != 1 {
		t.Errorf("apply count = %d, want 1", fake.applies())
	}

	records, err := p.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("List = %d records, want 1", len(records))
	}
	if records[0].Hostname != "web.example.com" || records[0].Type != provider.RecordTypeA || records[0].Target != "10.0.0.1" {
		t.Errorf("unexpected record: %+v", records[0])
	}
}

func TestUnboundDualStackMerge(t *testing.T) {
	fake := newFakePfrest(t, EngineUnbound)
	p, _ := newTestProvider(t, fake, EngineUnbound, ReconfigureModePerWrite)
	ctx := context.Background()

	if err := p.Create(ctx, provider.Record{Hostname: "web.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"}); err != nil {
		t.Fatalf("Create A: %v", err)
	}
	if err := p.Create(ctx, provider.Record{Hostname: "web.example.com", Type: provider.RecordTypeAAAA, Target: "2001:db8::1"}); err != nil {
		t.Fatalf("Create AAAA: %v", err)
	}

	// Both IPs must live on a single override row (unique host+domain).
	fake.mu.Lock()
	rowCount := len(fake.rows)
	fake.mu.Unlock()
	if rowCount != 1 {
		t.Fatalf("row count = %d, want 1 (dual-stack merged into one override)", rowCount)
	}

	records, err := p.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("List = %d records, want 2 (A + AAAA)", len(records))
	}
	types := []string{string(records[0].Type), string(records[1].Type)}
	sort.Strings(types)
	if types[0] != "A" || types[1] != "AAAA" {
		t.Errorf("record types = %v, want [A AAAA]", types)
	}
}

func TestUnboundCreateIdempotent(t *testing.T) {
	fake := newFakePfrest(t, EngineUnbound)
	p, _ := newTestProvider(t, fake, EngineUnbound, ReconfigureModeNever)
	ctx := context.Background()

	rec := provider.Record{Hostname: "web.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"}
	if err := p.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := p.Create(ctx, rec); err != nil {
		t.Fatalf("Create (repeat): %v", err)
	}
	fake.mu.Lock()
	rowCount := len(fake.rows)
	ipCount := len(fake.rows[0].ips)
	fake.mu.Unlock()
	if rowCount != 1 || ipCount != 1 {
		t.Errorf("rows=%d ips=%d, want 1/1 (idempotent create)", rowCount, ipCount)
	}
	if fake.applies() != 0 {
		t.Errorf("apply count = %d, want 0 (RECONFIGURE_MODE=never)", fake.applies())
	}
}

func TestRefuseOperatorOwnedOverride(t *testing.T) {
	fake := newFakePfrest(t, EngineUnbound)
	fake.seed(fakeRow{host: "web", domain: "example.com", ips: []string{"10.0.0.9"}, descr: "managed by ops"})
	p, _ := newTestProvider(t, fake, EngineUnbound, ReconfigureModePerWrite)

	err := p.Create(context.Background(), provider.Record{Hostname: "web.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"})
	if err == nil {
		t.Fatal("expected error creating over an operator-owned override")
	}
}

func TestDnsmasqSingleIPConflict(t *testing.T) {
	fake := newFakePfrest(t, EngineDnsmasq)
	p, _ := newTestProvider(t, fake, EngineDnsmasq, ReconfigureModePerWrite)
	ctx := context.Background()

	if err := p.Create(ctx, provider.Record{Hostname: "web.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"}); err != nil {
		t.Fatalf("Create A: %v", err)
	}
	err := p.Create(ctx, provider.Record{Hostname: "web.example.com", Type: provider.RecordTypeAAAA, Target: "2001:db8::1"})
	if err == nil {
		t.Fatal("expected dual-stack conflict error on dnsmasq forwarder")
	}
}

func TestDnsmasqCreateListDelete(t *testing.T) {
	fake := newFakePfrest(t, EngineDnsmasq)
	p, _ := newTestProvider(t, fake, EngineDnsmasq, ReconfigureModePerWrite)
	ctx := context.Background()

	rec := provider.Record{Hostname: "api.example.com", Type: provider.RecordTypeA, Target: "10.0.0.5"}
	if err := p.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	records, err := p.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 1 || records[0].Target != "10.0.0.5" {
		t.Fatalf("List = %+v, want single 10.0.0.5", records)
	}
	if err := p.Delete(ctx, rec); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	fake.mu.Lock()
	rowCount := len(fake.rows)
	fake.mu.Unlock()
	if rowCount != 0 {
		t.Errorf("row count after delete = %d, want 0", rowCount)
	}
}

func TestUnboundDeleteOneIPKeepsOverride(t *testing.T) {
	fake := newFakePfrest(t, EngineUnbound)
	p, _ := newTestProvider(t, fake, EngineUnbound, ReconfigureModePerWrite)
	ctx := context.Background()

	_ = p.Create(ctx, provider.Record{Hostname: "web.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"})
	_ = p.Create(ctx, provider.Record{Hostname: "web.example.com", Type: provider.RecordTypeAAAA, Target: "2001:db8::1"})

	if err := p.Delete(ctx, provider.Record{Hostname: "web.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"}); err != nil {
		t.Fatalf("Delete A: %v", err)
	}

	fake.mu.Lock()
	rowCount := len(fake.rows)
	var remaining []string
	if rowCount == 1 {
		remaining = fake.rows[0].ips
	}
	fake.mu.Unlock()
	if rowCount != 1 {
		t.Fatalf("row count = %d, want 1 (override kept for remaining AAAA)", rowCount)
	}
	if len(remaining) != 1 || remaining[0] != "2001:db8::1" {
		t.Errorf("remaining IPs = %v, want [2001:db8::1]", remaining)
	}
}

func TestDeleteNotFound(t *testing.T) {
	fake := newFakePfrest(t, EngineUnbound)
	p, _ := newTestProvider(t, fake, EngineUnbound, ReconfigureModePerWrite)

	err := p.Delete(context.Background(), provider.Record{Hostname: "ghost.example.com", Type: provider.RecordTypeA, Target: "10.0.0.9"})
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("Delete missing = %v, want ErrNotFound", err)
	}
}

func TestDeleteIgnoresOperatorOwned(t *testing.T) {
	fake := newFakePfrest(t, EngineUnbound)
	fake.seed(fakeRow{host: "web", domain: "example.com", ips: []string{"10.0.0.9"}, descr: "ops override"})
	p, _ := newTestProvider(t, fake, EngineUnbound, ReconfigureModePerWrite)

	err := p.Delete(context.Background(), provider.Record{Hostname: "web.example.com", Type: provider.RecordTypeA, Target: "10.0.0.9"})
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("Delete operator-owned = %v, want ErrNotFound (never touched)", err)
	}
	fake.mu.Lock()
	rowCount := len(fake.rows)
	fake.mu.Unlock()
	if rowCount != 1 {
		t.Errorf("operator override was modified; row count = %d, want 1", rowCount)
	}
}

func TestListFiltersByZone(t *testing.T) {
	fake := newFakePfrest(t, EngineUnbound)
	fake.seed(fakeRow{host: "web", domain: "example.com", ips: []string{"10.0.0.1"}, descr: "dnsweaver:edge-fw"})
	fake.seed(fakeRow{host: "api", domain: "other.com", ips: []string{"10.0.0.2"}, descr: "dnsweaver:edge-fw"})

	cfg := &Config{URL: "http://x", APIKey: fake.apiKey, Engine: EngineUnbound, ReconfigureMode: ReconfigureModePerWrite, Zone: "example.com", TTL: DefaultTTL}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)
	cfg.URL = srv.URL
	p, err := New("edge-fw", cfg, WithProviderHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	records, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 1 || records[0].Hostname != "web.example.com" {
		t.Fatalf("zone filter = %+v, want only web.example.com", records)
	}
}

func TestPingUnauthorized(t *testing.T) {
	fake := newFakePfrest(t, EngineUnbound)
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	cfg := &Config{URL: srv.URL, APIKey: "wrong-key", Engine: EngineUnbound, ReconfigureMode: ReconfigureModePerWrite, TTL: DefaultTTL}
	p, err := New("edge-fw", cfg, WithProviderHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := p.Ping(context.Background()); !errors.Is(err, provider.ErrUnauthorized) {
		t.Fatalf("Ping with bad key = %v, want ErrUnauthorized", err)
	}
}

func TestPingSuccess(t *testing.T) {
	fake := newFakePfrest(t, EngineUnbound)
	p, _ := newTestProvider(t, fake, EngineUnbound, ReconfigureModePerWrite)
	if err := p.Ping(context.Background()); err != nil {
		t.Fatalf("Ping = %v, want nil", err)
	}
}

func TestIdentityAndType(t *testing.T) {
	fake := newFakePfrest(t, EngineDnsmasq)
	p, _ := newTestProvider(t, fake, EngineDnsmasq, ReconfigureModePerWrite)
	if p.Type() != "pfsense" {
		t.Errorf("Type = %q, want pfsense", p.Type())
	}
	id := p.Identity()
	if id.Type != "pfsense/dnsmasq" {
		t.Errorf("Identity.Type = %q, want pfsense/dnsmasq", id.Type)
	}
}
