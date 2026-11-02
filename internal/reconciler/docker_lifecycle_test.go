package reconciler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dockerclient "github.com/maxfield-allison/dnsweaver/internal/docker"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
	dnsweaversource "github.com/maxfield-allison/dnsweaver/sources/dnsweaver"
)

func TestReconcile_DockerContainerLifecycle(t *testing.T) {
	for _, current := range []string{"created", "running", "paused", "restarting", "exited", "removing", "dead"} {
		t.Run(current, func(t *testing.T) {
			for _, cleanupOnStop := range []bool{false, true} {
				name := "keep records until removal"
				if cleanupOnStop {
					name = "clean up non-running containers"
				}
				t.Run(name, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					r, backend, state := dockerLifecycleFixture(ctx, t, cleanupOnStop)
					if _, err := r.Reconcile(ctx); err != nil {
						t.Fatal(err)
					}
					assertDockerLifecycleRecord(ctx, t, backend, true)

					state.Store(current)
					if _, err := r.Reconcile(ctx); err != nil {
						t.Fatal(err)
					}
					want := !cleanupOnStop || current == "running" || current == "paused" || current == "restarting"
					assertDockerLifecycleRecord(ctx, t, backend, want)

					state.Store("running")
					if _, err := r.Reconcile(ctx); err != nil {
						t.Fatal(err)
					}
					assertDockerLifecycleRecord(ctx, t, backend, true)

					state.Store("removed")
					if _, err := r.Reconcile(ctx); err != nil {
						t.Fatal(err)
					}
					remaining, err := backend.List(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if len(remaining) != 0 {
						t.Fatalf("removed container left DNS or ownership records: %+v", remaining)
					}
				})
			}
		})
	}
}

// The API fixture applies the SDK's actual status filter. A restarting
// container still exists; only the removed state disappears unconditionally.
func dockerLifecycleFixture(ctx context.Context, t *testing.T, cleanupOnStop bool) (*Reconciler, *testMockProvider, *atomic.Value) {
	t.Helper()
	state := &atomic.Value{}
	state.Store("running")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(req.URL.Path, "/_ping"):
			w.Header().Set("API-Version", "1.52")
			_, _ = w.Write([]byte("OK"))
		case strings.HasSuffix(req.URL.Path, "/info"):
			_, _ = w.Write([]byte(`{"Swarm":{"LocalNodeState":"inactive"}}`))
		case strings.HasSuffix(req.URL.Path, "/containers/json"):
			var filters map[string]map[string]bool
			raw := req.URL.Query().Get("filters")
			if raw != "" {
				if err := json.Unmarshal([]byte(raw), &filters); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
			}
			current := state.Load().(string)
			all := req.URL.Query().Get("all") == "1" || req.URL.Query().Get("all") == "true"
			running := current == "running" || current == "paused" || current == "restarting"
			if current == "removed" || len(filters["status"]) == 0 && !all && !running || len(filters["status"]) > 0 && !filters["status"][current] {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_ = json.NewEncoder(w).Encode([]any{map[string]any{
				"Id": "crash", "Names": []string{"/crash"}, "State": current,
				"Labels": map[string]string{"dnsweaver.hostname": "crash.example.com"},
			}})
		default:
			t.Errorf("unexpected Docker endpoint %s", req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	for _, key := range []string{"DOCKER_HOST", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH"} {
		t.Setenv(key, "")
	}
	c, err := dockerclient.NewClient(ctx, dockerclient.WithHost(server.URL), dockerclient.WithMode(dockerclient.ModeStandalone), dockerclient.WithCleanupOnStop(cleanupOnStop), dockerclient.WithConnectTimeout(0), dockerclient.WithLogger(quietLogger()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	backend := newTestMockProvider("dns")
	providers := testProviderRegistry(quietLogger(), backend)
	if err := providers.CreateInstance(provider.ProviderInstanceConfig{
		Name: "dns", TypeName: "mock", RecordType: provider.RecordTypeA,
		Target: "192.0.2.1", TTL: 300, Domains: []string{"*.example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	sources := source.NewRegistry(quietLogger())
	sources.Register(dnsweaversource.New(dnsweaversource.WithLogger(quietLogger())))
	r := New([]workload.Lister{dockerclient.NewWorkloadListerAdapter(c)}, sources, providers, WithConfig(DefaultConfig()), WithLogger(quietLogger()))
	return r, backend, state
}

func assertDockerLifecycleRecord(ctx context.Context, t *testing.T, backend provider.Provider, want bool) {
	t.Helper()
	records, err := backend.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	present := false
	for _, record := range records {
		if record.Hostname == "crash.example.com" && record.Type == provider.RecordTypeA && record.Target == "192.0.2.1" {
			present = true
		}
	}
	if present != want {
		t.Fatalf("container DNS present = %v, want %v; records=%+v", present, want, records)
	}
}
