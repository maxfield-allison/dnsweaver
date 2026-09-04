package reconciler

import (
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
)

func TestCompileDesiredRecordSets_PreservesDistinctMembersAndClaimCounts(t *testing.T) {
	mock := newTestMockProvider("dns")
	providers := testProviderRegistry(quietLogger(), mock)
	if err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name: "dns", TypeName: "mock", RecordType: provider.RecordTypeA,
		Target: "192.0.2.1", TTL: 300, Domains: []string{"*.example.com"},
	}); err != nil {
		t.Fatalf("CreateInstance() error = %v", err)
	}
	r := New(nil, source.NewRegistry(quietLogger()), providers, WithLogger(quietLogger()))

	claims := []*source.Hostname{
		{Name: "app.example.com", Source: "native", RecordHints: &source.RecordHints{Type: "A", Target: "192.0.2.10"}},
		{Name: "app.example.com", Source: "native", RecordHints: &source.RecordHints{Type: "A", Target: "192.0.2.11"}},
		{Name: "app.example.com", Source: "traefik", RecordHints: &source.RecordHints{Type: "A", Target: "192.0.2.10"}},
	}
	compiled := r.compileDesiredRecordSets(claims)
	if len(compiled.Sets) != 1 {
		t.Fatalf("sets = %d, want 1", len(compiled.Sets))
	}
	set := compiled.Sets[0]
	if len(set.Members) != 2 {
		t.Fatalf("members = %+v, want two distinct targets", set.Members)
	}
	if set.Members[0].Record.Target != "192.0.2.10" || set.Members[0].ClaimCount != 2 {
		t.Fatalf("first member = %+v, want target .10 with two claims", set.Members[0])
	}
	if set.Members[1].Record.Target != "192.0.2.11" || set.Members[1].ClaimCount != 1 {
		t.Fatalf("second member = %+v, want target .11 with one claim", set.Members[1])
	}
}

func TestCompileDesiredRecordSets_PreservesDualStackClaims(t *testing.T) {
	mock := newTestMockProvider("dns")
	providers := testProviderRegistry(quietLogger(), mock)
	if err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name: "dns", TypeName: "mock", RecordType: provider.RecordTypeA,
		Target: "192.0.2.1", TTL: 300, Domains: []string{"*.example.com"},
	}); err != nil {
		t.Fatalf("CreateInstance() error = %v", err)
	}
	r := New(nil, source.NewRegistry(quietLogger()), providers, WithLogger(quietLogger()))
	claims := []*source.Hostname{
		{Name: "vm.example.com", Source: "proxmox", RecordHints: &source.RecordHints{Type: "A", Target: "192.0.2.10"}},
		{Name: "vm.example.com", Source: "proxmox", RecordHints: &source.RecordHints{Type: "AAAA", Target: "2001:db8::10"}},
	}

	compiled := r.compileDesiredRecordSets(claims)
	if len(compiled.Sets) != 2 {
		t.Fatalf("sets = %+v, want separate A and AAAA sets", compiled.Sets)
	}
	if compiled.Sets[0].Key.RecordType != provider.RecordTypeA || compiled.Sets[1].Key.RecordType != provider.RecordTypeAAAA {
		t.Fatalf("record types = %s, %s", compiled.Sets[0].Key.RecordType, compiled.Sets[1].Key.RecordType)
	}
}

func TestCompileDesiredRecordSets_DeduplicatesBackendIdentity(t *testing.T) {
	first := newTestMockProvider("first")
	second := newTestMockProvider("second")
	identity := provider.ProviderIdentity{Type: "mock", Endpoint: "same", Zone: "example.com"}
	first.identity = &identity
	second.identity = &identity
	providers := testProviderRegistry(quietLogger(), first, second)
	for _, name := range []string{"first", "second"} {
		if err := providers.CreateInstance(provider.ProviderInstanceConfig{
			Name: name, TypeName: "mock", RecordType: provider.RecordTypeA,
			Target: "192.0.2.1", TTL: 300, Domains: []string{"*.example.com"},
		}); err != nil {
			t.Fatalf("CreateInstance(%s) error = %v", name, err)
		}
	}
	r := New(nil, source.NewRegistry(quietLogger()), providers, WithLogger(quietLogger()))
	compiled := r.compileDesiredRecordSets([]*source.Hostname{{Name: "app.example.com", Source: "traefik"}})
	if len(compiled.Sets) != 1 || compiled.Sets[0].Instance.Name() != "first" {
		t.Fatalf("sets = %+v, want first instance only", compiled.Sets)
	}
	if got := compiled.HostnameProviders["app.example.com"]; len(got) != 1 || got[0] != "first" {
		t.Fatalf("provider mapping = %v, want first", got)
	}
}
