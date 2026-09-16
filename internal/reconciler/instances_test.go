package reconciler

import (
	"context"
	"errors"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
	native "github.com/maxfield-allison/dnsweaver/sources/dnsweaver"
	"github.com/maxfield-allison/dnsweaver/sources/traefik"
)

func instanceSelectionHarness(t *testing.T) (*Reconciler, *testMockWorkloadLister, []*testMockProvider) {
	t.Helper()
	backends := []*testMockProvider{newTestMockProvider("internal"), newTestMockProvider("external")}
	for _, p := range backends {
		identity := provider.ProviderIdentity{Type: "mock", Endpoint: p.Name(), Zone: "example.com"}
		p.identity = &identity
	}
	providers := testProviderRegistry(quietLogger(), backends...)
	providers.SetInstanceID("test-instance")
	for _, name := range []string{"internal", "external"} {
		if err := providers.CreateInstance(provider.ProviderInstanceConfig{Name: name, TypeName: "mock", RecordType: provider.RecordTypeA,
			Target: "192.0.2.1", TTL: 300, Mode: provider.ModeManaged, Domains: []string{"*.example.com"}}); err != nil {
			t.Fatal(err)
		}
	}
	sources := source.NewRegistry(quietLogger())
	if err := sources.Register(traefik.New()); err != nil {
		t.Fatal(err)
	}
	if err := sources.Register(native.New()); err != nil {
		t.Fatal(err)
	}
	lister := newTestMockWorkloadLister(workload.PlatformDocker)
	lister.workloads = []workload.Workload{{ID: "app", Name: "app", Platform: workload.PlatformDocker, Labels: map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.example.com`)",
		"dnsweaver.instances":           "internal,external",
	}}}
	cfg := DefaultConfig()
	cfg.InstanceID = "test-instance"
	return New([]workload.Lister{lister}, sources, providers, WithConfig(cfg), WithLogger(quietLogger())), lister, backends
}

func TestInstancesRouteChangePreservesSiblingsLiveAndAfterRestart(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "restart"}[restart], func(t *testing.T) {
			r, lister, backends := instanceSelectionHarness(t)
			manual := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.99", TTL: 300}
			backends[1].AddRecord(manual)
			if got := safetyReconcile(t, r); len(got.Created()) != 2 {
				t.Fatalf("initial creates: %v", got.Actions)
			}
			if got := safetyReconcile(t, r); len(got.Created())+len(got.Updated())+len(got.Deleted()) != 0 {
				t.Fatalf("unchanged writes: %v", got.Actions)
			}
			lister.workloads[0].Labels["dnsweaver.instances"] = "internal"
			if restart {
				r = New(r.listers, r.sources, r.providers, WithConfig(r.config), WithLogger(quietLogger()))
			}
			got := safetyReconcile(t, r)
			if len(got.Deleted()) != 1 || got.Deleted()[0].Provider != "external" || got.Deleted()[0].Target != "192.0.2.1" {
				t.Fatalf("route removal: %v", got.Actions)
			}
			safetyRecords(t, backends[1], manual)
			lister.workloads[0].Labels["dnsweaver.instances"] = "internal,external"
			if got := safetyReconcile(t, r); len(got.Created()) != 1 || got.Created()[0].Provider != "external" {
				t.Fatalf("route restoration: %v", got.Actions)
			}
		})
	}
}

func TestInvalidInstancesNeverAuthorizeRemoval(t *testing.T) {
	for _, selection := range []string{"", "internal,", "internal,unknown", "unknown"} {
		for _, restart := range []bool{false, true} {
			t.Run(selection+map[bool]string{false: "/live", true: "/restart"}[restart], func(t *testing.T) {
				r, lister, backends := instanceSelectionHarness(t)
				safetyReconcile(t, r)
				before := make([][]provider.Record, len(backends))
				for i, p := range backends {
					before[i], _ = p.List(context.Background())
				}
				lister.workloads[0].Labels["dnsweaver.instances"] = selection
				if restart {
					r = New(r.listers, r.sources, r.providers, WithConfig(r.config), WithLogger(quietLogger()))
				}
				got := safetyReconcile(t, r)
				if len(got.Created())+len(got.Updated())+len(got.Deleted()) != 0 {
					t.Fatalf("invalid routing mutated records: %v", got.Actions)
				}
				for i, p := range backends {
					safetyRecords(t, p, before[i]...)
				}
			})
		}
	}
}

func TestInstancesNamedRecordOverrideAndAbsentSelection(t *testing.T) {
	r, lister, _ := instanceSelectionHarness(t)
	labels := lister.workloads[0].Labels
	labels["dnsweaver.instances"] = "internal"
	labels["dnsweaver.records.app.hostname"] = "app.example.com"
	labels["dnsweaver.records.app.provider"] = "external"
	got := safetyReconcile(t, r)
	if len(got.Created()) != 1 || got.Created()[0].Provider != "external" {
		t.Fatalf("named override: %v", got.Actions)
	}
	delete(labels, "dnsweaver.records.app.provider")
	delete(labels, "dnsweaver.instances")
	got = safetyReconcile(t, r)
	if got.DesiredMembers != 2 || len(got.Created()) != 1 || got.Created()[0].Provider != "internal" {
		t.Fatalf("absent selection: %v", got.Actions)
	}
}

func TestInstancesUnavailableDestinationPreservesOldRoute(t *testing.T) {
	for _, failure := range []error{errors.New("authentication failed"), context.DeadlineExceeded} {
		r, lister, backends := instanceSelectionHarness(t)
		lister.workloads[0].Labels["dnsweaver.instances"] = "internal"
		safetyReconcile(t, r)
		before, _ := backends[0].List(context.Background())
		lister.workloads[0].Labels["dnsweaver.instances"] = "external"
		backends[1].listErr = failure
		got := safetyReconcile(t, r)
		if len(got.Deleted()) != 0 {
			t.Fatalf("unavailable destination removed source: %v", got.Actions)
		}
		safetyRecords(t, backends[0], before...)
		backends[1].listErr = nil
		got = safetyReconcile(t, r)
		if len(got.Deleted()) != 1 || len(got.Created()) != 1 {
			t.Fatalf("recovered route did not converge: %v", got.Actions)
		}
	}
}

func TestInstancesPartialDiscoveryPreservesDeselectedRoute(t *testing.T) {
	r, lister, backends := instanceSelectionHarness(t)
	safetyReconcile(t, r)
	before, _ := backends[1].List(context.Background())
	failed := newTestMockSource("partial")
	failed.err = errors.New("incomplete discovery")
	if err := r.sources.Register(failed); err != nil {
		t.Fatal(err)
	}
	lister.workloads[0].Labels["dnsweaver.instances"] = "internal"
	got := safetyReconcile(t, r)
	if len(got.Deleted()) != 0 {
		t.Fatalf("partial discovery removed route: %v", got.Actions)
	}
	safetyRecords(t, backends[1], before...)
}

func TestInstancesRespectDomainAndEntrypointScope(t *testing.T) {
	for _, hostname := range []string{"app.allowed.example", "outside.denied.example"} {
		for _, entrypoint := range []string{"internal", "external"} {
			mock := newTestMockProvider("restricted")
			providers := testProviderRegistry(quietLogger(), mock)
			if err := providers.CreateInstance(provider.ProviderInstanceConfig{Name: "restricted", TypeName: "mock", RecordType: provider.RecordTypeA,
				Target: "192.0.2.1", TTL: 300, Domains: []string{"*.allowed.example"}, MetadataFilters: map[string][]string{"traefik.entrypoint": {"internal"}}}); err != nil {
				t.Fatal(err)
			}
			r := New(nil, source.NewRegistry(quietLogger()), providers, WithLogger(quietLogger()))
			claim := &source.Hostname{Name: hostname, Instances: []string{"restricted"}, Metadata: map[string]string{"traefik.entrypoint": entrypoint}}
			got := r.compileDesiredRecordSets([]*source.Hostname{claim})
			allowed := hostname == "app.allowed.example" && entrypoint == "internal"
			if (len(got.Sets) == 1) != allowed || got.RoutingComplete != allowed {
				t.Fatalf("scope %s %s: %+v", hostname, entrypoint, got)
			}
		}
	}
}

func TestInstancesFailedDestinationWritePreservesOldRoute(t *testing.T) {
	for _, restart := range []bool{false, true} {
		for _, failType := range []provider.RecordType{provider.RecordTypeA, provider.RecordTypeTXT} {
			t.Run(string(failType)+map[bool]string{false: "/live", true: "/restart"}[restart], func(t *testing.T) {
				r, lister, backends := instanceSelectionHarness(t)
				lister.workloads[0].Labels["dnsweaver.instances"] = "internal"
				safetyReconcile(t, r)
				before, _ := backends[0].List(context.Background())
				lister.workloads[0].Labels["dnsweaver.instances"] = "external"
				if restart {
					r = New(r.listers, r.sources, r.providers, WithConfig(r.config), WithLogger(quietLogger()))
				}
				backends[1].createFn = func(_ context.Context, record provider.Record) error {
					if record.Type == failType {
						return errors.New("write rejected")
					}
					return nil
				}
				got := safetyReconcile(t, r)
				if len(got.Deleted()) != 0 {
					t.Fatalf("failed destination removed source: %v", got.Actions)
				}
				safetyRecords(t, backends[0], before...)
				backends[1].createFn = nil
				if failType == provider.RecordTypeTXT {
					// A record created without durable ownership cannot be
					// silently adopted on the next cycle. Keep the old route
					// until the operator repairs ownership at the destination.
					if got := safetyReconcile(t, r); len(got.Deleted()) != 0 {
						t.Fatalf("unowned destination retired old route: %v", got.Actions)
					}
					record := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.1", TTL: 300}
					backends[1].AddRecord(provider.MemberOwnershipRecord(record.Hostname, 300, "test-instance", record, nil))
				}
				got = safetyReconcile(t, r)
				if len(got.Deleted()) != 1 {
					t.Fatalf("recovered destination did not retire old route: %v", got.Actions)
				}
			})
		}
	}
}

func TestInstancesUnknownSelectionCannotBeBypassedByRecordOverride(t *testing.T) {
	r, lister, _ := instanceSelectionHarness(t)
	labels := lister.workloads[0].Labels
	labels["dnsweaver.instances"] = "internal,typo"
	labels["dnsweaver.records.app.hostname"] = "app.example.com"
	labels["dnsweaver.records.app.provider"] = "external"
	if got := safetyReconcile(t, r); got.DesiredMembers != 0 || len(got.Created()) != 0 {
		t.Fatalf("unknown selection accepted: %v", got.Actions)
	}
}

func TestInstancesKubernetesAnnotationRoutesNativeHostnames(t *testing.T) {
	r, lister, _ := instanceSelectionHarness(t)
	lister.workloads = []workload.Workload{{ID: "service", Platform: workload.PlatformKubernetes, Annotations: map[string]string{
		"dnsweaver.dev/hostname":  "service.example.com",
		"dnsweaver.dev/instances": "internal",
	}}}
	got := safetyReconcile(t, r)
	if len(got.Created()) != 1 || got.Created()[0].Provider != "internal" {
		t.Fatalf("annotation routing: %v", got.Actions)
	}
}
