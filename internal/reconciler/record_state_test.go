package reconciler

import (
	"context"
	"strconv"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
	dnsweaversource "github.com/maxfield-allison/dnsweaver/sources/dnsweaver"
)

// =============================================================================
// Provider-specific record state (issue #170)
//
// These tests cover the reconciler's handling of a provider that reports state
// the generic comparison cannot see, using a mock with the shape of the
// Cloudflare proxied flag: List reports it in Metadata["proxied"], writes
// resolve it from the per-record hint or the instance default, and a proxied
// record is stored with TTL 1. The mock implements provider.RecordComparer and
// provider.Updater so the in-place update path is exercised end to end.
// =============================================================================

const stateTestTarget = "203.0.113.10"

type stateUpdateCall struct {
	existing, desired provider.Record
}

// stateMockProvider wraps testMockProvider with Cloudflare-like proxied state.
type stateMockProvider struct {
	*testMockProvider
	proxied bool // instance default

	updated []stateUpdateCall
}

func newStateMockProvider(name string, proxied bool) *stateMockProvider {
	return &stateMockProvider{testMockProvider: newTestMockProvider(name), proxied: proxied}
}

func (m *stateMockProvider) resolveProxied(r provider.Record) bool {
	if v, ok := r.Metadata["proxied"]; ok {
		return v == "true"
	}
	return m.proxied
}

// stored returns the record as the mock lists it after a write: the proxied
// flag resolved into Metadata and, when proxied, TTL forced to 1.
func (m *stateMockProvider) stored(r provider.Record) provider.Record {
	if r.Type == provider.RecordTypeTXT {
		return r
	}
	proxied := m.resolveProxied(r)
	r.Metadata = map[string]string{"proxied": strconv.FormatBool(proxied)}
	if proxied {
		r.TTL = 1
	}
	return r
}

// RecordNeedsUpdate implements provider.RecordComparer the way the Cloudflare
// provider does: only the proxied flag is compared.
func (m *stateMockProvider) RecordNeedsUpdate(existing, desired provider.Record) bool {
	current, ok := existing.Metadata["proxied"]
	if !ok {
		return false
	}
	return (current == "true") != m.resolveProxied(desired)
}

func (m *stateMockProvider) Create(ctx context.Context, r provider.Record) error {
	return m.testMockProvider.Create(ctx, m.stored(r))
}

// Update implements provider.Updater, recording the call and replacing the
// stored record with what the provider would list afterwards.
func (m *stateMockProvider) Update(_ context.Context, existing, desired provider.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updated = append(m.updated, stateUpdateCall{existing: existing, desired: desired})
	for i, rec := range m.records {
		if rec.Hostname == existing.Hostname && rec.Type == existing.Type && rec.Target == existing.Target {
			m.records[i] = m.stored(desired)
			return nil
		}
	}
	return provider.ErrNotFound
}

func (m *stateMockProvider) Updated() []stateUpdateCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]stateUpdateCall, len(m.updated))
	copy(result, m.updated)
	return result
}

// record returns the stored record for hostname, or false.
func (m *stateMockProvider) record(hostname string, recordType provider.RecordType) (provider.Record, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rec := range m.records {
		if rec.Hostname == hostname && rec.Type == recordType {
			return rec, true
		}
	}
	return provider.Record{}, false
}

var (
	_ provider.RecordComparer = (*stateMockProvider)(nil)
	_ provider.Updater        = (*stateMockProvider)(nil)
)

// listedRecord builds an A record as the mock would list it.
func listedRecord(hostname, target string, proxied bool) provider.Record {
	rec := provider.Record{
		Hostname: hostname,
		Type:     provider.RecordTypeA,
		Target:   target,
		TTL:      300,
		Metadata: map[string]string{"proxied": strconv.FormatBool(proxied)},
	}
	if proxied {
		rec.TTL = 1
	}
	return rec
}

// ownershipTXT builds the legacy-format ownership record for hostname.
func ownershipTXT(hostname string) provider.Record {
	return provider.OwnershipRecord(hostname, 300, "", nil)
}

// memberOwnershipTXT builds a versioned ownership record for one A member.
func memberOwnershipTXT(hostname, target string) provider.Record {
	record := provider.Record{
		Hostname: hostname,
		Type:     provider.RecordTypeA,
		Target:   target,
		TTL:      300,
	}
	return provider.MemberOwnershipRecord(hostname, 300, "", record, nil)
}

// newStateTestRegistry wires prov as the only instance of a registry:
// A records pointing at stateTestTarget for *.example.com, TTL 300.
func newStateTestRegistry(t *testing.T, prov provider.Provider) *provider.Registry {
	t.Helper()
	logger := quietLogger()
	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("state-mock", func(_ provider.FactoryConfig) (provider.Provider, error) {
		return prov, nil
	})
	err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:       prov.Name(),
		TypeName:   "state-mock",
		RecordType: provider.RecordTypeA,
		Target:     stateTestTarget,
		TTL:        300,
		Domains:    []string{"*.example.com"},
	})
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}
	return providers
}

func newStateTestReconciler(providers *provider.Registry, cfg Config) *Reconciler {
	r := &Reconciler{
		providers:      providers,
		config:         cfg,
		logger:         quietLogger(),
		knownHostnames: make(map[string]struct{}),
	}
	r.syncAtomics()
	return r
}

func hostnameWithMetadata(name string, metadata map[string]string) *source.Hostname {
	h := &source.Hostname{Name: name, Source: "test"}
	if metadata != nil {
		h.RecordHints = &source.RecordHints{Metadata: metadata}
	}
	return h
}

func TestEnsureRecord_ProxiedStateChange(t *testing.T) {
	const host = "mail.example.com"

	tests := []struct {
		name            string
		instanceProxied bool              // provider default (DNSWEAVER_<NAME>_PROXIED)
		existingProxied bool              // state the record is listed with
		hint            map[string]string // per-record metadata from the source, nil for none
		wantUpdate      bool
		wantProxied     bool // state after the pass
	}{
		{
			name:            "label false flips an existing proxied record",
			instanceProxied: true,
			existingProxied: true,
			hint:            map[string]string{"proxied": "false"},
			wantUpdate:      true,
			wantProxied:     false,
		},
		{
			name:            "label true flips an existing DNS-only record",
			instanceProxied: false,
			existingProxied: false,
			hint:            map[string]string{"proxied": "true"},
			wantUpdate:      true,
			wantProxied:     true,
		},
		{
			name:            "label matching the existing state is a no-op",
			instanceProxied: true,
			existingProxied: false,
			hint:            map[string]string{"proxied": "false"},
			wantUpdate:      false,
			wantProxied:     false,
		},
		{
			name:            "no label, default matches the existing state (proxied TTL 1 vs configured 300)",
			instanceProxied: true,
			existingProxied: true,
			hint:            nil,
			wantUpdate:      false,
			wantProxied:     true,
		},
		{
			name:            "no label, default flipped to false",
			instanceProxied: false,
			existingProxied: true,
			hint:            nil,
			wantUpdate:      true,
			wantProxied:     false,
		},
		{
			name:            "no label, default flipped to true",
			instanceProxied: true,
			existingProxied: false,
			hint:            nil,
			wantUpdate:      true,
			wantProxied:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newStateMockProvider("cf", tt.instanceProxied)
			mock.AddRecord(listedRecord(host, stateTestTarget, tt.existingProxied))
			mock.AddRecord(ownershipTXT(host))

			providers := newStateTestRegistry(t, mock)
			cache := newRecordCache(context.Background(), providers, quietLogger())
			r := newStateTestReconciler(providers, DefaultConfig())

			actions := r.ensureRecord(context.Background(), hostnameWithMetadata(host, tt.hint), cache)
			if len(actions) != 1 {
				t.Fatalf("expected 1 action, got %d", len(actions))
			}

			wantType := ActionSkip
			if tt.wantUpdate {
				wantType = ActionUpdate
			}
			if actions[0].Type != wantType {
				t.Errorf("action type = %v, want %v (error=%q)", actions[0].Type, wantType, actions[0].Error)
			}
			if tt.wantUpdate && actions[0].Status != StatusSuccess {
				t.Errorf("status = %v, want %v", actions[0].Status, StatusSuccess)
			}

			updated := mock.Updated()
			if tt.wantUpdate && len(updated) != 1 {
				t.Fatalf("expected 1 update call, got %d", len(updated))
			}
			if !tt.wantUpdate && len(updated) != 0 {
				t.Fatalf("expected no update call, got %d", len(updated))
			}
			if tt.wantUpdate {
				if got := updated[0].existing.Target; got != stateTestTarget {
					t.Errorf("updated the wrong record: existing target %q", got)
				}
				if got := updated[0].desired.Metadata["proxied"]; tt.hint != nil && got != tt.hint["proxied"] {
					t.Errorf("desired metadata proxied = %q, want %q", got, tt.hint["proxied"])
				}
			}

			if created := mock.GetCreatedDNSRecords(); len(created) != 0 {
				t.Errorf("expected no DNS record creates, got %d", len(created))
			}
			if deleted := mock.GetDeleted(); len(deleted) != 0 {
				t.Errorf("expected no deletes, got %d", len(deleted))
			}

			rec, ok := mock.record(host, provider.RecordTypeA)
			if !ok {
				t.Fatal("record disappeared")
			}
			if got := rec.Metadata["proxied"]; got != strconv.FormatBool(tt.wantProxied) {
				t.Errorf("stored proxied = %q, want %v", got, tt.wantProxied)
			}
		})
	}
}

func TestEnsureRecord_ProxiedStateChange_OnlyForManagedRecords(t *testing.T) {
	const host = "mail.example.com"

	tests := []struct {
		name              string
		owned             bool
		adoptExisting     bool
		ownershipTracking bool
		wantUpdate        bool
	}{
		{"owned record is updated", true, false, true, true},
		{"unowned record is left alone without ADOPT_EXISTING", false, false, true, false},
		{"unowned record is adopted and updated with ADOPT_EXISTING", false, true, true, true},
		{"every record is managed when ownership tracking is off", false, false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newStateMockProvider("cf", true)
			mock.AddRecord(listedRecord(host, stateTestTarget, true))
			if tt.owned {
				mock.AddRecord(ownershipTXT(host))
			}

			cfg := DefaultConfig()
			cfg.AdoptExisting = tt.adoptExisting
			cfg.OwnershipTracking = tt.ownershipTracking

			providers := newStateTestRegistry(t, mock)
			cache := newRecordCache(context.Background(), providers, quietLogger())
			r := newStateTestReconciler(providers, cfg)

			hint := map[string]string{"proxied": "false"}
			actions := r.ensureRecord(context.Background(), hostnameWithMetadata(host, hint), cache)
			if len(actions) != 1 {
				t.Fatalf("expected 1 action, got %d", len(actions))
			}

			updated := mock.Updated()
			if tt.wantUpdate {
				if actions[0].Type != ActionUpdate {
					t.Errorf("action type = %v, want %v (error=%q)", actions[0].Type, ActionUpdate, actions[0].Error)
				}
				if len(updated) != 1 {
					t.Errorf("expected 1 update call, got %d", len(updated))
				}
			} else {
				if actions[0].Type != ActionSkip {
					t.Errorf("action type = %v, want %v", actions[0].Type, ActionSkip)
				}
				if len(updated) != 0 {
					t.Errorf("expected no update call, got %d", len(updated))
				}
			}

			if tt.adoptExisting && tt.ownershipTracking && len(mock.GetCreatedOwnershipRecords()) != 1 {
				t.Errorf("expected the record to be adopted (ownership TXT created), got %d", len(mock.GetCreatedOwnershipRecords()))
			}
		})
	}
}

func TestEnsureRecord_ProviderWithoutRecordComparerIsUnchanged(t *testing.T) {
	// A provider that does not implement provider.RecordComparer must keep
	// today's behavior: an existing record with the right target is skipped,
	// whatever its Metadata says and whatever the source asks for.
	const host = "mail.example.com"

	mock := newTestMockProvider("plain")
	mock.AddRecord(listedRecord(host, stateTestTarget, true))
	mock.AddRecord(ownershipTXT(host))

	providers := newStateTestRegistry(t, mock)
	cache := newRecordCache(context.Background(), providers, quietLogger())
	r := newStateTestReconciler(providers, DefaultConfig())

	hint := map[string]string{"proxied": "false"}
	actions := r.ensureRecord(context.Background(), hostnameWithMetadata(host, hint), cache)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionSkip || actions[0].Error != errRecordAlreadyExists {
		t.Errorf("expected skip with %q, got %v %q", errRecordAlreadyExists, actions[0].Type, actions[0].Error)
	}
	if created := mock.GetCreatedDNSRecords(); len(created) != 0 {
		t.Errorf("expected no creates, got %d", len(created))
	}
	if deleted := mock.GetDeleted(); len(deleted) != 0 {
		t.Errorf("expected no deletes, got %d", len(deleted))
	}
}

func TestEnsureRecord_TargetChangeKeepsProxiedOverride(t *testing.T) {
	// When the target changes, the update must carry the per-record metadata;
	// before #170 it did not, so a target change re-proxied a record whose
	// label said proxied=false.
	const host = "mail.example.com"
	const oldTarget = "203.0.113.99"
	hint := map[string]string{"proxied": "false"}

	t.Run("native update receives the metadata", func(t *testing.T) {
		mock := newStateMockProvider("cf", true)
		mock.AddRecord(listedRecord(host, oldTarget, false))
		mock.AddRecord(ownershipTXT(host))

		providers := newStateTestRegistry(t, mock)
		cache := newRecordCache(context.Background(), providers, quietLogger())
		r := newStateTestReconciler(providers, DefaultConfig())

		actions := r.ensureRecord(context.Background(), hostnameWithMetadata(host, hint), cache)
		if len(actions) != 1 || actions[0].Type != ActionUpdate {
			t.Fatalf("expected a single update action, got %+v", actions)
		}

		updated := mock.Updated()
		if len(updated) != 1 {
			t.Fatalf("expected 1 update call, got %d", len(updated))
		}
		if updated[0].existing.Target != oldTarget || updated[0].desired.Target != stateTestTarget {
			t.Errorf("update targets = %q -> %q, want %q -> %q", updated[0].existing.Target, updated[0].desired.Target, oldTarget, stateTestTarget)
		}
		if got := updated[0].desired.Metadata["proxied"]; got != "false" {
			t.Errorf("desired metadata proxied = %q, want %q", got, "false")
		}

		rec, ok := mock.record(host, provider.RecordTypeA)
		if !ok {
			t.Fatal("record disappeared")
		}
		if rec.Target != stateTestTarget || rec.Metadata["proxied"] != "false" {
			t.Errorf("stored record = target %q proxied %q, want %q / false", rec.Target, rec.Metadata["proxied"], stateTestTarget)
		}
	})

	t.Run("delete and create fallback receives the metadata", func(t *testing.T) {
		mock := newTestMockProvider("plain")
		mock.AddRecord(listedRecord(host, oldTarget, false))
		mock.AddRecord(ownershipTXT(host))

		providers := newStateTestRegistry(t, mock)
		cache := newRecordCache(context.Background(), providers, quietLogger())
		r := newStateTestReconciler(providers, DefaultConfig())

		actions := r.ensureRecord(context.Background(), hostnameWithMetadata(host, hint), cache)
		if len(actions) != 1 || actions[0].Type != ActionUpdate {
			t.Fatalf("expected a single update action, got %+v", actions)
		}

		created := mock.GetCreatedDNSRecords()
		if len(created) != 1 {
			t.Fatalf("expected 1 created DNS record, got %d", len(created))
		}
		if created[0].Target != stateTestTarget || created[0].Metadata["proxied"] != "false" {
			t.Errorf("created record = target %q proxied %q, want %q / false", created[0].Target, created[0].Metadata["proxied"], stateTestTarget)
		}
	})
}

func TestEnsureRecord_RecoveredMetadataDoesNotDriveStateUpdate(t *testing.T) {
	// Metadata recovered from ownership TXT records on startup informs
	// creation only. An existing record is compared against what the source
	// declares now (or the instance default), otherwise a stale TXT copy would
	// flip it back on every restart.
	const host = "mail.example.com"

	tests := []struct {
		name            string
		existingProxied bool
		wantUpdate      bool
		wantProxied     bool
	}{
		{"record matching the default stays put despite recovered proxied=false", true, false, true},
		{"record still carrying the removed label's state follows the default", false, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newStateMockProvider("cf", true)
			mock.AddRecord(listedRecord(host, stateTestTarget, tt.existingProxied))
			mock.AddRecord(ownershipTXT(host))

			providers := newStateTestRegistry(t, mock)
			cache := newRecordCache(context.Background(), providers, quietLogger())
			r := newStateTestReconciler(providers, DefaultConfig())
			r.recoveredMetadata = map[string]map[string]string{
				host: {"proxied": "false"},
			}

			actions := r.ensureRecord(context.Background(), hostnameWithMetadata(host, nil), cache)
			if len(actions) != 1 {
				t.Fatalf("expected 1 action, got %d", len(actions))
			}

			updated := mock.Updated()
			if tt.wantUpdate && (actions[0].Type != ActionUpdate || len(updated) != 1) {
				t.Errorf("expected one update, got action %v and %d update calls", actions[0].Type, len(updated))
			}
			if !tt.wantUpdate && (actions[0].Type != ActionSkip || len(updated) != 0) {
				t.Errorf("expected a skip, got action %v and %d update calls", actions[0].Type, len(updated))
			}

			rec, _ := mock.record(host, provider.RecordTypeA)
			if got := rec.Metadata["proxied"]; got != strconv.FormatBool(tt.wantProxied) {
				t.Errorf("stored proxied = %q, want %v", got, tt.wantProxied)
			}
			if remaining := r.RecoveredMetadata(); len(remaining) != 0 {
				t.Errorf("expected recovered metadata to be consumed, got %v", remaining)
			}
		})
	}
}

func TestReconcile_ProxiedStateConvergesWithoutChurn(t *testing.T) {
	// Two full passes: the first brings the record in line with the label, the
	// second finds nothing to do. A third pass after the label is removed
	// flips the record back to the instance default, and a fourth is again a
	// no-op. TTL is 1 while proxied and 300 while not, as Cloudflare reports
	// it, and must never by itself cause an update.
	const host = "mail.example.com"

	mock := newStateMockProvider("cf", true)
	mock.AddRecord(listedRecord(host, stateTestTarget, true))
	mock.AddRecord(ownershipTXT(host))
	providers := newStateTestRegistry(t, mock)

	logger := quietLogger()
	sources := source.NewRegistry(logger)
	sources.Register(dnsweaversource.New(dnsweaversource.WithLogger(logger)))

	lister := newTestMockWorkloadLister(workload.PlatformDocker)
	lister.AddWorkload("mail", map[string]string{
		"dnsweaver.hostname": host,
		"dnsweaver.proxied":  "false",
	})

	r := New([]workload.Lister{lister}, sources, providers,
		WithConfig(DefaultConfig()),
		WithLogger(logger),
	)

	pass := func(step string, wantUpdates int, wantProxied bool) {
		t.Helper()
		result, err := r.Reconcile(context.Background())
		if err != nil {
			t.Fatalf("%s: Reconcile error: %v", step, err)
		}
		if got := len(result.Updated()); got != wantUpdates {
			t.Errorf("%s: updated = %d, want %d", step, got, wantUpdates)
		}
		if got := len(result.Created()); got != 0 {
			t.Errorf("%s: created = %d, want 0", step, got)
		}
		if got := len(result.Deleted()); got != 0 {
			t.Errorf("%s: deleted = %d, want 0", step, got)
		}
		rec, ok := mock.record(host, provider.RecordTypeA)
		if !ok {
			t.Fatalf("%s: record disappeared", step)
		}
		if got := rec.Metadata["proxied"]; got != strconv.FormatBool(wantProxied) {
			t.Errorf("%s: stored proxied = %q, want %v", step, got, wantProxied)
		}
	}

	pass("first pass applies the label", 1, false)
	pass("second pass is a no-op", 0, false)

	lister.workloads[0].Labels = map[string]string{"dnsweaver.hostname": host}
	pass("label removed, default applies", 1, true)
	pass("steady state with proxied TTL 1 vs configured 300", 0, true)

	if got := len(mock.Updated()); got != 2 {
		t.Errorf("total update calls = %d, want 2", got)
	}
}
