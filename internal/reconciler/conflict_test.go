package reconciler

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
)

// newConflictReconciler wires one mock instance in the given mode holding the
// supplied pre-existing records, returning the reconciler, a warm cache and the
// mock. cfg replaces the default reconciler config.
func newConflictReconciler(t *testing.T, logger *slog.Logger, mode provider.OperationalMode, recordType provider.RecordType, target string, cfg Config, existing ...provider.Record) (*Reconciler, *recordCache, *testMockProvider) {
	t.Helper()

	mock := newTestMockProvider("test-dns")
	for _, rec := range existing {
		mock.AddRecord(rec)
	}

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("mock", func(_ provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	if err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       "test-dns",
		TypeName:   "mock",
		RecordType: recordType,
		Target:     target,
		TTL:        300,
		Mode:       mode,
		Domains:    []string{"*.example.com"},
	}); err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	r := &Reconciler{
		providers:      providers,
		config:         cfg,
		logger:         logger,
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()

	return r, newRecordCache(context.Background(), providers, logger), mock
}

// A hostname that already carries a record of an incompatible type (a CNAME
// where an A is wanted, or the reverse) used to be skipped forever. It is now
// replaced when the instance may delete it: authoritative mode, ADOPT_EXISTING,
// or an ownership record showing the conflicting record is dnsweaver's own.
// Additive mode never deletes, and the skip warns once (issue #171).
func TestEnsureRecord_TypeConflictPolicy(t *testing.T) {
	existingCNAME := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeCNAME, Target: "proxy.example.com", TTL: 300}
	existingA := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.99", TTL: 300}
	existingHTTPS := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeHTTPS, Target: ".", TTL: 300}
	ownership := provider.Record{Hostname: "_dnsweaver.app.example.com", Type: provider.RecordTypeTXT, Target: "heritage=dnsweaver"}

	type step struct {
		typ    ActionType
		status ActionStatus
	}
	replaced := []step{{ActionDelete, StatusSuccess}, {ActionCreate, StatusSuccess}}
	skipped := []step{{ActionSkip, StatusSkipped}}

	tests := []struct {
		name        string
		mode        provider.OperationalMode
		adopt       bool
		dryRun      bool
		desiredType provider.RecordType
		target      string
		existing    []provider.Record
		deleteErr   func(provider.Record) error
		want        []step
		wantErr     string
		wantDeleted int // records the mock actually removed
		wantCreated int // non-TXT records the mock actually created
	}{
		{
			name:        "unowned CNAME, managed, adopt off: skipped",
			mode:        provider.ModeManaged,
			desiredType: provider.RecordTypeA,
			target:      "10.0.0.1",
			existing:    []provider.Record{existingCNAME},
			want:        skipped,
			wantErr:     "conflict",
		},
		{
			name:        "unowned CNAME, managed, adopt on: replaced",
			mode:        provider.ModeManaged,
			adopt:       true,
			desiredType: provider.RecordTypeA,
			target:      "10.0.0.1",
			existing:    []provider.Record{existingCNAME},
			want:        replaced,
			wantDeleted: 1,
			wantCreated: 1,
		},
		{
			name:        "unowned CNAME, authoritative: replaced",
			mode:        provider.ModeAuthoritative,
			desiredType: provider.RecordTypeA,
			target:      "10.0.0.1",
			existing:    []provider.Record{existingCNAME},
			want:        replaced,
			wantDeleted: 1,
			wantCreated: 1,
		},
		{
			name:        "owned CNAME, managed, adopt off: replaced",
			mode:        provider.ModeManaged,
			desiredType: provider.RecordTypeA,
			target:      "10.0.0.1",
			existing:    []provider.Record{existingCNAME, ownership},
			want:        replaced,
			wantDeleted: 1,
			wantCreated: 1,
		},
		{
			name:        "additive never deletes, even with adopt on",
			mode:        provider.ModeAdditive,
			adopt:       true,
			desiredType: provider.RecordTypeA,
			target:      "10.0.0.1",
			existing:    []provider.Record{existingCNAME, ownership},
			want:        skipped,
			wantErr:     "conflict",
		},
		{
			name:        "dry-run reports the replacement without touching the provider",
			mode:        provider.ModeAuthoritative,
			dryRun:      true,
			desiredType: provider.RecordTypeA,
			target:      "10.0.0.1",
			existing:    []provider.Record{existingCNAME},
			want:        replaced,
		},
		{
			name:        "desired CNAME over existing A, adopt off: skipped",
			mode:        provider.ModeManaged,
			desiredType: provider.RecordTypeCNAME,
			target:      "proxy.example.com",
			existing:    []provider.Record{existingA},
			want:        skipped,
			wantErr:     "conflict",
		},
		{
			name:        "desired CNAME over existing A, adopt on: replaced",
			mode:        provider.ModeManaged,
			adopt:       true,
			desiredType: provider.RecordTypeCNAME,
			target:      "proxy.example.com",
			existing:    []provider.Record{existingA},
			want:        replaced,
			wantDeleted: 1,
			wantCreated: 1,
		},
		{
			name:        "desired CNAME over owned A and companion HTTPS: both replaced",
			mode:        provider.ModeManaged,
			desiredType: provider.RecordTypeCNAME,
			target:      "proxy.example.com",
			existing:    []provider.Record{existingA, existingHTTPS, ownership},
			want:        []step{{ActionDelete, StatusSuccess}, {ActionDelete, StatusSuccess}, {ActionCreate, StatusSuccess}},
			wantDeleted: 2,
			wantCreated: 1,
		},
		{
			name:        "delete failure fails the action and creates nothing",
			mode:        provider.ModeManaged,
			adopt:       true,
			desiredType: provider.RecordTypeA,
			target:      "10.0.0.1",
			existing:    []provider.Record{existingCNAME},
			deleteErr:   func(provider.Record) error { return errors.New("api down") },
			want:        []step{{ActionCreate, StatusFailed}},
			wantErr:     "api down",
		},
		{
			name:        "a failure part-way through keeps the deletes that succeeded and stops",
			mode:        provider.ModeAuthoritative,
			desiredType: provider.RecordTypeCNAME,
			target:      "proxy.example.com",
			existing:    []provider.Record{existingA, existingHTTPS},
			deleteErr: func(rec provider.Record) error {
				if rec.Type == provider.RecordTypeHTTPS {
					return errors.New("api down")
				}
				return nil
			},
			want:        []step{{ActionDelete, StatusSuccess}, {ActionCreate, StatusFailed}},
			wantErr:     "api down",
			wantDeleted: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.AdoptExisting = tt.adopt
			cfg.DryRun = tt.dryRun
			r, cache, mock := newConflictReconciler(t, quietLogger(), tt.mode, tt.desiredType, tt.target, cfg, tt.existing...)
			if tt.deleteErr != nil {
				mock.deleteFn = func(_ context.Context, rec provider.Record) error { return tt.deleteErr(rec) }
			}

			actions := r.ensureRecord(context.Background(), &source.Hostname{Name: "app.example.com", Source: "test"}, cache)

			if len(actions) != len(tt.want) {
				t.Fatalf("expected %d action(s), got %d: %v", len(tt.want), len(actions), actions)
			}
			for i, want := range tt.want {
				if actions[i].Type != want.typ || actions[i].Status != want.status {
					t.Errorf("action %d: expected %s/%s, got %s/%s (%s)", i, want.typ, want.status, actions[i].Type, actions[i].Status, actions[i].Error)
				}
			}
			last := actions[len(actions)-1]
			if tt.wantErr != "" && !containsHelper(last.Error, tt.wantErr) {
				t.Errorf("expected final action error to mention %q, got %q", tt.wantErr, last.Error)
			}
			if tt.wantErr == "" && last.Error != "" {
				t.Errorf("expected no error on the final action, got %q", last.Error)
			}

			if got := len(mock.GetDeleted()); got != tt.wantDeleted {
				t.Errorf("expected %d record(s) deleted, got %d: %v", tt.wantDeleted, got, mock.GetDeleted())
			}
			created := mock.GetCreatedDNSRecords()
			if len(created) != tt.wantCreated {
				t.Fatalf("expected %d record(s) created, got %d: %v", tt.wantCreated, len(created), created)
			}
			for _, rec := range created {
				if rec.Type != tt.desiredType || rec.Target != tt.target {
					t.Errorf("expected created record %s -> %s, got %s -> %s", tt.desiredType, tt.target, rec.Type, rec.Target)
				}
			}
		})
	}
}

// A conflict dnsweaver may not resolve is a standing condition, so the warning
// is logged once per provider/hostname and at debug level afterwards.
func TestEnsureRecord_TypeConflictWarnsOnce(t *testing.T) {
	var warnings int
	logger := slog.New(&countingHandler{level: slog.LevelWarn, count: &warnings})
	r, cache, _ := newConflictReconciler(t, logger, provider.ModeManaged, provider.RecordTypeA, "10.0.0.1", DefaultConfig(),
		provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeCNAME, Target: "proxy.example.com", TTL: 300},
	)

	for range 3 {
		actions := r.ensureRecord(context.Background(), &source.Hostname{Name: "app.example.com", Source: "test"}, cache)
		if len(actions) != 1 || actions[0].Type != ActionSkip {
			t.Fatalf("expected a single skip action, got %v", actions)
		}
	}
	if warnings != 1 {
		t.Errorf("expected 1 warn-level log across repeated passes, got %d", warnings)
	}

	r.ensureRecord(context.Background(), &source.Hostname{Name: "other.example.com", Source: "test"}, cache)
	if warnings != 1 {
		t.Errorf("a hostname with no conflict must not warn, got %d total", warnings)
	}
}
