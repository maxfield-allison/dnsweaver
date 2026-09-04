package source

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

// mockSource implements Source for testing.
type mockSource struct {
	name              string
	hostnames         []Hostname
	err               error
	discoverHostnames []Hostname
	discoverErr       error
	supportsDiscovery bool
	platforms         []workload.Platform
}

func (m *mockSource) Name() string { return m.name }

func (m *mockSource) Extract(ctx context.Context, w workload.Workload) ([]Hostname, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.hostnames, nil
}

func (m *mockSource) Discover(ctx context.Context) ([]Hostname, error) {
	if m.discoverErr != nil {
		return nil, m.discoverErr
	}
	return m.discoverHostnames, nil
}

func (m *mockSource) SupportsDiscovery() bool {
	return m.supportsDiscovery
}

func (m *mockSource) SupportedPlatforms() []workload.Platform {
	return m.platforms
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry(testLogger())

	src := &mockSource{name: "test"}
	err := r.Register(src)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if r.Count() != 1 {
		t.Errorf("Count() = %d, want 1", r.Count())
	}

	got := r.Get("test")
	if got != src {
		t.Error("Get returned wrong source")
	}
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	r := NewRegistry(testLogger())

	src1 := &mockSource{name: "dupe"}
	src2 := &mockSource{name: "dupe"}

	if err := r.Register(src1); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	err := r.Register(src2)
	if err == nil {
		t.Error("expected error for duplicate source")
	}

	var dupeErr *DuplicateSourceError
	if !errors.As(err, &dupeErr) {
		t.Errorf("error type = %T, want *DuplicateSourceError", err)
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := NewRegistry(testLogger())

	got := r.Get("nonexistent")
	if got != nil {
		t.Error("Get returned non-nil for missing source")
	}
}

func TestRegistry_All(t *testing.T) {
	r := NewRegistry(testLogger())

	src1 := &mockSource{name: "first"}
	src2 := &mockSource{name: "second"}
	src3 := &mockSource{name: "third"}

	_ = r.Register(src1)
	_ = r.Register(src2)
	_ = r.Register(src3)

	all := r.All()
	if len(all) != 3 {
		t.Fatalf("All() returned %d sources, want 3", len(all))
	}

	// Verify order is preserved
	if all[0].Name() != "first" {
		t.Errorf("all[0].Name() = %q, want %q", all[0].Name(), "first")
	}
	if all[1].Name() != "second" {
		t.Errorf("all[1].Name() = %q, want %q", all[1].Name(), "second")
	}
	if all[2].Name() != "third" {
		t.Errorf("all[2].Name() = %q, want %q", all[2].Name(), "third")
	}
}

func TestRegistry_ExtractAll(t *testing.T) {
	r := NewRegistry(testLogger())

	src1 := &mockSource{
		name: "source1",
		hostnames: []Hostname{
			{Name: "app1.example.com", Source: "source1", Router: "app1"},
		},
	}
	src2 := &mockSource{
		name: "source2",
		hostnames: []Hostname{
			{Name: "app2.example.com", Source: "source2", Router: "app2"},
			{Name: "app3.example.com", Source: "source2", Router: "app3"},
		},
	}

	_ = r.Register(src1)
	_ = r.Register(src2)

	labels := map[string]string{"some": "labels"}
	w := workload.Workload{Labels: labels, Platform: workload.PlatformDocker}
	hostnames := r.ExtractAll(context.Background(), w)

	if len(hostnames) != 3 {
		t.Fatalf("ExtractAll returned %d hostnames, want 3", len(hostnames))
	}

	// Verify order matches source registration order
	wantNames := []string{"app1.example.com", "app2.example.com", "app3.example.com"}
	for i, want := range wantNames {
		if hostnames[i].Name != want {
			t.Errorf("hostnames[%d].Name = %q, want %q", i, hostnames[i].Name, want)
		}
	}
}

func TestRegistry_ExtractAll_WithErrors(t *testing.T) {
	r := NewRegistry(testLogger())

	src1 := &mockSource{
		name: "good1",
		hostnames: []Hostname{
			{Name: "good1.example.com", Source: "good1"},
		},
	}
	src2 := &mockSource{
		name: "bad",
		err:  errors.New("parse error"),
	}
	src3 := &mockSource{
		name: "good2",
		hostnames: []Hostname{
			{Name: "good2.example.com", Source: "good2"},
		},
	}

	_ = r.Register(src1)
	_ = r.Register(src2)
	_ = r.Register(src3)

	// Should continue extraction despite error in middle source
	w := workload.Workload{Platform: workload.PlatformDocker}
	hostnames := r.ExtractAll(context.Background(), w)

	if len(hostnames) != 2 {
		t.Fatalf("ExtractAll returned %d hostnames, want 2", len(hostnames))
	}

	if hostnames[0].Name != "good1.example.com" {
		t.Errorf("hostnames[0].Name = %q, want %q", hostnames[0].Name, "good1.example.com")
	}
	if hostnames[1].Name != "good2.example.com" {
		t.Errorf("hostnames[1].Name = %q, want %q", hostnames[1].Name, "good2.example.com")
	}

	statusHostnames, complete := r.ExtractAllWithStatus(context.Background(), w)
	if complete {
		t.Fatal("ExtractAllWithStatus reported a complete snapshot after a source failed")
	}
	if len(statusHostnames) != 2 {
		t.Fatalf("ExtractAllWithStatus returned %d hostnames, want 2 partial results", len(statusHostnames))
	}
}

func TestRegistry_ExtractAll_Empty(t *testing.T) {
	r := NewRegistry(testLogger())

	// No sources registered
	w := workload.Workload{Platform: workload.PlatformDocker}
	hostnames := r.ExtractAll(context.Background(), w)
	if len(hostnames) != 0 {
		t.Errorf("ExtractAll returned %d hostnames, want 0", len(hostnames))
	}
}

func TestRegistry_ExtractFrom(t *testing.T) {
	r := NewRegistry(testLogger())

	src := &mockSource{
		name: "specific",
		hostnames: []Hostname{
			{Name: "app.example.com", Source: "specific"},
		},
	}

	_ = r.Register(src)

	w := workload.Workload{Platform: workload.PlatformDocker}
	hostnames, err := r.ExtractFrom(context.Background(), "specific", w)
	if err != nil {
		t.Fatalf("ExtractFrom failed: %v", err)
	}

	if len(hostnames) != 1 {
		t.Fatalf("ExtractFrom returned %d hostnames, want 1", len(hostnames))
	}
}

func TestRegistry_ExtractFrom_NotFound(t *testing.T) {
	r := NewRegistry(testLogger())

	w := workload.Workload{Platform: workload.PlatformDocker}
	_, err := r.ExtractFrom(context.Background(), "nonexistent", w)
	if err == nil {
		t.Error("expected error for missing source")
	}

	var notFoundErr *SourceNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Errorf("error type = %T, want *SourceNotFoundError", err)
	}
}

func TestRegistry_DiscoverAll(t *testing.T) {
	r := NewRegistry(testLogger())

	// Source with discovery enabled
	srcWithDiscovery := &mockSource{
		name:              "with-discovery",
		supportsDiscovery: true,
		discoverHostnames: []Hostname{
			{Name: "file1.example.com", Source: "with-discovery"},
			{Name: "file2.example.com", Source: "with-discovery"},
		},
	}

	// Source without discovery (supportsDiscovery = false by default)
	srcNoDiscovery := &mockSource{
		name:      "no-discovery",
		hostnames: []Hostname{{Name: "label.example.com", Source: "no-discovery"}},
	}

	_ = r.Register(srcWithDiscovery)
	_ = r.Register(srcNoDiscovery)

	hostnames := r.DiscoverAll(context.Background())

	// Should only find hostnames from discovery-enabled source
	if len(hostnames) != 2 {
		t.Errorf("DiscoverAll returned %d hostnames, want 2", len(hostnames))
	}

	// Verify both are from the discovery source
	for _, h := range hostnames {
		if h.Source != "with-discovery" {
			t.Errorf("unexpected source %q, want with-discovery", h.Source)
		}
	}
}

func TestRegistry_DiscoverAll_ErrorHandling(t *testing.T) {
	r := NewRegistry(testLogger())

	srcOk := &mockSource{
		name:              "ok",
		supportsDiscovery: true,
		discoverHostnames: []Hostname{{Name: "ok.example.com", Source: "ok"}},
	}

	srcErr := &mockSource{
		name:              "err",
		supportsDiscovery: true,
		discoverErr:       errors.New("discovery failed"),
	}

	_ = r.Register(srcOk)
	_ = r.Register(srcErr)

	// Should continue with remaining sources after error
	hostnames := r.DiscoverAll(context.Background())

	if len(hostnames) != 1 {
		t.Errorf("DiscoverAll returned %d hostnames, want 1 (from ok source)", len(hostnames))
	}

	statusHostnames, complete := r.DiscoverAllWithStatus(context.Background())
	if complete {
		t.Fatal("DiscoverAllWithStatus reported a complete snapshot after a source failed")
	}
	if len(statusHostnames) != 1 {
		t.Fatalf("DiscoverAllWithStatus returned %d hostnames, want 1 partial result", len(statusHostnames))
	}
}

func TestRegistry_DiscoverableSources(t *testing.T) {
	r := NewRegistry(testLogger())

	srcWith := &mockSource{name: "with", supportsDiscovery: true}
	srcWithout := &mockSource{name: "without", supportsDiscovery: false}

	_ = r.Register(srcWith)
	_ = r.Register(srcWithout)

	discoverable := r.DiscoverableSources()

	if len(discoverable) != 1 {
		t.Errorf("DiscoverableSources returned %d sources, want 1", len(discoverable))
	}

	if len(discoverable) > 0 && discoverable[0].Name() != "with" {
		t.Errorf("wrong discoverable source: %s", discoverable[0].Name())
	}
}

func TestRegistry_DiscoverFrom(t *testing.T) {
	r := NewRegistry(testLogger())

	src := &mockSource{
		name:              "test",
		supportsDiscovery: true,
		discoverHostnames: []Hostname{{Name: "discovered.example.com", Source: "test"}},
	}

	_ = r.Register(src)

	hostnames, err := r.DiscoverFrom(context.Background(), "test")
	if err != nil {
		t.Fatalf("DiscoverFrom failed: %v", err)
	}

	if len(hostnames) != 1 {
		t.Errorf("DiscoverFrom returned %d hostnames, want 1", len(hostnames))
	}
}

func TestRegistry_DiscoverFrom_NotSupported(t *testing.T) {
	r := NewRegistry(testLogger())

	src := &mockSource{
		name:              "test",
		supportsDiscovery: false,
	}

	_ = r.Register(src)

	hostnames, err := r.DiscoverFrom(context.Background(), "test")
	if err != nil {
		t.Fatalf("DiscoverFrom returned unexpected error: %v", err)
	}

	// Not an error, just returns nil when not supported
	if hostnames != nil {
		t.Errorf("DiscoverFrom returned %v, want nil", hostnames)
	}
}

func TestRegistry_DiscoverFrom_NotFound(t *testing.T) {
	r := NewRegistry(testLogger())

	_, err := r.DiscoverFrom(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for missing source")
	}
}

func TestIsWorkloadDisabled(t *testing.T) {
	tests := []struct {
		name        string
		labels      map[string]string
		annotations map[string]string
		want        bool
	}{
		{
			name:   "no labels or annotations",
			labels: map[string]string{},
			want:   false,
		},
		{
			name:   "docker label false",
			labels: map[string]string{"dnsweaver.enabled": "false"},
			want:   true,
		},
		{
			name:   "docker label FALSE (uppercase)",
			labels: map[string]string{"dnsweaver.enabled": "FALSE"},
			want:   true,
		},
		{
			name:   "docker label false with whitespace",
			labels: map[string]string{"dnsweaver.enabled": "  false  "},
			want:   true,
		},
		{
			name:   "docker label true",
			labels: map[string]string{"dnsweaver.enabled": "true"},
			want:   false,
		},
		{
			name:   "docker label empty",
			labels: map[string]string{"dnsweaver.enabled": ""},
			want:   false,
		},
		{
			name:        "k8s annotation false",
			labels:      map[string]string{},
			annotations: map[string]string{"dnsweaver.dev/enabled": "false"},
			want:        true,
		},
		{
			name:        "k8s annotation FALSE (uppercase)",
			labels:      map[string]string{},
			annotations: map[string]string{"dnsweaver.dev/enabled": "FALSE"},
			want:        true,
		},
		{
			name:        "k8s annotation true",
			labels:      map[string]string{},
			annotations: map[string]string{"dnsweaver.dev/enabled": "true"},
			want:        false,
		},
		{
			name:        "docker label takes precedence over annotation",
			labels:      map[string]string{"dnsweaver.enabled": "false"},
			annotations: map[string]string{"dnsweaver.dev/enabled": "true"},
			want:        true,
		},
		{
			name:   "unrelated labels only",
			labels: map[string]string{"traefik.enable": "false", "other": "value"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := workload.Workload{
				Labels:      tt.labels,
				Annotations: tt.annotations,
			}
			got := isWorkloadDisabled(w)
			if got != tt.want {
				t.Errorf("isWorkloadDisabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegistry_ExtractAll_DisabledWorkload(t *testing.T) {
	r := NewRegistry(testLogger())

	src := &mockSource{
		name: "traefik",
		hostnames: []Hostname{
			{Name: "app.example.com", Source: "traefik", Router: "app"},
		},
	}
	_ = r.Register(src)

	// Workload with dnsweaver.enabled=false should return no hostnames
	w := workload.Workload{
		Name:     "disabled-container",
		Labels:   map[string]string{"dnsweaver.enabled": "false", "traefik.http.routers.app.rule": "Host(`app.example.com`)"},
		Platform: workload.PlatformDocker,
	}
	hostnames := r.ExtractAll(context.Background(), w)

	if len(hostnames) != 0 {
		t.Errorf("ExtractAll returned %d hostnames for disabled workload, want 0", len(hostnames))
	}
}

func TestRegistry_ExtractAll_DisabledK8sWorkload(t *testing.T) {
	r := NewRegistry(testLogger())

	src := &mockSource{
		name: "traefik",
		hostnames: []Hostname{
			{Name: "app.example.com", Source: "traefik", Router: "app"},
		},
		platforms: []workload.Platform{workload.PlatformKubernetes},
	}
	_ = r.Register(src)

	// K8s workload with annotation opt-out
	w := workload.Workload{
		Name:        "disabled-ingress",
		Labels:      map[string]string{},
		Annotations: map[string]string{"dnsweaver.dev/enabled": "false"},
		Platform:    workload.PlatformKubernetes,
	}
	hostnames := r.ExtractAll(context.Background(), w)

	if len(hostnames) != 0 {
		t.Errorf("ExtractAll returned %d hostnames for disabled K8s workload, want 0", len(hostnames))
	}
}

func TestRegistry_ExtractAll_EnabledWorkload(t *testing.T) {
	r := NewRegistry(testLogger())

	src := &mockSource{
		name: "traefik",
		hostnames: []Hostname{
			{Name: "app.example.com", Source: "traefik", Router: "app"},
		},
	}
	_ = r.Register(src)

	// Workload with dnsweaver.enabled=true should work normally
	w := workload.Workload{
		Name:     "enabled-container",
		Labels:   map[string]string{"dnsweaver.enabled": "true"},
		Platform: workload.PlatformDocker,
	}
	hostnames := r.ExtractAll(context.Background(), w)

	if len(hostnames) != 1 {
		t.Fatalf("ExtractAll returned %d hostnames, want 1", len(hostnames))
	}
	if hostnames[0].Name != "app.example.com" {
		t.Errorf("hostname = %q, want %q", hostnames[0].Name, "app.example.com")
	}
}
