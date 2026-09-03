package source

import (
	"context"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

func TestRegistry_WorkloadAdoptionHintAppliesAcrossSources(t *testing.T) {
	r := NewRegistry(testLogger())
	_ = r.Register(&mockSource{name: "traefik", hostnames: []Hostname{{Name: "app.example.com", Source: "traefik"}}})

	hostnames := r.ExtractAll(context.Background(), workload.Workload{
		Name:     "app",
		Platform: workload.PlatformDocker,
		Labels:   map[string]string{"dnsweaver.adopt": "true"},
	})
	if len(hostnames) != 1 || hostnames[0].AdoptExisting == nil || !*hostnames[0].AdoptExisting {
		t.Fatalf("workload adoption hint was not applied: %+v", hostnames)
	}
}

func TestRegistry_RecordAdoptionHintOverridesWorkloadHint(t *testing.T) {
	recordAdopt := false
	r := NewRegistry(testLogger())
	_ = r.Register(&mockSource{name: "dnsweaver", hostnames: []Hostname{{
		Name:        "app.example.com",
		Source:      "dnsweaver",
		RecordHints: &RecordHints{AdoptExisting: &recordAdopt},
	}}})

	hostnames := r.ExtractAll(context.Background(), workload.Workload{
		Platform: workload.PlatformDocker,
		Labels:   map[string]string{"dnsweaver.adopt": "true"},
	})
	if got := hostnames[0].RecordHints.AdoptExisting; got == nil || *got {
		t.Fatalf("record adoption hint = %v, want false", got)
	}
}

func TestRegistry_KubernetesAdoptionAnnotation(t *testing.T) {
	r := NewRegistry(testLogger())
	_ = r.Register(&mockSource{name: "kubernetes", hostnames: []Hostname{{Name: "app.example.com", Source: "kubernetes"}}})

	hostnames := r.ExtractAll(context.Background(), workload.Workload{
		Platform:    workload.PlatformKubernetes,
		Annotations: map[string]string{"dnsweaver.dev/adopt": "false"},
	})
	if got := hostnames[0].AdoptExisting; got == nil || *got {
		t.Fatalf("annotation adoption hint = %v, want false", got)
	}
}

func TestRegistry_InvalidAdoptionHintIsIgnored(t *testing.T) {
	r := NewRegistry(testLogger())
	_ = r.Register(&mockSource{name: "traefik", hostnames: []Hostname{{Name: "app.example.com", Source: "traefik"}}})

	hostnames := r.ExtractAll(context.Background(), workload.Workload{
		Platform: workload.PlatformDocker,
		Labels:   map[string]string{"dnsweaver.adopt": "yes"},
	})
	if hostnames[0].AdoptExisting != nil {
		t.Fatalf("invalid hint should be ignored, got %v", hostnames[0].AdoptExisting)
	}
}
