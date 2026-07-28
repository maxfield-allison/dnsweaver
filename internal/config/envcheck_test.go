package config

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestLooksLikePrefixTypo(t *testing.T) {
	tests := []struct {
		segment string
		want    bool
	}{
		// Genuine misspellings of DNSWEAVER (distance 1-2).
		{"DNSWEVAER", true},  // transposed A/V (the #130 typo)
		{"DNSWEAER", true},   // dropped V
		{"DNSWAEVER", true},  // transposed A/E
		{"DNSWEAVR", true},   // dropped E
		{"DNSWEAVERR", true}, // doubled R

		// Exact prefix is not a typo.
		{"DNSWEAVER", false},

		// Unrelated prefixes.
		{"DOCKER", false},
		{"KUBERNETES", false},
		{"PATH", false},
		{"HOME", false},
		{"", false},

		// Right length but not letters-only, or too far away.
		{"DNSWEAVE1", false},
		{"COMPLETELY", false},
	}

	for _, tt := range tests {
		if got := looksLikePrefixTypo(tt.segment); got != tt.want {
			t.Errorf("looksLikePrefixTypo(%q) = %v, want %v", tt.segment, got, tt.want)
		}
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"DNSWEAVER", "DNSWEVAER", 2},
		{"DNSWEAVER", "DNSWEAER", 1},
		{"kitten", "sitting", 3},
	}
	for _, tt := range tests {
		if got := levenshtein(tt.a, tt.b); got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

// captureWarnings runs WarnOnMisspelledEnvPrefixes against a JSON logger backed
// by a buffer and returns the decoded log records.
func captureWarnings(t *testing.T) []map[string]any {
	t.Helper()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	WarnOnMisspelledEnvPrefixes(logger)

	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("failed to decode log line %q: %v", line, err)
		}
		records = append(records, rec)
	}
	return records
}

func TestWarnOnMisspelledEnvPrefixes_FlagsMisspelledPrefix(t *testing.T) {
	// The exact scenario reported in issue #130.
	t.Setenv("DNSWEVAER_CLOUDFLARE_TARGET_MODE", "public")

	records := captureWarnings(t)

	var found bool
	for _, rec := range records {
		if rec["variable"] == "DNSWEVAER_CLOUDFLARE_TARGET_MODE" {
			found = true
			if got, want := rec["did_you_mean"], "DNSWEAVER_CLOUDFLARE_TARGET_MODE"; got != want {
				t.Errorf("did_you_mean = %v, want %v", got, want)
			}
		}
	}
	if !found {
		t.Errorf("expected a warning for the misspelled prefix, got records: %v", records)
	}
}

func TestWarnOnMisspelledEnvPrefixes_IgnoresValidAndUnrelated(t *testing.T) {
	t.Setenv("DNSWEAVER_CLOUDFLARE_TARGET_MODE", "public") // correct prefix
	t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock") // unrelated
	t.Setenv("DNSWEAVER_INSTANCES", "cloudflare")          // correct prefix

	for _, rec := range captureWarnings(t) {
		if v, ok := rec["variable"].(string); ok && strings.HasPrefix(v, "DNSWEAVER_") {
			t.Errorf("correctly prefixed variable %q should not be flagged", v)
		}
		if rec["variable"] == "DOCKER_HOST" {
			t.Errorf("unrelated variable DOCKER_HOST should not be flagged")
		}
	}
}
