package source

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
)

// mockSource implements Source for testing.
type mockSource struct {
	name      string
	hostnames []Hostname
	err       error
}

func (m *mockSource) Name() string { return m.name }

func (m *mockSource) Extract(ctx context.Context, labels map[string]string) ([]Hostname, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.hostnames, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry(testLogger())

	src := &mockSource{name: "test"}
	err := r.Register(src)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if r.Count() != 1 {
		t.Errorf("Count() = %d, want 1", r.Count())
	}

	got := r.Get("test")
	if got != src {
		t.Error("Get returned wrong source")
	}
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	r := NewRegistry(testLogger())

	src1 := &mockSource{name: "dupe"}
	src2 := &mockSource{name: "dupe"}

	if err := r.Register(src1); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	err := r.Register(src2)
	if err == nil {
		t.Error("expected error for duplicate source")
	}

	var dupeErr *DuplicateSourceError
	if !errors.As(err, &dupeErr) {
		t.Errorf("error type = %T, want *DuplicateSourceError", err)
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := NewRegistry(testLogger())

	got := r.Get("nonexistent")
	if got != nil {
		t.Error("Get returned non-nil for missing source")
	}
}

func TestRegistry_All(t *testing.T) {
	r := NewRegistry(testLogger())

	src1 := &mockSource{name: "first"}
	src2 := &mockSource{name: "second"}
	src3 := &mockSource{name: "third"}

	_ = r.Register(src1)
	_ = r.Register(src2)
	_ = r.Register(src3)

	all := r.All()
	if len(all) != 3 {
		t.Fatalf("All() returned %d sources, want 3", len(all))
	}

	// Verify order is preserved
	if all[0].Name() != "first" {
		t.Errorf("all[0].Name() = %q, want %q", all[0].Name(), "first")
	}
	if all[1].Name() != "second" {
		t.Errorf("all[1].Name() = %q, want %q", all[1].Name(), "second")
	}
	if all[2].Name() != "third" {
		t.Errorf("all[2].Name() = %q, want %q", all[2].Name(), "third")
	}
}

func TestRegistry_ExtractAll(t *testing.T) {
	r := NewRegistry(testLogger())

	src1 := &mockSource{
		name: "source1",
		hostnames: []Hostname{
			{Name: "app1.example.com", Source: "source1", Router: "app1"},
		},
	}
	src2 := &mockSource{
		name: "source2",
		hostnames: []Hostname{
			{Name: "app2.example.com", Source: "source2", Router: "app2"},
			{Name: "app3.example.com", Source: "source2", Router: "app3"},
		},
	}

	_ = r.Register(src1)
	_ = r.Register(src2)

	labels := map[string]string{"some": "labels"}
	hostnames := r.ExtractAll(context.Background(), labels)

	if len(hostnames) != 3 {
		t.Fatalf("ExtractAll returned %d hostnames, want 3", len(hostnames))
	}

	// Verify order matches source registration order
	wantNames := []string{"app1.example.com", "app2.example.com", "app3.example.com"}
	for i, want := range wantNames {
		if hostnames[i].Name != want {
			t.Errorf("hostnames[%d].Name = %q, want %q", i, hostnames[i].Name, want)
		}
	}
}

func TestRegistry_ExtractAll_WithErrors(t *testing.T) {
	r := NewRegistry(testLogger())

	src1 := &mockSource{
		name: "good1",
		hostnames: []Hostname{
			{Name: "good1.example.com", Source: "good1"},
		},
	}
	src2 := &mockSource{
		name: "bad",
		err:  errors.New("parse error"),
	}
	src3 := &mockSource{
		name: "good2",
		hostnames: []Hostname{
			{Name: "good2.example.com", Source: "good2"},
		},
	}

	_ = r.Register(src1)
	_ = r.Register(src2)
	_ = r.Register(src3)

	// Should continue extraction despite error in middle source
	hostnames := r.ExtractAll(context.Background(), nil)

	if len(hostnames) != 2 {
		t.Fatalf("ExtractAll returned %d hostnames, want 2", len(hostnames))
	}

	if hostnames[0].Name != "good1.example.com" {
		t.Errorf("hostnames[0].Name = %q, want %q", hostnames[0].Name, "good1.example.com")
	}
	if hostnames[1].Name != "good2.example.com" {
		t.Errorf("hostnames[1].Name = %q, want %q", hostnames[1].Name, "good2.example.com")
	}
}

func TestRegistry_ExtractAll_Empty(t *testing.T) {
	r := NewRegistry(testLogger())

	// No sources registered
	hostnames := r.ExtractAll(context.Background(), nil)
	if len(hostnames) != 0 {
		t.Errorf("ExtractAll returned %d hostnames, want 0", len(hostnames))
	}
}

func TestRegistry_ExtractFrom(t *testing.T) {
	r := NewRegistry(testLogger())

	src := &mockSource{
		name: "specific",
		hostnames: []Hostname{
			{Name: "app.example.com", Source: "specific"},
		},
	}

	_ = r.Register(src)

	hostnames, err := r.ExtractFrom(context.Background(), "specific", nil)
	if err != nil {
		t.Fatalf("ExtractFrom failed: %v", err)
	}

	if len(hostnames) != 1 {
		t.Fatalf("ExtractFrom returned %d hostnames, want 1", len(hostnames))
	}
}

func TestRegistry_ExtractFrom_NotFound(t *testing.T) {
	r := NewRegistry(testLogger())

	_, err := r.ExtractFrom(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Error("expected error for missing source")
	}

	var notFoundErr *SourceNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Errorf("error type = %T, want *SourceNotFoundError", err)
	}
}
