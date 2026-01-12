package reconciler

import (
	"context"
	"log/slog"
	"os"
	"sync"

	"gitlab.bluewillows.net/root/dnsweaver/internal/docker"
	"gitlab.bluewillows.net/root/dnsweaver/internal/matcher"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/source"
)

// =============================================================================
// Mock WorkloadLister for Reconcile() tests
// =============================================================================

// testMockWorkloadLister implements WorkloadLister for testing.
type testMockWorkloadLister struct {
	mode      docker.Mode
	workloads []docker.Workload
	listErr   error
}

func newTestMockWorkloadLister(mode docker.Mode) *testMockWorkloadLister {
	return &testMockWorkloadLister{
		mode:      mode,
		workloads: make([]docker.Workload, 0),
	}
}

func (m *testMockWorkloadLister) ListWorkloads(_ context.Context) ([]docker.Workload, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.workloads, nil
}

func (m *testMockWorkloadLister) Mode() docker.Mode {
	return m.mode
}

func (m *testMockWorkloadLister) AddWorkload(name string, labels map[string]string) {
	m.workloads = append(m.workloads, docker.Workload{
		ID:     "id-" + name,
		Name:   name,
		Labels: labels,
		Type:   docker.WorkloadTypeService,
	})
}

func (m *testMockWorkloadLister) SetListError(err error) {
	m.listErr = err
}

// testMockProvider implements provider.Provider for testing.
// It tracks all Create/Delete calls for verification.
type testMockProvider struct {
	name     string
	typeName string

	mu       sync.Mutex
	records  []provider.Record
	created  []provider.Record
	deleted  []provider.Record
	pingErr  error
	listErr  error
	createFn func(ctx context.Context, r provider.Record) error
	deleteFn func(ctx context.Context, r provider.Record) error
}

func newTestMockProvider(name string) *testMockProvider {
	return &testMockProvider{
		name:     name,
		typeName: "mock",
		records:  make([]provider.Record, 0),
		created:  make([]provider.Record, 0),
		deleted:  make([]provider.Record, 0),
	}
}

func (m *testMockProvider) Name() string { return m.name }
func (m *testMockProvider) Type() string { return m.typeName }

func (m *testMockProvider) Ping(_ context.Context) error {
	return m.pingErr
}

func (m *testMockProvider) List(_ context.Context) ([]provider.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	// Return a copy
	result := make([]provider.Record, len(m.records))
	copy(result, m.records)
	return result, nil
}

func (m *testMockProvider) Create(ctx context.Context, r provider.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for custom function
	if m.createFn != nil {
		if err := m.createFn(ctx, r); err != nil {
			return err
		}
	}

	m.created = append(m.created, r)
	m.records = append(m.records, r)
	return nil
}

func (m *testMockProvider) Delete(ctx context.Context, r provider.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for custom function
	if m.deleteFn != nil {
		if err := m.deleteFn(ctx, r); err != nil {
			return err
		}
	}

	m.deleted = append(m.deleted, r)
	// Remove from records
	newRecords := make([]provider.Record, 0, len(m.records))
	for _, rec := range m.records {
		if rec.Hostname != r.Hostname || rec.Type != r.Type || rec.Target != r.Target {
			newRecords = append(newRecords, rec)
		}
	}
	m.records = newRecords
	return nil
}

// Helper methods for tests
func (m *testMockProvider) AddRecord(r provider.Record) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, r)
}

func (m *testMockProvider) GetCreated() []provider.Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]provider.Record, len(m.created))
	copy(result, m.created)
	return result
}

func (m *testMockProvider) GetDeleted() []provider.Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]provider.Record, len(m.deleted))
	copy(result, m.deleted)
	return result
}

func (m *testMockProvider) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = nil
	m.created = nil
	m.deleted = nil
}

// GetCreatedDNSRecords returns only DNS records (A, AAAA, CNAME, SRV), excluding TXT ownership records.
func (m *testMockProvider) GetCreatedDNSRecords() []provider.Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []provider.Record
	for _, r := range m.created {
		if r.Type != provider.RecordTypeTXT {
			result = append(result, r)
		}
	}
	return result
}

// GetCreatedOwnershipRecords returns only TXT ownership records.
func (m *testMockProvider) GetCreatedOwnershipRecords() []provider.Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []provider.Record
	for _, r := range m.created {
		if r.Type == provider.RecordTypeTXT {
			result = append(result, r)
		}
	}
	return result
}

// =============================================================================
// Reserved Test Utilities
// These helpers are prepared for future test expansion (e.g., full Reconcile()
// function tests, source registry tests). Currently unused but intentionally
// kept for consistency and future use.
// =============================================================================

//nolint:unused // Reserved for future Reconcile() function tests
type testMockSource struct {
	name      string
	hostnames []source.Hostname
}

//nolint:unused // Reserved for future Reconcile() function tests
func newTestMockSource(name string, hostnames ...source.Hostname) *testMockSource {
	return &testMockSource{
		name:      name,
		hostnames: hostnames,
	}
}

//nolint:unused // Reserved for future Reconcile() function tests
func (m *testMockSource) Name() string { return m.name }

//nolint:unused // Reserved for future Reconcile() function tests
func (m *testMockSource) Extract(_ context.Context, _ map[string]string) ([]source.Hostname, error) {
	return m.hostnames, nil
}

//nolint:unused // Reserved for future Reconcile() function tests
func (m *testMockSource) Discover(_ context.Context) ([]source.Hostname, error) {
	return nil, nil
}

//nolint:unused // Reserved for future Reconcile() function tests
func (m *testMockSource) SupportsDiscovery() bool {
	return false
}

// testProviderRegistry creates a test provider registry with mock provider(s).
//
//nolint:unused // Reserved for future Reconcile() function tests
func testProviderRegistry(logger *slog.Logger, mocks ...*testMockProvider) *provider.Registry {
	reg := provider.NewRegistry(logger)

	// Register a factory for the mock type
	reg.RegisterFactory("mock", func(name string, _ map[string]string) (provider.Provider, error) {
		// Find the mock with this name
		for _, m := range mocks {
			if m.name == name {
				return m, nil
			}
		}
		// Return a new mock if not found
		return newTestMockProvider(name), nil
	})

	return reg
}

// testProviderInstance creates a ProviderInstance wrapping a mock provider.
//
//nolint:unused // Reserved for future Reconcile() function tests
func testProviderInstance(mock *testMockProvider, domains []string, recordType provider.RecordType, target string) *provider.ProviderInstance {
	matcherCfg := matcher.DomainMatcherConfig{
		Includes: domains,
		Excludes: nil,
		UseRegex: false,
	}
	domainMatcher, _ := matcher.NewDomainMatcher(matcherCfg)

	return &provider.ProviderInstance{
		Provider:   mock,
		Matcher:    domainMatcher,
		RecordType: recordType,
		Target:     target,
		TTL:        300,
	}
}

// testSourceRegistry creates a test source registry with mock source(s).
//
//nolint:unused // Reserved for future Reconcile() function tests
func testSourceRegistry(logger *slog.Logger, sources ...*testMockSource) *source.Registry {
	reg := source.NewRegistry(logger)
	for _, s := range sources {
		reg.Register(s)
	}
	return reg
}

// quietLogger returns a logger that discards all output.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// testLogger returns a logger suitable for tests.
//
//nolint:unused // Reserved for future debug-level test logging
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// hostnamePtr creates a source.Hostname and returns its pointer.
//
//nolint:unused // Reserved for future pointer-based hostname tests
func hostnamePtr(name, src string) *source.Hostname {
	return &source.Hostname{Name: name, Source: src}
}
