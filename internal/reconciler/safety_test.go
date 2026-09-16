package reconciler

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

func safetyClaim(typ, target string) *source.Hostname {
	return &source.Hostname{Name: "app.example.com", Source: "native", RecordHints: &source.RecordHints{Type: typ, Target: target}}
}

// Compare the complete provider contents, including ownership markers and TTLs.
// Audit counters alone can hide successful writes followed by destructive cleanup.
func safetyRecords(t *testing.T, m *testMockProvider, want ...provider.Record) {
	t.Helper()
	got, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	canonical := func(records []provider.Record) []string {
		result := make([]string, 0, len(records))
		for _, r := range records {
			result = append(result, fmt.Sprintf("%s %s %s ttl=%d srv=%v metadata=%v", r.Hostname, r.Type, r.Target, r.TTL, r.SRV, r.Metadata))
		}
		sort.Strings(result)
		return result
	}
	if !reflect.DeepEqual(canonical(got), canonical(want)) {
		t.Fatalf("final records:\n got %v\nwant %v", canonical(got), canonical(want))
	}
}

func safetyMarker(r provider.Record) provider.Record {
	return provider.MemberOwnershipRecord(r.Hostname, 300, "test-instance", r, r.Metadata)
}

func TestSafetyTXTModeLifecycle(t *testing.T) {
	for _, mode := range []provider.OperationalMode{provider.ModeManaged, provider.ModeAuthoritative, provider.ModeAdditive} {
		for _, adopt := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/adopt=%t", mode, adopt), func(t *testing.T) {
				manual := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeTXT, Target: "v=spf1 -all", TTL: 300}
				foreign := provider.Record{Hostname: manual.Hostname, Type: manual.Type, Target: "other-instance=value", TTL: 300}
				foreignMarker := provider.MemberOwnershipRecord(foreign.Hostname, 300, "other-instance", foreign, nil)
				claim := safetyClaim("TXT", "verification=one")
				r, m, set, cache := setTestHarness(t, mode, setTestCapabilities(true, false), []provider.Record{manual, foreign, foreignMarker}, claim)
				r.config.AdoptExisting = adopt
				first := set.Members[0].Record
				r.reconcileDesiredSet(context.Background(), set, cache, nil)
				safetyRecords(t, m, manual, foreign, foreignMarker, first, safetyMarker(first))
				cache = newRecordCache(context.Background(), r.providers, quietLogger())
				creates, deletes := len(m.GetCreated()), len(m.GetDeleted())
				r.reconcileDesiredSet(context.Background(), set, cache, nil)
				if len(m.GetCreated()) != creates || len(m.GetDeleted()) != deletes {
					t.Fatal("unchanged TXT wrote provider")
				}
				claim.RecordHints.Target = "verification=two"
				set = r.compileDesiredRecordSets([]*source.Hostname{claim}).Sets[0]
				second := set.Members[0].Record
				r.reconcileDesiredSet(context.Background(), set, cache, nil)
				want := []provider.Record{manual, foreign, foreignMarker, second, safetyMarker(second)}
				if mode == provider.ModeAdditive {
					want = append(want, first, safetyMarker(first))
				}
				safetyRecords(t, m, want...)
				// A new reconciler has no memory: durable exact markers must suffice.
				r = New(nil, source.NewRegistry(quietLogger()), r.providers, WithConfig(r.config), WithLogger(quietLogger()))
				cache = newRecordCache(context.Background(), r.providers, quietLogger())
				set.Members = nil
				r.reconcileDesiredSet(context.Background(), set, cache, nil)
				if mode != provider.ModeAdditive {
					want = []provider.Record{manual, foreign, foreignMarker}
				}
				safetyRecords(t, m, want...)
			})
		}
	}
}

func TestSafetyNoTXTComparerUsesLiveMemory(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(fmt.Sprint(live), func(t *testing.T) {
			m := newStateMockProvider("state", true)
			caps := setTestCapabilities(false, true)
			m.capabilities = &caps
			a := listedRecord("app.example.com", "192.0.2.1", false)
			b := listedRecord("app.example.com", "192.0.2.2", false)
			m.AddRecord(a)
			m.AddRecord(b)
			reg := newStateTestRegistry(t, m)
			r := New(nil, source.NewRegistry(quietLogger()), reg, WithConfig(DefaultConfig()), WithLogger(quietLogger()))
			set := r.compileDesiredRecordSets([]*source.Hostname{hintedA(a.Hostname, a.Target), hintedA(b.Hostname, b.Target)}).Sets[0]
			var previous []provider.Record
			if live {
				previous = []provider.Record{a, b}
			}
			_, managed := r.reconcileDesiredSetWithState(context.Background(), set, newRecordCache(context.Background(), reg, quietLogger()), previous, true)
			if live {
				if len(m.Updated()) != 2 || len(managed) != 2 {
					t.Fatalf("updates=%v managed=%v", m.Updated(), managed)
				}
				safetyRecords(t, m.testMockProvider, listedRecord(a.Hostname, a.Target, true), listedRecord(b.Hostname, b.Target, true))
			} else {
				if len(m.Updated()) != 0 || len(managed) != 0 {
					t.Fatalf("restart acquired authority: %v %v", m.Updated(), managed)
				}
				safetyRecords(t, m.testMockProvider, a, b)
			}
		})
	}
}

// Stale ownership markers remain a characterized backend limitation.
func TestSafetyStaleMarkerCanReclaimExactManualRecreation(t *testing.T) {
	old := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.10", TTL: 300}
	marker := safetyMarker(old)
	r, m, set, cache := setTestHarness(t, provider.ModeManaged, setTestCapabilities(true, false), []provider.Record{old, marker}, hintedA(old.Hostname, old.Target))
	set.Members = nil
	m.deleteFn = func(_ context.Context, rec provider.Record) error {
		if provider.IsOwnershipRecord(rec.Hostname) {
			return errors.New("marker delete unavailable")
		}
		return nil
	}
	r.reconcileDesiredSet(context.Background(), set, cache, nil)
	safetyRecords(t, m, marker)
	m.AddRecord(old)
	m.deleteFn = nil
	cache = newRecordCache(context.Background(), r.providers, quietLogger())
	if !r.mayDeleteMember(set.Instance, old, cache, nil, false) {
		t.Fatal("stale marker behavior changed; review the backend limitation expectation")
	}
	r.reconcileDesiredSet(context.Background(), set, cache, nil)
	safetyRecords(t, m)
}

func TestSafetyCrossTypeFailureMatrix(t *testing.T) {
	for _, transition := range []struct {
		from, to string
		old, new []string
	}{
		{"A", "CNAME", []string{"192.0.2.10", "192.0.2.11"}, []string{"new.example.com"}},
		{"AAAA", "CNAME", []string{"2001:db8::10", "2001:db8::11"}, []string{"new.example.com"}},
		{"CNAME", "A", []string{"old.example.com"}, []string{"192.0.2.10", "192.0.2.11"}},
		{"CNAME", "AAAA", []string{"old.example.com"}, []string{"2001:db8::10", "2001:db8::11"}},
	} {
		for _, mode := range []provider.OperationalMode{provider.ModeManaged, provider.ModeAuthoritative, provider.ModeAdditive} {
			for _, restart := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s-%s/%s/restart=%t", transition.from, transition.to, mode, restart), func(t *testing.T) {
					s := newTestMockSource("native")
					for _, target := range transition.old {
						s.hostnames = append(s.hostnames, *safetyClaim(transition.from, target))
					}
					r, m, reg := liveSetHarness(t, setTestCapabilities(true, false), s, []workload.Workload{{ID: "one", Platform: workload.PlatformDocker}})
					reg.All()[0].Mode = mode
					if _, err := r.Reconcile(context.Background()); err != nil {
						t.Fatal(err)
					}
					before, _ := m.List(context.Background())
					s.hostnames = nil
					for _, target := range transition.new {
						s.hostnames = append(s.hostnames, *safetyClaim(transition.to, target))
					}
					if restart {
						r = New(r.listers, r.sources, reg, WithConfig(r.config), WithLogger(quietLogger()))
					}
					m.createFn = func(_ context.Context, rec provider.Record) error {
						if string(rec.Type) == transition.to && rec.Target == transition.new[len(transition.new)-1] {
							return errors.New("replacement rejected")
						}
						return nil
					}
					if _, err := r.Reconcile(context.Background()); err != nil {
						t.Fatal(err)
					}
					safetyRecords(t, m, before...)
				})
			}
		}
	}
}

func TestSafetyTXTLegacyAndCacheMiss(t *testing.T) {
	for _, mode := range []provider.OperationalMode{provider.ModeManaged, provider.ModeAuthoritative, provider.ModeAdditive} {
		for _, adopt := range []bool{false, true} {
			for _, cached := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/adopt=%t/cache=%t", mode, adopt, cached), func(t *testing.T) {
					manual := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeTXT, Target: "v=DKIM1; p=manual", TTL: 300}
					claim := safetyClaim("TXT", "verification=wanted")
					r, m, set, cache := setTestHarness(t, mode, setTestCapabilities(true, false), []provider.Record{manual}, claim)
					r.config.AdoptExisting = adopt
					if !cached {
						cache = nil
					}
					r.ensureRecordForProvider(context.Background(), claim, set.Instance, cache)
					desired := set.Members[0].Record
					safetyRecords(t, m, manual, desired, safetyMarker(desired))
					// Repeat through the public single-host entry point's no-cache path.
					r.ensureRecordForProvider(context.Background(), claim, set.Instance, nil)
					safetyRecords(t, m, manual, desired, safetyMarker(desired))
				})
			}
		}
	}
}

func TestSafetyTXTMatchingForeignAndLegacyMarkers(t *testing.T) {
	for _, markerKind := range []string{"foreign", "legacy", "manual"} {
		for _, adopt := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/adopt=%t", markerKind, adopt), func(t *testing.T) {
				data := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeTXT, Target: "verification=wanted", TTL: 300}
				want := []provider.Record{data}
				if markerKind == "foreign" {
					want = append(want, provider.MemberOwnershipRecord(data.Hostname, 300, "someone-else", data, nil))
				}
				if markerKind == "legacy" {
					want = append(want, provider.OwnershipRecord(data.Hostname, 300, "test-instance", nil))
				}
				r, m, set, cache := setTestHarness(t, provider.ModeAuthoritative, setTestCapabilities(true, false), want, safetyClaim("TXT", data.Target))
				r.config.AdoptExisting = adopt
				r.reconcileDesiredSet(context.Background(), set, cache, nil)
				if adopt && markerKind != "foreign" {
					want = append(want, safetyMarker(data))
				}
				safetyRecords(t, m, want...)
			})
		}
	}
}

func TestSafetyCrossTypeRecoveryFailures(t *testing.T) {
	for _, failure := range []string{"old-delete", "restore", "new-delete", "new-marker-delete", "new-marker-create"} {
		t.Run(failure, func(t *testing.T) {
			from, to := "A", "CNAME"
			oldTargets, newTargets := []string{"192.0.2.10", "192.0.2.11"}, []string{"new.example.com"}
			if failure == "new-delete" || failure == "new-marker-delete" {
				from, to = "CNAME", "A"
				oldTargets = []string{"old.example.com"}
				newTargets = []string{"192.0.2.10", "192.0.2.11"}
			}
			s := newTestMockSource("native")
			for _, target := range oldTargets {
				s.hostnames = append(s.hostnames, *safetyClaim(from, target))
			}
			r, m, _ := liveSetHarness(t, setTestCapabilities(true, false), s, []workload.Workload{{ID: "one", Platform: workload.PlatformDocker}})
			if _, err := r.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			before, _ := m.List(context.Background())
			s.hostnames = nil
			for _, target := range newTargets {
				s.hostnames = append(s.hostnames, *safetyClaim(to, target))
			}
			m.createFn = func(_ context.Context, rec provider.Record) error {
				if failure == "new-marker-create" && provider.IsOwnershipRecord(rec.Hostname) {
					return errors.New("new marker rejected")
				}
				if failure != "new-marker-create" && string(rec.Type) == to && rec.Target == newTargets[len(newTargets)-1] {
					return errors.New("replacement rejected")
				}
				if failure == "restore" && string(rec.Type) == from {
					return errors.New("restore rejected")
				}
				return nil
			}
			m.deleteFn = func(_ context.Context, rec provider.Record) error {
				if failure == "old-delete" && rec.Target == oldTargets[len(oldTargets)-1] {
					return errors.New("old delete rejected")
				}
				if failure == "new-delete" && string(rec.Type) == to {
					return errors.New("new delete rejected")
				}
				if failure == "new-marker-delete" && provider.MatchesMemberOwnership(rec.Target, "test-instance", provider.Record{Type: provider.RecordTypeA, Target: newTargets[0]}) {
					return errors.New("new marker delete rejected")
				}
				return nil
			}
			result, err := r.Reconcile(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.FailedCount() == 0 {
				t.Fatal("failure reported success")
			}
			if failure == "old-delete" || failure == "new-marker-create" {
				safetyRecords(t, m, before...)
				return
			}
			if len(r.pendingReplacements) != 1 {
				t.Fatalf("missing recovery: %+v actions=%+v", r.pendingReplacements, result.Actions)
			}
			// While recovery is pending, dry-run and unavailable snapshots cannot write.
			writes := len(m.GetCreated()) + len(m.GetDeleted())
			r.SetDryRun(true)
			safetyReconcile(t, r)
			r.SetDryRun(false)
			m.listErr = errors.New("snapshot unavailable")
			unavailable := safetyReconcile(t, r)
			if unavailable.FailedCount() == 0 {
				t.Fatal("pending recovery reported success without provider snapshot")
			}
			m.listErr = nil
			if got := len(m.GetCreated()) + len(m.GetDeleted()); got != writes {
				t.Fatalf("recovery wrote without permission/snapshot: %d -> %d", writes, got)
			}
			m.createFn = nil
			m.deleteFn = nil
			result, err = r.Reconcile(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.FailedCount() != 0 {
				t.Fatalf("recovery failed: %v", result.Actions)
			}
			safetyRecords(t, m, before...)
			if len(r.pendingReplacements) != 0 {
				t.Fatal("recovery did not clear")
			}
			// The next ordinary cycle may now complete the requested transition.
			result, err = r.Reconcile(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.FailedCount() != 0 {
				t.Fatalf("retry failed: %v", result.Actions)
			}
			var want []provider.Record
			for _, claim := range s.hostnames {
				rec := desiredRecordForClaim(&claim, r.providers.All()[0])
				want = append(want, rec, safetyMarker(rec))
			}
			safetyRecords(t, m, want...)
		})
	}
}

func TestSafetyIncompleteAndUnavailableSnapshots(t *testing.T) {
	for _, failure := range []string{"source", "provider", "dry-run", "cleanup-disabled"} {
		t.Run(failure, func(t *testing.T) {
			s := newTestMockSource("native", *safetyClaim("A", "192.0.2.10"))
			r, m, _ := liveSetHarness(t, setTestCapabilities(true, false), s, []workload.Workload{{ID: "one", Platform: workload.PlatformDocker}})
			safetyReconcile(t, r)
			before, _ := m.List(context.Background())
			s.hostnames = source.Hostnames{*safetyClaim("CNAME", "new.example.com")}
			switch failure {
			case "source":
				s.err = errors.New("partial source")
			case "provider":
				m.listErr = errors.New("provider down")
			case "dry-run":
				r.SetDryRun(true)
			case "cleanup-disabled":
				r.config.CleanupOrphans = false
			}
			safetyReconcile(t, r)
			m.listErr = nil
			safetyRecords(t, m, before...)
		})
	}
}

func TestSafetyCrossTypeSuccessAndNoTXTRestart(t *testing.T) {
	for _, ownership := range []bool{false, true} {
		for _, restart := range []bool{false, true} {
			for _, direction := range []bool{false, true} {
				t.Run(fmt.Sprintf("ownership=%t/restart=%t/reverse=%t", ownership, restart, direction), func(t *testing.T) {
					from, to, oldTarget, newTarget := "A", "CNAME", "192.0.2.10", "new.example.com"
					if direction {
						from, to, oldTarget, newTarget = "CNAME", "A", "old.example.com", "192.0.2.10"
					}
					s := newTestMockSource("native", *safetyClaim(from, oldTarget))
					r, m, reg := liveSetHarness(t, setTestCapabilities(ownership, false), s, []workload.Workload{{ID: "one", Platform: workload.PlatformDocker}})
					safetyReconcile(t, r)
					before, _ := m.List(context.Background())
					s.hostnames = source.Hostnames{*safetyClaim(to, newTarget)}
					if restart {
						r = New(r.listers, r.sources, reg, WithConfig(r.config), WithLogger(quietLogger()))
					}
					result, err := r.Reconcile(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					if result.FailedCount() != 0 {
						t.Fatalf("actions: %v", result.Actions)
					}
					if restart && !ownership {
						safetyRecords(t, m, before...)
						return
					}
					rec := desiredRecordForClaim(&s.hostnames[0], reg.All()[0])
					want := []provider.Record{rec}
					if ownership {
						want = append(want, safetyMarker(rec))
					}
					safetyRecords(t, m, want...)
					safetyReconcile(t, r)
					safetyRecords(t, m, want...)
				})
			}
		}
	}
}

func TestSafetyTXTFailedChangePreservesOwnedAndManual(t *testing.T) {
	s := newTestMockSource("native", *safetyClaim("TXT", "verification=old"))
	r, m, _ := liveSetHarness(t, setTestCapabilities(true, false), s, []workload.Workload{{ID: "one", Platform: workload.PlatformDocker}})
	manual := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeTXT, Target: "v=spf1 -all", TTL: 300}
	m.AddRecord(manual)
	safetyReconcile(t, r)
	before, _ := m.List(context.Background())
	s.hostnames = source.Hostnames{*safetyClaim("TXT", "verification=new")}
	m.createFn = func(_ context.Context, rec provider.Record) error {
		if rec.Target == "verification=new" {
			return errors.New("TXT create rejected")
		}
		return nil
	}
	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.FailedCount() != 1 {
		t.Fatalf("actions: %v", result.Actions)
	}
	safetyRecords(t, m, before...)
}

func TestSafetyTXTCacheExcludesOwnershipName(t *testing.T) {
	data := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeTXT, Target: "verification=value", TTL: 300}
	marker := safetyMarker(data)
	_, _, set, cache := setTestHarness(t, provider.ModeManaged, setTestCapabilities(true, false), []provider.Record{data, marker}, safetyClaim("TXT", data.Target))
	got, ok := cache.getExistingRecords(set.Instance.Name(), marker.Hostname, provider.RecordTypeTXT)
	if !ok || len(got) != 0 {
		t.Fatalf("ownership exposed as data: %v", got)
	}
}

func TestSafetyNoTXTForgetsRemovedType(t *testing.T) {
	s := newTestMockSource("native", *safetyClaim("A", "192.0.2.10"))
	r, m, _ := liveSetHarness(t, setTestCapabilities(false, false), s, []workload.Workload{{ID: "one", Platform: workload.PlatformDocker}})
	safetyReconcile(t, r)
	old, _ := m.List(context.Background())
	s.hostnames = source.Hostnames{*safetyClaim("AAAA", "2001:db8::10")}
	safetyReconcile(t, r)
	// A was intentionally retired. Recreating it manually must not inherit
	// the no-TXT process's obsolete authority on a later cycle.
	m.AddRecord(old[0])
	safetyReconcile(t, r)
	current := desiredRecordForClaim(&s.hostnames[0], r.providers.All()[0])
	safetyRecords(t, m, old[0], current)
}

func TestSafetyTXTSourceRemovalAfterRestart(t *testing.T) {
	s := newTestMockSource("native", *safetyClaim("TXT", "verification=old"))
	r, m, reg := liveSetHarness(t, setTestCapabilities(true, false), s, []workload.Workload{{ID: "one", Platform: workload.PlatformDocker}})
	manual := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeTXT, Target: "v=spf1 -all", TTL: 300}
	m.AddRecord(manual)
	safetyReconcile(t, r)
	s.hostnames = nil
	r = New(r.listers, r.sources, reg, WithConfig(r.config), WithLogger(quietLogger()))
	safetyReconcile(t, r)
	safetyRecords(t, m, manual)
}

func TestSafetyManuallyDeletedAnswerLeavesMarker(t *testing.T) {
	old := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.10", TTL: 300}
	marker := safetyMarker(old)
	r, m, set, cache := setTestHarness(t, provider.ModeManaged, setTestCapabilities(true, false), []provider.Record{marker}, hintedA(old.Hostname, old.Target))
	set.Members = nil
	r.reconcileDesiredSet(context.Background(), set, cache, nil)
	safetyRecords(t, m, marker)
}

func safetyReconcile(t *testing.T, r *Reconciler) *Result {
	t.Helper()
	result, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return result
}
