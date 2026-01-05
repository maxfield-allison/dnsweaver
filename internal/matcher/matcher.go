// Package matcher implements domain pattern matching for provider routing.
// Supports both glob patterns (default) and regex (opt-in via _DOMAINS_REGEX).
package matcher

import (
	"fmt"
	"regexp"
	"strings"
)

// PatternType indicates whether a pattern is glob or regex.
type PatternType int

const (
	// PatternTypeGlob uses glob-style wildcards: *, ?, [abc]
	PatternTypeGlob PatternType = iota
	// PatternTypeRegex uses full regular expression matching
	PatternTypeRegex
)

// DomainMatcher handles hostname pattern matching for a single provider.
// It supports both include and exclude patterns.
type DomainMatcher struct {
	includes    []*compiledPattern
	excludes    []*compiledPattern
	patternType PatternType
}

// compiledPattern wraps a compiled regex and the original pattern string for debugging.
type compiledPattern struct {
	original string
	regex    *regexp.Regexp
}

// DomainMatcherConfig holds configuration for creating a DomainMatcher.
type DomainMatcherConfig struct {
	// Includes are patterns that the hostname must match (at least one).
	// For glob: "*.example.com", "?.example.com", "exact.example.com"
	// For regex: "^[a-z0-9-]+\\.example\\.com$"
	Includes []string

	// Excludes are patterns that cause the hostname to be rejected if matched.
	// Excludes are evaluated before includes.
	Excludes []string

	// UseRegex switches from glob (default) to regex pattern matching.
	UseRegex bool
}

// NewDomainMatcher creates a new DomainMatcher from configuration.
// Returns an error if any pattern is invalid.
func NewDomainMatcher(cfg DomainMatcherConfig) (*DomainMatcher, error) {
	if len(cfg.Includes) == 0 {
		return nil, fmt.Errorf("at least one include pattern is required")
	}

	m := &DomainMatcher{
		includes: make([]*compiledPattern, 0, len(cfg.Includes)),
		excludes: make([]*compiledPattern, 0, len(cfg.Excludes)),
	}

	if cfg.UseRegex {
		m.patternType = PatternTypeRegex
	} else {
		m.patternType = PatternTypeGlob
	}

	// Compile include patterns
	for _, p := range cfg.Includes {
		cp, err := m.compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid include pattern %q: %w", p, err)
		}
		m.includes = append(m.includes, cp)
	}

	// Compile exclude patterns
	for _, p := range cfg.Excludes {
		cp, err := m.compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid exclude pattern %q: %w", p, err)
		}
		m.excludes = append(m.excludes, cp)
	}

	return m, nil
}

// Matches returns true if the hostname matches this matcher's patterns.
// Evaluation order:
//  1. If any exclude pattern matches, return false
//  2. If any include pattern matches, return true
//  3. Otherwise return false
func (m *DomainMatcher) Matches(hostname string) bool {
	// Normalize hostname to lowercase for matching
	hostname = strings.ToLower(hostname)

	// Check excludes first
	for _, ex := range m.excludes {
		if ex.regex.MatchString(hostname) {
			return false
		}
	}

	// Check includes
	for _, inc := range m.includes {
		if inc.regex.MatchString(hostname) {
			return true
		}
	}

	return false
}

// compile converts a pattern to a compiled regex.
// For glob patterns, converts glob syntax to regex.
// For regex patterns, compiles directly.
func (m *DomainMatcher) compile(pattern string) (*compiledPattern, error) {
	var regexStr string

	if m.patternType == PatternTypeRegex {
		regexStr = pattern
	} else {
		regexStr = globToRegex(pattern)
	}

	// Make matching case-insensitive
	if !strings.HasPrefix(regexStr, "(?i)") {
		regexStr = "(?i)" + regexStr
	}

	re, err := regexp.Compile(regexStr)
	if err != nil {
		return nil, err
	}

	return &compiledPattern{
		original: pattern,
		regex:    re,
	}, nil
}

// globToRegex converts a glob pattern to a regex pattern.
// Supported glob syntax:
//   - * matches any number of characters (including dots for subdomain matching)
//   - ? matches exactly one character
//   - [abc] matches one character from the set
//   - Everything else is literal
//
// The pattern is anchored (^...$) for full hostname matching.
func globToRegex(pattern string) string {
	var sb strings.Builder
	sb.WriteString("^")

	i := 0
	for i < len(pattern) {
		c := pattern[i]
		switch c {
		case '*':
			// * matches any characters including dots (for subdomain matching)
			// *.example.com matches app.example.com AND foo.bar.example.com
			sb.WriteString(".*")
		case '?':
			// ? matches exactly one character (not a dot for subdomain safety)
			sb.WriteString("[^.]")
		case '[':
			// Character class - find the closing bracket
			end := strings.IndexByte(pattern[i:], ']')
			if end == -1 {
				// No closing bracket, treat as literal
				sb.WriteString(regexp.QuoteMeta(string(c)))
			} else {
				// Copy the character class as-is (it's valid regex syntax)
				sb.WriteString(pattern[i : i+end+1])
				i += end
			}
		case '.':
			// Escape dots (common in domain names)
			sb.WriteString("\\.")
		default:
			// Quote any regex special characters
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
		i++
	}

	sb.WriteString("$")
	return sb.String()
}

// String returns a human-readable representation of the matcher.
func (m *DomainMatcher) String() string {
	var parts []string

	typeStr := "glob"
	if m.patternType == PatternTypeRegex {
		typeStr = "regex"
	}

	parts = append(parts, fmt.Sprintf("type=%s", typeStr))

	if len(m.includes) > 0 {
		var patterns []string
		for _, p := range m.includes {
			patterns = append(patterns, p.original)
		}
		parts = append(parts, fmt.Sprintf("includes=[%s]", strings.Join(patterns, ", ")))
	}

	if len(m.excludes) > 0 {
		var patterns []string
		for _, p := range m.excludes {
			patterns = append(patterns, p.original)
		}
		parts = append(parts, fmt.Sprintf("excludes=[%s]", strings.Join(patterns, ", ")))
	}

	return fmt.Sprintf("DomainMatcher{%s}", strings.Join(parts, ", "))
}
