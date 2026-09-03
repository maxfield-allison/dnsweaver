package reconciler

import (
	"context"
	"errors"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

func liveSetHarness(t *testing.T, caps provider.Capabilities, sourceMock *testMockSource, workloads []workload.Workload) (*Reconciler, *testMockProvider, *provider.Registry) {
	t.Helper()
	mock := newTestMockProvider("dns")
	mock.capabilities = &caps
	providers := testProviderRegistry(quietLogger(), mock)
	providers.SetInstanceID("test-instance")
	if err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name: "dns", TypeName: "mock", RecordType: provider.RecordTypeA,
		Target: "192.0.2.1", TTL: 300, Mode: provider.ModeManaged, Domains: []string{"*.example.com"},
	}); err != nil {
		t.Fatalf("CreateInstance() error = %v", err)
	}
	sources := testSourceRegistry(quietLogger(), sourceMock)
	lister := newTestMockWorkloadLister(workload.PlatformDocker)
	lister.workloads = workloads
	cfg := DefaultConfig()
	cfg.InstanceID = "test-instance"
	return New([]workload.Lister{lister}, sources, providers, WithConfig(cfg), WithLogger(quietLogger())), mock, providers
}

func TestReconcile_RecordSetLifecycleSurvivesRestart(t *testing.T) {
	sourceMock := newTestMockSource("native",
		*hintedA("app.example.com", "192.0.2.10"),
		*hintedA("app.example.com", "192.0.2.11"),
	)
	workloads := []workload.Workload{{ID: "one", Name: "one", Platform: workload.PlatformDocker}}
	r, mock, providers := liveSetHarness(t, setTestCapabilities(true, false), sourceMock, workloads)

	first, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}
	if first.DesiredMembers != 2 || len(first.Created()) != 2 {
		t.Fatalf("first result = %+v, want two desired creates", first)
	}

	sourceMock.hostnames = source.Hostnames{*hintedA("app.example.com", "192.0.2.10")}
	second, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	if len(second.Deleted()) != 1 || second.Deleted()[0].Target != "192.0.2.11" {
		t.Fatalf("second actions = %+v, want only .11 deleted", second.Actions)
	}

	// A new reconciler has no previousDesired memory. The surviving member's
	// durable marker must still make final removal safe after restart.
	sourceMock.hostnames = nil
	cfg := DefaultConfig()
	cfg.InstanceID = "test-instance"
	restarted := New(r.listers, r.sources, providers, WithConfig(cfg), WithLogger(quietLogger()))
	third, err := restarted.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("restart Reconcile() error = %v", err)
	}
	if len(third.Deleted()) != 1 || third.Deleted()[0].Target != "192.0.2.10" {
		t.Fatalf("restart actions = %+v, want final member deleted", third.Actions)
	}
	if remaining := mock.GetCreatedDNSRecords(); len(remaining) != 2 {
		// GetCreated is an audit log, not current state. Keep this assertion here
		// only to make sure both creates occurred during the lifecycle.
		t.Fatalf("created audit = %+v", remaining)
	}
}

func TestReconcile_RepeatedIdenticalClaimsCreateOneMember(t *testing.T) {
	sourceMock := newTestMockSource("traefik", source.Hostname{Name: "app.example.com", Source: "traefik"})
	workloads := []workload.Workload{
		{ID: "one", Name: "one", Platform: workload.PlatformDocker},
		{ID: "two", Name: "two", Platform: workload.PlatformDocker},
	}
	r, mock, _ := liveSetHarness(t, setTestCapabilities(true, false), sourceMock, workloads)
	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.HostnamesDiscovered != 1 || result.DesiredMembers != 1 || len(mock.GetCreatedDNSRecords()) != 1 {
		t.Fatalf("result hostnames=%d members=%d created=%+v", result.HostnamesDiscovered, result.DesiredMembers, mock.GetCreatedDNSRecords())
	}
}

func TestReconcile_RemovingOneOfSeveralIdenticalClaimantsKeepsMember(t *testing.T) {
	sourceMock := newTestMockSource("traefik", source.Hostname{Name: "app.example.com", Source: "traefik"})
	workloads := []workload.Workload{
		{ID: "one", Name: "one", Platform: workload.PlatformDocker},
		{ID: "two", Name: "two", Platform: workload.PlatformDocker},
	}
	r, mock, _ := liveSetHarness(t, setTestCapabilities(true, false), sourceMock, workloads)
	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}

	lister := r.listers[0].(*testMockWorkloadLister)
	lister.workloads = workloads[:1]
	second, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	if len(second.Deleted()) != 0 || len(mock.GetDeleted()) != 0 {
		t.Fatalf("removing one claimant deleted shared member: actions=%+v deleted=%+v", second.Actions, mock.GetDeleted())
	}

	lister.workloads = nil
	third, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("third Reconcile() error = %v", err)
	}
	if len(third.Deleted()) != 1 || third.Deleted()[0].Target != "192.0.2.1" {
		t.Fatalf("last claimant removal actions = %+v, want one member delete", third.Actions)
	}
}

func TestReconcile_ProviderRouteChangesAreExactLiveAndAfterRestart(t *testing.T) {
	internal := newTestMockProvider("internal")
	external := newTestMockProvider("external")
	internalIdentity := provider.ProviderIdentity{Type: "mock", Endpoint: "internal", Zone: "example.com"}
	externalIdentity := provider.ProviderIdentity{Type: "mock", Endpoint: "external", Zone: "example.com"}
	internal.identity = &internalIdentity
	external.identity = &externalIdentity
	external.AddRecord(provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.99", TTL: 300})

	providers := testProviderRegistry(quietLogger(), internal, external)
	providers.SetInstanceID("test-instance")
	for _, name := range []string{"internal", "external"} {
		if err := providers.CreateInstance(provider.ProviderInstanceConfig{
			Name: name, TypeName: "mock", RecordType: provider.RecordTypeA,
			Target: "192.0.2.1", TTL: 300, Mode: provider.ModeManaged, Domains: []string{"*.example.com"},
		}); err != nil {
			t.Fatalf("CreateInstance(%s) error = %v", name, err)
		}
	}

	claim := hintedA("app.example.com", "192.0.2.10")
	claim.RecordHints.Provider = "internal"
	sourceMock := newTestMockSource("native", *claim)
	sources := testSourceRegistry(quietLogger(), sourceMock)
	lister := newTestMockWorkloadLister(workload.PlatformDocker)
	lister.workloads = []workload.Workload{{ID: "app", Name: "app", Platform: workload.PlatformDocker}}
	cfg := DefaultConfig()
	cfg.InstanceID = "test-instance"
	r := New([]workload.Lister{lister}, sources, providers, WithConfig(cfg), WithLogger(quietLogger()))
	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile() error = %v", err)
	}

	// Switch routes while constructing a fresh reconciler. The old internal
	// route must be recovered from its durable member marker.
	claim.RecordHints.Provider = "external"
	sourceMock.hostnames = source.Hostnames{*claim}
	restarted := New([]workload.Lister{lister}, sources, providers, WithConfig(cfg), WithLogger(quietLogger()))
	afterRestart, err := restarted.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("restart Reconcile() error = %v", err)
	}
	if got := afterRestart.Deleted(); len(got) != 1 || got[0].Provider != "internal" || got[0].Target != "192.0.2.10" {
		t.Fatalf("restart route cleanup = %+v, want exact internal member", got)
	}
	if got := afterRestart.Created(); len(got) != 1 || got[0].Provider != "external" || got[0].Target != "192.0.2.10" {
		t.Fatalf("restart route creation = %+v, want exact external member", got)
	}

	// A live switch back exercises the in-memory route history. The unrelated
	// external sibling must remain untouched.
	claim.RecordHints.Provider = "internal"
	sourceMock.hostnames = source.Hostnames{*claim}
	afterLiveSwitch, err := restarted.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("live route Reconcile() error = %v", err)
	}
	if got := afterLiveSwitch.Deleted(); len(got) != 1 || got[0].Provider != "external" || got[0].Target != "192.0.2.10" {
		t.Fatalf("live route cleanup = %+v, want exact external member", got)
	}
	for _, deleted := range external.GetDeleted() {
		if deleted.Type == provider.RecordTypeA && deleted.Target == "192.0.2.99" {
			t.Fatalf("route switch deleted unrelated external sibling: %+v", deleted)
		}
	}
}

func TestReconcile_MultiMemberProviderRouteChangeBypassesRemovalCircuitBreaker(t *testing.T) {
	internal := newTestMockProvider("internal")
	external := newTestMockProvider("external")
	internalIdentity := provider.ProviderIdentity{Type: "mock", Endpoint: "internal", Zone: "example.com"}
	externalIdentity := provider.ProviderIdentity{Type: "mock", Endpoint: "external", Zone: "example.com"}
	internal.identity = &internalIdentity
	external.identity = &externalIdentity

	providers := testProviderRegistry(quietLogger(), internal, external)
	providers.SetInstanceID("test-instance")
	for _, name := range []string{"internal", "external"} {
		if err := providers.CreateInstance(provider.ProviderInstanceConfig{
			Name: name, TypeName: "mock", RecordType: provider.RecordTypeA,
			Target: "192.0.2.1", TTL: 300, Mode: provider.ModeManaged, Domains: []string{"*.example.com"},
		}); err != nil {
			t.Fatalf("CreateInstance(%s) error = %v", name, err)
		}
	}

	claims := source.Hostnames{
		*hintedA("app.example.com", "192.0.2.10"),
		*hintedA("app.example.com", "192.0.2.11"),
		*hintedA("app.example.com", "192.0.2.12"),
	}
	for i := range claims {
		claims[i].RecordHints.Provider = "internal"
	}
	sourceMock := newTestMockSource("native", claims...)
	sources := testSourceRegistry(quietLogger(), sourceMock)
	lister := newTestMockWorkloadLister(workload.PlatformDocker)
	lister.workloads = []workload.Workload{{ID: "app", Name: "app", Platform: workload.PlatformDocker}}
	cfg := DefaultConfig()
	cfg.InstanceID = "test-instance"
	r := New([]workload.Lister{lister}, sources, providers, WithConfig(cfg), WithLogger(quietLogger()))
	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile() error = %v", err)
	}

	for i := range claims {
		claims[i].RecordHints.Provider = "external"
	}
	sourceMock.hostnames = claims
	restarted := New([]workload.Lister{lister}, sources, providers, WithConfig(cfg), WithLogger(quietLogger()))
	result, err := restarted.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("route Reconcile() error = %v", err)
	}
	if got := result.Created(); len(got) != 3 {
		t.Fatalf("route creates = %+v, want all three external members", got)
	}
	if got := result.Deleted(); len(got) != 3 {
		t.Fatalf("route deletes = %+v, want all three internal members", got)
	}
	for _, action := range result.Created() {
		if action.Provider != "external" {
			t.Fatalf("created on provider %q, want external", action.Provider)
		}
	}
	for _, action := range result.Deleted() {
		if action.Provider != "internal" {
			t.Fatalf("deleted from provider %q, want internal", action.Provider)
		}
	}
}

func TestReconcile_ProxmoxDualStackReachesProvider(t *testing.T) {
	sourceMock := newTestMockSource("proxmox",
		source.Hostname{Name: "vm.example.com", Source: "proxmox", RecordHints: &source.RecordHints{Type: "A", Target: "192.0.2.10"}},
		source.Hostname{Name: "vm.example.com", Source: "proxmox", RecordHints: &source.RecordHints{Type: "AAAA", Target: "2001:db8::10"}},
	)
	workloads := []workload.Workload{{ID: "vm", Name: "vm", Platform: workload.PlatformProxmox}}
	r, mock, _ := liveSetHarness(t, setTestCapabilities(true, false), sourceMock, workloads)
	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	created := mock.GetCreatedDNSRecords()
	if result.HostnamesDiscovered != 1 || result.DesiredMembers != 2 || len(created) != 2 {
		t.Fatalf("result hostnames=%d members=%d created=%+v", result.HostnamesDiscovered, result.DesiredMembers, created)
	}
	if created[0].Type != provider.RecordTypeA || created[1].Type != provider.RecordTypeAAAA {
		t.Fatalf("created = %+v, want A and AAAA", created)
	}
}

func TestReconcile_PartialSourceSnapshotSuppressesRemoval(t *testing.T) {
	sourceMock := newTestMockSource("native", *hintedA("app.example.com", "192.0.2.10"))
	workloads := []workload.Workload{{ID: "one", Name: "one", Platform: workload.PlatformDocker}}
	r, mock, _ := liveSetHarness(t, setTestCapabilities(false, false), sourceMock, workloads)
	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}

	sourceMock.hostnames = nil
	sourceMock.err = errors.New("source unavailable")
	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("partial Reconcile() error = %v", err)
	}
	if len(result.Deleted()) != 0 || len(mock.GetDeleted()) != 0 {
		t.Fatalf("partial snapshot deleted records: actions=%+v deleted=%+v", result.Actions, mock.GetDeleted())
	}
}

func TestReconcile_NoTXTProviderFailsSafeAfterRestart(t *testing.T) {
	sourceMock := newTestMockSource("native")
	workloads := []workload.Workload{{ID: "one", Name: "one", Platform: workload.PlatformDocker}}
	r, mock, _ := liveSetHarness(t, setTestCapabilities(false, false), sourceMock, workloads)
	mock.AddRecord(provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.10", TTL: 300})
	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(result.Deleted()) != 0 || len(mock.GetDeleted()) != 0 {
		t.Fatalf("restart without ownership deleted records: %+v", result.Actions)
	}
}

func TestReconcile_NoTXTProviderDoesNotClaimMatchingManualMember(t *testing.T) {
	sourceMock := newTestMockSource("native", *hintedA("app.example.com", "192.0.2.10"))
	workloads := []workload.Workload{{ID: "one", Name: "one", Platform: workload.PlatformDocker}}
	r, mock, _ := liveSetHarness(t, setTestCapabilities(false, false), sourceMock, workloads)
	mock.AddRecord(provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.10", TTL: 300})
	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}

	sourceMock.hostnames = nil
	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	if len(result.Deleted()) != 0 || len(mock.GetDeleted()) != 0 {
		t.Fatalf("matching manual member was treated as owned: actions=%+v deleted=%+v", result.Actions, mock.GetDeleted())
	}
}

func TestReconcile_DryRunDoesNotClaimNoTXTMember(t *testing.T) {
	sourceMock := newTestMockSource("native", *hintedA("app.example.com", "192.0.2.10"))
	workloads := []workload.Workload{{ID: "one", Name: "one", Platform: workload.PlatformDocker}}
	r, mock, _ := liveSetHarness(t, setTestCapabilities(false, false), sourceMock, workloads)
	r.SetDryRun(true)
	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("dry-run Reconcile() error = %v", err)
	}

	// Simulate an operator creating the same member after the dry run. The dry
	// run must not have recorded authority to delete it.
	mock.AddRecord(provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.10", TTL: 300})
	sourceMock.hostnames = nil
	r.SetDryRun(false)
	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("real Reconcile() error = %v", err)
	}
	if len(result.Deleted()) != 0 || len(mock.GetDeleted()) != 0 {
		t.Fatalf("dry-run state authorized a real deletion: actions=%+v deleted=%+v", result.Actions, mock.GetDeleted())
	}
}

func TestReconcile_NoTXTRetriesOldMemberAfterReplacementFailure(t *testing.T) {
	sourceMock := newTestMockSource("native", *hintedA("app.example.com", "192.0.2.99"))
	workloads := []workload.Workload{{ID: "one", Name: "one", Platform: workload.PlatformDocker}}
	r, mock, _ := liveSetHarness(t, setTestCapabilities(false, false), sourceMock, workloads)
	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile() error = %v", err)
	}

	sourceMock.hostnames = source.Hostnames{*hintedA("app.example.com", "192.0.2.10")}
	mock.createFn = func(_ context.Context, record provider.Record) error {
		if record.Type == provider.RecordTypeA && record.Target == "192.0.2.10" {
			return errors.New("temporary create failure")
		}
		return nil
	}
	failed, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("failed replacement Reconcile() error = %v", err)
	}
	if len(failed.Failed()) != 1 || len(mock.GetDeleted()) != 0 {
		t.Fatalf("failed replacement did not preserve old member: actions=%+v deleted=%+v", failed.Actions, mock.GetDeleted())
	}

	mock.createFn = nil
	retried, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("retry Reconcile() error = %v", err)
	}
	if got := retried.Deleted(); len(got) != 1 || got[0].Target != "192.0.2.99" {
		t.Fatalf("retry actions = %+v, want old member deletion after replacement succeeds", retried.Actions)
	}
}

func TestReconcile_MemberCircuitBreakerSuppressesMassRemoval(t *testing.T) {
	sourceMock := newTestMockSource("native",
		*hintedA("app.example.com", "192.0.2.10"),
		*hintedA("app.example.com", "192.0.2.11"),
		*hintedA("app.example.com", "192.0.2.12"),
		*hintedA("app.example.com", "192.0.2.13"),
	)
	workloads := []workload.Workload{{ID: "one", Name: "one", Platform: workload.PlatformDocker}}
	r, mock, _ := liveSetHarness(t, setTestCapabilities(true, false), sourceMock, workloads)
	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile() error = %v", err)
	}

	sourceMock.hostnames = source.Hostnames{*hintedA("app.example.com", "192.0.2.10")}
	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	if len(result.Deleted()) != 0 || len(mock.GetDeleted()) != 0 {
		t.Fatalf("mass-removal breaker allowed member deletion: actions=%+v deleted=%+v", result.Actions, mock.GetDeleted())
	}
}
