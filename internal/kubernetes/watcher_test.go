package kubernetes

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func TestFactoryNamespaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configured []string
		want       []string
	}{
		{name: "all namespaces", want: []string{""}},
		{name: "one namespace", configured: []string{"apps"}, want: []string{"apps"}},
		{
			name:       "multiple namespaces are trimmed and deduplicated",
			configured: []string{"apps", " edge ", "apps", ""},
			want:       []string{"apps", "edge"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			watcher := New(nil, WithConfig(Config{Namespaces: tt.configured}))
			if got := watcher.factoryNamespaces(); !slices.Equal(got, tt.want) {
				t.Fatalf("factoryNamespaces() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchesFiltersNormalizesConfiguredNamespaces(t *testing.T) {
	t.Parallel()

	watcher := New(nil, WithConfig(Config{Namespaces: []string{" apps "}}))
	if !watcher.matchesFilters("apps", nil) {
		t.Fatal("matchesFilters() rejected a namespace normalized by factoryNamespaces()")
	}
}

func TestWatcherScopesInformerRequestsToConfiguredNamespaces(t *testing.T) {
	t.Parallel()

	client := kubernetesfake.NewClientset()
	watcher := New(
		&Clients{Typed: client},
		WithConfig(Config{
			Namespaces:       []string{"apps", "edge"},
			WatchIngress:     true,
			DebounceInterval: time.Hour,
		}),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := watcher.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(watcher.Stop)

	listed := make(map[string]bool)
	watched := make(map[string]bool)
	for _, action := range client.Actions() {
		if action.GetResource().Resource != "ingresses" {
			continue
		}
		switch action.GetVerb() {
		case "list":
			listed[action.GetNamespace()] = true
		case "watch":
			watched[action.GetNamespace()] = true
		}
	}

	for _, namespace := range []string{"apps", "edge"} {
		if !listed[namespace] {
			t.Errorf("no Ingress list request for namespace %q", namespace)
		}
		if !watched[namespace] {
			t.Errorf("no Ingress watch request for namespace %q", namespace)
		}
	}
	if listed[""] || watched[""] {
		t.Fatal("namespace-filtered watcher made a cluster-scope Ingress request")
	}
}
