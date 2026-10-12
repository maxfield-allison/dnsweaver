package caddy

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestCaddy_Name(t *testing.T) {
	if got := New().Name(); got != "caddy" {
		t.Errorf("Name() = %q, want %q", got, "caddy")
	}
}

func TestCaddy_ImplementsSource(t *testing.T) {
	var _ source.Source = (*Caddy)(nil)
}

func TestCaddy_SupportsDiscovery_False(t *testing.T) {
	if New().SupportsDiscovery() {
		t.Error("SupportsDiscovery() = true, want false (Caddyfile discovery is not implemented)")
	}
}

func TestCaddy_Discover_NoOp(t *testing.T) {
	hs, err := New().Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v, want nil", err)
	}
	if hs != nil {
		t.Errorf("Discover() = %v, want nil", hs)
	}
}

func TestCaddy_Extract_Cases(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   []string
	}{
		{
			name:   "nil labels",
			labels: nil,
			want:   nil,
		},
		{
			name:   "no caddy labels",
			labels: map[string]string{"com.docker.compose.project": "x"},
			want:   nil,
		},
		{
			name: "single caddy label",
			labels: map[string]string{
				"caddy": "app.example.com",
			},
			want: []string{"app.example.com"},
		},
		{
			name: "indexed caddy labels",
			labels: map[string]string{
				"caddy_0": "app.example.com",
				"caddy_1": "www.example.com",
			},
			want: []string{"app.example.com", "www.example.com"},
		},
		{
			name: "comma-separated value",
			labels: map[string]string{
				"caddy": "app.example.com, www.example.com",
			},
			want: []string{"app.example.com", "www.example.com"},
		},
		{
			name: "whitespace-separated value",
			labels: map[string]string{
				"caddy": "app.example.com www.example.com",
			},
			want: []string{"app.example.com", "www.example.com"},
		},
		{
			name: "dedupe across labels",
			labels: map[string]string{
				"caddy":   "app.example.com",
				"caddy_0": "app.example.com",
			},
			want: []string{"app.example.com"},
		},
		{
			name: "mixed case normalized",
			labels: map[string]string{
				"caddy": "APP.Example.COM",
			},
			want: []string{"app.example.com"},
		},
		{
			name: "ignore directive labels",
			labels: map[string]string{
				"caddy":               "app.example.com",
				"caddy.reverse_proxy": "{{upstreams 8080}}",
				"caddy.tls":           "internal",
			},
			want: []string{"app.example.com"},
		},
		{
			name: "ignore indexed directives beside hostname labels",
			labels: map[string]string{
				"caddy_0":                    "komodo.example.com",
				"caddy_0.reverse_proxy":      "192.0.2.102:9120",
				"caddy_0.tls":                "internal",
				"caddy_1":                    "www.example.com",
				"caddy_1.reverse_proxy":      "upstream.example.com",
				"caddy_1.header.X-Forwarded": "proxy.example.com",
			},
			want: []string{"komodo.example.com", "www.example.com"},
		},
		{
			name: "indexed directives alone declare no hostnames",
			labels: map[string]string{
				"caddy_0.reverse_proxy": "upstream.example.com",
				"caddy_1.tls":           "internal",
			},
			want: nil,
		},
		{
			name: "empty value",
			labels: map[string]string{
				"caddy":   "",
				"caddy_0": "app.example.com",
			},
			want: []string{"app.example.com"},
		},
		{
			name: "trailing comma",
			labels: map[string]string{
				"caddy": "app.example.com,,",
			},
			want: []string{"app.example.com"},
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := New(WithLogger(testLogger()))
			w := workload.Workload{Labels: tc.labels, Platform: workload.PlatformDocker, Kind: workload.KindContainer}

			hostnames, err := c.Extract(ctx, w)
			if err != nil {
				t.Fatalf("Extract() error = %v", err)
			}

			got := source.Hostnames(hostnames).Names()
			if !sameStringSlice(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}

			for _, h := range hostnames {
				if h.Source != "caddy" {
					t.Errorf("hostname %q has Source = %q, want caddy", h.Name, h.Source)
				}
				if h.Router == "" {
					t.Errorf("hostname %q has empty Router", h.Name)
				}
			}
		})
	}
}

func TestCaddy_RegistryIntegration(t *testing.T) {
	registry := source.NewRegistry(testLogger())
	if err := registry.Register(New(WithLogger(testLogger()))); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	w := workload.Workload{
		Labels:   map[string]string{"caddy": "app.example.com"},
		Platform: workload.PlatformDocker,
		Kind:     workload.KindContainer,
	}

	got := registry.ExtractAll(context.Background(), w)
	if len(got) != 1 || got[0].Name != "app.example.com" {
		t.Fatalf("ExtractAll = %v, want [app.example.com]", got)
	}
	if got[0].Source != "caddy" {
		t.Errorf("Source = %q, want caddy", got[0].Source)
	}
}

// sameStringSlice compares two string slices for equal membership in order.
func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
