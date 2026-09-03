package reconciler

import (
	"context"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

func TestExtractClaims_PreservesDualStackAndCountsUniqueHostnames(t *testing.T) {
	sources := testSourceRegistry(quietLogger(), newTestMockSource("proxmox",
		source.Hostname{Name: "vm.example.com", Source: "proxmox", RecordHints: &source.RecordHints{Type: "A", Target: "192.0.2.10"}},
		source.Hostname{Name: "vm.example.com", Source: "proxmox", RecordHints: &source.RecordHints{Type: "AAAA", Target: "2001:db8::10"}},
	))
	r := New(nil, sources, testProviderRegistry(quietLogger()), WithLogger(quietLogger()))
	result := NewResult(false)
	extracted := r.extractClaims(context.Background(), []workload.Workload{{Name: "vm", Platform: workload.PlatformProxmox}}, result)
	if !extracted.Complete {
		t.Fatal("snapshot unexpectedly incomplete")
	}
	if len(extracted.Claims) != 2 {
		t.Fatalf("claims = %+v, want A and AAAA", extracted.Claims)
	}
	if len(extracted.Unique) != 1 {
		t.Fatalf("unique hostnames = %d, want 1", len(extracted.Unique))
	}
}

func TestExtractClaims_CountsDuplicateHostnameOncePerWorkload(t *testing.T) {
	sources := testSourceRegistry(quietLogger(), newTestMockSource("proxmox",
		source.Hostname{Name: "vm.example.com", Source: "proxmox", RecordHints: &source.RecordHints{Type: "A", Target: "192.0.2.10"}},
		source.Hostname{Name: "vm.example.com", Source: "proxmox", RecordHints: &source.RecordHints{Type: "AAAA", Target: "2001:db8::10"}},
	))
	r := New(nil, sources, testProviderRegistry(quietLogger()), WithLogger(quietLogger()))
	result := NewResult(false)
	extracted := r.extractClaims(context.Background(), []workload.Workload{
		{Name: "first", Platform: workload.PlatformProxmox},
		{Name: "second", Platform: workload.PlatformProxmox},
	}, result)
	if len(extracted.Claims) != 4 {
		t.Fatalf("claims = %d, want all four member claims", len(extracted.Claims))
	}
	if result.HostnamesDuplicate != 1 {
		t.Fatalf("HostnamesDuplicate = %d, want one duplicate hostname", result.HostnamesDuplicate)
	}
}

func TestSelectClaimsWithinWorkload_RecordHintSupersedesBareClaim(t *testing.T) {
	hostnames := source.Hostnames{
		{Name: "app.example.com", Source: "traefik", Metadata: map[string]string{"traefik.entrypoint": "web"}},
		{Name: "app.example.com", Source: "native", RecordHints: &source.RecordHints{Type: "A", Target: "192.0.2.10"}},
	}
	selected := selectClaimsWithinWorkload(hostnames)
	if len(selected) != 1 || selected[0].RecordHints == nil || selected[0].RecordHints.Target != "192.0.2.10" {
		t.Fatalf("selected = %+v, want hinted claim only", selected)
	}
	if selected[0].Metadata["traefik.entrypoint"] != "web" {
		t.Fatalf("metadata = %v, want entrypoint preserved", selected[0].Metadata)
	}
}

func TestSelectClaimsWithinWorkload_PreservesDistinctTargets(t *testing.T) {
	hostnames := source.Hostnames{
		{Name: "app.example.com", Source: "native", RecordHints: &source.RecordHints{Type: "A", Target: "192.0.2.10"}},
		{Name: "app.example.com", Source: "native", RecordHints: &source.RecordHints{Type: "A", Target: "192.0.2.11"}},
	}
	if selected := selectClaimsWithinWorkload(hostnames); len(selected) != 2 {
		t.Fatalf("selected = %+v, want two distinct members", selected)
	}
}

func TestSelectClaimsWithinWorkload_BareMetadataReachesEveryDistinctMember(t *testing.T) {
	hostnames := source.Hostnames{
		{Name: "app.example.com", Source: "native", RecordHints: &source.RecordHints{Type: "A", Target: "192.0.2.10"}},
		{Name: "app.example.com", Source: "native", RecordHints: &source.RecordHints{Type: "A", Target: "192.0.2.11"}},
		{Name: "app.example.com", Source: "traefik", Metadata: map[string]string{"traefik.entrypoint": "web"}},
	}
	selected := selectClaimsWithinWorkload(hostnames)
	if len(selected) != 2 {
		t.Fatalf("selected = %+v, want two distinct members", selected)
	}
	for _, claim := range selected {
		if claim.Metadata["traefik.entrypoint"] != "web" {
			t.Fatalf("metadata = %v, want entrypoint on every member", claim.Metadata)
		}
	}
}
