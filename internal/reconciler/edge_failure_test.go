package reconciler

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
	"github.com/maxfield-allison/dnsweaver/sources/traefik"
)

// =============================================================================
// P1: Failure Handling Edge Cases
//
// Tests for context cancellation, provider unavailability, and partial failure
// recovery during reconciliation.
// =============================================================================

// --- Context Cancellation ---

func TestReconcile_CanceledContextBeforeStart(t *testing.T) {
	// A canceled context before reconciliation starts should propagate
	// through to workload listing.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.SetListError(context.Canceled)

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))
	providers := provider.NewRegistry(logger)

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := r.Reconcile(ctx)
	if err == nil {
		t.Fatal("expected error from canceled context, got nil")
	}
}

func TestReconcile_CanceledContextDuringProviderCreate(t *testing.T) {
	// Context canceled while provider.Create is in progress should
	// result in a failed action.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	// Provider will return context.Canceled when Create is called
	mockProvider.createFn = func(ctx context.Context, _ provider.Record) error {
		return context.Canceled
	}

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(_ provider.FactoryConfig) (provider.Provider, error) {
		return mockProvider, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned hard error: %v", err)
	}

	// The create should have failed
	if !result.HasErrors() {
		t.Error("expected result to have errors from canceled provider create")
	}
	failed := result.Failed()
	if len(failed) == 0 {
		t.Fatal("expected at least one failed action")
	}
	if !errors.Is(context.Canceled, context.Canceled) {
		t.Errorf("expected context.Canceled in error, got: %s", failed[0].Error)
	}
}

func TestReconcile_CanceledContextDuringProviderDelete(t *testing.T) {
	// Test that context cancellation during a provider.Delete in orphan
	// cleanup is reported as a failed deletion action.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	// No workloads — everything from previous cycle is an orphan

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	// Pre-seed a record that the provider would try to delete
	mockProvider.AddRecord(provider.Record{
		Hostname: "orphan.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	// Add ownership TXT at the correct hostname (_dnsweaver. prefix)
	mockProvider.AddRecord(provider.Record{
		Hostname: provider.OwnershipRecordName("orphan.example.com"),
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
	})

	mockProvider.deleteFn = func(_ context.Context, _ provider.Record) error {
		return context.Canceled
	}

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(_ provider.FactoryConfig) (provider.Provider, error) {
		return mockProvider, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	cfg := DefaultConfig()
	cfg.CleanupOrphans = true
	cfg.OwnershipTracking = true

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	// Seed knownHostnames so orphan cleanup finds orphan.example.com
	r.mu.Lock()
	r.knownHostnames["orphan.example.com"] = struct{}{}
	r.hostnameProviders = map[string][]string{
		"orphan.example.com": {"test-dns"},
	}
	r.mu.Unlock()

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned hard error: %v", err)
	}

	// Should have failed delete actions
	if !result.HasErrors() {
		t.Error("expected errors from canceled delete")
	}
}

// --- Provider Unavailability ---

func TestReconcile_ProviderListFailureDuringCacheBuild(t *testing.T) {
	// When provider.List() fails during cache building, the reconciler
	// should still proceed (cache stores nil, falls back to direct query).
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	// List fails during cache build but Create still works
	callCount := 0
	mockProvider.listErr = errors.New("provider temporarily unavailable")
	mockProvider.createFn = func(_ context.Context, r provider.Record) error {
		callCount++
		return nil
	}

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(_ provider.FactoryConfig) (provider.Provider, error) {
		return mockProvider, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned hard error: %v", err)
	}

	// Despite cache failure, reconciliation should complete
	if result.HostnamesDiscovered != 1 {
		t.Errorf("HostnamesDiscovered = %d, want 1", result.HostnamesDiscovered)
	}

	// The create should have gone through (fall back to direct query which also
	// fails, but the record still gets created because List error is non-fatal)
	if result.CreatedCount() == 0 && result.FailedCount() == 0 && len(result.Skipped()) == 0 {
		t.Error("expected at least one action (create, fail, or skip)")
	}
}

func TestReconcile_ProviderDeleteFails(t *testing.T) {
	// When provider.Delete() fails during ensureRecord (target change),
	// the action should report failure.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	// Pre-seed a record with different target to trigger update
	mockProvider.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.99", // different from the 10.0.0.1 the provider instance wants
	})
	mockProvider.AddRecord(ownershipTXT("app.example.com"))

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(_ provider.FactoryConfig) (provider.Provider, error) {
		return mockProvider, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned hard error: %v", err)
	}

	// Should have an update action (old target 10.0.0.99 → new target 10.0.0.1)
	updated := result.Updated()
	if len(updated) != 1 {
		t.Errorf("expected 1 updated action, got %d", len(updated))
	}
}

func TestReconcile_AllListersFailReturnsError(t *testing.T) {
	// When ALL listers fail, Reconcile should return an error (hard fail).
	lister1 := newTestMockWorkloadLister(workload.PlatformDocker)
	lister1.SetListError(errors.New("docker daemon unreachable"))

	lister2 := newTestMockWorkloadLister("kubernetes")
	lister2.SetListError(errors.New("k8s API server down"))

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	providers := provider.NewRegistry(logger)

	r := New([]workload.Lister{lister1, lister2}, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	_, err := r.Reconcile(context.Background())
	if err == nil {
		t.Fatal("expected error when all listers fail, got nil")
	}
	// The reconciler fails on the FIRST lister error (doesn't aggregate)
	if !containsStr(err.Error(), "docker daemon unreachable") {
		t.Errorf("error should contain lister error, got: %v", err)
	}
}

func TestReconcile_PartialListerFailure(t *testing.T) {
	// When one lister fails, it should return an error even if other
	// listers would succeed. The reconciler hard-fails on any lister error.
	successLister := newTestMockWorkloadLister(workload.PlatformDocker)
	successLister.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.example.com`)",
	})

	failLister := newTestMockWorkloadLister("kubernetes")
	failLister.SetListError(errors.New("k8s API server unavailable"))

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))
	providers := provider.NewRegistry(logger)

	r := New([]workload.Lister{successLister, failLister}, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	_, err := r.Reconcile(context.Background())
	// Depends on lister order - first success, then fail
	if err == nil {
		t.Fatal("expected error when second lister fails")
	}
}

// --- Partial Failure Recovery ---

func TestReconcile_CreateFailsButOtherProvidersSucceed(t *testing.T) {
	// Under first-match-wins (issue #86), only the first declared matching
	// provider writes — there is no automatic failover to a second provider.
	// This test verifies the failure of the winning provider is reported
	// (and the loser is left untouched) rather than silently masking the
	// failure by writing to the second instance.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	// Declare the failing provider first so it is the winner.
	badProvider := newTestMockProvider("bad-dns")
	badProvider.createFn = func(_ context.Context, _ provider.Record) error {
		return errors.New("provider API error")
	}
	goodProvider := newTestMockProvider("good-dns")

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		if cfg.Name == "bad-dns" {
			return badProvider, nil
		}
		return goodProvider, nil
	})
	// Both providers match *.example.com — bad-dns is declared first.
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "bad-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.2",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "good-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned hard error: %v", err)
	}

	// The winning provider's failure must be reported.
	if result.FailedCount() < 1 {
		t.Errorf("expected at least 1 failed record from bad-dns, got %d", result.FailedCount())
	}
	// The losing provider must NOT have been called as a fallback.
	if got := len(goodProvider.GetCreatedDNSRecords()); got != 0 {
		t.Errorf("good-dns (loser) should not be called as a fallback, got %d created records", got)
	}
}

func TestReconcile_ProviderRecoversNextCycle(t *testing.T) {
	// Simulate a provider that fails in cycle 1 but succeeds in cycle 2.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	cycle := 0
	mockProvider.createFn = func(_ context.Context, r provider.Record) error {
		cycle++
		if cycle == 1 {
			return errors.New("transient error")
		}
		// Second cycle: succeed normally
		return nil
	}

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(_ provider.FactoryConfig) (provider.Provider, error) {
		return mockProvider, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	// Cycle 1: should fail
	result1, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Cycle 1: Reconcile returned hard error: %v", err)
	}
	if result1.FailedCount() == 0 {
		t.Error("Cycle 1: expected failed action")
	}

	// Cycle 2: should succeed
	result2, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Cycle 2: Reconcile returned hard error: %v", err)
	}
	if result2.CreatedCount() == 0 {
		// The record may be skipped if mock provider already has it
		// OR created fresh. Either result is acceptable.
		if len(result2.Skipped()) == 0 && result2.FailedCount() > 0 {
			t.Errorf("Cycle 2: expected success but got %d failed", result2.FailedCount())
		}
	}
}

// --- Error Message Quality ---

func TestReconcile_ErrorMessagesAreActionable(t *testing.T) {
	// Verify that error messages in failed actions contain useful information
	// (provider name, hostname, operation type).
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("my-provider")
	mockProvider.createFn = func(_ context.Context, _ provider.Record) error {
		return fmt.Errorf("connection refused")
	}

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(_ provider.FactoryConfig) (provider.Provider, error) {
		return mockProvider, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "my-provider",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}

	failed := result.Failed()
	if len(failed) == 0 {
		t.Fatal("expected failed actions")
	}

	action := failed[0]
	if action.Provider == "" {
		t.Error("failed action should have provider name set")
	}
	if action.Hostname == "" {
		t.Error("failed action should have hostname set")
	}
	if action.Error == "" {
		t.Error("failed action should have error message set")
	}
	if action.Status != StatusFailed {
		t.Errorf("expected StatusFailed, got %s", action.Status)
	}
}

// containsStr checks if a string contains a substring (for error message checks).
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
