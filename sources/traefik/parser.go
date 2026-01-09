package traefik

import (
	"log/slog"
	"regexp"
	"strings"
)

// hostRegex matches Host(`hostname`) patterns in Traefik router rules.
// Captures the hostname inside the backticks.
var hostRegex = regexp.MustCompile(`Host\(` + "`" + `([^` + "`" + `]+)` + "`" + `\)`)

// routerLabelPrefix is the prefix for Traefik HTTP router labels.
const routerLabelPrefix = "traefik.http.routers."

// routerRuleSuffix is the suffix for router rule labels.
const routerRuleSuffix = ".rule"

// HostnameExtraction represents a hostname extracted from a specific router.
type HostnameExtraction struct {
	Hostname string // The extracted hostname
	Router   string // The router name (e.g., "myapp")
}

// Parser extracts hostnames from Traefik labels.
type Parser struct {
	logger *slog.Logger
}

// ParserOption is a functional option for configuring the Parser.
type ParserOption func(*Parser)

// WithParserLogger sets a custom logger.
func WithParserLogger(logger *slog.Logger) ParserOption {
	return func(p *Parser) {
		p.logger = logger
	}
}

// NewParser creates a new Traefik label parser.
func NewParser(opts ...ParserOption) *Parser {
	p := &Parser{
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// ExtractHostnames extracts all hostnames from Traefik labels with router context.
// Returns a slice of extractions that include both hostname and router name.
func (p *Parser) ExtractHostnames(labels map[string]string) []HostnameExtraction {
	seen := make(map[string]struct{})
	var extractions []HostnameExtraction

	for key, value := range labels {
		// Only process traefik router rule labels
		router := extractRouterName(key)
		if router == "" {
			continue
		}

		p.logger.Debug("parsing traefik rule",
			slog.String("router", router),
			slog.String("rule", value),
		)

		// Extract all Host() patterns from the rule
		hosts := extractHostsFromRule(value)
		for _, hostname := range hosts {
			// Deduplicate by hostname (first occurrence wins)
			if _, exists := seen[hostname]; !exists {
				seen[hostname] = struct{}{}
				extractions = append(extractions, HostnameExtraction{
					Hostname: hostname,
					Router:   router,
				})
				p.logger.Debug("extracted hostname",
					slog.String("hostname", hostname),
					slog.String("router", router),
				)
			}
		}
	}

	p.logger.Debug("extraction complete",
		slog.Int("count", len(extractions)),
	)

	return extractions
}

// ExtractHosts extracts all hostnames from Traefik labels.
// Returns a deduplicated slice of hostname strings.
// This is a convenience method that discards router information.
func (p *Parser) ExtractHosts(labels map[string]string) []string {
	extractions := p.ExtractHostnames(labels)
	hosts := make([]string, len(extractions))
	for i, e := range extractions {
		hosts[i] = e.Hostname
	}
	return hosts
}

// extractRouterName extracts the router name from a Traefik label key.
// Returns empty string if this is not a router rule label.
//
// Examples:
//   - "traefik.http.routers.myapp.rule" -> "myapp"
//   - "traefik.http.routers.myapp.entrypoints" -> ""
//   - "traefik.enable" -> ""
func extractRouterName(key string) string {
	// Must start with prefix and end with suffix
	if !strings.HasPrefix(key, routerLabelPrefix) {
		return ""
	}
	if !strings.HasSuffix(key, routerRuleSuffix) {
		return ""
	}

	// Extract the router name between prefix and suffix
	// traefik.http.routers.<name>.rule
	withoutPrefix := strings.TrimPrefix(key, routerLabelPrefix)
	withoutSuffix := strings.TrimSuffix(withoutPrefix, routerRuleSuffix)

	// Handle edge case: traefik.http.routers..rule (empty name)
	if withoutSuffix == "" {
		return ""
	}

	return withoutSuffix
}

// extractHostsFromRule extracts all hostnames from a Traefik rule string.
// Handles various rule formats:
//   - Host(`example.com`)
//   - Host(`a.com`) || Host(`b.com`)
//   - Host(`example.com`) && PathPrefix(`/api`)
//   - (Host(`a.com`) || Host(`b.com`)) && PathPrefix(`/`)
func extractHostsFromRule(rule string) []string {
	seen := make(map[string]struct{})
	var hosts []string

	matches := hostRegex.FindAllStringSubmatch(rule, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		hostname := strings.TrimSpace(match[1])
		if hostname == "" {
			continue
		}

		// Deduplicate within the same rule
		if _, exists := seen[hostname]; !exists {
			seen[hostname] = struct{}{}
			hosts = append(hosts, hostname)
		}
	}

	return hosts
}

// ExtractHostsFromRule extracts hostnames from a single rule string.
// This is a convenience function for parsing rules without a Parser instance.
func ExtractHostsFromRule(rule string) []string {
	return extractHostsFromRule(rule)
}
