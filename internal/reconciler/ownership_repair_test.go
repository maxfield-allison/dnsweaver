package reconciler

import (
	"context"
	"fmt"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

func ownershipRepairHarness(t *testing.T) (*Reconciler, *testMockProvider, *provider.Registry) {
	t.Helper()
	s := newTestMockSource("native", source.Hostname{Name: "app.example.com", Source: "native", RecordHints: &source.RecordHints{Type: "CNAME", Target: "target.example.com"}})
	r, backend, registry := liveSetHarness(t, setTestCapabilities(true, false), s, []workload.Workload{{ID: "one", Name: "one", Platform: workload.PlatformDocker}})
	compact, err := provider.NewCompactOwnershipProvider(backend, "test-instance", "example.com", quietLogger(), nil)
	if err != nil {
		t.Fatal(err)
	}
	instance, _ := registry.Get("dns")
	instance.Provider = compact
	backend.createFn = func(_ context.Context, record provider.Record) error {
		if provider.IsOwnershipStateRecord(record.Hostname) {
			return fmt.Errorf("%w: ownership changed while reading", provider.ErrOwnershipState)
		}
		return nil
	}
	first, err := r.Reconcile(context.Background())
	if err != nil || !first.HasErrors() || len(backend.GetCreatedDNSRecords()) != 1 || len(r.pendingMemberOwnership) != 1 {
		t.Fatalf("confirmed answer and failed publication must retain one repair: result=%+v err=%v", first, err)
	}
	backend.createFn = nil
	return r, backend, registry
}

func TestMemberOwnershipRepairAfterConfirmedCreate(t *testing.T) {
	r, backend, registry := ownershipRepairHarness(t)
	second, err := r.Reconcile(context.Background())
	if err != nil || second.HasErrors() || len(r.pendingMemberOwnership) != 0 {
		t.Fatalf("later reconciliation did not finish ownership: actions=%+v err=%v", second.Actions, err)
	}
	if len(backend.GetCreatedDNSRecords()) != 1 {
		t.Fatal("repair repeated the data write")
	}
	instance, _ := registry.Get("dns")
	owned, err := instance.RecoverOwnedHostnames(context.Background())
	if err != nil || len(owned) != 1 || owned[0].Hostname != "app.example.com" {
		t.Fatalf("repaired ownership was not durable: owned=%+v err=%v", owned, err)
	}
}

func TestMemberOwnershipRepairDoesNotSurviveRestart(t *testing.T) {
	r, backend, registry := ownershipRepairHarness(t)
	restarted := New(r.listers, r.sources, registry, WithConfig(r.config), WithLogger(quietLogger()))
	if _, err := restarted.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	instance, _ := registry.Get("dns")
	owned, err := instance.RecoverOwnedHostnames(context.Background())
	if err != nil || len(owned) != 0 || len(backend.GetCreatedDNSRecords()) != 1 {
		t.Fatal("a restarted process adopted an unrelated unclaimed answer", err)
	}
}

func TestMemberOwnershipRepairPreservesForeignClaim(t *testing.T) {
	r, backend, registry := ownershipRepairHarness(t)
	foreign := provider.OwnershipRecord("app.example.com", 300, "offline-owner", nil)
	backend.AddRecord(foreign)
	result, err := r.Reconcile(context.Background())
	if err != nil || !result.HasErrors() || len(r.pendingMemberOwnership) != 1 {
		t.Fatalf("foreign ambiguous claim must block repair: actions=%+v err=%v", result.Actions, err)
	}
	instance, _ := registry.Get("dns")
	owned, err := instance.RecoverOwnedHostnames(context.Background())
	if err != nil || len(owned) != 0 || len(backend.GetDeleted()) != 0 {
		t.Fatal("repair acquired ownership or deleted a foreign record", err)
	}
}

func TestMemberOwnershipRepairRetainsFailureAndHandlesWithdrawal(t *testing.T) {
	r, backend, registry := ownershipRepairHarness(t)
	backend.createFn = func(_ context.Context, record provider.Record) error {
		if provider.IsOwnershipStateRecord(record.Hostname) {
			return fmt.Errorf("%w: ownership changed while reading", provider.ErrOwnershipState)
		}
		return nil
	}
	failed, err := r.Reconcile(context.Background())
	if err != nil || !failed.HasErrors() || len(r.pendingMemberOwnership) != 1 || len(backend.GetCreatedDNSRecords()) != 1 {
		t.Fatal("unresolved repair reported success or lost its intent", err)
	}
	backend.createFn = nil
	sourceMock, ok := r.sources.Get("native").(*testMockSource)
	if !ok {
		t.Fatal("fixture source missing")
	}
	sourceMock.hostnames = nil
	withdrawn, err := r.Reconcile(context.Background())
	if err != nil || withdrawn.HasErrors() || len(r.pendingMemberOwnership) != 0 {
		t.Fatalf("withdrawal did not finish repair and retire the answer: actions=%+v err=%v", withdrawn.Actions, err)
	}
	instance, _ := registry.Get("dns")
	owned, err := instance.RecoverOwnedHostnames(context.Background())
	if err != nil || len(owned) != 0 || len(withdrawn.Deleted()) != 1 {
		t.Fatal("withdrawal left an unclaimed answer or a live claim", err)
	}
}

func TestMemberOwnershipRepairPreservesDryRunAndChangedData(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			r, backend, registry := ownershipRepairHarness(t)
			before := len(backend.GetCreated())
			if changed {
				backend.records[0].Target = "operator.example.com"
			} else {
				r.SetDryRun(true)
			}
			if _, err := r.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(backend.GetCreated()) != before || len(backend.GetDeleted()) != 0 {
				t.Fatal("repair changed dry-run or externally replaced data")
			}
			if changed {
				instance, _ := registry.Get("dns")
				owned, err := instance.RecoverOwnedHostnames(context.Background())
				if err != nil || len(owned) != 0 || len(r.pendingMemberOwnership) != 0 {
					t.Fatal("changed data retained local ownership authority", err)
				}
			} else if len(r.pendingMemberOwnership) != 1 {
				t.Fatal("dry-run discarded pending repair")
			}
		})
	}
}
