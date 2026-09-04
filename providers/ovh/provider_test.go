package ovh

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

func testProvider(t *testing.T, serverURL string) *Provider {
	t.Helper()
	p, err := New("test-ovh", &Config{
		ApplicationKey:    testAppKey,
		ApplicationSecret: testAppSecret,
		ConsumerKey:       testConsumer,
		Zone:              "example.com",
		Endpoint:          DefaultEndpoint,
		TTL:               DefaultTTL,
	})
	if err != nil {
		t.Fatalf("creating provider: %v", err)
	}
	// Point the client at the test server.
	p.client = newTestClient(serverURL)
	return p
}

func TestProvider_NameTypeIdentity(t *testing.T) {
	p, err := New("test-ovh", validConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "test-ovh" {
		t.Errorf("Name() = %q, want test-ovh", p.Name())
	}
	if p.Type() != "ovh" {
		t.Errorf("Type() = %q, want ovh", p.Type())
	}
	id := p.Identity()
	if id.Type != "ovh" || id.Zone != "example.com" || id.Endpoint != DefaultEndpoint {
		t.Errorf("unexpected identity: %+v", id)
	}
}

func TestProvider_Capabilities(t *testing.T) {
	p, _ := New("test-ovh", validConfig())
	caps := p.Capabilities()
	if !caps.SupportsOwnershipTXT || !caps.SupportsNativeUpdate {
		t.Error("expected ownership TXT and native update support")
	}
	for _, rt := range []provider.RecordType{
		provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME,
		provider.RecordTypeTXT, provider.RecordTypeSRV,
	} {
		if !caps.SupportsRecordType(rt) {
			t.Errorf("expected support for %s", rt)
		}
	}
}

func TestProvider_SubDomainConversion(t *testing.T) {
	p, _ := New("test-ovh", validConfig())
	tests := []struct {
		hostname string
		sub      string
	}{
		{"app.example.com", "app"},
		{"app.example.com.", "app"},
		{"example.com", ""},
		{"a.b.example.com", "a.b"},
		{"_minecraft._tcp.example.com", "_minecraft._tcp"},
	}
	for _, tt := range tests {
		if got := p.toSubDomain(tt.hostname); got != tt.sub {
			t.Errorf("toSubDomain(%q) = %q, want %q", tt.hostname, got, tt.sub)
		}
		// Round-trip FQDN reconstruction.
		if got := p.toFQDN(tt.sub); got != strings.TrimSuffix(tt.hostname, ".") {
			t.Errorf("toFQDN(%q) = %q, want %q", tt.sub, got, strings.TrimSuffix(tt.hostname, "."))
		}
	}
}

func TestProvider_Ping(t *testing.T) {
	srv := ovhMux(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"server":"dns.ovh.net"}`))
	})
	defer srv.Close()

	p := testProvider(t, srv.URL)
	if err := p.Ping(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProvider_List(t *testing.T) {
	records := map[int64]ovhRecord{
		1: {ID: 1, Zone: "example.com", SubDomain: "app", FieldType: "A", Target: "10.0.0.1", TTL: 3600},
		2: {ID: 2, Zone: "example.com", SubDomain: "_minecraft._tcp", FieldType: "SRV", Target: "10 20 25565 mc.example.com", TTL: 3600},
		3: {ID: 3, Zone: "example.com", SubDomain: "_dnsweaver.app", FieldType: "TXT", Target: `"heritage=dnsweaver"`, TTL: 3600},
	}
	byType := map[string][]int64{"A": {1}, "SRV": {2}, "TXT": {3}}

	srv := ovhMux(t, func(w http.ResponseWriter, r *http.Request) {
		// Record detail: /domain/zone/example.com/record/{id}
		if strings.Contains(r.URL.Path, "/record/") {
			parts := strings.Split(r.URL.Path, "/")
			idStr := parts[len(parts)-1]
			for id, rec := range records {
				if idStr == strconv.FormatInt(id, 10) {
					_ = json.NewEncoder(w).Encode(rec)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Record list by type.
		ft := r.URL.Query().Get("fieldType")
		_ = json.NewEncoder(w).Encode(byType[ft])
	})
	defer srv.Close()

	p := testProvider(t, srv.URL)
	recs, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("expected 3 records, got %d", len(recs))
	}

	byHost := map[string]provider.Record{}
	for _, r := range recs {
		byHost[r.Hostname] = r
	}

	a := byHost["app.example.com"]
	if a.Type != provider.RecordTypeA || a.Target != "10.0.0.1" {
		t.Errorf("unexpected A record: %+v", a)
	}

	srvRec := byHost["_minecraft._tcp.example.com"]
	if srvRec.SRV == nil || srvRec.SRV.Port != 25565 || srvRec.Target != "mc.example.com" {
		t.Errorf("unexpected SRV record: %+v (srv=%+v)", srvRec, srvRec.SRV)
	}

	txt := byHost["_dnsweaver.app.example.com"]
	if txt.Target != "heritage=dnsweaver" {
		t.Errorf("expected unquoted TXT, got %q", txt.Target)
	}
}

func TestProvider_CreateAndRefresh(t *testing.T) {
	var mu sync.Mutex
	var created recordCreateRequest
	refreshed := false

	srv := ovhMux(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/refresh"):
			refreshed = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&created)
			_ = json.NewEncoder(w).Encode(ovhRecord{ID: 9})
		default:
			w.WriteHeader(http.StatusOK)
		}
	})
	defer srv.Close()

	p := testProvider(t, srv.URL)
	err := p.Create(context.Background(), provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
		TTL:      120,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if created.SubDomain != "app" || created.FieldType != "A" || created.Target != "10.0.0.1" || created.TTL != 120 {
		t.Errorf("unexpected create request: %+v", created)
	}
	if !refreshed {
		t.Error("expected zone refresh after create")
	}
}

func TestProvider_CreateSRV(t *testing.T) {
	var created recordCreateRequest
	srv := ovhMux(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/refresh") {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&created)
			_ = json.NewEncoder(w).Encode(ovhRecord{ID: 9})
		}
	})
	defer srv.Close()

	p := testProvider(t, srv.URL)
	err := p.Create(context.Background(), provider.Record{
		Hostname: "_minecraft._tcp.example.com",
		Type:     provider.RecordTypeSRV,
		Target:   "mc.example.com",
		SRV:      &provider.SRVData{Priority: 10, Weight: 20, Port: 25565},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.Target != "10 20 25565 mc.example.com" {
		t.Errorf("unexpected SRV target: %q", created.Target)
	}
}

func TestProvider_CreateSRV_MissingData(t *testing.T) {
	p := testProvider(t, "http://unused")
	err := p.Create(context.Background(), provider.Record{
		Hostname: "_minecraft._tcp.example.com",
		Type:     provider.RecordTypeSRV,
		Target:   "mc.example.com",
	})
	if err == nil {
		t.Fatal("expected error for SRV without data")
	}
}

func TestProvider_Delete(t *testing.T) {
	deleted := false
	refreshed := false
	srv := ovhMux(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/refresh"):
			refreshed = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/record"):
			_ = json.NewEncoder(w).Encode([]int64{42})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/record/42"):
			_ = json.NewEncoder(w).Encode(ovhRecord{ID: 42, SubDomain: "app", FieldType: "A", Target: "10.0.0.1"})
		case r.Method == http.MethodDelete:
			deleted = true
			w.WriteHeader(http.StatusOK)
		}
	})
	defer srv.Close()

	p := testProvider(t, srv.URL)
	err := p.Delete(context.Background(), provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleted {
		t.Error("expected record to be deleted")
	}
	if !refreshed {
		t.Error("expected zone refresh after delete")
	}
}

func TestProvider_Delete_NotFound(t *testing.T) {
	srv := ovhMux(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/record") {
			_ = json.NewEncoder(w).Encode([]int64{})
			return
		}
		t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
	})
	defer srv.Close()

	p := testProvider(t, srv.URL)
	// Deleting a missing record is a no-op, not an error.
	err := p.Delete(context.Background(), provider.Record{
		Hostname: "missing.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProvider_Delete_SelectsMatchingTarget(t *testing.T) {
	var deletedPath string
	srv := ovhMux(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/refresh"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/record"):
			_ = json.NewEncoder(w).Encode([]int64{41, 42})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/record/41"):
			_ = json.NewEncoder(w).Encode(ovhRecord{ID: 41, SubDomain: "app", FieldType: "A", Target: "10.0.0.2"})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/record/42"):
			_ = json.NewEncoder(w).Encode(ovhRecord{ID: 42, SubDomain: "app", FieldType: "A", Target: "10.0.0.1"})
		case r.Method == http.MethodDelete:
			deletedPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	p := testProvider(t, srv.URL)
	err := p.Delete(context.Background(), provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !strings.HasSuffix(deletedPath, "/record/42") {
		t.Fatalf("deleted path %q, want exact target record 42", deletedPath)
	}
}

func TestProvider_Delete_DoesNotDeleteWhenTargetMissing(t *testing.T) {
	deleteCalled := false
	refreshCalled := false
	srv := ovhMux(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/refresh"):
			refreshCalled = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/record"):
			_ = json.NewEncoder(w).Encode([]int64{41})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/record/41"):
			_ = json.NewEncoder(w).Encode(ovhRecord{ID: 41, SubDomain: "app", FieldType: "A", Target: "10.0.0.2"})
		case r.Method == http.MethodDelete:
			deleteCalled = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()

	p := testProvider(t, srv.URL)
	err := p.Delete(context.Background(), provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if deleteCalled || refreshCalled {
		t.Fatal("Delete() mutated the zone when the requested target was absent")
	}
}

func TestOVHTargetsEqual(t *testing.T) {
	tests := []struct {
		name       string
		recordType provider.RecordType
		actual     string
		desired    string
		want       bool
	}{
		{name: "quoted TXT", recordType: provider.RecordTypeTXT, actual: `"heritage=dnsweaver"`, desired: "heritage=dnsweaver", want: true},
		{name: "CNAME trailing dot", recordType: provider.RecordTypeCNAME, actual: "target.example.com.", desired: "target.example.com", want: true},
		{name: "SRV trailing dot", recordType: provider.RecordTypeSRV, actual: "10 20 5060 sip.example.com.", desired: "10 20 5060 sip.example.com", want: true},
		{name: "different SRV member", recordType: provider.RecordTypeSRV, actual: "10 20 5060 sip-2.example.com.", desired: "10 20 5060 sip-1.example.com", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ovhTargetsEqual(tt.recordType, tt.actual, tt.desired); got != tt.want {
				t.Fatalf("ovhTargetsEqual(%q, %q) = %v, want %v", tt.actual, tt.desired, got, tt.want)
			}
		})
	}
}

func TestProvider_Update(t *testing.T) {
	var updated recordUpdateRequest
	updateCalled := false
	srv := ovhMux(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/refresh"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/record"):
			_ = json.NewEncoder(w).Encode([]int64{7})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/record/7"):
			_ = json.NewEncoder(w).Encode(ovhRecord{ID: 7, SubDomain: "app", FieldType: "A", Target: "10.0.0.1"})
		case r.Method == http.MethodPut:
			updateCalled = true
			_ = json.NewDecoder(r.Body).Decode(&updated)
			w.WriteHeader(http.StatusOK)
		}
	})
	defer srv.Close()

	p := testProvider(t, srv.URL)
	err := p.Update(context.Background(),
		provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"},
		provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.2", TTL: 300},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updateCalled {
		t.Fatal("expected PUT update call")
	}
	if updated.Target != "10.0.0.2" || updated.TTL != 300 {
		t.Errorf("unexpected update body: %+v", updated)
	}
}

func TestProvider_Update_NotFound(t *testing.T) {
	srv := ovhMux(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/record") {
			_ = json.NewEncoder(w).Encode([]int64{})
			return
		}
	})
	defer srv.Close()

	p := testProvider(t, srv.URL)
	err := p.Update(context.Background(),
		provider.Record{Hostname: "missing.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"},
		provider.Record{Hostname: "missing.example.com", Type: provider.RecordTypeA, Target: "10.0.0.2"},
	)
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFactory(t *testing.T) {
	factory := Factory()
	p, err := factory(provider.FactoryConfig{
		Name: "test",
		ProviderConfig: map[string]string{
			"APPLICATION_KEY":    "ak",
			"APPLICATION_SECRET": "as",
			"CONSUMER_KEY":       "ck",
			"ZONE":               "example.com",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "test" {
		t.Errorf("expected name test, got %s", p.Name())
	}
	if p.Type() != "ovh" {
		t.Errorf("expected type ovh, got %s", p.Type())
	}
}

func TestFactory_InvalidConfig(t *testing.T) {
	factory := Factory()
	_, err := factory(provider.FactoryConfig{
		Name:           "test",
		ProviderConfig: map[string]string{"APPLICATION_KEY": "ak"},
	})
	if err == nil {
		t.Fatal("expected error for incomplete config")
	}
}
