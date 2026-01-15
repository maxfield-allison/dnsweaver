package reconciler

import (
	"context"
	"errors"
	"testing"

	"gitlab.bluewillows.net/root/dnsweaver/internal/docker"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/source"
	"gitlab.bluewillows.net/root/dnsweaver/sources/traefik"
)

// =============================================================================
// Reconcile() Full Flow Tests
// These tests exercise the complete Reconcile() function using mock components.
// =============================================================================

func TestReconcile_EmptyWorkloads(t *testing.T) {
	// Setup: no workloads, no hostnames expected
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	logger := quietLogger()

	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	providers := provider.NewRegistry(logger)

	r := New(dockerMock, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())

	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if result == nil {
		t.Fatal("Reconcile returned nil result")
	}
	if result.WorkloadsScanned != 0 {
		t.Errorf("WorkloadsScanned = %d, want 0", result.WorkloadsScanned)
	}
	if result.HostnamesDiscovered != 0 {
		t.Errorf("HostnamesDiscovered = %d, want 0", result.HostnamesDiscovered)
	}
	if len(result.Actions) != 0 {
		t.Errorf("Actions = %d, want 0", len(result.Actions))
	}
}

func TestReconcile_CreatesRecordsForWorkloads(t *testing.T) {
	// Setup: one workload with Traefik label
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.AddWorkload("my-app", map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()

	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
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

	r := New(dockerMock, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())

	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if result.WorkloadsScanned != 1 {
		t.Errorf("WorkloadsScanned = %d, want 1", result.WorkloadsScanned)
	}
	if result.HostnamesDiscovered != 1 {
		t.Errorf("HostnamesDiscovered = %d, want 1", result.HostnamesDiscovered)
	}

	// Should have created the record
	created := mockProvider.GetCreatedDNSRecords()
	if len(created) != 1 {
		t.Fatalf("expected 1 created DNS record, got %d", len(created))
	}
	if created[0].Hostname != "app.example.com" {
		t.Errorf("created hostname = %q, want 'app.example.com'", created[0].Hostname)
	}
}

func TestReconcile_MultipleHostnamesFromOneWorkload(t *testing.T) {
	// Workload with multiple Host() rules
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.AddWorkload("multi-host", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app1.example.com`) || Host(`app2.example.com`)",
	})

	logger := quietLogger()

	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
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

	r := New(dockerMock, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())

	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if result.HostnamesDiscovered != 2 {
		t.Errorf("HostnamesDiscovered = %d, want 2", result.HostnamesDiscovered)
	}

	// Should have created 2 DNS records (plus ownership TXT records)
	created := mockProvider.GetCreatedDNSRecords()
	if len(created) != 2 {
		t.Fatalf("expected 2 created DNS records, got %d", len(created))
	}
}

func TestReconcile_MultipleWorkloads(t *testing.T) {
	// Setup: multiple workloads with different hostnames
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.AddWorkload("app1", map[string]string{
		"traefik.http.routers.app1.rule": "Host(`app1.example.com`)",
	})
	dockerMock.AddWorkload("app2", map[string]string{
		"traefik.http.routers.app2.rule": "Host(`app2.example.com`)",
	})
	dockerMock.AddWorkload("no-traefik", map[string]string{
		"some.other.label": "value",
	})

	logger := quietLogger()

	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
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

	r := New(dockerMock, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())

	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if result.WorkloadsScanned != 3 {
		t.Errorf("WorkloadsScanned = %d, want 3", result.WorkloadsScanned)
	}
	if result.HostnamesDiscovered != 2 {
		t.Errorf("HostnamesDiscovered = %d, want 2 (from 2 workloads with traefik labels)", result.HostnamesDiscovered)
	}

	created := mockProvider.GetCreatedDNSRecords()
	if len(created) != 2 {
		t.Errorf("expected 2 created DNS records, got %d", len(created))
	}
}

func TestReconcile_DryRunNoChanges(t *testing.T) {
	// Setup: dry-run mode should not create records
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.AddWorkload("my-app", map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()

	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
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

	r := New(dockerMock, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())

	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if !result.DryRun {
		t.Error("Result should indicate dry-run mode")
	}

	// Provider should NOT have been called
	created := mockProvider.GetCreated()
	if len(created) != 0 {
		t.Errorf("dry-run should not create records, got %d", len(created))
	}

	// But result should still show actions (what would have been done)
	if len(result.Actions) != 1 {
		t.Errorf("dry-run should still report actions, got %d", len(result.Actions))
	}
}

func TestReconcile_DockerListError(t *testing.T) {
	// Setup: Docker client returns an error
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.SetListError(errors.New("connection refused"))

	logger := quietLogger()

	sources := source.NewRegistry(logger)
	providers := provider.NewRegistry(logger)

	r := New(dockerMock, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())

	if err == nil {
		t.Fatal("expected error when Docker list fails")
	}
	if result != nil {
		t.Error("result should be nil on error")
	}
}

func TestReconcile_NoMatchingProvider(t *testing.T) {
	// Setup: hostname doesn't match any provider
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.AddWorkload("my-app", map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`app.other-domain.com`)",
	})

	logger := quietLogger()

	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mockProvider, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"}, // Only matches example.com
	})

	r := New(dockerMock, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())

	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if result.HostnamesDiscovered != 1 {
		t.Errorf("HostnamesDiscovered = %d, want 1", result.HostnamesDiscovered)
	}

	// Should have a skip action for no matching provider
	skipped := result.Skipped()
	if len(skipped) != 1 {
		t.Errorf("expected 1 skipped action, got %d", len(skipped))
	}

	// No records should have been created
	created := mockProvider.GetCreated()
	if len(created) != 0 {
		t.Errorf("expected no created records, got %d", len(created))
	}
}

func TestReconcile_DuplicateHostnameAcrossWorkloads(t *testing.T) {
	// Setup: same hostname in multiple workloads (first wins)
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.AddWorkload("first-app", map[string]string{
		"traefik.http.routers.first.rule": "Host(`app.example.com`)",
	})
	dockerMock.AddWorkload("second-app", map[string]string{
		"traefik.http.routers.second.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()

	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
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

	r := New(dockerMock, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())

	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if result.HostnamesDuplicate != 1 {
		t.Errorf("HostnamesDuplicate = %d, want 1", result.HostnamesDuplicate)
	}
	// Only one unique hostname should be discovered
	if result.HostnamesDiscovered != 1 {
		t.Errorf("HostnamesDiscovered = %d, want 1 (duplicates are counted once)", result.HostnamesDiscovered)
	}

	// Only one DNS record should be created
	created := mockProvider.GetCreatedDNSRecords()
	if len(created) != 1 {
		t.Errorf("expected 1 DNS record (not 2), got %d", len(created))
	}
}

func TestReconcile_OrphanCleanup(t *testing.T) {
	// Setup: provider has a record that isn't in any workload
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.AddWorkload("current-app", map[string]string{
		"traefik.http.routers.current.rule": "Host(`current.example.com`)",
	})

	logger := quietLogger()

	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	// Add existing record for current app
	mockProvider.AddRecord(provider.Record{
		Hostname: "current.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
		TTL:      300,
	})
	// Add ownership record for current app (correct format: _dnsweaver.hostname)
	mockProvider.AddRecord(provider.Record{
		Hostname: "_dnsweaver.current.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
		TTL:      300,
	})
	// Add orphan record (workload no longer exists)
	mockProvider.AddRecord(provider.Record{
		Hostname: "orphan.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
		TTL:      300,
	})
	// Add ownership record for orphan (so it can be deleted)
	mockProvider.AddRecord(provider.Record{
		Hostname: "_dnsweaver.orphan.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
		TTL:      300,
	})

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
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

	// First reconciliation to establish known hostnames
	r := New(dockerMock, sources, providers,
		WithConfig(Config{
			Enabled:           true,
			CleanupOrphans:    true,
			OwnershipTracking: true,
		}),
		WithLogger(logger),
	)

	// Set known hostnames to include the orphan (simulate previous reconciliation)
	r.mu.Lock()
	r.knownHostnames["orphan.example.com"] = struct{}{}
	r.knownHostnames["current.example.com"] = struct{}{}
	r.mu.Unlock()

	result, err := r.Reconcile(context.Background())

	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// Should have deleted actions for the orphan
	deleted := result.Deleted()
	// Orphan cleanup should trigger deletion
	if len(deleted) < 1 {
		t.Logf("Actions: %+v", result.Actions)
		t.Errorf("expected at least 1 delete action for orphan, got %d", len(deleted))
	}
}

func TestReconcile_DisabledReturnsEmpty(t *testing.T) {
	// This is already tested in reconciler_test.go but adding here for completeness
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.AddWorkload("my-app", map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	providers := provider.NewRegistry(logger)

	cfg := DefaultConfig()
	cfg.Enabled = false

	r := New(dockerMock, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())

	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if len(result.Actions) != 0 {
		t.Errorf("disabled reconciler should return no actions, got %d", len(result.Actions))
	}
	// WorkloadsScanned should be 0 since we returned early
	if result.WorkloadsScanned != 0 {
		t.Errorf("disabled reconciler should not scan workloads, got %d", result.WorkloadsScanned)
	}
}

func TestReconcile_KnownHostnamesUpdated(t *testing.T) {
	// Verify that knownHostnames is updated after reconciliation
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.AddWorkload("app1", map[string]string{
		"traefik.http.routers.app1.rule": "Host(`app1.example.com`)",
	})
	dockerMock.AddWorkload("app2", map[string]string{
		"traefik.http.routers.app2.rule": "Host(`app2.example.com`)",
	})

	logger := quietLogger()

	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
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

	r := New(dockerMock, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	// Before reconciliation, knownHostnames should be empty
	if len(r.KnownHostnames()) != 0 {
		t.Errorf("initial KnownHostnames should be empty, got %d", len(r.KnownHostnames()))
	}

	_, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// After reconciliation, knownHostnames should contain both hostnames
	known := r.KnownHostnames()
	if len(known) != 2 {
		t.Errorf("KnownHostnames should have 2 entries, got %d", len(known))
	}

	// Verify both hostnames are tracked
	foundApp1, foundApp2 := false, false
	for _, h := range known {
		if h == "app1.example.com" {
			foundApp1 = true
		}
		if h == "app2.example.com" {
			foundApp2 = true
		}
	}
	if !foundApp1 || !foundApp2 {
		t.Errorf("expected both app1 and app2 in KnownHostnames, got %v", known)
	}
}

func TestReconcile_OwnershipRecordsCreated(t *testing.T) {
	// Verify ownership TXT records are created when OwnershipTracking is enabled
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.AddWorkload("my-app", map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()

	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
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
	cfg.OwnershipTracking = true

	r := New(dockerMock, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	_, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// Check for ownership TXT record
	ownershipRecords := mockProvider.GetCreatedOwnershipRecords()
	if len(ownershipRecords) != 1 {
		t.Errorf("expected 1 ownership TXT record, got %d", len(ownershipRecords))
	}
	if len(ownershipRecords) > 0 && ownershipRecords[0].Type != provider.RecordTypeTXT {
		t.Errorf("ownership record should be TXT, got %s", ownershipRecords[0].Type)
	}
}

func TestReconcile_NoOwnershipWhenDisabled(t *testing.T) {
	// Verify ownership TXT records are NOT created when OwnershipTracking is disabled
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.AddWorkload("my-app", map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()

	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
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
	cfg.OwnershipTracking = false

	r := New(dockerMock, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	_, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// Check that NO ownership TXT records were created
	ownershipRecords := mockProvider.GetCreatedOwnershipRecords()
	if len(ownershipRecords) != 0 {
		t.Errorf("expected 0 ownership TXT records when disabled, got %d", len(ownershipRecords))
	}

	// But DNS records should still be created
	dnsRecords := mockProvider.GetCreatedDNSRecords()
	if len(dnsRecords) != 1 {
		t.Errorf("expected 1 DNS record, got %d", len(dnsRecords))
	}
}

// =============================================================================
// RecoverOwnership Tests
// =============================================================================

func TestRecoverOwnership_SkipsWhenDisabled(t *testing.T) {
	// RecoverOwnership should skip when CleanupOrphans or OwnershipTracking is disabled
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	logger := quietLogger()
	sources := source.NewRegistry(logger)
	providers := provider.NewRegistry(logger)

	testCases := []struct {
		name              string
		cleanupOrphans    bool
		ownershipTracking bool
	}{
		{"cleanup disabled", false, true},
		{"ownership disabled", true, false},
		{"both disabled", false, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.CleanupOrphans = tc.cleanupOrphans
			cfg.OwnershipTracking = tc.ownershipTracking

			r := New(dockerMock, sources, providers,
				WithConfig(cfg),
				WithLogger(logger),
			)

			err := r.RecoverOwnership(context.Background())
			if err != nil {
				t.Errorf("RecoverOwnership should not error when skipped: %v", err)
			}

			// Should not have recovered any hostnames
			if len(r.KnownHostnames()) != 0 {
				t.Errorf("should not recover hostnames when disabled, got %d", len(r.KnownHostnames()))
			}
		})
	}
}

func TestRecoverOwnership_RecoversHostnamesFromProvider(t *testing.T) {
	// RecoverOwnership should populate knownHostnames from ownership records
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	logger := quietLogger()
	sources := source.NewRegistry(logger)

	mockProvider := newTestMockProvider("test-dns")
	// Add ownership TXT records (simulating records created before restart)
	mockProvider.AddRecord(provider.Record{
		Hostname: "_dnsweaver.app1.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
		TTL:      300,
	})
	mockProvider.AddRecord(provider.Record{
		Hostname: "_dnsweaver.app2.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
		TTL:      300,
	})

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
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

	r := New(dockerMock, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	// Before recovery, should be empty
	if len(r.KnownHostnames()) != 0 {
		t.Errorf("initial KnownHostnames should be empty, got %d", len(r.KnownHostnames()))
	}

	err := r.RecoverOwnership(context.Background())
	if err != nil {
		t.Fatalf("RecoverOwnership returned error: %v", err)
	}

	// After recovery, should have both hostnames
	known := r.KnownHostnames()
	if len(known) != 2 {
		t.Errorf("expected 2 recovered hostnames, got %d", len(known))
	}

	// Verify both hostnames
	foundApp1, foundApp2 := false, false
	for _, h := range known {
		if h == "app1.example.com" {
			foundApp1 = true
		}
		if h == "app2.example.com" {
			foundApp2 = true
		}
	}
	if !foundApp1 || !foundApp2 {
		t.Errorf("expected app1 and app2 in recovered hostnames, got %v", known)
	}
}

func TestRecoverOwnership_MultipleProviders(t *testing.T) {
	// RecoverOwnership should recover from all providers
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	logger := quietLogger()
	sources := source.NewRegistry(logger)

	mockProvider1 := newTestMockProvider("provider1")
	mockProvider1.AddRecord(provider.Record{
		Hostname: "_dnsweaver.p1-app.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
		TTL:      300,
	})

	mockProvider2 := newTestMockProvider("provider2")
	mockProvider2.AddRecord(provider.Record{
		Hostname: "_dnsweaver.p2-app.internal.local",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
		TTL:      300,
	})

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		if cfg.Name == "provider1" {
			return mockProvider1, nil
		}
		return mockProvider2, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "provider1",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "provider2",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.2",
		TTL:        300,
		Domains:    []string{"*.internal.local"},
	})

	cfg := DefaultConfig()
	cfg.CleanupOrphans = true
	cfg.OwnershipTracking = true

	r := New(dockerMock, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	err := r.RecoverOwnership(context.Background())
	if err != nil {
		t.Fatalf("RecoverOwnership returned error: %v", err)
	}

	// Should have recovered from both providers
	known := r.KnownHostnames()
	if len(known) != 2 {
		t.Errorf("expected 2 recovered hostnames from 2 providers, got %d", len(known))
	}
}

// =============================================================================
// Edge Case Tests — Potential Bug Detection
// =============================================================================

// TestReconcile_CaseSensitivity verifies that hostnames differing only in case
// are treated as the same hostname (DNS is case-insensitive per RFC 1035).
// BUG: Currently the code uses case-sensitive map keys, so this test FAILS.
func TestReconcile_CaseSensitivity(t *testing.T) {
	// Two workloads with same hostname in different cases
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.AddWorkload("app-lowercase", map[string]string{
		"traefik.http.routers.app1.rule": "Host(`app.example.com`)",
	})
	dockerMock.AddWorkload("app-uppercase", map[string]string{
		"traefik.http.routers.app2.rule": "Host(`APP.EXAMPLE.COM`)",
	})

	logger := quietLogger()

	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
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

	r := New(dockerMock, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())

	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// DNS is case-insensitive, so these should be treated as duplicates
	// Currently FAILS because code uses case-sensitive comparison
	if result.HostnamesDuplicate != 1 {
		t.Errorf("HostnamesDuplicate = %d, want 1 (DNS is case-insensitive)", result.HostnamesDuplicate)
	}

	// Should only create ONE DNS record
	created := mockProvider.GetCreatedDNSRecords()
	if len(created) != 1 {
		t.Errorf("expected 1 DNS record (case-insensitive dedup), got %d", len(created))
	}
}

// TestReconcile_ProviderCreateFailsAfterDelete tests the scenario where:
// 1. Old record exists with wrong target
// 2. Old record is deleted successfully
// 3. New record creation FAILS
// Result: Hostname has NO DNS record - partial failure state
func TestReconcile_ProviderCreateFailsAfterDelete(t *testing.T) {
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.AddWorkload("my-app", map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()

	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	// Add existing record with OLD target
	mockProvider.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.99", // Old target - will be deleted
		TTL:      300,
	})

	// Make Create fail
	createCallCount := 0
	mockProvider.createFn = func(_ context.Context, r provider.Record) error {
		createCallCount++
		// Fail DNS record creation but not ownership TXT
		if r.Type != provider.RecordTypeTXT {
			return errors.New("provider temporarily unavailable")
		}
		return nil
	}

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mockProvider, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1", // New target - creation will fail
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := New(dockerMock, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	// Build cache first
	result, err := r.Reconcile(context.Background())

	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// Old record should have been deleted
	deleted := mockProvider.GetDeleted()
	if len(deleted) < 1 {
		t.Errorf("expected old record to be deleted, got %d deletions", len(deleted))
	}

	// Create should have been attempted and failed
	if createCallCount == 0 {
		t.Error("expected Create to be called")
	}

	// Result should show a FAILED action
	failed := result.Failed()
	if len(failed) != 1 {
		t.Errorf("expected 1 failed action, got %d", len(failed))
	}

	// The concerning state: old record deleted, new record not created
	// This is a known limitation - the test documents it
	t.Log("Note: After this failure, the hostname has no DNS record (old deleted, new failed)")
}

// TestReconcile_FirstRunAfterRestart verifies that the first reconciliation
// after restart (empty knownHostnames) doesn't delete valid records as orphans.
func TestReconcile_FirstRunAfterRestart(t *testing.T) {
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.AddWorkload("my-app", map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()

	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	// Existing record from before restart
	mockProvider.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
		TTL:      300,
	})
	mockProvider.AddRecord(provider.Record{
		Hostname: "_dnsweaver.app.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
		TTL:      300,
	})

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
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

	r := New(dockerMock, sources, providers,
		WithConfig(Config{
			Enabled:           true,
			CleanupOrphans:    true,
			OwnershipTracking: true,
		}),
		WithLogger(logger),
	)

	// First reconciliation - knownHostnames is empty
	result, err := r.Reconcile(context.Background())

	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// Record should NOT be deleted (it's discovered from workload, not orphan)
	deleted := mockProvider.GetDeleted()
	if len(deleted) != 0 {
		t.Errorf("expected 0 deletions on first run, got %d: %+v", len(deleted), deleted)
	}

	// Record should be recognized as existing (skip, not create)
	skipped := result.Skipped()
	foundSkip := false
	for _, s := range skipped {
		if s.Hostname == "app.example.com" && s.Error == "record already exists" {
			foundSkip = true
			break
		}
	}
	if !foundSkip {
		t.Error("expected existing record to be skipped")
	}
}

// TestReconcile_AdoptExistingEnabled verifies that AdoptExisting=true creates
// ownership TXT records for pre-existing DNS records.
func TestReconcile_AdoptExistingEnabled(t *testing.T) {
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.AddWorkload("my-app", map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()

	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	// Existing record WITHOUT ownership TXT (created manually or by another system)
	mockProvider.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1", // Same target as provider config
		TTL:      300,
	})
	// NO ownership TXT record

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
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
	cfg.AdoptExisting = true
	cfg.OwnershipTracking = true

	r := New(dockerMock, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	_, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// Should have created ownership TXT record (adopting the existing record)
	ownershipRecords := mockProvider.GetCreatedOwnershipRecords()
	if len(ownershipRecords) != 1 {
		t.Errorf("expected 1 ownership TXT record (adoption), got %d", len(ownershipRecords))
	}

	// DNS record should NOT be re-created (it already exists)
	dnsRecords := mockProvider.GetCreatedDNSRecords()
	if len(dnsRecords) != 0 {
		t.Errorf("expected 0 DNS records created (already exists), got %d", len(dnsRecords))
	}
}

// TestReconcile_AdoptExistingDisabled verifies that AdoptExisting=false does NOT
// create ownership TXT records for pre-existing DNS records.
func TestReconcile_AdoptExistingDisabled(t *testing.T) {
	dockerMock := newTestMockWorkloadLister(docker.ModeSwarm)
	dockerMock.AddWorkload("my-app", map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()

	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	// Existing record WITHOUT ownership TXT
	mockProvider.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
		TTL:      300,
	})

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
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
	cfg.AdoptExisting = false // Explicitly disabled
	cfg.OwnershipTracking = true

	r := New(dockerMock, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	_, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// Should NOT create ownership TXT (AdoptExisting is false)
	ownershipRecords := mockProvider.GetCreatedOwnershipRecords()
	if len(ownershipRecords) != 0 {
		t.Errorf("expected 0 ownership TXT records (no adoption), got %d", len(ownershipRecords))
	}
}
