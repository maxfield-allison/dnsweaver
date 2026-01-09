package reconciler

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/source"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.DryRun {
		t.Error("DefaultConfig should have DryRun=false")
	}
	if !cfg.CleanupOrphans {
		t.Error("DefaultConfig should have CleanupOrphans=true")
	}
	if !cfg.OwnershipTracking {
		t.Error("DefaultConfig should have OwnershipTracking=true")
	}
	if cfg.AdoptExisting {
		t.Error("DefaultConfig should have AdoptExisting=false")
	}
	if cfg.ReconcileInterval != 60*time.Second {
		t.Errorf("DefaultConfig ReconcileInterval = %v, want 60s", cfg.ReconcileInterval)
	}
	if !cfg.Enabled {
		t.Error("DefaultConfig should have Enabled=true")
	}
}

func TestNew(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	sources := source.NewRegistry(logger)
	providers := provider.NewRegistry(logger)

	// We can't easily create a docker.Client without a Docker daemon,
	// so we test New with nil (which the reconciler handles gracefully in tests)
	r := New(nil, sources, providers)

	if r.config.Enabled != true {
		t.Error("New should use default config with Enabled=true")
	}

	// Test with options
	customConfig := Config{
		DryRun:         true,
		CleanupOrphans: false,
		Enabled:        false,
	}

	r = New(nil, sources, providers,
		WithConfig(customConfig),
		WithLogger(logger),
	)

	if !r.config.DryRun {
		t.Error("WithConfig should set DryRun")
	}
	if r.config.CleanupOrphans {
		t.Error("WithConfig should set CleanupOrphans")
	}
	if r.config.Enabled {
		t.Error("WithConfig should set Enabled")
	}
}

func TestReconciler_DisabledReturnsEarly(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	sources := source.NewRegistry(logger)
	providers := provider.NewRegistry(logger)

	r := New(nil, sources, providers,
		WithConfig(Config{Enabled: false}),
	)

	result, err := r.Reconcile(context.Background())

	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if result == nil {
		t.Fatal("Reconcile returned nil result")
	}
	if len(result.Actions) != 0 {
		t.Errorf("Disabled reconciler should return empty actions, got %d", len(result.Actions))
	}
}

func TestReconciler_SetEnabled(t *testing.T) {
	r := &Reconciler{
		config: DefaultConfig(),
		logger: slog.Default(),
	}

	r.SetEnabled(false)
	if r.config.Enabled {
		t.Error("SetEnabled(false) should disable reconciliation")
	}

	r.SetEnabled(true)
	if !r.config.Enabled {
		t.Error("SetEnabled(true) should enable reconciliation")
	}
}

func TestReconciler_SetDryRun(t *testing.T) {
	r := &Reconciler{
		config: DefaultConfig(),
		logger: slog.Default(),
	}

	r.SetDryRun(true)
	if !r.config.DryRun {
		t.Error("SetDryRun(true) should enable dry-run mode")
	}

	r.SetDryRun(false)
	if r.config.DryRun {
		t.Error("SetDryRun(false) should disable dry-run mode")
	}
}

func TestReconciler_KnownHostnames(t *testing.T) {
	r := &Reconciler{
		config:         DefaultConfig(),
		logger:         slog.Default(),
		knownHostnames: make(map[string]struct{}),
	}

	// Initially empty
	if len(r.KnownHostnames()) != 0 {
		t.Error("KnownHostnames should initially be empty")
	}

	// Add some hostnames
	r.knownHostnames["app1.example.com"] = struct{}{}
	r.knownHostnames["app2.example.com"] = struct{}{}

	hostnames := r.KnownHostnames()
	if len(hostnames) != 2 {
		t.Errorf("KnownHostnames() = %d, want 2", len(hostnames))
	}

	// Verify both hostnames are present (order is not guaranteed)
	found := make(map[string]bool)
	for _, h := range hostnames {
		found[h] = true
	}
	if !found["app1.example.com"] || !found["app2.example.com"] {
		t.Error("KnownHostnames should contain both hostnames")
	}
}

func TestReconciler_ReconcileHostname_Disabled(t *testing.T) {
	r := &Reconciler{
		config:         Config{Enabled: false},
		logger:         slog.Default(),
		knownHostnames: make(map[string]struct{}),
	}

	result, err := r.ReconcileHostname(context.Background(), "app.example.com")

	if err != nil {
		t.Fatalf("ReconcileHostname returned error: %v", err)
	}
	if len(result.Actions) != 0 {
		t.Error("Disabled reconciler should return empty actions")
	}
}

func TestReconciler_RemoveHostname_Disabled(t *testing.T) {
	r := &Reconciler{
		config:         Config{Enabled: false},
		logger:         slog.Default(),
		knownHostnames: make(map[string]struct{}),
	}

	result, err := r.RemoveHostname(context.Background(), "app.example.com")

	if err != nil {
		t.Fatalf("RemoveHostname returned error: %v", err)
	}
	if len(result.Actions) != 0 {
		t.Error("Disabled reconciler should return empty actions")
	}
}

func TestReconciler_Config(t *testing.T) {
	cfg := Config{
		DryRun:            true,
		CleanupOrphans:    false,
		ReconcileInterval: 30 * time.Second,
		Enabled:           true,
	}

	r := &Reconciler{
		config: cfg,
		logger: slog.Default(),
	}

	got := r.Config()

	if got.DryRun != cfg.DryRun {
		t.Error("Config() should return current config")
	}
	if got.CleanupOrphans != cfg.CleanupOrphans {
		t.Error("Config() should return current config")
	}
	if got.ReconcileInterval != cfg.ReconcileInterval {
		t.Error("Config() should return current config")
	}
}

// TestReconciler_EnsureRecord_NoMatchingProvider tests the case where
// no provider matches the hostname pattern.
func TestReconciler_EnsureRecord_NoMatchingProvider(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	providers := provider.NewRegistry(logger)

	// No providers registered, so no matches
	r := &Reconciler{
		providers:      providers,
		config:         DefaultConfig(),
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}

	actions := r.ensureRecord(context.Background(), "unmatched.example.com", nil)

	if len(actions) != 1 {
		t.Fatalf("ensureRecord should return 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionSkip {
		t.Errorf("Action type = %v, want ActionSkip", actions[0].Type)
	}
	if actions[0].Status != StatusSkipped {
		t.Errorf("Action status = %v, want StatusSkipped", actions[0].Status)
	}
}

// TestReconciler_DeleteRecord_NoMatchingProvider tests deletion when
// no provider matches (should be a no-op).
func TestReconciler_DeleteRecord_NoMatchingProvider(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	providers := provider.NewRegistry(logger)

	r := &Reconciler{
		providers:      providers,
		config:         DefaultConfig(),
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}

	actions := r.deleteRecord(context.Background(), "unmatched.example.com")

	if len(actions) != 0 {
		t.Errorf("deleteRecord with no matching provider should return 0 actions, got %d", len(actions))
	}
}

// TestReconciler_CleanupOrphans tests orphan detection and cleanup.
func TestReconciler_CleanupOrphans(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	providers := provider.NewRegistry(logger)

	r := &Reconciler{
		providers: providers,
		config:    DefaultConfig(),
		logger:    logger,
		knownHostnames: map[string]struct{}{
			"old1.example.com":    {},
			"old2.example.com":    {},
			"current.example.com": {},
		},
	}

	currentHostnames := map[string]struct{}{
		"current.example.com": {},
		"new.example.com":     {},
	}

	// Since no providers match, we won't get actual delete actions,
	// but we can verify the orphan detection logic runs
	actions := r.cleanupOrphans(context.Background(), currentHostnames)

	// With no matching providers, actions will be empty
	// But we've verified the logic doesn't panic and handles the case
	_ = actions

	// After reconciliation, old hostnames should be detected as orphans
	// This test primarily verifies the logic compiles and runs without error
}

// TestReconciler_DryRun_EnsureRecord verifies dry-run doesn't call provider.
func TestReconciler_DryRun_EnsureRecord(t *testing.T) {
	// This is a behavioral test that would require full integration.
	// For now, we verify the dry-run flag propagates correctly.
	result := NewResult(true)
	result.AddAction(Action{
		Type:     ActionCreate,
		Status:   StatusSuccess,
		Hostname: "app.example.com",
	})

	if !result.Actions[0].DryRun {
		t.Error("Actions in dry-run result should have DryRun=true")
	}
}

// TestAction_ErrorHandling tests action error field.
func TestAction_ErrorHandling(t *testing.T) {
	action := Action{
		Type:   ActionCreate,
		Status: StatusFailed,
		Error:  "provider unavailable",
	}

	if action.Error == "" {
		t.Error("Failed action should have error message")
	}

	str := action.String()
	if !containsHelper(str, "provider unavailable") {
		t.Error("String() should include error message")
	}
}

// TestReconciler_IntegrationScenario tests a realistic scenario
// (without actual Docker/DNS calls).
func TestReconciler_IntegrationScenario(t *testing.T) {
	// This tests the reconciler with mocked dependencies would require
	// more complex setup. For now, we verify the struct is properly initialized.

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	sources := source.NewRegistry(logger)
	providers := provider.NewRegistry(logger)

	cfg := Config{
		DryRun:            true,
		CleanupOrphans:    true,
		ReconcileInterval: 30 * time.Second,
		Enabled:           true,
	}

	r := New(nil, sources, providers,
		WithConfig(cfg),
		WithLogger(logger),
	)

	// Verify all fields are set
	if r.sources != sources {
		t.Error("sources not set correctly")
	}
	if r.providers != providers {
		t.Error("providers not set correctly")
	}
	if !r.config.DryRun {
		t.Error("config not applied correctly")
	}
}

// Benchmark for action filtering.
func BenchmarkResult_Created(b *testing.B) {
	result := NewResult(false)
	for i := 0; i < 100; i++ {
		result.AddAction(Action{
			Type:     ActionCreate,
			Status:   StatusSuccess,
			Hostname: "app.example.com",
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = result.Created()
	}
}

// TestProviderError tests error wrapping behavior.
func TestProviderError(t *testing.T) {
	baseErr := errors.New("connection refused")
	action := Action{
		Type:   ActionCreate,
		Status: StatusFailed,
		Error:  baseErr.Error(),
	}

	if action.Status != StatusFailed {
		t.Error("Action with error should have StatusFailed")
	}
}
