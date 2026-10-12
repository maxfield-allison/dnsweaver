package caddy

import (
	"log/slog"
	"sort"
	"strings"
)

// labelPrefix identifies labels that declare Caddy hostnames.
//
// The parser accepts exactly "caddy" or a label beginning with "caddy_"
// without a dot (e.g. "caddy_0", "caddy_1"). Labels like
// "caddy.reverse_proxy" or "caddy_0.tls" are directive labels and are
// intentionally ignored.
const (
	labelName   = "caddy"
	labelPrefix = "caddy_"
)

// Extraction represents a single hostname discovered from a label.
type Extraction struct {
	// Hostname is the extracted hostname (lowercased, trimmed).
	Hostname string
	// Router is the label key the hostname was parsed from
	// (e.g. "caddy", "caddy_0"). Useful for logging and debugging.
	Router string
}

// Parser converts Caddy-style labels into hostname extractions.
type Parser struct {
	logger *slog.Logger
}

// ParserOption configures a Parser.
type ParserOption func(*Parser)

// WithParserLogger sets a custom logger on the parser.
func WithParserLogger(logger *slog.Logger) ParserOption {
	return func(p *Parser) {
		p.logger = logger
	}
}

// NewParser creates a parser with the given options.
func NewParser(opts ...ParserOption) *Parser {
	p := &Parser{logger: slog.Default()}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ExtractHostnames returns all hostnames declared on caddy-prefixed labels.
//
// Results are returned in a deterministic order (sorted by label key, then
// by hostname) with duplicates removed across labels. Empty values and
// whitespace-only values are silently skipped.
func (p *Parser) ExtractHostnames(labels map[string]string) []Extraction {
	if len(labels) == 0 {
		return nil
	}

	keys := make([]string, 0, len(labels))
	for k := range labels {
		if isCaddyHostnameLabel(k) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)

	seen := make(map[string]struct{})
	extractions := make([]Extraction, 0, len(keys))

	for _, key := range keys {
		for _, host := range splitHostnames(labels[key]) {
			if _, dup := seen[host]; dup {
				continue
			}
			seen[host] = struct{}{}
			extractions = append(extractions, Extraction{
				Hostname: host,
				Router:   key,
			})
		}
	}

	return extractions
}

// isCaddyHostnameLabel reports whether the label key declares Caddy
// hostnames. Matches exactly "caddy" or a dot-free key beginning with "caddy_".
// Directive-style keys such as "caddy_0.reverse_proxy" are excluded.
func isCaddyHostnameLabel(key string) bool {
	return key == labelName || (strings.HasPrefix(key, labelPrefix) && !strings.Contains(key, "."))
}

// splitHostnames splits a label value on commas or whitespace and returns
// the lowercased, trimmed hostname values. Empty entries are dropped.
func splitHostnames(value string) []string {
	if value == "" {
		return nil
	}
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		h := strings.ToLower(strings.TrimSpace(f))
		if h == "" {
			continue
		}
		out = append(out, h)
	}
	return out
}
