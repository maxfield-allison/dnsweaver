package watcher

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/api/types/events"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.DebounceInterval != 2*time.Second {
		t.Errorf("expected DebounceInterval 2s, got %v", cfg.DebounceInterval)
	}

	if cfg.ReconnectInterval != 5*time.Second {
		t.Errorf("expected ReconnectInterval 5s, got %v", cfg.ReconnectInterval)
	}
}

func TestMockWatcher_Start(t *testing.T) {
	mock := NewMockWatcher()

	if mock.IsRunning() {
		t.Error("expected mock to not be running initially")
	}

	err := mock.Start(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !mock.IsRunning() {
		t.Error("expected mock to be running after Start")
	}
}

func TestMockWatcher_Stop(t *testing.T) {
	mock := NewMockWatcher()
	_ = mock.Start(context.Background())

	mock.Stop()

	if mock.IsRunning() {
		t.Error("expected mock to not be running after Stop")
	}
}

func TestMockWatcher_SimulateEvent(t *testing.T) {
	mock := NewMockWatcher()

	if mock.TriggerCount() != 0 {
		t.Error("expected 0 triggers initially")
	}

	mock.SimulateEvent()
	mock.SimulateEvent()
	mock.SimulateEvent()

	if mock.TriggerCount() != 3 {
		t.Errorf("expected 3 triggers, got %d", mock.TriggerCount())
	}
}

func TestMockWatcher_OnTrigger(t *testing.T) {
	mock := NewMockWatcher()

	var called int32
	mock.OnTrigger(func() {
		atomic.AddInt32(&called, 1)
	})

	mock.SimulateEvent()
	mock.SimulateEvent()

	if atomic.LoadInt32(&called) != 2 {
		t.Errorf("expected callback called 2 times, got %d", called)
	}
}

func TestMockWatcher_SimulatedError(t *testing.T) {
	mock := NewMockWatcher()
	mock.SimulatedError = context.DeadlineExceeded

	err := mock.Start(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded error, got %v", err)
	}

	if mock.IsRunning() {
		t.Error("expected mock to not be running when Start fails")
	}
}

// TestNew_WithOptions tests the watcher constructor with options.
func TestNew_WithOptions(t *testing.T) {
	cfg := Config{
		DebounceInterval:  100 * time.Millisecond,
		ReconnectInterval: 200 * time.Millisecond,
	}

	var called bool
	onReconcile := func() {
		called = true
	}

	// Can't test with real Docker client in unit tests,
	// but we can verify the constructor doesn't panic
	w := New(nil, onReconcile, WithConfig(cfg))

	if w.config.DebounceInterval != 100*time.Millisecond {
		t.Errorf("expected debounce 100ms, got %v", w.config.DebounceInterval)
	}

	if w.config.ReconnectInterval != 200*time.Millisecond {
		t.Errorf("expected reconnect 200ms, got %v", w.config.ReconnectInterval)
	}

	// Verify callback is set
	if w.onReconcile == nil {
		t.Error("expected onReconcile to be set")
	}

	// But not called yet
	if called {
		t.Error("expected callback not to be called yet")
	}
}

func TestWatcher_TriggerNow(t *testing.T) {
	var triggered bool
	onReconcile := func() {
		triggered = true
	}

	w := New(nil, onReconcile)

	// TriggerNow should call the callback immediately
	w.TriggerNow()

	if !triggered {
		t.Error("expected TriggerNow to call onReconcile")
	}
}

func TestWatcher_IsRunning(t *testing.T) {
	w := New(nil, func() {})

	// Initially not running
	if w.IsRunning() {
		t.Error("expected watcher to not be running initially")
	}
}

// ============================================================================
// Event Debouncing Tests (#68)
// ============================================================================

// TestWatcher_Debounce_SingleEvent verifies that a single event triggers
// reconciliation after the debounce interval.
func TestWatcher_Debounce_SingleEvent(t *testing.T) {
	var reconcileCalled int32
	onReconcile := func() {
		atomic.AddInt32(&reconcileCalled, 1)
	}

	w := New(nil, onReconcile, WithConfig(Config{
		DebounceInterval:  50 * time.Millisecond,
		ReconnectInterval: 100 * time.Millisecond,
	}))

	// Simulate receiving an event (call handleEvent directly)
	w.handleEvent(createTestEvent("container", "start", "test-container"))

	// Immediately after, reconcile should NOT have been called
	if atomic.LoadInt32(&reconcileCalled) != 0 {
		t.Error("reconcile should not be called immediately after event")
	}

	// Wait for debounce interval + buffer
	time.Sleep(80 * time.Millisecond)

	if atomic.LoadInt32(&reconcileCalled) != 1 {
		t.Errorf("expected exactly 1 reconcile call, got %d", reconcileCalled)
	}
}

// TestWatcher_Debounce_RapidEvents verifies that multiple rapid events only
// trigger ONE reconciliation (debounce timer resets on each event).
func TestWatcher_Debounce_RapidEvents(t *testing.T) {
	var reconcileCalled int32
	onReconcile := func() {
		atomic.AddInt32(&reconcileCalled, 1)
	}

	w := New(nil, onReconcile, WithConfig(Config{
		DebounceInterval:  100 * time.Millisecond,
		ReconnectInterval: 100 * time.Millisecond,
	}))

	// Simulate 5 rapid events, each 20ms apart
	// Total time: 80ms < debounce interval (100ms)
	for i := 0; i < 5; i++ {
		w.handleEvent(createTestEvent("container", "start", "container-"+string(rune('a'+i))))
		time.Sleep(20 * time.Millisecond)
	}

	// At this point, last event was at ~80ms, debounce timer will fire at ~180ms
	// Wait until 130ms from start — still before debounce fires
	time.Sleep(30 * time.Millisecond) // Now at ~110ms total

	if atomic.LoadInt32(&reconcileCalled) != 0 {
		t.Error("reconcile should not be called during debounce period")
	}

	// Wait for debounce to complete (need ~70ms more from the last event)
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&reconcileCalled) != 1 {
		t.Errorf("expected exactly 1 reconcile call after debounce, got %d", reconcileCalled)
	}
}

// TestWatcher_Debounce_RespectsInterval verifies debounce uses configured interval.
func TestWatcher_Debounce_RespectsInterval(t *testing.T) {
	var reconcileTime time.Time
	var reconcileCalled int32
	var eventTime time.Time

	onReconcile := func() {
		reconcileTime = time.Now()
		atomic.AddInt32(&reconcileCalled, 1)
	}

	debounceInterval := 100 * time.Millisecond
	w := New(nil, onReconcile, WithConfig(Config{
		DebounceInterval:  debounceInterval,
		ReconnectInterval: 100 * time.Millisecond,
	}))

	// Fire event and record time
	eventTime = time.Now()
	w.handleEvent(createTestEvent("container", "start", "test"))

	// Wait for reconcile
	time.Sleep(150 * time.Millisecond)

	if atomic.LoadInt32(&reconcileCalled) != 1 {
		t.Fatalf("expected reconcile to be called, got %d calls", reconcileCalled)
	}

	// Verify timing — reconcile should happen ~debounceInterval after event
	elapsed := reconcileTime.Sub(eventTime)
	if elapsed < debounceInterval {
		t.Errorf("reconcile happened too early: %v < %v", elapsed, debounceInterval)
	}
	// Allow some slack for timing
	if elapsed > debounceInterval+50*time.Millisecond {
		t.Errorf("reconcile happened too late: %v > %v", elapsed, debounceInterval+50*time.Millisecond)
	}
}

// ============================================================================
// Lifecycle Edge Case Tests (#68)
// ============================================================================

// TestWatcher_Stop_Idempotent verifies calling Stop multiple times is safe.
func TestWatcher_Stop_Idempotent(t *testing.T) {
	w := New(nil, func() {})

	// Stop without starting — should not panic
	w.Stop()
	w.Stop()
	w.Stop()

	// All stops should be no-ops
	if w.IsRunning() {
		t.Error("watcher should not be running after Stop calls")
	}
}

// TestWatcher_Start_WhenAlreadyRunning verifies Start is idempotent.
func TestWatcher_Start_WhenAlreadyRunning(t *testing.T) {
	w := New(nil, func() {})

	// Manually set running state (simulating a started watcher)
	w.mu.Lock()
	w.running = true
	w.mu.Unlock()

	// Start should return nil and not change state
	err := w.Start(context.Background())
	if err != nil {
		t.Errorf("Start when already running should return nil, got %v", err)
	}

	if !w.IsRunning() {
		t.Error("watcher should still be running")
	}
}

// TestWatcher_TriggerNow_CancelsDebounce verifies TriggerNow cancels pending debounce.
func TestWatcher_TriggerNow_CancelsDebounce(t *testing.T) {
	var reconcileCalled int32
	onReconcile := func() {
		atomic.AddInt32(&reconcileCalled, 1)
	}

	w := New(nil, onReconcile, WithConfig(Config{
		DebounceInterval:  500 * time.Millisecond, // Long debounce
		ReconnectInterval: 100 * time.Millisecond,
	}))

	// Start a debounce timer
	w.handleEvent(createTestEvent("container", "start", "test"))

	// Immediately call TriggerNow — should cancel debounce and trigger immediately
	time.Sleep(10 * time.Millisecond) // Small delay to ensure timer is set
	w.TriggerNow()

	// Should have exactly 1 call from TriggerNow
	if atomic.LoadInt32(&reconcileCalled) != 1 {
		t.Errorf("expected 1 reconcile call from TriggerNow, got %d", reconcileCalled)
	}

	// Wait to ensure debounce timer doesn't fire again
	time.Sleep(600 * time.Millisecond)

	// Should still be exactly 1 call
	if atomic.LoadInt32(&reconcileCalled) != 1 {
		t.Errorf("debounce timer should have been canceled, got %d calls", reconcileCalled)
	}
}

// TestWatcher_TriggerNow_WithNilCallback verifies TriggerNow handles nil callback safely.
func TestWatcher_TriggerNow_WithNilCallback(t *testing.T) {
	w := New(nil, nil) // nil callback

	// Should not panic
	w.TriggerNow()
}

// TestWatcher_Stop_CancelsPendingDebounce verifies Stop cancels pending debounce timer.
func TestWatcher_Stop_CancelsPendingDebounce(t *testing.T) {
	var reconcileCalled int32
	onReconcile := func() {
		atomic.AddInt32(&reconcileCalled, 1)
	}

	w := New(nil, onReconcile, WithConfig(Config{
		DebounceInterval:  200 * time.Millisecond,
		ReconnectInterval: 100 * time.Millisecond,
	}))

	// Start a debounce timer
	w.handleEvent(createTestEvent("container", "start", "test"))

	// Stop the watcher
	w.Stop()

	// Wait for what would have been the debounce period
	time.Sleep(300 * time.Millisecond)

	// Reconcile should NOT have been called because Stop canceled the timer
	if atomic.LoadInt32(&reconcileCalled) != 0 {
		t.Errorf("Stop should cancel debounce timer, got %d calls", reconcileCalled)
	}
}

// ============================================================================
// Event Filtering Tests (#68)
// ============================================================================

// TestWatcher_BuildEventFilters_SwarmMode verifies correct filters for Swarm.
func TestWatcher_BuildEventFilters_SwarmMode(t *testing.T) {
	w := New(nil, func() {})

	filters := w.buildEventFilters(true) // isSwarm = true

	// Should filter for service events
	typeFilters := filters.Get("type")
	if len(typeFilters) != 1 || typeFilters[0] != "service" {
		t.Errorf("expected type filter 'service', got %v", typeFilters)
	}

	// Should include create, update, remove actions
	eventFilters := filters.Get("event")
	expectedEvents := map[string]bool{"create": true, "update": true, "remove": true}
	if len(eventFilters) != 3 {
		t.Errorf("expected 3 event filters, got %d: %v", len(eventFilters), eventFilters)
	}
	for _, e := range eventFilters {
		if !expectedEvents[e] {
			t.Errorf("unexpected event filter: %s", e)
		}
	}
}

// TestWatcher_BuildEventFilters_StandaloneMode verifies correct filters for standalone.
func TestWatcher_BuildEventFilters_StandaloneMode(t *testing.T) {
	w := New(nil, func() {})

	filters := w.buildEventFilters(false) // isSwarm = false

	// Should filter for container events
	typeFilters := filters.Get("type")
	if len(typeFilters) != 1 || typeFilters[0] != "container" {
		t.Errorf("expected type filter 'container', got %v", typeFilters)
	}

	// Should include start, stop, die, destroy actions
	eventFilters := filters.Get("event")
	expectedEvents := map[string]bool{"start": true, "stop": true, "die": true, "destroy": true}
	if len(eventFilters) != 4 {
		t.Errorf("expected 4 event filters, got %d: %v", len(eventFilters), eventFilters)
	}
	for _, e := range eventFilters {
		if !expectedEvents[e] {
			t.Errorf("unexpected event filter: %s", e)
		}
	}
}

// ============================================================================
// WithLogger Option Tests
// ============================================================================

// TestWatcher_WithLogger verifies the logger option works correctly.
func TestWatcher_WithLogger(t *testing.T) {
	// Create watcher without logger option — should use default
	w := New(nil, func() {})
	if w.logger == nil {
		t.Error("expected default logger to be set")
	}
}

// TestWatcher_WithLogger_Nil verifies nil logger is ignored.
func TestWatcher_WithLogger_Nil(t *testing.T) {
	w := New(nil, func() {}, WithLogger(nil))
	if w.logger == nil {
		t.Error("nil logger option should be ignored, default should be used")
	}
}

// ============================================================================
// Helper Functions
// ============================================================================

// createTestEvent creates a Docker event message for testing.
func createTestEvent(eventType, action, actorID string) events.Message {
	return events.Message{
		Type:   events.Type(eventType),
		Action: events.Action(action),
		Actor: events.Actor{
			ID: actorID,
			Attributes: map[string]string{
				"name": actorID,
			},
		},
		Time:     time.Now().Unix(),
		TimeNano: time.Now().UnixNano(),
	}
}
