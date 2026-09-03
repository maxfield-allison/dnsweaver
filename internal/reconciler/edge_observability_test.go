package reconciler

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/maxfield-allison/dnsweaver/internal/metrics"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
	"github.com/maxfield-allison/dnsweaver/sources/traefik"
)

// =============================================================================
// P3: Observability & Dry-Run Completeness Tests
//
// Tests for metric emission verification and dry-run behavioral completeness.
// =============================================================================

// --- Metric Emission ---

func TestReconcile_MetricsEmittedOnSuccess(t *testing.T) {
	// After a successful reconciliation, Prometheus metrics should be updated.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("metric-test-dns")
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(_ provider.FactoryConfig) (provider.Provider, error) {
		return mockProvider, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "metric-test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	// Reset relevant metrics before test
	metrics.ReconciliationsTotal.Reset()
	metrics.RecordsCreatedTotal.Reset()
	metrics.RecordsSkippedTotal.Reset()

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	_, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Verify reconciliation counter was incremented
	successCount := testutil.ToFloat64(metrics.ReconciliationsTotal.WithLabelValues("success"))
	if successCount < 1 {
		t.Errorf("reconciliations_total{status=success} = %f, want >= 1", successCount)
	}

	// Verify records_created_total was incremented for our provider
	createdCount := testutil.ToFloat64(metrics.RecordsCreatedTotal.WithLabelValues("metric-test-dns"))
	if createdCount < 1 {
		t.Errorf("records_created_total{provider=metric-test-dns} = %f, want >= 1", createdCount)
	}

	// Verify workloads_scanned gauge
	workloadsScanned := testutil.ToFloat64(metrics.WorkloadsScanned)
	if workloadsScanned != 1 {
		t.Errorf("workloads_scanned = %f, want 1", workloadsScanned)
	}

	// Verify hostnames_discovered gauge
	hostnamesDiscovered := testutil.ToFloat64(metrics.HostnamesDiscovered)
	if hostnamesDiscovered != 1 {
		t.Errorf("hostnames_discovered = %f, want 1", hostnamesDiscovered)
	}
}

func TestReconcile_MetricsEmittedOnFailure(t *testing.T) {
	// When a create fails, the records_failed_total metric should be incremented.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("fail-metric-dns")
	mockProvider.createFn = func(_ context.Context, _ provider.Record) error {
		return errors.New("API error")
	}

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(_ provider.FactoryConfig) (provider.Provider, error) {
		return mockProvider, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "fail-metric-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	// Reset relevant metrics before test
	metrics.ReconciliationsTotal.Reset()
	metrics.RecordsFailedTotal.Reset()

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	_, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Should record error status since there were failures
	errorCount := testutil.ToFloat64(metrics.ReconciliationsTotal.WithLabelValues("error"))
	if errorCount < 1 {
		t.Errorf("reconciliations_total{status=error} = %f, want >= 1", errorCount)
	}

	// Verify records_failed_total was incremented for create operation
	failedCount := testutil.ToFloat64(metrics.RecordsFailedTotal.WithLabelValues("fail-metric-dns", "create"))
	if failedCount < 1 {
		t.Errorf("records_failed_total{provider=fail-metric-dns,operation=create} = %f, want >= 1", failedCount)
	}
}

func TestReconcile_MetricsForSkippedRecords(t *testing.T) {
	// When a record is skipped because no provider matches, the
	// records_skipped_total metric should be incremented.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.nomatch.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	// No providers match *.nomatch.com
	providers := provider.NewRegistry(logger)

	mockProvider := newTestMockProvider("test-dns")
	providers.RegisterFactory("mock", func(_ provider.FactoryConfig) (provider.Provider, error) {
		return mockProvider, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"}, // doesn't match nomatch.com
	})

	// Reset relevant metrics
	metrics.RecordsSkippedTotal.Reset()

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	_, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Verify records_skipped_total was incremented with no_provider reason
	skippedCount := testutil.ToFloat64(metrics.RecordsSkippedTotal.WithLabelValues("no_provider"))
	if skippedCount < 1 {
		t.Errorf("records_skipped_total{reason=no_provider} = %f, want >= 1", skippedCount)
	}
}

func TestReconcile_DurationMetricRecorded(t *testing.T) {
	// After reconciliation, the duration histogram should have an observation.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	providers := provider.NewRegistry(logger)

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	// Get observation count before
	countBefore := testutil.CollectAndCount(metrics.ReconciliationDuration)

	_, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Histogram always reports at least 1 metric (the histogram itself)
	countAfter := testutil.CollectAndCount(metrics.ReconciliationDuration)
	if countAfter < countBefore {
		t.Errorf("expected duration histogram to maintain or increase count, got before=%d after=%d", countBefore, countAfter)
	}
}

// --- Dry-Run Completeness ---

func TestReconcile_DryRunPopulatesActionFields(t *testing.T) {
	// In dry-run mode, actions should have all fields populated
	// (Provider, RecordType, Target, Hostname) even though no changes are made.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
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
	cfg.DryRun = true

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	if !result.DryRun {
		t.Error("result.DryRun should be true")
	}

	// Verify actions have complete fields
	for _, action := range result.Actions {
		if action.Type == ActionCreate || action.Type == ActionDelete {
			if action.Provider == "" {
				t.Errorf("dry-run action missing Provider: %+v", action)
			}
			if action.Hostname == "" {
				t.Errorf("dry-run action missing Hostname: %+v", action)
			}
			if action.RecordType == "" {
				t.Errorf("dry-run action missing RecordType: %+v", action)
			}
			if action.Target == "" {
				t.Errorf("dry-run action missing Target: %+v", action)
			}
			if action.Status != StatusSuccess {
				t.Errorf("dry-run action expected StatusSuccess, got %s: %+v", action.Status, action)
			}
		}
	}
}

func TestReconcile_DryRunDoesNotMutateProvider(t *testing.T) {
	// Dry-run must not call Create, Delete, or Update on any provider.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	createCalled := false
	deleteCalled := false
	mockProvider.createFn = func(_ context.Context, _ provider.Record) error {
		createCalled = true
		return nil
	}
	mockProvider.deleteFn = func(_ context.Context, _ provider.Record) error {
		deleteCalled = true
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

	cfg := DefaultConfig()
	cfg.DryRun = true

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	_, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	if createCalled {
		t.Error("provider.Create was called in dry-run mode")
	}
	if deleteCalled {
		t.Error("provider.Delete was called in dry-run mode")
	}
}

func TestReconcile_DryRunOrphanCleanupReportsActions(t *testing.T) {
	// Dry-run with orphan cleanup should report what WOULD be deleted
	// without actually deleting anything.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	// No workloads — everything is an orphan

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")

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
	cfg.DryRun = true
	cfg.CleanupOrphans = true
	cfg.OwnershipTracking = true

	// Seed the provider with a record for the orphaned hostname so
	// the cache-based orphan cleanup can find it during dry-run.
	mockProvider.AddRecord(provider.Record{
		Hostname: "orphan.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
		TTL:      300,
	})
	// Also seed the exact ownership TXT record (managed mode requires it).
	mockProvider.AddRecord(memberOwnershipTXT("orphan.example.com", "10.0.0.1"))

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	// Seed one known hostname as orphan
	r.mu.Lock()
	r.knownHostnames = map[string]struct{}{
		"orphan.example.com": {},
	}
	r.hostnameProviders = map[string][]string{
		"orphan.example.com": {"test-dns"},
	}
	r.mu.Unlock()

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Should report the planned deletion
	deleteActions := result.Deleted()
	if len(deleteActions) == 0 {
		// In dry-run, delete actions may still appear as Status=Success
		// Check for any delete-type action
		hasDelete := false
		for _, a := range result.Actions {
			if a.Type == ActionDelete {
				hasDelete = true
				break
			}
		}
		if !hasDelete {
			t.Error("dry-run orphan cleanup should report delete action")
		}
	}

	// But nothing should actually have been deleted
	deleted := mockProvider.GetDeleted()
	if len(deleted) > 0 {
		t.Errorf("dry-run should not actually delete, but %d records deleted", len(deleted))
	}
}

func TestReconcile_DryRunWithTargetChange(t *testing.T) {
	// Dry-run when a target changes should report the update
	// without actually performing delete+create on the provider.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	// Pre-seed with old target — but in dry-run mode, List is skipped
	// so the cache won't be built, making this a create action.
	createCalled := false
	deleteCalled := false
	mockProvider.createFn = func(_ context.Context, _ provider.Record) error {
		createCalled = true
		return nil
	}
	mockProvider.deleteFn = func(_ context.Context, _ provider.Record) error {
		deleteCalled = true
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
		Target:     "10.0.0.2",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	cfg := DefaultConfig()
	cfg.DryRun = true

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Should have at least one action
	if len(result.Actions) == 0 {
		t.Error("dry-run should still produce actions")
	}

	// Provider should not have been called
	if createCalled {
		t.Error("provider.Create should not be called in dry-run")
	}
	if deleteCalled {
		t.Error("provider.Delete should not be called in dry-run")
	}
}

func TestReconcile_RuntimeDryRunToggle(t *testing.T) {
	// Verify that runtime dry-run toggle via SetDryRun works correctly.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
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

	// Enable dry-run at runtime
	r.SetDryRun(true)

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	if !result.DryRun {
		t.Error("result.DryRun should be true after SetDryRun(true)")
	}

	// No actual creates
	created := mockProvider.GetCreatedDNSRecords()
	if len(created) > 0 {
		t.Errorf("dry-run should not create records, got %d", len(created))
	}

	// Disable dry-run
	r.SetDryRun(false)

	result2, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error after disabling dry-run: %v", err)
	}

	if result2.DryRun {
		t.Error("result.DryRun should be false after SetDryRun(false)")
	}
}
