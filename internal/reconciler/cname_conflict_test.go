package reconciler

import (
	"context"
	"reflect"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

// A workload asking for an A and another asking for a CNAME at the same name
// is a contradiction DNS cannot represent. Reconciling the A set and then the
// CNAME set used to delete the existing A, with its ownership marker, and
// install the CNAME.
func TestMixedCNAMEAndAddressDesiredStateMakesNoWrites(t *testing.T) {
	api := newFakeUniFiAPI()
	server := api.start(t)
	lister := newTestMockWorkloadLister(workload.PlatformDocker)
	setUniFiWorkload(lister, "A", "192.0.2.10")
	r := newUniFiStateFixture(t, server, lister)
	if result := reconcileUniFi(t, r); result.FailedCount() != 0 {
		t.Fatalf("setup failed: %+v", result.Actions)
	}
	before := unifiCanonicalState(api)
	writes := api.writeCount()

	lister.AddWorkload("alias", unifiRecordLabels("CNAME", "one.example.com"))
	result := reconcileUniFi(t, r)

	if api.writeCount() != writes || !reflect.DeepEqual(unifiCanonicalState(api), before) {
		t.Fatalf("contradictory A+CNAME desired state caused writes: actions=%+v\nstate:\n%s", result.Actions, unifiStateDump(api))
	}
	assertUniFiRecord(t, api, provider.RecordTypeA, "192.0.2.10", true)
	assertMixedTypeSkips(t, result, 2)
}

// After a restart the reconciler has no remembered desired state; the old
// answer is known only through its ownership marker. The contradiction must be
// caught before that marker's empty set can retire the answer.
func TestMixedCNAMEAndAddressDesiredStateAfterRestartMakesNoWrites(t *testing.T) {
	api := newFakeUniFiAPI()
	server := api.start(t)
	lister := newTestMockWorkloadLister(workload.PlatformDocker)
	setUniFiWorkload(lister, "A", "192.0.2.10")
	if result := reconcileUniFi(t, newUniFiStateFixture(t, server, lister)); result.FailedCount() != 0 {
		t.Fatalf("setup failed: %+v", result.Actions)
	}
	before := unifiCanonicalState(api)
	writes := api.writeCount()

	lister.workloads = nil
	lister.AddWorkload("other", unifiRecordLabels("A", "192.0.2.20"))
	lister.AddWorkload("alias", unifiRecordLabels("CNAME", "one.example.com"))
	result := reconcileUniFi(t, newUniFiStateFixture(t, server, lister))

	if api.writeCount() != writes || !reflect.DeepEqual(unifiCanonicalState(api), before) {
		t.Fatalf("restarted reconciler wrote through contradictory desired state: actions=%+v\nstate:\n%s", result.Actions, unifiStateDump(api))
	}
	assertMixedTypeSkips(t, result, 2)
}

// Rejecting one name must not hold back the rest of the cycle.
func TestMixedCNAMERejectionLeavesOtherNamesReconciling(t *testing.T) {
	api := newFakeUniFiAPI()
	server := api.start(t)
	lister := newTestMockWorkloadLister(workload.PlatformDocker)
	setUniFiWorkload(lister, "A", "192.0.2.10")
	r := newUniFiStateFixture(t, server, lister)
	if result := reconcileUniFi(t, r); result.FailedCount() != 0 {
		t.Fatalf("setup failed: %+v", result.Actions)
	}

	lister.AddWorkload("alias", unifiRecordLabels("CNAME", "one.example.com"))
	lister.AddWorkload("neighbor", map[string]string{
		"dnsweaver.records.web.hostname": "web.example.com",
		"dnsweaver.records.web.type":     "A",
		"dnsweaver.records.web.target":   "192.0.2.30",
	})
	result := reconcileUniFi(t, r)

	created := false
	for _, pol := range api.snapshot() {
		if unifiPolicyMatches(pol, provider.RecordTypeA, "web.example.com", "192.0.2.30") {
			created = true
		}
	}
	if !created {
		t.Fatalf("unrelated name was not created alongside the rejected one: actions=%+v\nstate:\n%s", result.Actions, unifiStateDump(api))
	}
	assertUniFiRecord(t, api, provider.RecordTypeA, "192.0.2.10", true)
}

// Once the sources agree again, the name reconciles normally: dropping the
// stray CNAME claim leaves the A untouched, with no delete or recreate.
func TestMixedCNAMERejectionClearsWhenSourcesAgree(t *testing.T) {
	api := newFakeUniFiAPI()
	server := api.start(t)
	lister := newTestMockWorkloadLister(workload.PlatformDocker)
	setUniFiWorkload(lister, "A", "192.0.2.10")
	r := newUniFiStateFixture(t, server, lister)
	if result := reconcileUniFi(t, r); result.FailedCount() != 0 {
		t.Fatalf("setup failed: %+v", result.Actions)
	}
	before := unifiCanonicalState(api)
	lister.AddWorkload("alias", unifiRecordLabels("CNAME", "one.example.com"))
	reconcileUniFi(t, r)

	setUniFiWorkload(lister, "A", "192.0.2.10")
	result := reconcileUniFi(t, r)

	if result.FailedCount() != 0 || result.DeletedCount() != 0 || result.CreatedCount() != 0 {
		t.Fatalf("recovered name was rewritten: actions=%+v", result.Actions)
	}
	if !reflect.DeepEqual(unifiCanonicalState(api), before) {
		t.Fatalf("recovered name changed state:\n%s", unifiStateDump(api))
	}
}

// A and AAAA may share a name, so dual-stack desired state is not rejected.
func TestAddressPairIsNotRejectedAsMixedTypes(t *testing.T) {
	api := newFakeUniFiAPI()
	server := api.start(t)
	lister := newTestMockWorkloadLister(workload.PlatformDocker)
	setUniFiWorkload(lister, "A", "192.0.2.10")
	lister.AddWorkload("v6", unifiRecordLabels("AAAA", "2001:db8::10"))
	r := newUniFiStateFixture(t, server, lister)

	result := reconcileUniFi(t, r)

	if result.FailedCount() != 0 {
		t.Fatalf("dual-stack desired state failed: %+v", result.Actions)
	}
	assertMixedTypeSkips(t, result, 0)
	assertUniFiRecord(t, api, provider.RecordTypeA, "192.0.2.10", true)
	assertUniFiRecord(t, api, provider.RecordTypeAAAA, "2001:db8::10", true)
}

func assertMixedTypeSkips(t *testing.T, result *Result, want int) {
	t.Helper()
	got := 0
	for _, action := range result.Actions {
		if action.Status == StatusSkipped && action.Error == errCNAMEWithOtherTypes {
			got++
		}
	}
	if got != want {
		t.Fatalf("%d mixed-type skips, want %d: actions=%+v", got, want, result.Actions)
	}
}

// A hostname moving to a backend where its sources contradict each other
// publishes nothing there, so the old route must survive the move.
func TestMixedCNAMEAtNewRouteKeepsOldRoute(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "restart"}[restart], func(t *testing.T) {
			r, lister, backends := instanceSelectionHarness(t)
			lister.workloads[0].Labels["dnsweaver.instances"] = "internal"
			safetyReconcile(t, r)
			before, _ := backends[0].List(context.Background())

			lister.workloads[0].Labels["dnsweaver.instances"] = "external"
			lister.workloads = append(lister.workloads, workload.Workload{ID: "alias", Name: "alias", Platform: workload.PlatformDocker, Labels: map[string]string{
				"dnsweaver.records.alias.hostname": "app.example.com",
				"dnsweaver.records.alias.type":     "CNAME",
				"dnsweaver.records.alias.target":   "one.example.com",
				"dnsweaver.records.alias.provider": "external",
			}})
			if restart {
				r = New(r.listers, r.sources, r.providers, WithConfig(r.config), WithLogger(quietLogger()))
			}
			got := safetyReconcile(t, r)

			if len(got.Deleted()) != 0 || len(got.Created()) != 0 {
				t.Fatalf("contradictory destination changed DNS: %v", got.Actions)
			}
			safetyRecords(t, backends[0], before...)
			safetyRecords(t, backends[1])
			assertMixedTypeSkips(t, got, 2)
		})
	}
}

// Recovery from an interrupted replacement waits while the name's sources
// contradict each other, and stays pending for a later cycle.
func TestMixedCNAMEHoldsPendingReplacementRecovery(t *testing.T) {
	r, lister, backends := instanceSelectionHarness(t)
	lister.workloads[0].Labels["dnsweaver.instances"] = "internal"
	safetyReconcile(t, r)
	before, _ := backends[0].List(context.Background())
	internal, _ := r.providers.Get("internal")
	key := replacementKey{internal.Identity, "app.example.com"}
	r.pendingReplacements = map[replacementKey]replacementRecovery{key: {
		Instance: internal,
		Removed:  []provider.Record{{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.50", TTL: 300}},
		Created:  []provider.Record{{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.1", TTL: 300}},
	}}
	lister.workloads = append(lister.workloads, workload.Workload{ID: "alias", Name: "alias", Platform: workload.PlatformDocker, Labels: map[string]string{
		"dnsweaver.records.alias.hostname": "app.example.com",
		"dnsweaver.records.alias.type":     "CNAME",
		"dnsweaver.records.alias.target":   "one.example.com",
		"dnsweaver.records.alias.provider": "internal",
	}})

	got := safetyReconcile(t, r)

	safetyRecords(t, backends[0], before...)
	if _, pending := r.pendingReplacements[key]; !pending {
		t.Fatalf("held recovery was dropped: %v", got.Actions)
	}
	held := false
	for _, action := range got.Actions {
		held = held || action.Error == "replacement recovery held: "+errCNAMEWithOtherTypes
	}
	if !held {
		t.Fatalf("held recovery was not reported: %v", got.Actions)
	}
}
