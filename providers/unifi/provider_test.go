package unifi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

func newTestProvider(t *testing.T, serverURL string, zone string) *Provider {
	t.Helper()
	config := &Config{
		URL:    serverURL,
		APIKey: testAPIKey,
		Site:   "default",
		Zone:   zone,
		TTL:    300,
	}
	p, err := New("test-provider", config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	return p
}

func TestProvider_Name(t *testing.T) {
	p := newTestProvider(t, "https://unifi.local", "")
	if p.Name() != "test-provider" {
		t.Errorf("expected name 'test-provider', got %s", p.Name())
	}
}

func TestProvider_Type(t *testing.T) {
	p := newTestProvider(t, "https://unifi.local", "")
	if p.Type() != "unifi" {
		t.Errorf("expected type 'unifi', got %s", p.Type())
	}
}

func TestProvider_Zone(t *testing.T) {
	p := newTestProvider(t, "https://unifi.local", "home.example.com")
	if p.Zone() != "home.example.com" {
		t.Errorf("expected zone 'home.example.com', got %s", p.Zone())
	}
}

func TestProvider_Identity(t *testing.T) {
	p := newTestProvider(t, "https://unifi.local", "home.example.com")
	identity := p.Identity()
	want := provider.ProviderIdentity{Type: "unifi", Endpoint: "https://unifi.local/sites/default", Zone: "home.example.com"}
	if identity != want {
		t.Errorf("Identity() = %+v, want %+v", identity, want)
	}

	other, _ := New("other", &Config{URL: "https://unifi.local", APIKey: testAPIKey, Site: "branch", Zone: "home.example.com"})
	if other.Identity() == identity {
		t.Error("instances on different sites must have distinct identities")
	}
}

func TestProvider_Capabilities(t *testing.T) {
	p := newTestProvider(t, "https://unifi.local", "")
	caps := p.Capabilities()

	if !caps.SupportsOwnershipTXT {
		t.Error("SupportsOwnershipTXT should be true")
	}
	if !caps.SupportsNativeUpdate {
		t.Error("SupportsNativeUpdate should be true")
	}

	for _, rt := range []provider.RecordType{provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME, provider.RecordTypeTXT, provider.RecordTypeSRV} {
		if !caps.SupportsRecordType(rt) {
			t.Errorf("missing supported record type: %s", rt)
		}
	}
	if caps.SupportsRecordType(provider.RecordTypeHTTPS) {
		t.Error("HTTPS records must not be reported as supported")
	}
}

func TestFactory(t *testing.T) {
	p, err := Factory()(provider.FactoryConfig{
		Name: "unifi-dns",
		ProviderConfig: map[string]string{
			"URL":     "https://192.168.1.1",
			"API_KEY": testAPIKey,
			"SITE":    "branch",
			"ZONE":    "home.example.com",
		},
	})
	if err != nil {
		t.Fatalf("Factory() error = %v", err)
	}

	if p.Name() != "unifi-dns" {
		t.Errorf("expected name unifi-dns, got %s", p.Name())
	}
	if p.Type() != "unifi" {
		t.Errorf("expected type unifi, got %s", p.Type())
	}

	identity := provider.IdentityOf(p)
	if identity.Endpoint != "https://192.168.1.1/sites/branch" || identity.Zone != "home.example.com" {
		t.Fatalf("Identity() = %+v, want configured URL, site and zone", identity)
	}
}

func TestFactory_InvalidConfig(t *testing.T) {
	_, err := Factory()(provider.FactoryConfig{
		Name:           "bad",
		ProviderConfig: map[string]string{"URL": "https://192.168.1.1"},
	})
	if err == nil || !strings.Contains(err.Error(), "API_KEY is required") {
		t.Fatalf("expected API_KEY validation error, got %v", err)
	}
}

func TestProvider_New_NilConfig(t *testing.T) {
	if _, err := New("test", nil); err == nil {
		t.Error("expected error for nil config, got nil")
	}
}

func TestProvider_New_InvalidConfig(t *testing.T) {
	if _, err := New("test", &Config{}); err == nil {
		t.Error("expected error for invalid config, got nil")
	}
}

func TestProvider_WithProviderHTTPClient(t *testing.T) {
	custom := &http.Client{}
	p := newTestProvider(t, "https://unifi.local", "")
	WithProviderHTTPClient(custom)(p)
	if p.client.httpClient != custom {
		t.Error("expected custom HTTP client to be applied to the API client")
	}
	WithProviderHTTPClient(nil)(p)
	if p.client.httpClient != custom {
		t.Error("nil HTTP client must be ignored")
	}
}

func TestProvider_WithProviderLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := newTestProvider(t, "https://unifi.local", "")
	WithProviderLogger(logger)(p)
	if p.logger != logger || p.client.logger != logger {
		t.Error("expected logger to be applied to both the provider and the API client")
	}
	WithProviderLogger(nil)(p)
	if p.logger != logger || p.client.logger != logger {
		t.Error("nil logger must be ignored")
	}
}

func TestProvider_Ping(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  string
		site    string
		version string
		wantErr error
		wantMsg string
	}{
		{name: "success", apiKey: testAPIKey, site: "default"},
		{name: "bad key", apiKey: "wrong", site: "default", wantErr: provider.ErrUnauthorized},
		{name: "unknown site", apiKey: testAPIKey, site: "missing", wantMsg: `site "missing" not found`},
		{name: "console too old", apiKey: testAPIKey, site: "default", version: "9.3.45", wantMsg: "version 10.1 or later is required"},
		{name: "minimum version passes", apiKey: testAPIKey, site: "default", version: "10.1.68"},
		{name: "unparsable version passes", apiKey: testAPIKey, site: "default", version: "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeController(t)
			if tt.version != "" {
				fake.version = tt.version
			}
			server := fake.server()

			p, err := New("test", &Config{URL: server.URL, APIKey: tt.apiKey, Site: tt.site, TTL: 300})
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}

			err = p.Ping(context.Background())
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Ping() error = %v, want %v", err, tt.wantErr)
				}
			case tt.wantMsg != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
					t.Fatalf("Ping() error = %v, want containing %q", err, tt.wantMsg)
				}
			default:
				if err != nil {
					t.Fatalf("Ping() unexpected error: %v", err)
				}
				if p.siteID != testSiteID {
					t.Errorf("site id not cached after Ping: %q", p.siteID)
				}
			}
		})
	}
}

func TestProvider_SiteResolvedOnce(t *testing.T) {
	fake := newFakeController(t)
	server := fake.server()
	p := newTestProvider(t, server.URL, "")

	for i := 0; i < 3; i++ {
		if _, err := p.List(context.Background()); err != nil {
			t.Fatalf("List() error: %v", err)
		}
	}
	if got := fake.requestCount(http.MethodGet, "/sites"); got != 1 {
		t.Errorf("GET /sites requests = %d, want 1 (site id should be cached)", got)
	}
}

func TestProvider_List(t *testing.T) {
	tests := []struct {
		name            string
		zone            string
		keepNilMetadata bool // seed without stamping USER_DEFINED metadata
		policies        []dnsPolicy
		want            []provider.Record
	}{
		{
			name: "every managed type",
			policies: []dnsPolicy{
				{Type: policyTypeA, Enabled: true, Domain: "a.example.com", IPv4Address: "10.0.0.1", TTLSeconds: intPtr(600)},
				{Type: policyTypeAAAA, Enabled: true, Domain: "aaaa.example.com", IPv6Address: "2001:db8::1", TTLSeconds: intPtr(600)},
				{Type: policyTypeCNAME, Enabled: true, Domain: "alias.example.com", TargetDomain: "a.example.com", TTLSeconds: intPtr(120)},
				{Type: policyTypeTXT, Enabled: true, Domain: "_dnsweaver.a.example.com", Text: `"heritage=dnsweaver,instance=home"`},
				{Type: policyTypeSRV, Enabled: true, Domain: "example.com", Service: "_minecraft", Protocol: "_tcp", ServerDomain: "mc.example.com", Port: intPtr(25565), Priority: intPtr(10), Weight: intPtr(5)},
			},
			want: []provider.Record{
				{Hostname: "a.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 600},
				{Hostname: "aaaa.example.com", Type: provider.RecordTypeAAAA, Target: "2001:db8::1", TTL: 600},
				{Hostname: "alias.example.com", Type: provider.RecordTypeCNAME, Target: "a.example.com", TTL: 120},
				{Hostname: "_dnsweaver.a.example.com", Type: provider.RecordTypeTXT, Target: "heritage=dnsweaver,instance=home", TTL: 300},
				{Hostname: "_minecraft._tcp.example.com", Type: provider.RecordTypeSRV, Target: "mc.example.com", TTL: 300, SRV: &provider.SRVData{Priority: 10, Weight: 5, Port: 25565}},
			},
		},
		{
			name: "skips disabled, system-derived, MX and forward domain policies",
			policies: []dnsPolicy{
				{Type: policyTypeA, Enabled: true, Domain: "keep.example.com", IPv4Address: "10.0.0.1", TTLSeconds: intPtr(300)},
				{Type: policyTypeA, Enabled: false, Domain: "disabled.example.com", IPv4Address: "10.0.0.2", TTLSeconds: intPtr(300)},
				{Type: policyTypeA, Enabled: true, Domain: "printer.example.com", IPv4Address: "10.0.0.3", TTLSeconds: intPtr(300), Metadata: &policyMetadata{Origin: "SYSTEM_DEFINED"}},
				{Type: policyTypeA, Enabled: true, Domain: "device.example.com", IPv4Address: "10.0.0.4", TTLSeconds: intPtr(300), Metadata: &policyMetadata{Origin: "DERIVED"}},
				{Type: policyTypeMX, Enabled: true, Domain: "example.com"},
				{Type: policyTypeForwardDomain, Enabled: true, Domain: "corp.example.com"},
			},
			want: []provider.Record{
				{Hostname: "keep.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300},
			},
		},
		{
			name: "zone filtering",
			zone: "home.example.com",
			policies: []dnsPolicy{
				{Type: policyTypeA, Enabled: true, Domain: "nas.home.example.com", IPv4Address: "10.0.0.1", TTLSeconds: intPtr(300)},
				{Type: policyTypeA, Enabled: true, Domain: "home.example.com", IPv4Address: "10.0.0.2", TTLSeconds: intPtr(300)},
				{Type: policyTypeA, Enabled: true, Domain: "other.example.com", IPv4Address: "10.0.0.3", TTLSeconds: intPtr(300)},
				{Type: policyTypeA, Enabled: true, Domain: "nothome.example.com", IPv4Address: "10.0.0.4", TTLSeconds: intPtr(300)},
				{Type: policyTypeA, Enabled: true, Domain: "Printer.HOME.example.com", IPv4Address: "10.0.0.5", TTLSeconds: intPtr(300)},
			},
			want: []provider.Record{
				{Hostname: "nas.home.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300},
				{Hostname: "home.example.com", Type: provider.RecordTypeA, Target: "10.0.0.2", TTL: 300},
				{Hostname: "Printer.HOME.example.com", Type: provider.RecordTypeA, Target: "10.0.0.5", TTL: 300},
			},
		},
		{
			name:            "missing metadata (pre-10.3 console) is treated as user-defined",
			keepNilMetadata: true,
			policies: []dnsPolicy{
				{Type: policyTypeA, Enabled: true, Domain: "old.example.com", IPv4Address: "10.0.0.1", TTLSeconds: intPtr(300), Metadata: nil},
			},
			want: []provider.Record{
				{Hostname: "old.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300},
			},
		},
		{
			name: "missing ttlSeconds falls back to instance TTL",
			policies: []dnsPolicy{
				{Type: policyTypeA, Enabled: true, Domain: "nottl.example.com", IPv4Address: "10.0.0.1"},
			},
			want: []provider.Record{
				{Hostname: "nottl.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeController(t)
			for _, pol := range tt.policies {
				if tt.keepNilMetadata {
					fake.seedRaw(pol)
				} else {
					fake.seed(pol)
				}
			}
			server := fake.server()

			p := newTestProvider(t, server.URL, tt.zone)
			records, err := p.List(context.Background())
			if err != nil {
				t.Fatalf("List() error: %v", err)
			}

			if len(records) != len(tt.want) {
				t.Fatalf("List() returned %d records, want %d: %+v", len(records), len(tt.want), records)
			}
			for i, want := range tt.want {
				got := records[i]
				if got.ProviderID == "" {
					t.Errorf("record %d has no ProviderID", i)
				}
				got.ProviderID = ""
				if !provider.RecordEquals(got, want) {
					t.Errorf("record %d = %+v, want %+v", i, got, want)
				}
			}
		})
	}
}

func TestProvider_Create(t *testing.T) {
	tests := []struct {
		name    string
		record  provider.Record
		want    dnsPolicy // stored wire shape (id and metadata ignored)
		wantErr string
	}{
		{
			name:   "A record with record TTL",
			record: provider.Record{Hostname: "a.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 60},
			want:   dnsPolicy{Type: policyTypeA, Enabled: true, Domain: "a.example.com", IPv4Address: "10.0.0.1", TTLSeconds: intPtr(60)},
		},
		{
			name:   "A record falls back to instance TTL",
			record: provider.Record{Hostname: "a.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"},
			want:   dnsPolicy{Type: policyTypeA, Enabled: true, Domain: "a.example.com", IPv4Address: "10.0.0.1", TTLSeconds: intPtr(300)},
		},
		{
			name:   "AAAA record",
			record: provider.Record{Hostname: "v6.example.com", Type: provider.RecordTypeAAAA, Target: "2001:db8::1"},
			want:   dnsPolicy{Type: policyTypeAAAA, Enabled: true, Domain: "v6.example.com", IPv6Address: "2001:db8::1", TTLSeconds: intPtr(300)},
		},
		{
			name:   "CNAME record",
			record: provider.Record{Hostname: "alias.example.com", Type: provider.RecordTypeCNAME, Target: "a.example.com"},
			want:   dnsPolicy{Type: policyTypeCNAME, Enabled: true, Domain: "alias.example.com", TargetDomain: "a.example.com", TTLSeconds: intPtr(300)},
		},
		{
			name:   "TXT ownership record is quoted and carries no TTL",
			record: provider.Record{Hostname: "_dnsweaver.a.example.com", Type: provider.RecordTypeTXT, Target: "heritage=dnsweaver,instance=home"},
			want:   dnsPolicy{Type: policyTypeTXT, Enabled: true, Domain: "_dnsweaver.a.example.com", Text: `"heritage=dnsweaver,instance=home"`},
		},
		{
			name:   "TXT without commas is not quoted",
			record: provider.Record{Hostname: "txt.example.com", Type: provider.RecordTypeTXT, Target: "v=spf1 -all"},
			want:   dnsPolicy{Type: policyTypeTXT, Enabled: true, Domain: "txt.example.com", Text: "v=spf1 -all"},
		},
		{
			name:   "SRV record is split into service, protocol and domain",
			record: provider.Record{Hostname: "_minecraft._tcp.example.com", Type: provider.RecordTypeSRV, Target: "mc.example.com", SRV: &provider.SRVData{Priority: 10, Weight: 5, Port: 25565}},
			want:   dnsPolicy{Type: policyTypeSRV, Enabled: true, Domain: "example.com", Service: "_minecraft", Protocol: "_tcp", ServerDomain: "mc.example.com", Port: intPtr(25565), Priority: intPtr(10), Weight: intPtr(5)},
		},
		{
			name:    "A record with an IPv6 target",
			record:  provider.Record{Hostname: "a.example.com", Type: provider.RecordTypeA, Target: "2001:db8::1"},
			wantErr: "not an IPv4 address",
		},
		{
			name:    "AAAA record with an IPv4 target",
			record:  provider.Record{Hostname: "v6.example.com", Type: provider.RecordTypeAAAA, Target: "10.0.0.1"},
			wantErr: "not an IPv6 address",
		},
		{
			name:    "A record with a hostname target",
			record:  provider.Record{Hostname: "a.example.com", Type: provider.RecordTypeA, Target: "target.example.com"},
			wantErr: "not an IPv4 address",
		},
		{
			name:    "TXT that is quoted and contains a comma is rejected",
			record:  provider.Record{Hostname: "txt.example.com", Type: provider.RecordTypeTXT, Target: `"a,b"`},
			wantErr: "cannot store losslessly",
		},
		{
			name:    "SRV without data",
			record:  provider.Record{Hostname: "_sip._tcp.example.com", Type: provider.RecordTypeSRV, Target: "sip.example.com"},
			wantErr: "SRV data is required",
		},
		{
			name:    "SRV with malformed hostname",
			record:  provider.Record{Hostname: "sip.example.com", Type: provider.RecordTypeSRV, Target: "sip.example.com", SRV: &provider.SRVData{Port: 5060}},
			wantErr: "_service._proto.domain",
		},
		{
			name:    "unsupported HTTPS record",
			record:  provider.Record{Hostname: "a.example.com", Type: provider.RecordTypeHTTPS, Target: ".", HTTPS: &provider.HTTPSData{Priority: 1, TargetName: ".", ALPN: "h2"}},
			wantErr: "unsupported record type",
		},
		{
			name:    "domain over 127 characters",
			record:  provider.Record{Hostname: strings.Repeat("a", 120) + ".example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"},
			wantErr: "127 character limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeController(t)
			server := fake.server()
			p := newTestProvider(t, server.URL, "")

			err := p.Create(context.Background(), tt.record)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Create() error = %v, want containing %q", err, tt.wantErr)
				}
				if fake.count() != 0 {
					t.Errorf("expected no policy to be created on error, got %d", fake.count())
				}
				return
			}
			if err != nil {
				t.Fatalf("Create() error: %v", err)
			}
			if fake.count() != 1 {
				t.Fatalf("expected 1 stored policy, got %d", fake.count())
			}

			got := fake.first()
			got.ID, got.Metadata = "", nil
			assertPolicyEqual(t, got, tt.want)
		})
	}
}

func TestProvider_Update(t *testing.T) {
	t.Run("uses ProviderID from List", func(t *testing.T) {
		fake := newFakeController(t)
		id := fake.seed(dnsPolicy{Type: policyTypeA, Enabled: true, Domain: "a.example.com", IPv4Address: "10.0.0.1", TTLSeconds: intPtr(300)})
		server := fake.server()
		p := newTestProvider(t, server.URL, "")

		existing := provider.Record{Hostname: "a.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300, ProviderID: id}
		desired := provider.Record{Hostname: "a.example.com", Type: provider.RecordTypeA, Target: "10.0.0.2", TTL: 120}
		if err := p.Update(context.Background(), existing, desired); err != nil {
			t.Fatalf("Update() error: %v", err)
		}

		stored, ok := fake.get(id)
		if !ok {
			t.Fatal("policy disappeared after update")
		}
		if stored.IPv4Address != "10.0.0.2" || stored.TTLSeconds == nil || *stored.TTLSeconds != 120 {
			t.Errorf("stored policy = %+v, want target 10.0.0.2 ttl 120", stored)
		}
		if got := fake.requestCount(http.MethodGet, "/dns/policies"); got != 0 {
			t.Errorf("expected no list lookup when ProviderID is set, got %d", got)
		}
	})

	t.Run("looks up the policy when ProviderID is empty", func(t *testing.T) {
		fake := newFakeController(t)
		fake.seed(dnsPolicy{Type: policyTypeA, Enabled: true, Domain: "other.example.com", IPv4Address: "10.0.0.9", TTLSeconds: intPtr(300)})
		id := fake.seed(dnsPolicy{Type: policyTypeCNAME, Enabled: true, Domain: "alias.example.com", TargetDomain: "old.example.com", TTLSeconds: intPtr(300)})
		server := fake.server()
		p := newTestProvider(t, server.URL, "")

		existing := provider.Record{Hostname: "alias.example.com", Type: provider.RecordTypeCNAME, Target: "old.example.com"}
		desired := provider.Record{Hostname: "alias.example.com", Type: provider.RecordTypeCNAME, Target: "new.example.com"}
		if err := p.Update(context.Background(), existing, desired); err != nil {
			t.Fatalf("Update() error: %v", err)
		}

		stored, _ := fake.get(id)
		if stored.TargetDomain != "new.example.com" {
			t.Errorf("stored target = %q, want new.example.com", stored.TargetDomain)
		}
	})

	t.Run("SRV update matches on SRV fields", func(t *testing.T) {
		fake := newFakeController(t)
		fake.seed(dnsPolicy{Type: policyTypeSRV, Enabled: true, Domain: "example.com", Service: "_sip", Protocol: "_tcp", ServerDomain: "sip.example.com", Port: intPtr(5061), Priority: intPtr(0), Weight: intPtr(0)})
		id := fake.seed(dnsPolicy{Type: policyTypeSRV, Enabled: true, Domain: "example.com", Service: "_sip", Protocol: "_tcp", ServerDomain: "sip.example.com", Port: intPtr(5060), Priority: intPtr(0), Weight: intPtr(0)})
		server := fake.server()
		p := newTestProvider(t, server.URL, "")

		existing := provider.Record{Hostname: "_sip._tcp.example.com", Type: provider.RecordTypeSRV, Target: "sip.example.com", SRV: &provider.SRVData{Port: 5060}}
		desired := provider.Record{Hostname: "_sip._tcp.example.com", Type: provider.RecordTypeSRV, Target: "sip2.example.com", SRV: &provider.SRVData{Port: 5060}}
		if err := p.Update(context.Background(), existing, desired); err != nil {
			t.Fatalf("Update() error: %v", err)
		}

		stored, _ := fake.get(id)
		if stored.ServerDomain != "sip2.example.com" {
			t.Errorf("stored server domain = %q, want sip2.example.com", stored.ServerDomain)
		}
	})

	t.Run("returns ErrNotFound when no policy matches", func(t *testing.T) {
		fake := newFakeController(t)
		server := fake.server()
		p := newTestProvider(t, server.URL, "")

		existing := provider.Record{Hostname: "ghost.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"}
		desired := provider.Record{Hostname: "ghost.example.com", Type: provider.RecordTypeA, Target: "10.0.0.2"}
		err := p.Update(context.Background(), existing, desired)
		if !errors.Is(err, provider.ErrNotFound) {
			t.Fatalf("Update() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("returns ErrNotFound when the console deleted the policy", func(t *testing.T) {
		fake := newFakeController(t)
		server := fake.server()
		p := newTestProvider(t, server.URL, "")

		existing := provider.Record{Hostname: "gone.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", ProviderID: "00000000-0000-4000-8000-000000000042"}
		desired := provider.Record{Hostname: "gone.example.com", Type: provider.RecordTypeA, Target: "10.0.0.2"}
		err := p.Update(context.Background(), existing, desired)
		if !errors.Is(err, provider.ErrNotFound) {
			t.Fatalf("Update() error = %v, want ErrNotFound", err)
		}
	})
}

func TestProvider_Delete(t *testing.T) {
	t.Run("uses ProviderID from List", func(t *testing.T) {
		fake := newFakeController(t)
		id := fake.seed(dnsPolicy{Type: policyTypeA, Enabled: true, Domain: "a.example.com", IPv4Address: "10.0.0.1", TTLSeconds: intPtr(300)})
		server := fake.server()
		p := newTestProvider(t, server.URL, "")

		record := provider.Record{Hostname: "a.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", ProviderID: id}
		if err := p.Delete(context.Background(), record); err != nil {
			t.Fatalf("Delete() error: %v", err)
		}
		if fake.count() != 0 {
			t.Errorf("expected policy to be deleted, %d remain", fake.count())
		}
	})

	t.Run("looks up the exact member when ProviderID is empty", func(t *testing.T) {
		fake := newFakeController(t)
		keep := fake.seed(dnsPolicy{Type: policyTypeA, Enabled: true, Domain: "a.example.com", IPv4Address: "10.0.0.1", TTLSeconds: intPtr(300)})
		fake.seed(dnsPolicy{Type: policyTypeA, Enabled: true, Domain: "a.example.com", IPv4Address: "10.0.0.2", TTLSeconds: intPtr(300)})
		server := fake.server()
		p := newTestProvider(t, server.URL, "")

		record := provider.Record{Hostname: "a.example.com", Type: provider.RecordTypeA, Target: "10.0.0.2"}
		if err := p.Delete(context.Background(), record); err != nil {
			t.Fatalf("Delete() error: %v", err)
		}
		if fake.count() != 1 {
			t.Fatalf("expected 1 policy to remain, got %d", fake.count())
		}
		if _, ok := fake.get(keep); !ok {
			t.Error("the sibling A record must be preserved")
		}
	})

	t.Run("TXT ownership record matches after unquoting", func(t *testing.T) {
		fake := newFakeController(t)
		fake.seed(dnsPolicy{Type: policyTypeTXT, Enabled: true, Domain: "_dnsweaver.a.example.com", Text: `"heritage=dnsweaver,instance=home"`})
		server := fake.server()
		p := newTestProvider(t, server.URL, "")

		record := provider.Record{Hostname: "_dnsweaver.a.example.com", Type: provider.RecordTypeTXT, Target: "heritage=dnsweaver,instance=home"}
		if err := p.Delete(context.Background(), record); err != nil {
			t.Fatalf("Delete() error: %v", err)
		}
		if fake.count() != 0 {
			t.Errorf("expected TXT policy to be deleted, %d remain", fake.count())
		}
	})

	t.Run("already absent is a no-op", func(t *testing.T) {
		fake := newFakeController(t)
		server := fake.server()
		p := newTestProvider(t, server.URL, "")

		record := provider.Record{Hostname: "ghost.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"}
		if err := p.Delete(context.Background(), record); err != nil {
			t.Fatalf("Delete() of absent record should succeed, got %v", err)
		}
		if got := fake.requestCount(http.MethodDelete, ""); got != 0 {
			t.Errorf("expected no DELETE request, got %d", got)
		}
	})

	t.Run("404 from the console is a no-op", func(t *testing.T) {
		fake := newFakeController(t)
		server := fake.server()
		p := newTestProvider(t, server.URL, "")

		record := provider.Record{Hostname: "gone.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", ProviderID: "00000000-0000-4000-8000-000000000042"}
		if err := p.Delete(context.Background(), record); err != nil {
			t.Fatalf("Delete() with stale id should succeed, got %v", err)
		}
	})
}

func TestProvider_Create_Duplicate(t *testing.T) {
	fake := newFakeController(t)
	server := fake.server()
	p := newTestProvider(t, server.URL, "")

	record := provider.Record{Hostname: "a.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"}
	if err := p.Create(context.Background(), record); err != nil {
		t.Fatalf("first Create() error: %v", err)
	}
	err := p.Create(context.Background(), record)
	if !errors.Is(err, provider.ErrConflict) {
		t.Fatalf("second Create() error = %v, want ErrConflict", err)
	}
	if fake.count() != 1 {
		t.Errorf("expected 1 stored policy, got %d", fake.count())
	}
}

func TestProvider_Create_Unauthorized(t *testing.T) {
	fake := newFakeController(t)
	server := fake.server()
	p, _ := New("test", &Config{URL: server.URL, APIKey: "wrong", Site: "default", TTL: 300})

	err := p.Create(context.Background(), provider.Record{Hostname: "a.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"})
	if !errors.Is(err, provider.ErrUnauthorized) {
		t.Fatalf("Create() error = %v, want ErrUnauthorized", err)
	}
}

func TestQuoteTXT(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{"heritage=dnsweaver", "heritage=dnsweaver"},
		{"heritage=dnsweaver,instance=home", `"heritage=dnsweaver,instance=home"`},
		{"v=spf1 -all", "v=spf1 -all"},
		{`"quoted"`, `"quoted"`},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := quoteTXT(tt.text); got != tt.want {
				t.Errorf("quoteTXT(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestUnquoteTXT(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{`"heritage=dnsweaver,instance=home"`, "heritage=dnsweaver,instance=home"},
		{"heritage=dnsweaver,instance=home", "heritage=dnsweaver,instance=home"},
		{`"hello"`, `"hello"`}, // no comma: quotes were never added by quoteTXT
		{`"`, `"`},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := unquoteTXT(tt.text); got != tt.want {
				t.Errorf("unquoteTXT(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestTXTRoundTrip(t *testing.T) {
	for _, text := range []string{
		"heritage=dnsweaver",
		"heritage=dnsweaver,instance=home",
		"heritage=dnsweaver,instance=home,record-version=2,record-type=A,record-target=MTAuMC4wLjE",
		`"quoted"`,
		"a, b, c",
		`say "hi", friend`,
	} {
		if got := unquoteTXT(quoteTXT(text)); got != text {
			t.Errorf("round trip of %q = %q", text, got)
		}
	}
}

func TestMeetsMinimumVersion(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"10.6.106", true},
		{"10.1.68", true},
		{"10.1", true},
		{"11.0.1", true},
		{"10.0.162", false},
		{"9.3.45", false},
		{"", true},
		{"dev", true},
		{"x.y.z", true},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := meetsMinimumVersion(tt.version); got != tt.want {
				t.Errorf("meetsMinimumVersion(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestProvider_InZone(t *testing.T) {
	p := newTestProvider(t, "https://unifi.local", "Home.Example.com")
	for name, want := range map[string]bool{
		"nas.home.example.com":  true,
		"NAS.HOME.EXAMPLE.COM":  true,
		"nas.home.example.com.": true,
		"home.example.com":      true,
		"nothome.example.com":   false,
		"other.example.com":     false,
	} {
		if got := p.inZone(name); got != want {
			t.Errorf("inZone(%q) = %v, want %v", name, got, want)
		}
	}

	open := newTestProvider(t, "https://unifi.local", "")
	if !open.inZone("anything.example.org") {
		t.Error("with no zone configured every hostname must match")
	}
}

func TestSplitSRVHostname(t *testing.T) {
	tests := []struct {
		hostname     string
		wantService  string
		wantProtocol string
		wantDomain   string
		wantErr      bool
	}{
		{hostname: "_minecraft._tcp.example.com", wantService: "_minecraft", wantProtocol: "_tcp", wantDomain: "example.com"},
		{hostname: "_ldap._tcp.dc.corp.example.com", wantService: "_ldap", wantProtocol: "_tcp", wantDomain: "dc.corp.example.com"},
		{hostname: "_sip._udp.example.com", wantService: "_sip", wantProtocol: "_udp", wantDomain: "example.com"},
		{hostname: "sip._tcp.example.com", wantErr: true},
		{hostname: "_sip.tcp.example.com", wantErr: true},
		{hostname: "_sip._tcp", wantErr: true},
		{hostname: "_sip._tcp.", wantErr: true},
		{hostname: "example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			service, protocol, domain, err := splitSRVHostname(tt.hostname)
			if (err != nil) != tt.wantErr {
				t.Fatalf("splitSRVHostname(%q) error = %v, wantErr %v", tt.hostname, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if service != tt.wantService || protocol != tt.wantProtocol || domain != tt.wantDomain {
				t.Errorf("got (%q, %q, %q), want (%q, %q, %q)", service, protocol, domain, tt.wantService, tt.wantProtocol, tt.wantDomain)
			}
			if joined := joinSRVHostname(service, protocol, domain); joined != tt.hostname {
				t.Errorf("joinSRVHostname = %q, want %q", joined, tt.hostname)
			}
		})
	}
}

func TestPolicyToRecord_Unmanaged(t *testing.T) {
	for _, typ := range []string{policyTypeMX, policyTypeForwardDomain, "NS_RECORD", ""} {
		if _, ok := policyToRecord(dnsPolicy{Type: typ, Enabled: true, Domain: "example.com"}, 300); ok {
			t.Errorf("policyToRecord(%q) should not produce a record", typ)
		}
	}
}

// assertPolicyEqual compares the wire-relevant fields of two policies.
func assertPolicyEqual(t *testing.T, got, want dnsPolicy) {
	t.Helper()
	if got.Type != want.Type || got.Enabled != want.Enabled || got.Domain != want.Domain ||
		got.IPv4Address != want.IPv4Address || got.IPv6Address != want.IPv6Address ||
		got.TargetDomain != want.TargetDomain || got.Text != want.Text ||
		got.Service != want.Service || got.Protocol != want.Protocol || got.ServerDomain != want.ServerDomain {
		t.Errorf("policy = %+v, want %+v", got, want)
	}
	assertIntPtrEqual(t, "ttlSeconds", got.TTLSeconds, want.TTLSeconds)
	assertIntPtrEqual(t, "port", got.Port, want.Port)
	assertIntPtrEqual(t, "priority", got.Priority, want.Priority)
	assertIntPtrEqual(t, "weight", got.Weight, want.Weight)
}

func assertIntPtrEqual(t *testing.T, field string, got, want *int) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil || want == nil:
		t.Errorf("%s = %v, want %v", field, ptrString(got), ptrString(want))
	case *got != *want:
		t.Errorf("%s = %d, want %d", field, *got, *want)
	}
}

func ptrString(v *int) string {
	if v == nil {
		return "<nil>"
	}
	return strconv.Itoa(*v)
}
