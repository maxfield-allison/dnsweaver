package unifi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

const (
	testAPIKey = "test-key"
	testSiteID = "5b0d5f4e-1c2a-4f3b-9e8d-0a1b2c3d4e5f"
)

// fakeController simulates the UniFi Network Integration API DNS policy
// endpoints. It enforces X-API-KEY auth, renders the real page envelope with
// offset/limit paging, assigns UUID-like ids, stamps USER_DEFINED metadata on
// created policies, and returns the documented error body on failures.
type fakeController struct {
	t       *testing.T
	apiKey  string
	version string // applicationVersion reported by GET /info
	sites   []site

	mu       sync.Mutex
	policies []dnsPolicy
	nextID   int
	requests []string // "METHOD path" in order received

	// rateLimitGETs makes the next N GET requests answer 429 with Retry-After: 0.
	rateLimitGETs int
	// rateLimitPOSTs makes the next N POST requests answer 429.
	rateLimitPOSTs int
}

func newFakeController(t *testing.T) *fakeController {
	t.Helper()
	return &fakeController{
		t:       t,
		apiKey:  testAPIKey,
		version: "10.6.106",
		sites: []site{
			{ID: testSiteID, InternalReference: "default", Name: "Default"},
			{ID: "9a8b7c6d-0000-4000-8000-000000000002", InternalReference: "branch", Name: "Branch Office"},
		},
	}
}

// seed stores a policy directly, assigning an id and stamping USER_DEFINED
// metadata when none is set, and returns that id.
func (f *fakeController) seed(pol dnsPolicy) string {
	if pol.Metadata == nil {
		pol.Metadata = &policyMetadata{Origin: originUserDefined}
	}
	return f.seedRaw(pol)
}

// seedRaw stores a policy exactly as given (apart from the id), so tests can
// model consoles that omit metadata.
func (f *fakeController) seedRaw(pol dnsPolicy) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	pol.ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", f.nextID)
	f.policies = append(f.policies, pol)
	return pol.ID
}

// hasDuplicate reports whether a policy with the same type and properties is
// already stored (id and metadata excluded).
func (f *fakeController) hasDuplicate(pol dnsPolicy) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.policies {
		if policyKey(existing) == policyKey(pol) {
			return true
		}
	}
	return false
}

func policyKey(pol dnsPolicy) string {
	pol.ID, pol.Metadata = "", nil
	key, _ := json.Marshal(pol)
	return string(key)
}

// get returns a stored policy by id.
func (f *fakeController) get(id string) (dnsPolicy, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, pol := range f.policies {
		if pol.ID == id {
			return pol, true
		}
	}
	return dnsPolicy{}, false
}

// first returns the first stored policy.
func (f *fakeController) first() dnsPolicy {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.policies) == 0 {
		f.t.Fatal("no policies stored")
	}
	return f.policies[0]
}

func (f *fakeController) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.policies)
}

func (f *fakeController) requestCount(method, pathSuffix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if strings.HasPrefix(r, method+" ") && strings.HasSuffix(r, pathSuffix) {
			n++
		}
	}
	return n
}

func (f *fakeController) server() *httptest.Server {
	srv := httptest.NewServer(f.handler())
	f.t.Cleanup(srv.Close)
	return srv
}

func (f *fakeController) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		f.mu.Unlock()

		if r.Header.Get("X-API-KEY") != f.apiKey {
			f.writeError(w, http.StatusUnauthorized, "api.authentication.missing-credentials", "Missing credentials")
			return
		}

		if !strings.HasPrefix(r.URL.Path, integrationAPIPath+"/") {
			f.writeError(w, http.StatusNotFound, "api.err.NotFound", "unknown path "+r.URL.Path)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, integrationAPIPath)

		f.mu.Lock()
		if r.Method == http.MethodGet && f.rateLimitGETs > 0 {
			f.rateLimitGETs--
			f.mu.Unlock()
			w.Header().Set("Retry-After", "0")
			f.writeError(w, http.StatusTooManyRequests, "api.err.RateLimited", "Too many requests")
			return
		}
		if r.Method == http.MethodPost && f.rateLimitPOSTs > 0 {
			f.rateLimitPOSTs--
			f.mu.Unlock()
			w.Header().Set("Retry-After", "0")
			f.writeError(w, http.StatusTooManyRequests, "api.err.RateLimited", "Too many requests")
			return
		}
		f.mu.Unlock()

		switch {
		case path == "/info" && r.Method == http.MethodGet:
			f.writeJSON(w, http.StatusOK, applicationInfo{ApplicationVersion: f.version})
		case path == "/sites" && r.Method == http.MethodGet:
			f.writePage(w, r, f.sites)
		case strings.HasPrefix(path, "/sites/"):
			f.handleSite(w, r, strings.TrimPrefix(path, "/sites/"))
		default:
			f.writeError(w, http.StatusNotFound, "api.err.NotFound", "unknown path "+path)
		}
	})
}

func (f *fakeController) handleSite(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(rest, "/")
	if parts[0] != testSiteID {
		f.writeError(w, http.StatusNotFound, "api.err.SiteNotFound", "site not found")
		return
	}
	if len(parts) < 3 || parts[1] != "dns" || parts[2] != "policies" {
		f.writeError(w, http.StatusNotFound, "api.err.NotFound", "unknown path")
		return
	}

	switch {
	case len(parts) == 3 && r.Method == http.MethodGet:
		f.mu.Lock()
		policies := append([]dnsPolicy(nil), f.policies...)
		f.mu.Unlock()
		f.writePage(w, r, policies)
	case len(parts) == 3 && r.Method == http.MethodPost:
		var pol dnsPolicy
		if err := json.NewDecoder(r.Body).Decode(&pol); err != nil {
			f.writeError(w, http.StatusBadRequest, "api.err.InvalidPayload", err.Error())
			return
		}
		if pol.Type == "" || pol.Domain == "" {
			f.writeError(w, http.StatusBadRequest, "api.err.InvalidPayload", "type and domain are required")
			return
		}
		if f.hasDuplicate(pol) {
			// Observed on UniFi OS Server 10.6: duplicates are a 400 with a
			// dedicated code, not a 409.
			f.writeError(w, http.StatusBadRequest, errorCodeAlreadyExists, "DNS policy with the same type and properties already exists")
			return
		}
		id := f.seed(pol)
		stored, _ := f.get(id)
		f.writeJSON(w, http.StatusCreated, stored)
	case len(parts) == 4 && r.Method == http.MethodPut:
		var pol dnsPolicy
		if err := json.NewDecoder(r.Body).Decode(&pol); err != nil {
			f.writeError(w, http.StatusBadRequest, "api.err.InvalidPayload", err.Error())
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		for i := range f.policies {
			if f.policies[i].ID == parts[3] {
				pol.ID = parts[3]
				pol.Metadata = f.policies[i].Metadata
				f.policies[i] = pol
				f.writeJSON(w, http.StatusOK, pol)
				return
			}
		}
		f.writeError(w, http.StatusNotFound, "api.err.NotFound", "policy not found")
	case len(parts) == 4 && r.Method == http.MethodDelete:
		f.mu.Lock()
		defer f.mu.Unlock()
		for i := range f.policies {
			if f.policies[i].ID == parts[3] {
				f.policies = append(f.policies[:i], f.policies[i+1:]...)
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		f.writeError(w, http.StatusNotFound, "api.err.NotFound", "policy not found")
	default:
		f.writeError(w, http.StatusMethodNotAllowed, "api.err.MethodNotAllowed", "method not allowed")
	}
}

func (f *fakeController) writePage(w http.ResponseWriter, r *http.Request, items any) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 25
	}
	if limit > pageLimit {
		f.writeError(w, http.StatusBadRequest, "api.err.InvalidLimit", "limit exceeds 200")
		return
	}

	switch list := items.(type) {
	case []site:
		end := min(offset+limit, len(list))
		start := min(offset, end)
		f.writeJSON(w, http.StatusOK, page[site]{Offset: offset, Limit: limit, Count: end - start, TotalCount: len(list), Data: list[start:end]})
	case []dnsPolicy:
		end := min(offset+limit, len(list))
		start := min(offset, end)
		f.writeJSON(w, http.StatusOK, page[dnsPolicy]{Offset: offset, Limit: limit, Count: end - start, TotalCount: len(list), Data: list[start:end]})
	default:
		f.t.Fatalf("writePage: unsupported item type %T", items)
	}
}

func (f *fakeController) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeController) writeError(w http.ResponseWriter, status int, code, message string) {
	f.writeJSON(w, status, map[string]any{
		"statusCode":  status,
		"statusName":  http.StatusText(status),
		"code":        code,
		"message":     message,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"requestPath": "/integration/v1",
		"requestId":   "00000000-0000-4000-8000-0000000000ff",
	})
}

func intPtr(v int) *int { return &v }

func TestNewClient(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"plain", "https://unifi.local", "https://unifi.local/proxy/network/integration/v1"},
		{"trailing slash on URL", "https://unifi.local/", "https://unifi.local/proxy/network/integration/v1"},
		{"with port", "https://unifi.local:8443", "https://unifi.local:8443/proxy/network/integration/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.url, testAPIKey)
			if client.baseURL != tt.want {
				t.Errorf("baseURL = %q, want %q", client.baseURL, tt.want)
			}
			if client.apiKey != testAPIKey {
				t.Errorf("apiKey = %q, want %q", client.apiKey, testAPIKey)
			}
			if client.httpClient == nil {
				t.Error("expected httpClient to be initialized")
			}
			if client.logger == nil {
				t.Error("expected logger to be initialized")
			}
		})
	}
}

func TestClient_Ping_Success(t *testing.T) {
	var gotHeader, gotAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != integrationAPIPath+"/info" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotHeader = r.Header.Get("X-API-KEY")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(applicationInfo{ApplicationVersion: "10.3.58"})
	}))
	defer server.Close()

	client := NewClient(server.URL, testAPIKey)
	version, err := client.Ping(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "10.3.58" {
		t.Errorf("version = %q, want 10.3.58", version)
	}
	if gotHeader != testAPIKey {
		t.Errorf("X-API-KEY = %q, want %q", gotHeader, testAPIKey)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
}

func TestClient_Ping_Unauthorized(t *testing.T) {
	fake := newFakeController(t)
	server := fake.server()

	client := NewClient(server.URL, "wrong-key")
	_, err := client.Ping(context.Background())
	if !errors.Is(err, provider.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestClient_Ping_Unreachable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close() // closed immediately so the connection is refused

	client := NewClient(server.URL, testAPIKey)
	_, err := client.Ping(context.Background())
	if !errors.Is(err, provider.ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}
}

func TestClient_ResolveSiteID(t *testing.T) {
	fake := newFakeController(t)
	server := fake.server()
	client := NewClient(server.URL, testAPIKey)

	tests := []struct {
		name    string
		site    string
		wantID  string
		wantErr string
	}{
		{name: "by internalReference", site: "default", wantID: testSiteID},
		{name: "by UUID", site: testSiteID, wantID: testSiteID},
		{name: "second site", site: "branch", wantID: "9a8b7c6d-0000-4000-8000-000000000002"},
		{name: "unknown lists available sites", site: "nope", wantErr: "available: default, branch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := client.ResolveSiteID(context.Background(), tt.site)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id != tt.wantID {
				t.Errorf("id = %q, want %q", id, tt.wantID)
			}
		})
	}
}

func TestClient_ListDNSPolicies_Pagination(t *testing.T) {
	fake := newFakeController(t)
	const total = 450 // three pages at the 200 limit, last one partial
	for i := 0; i < total; i++ {
		fake.seed(dnsPolicy{Type: policyTypeA, Enabled: true, Domain: fmt.Sprintf("host%d.example.com", i), IPv4Address: "10.0.0.1"})
	}
	server := fake.server()

	client := NewClient(server.URL, testAPIKey)
	policies, err := client.ListDNSPolicies(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(policies) != total {
		t.Fatalf("got %d policies, want %d", len(policies), total)
	}
	if got := fake.requestCount(http.MethodGet, "/dns/policies"); got != 3 {
		t.Errorf("list requests = %d, want 3", got)
	}

	seen := make(map[string]bool, total)
	for _, pol := range policies {
		if seen[pol.ID] {
			t.Errorf("policy %s returned twice", pol.ID)
		}
		seen[pol.ID] = true
	}
}

func TestClient_ListDNSPolicies_Empty(t *testing.T) {
	fake := newFakeController(t)
	server := fake.server()

	client := NewClient(server.URL, testAPIKey)
	policies, err := client.ListDNSPolicies(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(policies) != 0 {
		t.Errorf("got %d policies, want 0", len(policies))
	}
}

func TestClient_RateLimit_RetriesGET(t *testing.T) {
	fake := newFakeController(t)
	fake.rateLimitGETs = 2
	server := fake.server()

	client := NewClient(server.URL, testAPIKey)
	if _, err := client.Ping(context.Background()); err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if got := fake.requestCount(http.MethodGet, "/info"); got != 3 {
		t.Errorf("GET /info attempts = %d, want 3", got)
	}
}

func TestClient_RateLimit_GivesUpAfterMaxRetries(t *testing.T) {
	fake := newFakeController(t)
	fake.rateLimitGETs = maxRetries + 1
	server := fake.server()

	client := NewClient(server.URL, testAPIKey)
	_, err := client.Ping(context.Background())
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("expected 429 error after retries, got %v", err)
	}
	if got := fake.requestCount(http.MethodGet, "/info"); got != maxRetries {
		t.Errorf("GET /info attempts = %d, want %d", got, maxRetries)
	}
}

func TestClient_RateLimit_NeverRetriesPOST(t *testing.T) {
	fake := newFakeController(t)
	fake.rateLimitPOSTs = 1
	server := fake.server()

	client := NewClient(server.URL, testAPIKey)
	_, err := client.CreateDNSPolicy(context.Background(), testSiteID, dnsPolicy{
		Type: policyTypeA, Enabled: true, Domain: "a.example.com", IPv4Address: "10.0.0.1", TTLSeconds: intPtr(300),
	})
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("expected 429 error, got %v", err)
	}
	if got := fake.requestCount(http.MethodPost, "/dns/policies"); got != 1 {
		t.Errorf("POST attempts = %d, want 1 (mutations must not be retried)", got)
	}
	if fake.count() != 0 {
		t.Errorf("expected no policy to be stored, got %d", fake.count())
	}
}

func TestClient_CreateDNSPolicy_ReturnsID(t *testing.T) {
	fake := newFakeController(t)
	server := fake.server()

	client := NewClient(server.URL, testAPIKey)
	created, err := client.CreateDNSPolicy(context.Background(), testSiteID, dnsPolicy{
		Type: policyTypeA, Enabled: true, Domain: "a.example.com", IPv4Address: "10.0.0.1", TTLSeconds: intPtr(300),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID == "" {
		t.Error("expected created policy to carry an id")
	}
	if created.Metadata == nil || created.Metadata.Origin != originUserDefined {
		t.Errorf("metadata = %+v, want USER_DEFINED origin", created.Metadata)
	}
}

func TestClient_DeleteDNSPolicy_NotFound(t *testing.T) {
	fake := newFakeController(t)
	server := fake.server()

	client := NewClient(server.URL, testAPIKey)
	err := client.DeleteDNSPolicy(context.Background(), testSiteID, "00000000-0000-4000-8000-000000000099")
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestClient_UpdateDNSPolicy_RequiresID(t *testing.T) {
	client := NewClient("http://unused", testAPIKey)
	err := client.UpdateDNSPolicy(context.Background(), testSiteID, "", dnsPolicy{Type: policyTypeA, Domain: "a.example.com"})
	if err == nil {
		t.Fatal("expected error for empty policy id")
	}
}

func TestMapStatusError(t *testing.T) {
	jsonBody := []byte(`{"statusCode":401,"statusName":"UNAUTHORIZED","code":"api.authentication.missing-credentials","message":"Missing credentials"}`)
	htmlBody := []byte(`<html><body><h1>405 Method Not Allowed</h1></body></html>`)

	tests := []struct {
		name     string
		status   int
		body     []byte
		wantIs   error
		wantText string
	}{
		{name: "200 is nil", status: http.StatusOK, body: jsonBody},
		{name: "201 is nil", status: http.StatusCreated, body: nil},
		{name: "401 unauthorized with JSON detail", status: http.StatusUnauthorized, body: jsonBody, wantIs: provider.ErrUnauthorized, wantText: "api.authentication.missing-credentials: Missing credentials"},
		{name: "403 unauthorized", status: http.StatusForbidden, body: nil, wantIs: provider.ErrUnauthorized},
		{name: "404 not found", status: http.StatusNotFound, body: nil, wantIs: provider.ErrNotFound},
		{name: "409 conflict", status: http.StatusConflict, body: nil, wantIs: provider.ErrConflict},
		{name: "400 record-already-exists is a conflict", status: http.StatusBadRequest, body: []byte(`{"statusCode":400,"statusName":"BAD_REQUEST","code":"api.dns.policy.validation.record-already-exists","message":"DNS policy with the same type and properties already exists"}`), wantIs: provider.ErrConflict, wantText: "already exists"},
		{name: "401 from the UniFi OS proxy uses its message", status: http.StatusUnauthorized, body: []byte(`{"error":{"code":401,"message":"Unauthorized"}}`), wantIs: provider.ErrUnauthorized, wantText: "401: Unauthorized"},
		{name: "500 unavailable", status: http.StatusInternalServerError, body: nil, wantIs: provider.ErrProviderUnavailable},
		{name: "405 with HTML body falls back to raw body", status: http.StatusMethodNotAllowed, body: htmlBody, wantText: "405 Method Not Allowed"},
		{name: "400 plain", status: http.StatusBadRequest, body: []byte(`{"message":"bad domain"}`), wantText: "bad domain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mapStatusError(tt.status, tt.body)
			if tt.wantIs == nil && tt.wantText == "" {
				if err != nil {
					t.Fatalf("expected nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("errors.Is(%v, %v) = false", err, tt.wantIs)
			}
			if tt.wantText != "" && !strings.Contains(err.Error(), tt.wantText) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantText)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		header string
		want   time.Duration
	}{
		{"", time.Second},
		{"garbage", time.Second},
		{"-5", time.Second},
		{"0", 0},
		{"2", 2 * time.Second},
		{"3600", maxRetryDelay},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			if got := parseRetryAfter(tt.header); got != tt.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.header, got, tt.want)
			}
		})
	}
}
