package traefik

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParser_DiscoverFromFiles_SingleFile(t *testing.T) {
	// Create a temporary directory with a test file
	tmpDir := t.TempDir()

	yamlContent := "http:\n" +
		"  routers:\n" +
		"    myapp:\n" +
		"      rule: \"Host(`app.example.com`)\"\n" +
		"      service: myapp\n" +
		"    api:\n" +
		"      rule: \"Host(`api.example.com`) && PathPrefix(`/v1`)\"\n" +
		"      service: api\n"

	testFile := filepath.Join(tmpDir, "routers.yml")
	if err := os.WriteFile(testFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	parser := NewParser()
	extractions, err := parser.DiscoverFromFiles(
		context.Background(),
		[]string{testFile},
		"*.yml",
	)

	if err != nil {
		t.Fatalf("DiscoverFromFiles returned error: %v", err)
	}

	if len(extractions) != 2 {
		t.Errorf("expected 2 extractions, got %d", len(extractions))
	}

	// Check that both hostnames are found
	found := make(map[string]string)
	for _, e := range extractions {
		found[e.Hostname] = e.Router
	}

	if found["app.example.com"] != "myapp" {
		t.Errorf("expected app.example.com with router myapp, got %v", found)
	}
	if found["api.example.com"] != "api" {
		t.Errorf("expected api.example.com with router api, got %v", found)
	}
}

func TestParser_DiscoverFromFiles_Directory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple YAML files
	files := map[string]string{
		"app.yml": `
http:
  routers:
    app:
      rule: "Host(` + "`app.example.com`" + `)"
`,
		"web.yaml": `
http:
  routers:
    web:
      rule: "Host(` + "`web.example.com`" + `)"
`,
		"middleware.yml": `
http:
  middlewares:
    auth:
      basicAuth:
        users:
          - "admin:password"
`,
	}

	for name, content := range files {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	parser := NewParser()
	extractions, err := parser.DiscoverFromFiles(
		context.Background(),
		[]string{tmpDir},
		"*.yml,*.yaml",
	)

	if err != nil {
		t.Fatalf("DiscoverFromFiles returned error: %v", err)
	}

	// Should find 2 hostnames (app.example.com, web.example.com)
	// middleware.yml has no routers, so it contributes nothing
	if len(extractions) != 2 {
		t.Errorf("expected 2 extractions, got %d: %v", len(extractions), extractions)
	}

	found := make(map[string]bool)
	for _, e := range extractions {
		found[e.Hostname] = true
	}

	if !found["app.example.com"] {
		t.Error("expected to find app.example.com")
	}
	if !found["web.example.com"] {
		t.Error("expected to find web.example.com")
	}
}

func TestParser_DiscoverFromFiles_IgnoresMiddleware(t *testing.T) {
	tmpDir := t.TempDir()

	// This file has middlewares but no routers - should find nothing
	yamlContent := `
http:
  middlewares:
    strip-prefix:
      stripPrefix:
        prefixes:
          - "/api"
    rate-limit:
      rateLimit:
        average: 100
        burst: 200
`
	testFile := filepath.Join(tmpDir, "middleware.yml")
	if err := os.WriteFile(testFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	parser := NewParser()
	extractions, err := parser.DiscoverFromFiles(
		context.Background(),
		[]string{testFile},
		"*.yml",
	)

	if err != nil {
		t.Fatalf("DiscoverFromFiles returned error: %v", err)
	}

	if len(extractions) != 0 {
		t.Errorf("expected 0 extractions from middleware file, got %d: %v", len(extractions), extractions)
	}
}

func TestParser_DiscoverFromFiles_NonExistentPath(t *testing.T) {
	parser := NewParser()
	extractions, err := parser.DiscoverFromFiles(
		context.Background(),
		[]string{"/nonexistent/path"},
		"*.yml",
	)

	// Should not error, just return empty (with a warning logged)
	if err != nil {
		t.Fatalf("DiscoverFromFiles returned unexpected error: %v", err)
	}

	if len(extractions) != 0 {
		t.Errorf("expected 0 extractions from nonexistent path, got %d", len(extractions))
	}
}

func TestParser_DiscoverFromFiles_MultipleHostsInRule(t *testing.T) {
	tmpDir := t.TempDir()

	yamlContent := `
http:
  routers:
    multi:
      rule: "Host(` + "`app.example.com`" + `) || Host(` + "`www.example.com`" + `)"
`
	testFile := filepath.Join(tmpDir, "multi.yml")
	if err := os.WriteFile(testFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	parser := NewParser()
	extractions, err := parser.DiscoverFromFiles(
		context.Background(),
		[]string{testFile},
		"*.yml",
	)

	if err != nil {
		t.Fatalf("DiscoverFromFiles returned error: %v", err)
	}

	if len(extractions) != 2 {
		t.Errorf("expected 2 extractions, got %d", len(extractions))
	}

	found := make(map[string]bool)
	for _, e := range extractions {
		found[e.Hostname] = true
	}

	if !found["app.example.com"] {
		t.Error("expected to find app.example.com")
	}
	if !found["www.example.com"] {
		t.Error("expected to find www.example.com")
	}
}

func TestParser_DiscoverFromFiles_PatternMatching(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files with different extensions
	files := map[string]string{
		"config.yml":  `http: {routers: {a: {rule: "Host(` + "`a.example.com`" + `)"}}}`,
		"config.yaml": `http: {routers: {b: {rule: "Host(` + "`b.example.com`" + `)"}}}`,
		"config.json": `{"this": "is ignored"}`,
		"config.toml": `[this.is.also.ignored]`,
	}

	for name, content := range files {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	parser := NewParser()
	extractions, err := parser.DiscoverFromFiles(
		context.Background(),
		[]string{tmpDir},
		"*.yml,*.yaml", // Only yml and yaml
	)

	if err != nil {
		t.Fatalf("DiscoverFromFiles returned error: %v", err)
	}

	// Should find exactly 2 (from .yml and .yaml files)
	if len(extractions) != 2 {
		t.Errorf("expected 2 extractions, got %d: %v", len(extractions), extractions)
	}
}

func TestParser_DiscoverFromFiles_Deduplication(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two files with the same hostname
	files := map[string]string{
		"file1.yml": `http: {routers: {a: {rule: "Host(` + "`shared.example.com`" + `)"}}}`,
		"file2.yml": `http: {routers: {b: {rule: "Host(` + "`shared.example.com`" + `)"}}}`,
	}

	for name, content := range files {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	parser := NewParser()
	extractions, err := parser.DiscoverFromFiles(
		context.Background(),
		[]string{tmpDir},
		"*.yml",
	)

	if err != nil {
		t.Fatalf("DiscoverFromFiles returned error: %v", err)
	}

	// Should deduplicate - only 1 unique hostname
	if len(extractions) != 1 {
		t.Errorf("expected 1 extraction (deduplicated), got %d: %v", len(extractions), extractions)
	}
}

func TestParser_DiscoverFromFiles_EmptyPaths(t *testing.T) {
	parser := NewParser()
	extractions, err := parser.DiscoverFromFiles(
		context.Background(),
		[]string{},
		"*.yml",
	)

	if err != nil {
		t.Fatalf("DiscoverFromFiles returned error: %v", err)
	}

	if len(extractions) != 0 {
		t.Errorf("expected 0 extractions from empty paths, got %d", len(extractions))
	}
}

func TestParser_DiscoverFromFiles_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()

	yamlContent := `http: {routers: {a: {rule: "Host(` + "`a.example.com`" + `)"}}}`
	testFile := filepath.Join(tmpDir, "config.yml")
	if err := os.WriteFile(testFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	parser := NewParser()
	_, err := parser.DiscoverFromFiles(
		ctx,
		[]string{testFile},
		"*.yml",
	)

	if err != context.Canceled {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}
