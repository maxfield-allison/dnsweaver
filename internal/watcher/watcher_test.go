package watcher

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
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
