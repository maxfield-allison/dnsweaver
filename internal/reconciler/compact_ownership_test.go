package reconciler

import (
	"context"
	"fmt"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

func compactReconciler(t *testing.T, dns *testMockProvider, owner string, src *testMockSource, dryRun bool) *Reconciler {
	t.Helper()
	registry := provider.NewRegistry(quietLogger())
	registry.SetInstanceID(owner)
	registry.SetCompactOwnership(true)
	registry.RegisterFactory("mock", func(provider.FactoryConfig) (provider.Provider, error) { return dns, nil })
	if err := registry.CreateInstance(provider.ProviderInstanceConfig{Name: "dns", TypeName: "mock", RecordType: provider.RecordTypeA, Target: "192.0.2.1", TTL: 300, Mode: provider.ModeManaged, Domains: []string{"*.example.com"}, ProviderConfig: map[string]string{"ZONE": "example.com"}}); err != nil {
		t.Fatal(err)
	}
	lister := newTestMockWorkloadLister(workload.PlatformDocker)
	lister.workloads = []workload.Workload{{ID: "one", Name: "one", Platform: workload.PlatformDocker}}
	cfg := DefaultConfig()
	cfg.InstanceID = owner
	cfg.DryRun = dryRun
	return New([]workload.Lister{lister}, testSourceRegistry(quietLogger(), src), registry, WithConfig(cfg), WithLogger(quietLogger()))
}

func TestReconcileCompactOwnershipSharedLifecycleAcrossProcessRestarts(t *testing.T) {
	ctx := context.Background()
	dns := newTestMockProvider("dns")
	aSource := newTestMockSource("native", *hintedA("shared.example.com", "192.0.2.10"))
	bSource := newTestMockSource("native", *hintedA("shared.example.com", "192.0.2.10"))
	a := compactReconciler(t, dns, "docker", aSource, false)
	if result, err := a.Reconcile(ctx); err != nil || len(result.Created()) != 1 {
		t.Fatalf("first create = %+v, %v", result, err)
	}
	b := compactReconciler(t, dns, "cluster", bSource, false)
	if result, err := b.Reconcile(ctx); err != nil || len(result.Failed()) != 0 {
		t.Fatalf("second claimant = %+v, %v", result, err)
	}
	aSource.hostnames = nil
	// Both the reconciler and ownership adapter are discarded on restart.
	a = compactReconciler(t, dns, "docker", aSource, false)
	result, err := a.Reconcile(ctx)
	if err != nil || len(result.Failed()) != 0 || len(result.Deleted()) != 0 {
		t.Fatalf("withdraw shared claim = %+v, %v", result, err)
	}
	records, err := a.providers.All()[0].Provider.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	claims := 0
	data := 0
	for _, r := range records {
		if r.Type == provider.RecordTypeA {
			data++
		}
		if provider.IsOwnershipRecord(r.Hostname) {
			claims++
			if !provider.MatchesOwnership(r.Target, "cluster") {
				t.Fatal("withdrawn owner remained", r)
			}
		}
	}
	if data != 1 || claims != 1 {
		t.Fatalf("surviving data=%d claims=%d", data, claims)
	}
	bSource.hostnames = nil
	b = compactReconciler(t, dns, "cluster", bSource, false)
	result, err = b.Reconcile(ctx)
	if err != nil || len(result.Deleted()) != 1 {
		t.Fatalf("last claimant removal = %+v, %v", result, err)
	}
	raw, _ := dns.List(ctx)
	for _, r := range raw {
		if r.Type == provider.RecordTypeA || provider.IsOwnershipRecord(r.Hostname) {
			t.Fatal("last claim left managed data or legacy TXT", r)
		}
	}
}

func TestReconcileCompactDryRunAndSourceInterruptionPreserveState(t *testing.T) {
	ctx := context.Background()
	dns := newTestMockProvider("dns")
	member := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.10", TTL: 300}
	legacy := provider.MemberOwnershipRecord(member.Hostname, 300, "docker", member, nil)
	dns.AddRecord(member)
	dns.AddRecord(legacy)
	src := newTestMockSource("native", *hintedA(member.Hostname, member.Target))
	dry := compactReconciler(t, dns, "docker", src, true)
	if _, err := dry.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if len(dns.GetCreated()) != 0 || len(dns.GetDeleted()) != 0 {
		t.Fatal("dry-run changed DNS")
	}
	live := compactReconciler(t, dns, "docker", src, false)
	if _, err := live.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	src.hostnames = nil
	live.listers[0].(*testMockWorkloadLister).listErr = fmt.Errorf("source disconnected")
	if _, err := live.Reconcile(ctx); err == nil {
		t.Fatal("source interruption should report unavailable discovery")
	}
	raw, _ := dns.List(ctx)
	found := false
	for _, r := range raw {
		found = found || r.Type == provider.RecordTypeA && r.Target == member.Target
	}
	if !found {
		t.Fatal("source outage deleted managed member")
	}
}

func TestReconcileCompactConflictingOwnerKeepsForeignCNAME(t *testing.T) {
	ctx := context.Background()
	dns := newTestMockProvider("dns")
	member := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeCNAME, Target: "upstream.example.com", TTL: 300}
	dns.AddRecord(member)
	dns.AddRecord(provider.MemberOwnershipRecord(member.Hostname, 300, "docker", member, nil))
	src := newTestMockSource("native", *hintedA(member.Hostname, "192.0.2.10"))
	r := compactReconciler(t, dns, "cluster", src, false)
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	for _, deleted := range dns.GetDeleted() {
		if deleted.Type == provider.RecordTypeCNAME {
			t.Fatal("foreign CNAME deleted")
		}
	}
}

func TestReconcileCompactCrashAfterDataDeletionRetiresOnlyAbsentLocalClaim(t *testing.T) {
	ctx := context.Background()
	dns := newTestMockProvider("dns")
	member := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.10", TTL: 300}
	src := newTestMockSource("native", *hintedA(member.Hostname, member.Target))
	r := compactReconciler(t, dns, "docker", src, false)
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	// Model the crash boundary after deleting data, before removing ownership.
	if err := dns.Delete(ctx, member); err != nil {
		t.Fatal(err)
	}
	foreign := provider.MemberOwnershipRecord(member.Hostname, 300, "offline-owner", member, nil)
	dns.AddRecord(foreign)
	src.hostnames = nil
	restarted := compactReconciler(t, dns, "docker", src, false)
	result, err := restarted.Reconcile(ctx)
	if err != nil || len(result.Failed()) != 0 {
		t.Fatalf("crash recovery = %+v, %v", result, err)
	}
	logical, err := restarted.providers.All()[0].Provider.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range logical {
		if provider.IsOwnershipRecord(record.Hostname) && provider.MatchesOwnership(record.Target, "docker") {
			t.Fatal("absent local claim survived restart cleanup")
		}
	}
	raw, _ := dns.List(ctx)
	foreignPresent := false
	for _, record := range raw {
		foreignPresent = foreignPresent || record.Hostname == foreign.Hostname && record.Target == foreign.Target
	}
	if !foreignPresent {
		t.Fatal("cleanup removed an offline owner's claim")
	}
}

var _ source.Source = (*testMockSource)(nil)

func TestReconcileLegacyTXTForeignProtectionRemainsIntact(t *testing.T) {
	member := provider.Record{Hostname: "txt.example.com", Type: provider.RecordTypeTXT, Target: "owned text", TTL: 300}
	records := []provider.Record{member,
		provider.MemberOwnershipRecord(member.Hostname, 300, "test-instance", member, nil),
		provider.MemberOwnershipRecord(member.Hostname, 300, "other", member, nil),
	}
	claim := &source.Hostname{Name: member.Hostname, Source: "native", RecordHints: &source.RecordHints{Type: "TXT", Target: member.Target, Metadata: map[string]string{"changed": "true"}}}
	r, mock, set, cache := setTestHarness(t, provider.ModeManaged, setTestCapabilities(true, false), records, claim)
	r.reconcileDesiredSet(context.Background(), set, cache, nil)
	if len(mock.GetCreated()) != 0 || len(mock.GetDeleted()) != 0 {
		t.Fatal("legacy provider mutated a foreign-owned TXT member or marker")
	}
}
