package reconciler

import (
	"context"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
	"github.com/maxfield-allison/dnsweaver/sources/traefik"
)

func adoptionBool(value bool) *bool { return &value }

func newAdoptionOverrideFixture(
	t *testing.T,
	globalAdopt bool,
	instanceAdopt *bool,
	allowOverrides bool,
	existing provider.Record,
) (*Reconciler, *provider.ProviderInstance, *testMockProvider, *recordCache) {
	t.Helper()
	logger := quietLogger()
	mock := newTestMockProvider("test-dns")
	mock.AddRecord(existing)

	providers := provider.NewRegistry(logger)
	providers.RegisterFactory("adoption-mock", func(provider.FactoryConfig) (provider.Provider, error) {
		return mock, nil
	})
	if err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name:                        "test-dns",
		TypeName:                    "adoption-mock",
		RecordType:                  provider.RecordTypeA,
		Target:                      "10.0.0.1",
		TTL:                         300,
		Mode:                        provider.ModeManaged,
		AdoptExisting:               instanceAdopt,
		AdoptExistingAllowOverrides: allowOverrides,
		Domains:                     []string{"*.example.com"},
	}); err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}
	inst, ok := providers.Get("test-dns")
	if !ok {
		t.Fatal("provider instance not found")
	}

	cfg := DefaultConfig()
	cfg.AdoptExisting = globalAdopt
	r := New(nil, source.NewRegistry(logger), providers, WithConfig(cfg), WithLogger(logger))
	return r, inst, mock, newRecordCache(context.Background(), providers, logger)
}

func TestEffectiveAdoptExisting_PrecedenceAndGate(t *testing.T) {
	tests := []struct {
		name           string
		global         bool
		instance       *bool
		allowOverrides bool
		hint           *bool
		want           bool
	}{
		{name: "global default", global: true, want: true},
		{name: "instance enables against global", instance: adoptionBool(true), want: true},
		{name: "instance disables against global", global: true, instance: adoptionBool(false), want: false},
		{name: "workload may always disable", global: true, hint: adoptionBool(false), want: false},
		{name: "workload enable is blocked by default", hint: adoptionBool(true), want: false},
		{name: "workload enable allowed per provider", allowOverrides: true, hint: adoptionBool(true), want: true},
		{name: "workload cannot bypass instance false without gate", global: true, instance: adoptionBool(false), hint: adoptionBool(true), want: false},
		{name: "workload can bypass instance false with gate", global: true, instance: adoptionBool(false), allowOverrides: true, hint: adoptionBool(true), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, inst, _, _ := newAdoptionOverrideFixture(t, tt.global, tt.instance, tt.allowOverrides, provider.Record{
				Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1",
			})
			hostname := &source.Hostname{Name: "app.example.com"}
			if tt.hint != nil {
				hostname.AdoptExisting = tt.hint
			}
			if got := r.effectiveAdoptExisting(hostname, inst); got != tt.want {
				t.Errorf("effectiveAdoptExisting() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEffectiveAdoptExisting_RecordHintOverridesWorkloadHint(t *testing.T) {
	r, inst, _, _ := newAdoptionOverrideFixture(t, false, nil, true, provider.Record{
		Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1",
	})
	hostname := &source.Hostname{
		Name:          "app.example.com",
		AdoptExisting: adoptionBool(true),
		RecordHints:   &source.RecordHints{AdoptExisting: adoptionBool(false)},
	}
	if r.effectiveAdoptExisting(hostname, inst) {
		t.Error("record-specific adopt=false did not override workload adopt=true")
	}
}

func TestMergeDuplicateHostname_PreservesRecordSpecificAdoption(t *testing.T) {
	workloadAdopt := adoptionBool(true)
	recordAdopt := adoptionBool(false)
	traefikHostname := &source.Hostname{Name: "app.example.com", Source: "traefik", AdoptExisting: workloadAdopt}
	nativeHostname := &source.Hostname{
		Name:          "app.example.com",
		Source:        "dnsweaver",
		AdoptExisting: workloadAdopt,
		RecordHints:   &source.RecordHints{AdoptExisting: recordAdopt},
	}

	for _, pair := range [][2]*source.Hostname{{traefikHostname, nativeHostname}, {nativeHostname, traefikHostname}} {
		winner := mergeDuplicateHostname(pair[0], pair[1])
		if winner.Source != "dnsweaver" || winner.RecordHints == nil || winner.RecordHints.AdoptExisting == nil || *winner.RecordHints.AdoptExisting {
			t.Errorf("winner = %+v, want native record-specific adopt=false", winner)
		}
	}
}

func TestAdoptionOverride_ControlsExactMatchOwnership(t *testing.T) {
	tests := []struct {
		name           string
		global         bool
		instance       *bool
		allowOverrides bool
		hint           *bool
		wantOwnership  bool
	}{
		{name: "global disabled skips", wantOwnership: false},
		{name: "instance enables", instance: adoptionBool(true), wantOwnership: true},
		{name: "instance disables global", global: true, instance: adoptionBool(false), wantOwnership: false},
		{name: "label disables global", global: true, hint: adoptionBool(false), wantOwnership: false},
		{name: "label enable blocked", hint: adoptionBool(true), wantOwnership: false},
		{name: "label enable allowed", allowOverrides: true, hint: adoptionBool(true), wantOwnership: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, inst, mock, cache := newAdoptionOverrideFixture(t, tt.global, tt.instance, tt.allowOverrides, provider.Record{
				Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300,
			})
			hostname := &source.Hostname{Name: "app.example.com", Source: "test"}
			if tt.hint != nil {
				hostname.AdoptExisting = tt.hint
			}
			actions := r.ensureRecordForProvider(context.Background(), hostname, inst, cache)
			if len(actions) != 1 || actions[0].Type != ActionSkip {
				t.Fatalf("actions = %+v, want one skip", actions)
			}
			gotOwnership := len(mock.GetCreatedOwnershipRecords()) == 1
			if gotOwnership != tt.wantOwnership {
				t.Errorf("ownership created = %v, want %v", gotOwnership, tt.wantOwnership)
			}
		})
	}
}

func TestAdoptionOverride_ProtectsUnownedWrongTarget(t *testing.T) {
	tests := []struct {
		name           string
		allowOverrides bool
		wantUpdate     bool
	}{
		{name: "blocked label leaves record untouched"},
		{name: "allowed label adopts and updates", allowOverrides: true, wantUpdate: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, inst, mock, cache := newAdoptionOverrideFixture(t, false, nil, tt.allowOverrides, provider.Record{
				Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.99", TTL: 300,
			})
			hostname := &source.Hostname{
				Name:          "app.example.com",
				AdoptExisting: adoptionBool(true),
			}
			actions := r.ensureRecordForProvider(context.Background(), hostname, inst, cache)
			if len(actions) != 1 {
				t.Fatalf("actions = %+v", actions)
			}
			if tt.wantUpdate {
				if actions[0].Type != ActionUpdate || len(mock.GetDeleted()) != 1 || len(mock.GetCreatedOwnershipRecords()) != 1 {
					t.Errorf("adopted update did not complete: action=%+v deleted=%d ownership=%d", actions[0], len(mock.GetDeleted()), len(mock.GetCreatedOwnershipRecords()))
				}
			} else if actions[0].Type != ActionSkip || len(mock.GetDeleted()) != 0 || len(mock.GetCreated()) != 0 {
				t.Errorf("unowned record was modified: action=%+v deleted=%d created=%d", actions[0], len(mock.GetDeleted()), len(mock.GetCreated()))
			}
		})
	}
}

func TestAdoptionOverride_ControlsTypeConflictReplacement(t *testing.T) {
	for _, allow := range []bool{false, true} {
		name := "blocked"
		if allow {
			name = "allowed"
		}
		t.Run(name, func(t *testing.T) {
			r, inst, mock, cache := newAdoptionOverrideFixture(t, false, nil, allow, provider.Record{
				Hostname: "app.example.com", Type: provider.RecordTypeCNAME, Target: "old.example.com", TTL: 300,
			})
			hostname := &source.Hostname{
				Name:          "app.example.com",
				AdoptExisting: adoptionBool(true),
			}
			actions := r.ensureRecordForProvider(context.Background(), hostname, inst, cache)
			if allow {
				if len(actions) != 2 || actions[0].Type != ActionDelete || actions[1].Type != ActionCreate {
					t.Fatalf("actions = %+v, want delete then create", actions)
				}
			} else if len(actions) != 1 || actions[0].Type != ActionSkip || len(mock.GetDeleted()) != 0 {
				t.Fatalf("actions = %+v, deleted=%d; want untouched conflict", actions, len(mock.GetDeleted()))
			}
		})
	}
}

func TestAdoptionOverride_AppliesToTraefikWorkloadLabels(t *testing.T) {
	for _, allow := range []bool{false, true} {
		name := "blocked"
		if allow {
			name = "allowed"
		}
		t.Run(name, func(t *testing.T) {
			logger := quietLogger()
			lister := newTestMockWorkloadLister(workload.PlatformDocker)
			lister.AddWorkload("app", map[string]string{
				"traefik.http.routers.app.rule": "Host(`app.example.com`)",
				"dnsweaver.adopt":               "true",
			})
			sources := source.NewRegistry(logger)
			_ = sources.Register(traefik.New(traefik.WithLogger(logger)))

			mock := newTestMockProvider("test-dns")
			mock.AddRecord(provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300})
			providers := provider.NewRegistry(logger)
			providers.RegisterFactory("adoption-mock", func(provider.FactoryConfig) (provider.Provider, error) { return mock, nil })
			if err := providers.CreateInstance(provider.ProviderInstanceConfig{
				Name:                        "test-dns",
				TypeName:                    "adoption-mock",
				RecordType:                  provider.RecordTypeA,
				Target:                      "10.0.0.1",
				TTL:                         300,
				Mode:                        provider.ModeManaged,
				AdoptExistingAllowOverrides: allow,
				Domains:                     []string{"*.example.com"},
			}); err != nil {
				t.Fatal(err)
			}

			r := New([]workload.Lister{lister}, sources, providers, WithConfig(DefaultConfig()), WithLogger(logger))
			if _, err := r.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			got := len(mock.GetCreatedOwnershipRecords()) == 1
			if got != allow {
				t.Errorf("ownership created = %v, want %v", got, allow)
			}
		})
	}
}

func TestAdoptionOverride_CreateConflictDoesNotBypassPolicy(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "blocked", true: "allowed"}[allow], func(t *testing.T) {
			r, inst, mock, cache := newAdoptionOverrideFixture(t, false, nil, allow, provider.Record{
				Hostname: "unrelated.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1",
			})
			mock.createFn = func(_ context.Context, record provider.Record) error {
				if record.Type == provider.RecordTypeA {
					return provider.ErrConflict
				}
				return nil
			}
			hostname := &source.Hostname{Name: "app.example.com", AdoptExisting: adoptionBool(true)}
			actions := r.ensureRecordForProvider(context.Background(), hostname, inst, cache)
			if len(actions) != 1 || actions[0].Type != ActionSkip {
				t.Fatalf("actions = %+v, want one conflict skip", actions)
			}
			gotOwnership := len(mock.GetCreatedOwnershipRecords()) == 1
			if gotOwnership != allow {
				t.Errorf("ownership created after create conflict = %v, want %v", gotOwnership, allow)
			}
		})
	}
}
