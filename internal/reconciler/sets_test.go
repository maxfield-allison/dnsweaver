package reconciler

import (
	"context"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
)

func setTestHarness(t *testing.T, mode provider.OperationalMode, caps provider.Capabilities, records []provider.Record, claims ...*source.Hostname) (*Reconciler, *testMockProvider, *desiredRecordSet, *recordCache) {
	t.Helper()
	mock := newTestMockProvider("dns")
	mock.capabilities = &caps
	for _, record := range records {
		mock.AddRecord(record)
	}
	providers := testProviderRegistry(quietLogger(), mock)
	providers.SetInstanceID("test-instance")
	if err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name: "dns", TypeName: "mock", RecordType: provider.RecordTypeA,
		Target: "192.0.2.1", TTL: 300, Mode: mode, Domains: []string{"*.example.com"},
	}); err != nil {
		t.Fatalf("CreateInstance() error = %v", err)
	}
	cfg := DefaultConfig()
	cfg.InstanceID = "test-instance"
	r := New(nil, source.NewRegistry(quietLogger()), providers, WithConfig(cfg), WithLogger(quietLogger()))
	compiled := r.compileDesiredRecordSets(claims)
	if len(compiled.Sets) != 1 {
		t.Fatalf("compiled sets = %+v, want one", compiled.Sets)
	}
	cache := newRecordCache(context.Background(), providers, quietLogger())
	return r, mock, compiled.Sets[0], cache
}

func setTestCapabilities(ownership, nativeUpdate bool) provider.Capabilities {
	return provider.Capabilities{
		SupportsOwnershipTXT: ownership,
		SupportsNativeUpdate: nativeUpdate,
		SupportedRecordTypes: []provider.RecordType{provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME, provider.RecordTypeSRV, provider.RecordTypeTXT},
	}
}

func hintedA(hostname, target string) *source.Hostname {
	return &source.Hostname{Name: hostname, Source: "native", RecordHints: &source.RecordHints{Type: "A", Target: target}}
}

func TestReconcileDesiredSet_CreatesEveryDistinctMember(t *testing.T) {
	r, mock, set, cache := setTestHarness(t, provider.ModeManaged, setTestCapabilities(true, false), nil,
		hintedA("app.example.com", "192.0.2.10"),
		hintedA("app.example.com", "192.0.2.11"),
	)
	actions := r.reconcileDesiredSet(context.Background(), set, cache, nil)
	if len(actions) != 2 || actions[0].Type != ActionCreate || actions[1].Type != ActionCreate {
		t.Fatalf("actions = %+v, want two creates", actions)
	}
	if got := mock.GetCreatedDNSRecords(); len(got) != 2 {
		t.Fatalf("created data records = %+v, want two", got)
	}
	if got := mock.GetCreatedOwnershipRecords(); len(got) != 2 {
		t.Fatalf("created ownership records = %+v, want one per member", got)
	}
}

func TestReconcileDesiredSet_ManagedDeletesOnlyOwnedMissingMember(t *testing.T) {
	first := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.10", TTL: 300}
	second := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.11", TTL: 300}
	records := []provider.Record{
		first,
		second,
		provider.MemberOwnershipRecord(first.Hostname, 300, "test-instance", first, nil),
		provider.MemberOwnershipRecord(second.Hostname, 300, "test-instance", second, nil),
	}
	r, mock, set, cache := setTestHarness(t, provider.ModeManaged, setTestCapabilities(true, false), records,
		hintedA(first.Hostname, first.Target),
	)
	actions := r.reconcileDesiredSet(context.Background(), set, cache, nil)
	if len(actions) != 2 || actions[1].Type != ActionDelete || actions[1].Target != second.Target {
		t.Fatalf("actions = %+v, want only second member deleted", actions)
	}
	deleted := mock.GetDeleted()
	if len(deleted) != 2 || !provider.SameRecordMember(deleted[0], second) || deleted[1].Type != provider.RecordTypeTXT {
		t.Fatalf("deleted records = %+v, want second member and its marker", deleted)
	}
}

func TestReconcileDesiredSet_LegacyMarkerUpgradesOnlyDesiredMember(t *testing.T) {
	first := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.10", TTL: 300}
	manualSibling := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.99", TTL: 300}
	legacy := provider.OwnershipRecord(first.Hostname, 300, "test-instance", nil)
	r, mock, set, cache := setTestHarness(t, provider.ModeManaged, setTestCapabilities(true, false),
		[]provider.Record{first, manualSibling, legacy}, hintedA(first.Hostname, first.Target))
	r.reconcileDesiredSet(context.Background(), set, cache, nil)

	created := mock.GetCreatedOwnershipRecords()
	if len(created) != 1 || !provider.MatchesMemberOwnership(created[0].Target, "test-instance", first) {
		t.Fatalf("created ownership = %+v, want exact desired member marker", created)
	}
	deleted := mock.GetDeleted()
	if len(deleted) != 1 || deleted[0].Target != legacy.Target {
		t.Fatalf("deleted = %+v, want legacy marker only", deleted)
	}
}

func TestReconcileDesiredSet_NoTXTUsesOnlyPreviousInMemoryMembers(t *testing.T) {
	first := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.10", TTL: 300}
	second := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.11", TTL: 300}
	caps := setTestCapabilities(false, false)

	r, mock, set, cache := setTestHarness(t, provider.ModeManaged, caps, []provider.Record{first, second}, hintedA(first.Hostname, first.Target))
	r.reconcileDesiredSet(context.Background(), set, cache, []provider.Record{first, second})
	if deleted := mock.GetDeleted(); len(deleted) != 1 || deleted[0].Target != second.Target {
		t.Fatalf("deleted with previous state = %+v, want second member", deleted)
	}

	r, mock, set, cache = setTestHarness(t, provider.ModeManaged, caps, []provider.Record{first, second}, hintedA(first.Hostname, first.Target))
	r.reconcileDesiredSet(context.Background(), set, cache, nil)
	if deleted := mock.GetDeleted(); len(deleted) != 0 {
		t.Fatalf("deleted after simulated restart without durable ownership = %+v", deleted)
	}
}

func TestReconcileDesiredSet_AuthoritativeRemovesUnownedSibling(t *testing.T) {
	first := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.10", TTL: 300}
	second := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.11", TTL: 300}
	r, mock, set, cache := setTestHarness(t, provider.ModeAuthoritative, setTestCapabilities(true, false),
		[]provider.Record{first, second}, hintedA(first.Hostname, first.Target))
	r.reconcileDesiredSet(context.Background(), set, cache, nil)
	if deleted := mock.GetDeleted(); len(deleted) != 1 || deleted[0].Target != second.Target {
		t.Fatalf("deleted = %+v, want unowned sibling in authoritative mode", deleted)
	}
}

func TestReconcileDesiredSet_AdditiveDoesNotDeleteOrFallbackUpdate(t *testing.T) {
	existing := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.10", TTL: 60}
	sibling := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.11", TTL: 300}
	claim := hintedA(existing.Hostname, existing.Target)
	claim.RecordHints.TTL = 300
	r, mock, set, cache := setTestHarness(t, provider.ModeAdditive, setTestCapabilities(true, false),
		[]provider.Record{existing, sibling}, claim)
	actions := r.reconcileDesiredSet(context.Background(), set, cache, nil)
	if len(actions) != 1 || actions[0].Type != ActionSkip {
		t.Fatalf("actions = %+v, want update skipped", actions)
	}
	if len(mock.GetDeleted()) != 0 || len(mock.GetCreatedDNSRecords()) != 0 {
		t.Fatalf("additive mode mutated records: created=%+v deleted=%+v", mock.GetCreated(), mock.GetDeleted())
	}
}

func TestReconcileDesiredSet_CNAMEUsesSingleValueUpdate(t *testing.T) {
	old := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeCNAME, Target: "old.example.com", TTL: 300}
	marker := provider.MemberOwnershipRecord(old.Hostname, 300, "test-instance", old, nil)
	claim := &source.Hostname{
		Name:   old.Hostname,
		Source: "native",
		RecordHints: &source.RecordHints{
			Type:   string(provider.RecordTypeCNAME),
			Target: "new.example.com",
		},
	}
	r, mock, set, cache := setTestHarness(t, provider.ModeManaged, setTestCapabilities(true, false), []provider.Record{old, marker}, claim)
	oldDeleted := false
	mock.deleteFn = func(_ context.Context, record provider.Record) error {
		if record.Type == provider.RecordTypeCNAME && provider.SameRecordMember(record, old) {
			oldDeleted = true
		}
		return nil
	}
	mock.createFn = func(_ context.Context, record provider.Record) error {
		if record.Type == provider.RecordTypeCNAME && !oldDeleted {
			return provider.ErrConflict
		}
		return nil
	}

	actions := r.reconcileDesiredSet(context.Background(), set, cache, nil)
	if len(actions) != 1 || actions[0].Type != ActionUpdate || actions[0].Status != StatusSuccess {
		t.Fatalf("actions = %+v, want one successful CNAME update", actions)
	}
	created := mock.GetCreatedDNSRecords()
	if len(created) != 1 || created[0].Target != "new.example.com" {
		t.Fatalf("created = %+v, want replacement CNAME after old deletion", created)
	}
}

func TestReconcileDesiredSet_RejectsMultipleCNAMEMembers(t *testing.T) {
	first := &source.Hostname{Name: "app.example.com", Source: "native", RecordHints: &source.RecordHints{Type: "CNAME", Target: "one.example.com"}}
	second := &source.Hostname{Name: "app.example.com", Source: "native", RecordHints: &source.RecordHints{Type: "CNAME", Target: "two.example.com"}}
	r, mock, set, cache := setTestHarness(t, provider.ModeManaged, setTestCapabilities(true, true), nil, first, second)
	actions := r.reconcileDesiredSet(context.Background(), set, cache, nil)
	if len(actions) != 2 || actions[0].Error != errMultipleCNAMEMembers || actions[1].Error != errMultipleCNAMEMembers {
		t.Fatalf("actions = %+v, want both CNAME members rejected", actions)
	}
	if len(mock.GetCreated()) != 0 || len(mock.GetDeleted()) != 0 {
		t.Fatalf("ambiguous CNAME set mutated provider: created=%+v deleted=%+v", mock.GetCreated(), mock.GetDeleted())
	}
}
