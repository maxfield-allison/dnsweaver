package provider

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

// mockProvider implements Provider for testing.
type mockProvider struct {
	name     string
	typeName string
	pingErr  error
	records  []Record
}

func (m *mockProvider) Name() string                           { return m.name }
func (m *mockProvider) Type() string                           { return m.typeName }
func (m *mockProvider) Ping(ctx context.Context) error         { return m.pingErr }
func (m *mockProvider) List(ctx context.Context) ([]Record, error) { return m.records, nil }
func (m *mockProvider) Create(ctx context.Context, r Record) error  { return nil }
func (m *mockProvider) Delete(ctx context.Context, r Record) error  { return nil }

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRegistry_RegisterFactory(t *testing.T) {
	r := NewRegistry(testLogger())

	called := false
	factory := func(name string, config map[string]string) (Provider, error) {
		called = true
		return &mockProvider{name: name, typeName: "test"}, nil
	}

	r.RegisterFactory("test", factory)

	err := r.CreateInstance(ProviderInstanceConfig{
		Name:           "test-instance",
		TypeName:       "test",
		RecordType:     RecordTypeA,
		Target:         "10.0.0.1",
		TTL:            300,
		Domains:        []string{"*.example.com"},
		ProviderConfig: map[string]string{},
	})
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	if !called {
		t.Error("factory was not called")
	}
}

func TestRegistry_CreateInstance_UnknownType(t *testing.T) {
	r := NewRegistry(testLogger())

	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "test",
		TypeName:   "unknown",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})
	if err == nil {
		t.Error("expected error for unknown type")
	}
}

func TestRegistry_CreateInstance_ValidationError(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	tests := []struct {
		name   string
		cfg    ProviderInstanceConfig
		errMsg string
	}{
		{
			name:   "missing name",
			cfg:    ProviderInstanceConfig{TypeName: "test", RecordType: RecordTypeA, Target: "10.0.0.1", TTL: 300, Domains: []string{"*"}},
			errMsg: "name",
		},
		{
			name:   "missing type",
			cfg:    ProviderInstanceConfig{Name: "test", RecordType: RecordTypeA, Target: "10.0.0.1", TTL: 300, Domains: []string{"*"}},
			errMsg: "type",
		},
		{
			name:   "missing domains",
			cfg:    ProviderInstanceConfig{Name: "test", TypeName: "test", RecordType: RecordTypeA, Target: "10.0.0.1", TTL: 300},
			errMsg: "domains",
		},
		{
			name:   "missing target",
			cfg:    ProviderInstanceConfig{Name: "test", TypeName: "test", RecordType: RecordTypeA, TTL: 300, Domains: []string{"*"}},
			errMsg: "target",
		},
		{
			name:   "invalid TTL",
			cfg:    ProviderInstanceConfig{Name: "test", TypeName: "test", RecordType: RecordTypeA, Target: "10.0.0.1", TTL: 0, Domains: []string{"*"}},
			errMsg: "ttl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := r.CreateInstance(tt.cfg)
			if err == nil {
				t.Errorf("expected error containing %q", tt.errMsg)
			}
		})
	}
}

func TestRegistry_CreateInstance_DuplicateName(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	cfg := ProviderInstanceConfig{
		Name:       "dupe",
		TypeName:   "test",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	}

	if err := r.CreateInstance(cfg); err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	if err := r.CreateInstance(cfg); err == nil {
		t.Error("expected error for duplicate name")
	}
}

func TestRegistry_MatchingProviders(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	// Create two providers with different domain patterns
	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "internal",
		TypeName:   "test",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.internal.example.com"},
	})
	if err != nil {
		t.Fatalf("create internal failed: %v", err)
	}

	err = r.CreateInstance(ProviderInstanceConfig{
		Name:           "external",
		TypeName:       "test",
		RecordType:     RecordTypeCNAME,
		Target:         "example.com",
		TTL:            300,
		Domains:        []string{"*.example.com"},
		ExcludeDomains: []string{"*.internal.example.com"},
	})
	if err != nil {
		t.Fatalf("create external failed: %v", err)
	}

	tests := []struct {
		hostname string
		wantLen  int
		wantName string // first match name
	}{
		{"app.internal.example.com", 1, "internal"},
		{"app.example.com", 1, "external"},
		{"unrelated.com", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			matches := r.MatchingProviders(tt.hostname)
			if len(matches) != tt.wantLen {
				t.Errorf("MatchingProviders(%q) = %d matches, want %d", tt.hostname, len(matches), tt.wantLen)
			}
			if tt.wantLen > 0 && matches[0].Name() != tt.wantName {
				t.Errorf("first match name = %q, want %q", matches[0].Name(), tt.wantName)
			}
		})
	}
}

func TestRegistry_FirstMatchingProvider(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	// Create provider
	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "main",
		TypeName:   "test",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Should match
	p := r.FirstMatchingProvider("app.example.com")
	if p == nil {
		t.Error("expected to find matching provider")
	}
	if p.Name() != "main" {
		t.Errorf("name = %q, want %q", p.Name(), "main")
	}

	// Should not match
	p = r.FirstMatchingProvider("other.com")
	if p != nil {
		t.Error("expected no matching provider")
	}
}

func TestRegistry_All_PreservesOrder(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	names := []string{"first", "second", "third"}
	for _, name := range names {
		err := r.CreateInstance(ProviderInstanceConfig{
			Name:       name,
			TypeName:   "test",
			RecordType: RecordTypeA,
			Target:     "10.0.0.1",
			TTL:        300,
			Domains:    []string{"*." + name + ".com"},
		})
		if err != nil {
			t.Fatalf("create %s failed: %v", name, err)
		}
	}

	all := r.All()
	if len(all) != 3 {
		t.Fatalf("All() returned %d instances, want 3", len(all))
	}

	for i, name := range names {
		if all[i].Name() != name {
			t.Errorf("All()[%d].Name() = %q, want %q", i, all[i].Name(), name)
		}
	}
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "findme",
		TypeName:   "test",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	p, ok := r.Get("findme")
	if !ok {
		t.Error("expected to find instance")
	}
	if p.Name() != "findme" {
		t.Errorf("name = %q, want %q", p.Name(), "findme")
	}

	_, ok = r.Get("notfound")
	if ok {
		t.Error("expected not to find non-existent instance")
	}
}

func TestRegistry_Count(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	if r.Count() != 0 {
		t.Errorf("Count() = %d, want 0", r.Count())
	}

	r.CreateInstance(ProviderInstanceConfig{
		Name:       "one",
		TypeName:   "test",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	if r.Count() != 1 {
		t.Errorf("Count() = %d, want 1", r.Count())
	}
}

func TestRegistry_Close(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	r.CreateInstance(ProviderInstanceConfig{
		Name:       "one",
		TypeName:   "test",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	err := r.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}

	if r.Count() != 0 {
		t.Errorf("Count() after Close() = %d, want 0", r.Count())
	}
}
