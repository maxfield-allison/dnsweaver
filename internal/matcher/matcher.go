// Package matcher implements domain pattern matching for provider routing.
// Supports both glob patterns (default) and regex (opt-in).
package matcher

// Matcher determines which provider should handle a given hostname.
type Matcher struct {
	// TODO: Add fields in Issue #21 - Multi-provider design
}

// Result indicates which provider matched a hostname.
type Result struct {
	ProviderName string
	Matched      bool
}

// TODO: Implement in Issue #21
// - NewMatcher() constructor with provider order
// - Match(hostname) returns first matching provider
// - Glob pattern support (*.example.com, ?.example.com)
// - Regex pattern support (opt-in via _DOMAINS_REGEX)
// - Exclude pattern evaluation before include patterns
