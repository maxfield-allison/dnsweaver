package reconciler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
	"github.com/maxfield-allison/dnsweaver/providers/unifi"
	dnsweaversource "github.com/maxfield-allison/dnsweaver/sources/dnsweaver"
)

// =============================================================================
// The real UniFi provider driven through the reconciler
//
// The UniFi provider is wired to an in-memory stand-in for the UniFi Network
// Integration API so the reconciler, the provider's policy conversion, TXT
// quoting and paged listing are all exercised together. The fake applies the
// console rules that matter for safety: paged list envelopes, duplicate
// policies rejected with HTTP 400, unquoted commas in TXT text rejected, and
// server-assigned metadata.origin on every created policy.
// =============================================================================

const (
	unifiTestAPIKey   = "test-api-key"
	unifiTestSiteID   = "88f7af54-98f8-306a-a1c7-c9349722b1f6"
	unifiTestHost     = "app.example.com"
	unifiAPIPrefix    = "/proxy/network/integration/v1"
	unifiOriginUser   = "USER_DEFINED"
	unifiOriginDerive = "DERIVED"
)

type fakeUniFiMetadata struct {
	Origin string `json:"origin"`
}

// fakeUniFiPolicy mirrors the Integration API's polymorphic DNS policy,
// including MX fields the provider itself never reads.
type fakeUniFiPolicy struct {
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type"`
	Enabled  bool               `json:"enabled"`
	Metadata *fakeUniFiMetadata `json:"metadata,omitempty"`

	Domain     string `json:"domain,omitempty"`
	TTLSeconds *int   `json:"ttlSeconds,omitempty"`

	IPv4Address      string `json:"ipv4Address,omitempty"`
	IPv6Address      string `json:"ipv6Address,omitempty"`
	TargetDomain     string `json:"targetDomain,omitempty"`
	Text             string `json:"text,omitempty"`
	MailServerDomain string `json:"mailServerDomain,omitempty"`

	Service      string `json:"service,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	ServerDomain string `json:"serverDomain,omitempty"`
	Port         *int   `json:"port,omitempty"`
	Priority     *int   `json:"priority,omitempty"`
	Weight       *int   `json:"weight,omitempty"`
}

// identity is what the console compares when rejecting a duplicate: the type
// and every type-specific property, but not the id, TTL or enabled flag.
func (p fakeUniFiPolicy) identity() string {
	deref := func(v *int) string {
		if v == nil {
			return "-"
		}
		return strconv.Itoa(*v)
	}
	return strings.Join([]string{
		p.Type, strings.ToLower(p.Domain), p.IPv4Address, p.IPv6Address, p.TargetDomain, p.Text,
		p.MailServerDomain, p.Service, p.Protocol, p.ServerDomain, deref(p.Port), deref(p.Priority), deref(p.Weight),
	}, "|")
}

// canonical renders a policy without its server-assigned id, for comparing
// state across a delete and re-create.
func (p fakeUniFiPolicy) canonical() string {
	origin := "<none>"
	if p.Metadata != nil {
		origin = p.Metadata.Origin
	}
	ttl := "-"
	if p.TTLSeconds != nil {
		ttl = strconv.Itoa(*p.TTLSeconds)
	}
	return fmt.Sprintf("%s enabled=%t origin=%s ttl=%s %s", p.Type, p.Enabled, origin, ttl, p.identity())
}

// fakeUniFiListFault simulates a console answering a policy listing with an
// incomplete response.
type fakeUniFiListFault int

const (
	unifiListOK fakeUniFiListFault = iota
	// unifiListEmptyFirstPage answers every page with no data while still
	// reporting the full totalCount.
	unifiListEmptyFirstPage
	// unifiListEmptyLaterPage serves the first page and answers every later
	// page with no data before totalCount is reached.
	unifiListEmptyLaterPage
	// unifiListEmptyObject answers with a bare {} body.
	unifiListEmptyObject
)

type fakeUniFiAPI struct {
	mu       sync.Mutex
	nextID   int
	policies []fakeUniFiPolicy

	// pageSize, when positive, caps the page size below the requested limit
	// so listings span several pages.
	pageSize int
	// listFault makes DNS policy listings incomplete.
	listFault fakeUniFiListFault
	// reject, when set, fails a POST (given the request body) or a DELETE
	// (given the stored policy) with HTTP 400.
	reject func(method string, policy fakeUniFiPolicy) bool

	writes map[string]int // "METHOD TYPE" -> requests received, rejected ones included
}

func newFakeUniFiAPI() *fakeUniFiAPI {
	return &fakeUniFiAPI{writes: make(map[string]int)}
}

// seed stores a policy as-is, as if created on the console by hand or derived
// by it, and returns it with its assigned id.
func (f *fakeUniFiAPI) seed(p fakeUniFiPolicy) fakeUniFiPolicy {
	f.mu.Lock()
	defer f.mu.Unlock()
	p.ID = f.newIDLocked()
	f.policies = append(f.policies, p)
	return p
}

func (f *fakeUniFiAPI) newIDLocked() string {
	f.nextID++
	return fmt.Sprintf("6620f2a1-0000-4000-8000-%012d", f.nextID)
}

func (f *fakeUniFiAPI) snapshot() []fakeUniFiPolicy {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeUniFiPolicy(nil), f.policies...)
}

func (f *fakeUniFiAPI) setListFault(fault fakeUniFiListFault) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listFault = fault
}

func (f *fakeUniFiAPI) setReject(reject func(method string, policy fakeUniFiPolicy) bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reject = reject
}

// writeCount returns the number of POST, PUT and DELETE requests received.
func (f *fakeUniFiAPI) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := 0
	for _, n := range f.writes {
		total += n
	}
	return total
}

func (f *fakeUniFiAPI) writesSummary() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.writes))
	for k, n := range f.writes {
		keys = append(keys, fmt.Sprintf("%s=%d", k, n))
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func writeUniFiJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeUniFiError(w http.ResponseWriter, status int, code, message string) {
	writeUniFiJSON(w, status, map[string]any{
		"statusCode": status,
		"statusName": strings.ToUpper(strings.ReplaceAll(http.StatusText(status), " ", "_")),
		"code":       code,
		"message":    message,
	})
}

// writeUniFiPage answers a list request with the Integration API envelope,
// honoring offset and limit.
func writeUniFiPage[T any](w http.ResponseWriter, r *http.Request, items []T, pageSize int) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 {
		limit = 25
	}
	if pageSize > 0 && pageSize < limit {
		limit = pageSize
	}
	offset = min(max(offset, 0), len(items))
	end := min(offset+limit, len(items))
	data := append([]T{}, items[offset:end]...)
	writeUniFiJSON(w, http.StatusOK, map[string]any{
		"offset": offset, "limit": limit, "count": len(data), "totalCount": len(items), "data": data,
	})
}

// validateLocked applies the console's write validation to a policy body.
// skipID excludes the policy being replaced from the duplicate check.
func (f *fakeUniFiAPI) validateLocked(w http.ResponseWriter, p fakeUniFiPolicy, skipID string) bool {
	quoted := len(p.Text) >= 2 && strings.HasPrefix(p.Text, `"`) && strings.HasSuffix(p.Text, `"`)
	if p.Type == "TXT_RECORD" && strings.Contains(p.Text, ",") && !quoted {
		writeUniFiError(w, http.StatusBadRequest, "api.dns.policy.validation.comma-not-quoted",
			"TXT text containing commas must be enclosed in double quotes")
		return false
	}
	for _, existing := range f.policies {
		if existing.ID != skipID && existing.identity() == p.identity() {
			writeUniFiError(w, http.StatusBadRequest, "api.dns.policy.validation.record-already-exists",
				"A DNS policy with the same type and properties already exists")
			return false
		}
	}
	return true
}

func (f *fakeUniFiAPI) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET "+unifiAPIPrefix+"/info", func(w http.ResponseWriter, _ *http.Request) {
		writeUniFiJSON(w, http.StatusOK, map[string]string{"applicationVersion": "10.6.106"})
	})

	mux.HandleFunc("GET "+unifiAPIPrefix+"/sites", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		pageSize := f.pageSize
		f.mu.Unlock()
		sites := []map[string]string{{"id": unifiTestSiteID, "internalReference": "default", "name": "Default"}}
		writeUniFiPage(w, r, sites, pageSize)
	})

	policies := unifiAPIPrefix + "/sites/{site}/dns/policies"

	mux.HandleFunc("GET "+policies, func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		items := append([]fakeUniFiPolicy(nil), f.policies...)
		pageSize, fault := f.pageSize, f.listFault
		f.mu.Unlock()
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		switch {
		case fault == unifiListEmptyObject:
			writeUniFiJSON(w, http.StatusOK, map[string]any{})
		case fault == unifiListEmptyFirstPage, fault == unifiListEmptyLaterPage && offset > 0:
			writeUniFiJSON(w, http.StatusOK, map[string]any{
				"offset": offset, "limit": pageSize, "count": 0, "totalCount": len(items), "data": []fakeUniFiPolicy{},
			})
		default:
			writeUniFiPage(w, r, items, pageSize)
		}
	})

	mux.HandleFunc("POST "+policies, func(w http.ResponseWriter, r *http.Request) {
		var body fakeUniFiPolicy
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeUniFiError(w, http.StatusBadRequest, "api.request.invalid-body", err.Error())
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.writes["POST "+body.Type]++
		if f.reject != nil && f.reject(http.MethodPost, body) {
			writeUniFiError(w, http.StatusBadRequest, "api.dns.policy.validation.rejected", "rejected by test")
			return
		}
		if !f.validateLocked(w, body, "") {
			return
		}
		body.ID = f.newIDLocked()
		body.Metadata = &fakeUniFiMetadata{Origin: unifiOriginUser}
		f.policies = append(f.policies, body)
		writeUniFiJSON(w, http.StatusCreated, body)
	})

	mux.HandleFunc("PUT "+policies+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body fakeUniFiPolicy
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeUniFiError(w, http.StatusBadRequest, "api.request.invalid-body", err.Error())
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.writes["PUT "+body.Type]++
		id := r.PathValue("id")
		for i := range f.policies {
			if f.policies[i].ID != id {
				continue
			}
			if !f.validateLocked(w, body, id) {
				return
			}
			body.ID = id
			body.Metadata = f.policies[i].Metadata // server-assigned, not writable
			f.policies[i] = body
			writeUniFiJSON(w, http.StatusOK, body)
			return
		}
		writeUniFiError(w, http.StatusNotFound, "api.dns.policy.not-found", "DNS policy not found")
	})

	mux.HandleFunc("DELETE "+policies+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		id := r.PathValue("id")
		for i, p := range f.policies {
			if p.ID != id {
				continue
			}
			f.writes["DELETE "+p.Type]++
			if f.reject != nil && f.reject(http.MethodDelete, p) {
				writeUniFiError(w, http.StatusBadRequest, "api.dns.policy.validation.rejected", "rejected by test")
				return
			}
			f.policies = append(f.policies[:i], f.policies[i+1:]...)
			w.WriteHeader(http.StatusOK)
			return
		}
		f.writes["DELETE <unknown>"]++
		writeUniFiError(w, http.StatusNotFound, "api.dns.policy.not-found", "DNS policy not found")
	})

	// Authentication and site checks run in front of every route, as the
	// UniFi OS proxy and Network application do.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != unifiTestAPIKey {
			writeUniFiJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"code": 401, "message": "Unauthorized"}})
			return
		}
		sitePrefix := unifiAPIPrefix + "/sites/"
		if rest, ok := strings.CutPrefix(r.URL.Path, sitePrefix); ok && rest != "" {
			if site, _, _ := strings.Cut(rest, "/"); site != unifiTestSiteID {
				writeUniFiError(w, http.StatusNotFound, "api.site.not-found", "site not found")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func (f *fakeUniFiAPI) start(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(f.handler())
	t.Cleanup(server.Close)
	return server
}

// newUniFiStateFixture returns a reconciler whose single managed instance is
// built by the real unifi.Factory against server, exactly as the registry
// builds it in production (including resolving the site at construction).
// Calling it again against the same server simulates a dnsweaver restart: no
// provider, registry or reconciler state survives, only what the console
// stores.
func newUniFiStateFixture(t *testing.T, server *httptest.Server, lister *testMockWorkloadLister) *Reconciler {
	t.Helper()

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.SetInstanceID("test-instance")
	providers.RegisterFactory("unifi", unifi.Factory())
	if err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "unifi",
		TypeName:   "unifi",
		RecordType: provider.RecordTypeA,
		Target:     "192.0.2.1",
		TTL:        300,
		Mode:       provider.ModeManaged,
		Domains:    []string{"*.example.com"},
		ProviderConfig: map[string]string{
			"URL": server.URL, "API_KEY": unifiTestAPIKey, "SITE": "default", "ZONE": "example.com", "TTL": "300",
		},
	}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	inst, _ := providers.Get("unifi")
	if want := server.URL + "/sites/" + unifiTestSiteID; inst.Identity.Endpoint != want {
		t.Fatalf("identity endpoint = %q, want %q (site resolved before the registry records it)", inst.Identity.Endpoint, want)
	}

	sources := source.NewRegistry(logger)
	sources.Register(dnsweaversource.New(dnsweaversource.WithLogger(logger)))
	cfg := DefaultConfig()
	cfg.InstanceID = "test-instance"
	return New([]workload.Lister{lister}, sources, providers, WithConfig(cfg), WithLogger(logger))
}

// unifiRecordLabels declares one explicit record on unifiTestHost.
func unifiRecordLabels(recordType, target string) map[string]string {
	return map[string]string{
		"dnsweaver.records.app.hostname": unifiTestHost,
		"dnsweaver.records.app.type":     recordType,
		"dnsweaver.records.app.target":   target,
	}
}

func setUniFiWorkload(lister *testMockWorkloadLister, recordType, target string) {
	lister.workloads = nil
	lister.AddWorkload("app", unifiRecordLabels(recordType, target))
}

// unifiPolicyMatches reports whether p is the enabled policy storing a record,
// allowing for the double quotes the console requires around TXT commas.
func unifiPolicyMatches(p fakeUniFiPolicy, recordType provider.RecordType, hostname, target string) bool {
	if !strings.EqualFold(p.Domain, hostname) || !p.Enabled {
		return false
	}
	switch recordType {
	case provider.RecordTypeA:
		return p.Type == "A_RECORD" && p.IPv4Address == target
	case provider.RecordTypeAAAA:
		return p.Type == "AAAA_RECORD" && p.IPv6Address == target
	case provider.RecordTypeCNAME:
		return p.Type == "CNAME_RECORD" && p.TargetDomain == target
	case provider.RecordTypeTXT:
		text := p.Text
		if strings.Contains(target, ",") {
			text = strings.TrimSuffix(strings.TrimPrefix(text, `"`), `"`)
		}
		return p.Type == "TXT_RECORD" && text == target
	default:
		return false
	}
}

// unifiCountRecord counts stored policies for the record, and for its
// member ownership marker under test-instance.
func unifiCountRecord(api *fakeUniFiAPI, recordType provider.RecordType, target string) (data, markers int) {
	member := provider.Record{Hostname: unifiTestHost, Type: recordType, Target: target}
	for _, p := range api.snapshot() {
		if unifiPolicyMatches(p, recordType, unifiTestHost, target) {
			data++
		}
		if p.Type == "TXT_RECORD" && strings.EqualFold(p.Domain, provider.OwnershipRecordName(unifiTestHost)) &&
			provider.MatchesMemberOwnership(strings.Trim(p.Text, `"`), "test-instance", member) {
			markers++
		}
	}
	return data, markers
}

func assertUniFiRecord(t *testing.T, api *fakeUniFiAPI, recordType provider.RecordType, target string, want bool) {
	t.Helper()
	wantCount := 0
	if want {
		wantCount = 1
	}
	data, markers := unifiCountRecord(api, recordType, target)
	if data != wantCount || markers != wantCount {
		t.Fatalf("%s %s: %d data policies and %d ownership markers, want %d of each\nstate:\n%s",
			recordType, target, data, markers, wantCount, unifiStateDump(api))
	}
}

func unifiStateDump(api *fakeUniFiAPI) string {
	var lines []string
	for _, p := range api.snapshot() {
		lines = append(lines, "  "+p.canonical())
	}
	return strings.Join(lines, "\n")
}

// unifiCanonicalState lists stored policies without their ids, sorted.
func unifiCanonicalState(api *fakeUniFiAPI) []string {
	var out []string
	for _, p := range api.snapshot() {
		out = append(out, p.canonical())
	}
	sort.Strings(out)
	return out
}

func reconcileUniFi(t *testing.T, r *Reconciler) *Result {
	t.Helper()
	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	return result
}

// seedUniFiNeighbors stores policies dnsweaver must never touch on the
// hostname it manages: a manual A with a different address, manual TXT values
// (one quoted because it contains a comma), a console-derived A, a disabled
// user A and an MX policy.
func seedUniFiNeighbors(api *fakeUniFiAPI) []fakeUniFiPolicy {
	ttl, priority := 600, 10
	user := &fakeUniFiMetadata{Origin: unifiOriginUser}
	return []fakeUniFiPolicy{
		api.seed(fakeUniFiPolicy{Type: "A_RECORD", Enabled: true, Metadata: user, Domain: unifiTestHost, TTLSeconds: &ttl, IPv4Address: "192.0.2.99"}),
		api.seed(fakeUniFiPolicy{Type: "TXT_RECORD", Enabled: true, Metadata: user, Domain: unifiTestHost, Text: "v=spf1 -all"}),
		api.seed(fakeUniFiPolicy{Type: "TXT_RECORD", Enabled: true, Metadata: user, Domain: unifiTestHost, Text: `"k=v,other=value"`}),
		api.seed(fakeUniFiPolicy{Type: "A_RECORD", Enabled: true, Metadata: &fakeUniFiMetadata{Origin: unifiOriginDerive}, Domain: unifiTestHost, TTLSeconds: &ttl, IPv4Address: "192.0.2.50"}),
		api.seed(fakeUniFiPolicy{Type: "A_RECORD", Enabled: false, Metadata: user, Domain: unifiTestHost, TTLSeconds: &ttl, IPv4Address: "192.0.2.60"}),
		api.seed(fakeUniFiPolicy{Type: "MX_RECORD", Enabled: true, Metadata: user, Domain: unifiTestHost, MailServerDomain: "mail.example.com", Priority: &priority}),
	}
}

// assertUniFiNeighborsIntact fails unless every seeded policy is still stored
// with the same id and content.
func assertUniFiNeighborsIntact(t *testing.T, api *fakeUniFiAPI, seeds []fakeUniFiPolicy) {
	t.Helper()
	current := make(map[string]fakeUniFiPolicy)
	for _, p := range api.snapshot() {
		current[p.ID] = p
	}
	for _, seed := range seeds {
		got, ok := current[seed.ID]
		if !ok {
			t.Errorf("seeded policy deleted: %s", seed.canonical())
			continue
		}
		if !reflect.DeepEqual(got, seed) {
			t.Errorf("seeded policy changed:\n got %s\nwant %s", got.canonical(), seed.canonical())
		}
	}
}

func TestReconcile_UniFiUnchangedSecondPassMakesNoWrites(t *testing.T) {
	tests := []struct {
		name       string
		recordType provider.RecordType
		target     string
	}{
		{"A", provider.RecordTypeA, "192.0.2.10"},
		{"TXT", provider.RecordTypeTXT, "verification=unifi"},
		{"TXT with comma", provider.RecordTypeTXT, "v=DKIM1, k=rsa, p=MIGf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeUniFiAPI()
			server := api.start(t)
			lister := newTestMockWorkloadLister(workload.PlatformDocker)
			setUniFiWorkload(lister, string(tt.recordType), tt.target)
			r := newUniFiStateFixture(t, server, lister)

			first := reconcileUniFi(t, r)
			if first.FailedCount() != 0 {
				t.Fatalf("first pass failed: %+v", first.Actions)
			}
			assertUniFiRecord(t, api, tt.recordType, tt.target, true)
			writes := api.writeCount()

			second := reconcileUniFi(t, r)
			if second.FailedCount() != 0 || len(second.Created()) != 0 || len(second.Updated()) != 0 || len(second.Deleted()) != 0 {
				t.Errorf("second pass actions = %+v, want none", second.Actions)
			}
			if got := api.writeCount(); got != writes {
				t.Errorf("second pass issued %d writes (all passes: %s), want 0", got-writes, api.writesSummary())
			}
			assertUniFiRecord(t, api, tt.recordType, tt.target, true)
		})
	}
}

func TestReconcile_UniFiPreservesSiblingAndUnrelatedPolicies(t *testing.T) {
	api := newFakeUniFiAPI()
	seeds := seedUniFiNeighbors(api)
	server := api.start(t)
	lister := newTestMockWorkloadLister(workload.PlatformDocker)
	setUniFiWorkload(lister, "A", "192.0.2.10")
	r := newUniFiStateFixture(t, server, lister)

	result := reconcileUniFi(t, r)
	if result.FailedCount() != 0 {
		t.Fatalf("create pass failed: %+v", result.Actions)
	}
	assertUniFiRecord(t, api, provider.RecordTypeA, "192.0.2.10", true)
	assertUniFiNeighborsIntact(t, api, seeds)

	setUniFiWorkload(lister, "A", "192.0.2.20")
	result = reconcileUniFi(t, r)
	if result.FailedCount() != 0 {
		t.Fatalf("target change failed: %+v", result.Actions)
	}
	assertUniFiRecord(t, api, provider.RecordTypeA, "192.0.2.10", false)
	assertUniFiRecord(t, api, provider.RecordTypeA, "192.0.2.20", true)
	assertUniFiNeighborsIntact(t, api, seeds)

	lister.workloads = nil
	result = reconcileUniFi(t, r)
	if result.FailedCount() != 0 {
		t.Fatalf("removal pass failed: %+v", result.Actions)
	}
	assertUniFiNeighborsIntact(t, api, seeds)
	if got := api.snapshot(); len(got) != len(seeds) {
		t.Fatalf("after removal %d policies remain, want only the %d seeded\nstate:\n%s", len(got), len(seeds), unifiStateDump(api))
	}
}

func TestReconcile_UniFiOwnershipSurvivesRestart(t *testing.T) {
	api := newFakeUniFiAPI()
	seeds := seedUniFiNeighbors(api)
	server := api.start(t)
	lister := newTestMockWorkloadLister(workload.PlatformDocker)
	setUniFiWorkload(lister, "A", "192.0.2.10")

	reconcileUniFi(t, newUniFiStateFixture(t, server, lister))
	assertUniFiRecord(t, api, provider.RecordTypeA, "192.0.2.10", true)

	// A restarted process with the workload unchanged recognizes its own
	// record from the durable marker and writes nothing.
	writes := api.writeCount()
	result := reconcileUniFi(t, newUniFiStateFixture(t, server, lister))
	if result.FailedCount() != 0 {
		t.Fatalf("restarted unchanged pass failed: %+v", result.Actions)
	}
	if got := api.writeCount(); got != writes {
		t.Errorf("restarted unchanged pass issued %d writes (all passes: %s), want 0", got-writes, api.writesSummary())
	}

	// Another restart after the workload is gone: the marker alone must
	// authorize removal of dnsweaver's record, and nothing else.
	lister.workloads = nil
	result = reconcileUniFi(t, newUniFiStateFixture(t, server, lister))
	if result.FailedCount() != 0 {
		t.Fatalf("restarted removal pass failed: %+v", result.Actions)
	}
	assertUniFiRecord(t, api, provider.RecordTypeA, "192.0.2.10", false)
	assertUniFiNeighborsIntact(t, api, seeds)
	if got := api.snapshot(); len(got) != len(seeds) {
		t.Fatalf("after removal %d policies remain, want only the %d seeded\nstate:\n%s", len(got), len(seeds), unifiStateDump(api))
	}
}

func TestReconcile_UniFiFailedReplacementRestoresOriginal(t *testing.T) {
	// Mirrors TestRegressionCrossTypeFailureRestoresAnswer against the real
	// provider: the console rejects the CNAME that would replace an A record.
	// No neighbors share the name: an unowned A there would make the CNAME a
	// type conflict the reconciler refuses before any write.
	api := newFakeUniFiAPI()
	server := api.start(t)
	lister := newTestMockWorkloadLister(workload.PlatformDocker)
	setUniFiWorkload(lister, "A", "192.0.2.10")
	r := newUniFiStateFixture(t, server, lister)

	reconcileUniFi(t, r)
	assertUniFiRecord(t, api, provider.RecordTypeA, "192.0.2.10", true)
	before := unifiCanonicalState(api)

	setUniFiWorkload(lister, "CNAME", "new.example.com")
	api.setReject(func(method string, p fakeUniFiPolicy) bool {
		return method == http.MethodPost && p.Type == "CNAME_RECORD"
	})
	result := reconcileUniFi(t, r)
	if result.FailedCount() == 0 {
		t.Fatalf("rejected replacement reported success: %+v", result.Actions)
	}
	if got := api.writesSummary(); !strings.Contains(got, "DELETE A_RECORD=1") || !strings.Contains(got, "POST CNAME_RECORD=1") {
		t.Fatalf("writes = %s, want the A deleted and the CNAME attempted", got)
	}
	// The A is re-created under a new id; everything else is as it was.
	assertUniFiRecord(t, api, provider.RecordTypeA, "192.0.2.10", true)
	assertUniFiRecord(t, api, provider.RecordTypeCNAME, "new.example.com", false)
	if got := unifiCanonicalState(api); !reflect.DeepEqual(got, before) {
		t.Fatalf("state after failed replacement:\n got %v\nwant %v", got, before)
	}

	// Once the console accepts the CNAME the replacement completes.
	api.setReject(nil)
	result = reconcileUniFi(t, r)
	if result.FailedCount() != 0 {
		t.Fatalf("retried replacement failed: %+v", result.Actions)
	}
	assertUniFiRecord(t, api, provider.RecordTypeA, "192.0.2.10", false)
	assertUniFiRecord(t, api, provider.RecordTypeCNAME, "new.example.com", true)
	if got := api.snapshot(); len(got) != 2 {
		t.Fatalf("after replacement %d policies remain, want the CNAME and its marker\nstate:\n%s", len(got), unifiStateDump(api))
	}
}

func TestReconcile_UniFiIncompleteListingStopsReconciliation(t *testing.T) {
	tests := []struct {
		name  string
		fault fakeUniFiListFault
	}{
		{"empty page before totalCount", unifiListEmptyLaterPage},
		{"empty first page with nonzero totalCount", unifiListEmptyFirstPage},
		{"empty object body", unifiListEmptyObject},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeUniFiAPI()
			// Two policies per page, so the neighbors fill the first pages and
			// dnsweaver's own policies are only visible on later ones.
			api.pageSize = 2
			seeds := seedUniFiNeighbors(api)
			server := api.start(t)
			lister := newTestMockWorkloadLister(workload.PlatformDocker)
			setUniFiWorkload(lister, "A", "192.0.2.10")
			r := newUniFiStateFixture(t, server, lister)

			if result := reconcileUniFi(t, r); result.FailedCount() != 0 {
				t.Fatalf("first pass failed: %+v", result.Actions)
			}
			assertUniFiRecord(t, api, provider.RecordTypeA, "192.0.2.10", true)
			before := api.snapshot()
			writes := api.writeCount()

			setUniFiWorkload(lister, "A", "192.0.2.20")
			api.setListFault(tt.fault)
			result, err := r.Reconcile(context.Background())
			if got := api.writeCount(); got != writes {
				t.Errorf("incomplete listing pass issued %d writes (all passes: %s), want 0", got-writes, api.writesSummary())
			}
			if err == nil && result.FailedCount() == 0 {
				t.Errorf("incomplete listing pass reported success: %+v", result.Actions)
			}
			if got := api.snapshot(); !reflect.DeepEqual(got, before) {
				t.Errorf("state changed during incomplete listing pass\nstate:\n%s", unifiStateDump(api))
			}

			// A complete listing on the next pass converges normally.
			api.setListFault(unifiListOK)
			if result := reconcileUniFi(t, r); result.FailedCount() != 0 {
				t.Fatalf("recovery pass failed: %+v", result.Actions)
			}
			assertUniFiRecord(t, api, provider.RecordTypeA, "192.0.2.10", false)
			assertUniFiRecord(t, api, provider.RecordTypeA, "192.0.2.20", true)
			assertUniFiNeighborsIntact(t, api, seeds)
		})
	}
}
