package provider

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// managerTestProvider implements Provider for manager tests.
type managerTestProvider struct {
	name         string
	typeName     string
	pingErr      error
	pingCount    atomic.Int32
	capabilities Capabilities
}

func (m *managerTestProvider) Name() string                           { return m.name }
func (m *managerTestProvider) Type() string                           { return m.typeName }
func (m *managerTestProvider) Capabilities() Capabilities             { return m.capabilities }
func (m *managerTestProvider) List(context.Context) ([]Record, error) { return nil, nil }
func (m *managerTestProvider) Create(context.Context, Record) error   { return nil }
func (m *managerTestProvider) Delete(context.Context, Record) error   { return nil }
func (m *managerTestProvider) Ping(ctx context.Context) error {
	m.pingCount.Add(1)
	return m.pingErr
}

// dynamicPingProvider allows dynamic ping behavior for testing recovery scenarios.
type dynamicPingProvider struct {
	name         string
	typeName     string
	pingAttempts *atomic.Int32
	failUntil    int32 // Fail ping until this many attempts
	capabilities Capabilities
}

func (d *dynamicPingProvider) Name() string                           { return d.name }
func (d *dynamicPingProvider) Type() string                           { return d.typeName }
func (d *dynamicPingProvider) Capabilities() Capabilities             { return d.capabilities }
func (d *dynamicPingProvider) List(context.Context) ([]Record, error) { return nil, nil }
func (d *dynamicPingProvider) Create(context.Context, Record) error   { return nil }
func (d *dynamicPingProvider) Delete(context.Context, Record) error   { return nil }
func (d *dynamicPingProvider) Ping(ctx context.Context) error {
	count := d.pingAttempts.Add(1)
	if count <= d.failUntil {
		return errors.New("connection refused")
	}
	return nil
}

// failingFactory creates a factory that fails N times before succeeding.
func failingFactory(failCount int, finalProvider Provider) Factory {
	var mu sync.Mutex
	attempts := 0
	return func(cfg FactoryConfig) (Provider, error) {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts <= failCount {
			return nil, errors.New("connection refused")
		}
		return finalProvider, nil
	}
}

// alwaysFailFactory creates a factory that always fails.
func alwaysFailFactory() Factory {
	return func(cfg FactoryConfig) (Provider, error) {
		return nil, errors.New("connection refused")
	}
}

// successFactory creates a factory that always succeeds.
func successFactory(p Provider) Factory {
	return func(cfg FactoryConfig) (Provider, error) {
		return p, nil
	}
}

func TestManager_InitializeProvider_Success(t *testing.T) {
	logger := slog.Default()
	registry := NewRegistry(logger)

	mp := &managerTestProvider{name: "test-provider", typeName: "mock"}
	registry.RegisterFactory("mock", successFactory(mp))

	manager := NewManager(registry, WithManagerLogger(logger))

	cfg := ProviderInstanceConfig{
		Name:       "test-provider",
		TypeName:   "mock",
		RecordType: RecordTypeA,
		Target:     "192.0.2.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	}

	err := manager.InitializeProvider(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if manager.ReadyCount() != 1 {
		t.Errorf("expected 1 ready provider, got %d", manager.ReadyCount())
	}
	if manager.PendingCount() != 0 {
		t.Errorf("expected 0 pending providers, got %d", manager.PendingCount())
	}
	if !manager.IsFullyReady() {
		t.Error("expected manager to be fully ready")
	}
}

func TestManager_InitializeProvider_FailedConnectionQueuesForRetry(t *testing.T) {
	logger := slog.Default()
	registry := NewRegistry(logger)

	registry.RegisterFactory("mock", alwaysFailFactory())

	manager := NewManager(registry,
		WithManagerLogger(logger),
		WithManagerConfig(ManagerConfig{
			InitialRetryInterval:   100 * time.Millisecond,
			MaxRetryInterval:       1 * time.Second,
			RetryBackoffMultiplier: 2.0,
		}),
	)

	cfg := ProviderInstanceConfig{
		Name:       "failing-provider",
		TypeName:   "mock",
		RecordType: RecordTypeA,
		Target:     "192.0.2.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	}

	// InitializeProvider should NOT return an error for connection failures
	err := manager.InitializeProvider(cfg)
	if err != nil {
		t.Fatalf("expected no error (connection failure queues for retry), got: %v", err)
	}

	if manager.ReadyCount() != 0 {
		t.Errorf("expected 0 ready providers, got %d", manager.ReadyCount())
	}
	if manager.PendingCount() != 1 {
		t.Errorf("expected 1 pending provider, got %d", manager.PendingCount())
	}
	if manager.IsFullyReady() {
		t.Error("expected manager to NOT be fully ready")
	}

	// Check pending provider status
	pending := manager.PendingProviders()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending provider status, got %d", len(pending))
	}
	if pending[0].Name != "failing-provider" {
		t.Errorf("expected pending provider name 'failing-provider', got %s", pending[0].Name)
	}
	if pending[0].AttemptCount != 1 {
		t.Errorf("expected 1 attempt, got %d", pending[0].AttemptCount)
	}
}

func TestManager_InitializeProvider_InvalidConfigFails(t *testing.T) {
	logger := slog.Default()
	registry := NewRegistry(logger)

	manager := NewManager(registry, WithManagerLogger(logger))

	// Missing required fields should return an error immediately
	cfg := ProviderInstanceConfig{
		Name:     "",
		TypeName: "mock",
	}

	err := manager.InitializeProvider(cfg)
	if err == nil {
		t.Fatal("expected error for invalid config, got nil")
	}
}

func TestManager_RetryLoop_RecoversPendingProvider(t *testing.T) {
	logger := slog.Default()
	registry := NewRegistry(logger)

	mp := &managerTestProvider{name: "retry-provider", typeName: "mock"}
	// Fail 2 times, then succeed
	registry.RegisterFactory("mock", failingFactory(2, mp))

	manager := NewManager(registry,
		WithManagerLogger(logger),
		WithManagerConfig(ManagerConfig{
			InitialRetryInterval:   50 * time.Millisecond,
			MaxRetryInterval:       200 * time.Millisecond,
			RetryBackoffMultiplier: 1.5,
		}),
	)

	cfg := ProviderInstanceConfig{
		Name:       "retry-provider",
		TypeName:   "mock",
		RecordType: RecordTypeA,
		Target:     "192.0.2.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	}

	// First attempt fails
	err := manager.InitializeProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if manager.PendingCount() != 1 {
		t.Fatalf("expected 1 pending provider, got %d", manager.PendingCount())
	}

	// Start the retry loop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := manager.Start(ctx); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}

	// Wait for retries to complete (should take ~100-150ms for 2 more attempts)
	timeout := time.After(3 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			// Debug: print pending map contents
			pending := manager.PendingProviders()
			t.Logf("DEBUG: pending providers: %+v", pending)
			t.Fatalf("timeout waiting for provider to recover, pending=%d, ready=%d",
				manager.PendingCount(), manager.ReadyCount())
		case <-ticker.C:
			// Check if provider recovered - either fully ready OR moved from pending to ready
			if manager.ReadyCount() == 1 && manager.PendingCount() == 0 {
				// Success!
				manager.Stop()
				return
			}
		}
	}
}

func TestManager_AllProviderStatuses(t *testing.T) {
	logger := slog.Default()
	registry := NewRegistry(logger)

	mp := &managerTestProvider{name: "good-provider", typeName: "mock"}
	registry.RegisterFactory("mock", successFactory(mp))
	registry.RegisterFactory("broken", alwaysFailFactory())

	manager := NewManager(registry, WithManagerLogger(logger))

	// Initialize a successful provider
	_ = manager.InitializeProvider(ProviderInstanceConfig{
		Name:       "good-provider",
		TypeName:   "mock",
		RecordType: RecordTypeA,
		Target:     "192.0.2.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	// Initialize a failing provider
	_ = manager.InitializeProvider(ProviderInstanceConfig{
		Name:       "bad-provider",
		TypeName:   "broken",
		RecordType: RecordTypeA,
		Target:     "192.0.2.2",
		TTL:        300,
		Domains:    []string{"*.test.com"},
	})

	statuses := manager.AllProviderStatuses()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	// Find each status
	var goodStatus, badStatus *ProviderStatus
	for i := range statuses {
		if statuses[i].Name == "good-provider" {
			goodStatus = &statuses[i]
		} else if statuses[i].Name == "bad-provider" {
			badStatus = &statuses[i]
		}
	}

	if goodStatus == nil {
		t.Fatal("missing good-provider status")
	}
	if !goodStatus.Available {
		t.Error("good-provider should be available")
	}

	if badStatus == nil {
		t.Fatal("missing bad-provider status")
	}
	if badStatus.Available {
		t.Error("bad-provider should NOT be available")
	}
	if badStatus.Error == "" {
		t.Error("bad-provider should have an error message")
	}
}

func TestManager_ExponentialBackoff(t *testing.T) {
	logger := slog.Default()
	registry := NewRegistry(logger)

	registry.RegisterFactory("mock", alwaysFailFactory())

	manager := NewManager(registry,
		WithManagerLogger(logger),
		WithManagerConfig(ManagerConfig{
			InitialRetryInterval:   100 * time.Millisecond,
			MaxRetryInterval:       400 * time.Millisecond,
			RetryBackoffMultiplier: 2.0,
		}),
	)

	cfg := ProviderInstanceConfig{
		Name:       "backoff-test",
		TypeName:   "mock",
		RecordType: RecordTypeA,
		Target:     "192.0.2.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	}

	_ = manager.InitializeProvider(cfg)

	// Check initial retry interval
	pending := manager.PendingProviders()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}

	initialInterval := pending[0].NextRetryAt.Sub(pending[0].LastAttempt)
	if initialInterval < 90*time.Millisecond || initialInterval > 110*time.Millisecond {
		t.Errorf("expected ~100ms initial interval, got %v", initialInterval)
	}
}

func TestManager_InitializeProvider_PingFailsQueuesForRetry(t *testing.T) {
	// Test the case where the factory succeeds but Ping() fails.
	// This is the key scenario for providers like webhook that don't probe during creation.
	logger := slog.Default()
	registry := NewRegistry(logger)

	// Create provider that will fail Ping
	mp := &managerTestProvider{
		name:     "ping-fail-provider",
		typeName: "mock",
		pingErr:  errors.New("connection refused"),
	}
	registry.RegisterFactory("mock", successFactory(mp))

	manager := NewManager(registry,
		WithManagerLogger(logger),
		WithManagerConfig(ManagerConfig{
			InitialRetryInterval:   100 * time.Millisecond,
			MaxRetryInterval:       1 * time.Second,
			RetryBackoffMultiplier: 2.0,
		}),
	)

	cfg := ProviderInstanceConfig{
		Name:       "ping-fail-provider",
		TypeName:   "mock",
		RecordType: RecordTypeA,
		Target:     "192.0.2.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	}

	// InitializeProvider should NOT return an error - queues for retry
	err := manager.InitializeProvider(cfg)
	if err != nil {
		t.Fatalf("expected no error (ping failure queues for retry), got: %v", err)
	}

	// Provider should be pending, not ready
	if manager.ReadyCount() != 0 {
		t.Errorf("expected 0 ready providers, got %d", manager.ReadyCount())
	}
	if manager.PendingCount() != 1 {
		t.Errorf("expected 1 pending provider, got %d", manager.PendingCount())
	}

	// Verify Ping was called
	if mp.pingCount.Load() != 1 {
		t.Errorf("expected Ping to be called once, got %d", mp.pingCount.Load())
	}

	// Provider should NOT be in registry (was removed after ping failure)
	if _, ok := registry.Get("ping-fail-provider"); ok {
		t.Error("expected provider to be removed from registry after ping failure")
	}

	// Check pending status includes the ping error
	pending := manager.PendingProviders()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending provider, got %d", len(pending))
	}
	if pending[0].LastError == "" {
		t.Error("expected error message in pending status")
	}
}

func TestManager_RetryLoop_PingRecovery(t *testing.T) {
	// Test that retry loop correctly recovers when Ping starts succeeding.
	logger := slog.Default()
	registry := NewRegistry(logger)

	// Use a dynamic provider that fails Ping until a threshold
	pingAttempts := atomic.Int32{}
	dp := &dynamicPingProvider{
		name:         "ping-recover-provider",
		typeName:     "mock",
		pingAttempts: &pingAttempts,
		failUntil:    2, // Fail first 2 pings (init + first retry), succeed on third
	}

	registry.RegisterFactory("mock", func(cfg FactoryConfig) (Provider, error) {
		return dp, nil
	})

	manager := NewManager(registry,
		WithManagerLogger(logger),
		WithManagerConfig(ManagerConfig{
			InitialRetryInterval:   50 * time.Millisecond,
			MaxRetryInterval:       200 * time.Millisecond,
			RetryBackoffMultiplier: 1.5,
		}),
	)

	cfg := ProviderInstanceConfig{
		Name:       "ping-recover-provider",
		TypeName:   "mock",
		RecordType: RecordTypeA,
		Target:     "192.0.2.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	}

	// Initial attempt - ping fails (attempt 1)
	_ = manager.InitializeProvider(cfg)
	if manager.ReadyCount() != 0 {
		t.Errorf("expected 0 ready after initial attempt, got %d", manager.ReadyCount())
	}

	// Start retry loop
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_ = manager.Start(ctx)
	defer manager.Stop()

	// Wait for recovery (need 2 more retries: attempt 2 fails, attempt 3 succeeds)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if manager.ReadyCount() == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if manager.ReadyCount() != 1 {
		t.Errorf("expected 1 ready provider after recovery, got %d (ping attempts: %d)", manager.ReadyCount(), pingAttempts.Load())
	}
	if manager.PendingCount() != 0 {
		t.Errorf("expected 0 pending providers after recovery, got %d", manager.PendingCount())
	}
}
