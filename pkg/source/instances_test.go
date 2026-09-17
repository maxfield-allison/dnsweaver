package source

import (
	"context"
	"reflect"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

func TestWorkloadInstances(t *testing.T) {
	for _, tt := range []struct {
		name                string
		labels, annotations map[string]string
		want                []string
		invalid             bool
	}{
		{name: "absent"},
		{name: "one", labels: map[string]string{"dnsweaver.instances": "internal"}, want: []string{"internal"}},
		{name: "normalize", labels: map[string]string{"dnsweaver.instances": " internal, external,internal "}, want: []string{"external", "internal"}},
		{name: "annotation", annotations: map[string]string{"dnsweaver.dev/instances": "internal"}, want: []string{"internal"}},
		{name: "agree", labels: map[string]string{"dnsweaver.instances": "external,internal"}, annotations: map[string]string{"dnsweaver.dev/instances": "internal,external"}, want: []string{"external", "internal"}},
		{name: "conflict", labels: map[string]string{"dnsweaver.instances": "internal"}, annotations: map[string]string{"dnsweaver.dev/instances": "external"}, invalid: true},
		{name: "empty", labels: map[string]string{"dnsweaver.instances": " "}, invalid: true},
		{name: "empty member", labels: map[string]string{"dnsweaver.instances": "internal,,external"}, invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := workloadInstances(workload.Workload{Labels: tt.labels, Annotations: tt.annotations})
			if (err != nil) != tt.invalid || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("selection=%v err=%v, want %v invalid=%v", got, err, tt.want, tt.invalid)
			}
		})
	}
}

func TestRegistryInstancesAppliesToEverySourceAndRejectsMalformed(t *testing.T) {
	r := NewRegistry(testLogger())
	for _, name := range []string{"first", "second"} {
		if err := r.Register(&mockSource{name: name, hostnames: Hostnames{{Name: "app.example.com"}}}); err != nil {
			t.Fatal(err)
		}
	}
	w := workload.Workload{Labels: map[string]string{"dnsweaver.instances": "internal"}}
	hosts, complete := r.ExtractAllWithStatus(context.Background(), w)
	if !complete || len(hosts) != 2 {
		t.Fatalf("hosts=%v complete=%v", hosts, complete)
	}
	for _, h := range hosts {
		if !reflect.DeepEqual(h.Instances, []string{"internal"}) {
			t.Fatalf("selection missing: %+v", h)
		}
	}
	w.Labels["dnsweaver.instances"] = "internal,"
	hosts, complete = r.ExtractAllWithStatus(context.Background(), w)
	if complete || len(hosts) != 0 {
		t.Fatalf("invalid selection accepted: %v %v", hosts, complete)
	}
	if _, err := r.ExtractFrom(context.Background(), "first", w); err == nil {
		t.Fatal("ExtractFrom accepted invalid selection")
	}
}

func TestRegistrySelectionDoesNotMutateSourceBuffer(t *testing.T) {
	src := &mockSource{name: "reused", hostnames: Hostnames{{Name: "app.example.com"}}}
	r := NewRegistry(testLogger())
	if err := r.Register(src); err != nil {
		t.Fatal(err)
	}
	first, err := r.ExtractFrom(context.Background(), "reused", workload.Workload{Labels: map[string]string{"dnsweaver.instances": "internal"}})
	if err != nil {
		t.Fatal(err)
	}
	second, complete := r.ExtractAllWithStatus(context.Background(), workload.Workload{})
	if !complete || len(second) != 1 || second[0].Instances != nil || src.hostnames[0].Instances != nil {
		t.Fatalf("selection leaked into another workload: %v", second)
	}
	if !reflect.DeepEqual(first[0].Instances, []string{"internal"}) {
		t.Fatalf("later extraction changed first result: %v", first)
	}
}
