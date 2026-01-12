package matcher

import (
	"testing"
)

func TestNewDomainMatcher_RequiresIncludes(t *testing.T) {
	_, err := NewDomainMatcher(DomainMatcherConfig{
		Includes: []string{},
	})
	if err == nil {
		t.Error("expected error for empty includes, got nil")
	}
}

func TestNewDomainMatcher_InvalidGlobPattern(t *testing.T) {
	// All glob patterns are valid since we convert them to regex
	// But we can test the resulting regex is valid
	m, err := NewDomainMatcher(DomainMatcherConfig{
		Includes: []string{"*.example.com"},
	})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if m == nil {
		t.Error("expected matcher to be created")
	}
}

func TestNewDomainMatcher_InvalidRegexPattern(t *testing.T) {
	_, err := NewDomainMatcher(DomainMatcherConfig{
		Includes: []string{"[invalid"},
		UseRegex: true,
	})
	if err == nil {
		t.Error("expected error for invalid regex, got nil")
	}
}

func TestDomainMatcher_GlobWildcard(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		hostname string
		want     bool
	}{
		// Basic wildcard matching
		{"wildcard matches single subdomain", "*.example.com", "app.example.com", true},
		{"wildcard matches nested subdomain", "*.example.com", "foo.bar.example.com", true},
		{"wildcard doesn't match root", "*.example.com", "example.com", false},
		{"wildcard matches any prefix", "*.example.com", "a.example.com", true},

		// Exact matching
		{"exact match works", "app.example.com", "app.example.com", true},
		{"exact match case insensitive", "App.Example.Com", "app.example.com", true},
		{"exact match fails on mismatch", "app.example.com", "other.example.com", false},

		// Question mark matching
		{"question mark matches single char", "?.example.com", "a.example.com", true},
		{"question mark doesn't match multiple", "?.example.com", "ab.example.com", false},
		{"question mark doesn't match dot", "?.example.com", "a.b.example.com", false},

		// Character class matching
		{"char class matches", "[abc].example.com", "a.example.com", true},
		{"char class matches b", "[abc].example.com", "b.example.com", true},
		{"char class doesn't match other", "[abc].example.com", "d.example.com", false},

		// More complex patterns
		{"local subdomain pattern", "*.local.example.com", "app.local.example.com", true},
		{"local subdomain nested", "*.local.example.com", "a.b.local.example.com", true},
		{"local subdomain no match", "*.local.example.com", "app.example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewDomainMatcher(DomainMatcherConfig{
				Includes: []string{tt.pattern},
			})
			if err != nil {
				t.Fatalf("failed to create matcher: %v", err)
			}

			got := m.Matches(tt.hostname)
			if got != tt.want {
				t.Errorf("Matches(%q) with pattern %q = %v, want %v", tt.hostname, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestDomainMatcher_GlobExcludes(t *testing.T) {
	tests := []struct {
		name     string
		includes []string
		excludes []string
		hostname string
		want     bool
	}{
		{
			name:     "exclude takes precedence",
			includes: []string{"*.example.com"},
			excludes: []string{"*.local.example.com"},
			hostname: "app.local.example.com",
			want:     false,
		},
		{
			name:     "non-excluded hostname matches",
			includes: []string{"*.example.com"},
			excludes: []string{"*.local.example.com"},
			hostname: "app.example.com",
			want:     true,
		},
		{
			name:     "multiple excludes checked",
			includes: []string{"*.example.com"},
			excludes: []string{"*.local.example.com", "admin.example.com"},
			hostname: "admin.example.com",
			want:     false,
		},
		{
			name:     "multiple includes any match works",
			includes: []string{"*.example.com", "*.test.com"},
			excludes: []string{},
			hostname: "app.test.com",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewDomainMatcher(DomainMatcherConfig{
				Includes: tt.includes,
				Excludes: tt.excludes,
			})
			if err != nil {
				t.Fatalf("failed to create matcher: %v", err)
			}

			got := m.Matches(tt.hostname)
			if got != tt.want {
				t.Errorf("Matches(%q) = %v, want %v", tt.hostname, got, tt.want)
			}
		})
	}
}

func TestDomainMatcher_Regex(t *testing.T) {
	tests := []struct {
		name     string
		includes []string
		excludes []string
		hostname string
		want     bool
	}{
		{
			name:     "regex anchor matches",
			includes: []string{"^[a-z0-9-]+\\.example\\.com$"},
			hostname: "app.example.com",
			want:     true,
		},
		{
			name:     "regex anchor matches hyphenated",
			includes: []string{"^[a-z0-9-]+\\.example\\.com$"},
			hostname: "my-app.example.com",
			want:     true,
		},
		{
			name:     "regex fails on nested subdomain",
			includes: []string{"^[a-z0-9-]+\\.example\\.com$"},
			hostname: "foo.bar.example.com",
			want:     false, // no dots allowed in subdomain part
		},
		{
			name:     "regex with exclude",
			includes: []string{".*\\.example\\.com$"},
			excludes: []string{"^(test|dev)\\..*"},
			hostname: "test.example.com",
			want:     false,
		},
		{
			name:     "regex with exclude non-match",
			includes: []string{".*\\.example\\.com$"},
			excludes: []string{"^(test|dev)\\..*"},
			hostname: "prod.example.com",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewDomainMatcher(DomainMatcherConfig{
				Includes: tt.includes,
				Excludes: tt.excludes,
				UseRegex: true,
			})
			if err != nil {
				t.Fatalf("failed to create matcher: %v", err)
			}

			got := m.Matches(tt.hostname)
			if got != tt.want {
				t.Errorf("Matches(%q) = %v, want %v", tt.hostname, got, tt.want)
			}
		})
	}
}

func TestDomainMatcher_CaseInsensitive(t *testing.T) {
	m, err := NewDomainMatcher(DomainMatcherConfig{
		Includes: []string{"*.example.com"},
	})
	if err != nil {
		t.Fatalf("failed to create matcher: %v", err)
	}

	tests := []string{
		"APP.example.com",
		"app.EXAMPLE.com",
		"App.Example.Com",
		"app.example.com",
	}

	for _, hostname := range tests {
		if !m.Matches(hostname) {
			t.Errorf("expected %q to match *.example.com (case insensitive)", hostname)
		}
	}
}

func TestDomainMatcher_String(t *testing.T) {
	m, err := NewDomainMatcher(DomainMatcherConfig{
		Includes: []string{"*.example.com"},
		Excludes: []string{"*.local.example.com"},
	})
	if err != nil {
		t.Fatalf("failed to create matcher: %v", err)
	}

	s := m.String()
	if s == "" {
		t.Error("expected non-empty string representation")
	}
	// Check it contains key elements
	if !contains(s, "glob") {
		t.Error("expected string to mention 'glob'")
	}
	if !contains(s, "*.example.com") {
		t.Error("expected string to contain include pattern")
	}
}

func TestGlobToRegex(t *testing.T) {
	tests := []struct {
		glob  string
		input string
		want  bool
	}{
		{"*.com", "example.com", true},
		{"*.com", "foo.bar.com", true},
		{"?.com", "a.com", true},
		{"?.com", "ab.com", false},
		{"[abc].com", "a.com", true},
		{"[abc].com", "d.com", false},
		{"exact.com", "exact.com", true},
		{"exact.com", "other.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.glob+"_"+tt.input, func(t *testing.T) {
			m, _ := NewDomainMatcher(DomainMatcherConfig{
				Includes: []string{tt.glob},
			})
			got := m.Matches(tt.input)
			if got != tt.want {
				t.Errorf("glob %q against %q = %v, want %v", tt.glob, tt.input, got, tt.want)
			}
		})
	}
}

// TestRealWorldSplitHorizon tests the exact use case from Issue #21:
// - *.local.bluewillows.net → Technitium
// - *.bluewillows.net (excluding *.local.*) → Cloudflare
func TestRealWorldSplitHorizon(t *testing.T) {
	// Technitium matcher - handles *.local.bluewillows.net
	techMatcher, err := NewDomainMatcher(DomainMatcherConfig{
		Includes: []string{"*.local.bluewillows.net"},
	})
	if err != nil {
		t.Fatalf("failed to create technitium matcher: %v", err)
	}

	// Cloudflare matcher - handles *.bluewillows.net except *.local.*
	cfMatcher, err := NewDomainMatcher(DomainMatcherConfig{
		Includes: []string{"*.bluewillows.net"},
		Excludes: []string{"*.local.bluewillows.net"},
	})
	if err != nil {
		t.Fatalf("failed to create cloudflare matcher: %v", err)
	}

	tests := []struct {
		hostname string
		wantTech bool
		wantCF   bool
	}{
		{"sonarr.local.bluewillows.net", true, false},
		{"sonarr.bluewillows.net", false, true},
		{"grafana.local.bluewillows.net", true, false},
		{"grafana.bluewillows.net", false, true},
		{"deep.nested.local.bluewillows.net", true, false},
		{"bluewillows.net", false, false}, // Root domain - neither matches
	}

	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			gotTech := techMatcher.Matches(tt.hostname)
			gotCF := cfMatcher.Matches(tt.hostname)

			if gotTech != tt.wantTech {
				t.Errorf("technitium.Matches(%q) = %v, want %v", tt.hostname, gotTech, tt.wantTech)
			}
			if gotCF != tt.wantCF {
				t.Errorf("cloudflare.Matches(%q) = %v, want %v", tt.hostname, gotCF, tt.wantCF)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
