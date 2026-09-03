package reconciler

import (
	"context"
	"errors"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

// =============================================================================
// ensureRecord Tests
// =============================================================================

func TestEnsureRecord_CreatesNewRecord(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	logger := quietLogger()
	providers := provider.NewRegistry(logger)

	// Register factory and create instance
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	r := &Reconciler{
		providers:      providers,
		config:         DefaultConfig(),
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	hostname := &source.Hostname{Name: "app.example.com", Source: "test"}
	actions := r.ensureRecord(context.Background(), hostname, nil)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionCreate {
		t.Errorf("expected ActionCreate, got %v", actions[0].Type)
	}
	if actions[0].Status != StatusSuccess {
		t.Errorf("expected StatusSuccess, got %v", actions[0].Status)
	}
	if actions[0].Hostname != "app.example.com" {
		t.Errorf("expected hostname 'app.example.com', got %q", actions[0].Hostname)
	}

	// Verify provider was called
	created := mock.GetCreated()
	if len(created) == 0 {
		t.Error("expected provider Create to be called")
	}
}

func TestEnsureRecord_SkipsExistingRecord(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	// Add existing record with matching target
	mock.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
		TTL:      300,
	})

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	// Build cache from provider
	cache := newRecordCache(context.Background(), providers, logger)

	r := &Reconciler{
		providers:      providers,
		config:         DefaultConfig(),
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	hostname := &source.Hostname{Name: "app.example.com", Source: "test"}
	actions := r.ensureRecord(context.Background(), hostname, cache)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionSkip {
		t.Errorf("expected ActionSkip, got %v", actions[0].Type)
	}
	if actions[0].Error != "record already exists" {
		t.Errorf("expected 'record already exists' error, got %q", actions[0].Error)
	}
}

func TestEnsureRecord_UpdatesChangedTarget(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	// Add existing record with OLD target
	mock.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.99", // Old target
		TTL:      300,
	})
	mock.AddRecord(ownershipTXT("app.example.com"))

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1", // New target
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	cache := newRecordCache(context.Background(), providers, logger)

	r := &Reconciler{
		providers:      providers,
		config:         DefaultConfig(),
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	hostname := &source.Hostname{Name: "app.example.com", Source: "test"}
	actions := r.ensureRecord(context.Background(), hostname, cache)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionUpdate {
		t.Errorf("expected ActionUpdate, got %v", actions[0].Type)
	}
	if actions[0].Status != StatusSuccess {
		t.Errorf("expected StatusSuccess, got %v", actions[0].Status)
	}

	// Verify old record was deleted and new created
	deleted := mock.GetDeleted()
	if len(deleted) != 1 {
		t.Errorf("expected 1 deletion, got %d", len(deleted))
	}
	if len(deleted) > 0 && deleted[0].Target != "10.0.0.99" {
		t.Errorf("expected old target '10.0.0.99' to be deleted, got %q", deleted[0].Target)
	}

	// Check created records - find one with new target
	created := mock.GetCreated()
	var foundNewTarget bool
	for _, c := range created {
		if c.Hostname == "app.example.com" && c.Target == "10.0.0.1" {
			foundNewTarget = true
			break
		}
	}
	if !foundNewTarget {
		t.Error("expected new target '10.0.0.1' to be created")
	}
}

func TestEnsureRecord_SkipsTypeConflict(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	// Add existing CNAME record
	mock.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeCNAME, // Type conflict - want A, have CNAME
		Target:   "other.example.com",
		TTL:      300,
	})

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA, // We want A
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	cache := newRecordCache(context.Background(), providers, logger)

	r := &Reconciler{
		providers:      providers,
		config:         DefaultConfig(),
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	hostname := &source.Hostname{Name: "app.example.com", Source: "test"}
	actions := r.ensureRecord(context.Background(), hostname, cache)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionSkip {
		t.Errorf("expected ActionSkip, got %v", actions[0].Type)
	}
	if actions[0].Status != StatusSkipped {
		t.Errorf("expected StatusSkipped, got %v", actions[0].Status)
	}
	// Should contain "conflict" in the error
	if actions[0].Error == "" || !containsHelper(actions[0].Error, "conflict") {
		t.Errorf("expected conflict error, got %q", actions[0].Error)
	}
}

func TestEnsureRecord_NoMatchingProvider(t *testing.T) {
	mock := newTestMockProvider("test-dns")

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	// Only matches *.internal.local
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.internal.local"},
	})

	r := &Reconciler{
		providers:      providers,
		config:         DefaultConfig(),
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	// Hostname doesn't match pattern
	hostname := &source.Hostname{Name: "app.example.com", Source: "test"}
	actions := r.ensureRecord(context.Background(), hostname, nil)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionSkip {
		t.Errorf("expected ActionSkip, got %v", actions[0].Type)
	}
	if !containsHelper(actions[0].Error, "no matching provider") {
		t.Errorf("expected 'no matching provider' error, got %q", actions[0].Error)
	}
}

func TestEnsureRecord_DryRunDoesNotCallProvider(t *testing.T) {
	mock := newTestMockProvider("test-dns")

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := &Reconciler{
		providers:      providers,
		config:         Config{DryRun: true, Enabled: true},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	hostname := &source.Hostname{Name: "app.example.com", Source: "test"}
	actions := r.ensureRecord(context.Background(), hostname, nil)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionCreate {
		t.Errorf("expected ActionCreate, got %v", actions[0].Type)
	}
	if actions[0].Status != StatusSuccess {
		t.Errorf("expected StatusSuccess in dry-run, got %v", actions[0].Status)
	}

	// Verify provider was NOT called
	created := mock.GetCreated()
	if len(created) != 0 {
		t.Error("dry-run should NOT call provider Create")
	}
}

func TestEnsureRecord_WithRecordHints(t *testing.T) {
	mock := newTestMockProvider("test-dns")

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1", // Default target
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := &Reconciler{
		providers:      providers,
		config:         DefaultConfig(),
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	// Hostname with hints that override target
	hostname := &source.Hostname{
		Name:   "app.example.com",
		Source: "test",
		RecordHints: &source.RecordHints{
			Type:   "CNAME",
			Target: "custom.example.com",
			TTL:    600,
		},
	}
	actions := r.ensureRecord(context.Background(), hostname, nil)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}

	created := mock.GetCreated()
	// Find the record with hints applied
	var foundHintedRecord bool
	for _, c := range created {
		if c.Target == "custom.example.com" {
			foundHintedRecord = true
			// Verify hints were applied
			if c.Type != provider.RecordTypeCNAME {
				t.Errorf("expected CNAME type from hints, got %v", c.Type)
			}
			if c.TTL != 600 {
				t.Errorf("expected TTL 600 from hints, got %d", c.TTL)
			}
		}
	}
	if !foundHintedRecord {
		t.Error("expected record with custom target from hints")
	}
}

func TestEnsureRecord_ExplicitProviderHint(t *testing.T) {
	mock1 := newTestMockProvider("internal-dns")
	mock2 := newTestMockProvider("external-dns")

	logger := quietLogger()
	providers := provider.NewRegistry(logger)

	// Track DNS record calls per provider (exclude TXT ownership records)
	var internalCalls, externalCalls int

	providers.RegisterFactory("mock-internal", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		mock1.createFn = func(_ context.Context, r provider.Record) error {
			if r.Type != provider.RecordTypeTXT {
				internalCalls++
			}
			return nil
		}
		return mock1, nil
	})
	providers.RegisterFactory("mock-external", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		mock2.createFn = func(_ context.Context, r provider.Record) error {
			if r.Type != provider.RecordTypeTXT {
				externalCalls++
			}
			return nil
		}
		return mock2, nil
	})

	// Both providers match *.example.com
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "internal-dns",
		TypeName:   "mock-internal",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "external-dns",
		TypeName:   "mock-external",
		RecordType: provider.RecordTypeA,
		Target:     "203.0.113.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := &Reconciler{
		providers:      providers,
		config:         Config{Enabled: true, OwnershipTracking: true},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	// Route to specific provider via hint
	hostname := &source.Hostname{
		Name:   "app.example.com",
		Source: "test",
		RecordHints: &source.RecordHints{
			Provider: "external-dns",
		},
	}
	actions := r.ensureRecord(context.Background(), hostname, nil)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Provider != "external-dns" {
		t.Errorf("expected action for external-dns, got %q", actions[0].Provider)
	}

	// Verify only external-dns was called
	if internalCalls != 0 {
		t.Errorf("internal-dns should NOT be called when explicit provider hint is set, got %d calls", internalCalls)
	}
	if externalCalls != 1 {
		t.Errorf("external-dns should be called once, got %d calls", externalCalls)
	}
}

func TestEnsureRecord_ProviderCreateFails(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	mock.createFn = func(_ context.Context, _ provider.Record) error {
		return errors.New("network timeout")
	}

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := &Reconciler{
		providers:      providers,
		config:         DefaultConfig(),
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	hostname := &source.Hostname{Name: "app.example.com", Source: "test"}
	actions := r.ensureRecord(context.Background(), hostname, nil)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionCreate {
		t.Errorf("expected ActionCreate, got %v", actions[0].Type)
	}
	if actions[0].Status != StatusFailed {
		t.Errorf("expected StatusFailed, got %v", actions[0].Status)
	}
	if !containsHelper(actions[0].Error, "network timeout") {
		t.Errorf("expected 'network timeout' in error, got %q", actions[0].Error)
	}
}

// =============================================================================
// deleteRecord Tests
// =============================================================================

func TestDeleteRecord_DeletesExistingRecord(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	mock.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
		TTL:      300,
	})

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := &Reconciler{
		providers:      providers,
		config:         Config{OwnershipTracking: false, Enabled: true},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	actions := r.deleteRecord(context.Background(), "app.example.com")

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionDelete {
		t.Errorf("expected ActionDelete, got %v", actions[0].Type)
	}
	if actions[0].Status != StatusSuccess {
		t.Errorf("expected StatusSuccess, got %v", actions[0].Status)
	}

	deleted := mock.GetDeleted()
	if len(deleted) != 1 {
		t.Error("expected provider Delete to be called")
	}
}

func TestDeleteRecord_NoMatchingProvider(t *testing.T) {
	mock := newTestMockProvider("test-dns")

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.internal.local"},
	})

	r := &Reconciler{
		providers:      providers,
		config:         DefaultConfig(),
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	// No provider matches example.com
	actions := r.deleteRecord(context.Background(), "app.example.com")

	if len(actions) != 0 {
		t.Errorf("expected 0 actions for unmatched hostname, got %d", len(actions))
	}
}

func TestDeleteRecord_DryRunDoesNotDelete(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	mock.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := &Reconciler{
		providers:      providers,
		config:         Config{DryRun: true, Enabled: true},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	actions := r.deleteRecord(context.Background(), "app.example.com")

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionDelete {
		t.Errorf("expected ActionDelete, got %v", actions[0].Type)
	}

	// Verify provider was NOT called
	if len(mock.GetDeleted()) != 0 {
		t.Error("dry-run should NOT call provider Delete")
	}
}

func TestDeleteRecord_WithOwnershipTracking(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	mock.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	// Add ownership record
	mock.AddRecord(provider.Record{
		Hostname: "_dnsweaver.app.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
	})

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := &Reconciler{
		providers:      providers,
		config:         Config{OwnershipTracking: true, Enabled: true},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	actions := r.deleteRecord(context.Background(), "app.example.com")

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}

	// Should have deleted both the main record and ownership TXT
	deleted := mock.GetDeleted()
	if len(deleted) != 2 {
		t.Errorf("expected 2 deletions (record + ownership), got %d", len(deleted))
	}
}

// =============================================================================
// cleanupOrphans Tests
// =============================================================================

func TestCleanupOrphans_DeletesRemovedHostnames(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	mock.AddRecord(provider.Record{
		Hostname: "old.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	// Add ownership record so it will be deleted
	mock.AddRecord(provider.Record{
		Hostname: "_dnsweaver.old.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
	})

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	cache := newRecordCache(context.Background(), providers, logger)

	r := &Reconciler{
		providers: providers,
		config:    Config{CleanupOrphans: true, OwnershipTracking: true, Enabled: true},
		logger:    logger,
		knownHostnames: map[string]struct{}{
			"old.example.com":     {}, // Was known before
			"current.example.com": {},
		},
	}
	r.syncAtomics()

	// Current hostnames - "old.example.com" is gone
	currentHostnames := map[string]*source.Hostname{
		"current.example.com": {Name: "current.example.com", Source: "test"},
	}

	actions := r.cleanupOrphans(context.Background(), currentHostnames, cache)

	// Should have actions for deleting old.example.com
	if len(actions) == 0 {
		t.Error("expected at least 1 action for orphan cleanup")
	}

	var foundDelete bool
	for _, action := range actions {
		if action.Hostname == "old.example.com" && action.Type == ActionDelete {
			foundDelete = true
		}
	}
	if !foundDelete {
		t.Error("expected delete action for old.example.com")
	}
}

func TestCleanupOrphans_SkipsUnownedRecords(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	mock.AddRecord(provider.Record{
		Hostname: "manual.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	// NO ownership record - should be skipped

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	cache := newRecordCache(context.Background(), providers, logger)

	r := &Reconciler{
		providers: providers,
		config:    Config{CleanupOrphans: true, OwnershipTracking: true, Enabled: true},
		logger:    logger,
		knownHostnames: map[string]struct{}{
			"manual.example.com": {}, // Was known before
		},
	}
	r.syncAtomics()

	// No current hostnames - manual.example.com is orphaned
	currentHostnames := map[string]*source.Hostname{}

	actions := r.cleanupOrphans(context.Background(), currentHostnames, cache)

	// Should skip because no ownership record
	for _, action := range actions {
		if action.Hostname == "manual.example.com" {
			if action.Type != ActionSkip {
				t.Errorf("expected ActionSkip for unowned record, got %v", action.Type)
			}
		}
	}

	// Verify record was NOT deleted
	deleted := mock.GetDeleted()
	for _, d := range deleted {
		if d.Hostname == "manual.example.com" {
			t.Error("unowned record should NOT be deleted")
		}
	}
}

func TestCleanupOrphans_NoOrphans(t *testing.T) {
	logger := quietLogger()
	providers := provider.NewRegistry(logger)

	r := &Reconciler{
		providers: providers,
		config:    Config{CleanupOrphans: true, Enabled: true},
		logger:    logger,
		knownHostnames: map[string]struct{}{
			"app.example.com": {},
		},
	}
	r.syncAtomics()

	// Same hostname still exists - no orphans
	currentHostnames := map[string]*source.Hostname{
		"app.example.com": {Name: "app.example.com", Source: "test"},
	}

	actions := r.cleanupOrphans(context.Background(), currentHostnames, nil)

	if len(actions) != 0 {
		t.Errorf("expected 0 actions when no orphans, got %d", len(actions))
	}
}

// =============================================================================
// Ownership Record Tests
// =============================================================================

func TestEnsureOwnershipRecord_CreatesWhenEnabled(t *testing.T) {
	mock := newTestMockProvider("test-dns")

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := &Reconciler{
		providers:      providers,
		config:         Config{OwnershipTracking: true, Enabled: true},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	inst, _ := providers.Get("test-dns")
	r.ensureOwnershipRecord(context.Background(), "app.example.com", inst, nil, nil)

	created := mock.GetCreated()
	var foundOwnership bool
	for _, c := range created {
		if c.Hostname == "_dnsweaver.app.example.com" && c.Type == provider.RecordTypeTXT {
			foundOwnership = true
			if c.Target != "heritage=dnsweaver" {
				t.Errorf("expected ownership value 'heritage=dnsweaver', got %q", c.Target)
			}
		}
	}
	if !foundOwnership {
		t.Error("ownership TXT record should be created")
	}
}

func TestEnsureOwnershipRecord_SkipsWhenDisabled(t *testing.T) {
	mock := newTestMockProvider("test-dns")

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := &Reconciler{
		providers:      providers,
		config:         Config{OwnershipTracking: false, Enabled: true}, // Disabled
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	inst, _ := providers.Get("test-dns")
	r.ensureOwnershipRecord(context.Background(), "app.example.com", inst, nil, nil)

	created := mock.GetCreated()
	for _, c := range created {
		if c.Type == provider.RecordTypeTXT {
			t.Error("ownership TXT record should NOT be created when tracking disabled")
		}
	}
}

// TestEnsureOwnershipRecord_SkipsWhenCacheHasIt is a regression test for
// https://github.com/maxfield-allison/dnsweaver/issues/87 — the steady-state
// reconcile loop must not re-issue an ownership Create when the cache already
// shows the record exists, because upstream DNS servers (e.g. Technitium) log
// every duplicate-create as an error.
func TestEnsureOwnershipRecord_SkipsWhenCacheHasIt(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	// Pre-seed an existing ownership TXT record (legacy format, no instance ID).
	mock.AddRecord(provider.Record{
		Hostname: "_dnsweaver.app.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
		TTL:      300,
	})

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	cache := newRecordCache(context.Background(), providers, logger)

	r := &Reconciler{
		providers:      providers,
		config:         Config{OwnershipTracking: true, Enabled: true},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	inst, _ := providers.Get("test-dns")
	r.ensureOwnershipRecord(context.Background(), "app.example.com", inst, nil, cache)

	for _, c := range mock.GetCreated() {
		if c.Type == provider.RecordTypeTXT {
			t.Errorf("expected no ownership Create when cache shows record exists, got Create for %q", c.Hostname)
		}
	}
}

// =============================================================================
// Multiple Provider Tests
// =============================================================================

// TestEnsureRecord_MultipleMatchingProviders verifies first-match-wins
// precedence (issue #86) when matching providers share the same backend
// identity: only the first one in DNSWEAVER_INSTANCES declaration order
// writes the record. The other matching instances are skipped to prevent
// the providers from racing over the same physical record store.
//
// Mocks here both report the default mock identity (no Identity field set
// — IdentityOf falls back to {Type: "mock"}), so they collide. For the
// distinct-identity case where every backend writes, see
// TestEnsureRecord_DistinctIdentities_AllWrite.
func TestEnsureRecord_MultipleMatchingProviders(t *testing.T) {
	mock1 := newTestMockProvider("internal-dns")
	mock2 := newTestMockProvider("external-dns")

	logger := quietLogger()
	providers := provider.NewRegistry(logger)

	// Track DNS record calls per provider (exclude TXT ownership records)
	var createdMock1, createdMock2 int

	providers.RegisterFactory("mock-internal", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		mock1.createFn = func(_ context.Context, r provider.Record) error {
			if r.Type != provider.RecordTypeTXT {
				createdMock1++
			}
			return nil
		}
		return mock1, nil
	})
	providers.RegisterFactory("mock-external", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		mock2.createFn = func(_ context.Context, r provider.Record) error {
			if r.Type != provider.RecordTypeTXT {
				createdMock2++
			}
			return nil
		}
		return mock2, nil
	})

	// Both match *.example.com — internal-dns is declared first and should win.
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "internal-dns",
		TypeName:   "mock-internal",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "external-dns",
		TypeName:   "mock-external",
		RecordType: provider.RecordTypeA,
		Target:     "203.0.113.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := &Reconciler{
		providers:      providers,
		config:         Config{Enabled: true, OwnershipTracking: true},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	hostname := &source.Hostname{Name: "app.example.com", Source: "test"}
	actions := r.ensureRecord(context.Background(), hostname, nil)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action (first-match-wins), got %d", len(actions))
	}
	if actions[0].Provider != "internal-dns" {
		t.Errorf("expected winner=internal-dns (first declared), got %q", actions[0].Provider)
	}

	// Only the winning provider should have been called.
	if createdMock1 != 1 {
		t.Errorf("internal-dns should be called once, got %d", createdMock1)
	}
	if createdMock2 != 0 {
		t.Errorf("external-dns should NOT be called (loser), got %d", createdMock2)
	}
}

// TestEnsureRecord_DistinctIdentities_AllWrite verifies the regression fix
// for issue #88: when matching providers report different backend identities
// (different physical DNS systems), every matching instance writes the
// record. This is the core dnsweaver use case — publishing one hostname
// into multiple actual DNS systems (e.g. internal Technitium + public
// Cloudflare) simultaneously.
func TestEnsureRecord_DistinctIdentities_AllWrite(t *testing.T) {
	mockInternal := newTestMockProvider("internal-dns")
	mockInternal.identity = &provider.ProviderIdentity{
		Type:     "mock",
		Endpoint: "http://internal.dns",
		Zone:     "example.com",
	}
	mockExternal := newTestMockProvider("external-dns")
	mockExternal.identity = &provider.ProviderIdentity{
		Type:     "mock",
		Endpoint: "http://external.dns",
		Zone:     "example.com",
	}

	logger := quietLogger()
	providers := provider.NewRegistry(logger)

	var createdInternal, createdExternal int
	mockInternal.createFn = func(_ context.Context, r provider.Record) error {
		if r.Type != provider.RecordTypeTXT {
			createdInternal++
		}
		return nil
	}
	mockExternal.createFn = func(_ context.Context, r provider.Record) error {
		if r.Type != provider.RecordTypeTXT {
			createdExternal++
		}
		return nil
	}

	providers.RegisterFactory("mock-internal", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mockInternal, nil
	})
	providers.RegisterFactory("mock-external", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mockExternal, nil
	})

	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "internal-dns",
		TypeName:   "mock-internal",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "external-dns",
		TypeName:   "mock-external",
		RecordType: provider.RecordTypeA,
		Target:     "203.0.113.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := &Reconciler{
		providers:      providers,
		config:         Config{Enabled: true, OwnershipTracking: true},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	hostname := &source.Hostname{Name: "app.example.com", Source: "test"}
	actions := r.ensureRecord(context.Background(), hostname, nil)

	if len(actions) != 2 {
		t.Fatalf("expected 2 actions (one per distinct backend), got %d", len(actions))
	}
	if createdInternal != 1 {
		t.Errorf("internal-dns should be called once, got %d", createdInternal)
	}
	if createdExternal != 1 {
		t.Errorf("external-dns should be called once, got %d", createdExternal)
	}
}

// TestEnsureRecord_SameIdentityDifferentRecordType_AllWrite verifies that
// two instances pointing at the same backend but writing different record
// types (e.g. A vs AAAA on the same Cloudflare zone) do NOT collide — they
// own disjoint records, so both must write. The precedence key is
// (Identity, RecordType), not Identity alone.
func TestEnsureRecord_SameIdentityDifferentRecordType_AllWrite(t *testing.T) {
	mockA := newTestMockProvider("dns-a")
	mockA.identity = &provider.ProviderIdentity{Type: "mock", Endpoint: "http://dns", Zone: "example.com"}
	mockAAAA := newTestMockProvider("dns-aaaa")
	mockAAAA.identity = &provider.ProviderIdentity{Type: "mock", Endpoint: "http://dns", Zone: "example.com"}

	logger := quietLogger()
	providers := provider.NewRegistry(logger)

	var createdA, createdAAAA int
	mockA.createFn = func(_ context.Context, r provider.Record) error {
		if r.Type == provider.RecordTypeA {
			createdA++
		}
		return nil
	}
	mockAAAA.createFn = func(_ context.Context, r provider.Record) error {
		if r.Type == provider.RecordTypeAAAA {
			createdAAAA++
		}
		return nil
	}

	providers.RegisterFactory("mock-a", func(cfg provider.FactoryConfig) (provider.Provider, error) { return mockA, nil })
	providers.RegisterFactory("mock-aaaa", func(cfg provider.FactoryConfig) (provider.Provider, error) { return mockAAAA, nil })

	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name: "dns-a", TypeName: "mock-a",
		RecordType: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300,
		Domains: []string{"*.example.com"},
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name: "dns-aaaa", TypeName: "mock-aaaa",
		RecordType: provider.RecordTypeAAAA, Target: "2001:db8::1", TTL: 300,
		Domains: []string{"*.example.com"},
	})

	r := &Reconciler{
		providers:      providers,
		config:         Config{Enabled: true, OwnershipTracking: true},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	actions := r.ensureRecord(context.Background(), &source.Hostname{Name: "app.example.com", Source: "test"}, nil)

	if len(actions) != 2 {
		t.Fatalf("expected 2 actions (different record types are not a collision), got %d", len(actions))
	}
	if createdA != 1 {
		t.Errorf("A record should be written once, got %d", createdA)
	}
	if createdAAAA != 1 {
		t.Errorf("AAAA record should be written once, got %d", createdAAAA)
	}
}

// TestEnsureRecord_SameIdentitySameRecordType_FirstWins is the explicit
// counter-example to TestEnsureRecord_DistinctIdentities_AllWrite: two
// instances pointing at the same backend with the same record type collide,
// and the first by declaration order wins. This is the #86 race condition
// that the per-identity logic still prevents.
func TestEnsureRecord_SameIdentitySameRecordType_FirstWins(t *testing.T) {
	id := provider.ProviderIdentity{Type: "mock", Endpoint: "http://shared.dns", Zone: "example.com"}
	mock1 := newTestMockProvider("first")
	mock1.identity = &id
	mock2 := newTestMockProvider("second")
	mock2.identity = &id

	logger := quietLogger()
	providers := provider.NewRegistry(logger)

	var c1, c2 int
	mock1.createFn = func(_ context.Context, r provider.Record) error {
		if r.Type != provider.RecordTypeTXT {
			c1++
		}
		return nil
	}
	mock2.createFn = func(_ context.Context, r provider.Record) error {
		if r.Type != provider.RecordTypeTXT {
			c2++
		}
		return nil
	}

	providers.RegisterFactory("mock-first", func(cfg provider.FactoryConfig) (provider.Provider, error) { return mock1, nil })
	providers.RegisterFactory("mock-second", func(cfg provider.FactoryConfig) (provider.Provider, error) { return mock2, nil })

	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name: "first", TypeName: "mock-first",
		RecordType: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300,
		Domains: []string{"*.example.com"},
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name: "second", TypeName: "mock-second",
		RecordType: provider.RecordTypeA, Target: "10.0.0.2", TTL: 300,
		Domains: []string{"*.example.com"},
	})

	r := &Reconciler{
		providers:      providers,
		config:         Config{Enabled: true, OwnershipTracking: true},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	actions := r.ensureRecord(context.Background(), &source.Hostname{Name: "app.example.com", Source: "test"}, nil)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action (collision collapsed to first), got %d", len(actions))
	}
	if actions[0].Provider != "first" {
		t.Errorf("expected winner=first, got %q", actions[0].Provider)
	}
	if c1 != 1 || c2 != 0 {
		t.Errorf("expected first=1 second=0, got first=%d second=%d", c1, c2)
	}
}

// =============================================================================
// Operational Mode Tests
// =============================================================================

func TestCleanupOrphans_AdditiveMode_NeverDeletes(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	mock.AddRecord(provider.Record{
		Hostname: "orphan.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	// Add ownership record
	mock.AddRecord(provider.Record{
		Hostname: "_dnsweaver.orphan.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
	})

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	// Create instance with additive mode
	err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
		Mode:       provider.ModeAdditive,
	})
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	cache := newRecordCache(context.Background(), providers, logger)

	r := &Reconciler{
		providers: providers,
		config:    Config{CleanupOrphans: true, OwnershipTracking: true, Enabled: true},
		logger:    logger,
		knownHostnames: map[string]struct{}{
			"orphan.example.com": {}, // Was known before
		},
	}
	r.syncAtomics()

	// No current hostnames - orphan.example.com is orphaned
	currentHostnames := map[string]*source.Hostname{}

	actions := r.cleanupOrphans(context.Background(), currentHostnames, cache)

	// Should skip due to additive mode
	if len(actions) != 1 {
		t.Fatalf("expected 1 action (skip), got %d", len(actions))
	}
	if actions[0].Type != ActionSkip {
		t.Errorf("expected ActionSkip in additive mode, got %v", actions[0].Type)
	}

	// Verify record was NOT deleted
	deleted := mock.GetDeleted()
	if len(deleted) > 0 {
		t.Error("additive mode should NOT delete any records")
	}
}

func TestCleanupOrphans_ManagedMode_DeletesOwnedOnly(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	// Add record WITH ownership
	mock.AddRecord(provider.Record{
		Hostname: "owned.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	mock.AddRecord(provider.Record{
		Hostname: "_dnsweaver.owned.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
	})
	// Add record WITHOUT ownership
	mock.AddRecord(provider.Record{
		Hostname: "unowned.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.2",
	})

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	// Create instance with managed mode (default)
	err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
		Mode:       provider.ModeManaged,
	})
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	cache := newRecordCache(context.Background(), providers, logger)

	r := &Reconciler{
		providers: providers,
		config:    Config{CleanupOrphans: true, OwnershipTracking: true, Enabled: true},
		logger:    logger,
		knownHostnames: map[string]struct{}{
			"owned.example.com":   {},
			"unowned.example.com": {},
			"still1.example.com":  {},
			"still2.example.com":  {},
			"still3.example.com":  {},
		},
	}
	r.syncAtomics()

	// Only owned/unowned are orphaned (2/5 = 40%, below circuit breaker threshold)
	currentHostnames := map[string]*source.Hostname{
		"still1.example.com": {Name: "still1.example.com", Source: "test"},
		"still2.example.com": {Name: "still2.example.com", Source: "test"},
		"still3.example.com": {Name: "still3.example.com", Source: "test"},
	}

	actions := r.cleanupOrphans(context.Background(), currentHostnames, cache)

	// Should have 2 actions: delete owned, skip unowned
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}

	var ownedAction, unownedAction *Action
	for i := range actions {
		if actions[i].Hostname == "owned.example.com" {
			ownedAction = &actions[i]
		}
		if actions[i].Hostname == "unowned.example.com" {
			unownedAction = &actions[i]
		}
	}

	if ownedAction == nil || ownedAction.Type != ActionDelete {
		t.Error("owned record should be deleted in managed mode")
	}
	if unownedAction == nil || unownedAction.Type != ActionSkip {
		t.Error("unowned record should be skipped in managed mode")
	}
}

func TestCleanupOrphans_AuthoritativeMode_DeletesAll(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	// Add record WITH ownership
	mock.AddRecord(provider.Record{
		Hostname: "owned.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	mock.AddRecord(provider.Record{
		Hostname: "_dnsweaver.owned.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
	})
	// Add record WITHOUT ownership
	mock.AddRecord(provider.Record{
		Hostname: "unowned.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.2",
	})

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	// Create instance with authoritative mode
	err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
		Mode:       provider.ModeAuthoritative,
	})
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	cache := newRecordCache(context.Background(), providers, logger)

	r := &Reconciler{
		providers: providers,
		config:    Config{CleanupOrphans: true, OwnershipTracking: true, Enabled: true},
		logger:    logger,
		knownHostnames: map[string]struct{}{
			"owned.example.com":   {},
			"unowned.example.com": {},
			"active1.example.com": {},
			"active2.example.com": {},
			"active3.example.com": {},
		},
	}
	r.syncAtomics()

	// Only owned/unowned are orphaned (2/5 = 40%, below circuit breaker threshold)
	currentHostnames := map[string]*source.Hostname{
		"active1.example.com": {Name: "active1.example.com", Source: "test"},
		"active2.example.com": {Name: "active2.example.com", Source: "test"},
		"active3.example.com": {Name: "active3.example.com", Source: "test"},
	}

	actions := r.cleanupOrphans(context.Background(), currentHostnames, cache)

	// In authoritative mode, both should be deleted
	var deletedOwned, deletedUnowned bool
	for _, action := range actions {
		if action.Hostname == "owned.example.com" && action.Type == ActionDelete {
			deletedOwned = true
		}
		if action.Hostname == "unowned.example.com" && action.Type == ActionDelete {
			deletedUnowned = true
		}
	}

	if !deletedOwned {
		t.Error("owned record should be deleted in authoritative mode")
	}
	if !deletedUnowned {
		t.Error("unowned record should be deleted in authoritative mode (ignores ownership)")
	}
}

func TestEnsureRecord_UsesRecoveredMetadata(t *testing.T) {
	// When source provides no metadata but recovered metadata exists,
	// the reconciler should use recovered metadata as a fallback.
	logger := quietLogger()

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

	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	sources := source.NewRegistry(logger)

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.OwnershipTracking = true

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	// Simulate recovered metadata from ownership TXT
	r.recoveredMetadata = map[string]map[string]string{
		"app.example.com": {"proxied": "true", "custom": "recovered"},
	}

	// Create hostname WITHOUT metadata (simulating source that doesn't provide it)
	hostname := &source.Hostname{
		Name:   "app.example.com",
		Source: "test",
	}

	actions := r.ensureRecord(context.Background(), hostname, nil)

	// Verify record was created
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Status != StatusSuccess {
		t.Fatalf("expected success status, got %s: %s", actions[0].Status, actions[0].Error)
	}

	// Verify metadata was passed to the provider via the created record
	records := mockProvider.records
	var createdRecord provider.Record
	for _, rec := range records {
		if rec.Hostname == "app.example.com" && rec.Type == provider.RecordTypeA {
			createdRecord = rec
			break
		}
	}
	if createdRecord.Hostname == "" {
		t.Fatal("expected A record to be created for app.example.com")
	}
	if createdRecord.Metadata == nil {
		t.Fatal("expected metadata on created record (from recovered metadata)")
	}
	if createdRecord.Metadata["proxied"] != "true" {
		t.Errorf("expected proxied=true, got %q", createdRecord.Metadata["proxied"])
	}
	if createdRecord.Metadata["custom"] != "recovered" {
		t.Errorf("expected custom=recovered, got %q", createdRecord.Metadata["custom"])
	}

	// Verify recovered metadata was consumed
	remaining := r.RecoveredMetadata()
	if len(remaining) != 0 {
		t.Errorf("expected recovered metadata consumed after use, got %v", remaining)
	}
}

func TestEnsureRecord_SourceMetadataTakesPrecedence(t *testing.T) {
	// When source provides metadata, it should take precedence over recovered metadata.
	logger := quietLogger()

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

	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	sources := source.NewRegistry(logger)

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.OwnershipTracking = true

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	// Simulate recovered metadata from ownership TXT (old value)
	r.recoveredMetadata = map[string]map[string]string{
		"app.example.com": {"proxied": "true"},
	}

	// Create hostname WITH metadata (source provides new value)
	hostname := &source.Hostname{
		Name:   "app.example.com",
		Source: "test",
		RecordHints: &source.RecordHints{
			Metadata: map[string]string{"proxied": "false"},
		},
	}

	actions := r.ensureRecord(context.Background(), hostname, nil)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Status != StatusSuccess {
		t.Fatalf("expected success, got %s: %s", actions[0].Status, actions[0].Error)
	}

	// Verify source metadata was used (not recovered)
	var createdRecord provider.Record
	for _, rec := range mockProvider.records {
		if rec.Hostname == "app.example.com" && rec.Type == provider.RecordTypeA {
			createdRecord = rec
			break
		}
	}
	if createdRecord.Metadata == nil {
		t.Fatal("expected metadata on created record")
	}
	if createdRecord.Metadata["proxied"] != "false" {
		t.Errorf("source metadata should win: expected proxied=false, got %q", createdRecord.Metadata["proxied"])
	}
}
