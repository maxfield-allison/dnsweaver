package source

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mockDiscoverableSource implements Source with discovery support.
type mockDiscoverableSource struct {
	name       string
	hostnames  []Hostname
	discovered []Hostname
	mu         sync.Mutex
}

func (m *mockDiscoverableSource) Name() string {
	return m.name
}

func (m *mockDiscoverableSource) Extract(_ context.Context, _ map[string]string) ([]Hostname, error) {
	return m.hostnames, nil
}

func (m *mockDiscoverableSource) SupportsDiscovery() bool {
	return true
}

func (m *mockDiscoverableSource) Discover(_ context.Context) ([]Hostname, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.discovered, nil
}

func (m *mockDiscoverableSource) SetDiscovered(hostnames []Hostname) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.discovered = hostnames
}

func TestFileWatcher_Start(t *testing.T) {
	reg := NewRegistry(nil)
	source := &mockDiscoverableSource{
		name:       "test",
		discovered: []Hostname{{Name: "app.example.com"}},
	}
	_ = reg.Register(source)

	var callbackCalled bool
	var callbackMu sync.Mutex
	callback := func(sourceName string, hostnames []Hostname) {
		callbackMu.Lock()
		callbackCalled = true
		callbackMu.Unlock()
	}

	w := NewFileWatcher(reg, callback, WithPollInterval(10*time.Millisecond))

	ctx := context.Background()
	err := w.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for initial poll
	time.Sleep(50 * time.Millisecond)

	callbackMu.Lock()
	if !callbackCalled {
		t.Error("callback was not called on initial discovery")
	}
	callbackMu.Unlock()

	if !w.IsRunning() {
		t.Error("watcher should be running")
	}

	w.Stop()

	// Give it time to stop
	time.Sleep(20 * time.Millisecond)

	if w.IsRunning() {
		t.Error("watcher should not be running after Stop()")
	}
}

func TestFileWatcher_DetectsChanges(t *testing.T) {
	reg := NewRegistry(nil)
	source := &mockDiscoverableSource{
		name:       "test",
		discovered: []Hostname{{Name: "app1.example.com"}},
	}
	_ = reg.Register(source)

	var calls [][]Hostname
	var callMu sync.Mutex
	callback := func(sourceName string, hostnames []Hostname) {
		callMu.Lock()
		calls = append(calls, hostnames)
		callMu.Unlock()
	}

	w := NewFileWatcher(reg, callback, WithPollInterval(20*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = w.Start(ctx)

	// Wait for initial poll
	time.Sleep(30 * time.Millisecond)

	// Change discovered hostnames
	source.SetDiscovered([]Hostname{
		{Name: "app1.example.com"},
		{Name: "app2.example.com"},
	})

	// Wait for next poll
	time.Sleep(30 * time.Millisecond)

	callMu.Lock()
	if len(calls) < 2 {
		t.Errorf("expected at least 2 callback calls, got %d", len(calls))
	}
	callMu.Unlock()

	w.Stop()
}

func TestFileWatcher_NoChangeNoDuplicate(t *testing.T) {
	reg := NewRegistry(nil)
	source := &mockDiscoverableSource{
		name:       "test",
		discovered: []Hostname{{Name: "app.example.com"}},
	}
	_ = reg.Register(source)

	var callCount int
	var callMu sync.Mutex
	callback := func(sourceName string, hostnames []Hostname) {
		callMu.Lock()
		callCount++
		callMu.Unlock()
	}

	w := NewFileWatcher(reg, callback, WithPollInterval(15*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = w.Start(ctx)

	// Wait for multiple poll cycles
	time.Sleep(60 * time.Millisecond)

	callMu.Lock()
	// Should only be called once (initial discovery), not on subsequent polls
	if callCount != 1 {
		t.Errorf("expected 1 callback call (no changes), got %d", callCount)
	}
	callMu.Unlock()

	w.Stop()
}

func TestFileWatcher_PollNow(t *testing.T) {
	reg := NewRegistry(nil)
	source := &mockDiscoverableSource{
		name:       "test",
		discovered: []Hostname{{Name: "app.example.com"}},
	}
	_ = reg.Register(source)

	var callCount int
	var callMu sync.Mutex
	callback := func(sourceName string, hostnames []Hostname) {
		callMu.Lock()
		callCount++
		callMu.Unlock()
	}

	w := NewFileWatcher(reg, callback, WithPollInterval(1*time.Hour)) // Long interval

	ctx := context.Background()

	// PollNow without starting
	w.PollNow(ctx)

	callMu.Lock()
	if callCount != 1 {
		t.Errorf("PollNow should trigger callback, got %d calls", callCount)
	}
	callMu.Unlock()

	// Change hostnames and poll again
	source.SetDiscovered([]Hostname{
		{Name: "app.example.com"},
		{Name: "new.example.com"},
	})

	w.PollNow(ctx)

	callMu.Lock()
	if callCount != 2 {
		t.Errorf("PollNow after change should trigger callback, got %d calls", callCount)
	}
	callMu.Unlock()
}

func TestFileWatcher_SkipsNonDiscoverableSources(t *testing.T) {
	reg := NewRegistry(nil)

	// Register a non-discoverable source
	nonDisc := &mockSource{name: "static"}
	reg.Register(nonDisc)

	// Register a discoverable source
	disc := &mockDiscoverableSource{
		name:       "dynamic",
		discovered: []Hostname{{Name: "app.example.com"}},
	}
	reg.Register(disc)

	var sourcesCallled []string
	var callMu sync.Mutex
	callback := func(sourceName string, hostnames []Hostname) {
		callMu.Lock()
		sourcesCallled = append(sourcesCallled, sourceName)
		callMu.Unlock()
	}

	w := NewFileWatcher(reg, callback)

	ctx := context.Background()
	w.PollNow(ctx)

	callMu.Lock()
	if len(sourcesCallled) != 1 || sourcesCallled[0] != "dynamic" {
		t.Errorf("expected only 'dynamic' source, got %v", sourcesCallled)
	}
	callMu.Unlock()
}

func TestFileWatcher_EmptyRegistry(t *testing.T) {
	reg := NewRegistry(nil)

	var called bool
	callback := func(sourceName string, hostnames []Hostname) {
		called = true
	}

	w := NewFileWatcher(reg, callback)

	ctx := context.Background()
	w.PollNow(ctx)

	if called {
		t.Error("callback should not be called with empty registry")
	}
}
