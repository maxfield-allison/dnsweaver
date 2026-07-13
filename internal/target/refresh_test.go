package target

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubResolver returns queued values/errors in order.
type stubResolver struct {
	mu      sync.Mutex
	results []struct {
		val string
		err error
	}
	idx int
}

func (s *stubResolver) push(val string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results = append(s.results, struct {
		val string
		err error
	}{val, err})
}

func (s *stubResolver) Resolve(_ context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx >= len(s.results) {
		// Repeat the last result once exhausted.
		if len(s.results) == 0 {
			return "", errors.New("no result")
		}
		last := s.results[len(s.results)-1]
		return last.val, last.err
	}
	r := s.results[s.idx]
	s.idx++
	return r.val, r.err
}

func (s *stubResolver) Describe() string { return "stub" }

func TestRefresher_InitialResolveSetsTarget(t *testing.T) {
	stub := &stubResolver{}
	stub.push("203.0.113.1", nil)

	var got atomic.Pointer[string]
	r := NewRefresher(RefresherConfig{
		Resolver: stub,
		Interval: time.Hour, // no periodic tick during the test
		OnChange: func(v string) { got.Store(&v) },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	initial := r.Start(ctx)
	defer r.Stop()

	if initial != "203.0.113.1" {
		t.Errorf("initial = %q, want 203.0.113.1", initial)
	}
	if v := got.Load(); v == nil || *v != "203.0.113.1" {
		t.Errorf("OnChange not called with initial value: %v", v)
	}
}

func TestRefresher_KeepsLastGoodOnFailure(t *testing.T) {
	stub := &stubResolver{}
	stub.push("203.0.113.1", nil)     // initial ok
	stub.push("", errors.New("boom")) // refresh fails

	var changes int32
	r := NewRefresher(RefresherConfig{
		Resolver: stub,
		Interval: 20 * time.Millisecond,
		OnChange: func(_ string) { atomic.AddInt32(&changes, 1) },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	initial := r.Start(ctx)
	if initial != "203.0.113.1" {
		t.Fatalf("initial = %q, want 203.0.113.1", initial)
	}

	// Let a few refresh ticks fire (all failing).
	time.Sleep(80 * time.Millisecond)
	r.Stop()

	// Only the initial resolution should have triggered OnChange; failures keep
	// the last known-good value and do not call OnChange.
	if n := atomic.LoadInt32(&changes); n != 1 {
		t.Errorf("OnChange calls = %d, want 1 (failures must not churn)", n)
	}
}

func TestRefresher_OnChangeOnlyWhenChanged(t *testing.T) {
	stub := &stubResolver{}
	stub.push("203.0.113.1", nil) // initial
	stub.push("203.0.113.1", nil) // same value
	stub.push("203.0.113.9", nil) // changed

	var mu sync.Mutex
	var values []string
	r := NewRefresher(RefresherConfig{
		Resolver: stub,
		Interval: 20 * time.Millisecond,
		OnChange: func(v string) {
			mu.Lock()
			values = append(values, v)
			mu.Unlock()
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)
	time.Sleep(120 * time.Millisecond)
	r.Stop()

	mu.Lock()
	defer mu.Unlock()
	// Expect the initial value and the later changed value, but not the
	// duplicate in between.
	if len(values) < 2 {
		t.Fatalf("expected at least 2 OnChange calls, got %v", values)
	}
	if values[0] != "203.0.113.1" {
		t.Errorf("first value = %q, want 203.0.113.1", values[0])
	}
	last := values[len(values)-1]
	if last != "203.0.113.9" {
		t.Errorf("last value = %q, want 203.0.113.9", last)
	}
}
