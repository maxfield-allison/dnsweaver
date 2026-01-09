package traefik

import (
	"log/slog"
	"os"
	"reflect"
	"sort"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestNewParser(t *testing.T) {
	parser := NewParser()

	if parser == nil {
		t.Fatal("expected parser to be initialized")
	}
	if parser.logger == nil {
		t.Error("expected logger to be initialized")
	}
}

func TestNewParser_WithLogger(t *testing.T) {
	logger := testLogger()
	parser := NewParser(WithParserLogger(logger))

	if parser.logger != logger {
		t.Error("expected custom logger to be set")
	}
}

func TestParser_ExtractHosts_SingleHost(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`myapp.example.com`)",
	}

	hosts := parser.ExtractHosts(labels)

	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0] != "myapp.example.com" {
		t.Errorf("expected myapp.example.com, got %s", hosts[0])
	}
}

func TestParser_ExtractHosts_MultipleHostsOR(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`app.example.com`) || Host(`www.example.com`)",
	}

	hosts := parser.ExtractHosts(labels)
	sort.Strings(hosts)

	expected := []string{"app.example.com", "www.example.com"}
	sort.Strings(expected)

	if !reflect.DeepEqual(hosts, expected) {
		t.Errorf("expected %v, got %v", expected, hosts)
	}
}

func TestParser_ExtractHosts_HostWithPathPrefix(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`myapp.example.com`) && PathPrefix(`/api`)",
	}

	hosts := parser.ExtractHosts(labels)

	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0] != "myapp.example.com" {
		t.Errorf("expected myapp.example.com, got %s", hosts[0])
	}
}

func TestParser_ExtractHosts_MultipleRouters(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"traefik.http.routers.app.rule":       "Host(`app.example.com`)",
		"traefik.http.routers.api.rule":       "Host(`api.example.com`)",
		"traefik.http.routers.dashboard.rule": "Host(`dash.example.com`)",
	}

	hosts := parser.ExtractHosts(labels)
	sort.Strings(hosts)

	expected := []string{"api.example.com", "app.example.com", "dash.example.com"}

	if !reflect.DeepEqual(hosts, expected) {
		t.Errorf("expected %v, got %v", expected, hosts)
	}
}

func TestParser_ExtractHosts_DuplicatesRemoved(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"traefik.http.routers.http.rule":  "Host(`app.example.com`)",
		"traefik.http.routers.https.rule": "Host(`app.example.com`)",
	}

	hosts := parser.ExtractHosts(labels)

	if len(hosts) != 1 {
		t.Fatalf("expected 1 host (deduplicated), got %d", len(hosts))
	}
	if hosts[0] != "app.example.com" {
		t.Errorf("expected app.example.com, got %s", hosts[0])
	}
}

func TestParser_ExtractHosts_NoTraefikLabels(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"com.docker.stack.namespace": "mystack",
		"maintainer":                 "admin@example.com",
	}

	hosts := parser.ExtractHosts(labels)

	if len(hosts) != 0 {
		t.Errorf("expected 0 hosts, got %d", len(hosts))
	}
}

func TestParser_ExtractHosts_NilLabels(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	hosts := parser.ExtractHosts(nil)

	if len(hosts) != 0 {
		t.Errorf("expected 0 hosts from nil labels, got %d", len(hosts))
	}
}

func TestParser_ExtractHosts_EmptyLabels(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	hosts := parser.ExtractHosts(map[string]string{})

	if len(hosts) != 0 {
		t.Errorf("expected 0 hosts from empty labels, got %d", len(hosts))
	}
}

func TestParser_ExtractHosts_NonRuleLabels(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"traefik.http.routers.myapp.entrypoints":               "websecure",
		"traefik.http.routers.myapp.tls":                       "true",
		"traefik.http.services.myapp.loadbalancer.server.port": "8080",
	}

	hosts := parser.ExtractHosts(labels)

	if len(hosts) != 0 {
		t.Errorf("expected 0 hosts from non-rule labels, got %d", len(hosts))
	}
}

func TestParser_ExtractHosts_MixedLabels(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"traefik.enable":                         "true",
		"traefik.http.routers.myapp.rule":        "Host(`app.example.com`)",
		"traefik.http.routers.myapp.entrypoints": "websecure",
		"traefik.http.routers.myapp.tls":         "true",
		"com.docker.stack.namespace":             "mystack",
	}

	hosts := parser.ExtractHosts(labels)

	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0] != "app.example.com" {
		t.Errorf("expected app.example.com, got %s", hosts[0])
	}
}

func TestParser_ExtractHosts_ComplexRule(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	// Complex rule with multiple conditions
	labels := map[string]string{
		"traefik.http.routers.myapp.rule": "(Host(`app.example.com`) || Host(`app2.example.com`)) && PathPrefix(`/api`)",
	}

	hosts := parser.ExtractHosts(labels)
	sort.Strings(hosts)

	expected := []string{"app.example.com", "app2.example.com"}
	sort.Strings(expected)

	if !reflect.DeepEqual(hosts, expected) {
		t.Errorf("expected %v, got %v", expected, hosts)
	}
}

func TestParser_ExtractHosts_EmptyHostname(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"traefik.http.routers.myapp.rule": "Host(``)",
	}

	hosts := parser.ExtractHosts(labels)

	if len(hosts) != 0 {
		t.Errorf("expected 0 hosts from empty hostname, got %d", len(hosts))
	}
}

func TestParser_ExtractHosts_WhitespaceHostname(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"traefik.http.routers.myapp.rule": "Host(`   `)",
	}

	hosts := parser.ExtractHosts(labels)

	if len(hosts) != 0 {
		t.Errorf("expected 0 hosts from whitespace hostname, got %d", len(hosts))
	}
}

func TestParser_ExtractHostnames_RouterContext(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))

	labels := map[string]string{
		"traefik.http.routers.frontend.rule": "Host(`app.example.com`)",
		"traefik.http.routers.backend.rule":  "Host(`api.example.com`)",
	}

	extractions := parser.ExtractHostnames(labels)

	if len(extractions) != 2 {
		t.Fatalf("expected 2 extractions, got %d", len(extractions))
	}

	// Build a map for easier testing (order from map iteration is not guaranteed)
	byHost := make(map[string]string)
	for _, e := range extractions {
		byHost[e.Hostname] = e.Router
	}

	if router, ok := byHost["app.example.com"]; !ok {
		t.Error("missing app.example.com")
	} else if router != "frontend" {
		t.Errorf("expected router frontend, got %s", router)
	}

	if router, ok := byHost["api.example.com"]; !ok {
		t.Error("missing api.example.com")
	} else if router != "backend" {
		t.Errorf("expected router backend, got %s", router)
	}
}

func TestExtractRouterName(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"traefik.http.routers.myapp.rule", "myapp"},
		{"traefik.http.routers.my-app.rule", "my-app"},
		{"traefik.http.routers.my_app.rule", "my_app"},
		{"traefik.http.routers.app123.rule", "app123"},
		{"traefik.http.routers.myapp.entrypoints", ""},
		{"traefik.http.routers.myapp.tls", ""},
		{"traefik.http.services.myapp.loadbalancer", ""},
		{"traefik.enable", ""},
		{"other.label", ""},
		{"traefik.http.routers..rule", ""}, // empty name
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := extractRouterName(tt.key)
			if got != tt.want {
				t.Errorf("extractRouterName(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestExtractHostsFromRule(t *testing.T) {
	tests := []struct {
		name string
		rule string
		want []string
	}{
		{
			name: "simple host",
			rule: "Host(`example.com`)",
			want: []string{"example.com"},
		},
		{
			name: "multiple hosts with OR",
			rule: "Host(`a.example.com`) || Host(`b.example.com`)",
			want: []string{"a.example.com", "b.example.com"},
		},
		{
			name: "host with path",
			rule: "Host(`example.com`) && PathPrefix(`/api`)",
			want: []string{"example.com"},
		},
		{
			name: "complex grouped rule",
			rule: "(Host(`a.com`) || Host(`b.com`)) && PathPrefix(`/`)",
			want: []string{"a.com", "b.com"},
		},
		{
			name: "multiple matchers",
			rule: "Host(`example.com`) && Headers(`X-Api-Key`, `secret`) && Method(`GET`)",
			want: []string{"example.com"},
		},
		{
			name: "empty rule",
			rule: "",
			want: nil,
		},
		{
			name: "no host matcher",
			rule: "PathPrefix(`/api`)",
			want: nil,
		},
		{
			name: "subdomain with dashes",
			rule: "Host(`my-app.example.com`)",
			want: []string{"my-app.example.com"},
		},
		{
			name: "duplicate hosts in same rule",
			rule: "Host(`app.com`) || Host(`app.com`)",
			want: []string{"app.com"},
		},
		{
			name: "HostRegexp not matched",
			rule: "HostRegexp(`{subdomain:[a-z]+}.example.com`)",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractHostsFromRule(tt.rule)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractHostsFromRule(%q) = %v, want %v", tt.rule, got, tt.want)
			}
		})
	}
}
