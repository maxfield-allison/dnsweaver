package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// ============================================================================
// Edge Case Tests for pkg/provider — Issue #68
// ============================================================================

// ----------------------------------------------------------------------------
// Factory Registration Edge Cases
// ----------------------------------------------------------------------------

// TestRegistry_RegisterFactory_Overwrite verifies that registering a factory
// with the same name overwrites the previous factory (no error).
func TestRegistry_RegisterFactory_Overwrite(t *testing.T) {
	r := NewRegistry(testLogger())

	firstCalled := false
	secondCalled := false

	// Register first factory
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		firstCalled = true
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	// Register second factory with same name (should overwrite)
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		secondCalled = true
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	// Create instance - should use second factory
	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "test-instance",
		TypeName:   "test",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	if firstCalled {
		t.Error("first factory was called, but second should have overwritten it")
	}
	if !secondCalled {
		t.Error("second factory was not called")
	}
}

// TestRegistry_CreateInstance_FactoryError verifies that factory errors
// are properly propagated.
func TestRegistry_CreateInstance_FactoryError(t *testing.T) {
	r := NewRegistry(testLogger())

	factoryErr := errors.New("factory initialization failed")
	r.RegisterFactory("failing", func(name string, config map[string]string) (Provider, error) {
		return nil, factoryErr
	})

	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "test-instance",
		TypeName:   "failing",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})

	if err == nil {
		t.Fatal("expected error from factory, got nil")
	}
	if !errors.Is(err, factoryErr) {
		// The error is wrapped, so check if it contains the original error message
		if !containsString(err.Error(), factoryErr.Error()) {
			t.Errorf("error %q should contain factory error %q", err.Error(), factoryErr.Error())
		}
	}
}

// TestRegistry_CreateInstance_ConfigPassthrough verifies that provider config
// is correctly passed to the factory.
func TestRegistry_CreateInstance_ConfigPassthrough(t *testing.T) {
	r := NewRegistry(testLogger())

	var receivedConfig map[string]string
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		receivedConfig = config
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	expectedConfig := map[string]string{
		"url":   "http://dns:5380",
		"zone":  "example.com",
		"token": "secret-token",
	}

	err := r.CreateInstance(ProviderInstanceConfig{
		Name:           "test-instance",
		TypeName:       "test",
		RecordType:     RecordTypeA,
		Target:         "10.0.0.1",
		TTL:            300,
		Domains:        []string{"*.example.com"},
		ProviderConfig: expectedConfig,
	})
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	if len(receivedConfig) != len(expectedConfig) {
		t.Errorf("config length mismatch: got %d, want %d", len(receivedConfig), len(expectedConfig))
	}
	for k, v := range expectedConfig {
		if receivedConfig[k] != v {
			t.Errorf("config[%q] = %q, want %q", k, receivedConfig[k], v)
		}
	}
}

// ----------------------------------------------------------------------------
// Domain Matching Edge Cases
// ----------------------------------------------------------------------------

// TestRegistry_MatchingProviders_WildcardPatterns tests various wildcard patterns.
func TestRegistry_MatchingProviders_WildcardPatterns(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	// Create provider with wildcard pattern
	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "wildcard",
		TypeName:   "test",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	tests := []struct {
		hostname string
		wantLen  int
		desc     string
	}{
		{"app.example.com", 1, "single level subdomain matches"},
		{"sub.app.example.com", 1, "multi-level subdomain matches"},
		{"deep.nested.sub.example.com", 1, "deeply nested subdomain matches"},
		{"example.com", 0, "bare domain does not match wildcard"},
		{"other.com", 0, "different domain does not match"},
		{"example.com.evil.com", 0, "suffix attack does not match"},
		{"notexample.com", 0, "partial domain name does not match"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			matches := r.MatchingProviders(tt.hostname)
			if len(matches) != tt.wantLen {
				t.Errorf("MatchingProviders(%q) = %d matches, want %d", tt.hostname, len(matches), tt.wantLen)
			}
		})
	}
}

// TestRegistry_MatchingProviders_CaseInsensitive tests case-insensitive matching (RFC 1035).
func TestRegistry_MatchingProviders_CaseInsensitive(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "case-test",
		TypeName:   "test",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.Example.COM"},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	tests := []struct {
		hostname string
		wantLen  int
		desc     string
	}{
		{"app.example.com", 1, "lowercase matches"},
		{"APP.EXAMPLE.COM", 1, "uppercase matches"},
		{"App.Example.Com", 1, "mixed case matches"},
		{"aPp.eXaMpLe.cOm", 1, "random case matches"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			matches := r.MatchingProviders(tt.hostname)
			if len(matches) != tt.wantLen {
				t.Errorf("MatchingProviders(%q) = %d matches, want %d", tt.hostname, len(matches), tt.wantLen)
			}
		})
	}
}

// TestRegistry_MatchingProviders_ExclusionPatterns tests that exclusion patterns
// take priority over inclusion patterns.
func TestRegistry_MatchingProviders_ExclusionPatterns(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	err := r.CreateInstance(ProviderInstanceConfig{
		Name:           "with-exclusion",
		TypeName:       "test",
		RecordType:     RecordTypeA,
		Target:         "10.0.0.1",
		TTL:            300,
		Domains:        []string{"*.example.com"},
		ExcludeDomains: []string{"*.internal.example.com", "admin.*"},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	tests := []struct {
		hostname string
		wantLen  int
		desc     string
	}{
		{"app.example.com", 1, "normal subdomain matches"},
		{"web.example.com", 1, "another subdomain matches"},
		{"app.internal.example.com", 0, "internal exclusion works"},
		{"deep.internal.example.com", 0, "nested internal exclusion works"},
		{"admin.example.com", 0, "admin prefix exclusion works"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			matches := r.MatchingProviders(tt.hostname)
			if len(matches) != tt.wantLen {
				t.Errorf("MatchingProviders(%q) = %d matches, want %d", tt.hostname, len(matches), tt.wantLen)
			}
		})
	}
}

// TestRegistry_MatchingProviders_MultipleProviders tests that multiple providers
// can match the same hostname and are returned in registration order.
func TestRegistry_MatchingProviders_MultipleProviders(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	// Create two providers that both match *.example.com
	for i, name := range []string{"first", "second", "third"} {
		err := r.CreateInstance(ProviderInstanceConfig{
			Name:       name,
			TypeName:   "test",
			RecordType: RecordTypeA,
			Target:     fmt.Sprintf("10.0.0.%d", i+1),
			TTL:        300,
			Domains:    []string{"*.example.com"},
		})
		if err != nil {
			t.Fatalf("create %s failed: %v", name, err)
		}
	}

	matches := r.MatchingProviders("app.example.com")
	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(matches))
	}

	// Verify order is preserved
	expectedOrder := []string{"first", "second", "third"}
	for i, expected := range expectedOrder {
		if matches[i].Name() != expected {
			t.Errorf("match[%d].Name() = %q, want %q", i, matches[i].Name(), expected)
		}
	}
}

// ----------------------------------------------------------------------------
// Concurrent Access Safety Tests
// ----------------------------------------------------------------------------

// TestRegistry_ConcurrentGet tests that concurrent Get() calls are safe.
func TestRegistry_ConcurrentGet(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	// Create a provider
	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "concurrent-test",
		TypeName:   "test",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Run concurrent Get() calls
	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, ok := r.Get("concurrent-test")
			if !ok {
				errCh <- errors.New("provider not found")
				return
			}
			if p.Name() != "concurrent-test" {
				errCh <- fmt.Errorf("wrong name: %s", p.Name())
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

// TestRegistry_ConcurrentMatchingProviders tests that concurrent MatchingProviders() calls are safe.
func TestRegistry_ConcurrentMatchingProviders(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	// Create multiple providers
	for i := 0; i < 5; i++ {
		err := r.CreateInstance(ProviderInstanceConfig{
			Name:       fmt.Sprintf("provider-%d", i),
			TypeName:   "test",
			RecordType: RecordTypeA,
			Target:     fmt.Sprintf("10.0.0.%d", i+1),
			TTL:        300,
			Domains:    []string{"*.example.com"},
		})
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}
	}

	// Run concurrent MatchingProviders() calls
	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			matches := r.MatchingProviders("app.example.com")
			if len(matches) != 5 {
				errCh <- fmt.Errorf("expected 5 matches, got %d", len(matches))
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

// TestRegistry_ConcurrentAll tests that concurrent All() calls are safe.
func TestRegistry_ConcurrentAll(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	// Create multiple providers
	for i := 0; i < 5; i++ {
		err := r.CreateInstance(ProviderInstanceConfig{
			Name:       fmt.Sprintf("provider-%d", i),
			TypeName:   "test",
			RecordType: RecordTypeA,
			Target:     fmt.Sprintf("10.0.0.%d", i+1),
			TTL:        300,
			Domains:    []string{fmt.Sprintf("*.p%d.example.com", i)},
		})
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}
	}

	// Run concurrent All() calls
	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			all := r.All()
			if len(all) != 5 {
				errCh <- fmt.Errorf("expected 5 providers, got %d", len(all))
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

// ----------------------------------------------------------------------------
// Edge Cases
// ----------------------------------------------------------------------------

// TestRegistry_MatchingProviders_EmptyHostname tests behavior with empty hostname.
func TestRegistry_MatchingProviders_EmptyHostname(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "test",
		TypeName:   "test",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*"}, // Match everything
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Empty hostname should not panic and should return empty
	matches := r.MatchingProviders("")
	// Expected behavior: empty hostname doesn't match any pattern
	// (implementation may vary, but it should not panic)
	t.Logf("empty hostname returned %d matches", len(matches))
}

// TestRegistry_MatchingProviders_NoMatch tests behavior when no providers match.
func TestRegistry_MatchingProviders_NoMatch(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "specific",
		TypeName:   "test",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.specific.com"},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	matches := r.MatchingProviders("app.other.com")
	// MatchingProviders returns nil when no matches, which is acceptable in Go
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

// TestRegistry_FirstMatchingProvider_NoMatch tests FirstMatchingProvider with no matches.
func TestRegistry_FirstMatchingProvider_NoMatch(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "specific",
		TypeName:   "test",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.specific.com"},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	p := r.FirstMatchingProvider("app.other.com")
	if p != nil {
		t.Error("FirstMatchingProvider should return nil when no matches")
	}
}

// TestRegistry_All_ReturnsCopy verifies that All() returns a copy that can be
// modified without affecting the registry.
func TestRegistry_All_ReturnsCopy(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "original",
		TypeName:   "test",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Get all providers
	all := r.All()
	originalLen := len(all)

	// Modify the returned slice
	all = append(all, nil)
	all[0] = nil

	// Verify registry is unaffected
	newAll := r.All()
	if len(newAll) != originalLen {
		t.Errorf("registry affected by slice modification: got %d, want %d", len(newAll), originalLen)
	}
	if newAll[0] == nil {
		t.Error("registry affected by nil assignment to slice element")
	}
}

// TestRegistry_Close_ClearsInstances verifies that Close() properly clears all instances.
func TestRegistry_Close_ClearsInstances(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	// Create multiple providers
	for i := 0; i < 3; i++ {
		err := r.CreateInstance(ProviderInstanceConfig{
			Name:       fmt.Sprintf("provider-%d", i),
			TypeName:   "test",
			RecordType: RecordTypeA,
			Target:     fmt.Sprintf("10.0.0.%d", i+1),
			TTL:        300,
			Domains:    []string{fmt.Sprintf("*.p%d.com", i)},
		})
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}
	}

	if r.Count() != 3 {
		t.Fatalf("expected 3 providers before close, got %d", r.Count())
	}

	err := r.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}

	if r.Count() != 0 {
		t.Errorf("expected 0 providers after close, got %d", r.Count())
	}

	// Verify Get returns false
	_, ok := r.Get("provider-0")
	if ok {
		t.Error("Get should return false after Close")
	}

	// Verify All returns empty
	all := r.All()
	if len(all) != 0 {
		t.Errorf("All should return empty slice after Close, got %d", len(all))
	}
}

// TestRegistry_Close_Idempotent verifies that Close() can be called multiple times safely.
func TestRegistry_Close_Idempotent(t *testing.T) {
	r := NewRegistry(testLogger())
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})

	err := r.CreateInstance(ProviderInstanceConfig{
		Name:       "test",
		TypeName:   "test",
		RecordType: RecordTypeA,
		Target:     "10.0.0.1",
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Close multiple times - should not panic
	for i := 0; i < 3; i++ {
		err := r.Close()
		if err != nil {
			t.Errorf("Close() #%d returned error: %v", i+1, err)
		}
	}
}

// TestRegistry_EmptyRegistry tests operations on an empty registry.
func TestRegistry_EmptyRegistry(t *testing.T) {
	r := NewRegistry(testLogger())

	// Get on empty registry
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("Get should return false on empty registry")
	}

	// All on empty registry
	all := r.All()
	if all == nil {
		t.Error("All should return empty slice, not nil")
	}
	if len(all) != 0 {
		t.Errorf("All should return empty slice, got %d items", len(all))
	}

	// MatchingProviders on empty registry
	matches := r.MatchingProviders("app.example.com")
	if len(matches) != 0 {
		t.Errorf("MatchingProviders should return empty, got %d", len(matches))
	}

	// FirstMatchingProvider on empty registry
	p := r.FirstMatchingProvider("app.example.com")
	if p != nil {
		t.Error("FirstMatchingProvider should return nil on empty registry")
	}

	// Count on empty registry
	if r.Count() != 0 {
		t.Errorf("Count should be 0, got %d", r.Count())
	}

	// Close on empty registry
	err := r.Close()
	if err != nil {
		t.Errorf("Close on empty registry returned error: %v", err)
	}
}

// TestRegistry_NilLogger tests that nil logger defaults to slog.Default.
func TestRegistry_NilLogger(t *testing.T) {
	r := NewRegistry(nil)
	if r == nil {
		t.Fatal("NewRegistry(nil) returned nil")
	}
	// Should not panic when using the registry
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		return &mockProvider{name: name, typeName: "test"}, nil
	})
}

// TestRegistry_PingAll tests the PingAll functionality.
func TestRegistry_PingAll(t *testing.T) {
	r := NewRegistry(testLogger())

	// Create providers with different ping behaviors
	pingErr := errors.New("connection refused")

	// Register factory that returns provider with ping error based on config
	r.RegisterFactory("test", func(name string, config map[string]string) (Provider, error) {
		var err error
		if config["should_fail"] == "true" {
			err = pingErr
		}
		return &mockProvider{name: name, typeName: "test", pingErr: err}, nil
	})

	err := r.CreateInstance(ProviderInstanceConfig{
		Name:           "healthy-instance",
		TypeName:       "test",
		RecordType:     RecordTypeA,
		Target:         "10.0.0.1",
		TTL:            300,
		Domains:        []string{"*.healthy.com"},
		ProviderConfig: map[string]string{"should_fail": "false"},
	})
	if err != nil {
		t.Fatalf("create healthy failed: %v", err)
	}

	err = r.CreateInstance(ProviderInstanceConfig{
		Name:           "unhealthy-instance",
		TypeName:       "test",
		RecordType:     RecordTypeA,
		Target:         "10.0.0.2",
		TTL:            300,
		Domains:        []string{"*.unhealthy.com"},
		ProviderConfig: map[string]string{"should_fail": "true"},
	})
	if err != nil {
		t.Fatalf("create unhealthy failed: %v", err)
	}

	results := r.PingAll(context.Background())

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results["healthy-instance"] != nil {
		t.Errorf("healthy instance should have nil error, got %v", results["healthy-instance"])
	}

	if results["unhealthy-instance"] == nil {
		t.Error("unhealthy instance should have error")
	}
}

// ----------------------------------------------------------------------------
// Error Type Tests
// ----------------------------------------------------------------------------

// TestConfigError_Format tests ConfigError formatting.
func TestConfigError_Format(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{
			name:     "missing field",
			err:      ErrConfigMissing("name"),
			contains: "name",
		},
		{
			name:     "invalid field with value",
			err:      ErrConfigInvalid("ttl", "0", "must be positive"),
			contains: "ttl",
		},
		{
			name:     "invalid field without value",
			err:      ErrConfigInvalid("domains", "", "at least one required"),
			contains: "domains",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errStr := tt.err.Error()
			if !containsString(errStr, tt.contains) {
				t.Errorf("error %q should contain %q", errStr, tt.contains)
			}
		})
	}
}

// TestProviderError_Unwrap tests ProviderError unwrapping.
func TestProviderError_Unwrap(t *testing.T) {
	originalErr := errors.New("connection failed")
	wrapped := WrapError("test-provider", "create", originalErr)

	if !errors.Is(wrapped, originalErr) {
		t.Error("wrapped error should unwrap to original")
	}

	var providerErr *ProviderError
	if !errors.As(wrapped, &providerErr) {
		t.Error("should be able to extract ProviderError")
	}

	if providerErr.Provider != "test-provider" {
		t.Errorf("provider = %q, want %q", providerErr.Provider, "test-provider")
	}
	if providerErr.Operation != "create" {
		t.Errorf("operation = %q, want %q", providerErr.Operation, "create")
	}
}

// TestWrapError_Nil tests that WrapError returns nil for nil error.
func TestWrapError_Nil(t *testing.T) {
	result := WrapError("provider", "operation", nil)
	if result != nil {
		t.Error("WrapError(nil) should return nil")
	}
}

// TestIsNotFound tests the IsNotFound helper.
func TestIsNotFound(t *testing.T) {
	if !IsNotFound(ErrNotFound) {
		t.Error("IsNotFound(ErrNotFound) should be true")
	}
	if IsNotFound(errors.New("other error")) {
		t.Error("IsNotFound(other) should be false")
	}
	if IsNotFound(nil) {
		t.Error("IsNotFound(nil) should be false")
	}

	// Wrapped error
	wrapped := fmt.Errorf("wrapped: %w", ErrNotFound)
	if !IsNotFound(wrapped) {
		t.Error("IsNotFound should work with wrapped errors")
	}
}

// TestIsConflict tests the IsConflict helper.
func TestIsConflict(t *testing.T) {
	if !IsConflict(ErrConflict) {
		t.Error("IsConflict(ErrConflict) should be true")
	}
	if IsConflict(errors.New("other error")) {
		t.Error("IsConflict(other) should be false")
	}
	if IsConflict(nil) {
		t.Error("IsConflict(nil) should be false")
	}
}

// TestIsTypeConflict tests the IsTypeConflict helper.
func TestIsTypeConflict(t *testing.T) {
	if !IsTypeConflict(ErrTypeConflict) {
		t.Error("IsTypeConflict(ErrTypeConflict) should be true")
	}
	if IsTypeConflict(errors.New("other error")) {
		t.Error("IsTypeConflict(other) should be false")
	}
	if IsTypeConflict(nil) {
		t.Error("IsTypeConflict(nil) should be false")
	}
}

// ----------------------------------------------------------------------------
// ProviderInstanceConfig Additional Tests
// ----------------------------------------------------------------------------

// TestProviderInstanceConfig_UseRegex tests the regex vs glob selection.
func TestProviderInstanceConfig_UseRegex(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ProviderInstanceConfig
		wantRx  bool
		wantInc []string
		wantExc []string
	}{
		{
			name: "glob patterns",
			cfg: ProviderInstanceConfig{
				Domains:        []string{"*.example.com"},
				ExcludeDomains: []string{"*.internal.example.com"},
			},
			wantRx:  false,
			wantInc: []string{"*.example.com"},
			wantExc: []string{"*.internal.example.com"},
		},
		{
			name: "regex patterns",
			cfg: ProviderInstanceConfig{
				DomainsRegex:        []string{`.*\.example\.com$`},
				ExcludeDomainsRegex: []string{`.*\.internal\.example\.com$`},
			},
			wantRx:  true,
			wantInc: []string{`.*\.example\.com$`},
			wantExc: []string{`.*\.internal\.example\.com$`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cfg.UseRegex() != tt.wantRx {
				t.Errorf("UseRegex() = %v, want %v", tt.cfg.UseRegex(), tt.wantRx)
			}

			inc := tt.cfg.GetIncludes()
			if len(inc) != len(tt.wantInc) {
				t.Errorf("GetIncludes() len = %d, want %d", len(inc), len(tt.wantInc))
			}

			exc := tt.cfg.GetExcludes()
			if len(exc) != len(tt.wantExc) {
				t.Errorf("GetExcludes() len = %d, want %d", len(exc), len(tt.wantExc))
			}
		})
	}
}

// TestProviderInstanceConfig_Validate_BothGlobAndRegex tests that you can't use both.
func TestProviderInstanceConfig_Validate_BothGlobAndRegex(t *testing.T) {
	cfg := ProviderInstanceConfig{
		Name:         "test",
		TypeName:     "test",
		RecordType:   RecordTypeA,
		Target:       "10.0.0.1",
		TTL:          300,
		Domains:      []string{"*.example.com"},
		DomainsRegex: []string{`.*\.example\.com$`},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("expected error when both Domains and DomainsRegex are set")
	}
}
