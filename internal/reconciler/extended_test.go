package reconciler

import (
	"context"
	"testing"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/source"
)

// =============================================================================
// srvDataEquals Tests
// =============================================================================

func TestSrvDataEquals(t *testing.T) {
	tests := []struct {
		name string
		a    *provider.SRVData
		b    *provider.SRVData
		want bool
	}{
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "first nil",
			a:    nil,
			b:    &provider.SRVData{Priority: 1, Weight: 1, Port: 25565},
			want: false,
		},
		{
			name: "second nil",
			a:    &provider.SRVData{Priority: 1, Weight: 1, Port: 25565},
			b:    nil,
			want: false,
		},
		{
			name: "equal",
			a:    &provider.SRVData{Priority: 10, Weight: 5, Port: 25565},
			b:    &provider.SRVData{Priority: 10, Weight: 5, Port: 25565},
			want: true,
		},
		{
			name: "different priority",
			a:    &provider.SRVData{Priority: 10, Weight: 5, Port: 25565},
			b:    &provider.SRVData{Priority: 20, Weight: 5, Port: 25565},
			want: false,
		},
		{
			name: "different weight",
			a:    &provider.SRVData{Priority: 10, Weight: 5, Port: 25565},
			b:    &provider.SRVData{Priority: 10, Weight: 10, Port: 25565},
			want: false,
		},
		{
			name: "different port",
			a:    &provider.SRVData{Priority: 10, Weight: 5, Port: 25565},
			b:    &provider.SRVData{Priority: 10, Weight: 5, Port: 25566},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := srvDataEquals(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("srvDataEquals() = %v, want %v", got, tc.want)
			}
		})
	}
}

// =============================================================================
// ReconcileHostname Tests
// =============================================================================

func TestReconcileHostname_CreatesRecord(t *testing.T) {
	mock := newTestMockProvider("test-dns")

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(name string, _ map[string]string) (provider.Provider, error) {
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

	result, err := r.ReconcileHostname(context.Background(), "app.example.com")

	if err != nil {
		t.Fatalf("ReconcileHostname failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.HostnamesDiscovered != 1 {
		t.Errorf("expected 1 hostname discovered, got %d", result.HostnamesDiscovered)
	}
	if len(result.Created()) == 0 {
		t.Error("expected at least one created action")
	}

	// Verify hostname is now tracked
	known := r.KnownHostnames()
	found := false
	for _, h := range known {
		if h == "app.example.com" {
			found = true
			break
		}
	}
	if !found {
		t.Error("hostname should be tracked after ReconcileHostname")
	}
}

func TestReconcileHostname_SkipsNoMatch(t *testing.T) {
	mock := newTestMockProvider("test-dns")

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(name string, _ map[string]string) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.internal.local"}, // Doesn't match example.com
	})

	r := &Reconciler{
		providers:      providers,
		config:         DefaultConfig(),
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}

	result, err := r.ReconcileHostname(context.Background(), "app.example.com")

	if err != nil {
		t.Fatalf("ReconcileHostname failed: %v", err)
	}
	if len(result.Skipped()) == 0 {
		t.Error("expected skipped action for non-matching hostname")
	}
}

// =============================================================================
// RemoveHostname Tests
// =============================================================================

func TestRemoveHostname_DeletesRecord(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	mock.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(name string, _ map[string]string) (provider.Provider, error) {
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
		config:         Config{Enabled: true, OwnershipTracking: false},
		logger:         logger,
		knownHostnames: map[string]struct{}{"app.example.com": {}},
	}

	result, err := r.RemoveHostname(context.Background(), "app.example.com")

	if err != nil {
		t.Fatalf("RemoveHostname failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Deleted()) == 0 {
		t.Error("expected at least one deleted action")
	}

	// Verify hostname is no longer tracked
	known := r.KnownHostnames()
	for _, h := range known {
		if h == "app.example.com" {
			t.Error("hostname should be removed from tracking after RemoveHostname")
		}
	}
}

func TestRemoveHostname_NoMatchingProvider(t *testing.T) {
	mock := newTestMockProvider("test-dns")

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(name string, _ map[string]string) (provider.Provider, error) {
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
		config:         Config{Enabled: true},
		logger:         logger,
		knownHostnames: map[string]struct{}{"app.example.com": {}},
	}

	result, err := r.RemoveHostname(context.Background(), "app.example.com")

	if err != nil {
		t.Fatalf("RemoveHostname failed: %v", err)
	}
	// No matching provider, so no actions
	if len(result.Deleted()) != 0 {
		t.Errorf("expected 0 deleted actions, got %d", len(result.Deleted()))
	}
}

// =============================================================================
// deleteRecordFromCache Tests
// =============================================================================

func TestDeleteRecordFromCache_DeletesAllTypes(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	// Add multiple record types
	mock.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	mock.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeAAAA,
		Target:   "2001:db8::1",
	})
	// Also add a TXT ownership record (should be skipped)
	mock.AddRecord(provider.Record{
		Hostname: "_dnsweaver.app.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
	})

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(name string, _ map[string]string) (provider.Provider, error) {
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
		config:         Config{Enabled: true, OwnershipTracking: false},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}

	actions := r.deleteRecordFromCache(context.Background(), "app.example.com", cache)

	// Should delete A and AAAA records
	if len(actions) != 2 {
		t.Errorf("expected 2 delete actions, got %d", len(actions))
	}

	// Verify both record types were deleted
	var deletedA, deletedAAAA bool
	for _, a := range actions {
		if a.RecordType == "A" && a.Status == StatusSuccess {
			deletedA = true
		}
		if a.RecordType == "AAAA" && a.Status == StatusSuccess {
			deletedAAAA = true
		}
	}
	if !deletedA {
		t.Error("expected A record to be deleted")
	}
	if !deletedAAAA {
		t.Error("expected AAAA record to be deleted")
	}
}

func TestDeleteRecordFromCache_DryRun(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	mock.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(name string, _ map[string]string) (provider.Provider, error) {
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
		config:         Config{Enabled: true, DryRun: true},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}

	actions := r.deleteRecordFromCache(context.Background(), "app.example.com", cache)

	if len(actions) != 1 {
		t.Errorf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionDelete {
		t.Errorf("expected ActionDelete, got %v", actions[0].Type)
	}

	// Verify nothing was actually deleted
	if len(mock.GetDeleted()) != 0 {
		t.Error("dry-run should not delete records")
	}
}

// =============================================================================
// deleteRecordWithOwnershipCheck Tests
// =============================================================================

func TestDeleteRecordWithOwnershipCheck_DeletesOwnedRecords(t *testing.T) {
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
	providers.RegisterFactory("mock", func(name string, _ map[string]string) (provider.Provider, error) {
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
		config:         Config{Enabled: true, OwnershipTracking: true},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}

	actions := r.deleteRecordWithOwnershipCheck(context.Background(), "app.example.com", cache)

	// Should have deleted the A record
	var foundDelete bool
	for _, a := range actions {
		if a.Type == ActionDelete && a.Status == StatusSuccess {
			foundDelete = true
		}
	}
	if !foundDelete {
		t.Error("expected delete action for owned record")
	}
}

func TestDeleteRecordWithOwnershipCheck_SkipsUnownedRecords(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	mock.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	// NO ownership record

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(name string, _ map[string]string) (provider.Provider, error) {
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
		config:         Config{Enabled: true, OwnershipTracking: true},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}

	actions := r.deleteRecordWithOwnershipCheck(context.Background(), "app.example.com", cache)

	// Should skip because no ownership
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionSkip {
		t.Errorf("expected ActionSkip, got %v", actions[0].Type)
	}

	// Verify nothing was deleted
	if len(mock.GetDeleted()) != 0 {
		t.Error("unowned records should not be deleted")
	}
}

func TestDeleteRecordWithOwnershipCheck_DryRun(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	mock.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	mock.AddRecord(provider.Record{
		Hostname: "_dnsweaver.app.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
	})

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(name string, _ map[string]string) (provider.Provider, error) {
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
		config:         Config{Enabled: true, DryRun: true, OwnershipTracking: true},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}

	actions := r.deleteRecordWithOwnershipCheck(context.Background(), "app.example.com", cache)

	// Should have a delete action
	if len(actions) != 1 {
		t.Errorf("expected 1 action, got %d", len(actions))
	}

	// Verify nothing was actually deleted
	if len(mock.GetDeleted()) != 0 {
		t.Error("dry-run should not delete records")
	}
}

// =============================================================================
// SRV Record Tests
// =============================================================================

func TestEnsureRecord_SRVRecord(t *testing.T) {
	mock := newTestMockProvider("test-dns")

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(name string, _ map[string]string) (provider.Provider, error) {
		return mock, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA, // Default is A
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	r := &Reconciler{
		providers:      providers,
		config:         Config{Enabled: true, OwnershipTracking: false},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}

	// Use RecordHints to specify SRV record
	hostname := &source.Hostname{
		Name:   "_minecraft._tcp.mc.example.com",
		Source: "test",
		RecordHints: &source.RecordHints{
			Type:   "SRV",
			Target: "mc.example.com",
			TTL:    300,
			SRV: &source.SRVHints{
				Priority: 10,
				Weight:   5,
				Port:     25565,
			},
		},
	}
	actions := r.ensureRecord(context.Background(), hostname, nil)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Status != StatusSuccess {
		t.Errorf("expected success, got %v with error: %s", actions[0].Status, actions[0].Error)
	}

	// Verify SRV record was created
	created := mock.GetCreated()
	var foundSRV bool
	for _, c := range created {
		if c.Type == provider.RecordTypeSRV {
			foundSRV = true
			if c.SRV == nil {
				t.Error("SRV record should have SRV data")
			} else {
				if c.SRV.Priority != 10 {
					t.Errorf("expected priority 10, got %d", c.SRV.Priority)
				}
				if c.SRV.Port != 25565 {
					t.Errorf("expected port 25565, got %d", c.SRV.Port)
				}
			}
		}
	}
	if !foundSRV {
		t.Error("expected SRV record to be created")
	}
}

// TestEnsureRecord_SRVRecordSkipsMatchingExisting verifies that when an SRV record
// with matching hostname, target, and SRV data (priority, weight, port) already exists,
// the reconciler returns ActionSkip instead of creating a duplicate.
// This was fixed in issue #75.
func TestEnsureRecord_SRVRecordSkipsMatchingExisting(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	// Pre-populate with an existing SRV record
	mock.AddRecord(provider.Record{
		Hostname: "_minecraft._tcp.mc.example.com",
		Type:     provider.RecordTypeSRV,
		Target:   "mc.example.com",
		TTL:      300,
		SRV: &provider.SRVData{
			Priority: 10,
			Weight:   5,
			Port:     25565,
		},
	})

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(name string, _ map[string]string) (provider.Provider, error) {
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
		config:         Config{Enabled: true, OwnershipTracking: false},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}

	// Build cache from the mock provider's existing records
	cache := newRecordCache(context.Background(), providers, logger)

	// Request the same SRV record that already exists
	hostname := &source.Hostname{
		Name:   "_minecraft._tcp.mc.example.com",
		Source: "test",
		RecordHints: &source.RecordHints{
			Type:   "SRV",
			Target: "mc.example.com",
			TTL:    300,
			SRV: &source.SRVHints{
				Priority: 10,
				Weight:   5,
				Port:     25565,
			},
		},
	}
	actions := r.ensureRecord(context.Background(), hostname, cache)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}

	action := actions[0]
	if action.Type != ActionSkip {
		t.Errorf("expected ActionSkip, got %v", action.Type)
	}
	if action.Status != StatusSkipped {
		t.Errorf("expected StatusSkipped, got %v", action.Status)
	}

	// Verify no new records were created
	created := mock.GetCreated()
	if len(created) != 0 {
		t.Errorf("expected no records created, got %d: %+v", len(created), created)
	}
}

// TestEnsureRecord_SRVRecordCreatesWhenDifferentData verifies that an SRV record
// is updated when the existing SRV has different priority/weight/port.
func TestEnsureRecord_SRVRecordCreatesWhenDifferentData(t *testing.T) {
	mock := newTestMockProvider("test-dns")
	// Pre-populate with an existing SRV record with DIFFERENT port
	mock.AddRecord(provider.Record{
		Hostname: "_minecraft._tcp.mc.example.com",
		Type:     provider.RecordTypeSRV,
		Target:   "mc.example.com",
		TTL:      300,
		SRV: &provider.SRVData{
			Priority: 10,
			Weight:   5,
			Port:     25566, // Different port
		},
	})

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(name string, _ map[string]string) (provider.Provider, error) {
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
		config:         Config{Enabled: true, OwnershipTracking: false},
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}

	// Build cache from the mock provider's existing records
	cache := newRecordCache(context.Background(), providers, logger)

	// Request an SRV record with different port
	hostname := &source.Hostname{
		Name:   "_minecraft._tcp.mc.example.com",
		Source: "test",
		RecordHints: &source.RecordHints{
			Type:   "SRV",
			Target: "mc.example.com",
			TTL:    300,
			SRV: &source.SRVHints{
				Priority: 10,
				Weight:   5,
				Port:     25565, // Different from existing (25566)
			},
		},
	}
	actions := r.ensureRecord(context.Background(), hostname, cache)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}

	action := actions[0]
	// When SRV data differs, the reconciler detects the change and performs
	// an update (delete old + create new). The action type is ActionUpdate.
	if action.Type != ActionUpdate {
		t.Errorf("expected ActionUpdate (SRV data changed), got %v", action.Type)
	}
	if action.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess, got %v with error: %s", action.Status, action.Error)
	}

	// Verify the old record was deleted and new one created
	deleted := mock.GetDeleted()
	var foundDeletedSRV bool
	for _, d := range deleted {
		if d.Type == provider.RecordTypeSRV && d.SRV != nil && d.SRV.Port == 25566 {
			foundDeletedSRV = true
		}
	}
	if !foundDeletedSRV {
		t.Error("expected old SRV record with port 25566 to be deleted")
	}

	created := mock.GetCreated()
	var foundCreatedSRV bool
	for _, c := range created {
		if c.Type == provider.RecordTypeSRV && c.SRV != nil && c.SRV.Port == 25565 {
			foundCreatedSRV = true
		}
	}
	if !foundCreatedSRV {
		t.Error("expected new SRV record with port 25565 to be created")
	}
}
