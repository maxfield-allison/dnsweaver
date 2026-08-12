package reconciler

import (
	"context"
	"log/slog"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
)

// newCoexistenceReconciler wires a single mock instance holding the supplied
// pre-existing records, returning the reconciler and a warm cache.
func newCoexistenceReconciler(t *testing.T, recordType provider.RecordType, target string, existing ...provider.Record) (*Reconciler, *recordCache, *testMockProvider) {
	t.Helper()

	mock := newTestMockProvider("test-dns")
	for _, rec := range existing {
		mock.AddRecord(rec)
	}

	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(cfg provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	if err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: recordType,
		Target:     target,
		TTL:        300,
		Domains:    []string{"*.example.com"},
	}); err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	r := &Reconciler{
		providers:      providers,
		config:         DefaultConfig(),
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	return r, newRecordCache(context.Background(), providers, logger), mock
}

func TestTypesConflict(t *testing.T) {
	tests := []struct {
		name     string
		desired  provider.RecordType
		existing provider.RecordType
		want     bool
	}{
		{"A alongside AAAA", provider.RecordTypeA, provider.RecordTypeAAAA, false},
		{"AAAA alongside A", provider.RecordTypeAAAA, provider.RecordTypeA, false},
		{"A alongside companion HTTPS", provider.RecordTypeA, provider.RecordTypeHTTPS, false},
		{"AAAA alongside companion HTTPS", provider.RecordTypeAAAA, provider.RecordTypeHTTPS, false},
		{"SRV alongside A", provider.RecordTypeSRV, provider.RecordTypeA, false},
		{"HTTPS alongside A", provider.RecordTypeHTTPS, provider.RecordTypeA, false},
		{"A blocked by existing CNAME", provider.RecordTypeA, provider.RecordTypeCNAME, true},
		{"AAAA blocked by existing CNAME", provider.RecordTypeAAAA, provider.RecordTypeCNAME, true},
		{"HTTPS blocked by existing CNAME", provider.RecordTypeHTTPS, provider.RecordTypeCNAME, true},
		{"CNAME blocked by existing A", provider.RecordTypeCNAME, provider.RecordTypeA, true},
		{"CNAME blocked by existing HTTPS", provider.RecordTypeCNAME, provider.RecordTypeHTTPS, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := typesConflict(tt.desired, tt.existing); got != tt.want {
				t.Errorf("typesConflict(%s, %s) = %v, want %v", tt.desired, tt.existing, got, tt.want)
			}
		})
	}
}

// The Technitium provider creates a companion HTTPS record next to every A/AAAA
// record it writes. That record must not block later reconciles of the address
// record it accompanies — issue #165, where the stale target was never updated
// and a WARN was logged on every interval.
func TestEnsureRecord_CompanionHTTPSDoesNotBlockUpdate(t *testing.T) {
	r, cache, mock := newCoexistenceReconciler(t, provider.RecordTypeA, "10.0.0.1",
		provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.99", TTL: 300},
		provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeHTTPS, Target: ".", TTL: 300},
	)

	actions := r.ensureRecord(context.Background(), &source.Hostname{Name: "app.example.com", Source: "test"}, cache)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionUpdate || actions[0].Status != StatusSuccess {
		t.Fatalf("expected a successful update, got %v/%v (%s)", actions[0].Type, actions[0].Status, actions[0].Error)
	}

	for _, deleted := range mock.GetDeleted() {
		if deleted.Type == provider.RecordTypeHTTPS {
			t.Error("companion HTTPS record must not be deleted while updating the A record")
		}
	}
}

// Dual-stack: an AAAA instance and an A instance share every hostname, and
// neither may treat the other's record as a conflict.
func TestEnsureRecord_DualStackCoexists(t *testing.T) {
	r, cache, _ := newCoexistenceReconciler(t, provider.RecordTypeAAAA, "2001:db8::1",
		provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300},
		provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeHTTPS, Target: ".", TTL: 300},
	)

	actions := r.ensureRecord(context.Background(), &source.Hostname{Name: "app.example.com", Source: "test"}, cache)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionCreate || actions[0].Status != StatusSuccess {
		t.Fatalf("expected the AAAA record to be created, got %v/%v (%s)", actions[0].Type, actions[0].Status, actions[0].Error)
	}
}

// An existing CNAME still blocks address records: that conflict is real.
func TestEnsureRecord_ExistingCNAMEStillConflicts(t *testing.T) {
	r, cache, _ := newCoexistenceReconciler(t, provider.RecordTypeA, "10.0.0.1",
		provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeCNAME, Target: "proxy.example.com", TTL: 300},
	)

	actions := r.ensureRecord(context.Background(), &source.Hostname{Name: "app.example.com", Source: "test"}, cache)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionSkip || !containsHelper(actions[0].Error, "conflict") {
		t.Fatalf("expected a conflict skip, got %v (%s)", actions[0].Type, actions[0].Error)
	}
}

// A desired CNAME is exclusive, so any pre-existing record blocks it — including
// an HTTPS record left behind by an older dnsweaver release.
func TestEnsureRecord_DesiredCNAMEConflictsWithHTTPS(t *testing.T) {
	r, cache, _ := newCoexistenceReconciler(t, provider.RecordTypeCNAME, "proxy.example.com",
		provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeHTTPS, Target: ".", TTL: 300},
	)

	actions := r.ensureRecord(context.Background(), &source.Hostname{Name: "app.example.com", Source: "test"}, cache)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionSkip || !containsHelper(actions[0].Error, "conflict") {
		t.Fatalf("expected a conflict skip, got %v (%s)", actions[0].Type, actions[0].Error)
	}
}

// A CNAME whose target is the hostname itself is a resolution loop; it must be
// skipped rather than retried against the provider every interval.
func TestEnsureRecord_SkipsSelfReferentialCNAME(t *testing.T) {
	r, cache, mock := newCoexistenceReconciler(t, provider.RecordTypeCNAME, "proxy.example.com")

	actions := r.ensureRecord(context.Background(), &source.Hostname{Name: "proxy.example.com", Source: "test"}, cache)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionSkip || actions[0].Error != errSelfReferentialCNAME {
		t.Fatalf("expected a self-referential skip, got %v (%s)", actions[0].Type, actions[0].Error)
	}
	if len(mock.GetCreated()) != 0 {
		t.Error("no record should be created for a self-referential CNAME")
	}
}

func TestIsSelfReferential(t *testing.T) {
	tests := []struct {
		name       string
		hostname   string
		target     string
		recordType provider.RecordType
		want       bool
	}{
		{"identical CNAME", "proxy.example.com", "proxy.example.com", provider.RecordTypeCNAME, true},
		{"case and trailing dot", "Proxy.Example.com", "proxy.example.com.", provider.RecordTypeCNAME, true},
		{"different target", "app.example.com", "proxy.example.com", provider.RecordTypeCNAME, false},
		{"A record with matching target is not a loop", "proxy.example.com", "proxy.example.com", provider.RecordTypeA, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSelfReferential(tt.hostname, tt.target, tt.recordType); got != tt.want {
				t.Errorf("isSelfReferential(%q, %q, %s) = %v, want %v", tt.hostname, tt.target, tt.recordType, got, tt.want)
			}
		})
	}
}

// The self-referential warning must not repeat on every interval — the whole
// point of the skip is to stop the per-cycle noise (issue #165).
func TestWarnSelfReferentialOnce(t *testing.T) {
	var warnings int
	r := &Reconciler{
		logger: slog.New(&countingHandler{level: slog.LevelWarn, count: &warnings}),
		config: DefaultConfig(),
	}

	for range 5 {
		r.warnSelfReferentialOnce("proxy.example.com", "test-dns", "proxy.example.com")
	}
	if warnings != 1 {
		t.Errorf("expected 1 warn-level log for a repeated hostname, got %d", warnings)
	}

	r.warnSelfReferentialOnce("other.example.com", "test-dns", "other.example.com")
	if warnings != 2 {
		t.Errorf("expected a warning for a second hostname, got %d total", warnings)
	}
}

// countingHandler counts records at or above the configured level.
type countingHandler struct {
	level slog.Level
	count *int
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *countingHandler) Handle(_ context.Context, rec slog.Record) error {
	if rec.Level >= h.level {
		*h.count++
	}
	return nil
}
func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(string) slog.Handler      { return h }
