package reconciler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"

	incusclient "github.com/maxfield-allison/dnsweaver/internal/incus"
	kubernetesclient "github.com/maxfield-allison/dnsweaver/internal/kubernetes"
	proxmoxclient "github.com/maxfield-allison/dnsweaver/internal/proxmox"
	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
	dnsweaversource "github.com/maxfield-allison/dnsweaver/sources/dnsweaver"
	kubernetessource "github.com/maxfield-allison/dnsweaver/sources/kubernetes"
	proxmoxsource "github.com/maxfield-allison/dnsweaver/sources/proxmox"
	traefiksource "github.com/maxfield-allison/dnsweaver/sources/traefik"
)

type discoveryLifecycle struct {
	lister  workload.Lister
	fail    func()
	recover func()
	remove  func()
}

func TestReconcile_FileDiscoveryFailurePreservesDNS(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "crash.yml")
	valid := []byte("http:\n  routers:\n    crash:\n      rule: Host(`crash.example.com`)\n")
	write := func(data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(valid)
	r, backend := discoveryLifecycleReconciler(t, nil)
	r.listers = nil
	sources := source.NewRegistry(quietLogger())
	if err := sources.Register(traefiksource.New(traefiksource.WithFileDiscovery(source.FileDiscoveryConfig{FilePaths: []string{dir}, FilePattern: "*.yml"}), traefiksource.WithLogger(quietLogger()))); err != nil {
		t.Fatal(err)
	}
	r.sources = sources
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	assertDockerLifecycleRecord(ctx, t, backend, true)
	write([]byte("http: ["))
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	assertDockerLifecycleRecord(ctx, t, backend, true)
	write(valid)
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	remaining, err := backend.List(ctx)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("successful empty directory left DNS or ownership: records=%+v, err=%v", remaining, err)
	}
}

func TestReconcile_DiscoveryFailureRetainsDNSUntilConfirmedRemoval(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(context.Context, *testing.T) discoveryLifecycle
	}{
		{name: "proxmox guest lookup", setup: proxmoxDiscoveryLifecycle},
		{name: "incus API read", setup: incusDiscoveryLifecycle},
		{name: "kubernetes cache lifecycle", setup: kubernetesDiscoveryLifecycle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			lifecycle := tc.setup(ctx, t)
			r, backend := discoveryLifecycleReconciler(t, lifecycle.lister)
			if _, err := r.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			assertDockerLifecycleRecord(ctx, t, backend, true)
			before, err := backend.List(ctx)
			if err != nil || len(before) < 2 {
				t.Fatalf("initial DNS and ownership were not created: records=%+v, err=%v", before, err)
			}

			lifecycle.fail()
			if _, err := r.Reconcile(ctx); err == nil {
				records, readErr := backend.List(ctx)
				t.Fatalf("unavailable discovery was reported as success; records=%+v, err=%v", records, readErr)
			}
			assertDockerLifecycleRecord(ctx, t, backend, true)
			after, err := backend.List(ctx)
			if err != nil || len(after) != len(before) {
				t.Fatalf("discovery failure changed DNS or ownership: records=%+v, err=%v", after, err)
			}

			lifecycle.recover()
			if _, err := r.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			assertDockerLifecycleRecord(ctx, t, backend, true)
			lifecycle.remove()
			if _, err := r.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			remaining, err := backend.List(ctx)
			if err != nil || len(remaining) != 0 {
				t.Fatalf("confirmed removal left DNS or ownership: records=%+v, err=%v", remaining, err)
			}
		})
	}
}

func discoveryLifecycleReconciler(t *testing.T, lister workload.Lister) (*Reconciler, *testMockProvider) {
	t.Helper()
	backend := newTestMockProvider("dns")
	providers := testProviderRegistry(quietLogger(), backend)
	if err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name: "dns", TypeName: "mock", RecordType: provider.RecordTypeA,
		Target: "192.0.2.1", TTL: 300, Domains: []string{"*.example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	sources := source.NewRegistry(quietLogger())
	for _, src := range []source.Source{
		dnsweaversource.New(dnsweaversource.WithLogger(quietLogger())),
		kubernetessource.New(kubernetessource.WithLogger(quietLogger())),
		proxmoxsource.New(proxmoxsource.WithDomain("example.com"), proxmoxsource.WithTargetMode(proxmoxsource.TargetModeInstance)),
	} {
		if err := sources.Register(src); err != nil {
			t.Fatal(err)
		}
	}
	return New([]workload.Lister{lister}, sources, providers, WithConfig(DefaultConfig()), WithLogger(quietLogger())), backend
}

func proxmoxDiscoveryLifecycle(_ context.Context, t *testing.T) discoveryLifecycle {
	t.Helper()
	var state atomic.Value
	state.Store("present")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/api2/json/cluster/resources":
			resources := []any{}
			if state.Load() != "removed" {
				resources = append(resources, map[string]any{"vmid": 100, "name": "crash", "node": "node", "type": "lxc", "status": "running"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": resources})
		case "/api2/json/nodes/node/lxc/100/config":
			if state.Load() == "unavailable" {
				http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"net0": "name=eth0,ip=10.0.0.5/24"}})
		default:
			t.Errorf("unexpected Proxmox request: %s", req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	c, err := proxmoxclient.NewClient(proxmoxclient.ClientConfig{BaseURL: server.URL, TokenID: "fixture@pve!dns", TokenSecret: "fixture", TLS: &httputil.TLSConfig{}, Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	return discoveryLifecycle{
		lister: proxmoxclient.NewWorkloadListerAdapter(c, proxmoxclient.AdapterConfig{}, quietLogger()),
		fail:   func() { state.Store("unavailable") }, recover: func() { state.Store("present") }, remove: func() { state.Store("removed") },
	}
}

func incusDiscoveryLifecycle(_ context.Context, t *testing.T) discoveryLifecycle {
	t.Helper()
	var state atomic.Value
	state.Store("present")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/1.0/instances" {
			t.Errorf("unexpected Incus request: %s", req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if state.Load() == "unavailable" {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		instances := []any{}
		if state.Load() != "removed" {
			instances = append(instances, map[string]any{"name": "crash", "type": "container", "status": "Running", "project": "default", "config": map[string]string{"user.label.dnsweaver.hostname": "crash.example.com"}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "sync", "status_code": 200, "metadata": instances})
	}))
	t.Cleanup(server.Close)
	c, err := incusclient.NewClient(incusclient.ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	return discoveryLifecycle{
		lister: incusclient.NewWorkloadListerAdapter(c, incusclient.AdapterConfig{}, quietLogger()),
		fail:   func() { state.Store("unavailable") }, recover: func() { state.Store("present") }, remove: func() { state.Store("removed") },
	}
}

func kubernetesDiscoveryLifecycle(ctx context.Context, t *testing.T) discoveryLifecycle {
	t.Helper()
	c := kubernetesfake.NewClientset(&networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "crash", Namespace: "default"},
		Spec:       networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{Host: "crash.example.com"}}},
	})
	w := kubernetesclient.New(&kubernetesclient.Clients{Typed: c}, kubernetesclient.WithConfig(kubernetesclient.Config{WatchIngress: true}), kubernetesclient.WithLogger(quietLogger()))
	start := func() {
		if err := w.Start(ctx); err != nil {
			t.Fatal(err)
		}
	}
	start()
	t.Cleanup(w.Stop)
	return discoveryLifecycle{
		lister: w, fail: w.Stop, recover: start,
		remove: func() {
			if err := c.NetworkingV1().Ingresses("default").Delete(ctx, "crash", metav1.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
			w.Stop()
			start()
		},
	}
}
