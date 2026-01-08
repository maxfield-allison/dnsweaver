package traefik

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/maxfield-allison/dnsweaver/pkg/source"
)

func TestNew(t *testing.T) {
	src := New()

	if src == nil {
		t.Fatal("expected source to be initialized")
	}
	if src.parser == nil {
		t.Error("expected parser to be initialized")
	}
	if src.logger == nil {
		t.Error("expected logger to be initialized")
	}
}

func TestNew_WithLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	src := New(WithLogger(logger))

	if src.logger != logger {
		t.Error("expected custom logger to be set")
	}
}

func TestTraefik_Name(t *testing.T) {
	src := New()

	if src.Name() != "traefik" {
		t.Errorf("Name() = %q, want %q", src.Name(), "traefik")
	}
}

func TestTraefik_Extract_SingleHost(t *testing.T) {
	src := New(WithLogger(testLogger()))
	ctx := context.Background()

	labels := map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`app.example.com`)",
	}

	hostnames, err := src.Extract(ctx, labels)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(hostnames) != 1 {
		t.Fatalf("expected 1 hostname, got %d", len(hostnames))
	}

	h := hostnames[0]
	if h.Name != "app.example.com" {
		t.Errorf("Name = %q, want %q", h.Name, "app.example.com")
	}
	if h.Source != "traefik" {
		t.Errorf("Source = %q, want %q", h.Source, "traefik")
	}
	if h.Router != "myapp" {
		t.Errorf("Router = %q, want %q", h.Router, "myapp")
	}
}

func TestTraefik_Extract_MultipleHostnames(t *testing.T) {
	src := New(WithLogger(testLogger()))
	ctx := context.Background()

	labels := map[string]string{
		"traefik.http.routers.frontend.rule": "Host(`app.example.com`) || Host(`www.example.com`)",
		"traefik.http.routers.api.rule":      "Host(`api.example.com`)",
	}

	hostnames, err := src.Extract(ctx, labels)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(hostnames) != 3 {
		t.Fatalf("expected 3 hostnames, got %d", len(hostnames))
	}

	// All should have source=traefik
	for _, h := range hostnames {
		if h.Source != "traefik" {
			t.Errorf("hostname %q has Source = %q, want traefik", h.Name, h.Source)
		}
	}

	// Check names are present
	names := source.Hostnames(hostnames).Names()
	hasApp := false
	hasWww := false
	hasAPI := false
	for _, n := range names {
		switch n {
		case "app.example.com":
			hasApp = true
		case "www.example.com":
			hasWww = true
		case "api.example.com":
			hasAPI = true
		}
	}

	if !hasApp || !hasWww || !hasAPI {
		t.Errorf("missing expected hostnames: app=%v, www=%v, api=%v", hasApp, hasWww, hasAPI)
	}
}

func TestTraefik_Extract_EmptyLabels(t *testing.T) {
	src := New(WithLogger(testLogger()))
	ctx := context.Background()

	hostnames, err := src.Extract(ctx, map[string]string{})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(hostnames) != 0 {
		t.Errorf("expected 0 hostnames, got %d", len(hostnames))
	}
}

func TestTraefik_Extract_NilLabels(t *testing.T) {
	src := New(WithLogger(testLogger()))
	ctx := context.Background()

	hostnames, err := src.Extract(ctx, nil)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if hostnames != nil && len(hostnames) != 0 {
		t.Errorf("expected nil or empty, got %v", hostnames)
	}
}

func TestTraefik_Extract_NoTraefikLabels(t *testing.T) {
	src := New(WithLogger(testLogger()))
	ctx := context.Background()

	labels := map[string]string{
		"com.docker.compose.project": "myproject",
		"maintainer":                 "admin@example.com",
	}

	hostnames, err := src.Extract(ctx, labels)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(hostnames) != 0 {
		t.Errorf("expected 0 hostnames, got %d", len(hostnames))
	}
}

func TestTraefik_ImplementsSource(t *testing.T) {
	// Compile-time check that Traefik implements source.Source
	var _ source.Source = (*Traefik)(nil)
}

func TestTraefik_RegistryIntegration(t *testing.T) {
	// Test that Traefik works correctly with the source registry
	registry := source.NewRegistry(testLogger())
	traefik := New(WithLogger(testLogger()))

	err := registry.Register(traefik)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	labels := map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`app.example.com`)",
	}

	hostnames := registry.ExtractAll(context.Background(), labels)

	if len(hostnames) != 1 {
		t.Fatalf("expected 1 hostname, got %d", len(hostnames))
	}

	if hostnames[0].Name != "app.example.com" {
		t.Errorf("Name = %q, want %q", hostnames[0].Name, "app.example.com")
	}
}

func TestTraefik_SupportsDiscovery_Default(t *testing.T) {
	src := New()

	// Without file config, should not support discovery
	if src.SupportsDiscovery() {
		t.Error("expected SupportsDiscovery() = false without file config")
	}
}

func TestTraefik_SupportsDiscovery_WithConfig(t *testing.T) {
	src := New(
		WithFileDiscovery(source.FileDiscoveryConfig{
			FilePaths: []string{"/rules"},
		}),
	)

	if !src.SupportsDiscovery() {
		t.Error("expected SupportsDiscovery() = true with file paths configured")
	}
}

func TestTraefik_Discover_NoConfig(t *testing.T) {
	src := New()

	hostnames, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}

	if hostnames != nil {
		t.Errorf("expected nil, got %v", hostnames)
	}
}

func TestTraefik_FileConfig(t *testing.T) {
	config := source.FileDiscoveryConfig{
		FilePaths:    []string{"/rules", "/configs"},
		FilePattern:  "*.yaml",
		PollInterval: 30 * time.Second,
		WatchMethod:  "poll",
	}

	src := New(WithFileDiscovery(config))

	got := src.FileConfig()

	if len(got.FilePaths) != 2 {
		t.Errorf("FilePaths = %v, want 2 paths", got.FilePaths)
	}
	if got.PollInterval != 30*time.Second {
		t.Errorf("PollInterval = %v, want 30s", got.PollInterval)
	}
}

func TestTraefik_WithFileDiscovery_DefaultPattern(t *testing.T) {
	src := New(
		WithFileDiscovery(source.FileDiscoveryConfig{
			FilePaths: []string{"/rules"},
			// FilePattern is empty
		}),
	)

	config := src.FileConfig()

	// Should apply default pattern
	if config.FilePattern != DefaultFilePattern {
		t.Errorf("FilePattern = %q, want %q", config.FilePattern, DefaultFilePattern)
	}
}
