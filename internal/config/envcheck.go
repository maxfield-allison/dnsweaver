package config

import (
	"log/slog"
	"os"
	"strings"
)

// canonicalEnvPrefix is the correct first segment of every dnsweaver
// environment variable (the part before the first underscore). All config keys
// take the form DNSWEAVER_... — see DECISIONS.md.
const canonicalEnvPrefix = "DNSWEAVER"

// maxPrefixTypoDistance is the largest Levenshtein distance from
// canonicalEnvPrefix that is still treated as an obvious misspelling. Two edits
// covers the common cases (a transposition, a dropped letter, a doubled letter)
// while staying far away from any unrelated environment variable.
const maxPrefixTypoDistance = 2

// WarnOnMisspelledEnvPrefixes scans the process environment for variables whose
// first underscore-delimited segment closely resembles the DNSWEAVER prefix but
// is misspelled (for example DNSWEVAER_CLOUDFLARE_TARGET_MODE instead of
// DNSWEAVER_CLOUDFLARE_TARGET_MODE).
//
// Such a variable is silently ignored during config loading, which typically
// surfaces later as a confusing "required but not set" error for the value the
// user believed they had provided. Emitting a warning here makes the typo
// obvious and points at the intended variable name.
//
// Only segments within a small edit distance of the canonical prefix are
// reported, so ordinary environment variables (PATH, HOME, DOCKER_HOST, ...)
// are never flagged. A nil logger falls back to slog.Default().
func WarnOnMisspelledEnvPrefixes(logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}

	for _, env := range os.Environ() {
		name, _, ok := strings.Cut(env, "=")
		if !ok {
			continue
		}

		segment, rest, ok := strings.Cut(name, "_")
		if !ok || rest == "" {
			// No underscore, or nothing after it: not a DNSWEAVER_<KEY> shape.
			continue
		}
		if segment == canonicalEnvPrefix {
			// Correctly prefixed — either consumed or an as-yet-unknown key,
			// neither of which this check is responsible for.
			continue
		}
		if !looksLikePrefixTypo(segment) {
			continue
		}

		logger.Warn("environment variable has a misspelled DNSWEAVER prefix and will be ignored",
			slog.String("variable", name),
			slog.String("did_you_mean", canonicalEnvPrefix+"_"+rest),
		)
	}
}

// looksLikePrefixTypo reports whether segment is almost certainly a misspelling
// of canonicalEnvPrefix: an all-uppercase-letter token of roughly the same
// length that is within maxPrefixTypoDistance edits, but not an exact match.
//
// The length window and letter-only guard make unrelated prefixes such as
// DOCKER, K8S or KUBERNETES cheap to reject before computing edit distance.
func looksLikePrefixTypo(segment string) bool {
	if segment == "" || segment == canonicalEnvPrefix {
		return false
	}
	if len(segment) < len(canonicalEnvPrefix)-maxPrefixTypoDistance ||
		len(segment) > len(canonicalEnvPrefix)+maxPrefixTypoDistance {
		return false
	}
	for i := 0; i < len(segment); i++ {
		if c := segment[i]; c < 'A' || c > 'Z' {
			return false
		}
	}
	return levenshtein(segment, canonicalEnvPrefix) <= maxPrefixTypoDistance
}

// levenshtein returns the edit distance between two ASCII strings using the
// standard two-row dynamic programming formulation.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}
