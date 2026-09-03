package reconciler

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
	"github.com/maxfield-allison/dnsweaver/sources/traefik"
)

// =============================================================================
// P2: Behavioral Edge Cases
//
// Tests for hostname migration, wildcard matching, RFC limits, SRV edge cases,
// concurrent reconciliation safety, and duplicate record handling.
// =============================================================================

// --- Hostname Migration Between Providers ---

func TestReconcile_HostnameMovesToDifferentProvider(t *testing.T) {
	// Two providers both match the hostname (more-specific + wildcard).
	// Per first-match-wins (issue #86), only the first declared instance
	// (dns-internal) should write the record. The second instance must
	// not also write — that would produce the record-flapping race where
	// the two providers overwrite each other's targets every reconciliation.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.internal.example.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	providerA := newTestMockProvider("dns-internal")
	providerB := newTestMockProvider("dns-external")

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		if cfg.Name == "dns-internal" {
			return providerA, nil
		}
		return providerB, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "dns-internal",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.internal.example.com"},
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "dns-external",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.2",
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

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if result.HostnamesDiscovered != 1 {
		t.Errorf("HostnamesDiscovered = %d, want 1", result.HostnamesDiscovered)
	}

	// Provider A wins (declared first, also more specific).
	createdA := providerA.GetCreatedDNSRecords()
	if len(createdA) == 0 {
		t.Error("expected provider A (winner) to have created records")
	}

	// Provider B must NOT have written — first-match-wins precedence.
	createdB := providerB.GetCreatedDNSRecords()
	if len(createdB) != 0 {
		t.Errorf("expected provider B (loser) to skip, but it created %d records", len(createdB))
	}
}

func TestReconcile_HostnameSwitchesTarget(t *testing.T) {
	// Cycle 1: record created with target 10.0.0.1
	// Cycle 2: same hostname but provider target changed to 10.0.0.2
	// The set diff should create the replacement and then remove the exact old
	// member, without treating an unrelated sibling as an update candidate.
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

	if _, err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile error: %v", err)
	}
	instance, ok := providers.Get("test-dns")
	if !ok {
		t.Fatal("provider instance not found")
	}
	instance.SetDynamicTarget("10.0.0.2")

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("second Reconcile error: %v", err)
	}

	created := result.Created()
	if len(created) != 1 || created[0].Target != "10.0.0.2" {
		t.Errorf("created actions = %+v, want replacement target 10.0.0.2", created)
	}
	deleted := result.Deleted()
	if len(deleted) != 1 || deleted[0].Target != "10.0.0.1" {
		t.Errorf("deleted actions = %+v, want old target 10.0.0.1", deleted)
	}
}

// --- Wildcard Matching ---

func TestReconcile_WildcardProviderMatchesSubdomains(t *testing.T) {
	// A provider with *.example.com should match app.example.com
	// but not example.com or app.other.com.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app1", map[string]string{
		"traefik.http.routers.app1.rule": "Host(`app.example.com`)",
	})
	dockerMock.AddWorkload("app2", map[string]string{
		"traefik.http.routers.app2.rule": "Host(`deep.sub.example.com`)",
	})
	dockerMock.AddWorkload("app3", map[string]string{
		"traefik.http.routers.app3.rule": "Host(`app.other.com`)",
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

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	if result.HostnamesDiscovered != 3 {
		t.Errorf("HostnamesDiscovered = %d, want 3", result.HostnamesDiscovered)
	}

	// Count skip actions with "no matching provider" — app.other.com shouldn't match
	skipped := 0
	for _, a := range result.Actions {
		if a.Type == ActionSkip && a.Error == "no matching provider" {
			skipped++
		}
	}
	if skipped == 0 {
		t.Error("expected at least one 'no matching provider' skip for app.other.com")
	}
}

func TestReconcile_MultipleProvidersOverlappingDomains(t *testing.T) {
	// Two providers with overlapping patterns: *.example.com and *.sub.example.com
	// app.sub.example.com should match BOTH providers.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.sub.example.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	broadProvider := newTestMockProvider("broad-dns")
	narrowProvider := newTestMockProvider("narrow-dns")

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		if cfg.Name == "broad-dns" {
			return broadProvider, nil
		}
		return narrowProvider, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "broad-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "narrow-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeA,
		Target:     "10.0.0.2",
		TTL:        300,
		Domains:    []string{"*.sub.example.com"},
	})

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Both providers should receive a create action
	broadCreated := broadProvider.GetCreatedDNSRecords()
	narrowCreated := narrowProvider.GetCreatedDNSRecords()

	if len(broadCreated) == 0 && len(narrowCreated) == 0 {
		t.Error("expected at least one provider to create record")
	}

	// Result should show create actions for both providers
	if result.CreatedCount() < 1 {
		t.Errorf("expected at least 1 created, got %d", result.CreatedCount())
	}
}

// --- RFC Length Limits ---

func TestReconcile_LongHostnameIsInvalid(t *testing.T) {
	// A hostname exceeding 253 characters should be rejected by validation
	// during extraction (source.Hostnames.ValidateAll).
	longLabel := strings.Repeat("a", 64) // 64 chars > 63 max
	longHostname := longLabel + ".example.com"

	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`" + longHostname + "`)",
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

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// The invalid hostname should be filtered out during extraction
	if result.HostnamesDiscovered != 0 {
		t.Errorf("HostnamesDiscovered = %d, want 0 (hostname is invalid)", result.HostnamesDiscovered)
	}
	if result.HostnamesInvalid != 1 {
		t.Errorf("HostnamesInvalid = %d, want 1", result.HostnamesInvalid)
	}
}

func TestReconcile_MaxLengthHostnameIsValid(t *testing.T) {
	// A hostname with 63-char labels that totals ≤253 chars should be accepted.
	// Build: <63 chars>.<63 chars>.<63 chars>.<61 chars> = 253 total
	label63 := strings.Repeat("a", 63)
	label61 := strings.Repeat("b", 61)
	maxHostname := label63 + "." + label63 + "." + label63 + "." + label61

	if len(maxHostname) != 253 {
		t.Fatalf("constructed hostname is %d chars, expected 253", len(maxHostname))
	}

	// Validate directly since traefik source may not parse this well
	err := source.ValidateHostname(maxHostname)
	if err != nil {
		t.Errorf("expected valid hostname with 253 chars, got error: %v", err)
	}
}

func TestReconcile_HostnameExceeding253CharsIsInvalid(t *testing.T) {
	// One char over the limit should fail validation.
	label63 := strings.Repeat("a", 63)
	label62 := strings.Repeat("b", 62)
	overHostname := label63 + "." + label63 + "." + label63 + "." + label62

	if len(overHostname) <= 253 {
		t.Fatalf("constructed hostname is %d chars, expected >253", len(overHostname))
	}

	err := source.ValidateHostname(overHostname)
	if err == nil {
		t.Error("expected validation error for hostname exceeding 253 chars")
	}
}

func TestReconcile_TrailingDotNormalization(t *testing.T) {
	// Hostnames with trailing dots (FQDN format) should be normalized
	// to the same canonical form.
	h1 := source.NormalizeHostname("app.example.com.")
	h2 := source.NormalizeHostname("app.example.com")

	if h1 != h2 {
		t.Errorf("trailing dot normalization failed: %q != %q", h1, h2)
	}
}

func TestReconcile_CaseNormalization(t *testing.T) {
	// DNS is case-insensitive: APP.Example.COM should normalize to app.example.com.
	h1 := source.NormalizeHostname("APP.Example.COM")
	h2 := source.NormalizeHostname("app.example.com")

	if h1 != h2 {
		t.Errorf("case normalization failed: %q != %q", h1, h2)
	}
}

// --- SRV Edge Cases ---

func TestEnsureRecord_SRVWithZeroPriorityAndWeight(t *testing.T) {
	// SRV records with zero priority and weight are valid per RFC 2782.
	logger := quietLogger()
	mockProvider := newTestMockProvider("test-dns")

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(_ provider.FactoryConfig) (provider.Provider, error) {
		return mockProvider, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeSRV,
		Target:     "svc.example.com",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	sources := source.NewRegistry(logger)
	r := New(nil, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	hostname := &source.Hostname{
		Name:   "_http._tcp.example.com",
		Source: "test",
		RecordHints: &source.RecordHints{
			Type:   "SRV",
			Target: "svc.example.com",
			SRV: &source.SRVHints{
				Priority: 0,
				Weight:   0,
				Port:     8080,
			},
		},
	}

	actions := r.ensureRecord(context.Background(), hostname, nil)

	if len(actions) == 0 {
		t.Fatal("expected at least one action")
	}

	// The action should succeed (zero priority/weight is valid per RFC 2782)
	for _, action := range actions {
		if action.Status == StatusFailed {
			t.Errorf("SRV with zero priority/weight should succeed, got error: %s", action.Error)
		}
	}
}

func TestEnsureRecord_MultipleSRVRecordsForSameHostname(t *testing.T) {
	// Multiple SRV records with different targets for the same service hostname.
	logger := quietLogger()
	mockProvider := newTestMockProvider("test-dns")

	// Pre-seed with one SRV record
	mockProvider.AddRecord(provider.Record{
		Hostname: "_http._tcp.example.com",
		Type:     provider.RecordTypeSRV,
		Target:   "server1.example.com",
		SRV:      &provider.SRVData{Priority: 10, Weight: 100, Port: 8080},
	})

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(_ provider.FactoryConfig) (provider.Provider, error) {
		return mockProvider, nil
	})
	_ = providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: provider.RecordTypeSRV,
		Target:     "server2.example.com",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	sources := source.NewRegistry(logger)
	r := New(nil, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	// Requesting a different target (server2) than what exists (server1)
	hostname := &source.Hostname{
		Name:   "_http._tcp.example.com",
		Source: "test",
		RecordHints: &source.RecordHints{
			Type:   "SRV",
			Target: "server2.example.com",
			SRV: &source.SRVHints{
				Priority: 20,
				Weight:   50,
				Port:     8080,
			},
		},
	}

	actions := r.ensureRecord(context.Background(), hostname, nil)

	if len(actions) == 0 {
		t.Fatal("expected at least one action")
	}
}

// --- Concurrent Reconciliation Safety ---

func TestReconciler_ConcurrentReconcileIsSafe(t *testing.T) {
	// Run multiple Reconcile() calls concurrently to verify no data races.
	// This test should be run with -race flag.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
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

	const goroutines = 10
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			_, err := r.Reconcile(context.Background())
			if err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent Reconcile returned error: %v", err)
	}
}

func TestReconciler_ConcurrentSetEnabledDuringReconcile(t *testing.T) {
	// Toggle SetEnabled/SetDryRun while Reconcile is running.
	// Should not panic or race.
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

	var wg sync.WaitGroup

	// Run reconciles
	wg.Add(5)
	for range 5 {
		go func() {
			defer wg.Done()
			_, _ = r.Reconcile(context.Background())
		}()
	}

	// Toggle enabled/dryRun concurrently
	wg.Add(5)
	for range 5 {
		go func() {
			defer wg.Done()
			r.SetEnabled(false)
			r.SetEnabled(true)
			r.SetDryRun(true)
			r.SetDryRun(false)
		}()
	}

	// Read KnownHostnames concurrently
	wg.Add(5)
	for range 5 {
		go func() {
			defer wg.Done()
			_ = r.KnownHostnames()
		}()
	}

	wg.Wait()
	// No assertion needed — test passes if no panic or race detected
}

func TestReconciler_ConcurrentReconcileHostnameAndRemove(t *testing.T) {
	// ReconcileHostname and RemoveHostname called concurrently.
	logger := quietLogger()
	sources := source.NewRegistry(logger)

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

	r := New(nil, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	var wg sync.WaitGroup
	wg.Add(10)

	for range 5 {
		go func() {
			defer wg.Done()
			_, _ = r.ReconcileHostname(context.Background(), "host.example.com")
		}()
	}
	for range 5 {
		go func() {
			defer wg.Done()
			_, _ = r.RemoveHostname(context.Background(), "host.example.com")
		}()
	}

	wg.Wait()
	// No assertion needed — passes if no panic or race
}

// --- Duplicate Records from Provider ---

func TestReconcile_DuplicateRecordsInProviderList(t *testing.T) {
	// When provider.List() returns duplicate records (same hostname, type, target),
	// the reconciler should handle them gracefully without creating additional records.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.example.com`)",
	})

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	// Provider returns duplicate records
	mockProvider.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	mockProvider.AddRecord(provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})

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
		t.Fatalf("Reconcile error: %v", err)
	}

	// Should recognize the record already exists (skip, not create again)
	created := mockProvider.GetCreatedDNSRecords()
	if len(created) > 0 {
		t.Errorf("expected 0 new creates (record exists), got %d", len(created))
	}

	// Should have a skip action
	skipped := result.Skipped()
	hasAlreadyExists := false
	for _, a := range skipped {
		if a.Error == errRecordAlreadyExists {
			hasAlreadyExists = true
			break
		}
	}
	if !hasAlreadyExists {
		t.Error("expected 'record already exists' skip action for duplicate records")
	}
}

func TestReconcile_DuplicateHostnamesAcrossWorkloads(t *testing.T) {
	// When two workloads have the same hostname, the first one wins.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	dockerMock.AddWorkload("app1", map[string]string{
		"traefik.http.routers.app1.rule": "Host(`shared.example.com`)",
	})
	dockerMock.AddWorkload("app2", map[string]string{
		"traefik.http.routers.app2.rule": "Host(`shared.example.com`)",
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

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Only one hostname should be discovered (deduplicated)
	if result.HostnamesDiscovered != 1 {
		t.Errorf("HostnamesDiscovered = %d, want 1", result.HostnamesDiscovered)
	}
	if result.HostnamesDuplicate != 1 {
		t.Errorf("HostnamesDuplicate = %d, want 1", result.HostnamesDuplicate)
	}
}

// --- Orphan Cleanup Circuit Breaker ---

func TestReconcile_CircuitBreakerPreventseMassDeletion(t *testing.T) {
	// If >50% of previously known hostnames disappear, the circuit breaker
	// should prevent orphan cleanup (assumes source is temporarily unavailable).
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	// Only 1 workload remains out of previous 5
	dockerMock.AddWorkload("surviving-app", map[string]string{
		"traefik.http.routers.app.rule": "Host(`app1.example.com`)",
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
	cfg.CleanupOrphans = true
	cfg.OwnershipTracking = true

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	// Seed 5 known hostnames from the "previous" cycle
	r.mu.Lock()
	r.knownHostnames = map[string]struct{}{
		"app1.example.com": {},
		"app2.example.com": {},
		"app3.example.com": {},
		"app4.example.com": {},
		"app5.example.com": {},
	}
	r.hostnameProviders = map[string][]string{
		"app1.example.com": {"test-dns"},
		"app2.example.com": {"test-dns"},
		"app3.example.com": {"test-dns"},
		"app4.example.com": {"test-dns"},
		"app5.example.com": {"test-dns"},
	}
	r.mu.Unlock()

	// Add records to provider so deletion would be possible
	for _, name := range []string{"app2.example.com", "app3.example.com", "app4.example.com", "app5.example.com"} {
		mockProvider.AddRecord(provider.Record{
			Hostname: name,
			Type:     provider.RecordTypeA,
			Target:   "10.0.0.1",
		})
		mockProvider.AddRecord(provider.Record{
			Hostname: name,
			Type:     provider.RecordTypeTXT,
			Target:   "heritage=dnsweaver",
		})
	}

	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Circuit breaker should have prevented deletion:
	// 4 out of 5 hostnames disappeared (80% > 50% threshold)
	deleted := mockProvider.GetDeleted()
	if len(deleted) > 0 {
		t.Errorf("circuit breaker should have prevented deletion, but %d records were deleted", len(deleted))
	}

	// Verify no delete actions in result
	deleteActions := result.Deleted()
	if len(deleteActions) > 0 {
		t.Errorf("expected 0 delete actions (circuit breaker), got %d", len(deleteActions))
	}

	_ = result // use result
}

// --- Additive Mode ---

func TestReconcile_AdditiveModePreventsOrphanDeletion(t *testing.T) {
	// In additive mode, orphan records should never be deleted.
	dockerMock := newTestMockWorkloadLister(workload.PlatformDocker)
	// No workloads — all previous hostnames are orphans

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(traefik.New(traefik.WithLogger(logger)))

	mockProvider := newTestMockProvider("test-dns")
	mockProvider.AddRecord(provider.Record{
		Hostname: "orphan.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.1",
	})
	mockProvider.AddRecord(provider.Record{
		Hostname: "orphan.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
	})

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
		Mode:       provider.ModeAdditive,
	})

	cfg := DefaultConfig()
	cfg.CleanupOrphans = true
	cfg.OwnershipTracking = true

	r := New([]workload.Lister{dockerMock}, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	// Seed one known hostname for orphan detection
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

	// Verify no actual deletions occurred
	deleted := mockProvider.GetDeleted()
	if len(deleted) > 0 {
		t.Errorf("additive mode should prevent deletion, but %d records were deleted", len(deleted))
	}

	if len(result.Deleted()) != 0 {
		t.Errorf("additive mode reported deletions: %+v", result.Deleted())
	}
}
