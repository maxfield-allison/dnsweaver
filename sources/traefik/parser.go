package traefik

import (
	"log/slog"
	"regexp"
	"strings"
)

// hostRegex matches Host(`hostname`) patterns in Traefik router rules.
// Captures the hostname inside the backticks.
//
// Whitespace is tolerated between the parentheses and the backticks
// (Host( `app.example.com` )) because Traefik accepts it and rules rendered
// from a `defaultRule` template commonly contain it. The opening parenthesis
// must still follow "Host" directly, so HostRegexp(...) and HostSNI(...) never
// match.
var hostRegex = regexp.MustCompile(`Host\(\s*` + "`" + `([^` + "`" + `]+)` + "`" + `\s*\)`)

// routerLabelPrefix is the prefix for Traefik HTTP router labels.
const routerLabelPrefix = "traefik.http.routers."

// routerRuleSuffix is the suffix for router rule labels.
const routerRuleSuffix = ".rule"

// routerEntryPointsSuffix is the suffix for router entrypoints labels.
const routerEntryPointsSuffix = ".entrypoints"

// MetadataKeyEntryPoint is the Hostname.Metadata key under which the
// Traefik source records the entrypoint a hostname was discovered through.
// It is consumed by ProviderInstance.MetadataFilters when an instance is
// scoped via DNSWEAVER_{NAME}_ENTRYPOINTS.
const MetadataKeyEntryPoint = "traefik.entrypoint"

// HostnameExtraction represents a hostname extracted from a specific router.
type HostnameExtraction struct {
	Hostname   string // The extracted hostname
	Router     string // The router name (e.g., "myapp")
	EntryPoint string // The entrypoint this extraction is bound to (empty == wildcard)
}

// Parser extracts hostnames from Traefik labels.
type Parser struct {
	logger             *slog.Logger
	defaultEntryPoints []string
}

// ParserOption is a functional option for configuring the Parser.
type ParserOption func(*Parser)

// WithParserLogger sets a custom logger.
func WithParserLogger(logger *slog.Logger) ParserOption {
	return func(p *Parser) {
		p.logger = logger
	}
}

// WithDefaultEntryPoints configures the entrypoints that an unlabeled router
// (no `traefik.http.routers.<name>.entrypoints` label, no `entryPoints` field
// in static config) should be treated as bound to.
//
// This mirrors Traefik's `entryPoints.<name>.asDefault = true` configuration:
// when set in Traefik, routers without an explicit `entryPoints` declaration
// bind only to the entrypoints flagged `asDefault`, NOT to all entrypoints.
//
// dnsweaver cannot read Traefik's static config, so users with `asDefault`
// entrypoints must declare them here so unlabeled routers get fanned out
// only across those defaults rather than treated as wildcard.
//
// When unset (default), unlabeled routers continue to produce a single
// wildcard extraction (EntryPoint=""), preserving pre-1.4.2 behavior.
func WithParserDefaultEntryPoints(eps []string) ParserOption {
	return func(p *Parser) {
		p.defaultEntryPoints = eps
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

// ExtractHostnames extracts all hostnames from Traefik labels with router
// and entrypoint context.
//
// A router that declares `traefik.http.routers.<name>.entrypoints=webA,webB`
// fans out into one extraction per (host, entrypoint) pair so per-instance
// entrypoint filtering can claim each pair independently. Routers without
// an entrypoints label produce a single extraction with EntryPoint="" —
// this mirrors Traefik's own semantics ("undeclared = bound to all
// entrypoints") and matches every DNSWEAVER_{NAME}_ENTRYPOINTS filter as a
// wildcard.
//
// Dedupe key is widened from hostname → (hostname, entrypoint) so that the
// same host bound to two entrypoints is preserved.
func (p *Parser) ExtractHostnames(labels map[string]string) []HostnameExtraction {
	// First pass: collect the entrypoints declared per router.
	routerEntryPoints := make(map[string][]string)
	for key, value := range labels {
		router := extractRouterFromEntryPointsLabel(key)
		if router == "" {
			continue
		}
		eps := splitAndTrim(value)
		if len(eps) > 0 {
			routerEntryPoints[router] = eps
		}
	}

	type seenKey struct {
		hostname   string
		entrypoint string
	}
	seen := make(map[seenKey]struct{})
	var extractions []HostnameExtraction

	// Second pass: walk router rules and fan out per (host, entrypoint).
	for key, value := range labels {
		router := extractRouterName(key)
		if router == "" {
			continue
		}

		p.logger.Debug("parsing traefik rule",
			slog.String("router", router),
			slog.String("rule", value),
		)

		hosts := extractHostsFromRule(value)
		entryPoints := routerEntryPoints[router]
		// If router declared no entrypoints and the source was configured
		// with DefaultEntryPoints (Traefik `asDefault` mirror), fan out across
		// those defaults instead of treating the router as wildcard.
		if len(entryPoints) == 0 && len(p.defaultEntryPoints) > 0 {
			entryPoints = p.defaultEntryPoints
		}

		for _, hostname := range hosts {
			if len(entryPoints) == 0 {
				// Wildcard router — single extraction with empty entrypoint.
				k := seenKey{hostname: hostname, entrypoint: ""}
				if _, exists := seen[k]; exists {
					continue
				}
				seen[k] = struct{}{}
				extractions = append(extractions, HostnameExtraction{
					Hostname: hostname,
					Router:   router,
				})
				p.logger.Debug("extracted hostname",
					slog.String("hostname", hostname),
					slog.String("router", router),
				)
				continue
			}
			for _, ep := range entryPoints {
				k := seenKey{hostname: hostname, entrypoint: ep}
				if _, exists := seen[k]; exists {
					continue
				}
				seen[k] = struct{}{}
				extractions = append(extractions, HostnameExtraction{
					Hostname:   hostname,
					Router:     router,
					EntryPoint: ep,
				})
				p.logger.Debug("extracted hostname",
					slog.String("hostname", hostname),
					slog.String("router", router),
					slog.String("entrypoint", ep),
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

// extractRouterName extracts the router name from a Traefik *.rule label.
// Returns empty string if this is not a router rule label.
//
// Examples:
//   - "traefik.http.routers.myapp.rule" -> "myapp"
//   - "traefik.http.routers.myapp.entrypoints" -> ""
//   - "traefik.enable" -> ""
func extractRouterName(key string) string {
	return extractRouterWithSuffix(key, routerRuleSuffix)
}

// extractRouterFromEntryPointsLabel extracts the router name from a Traefik
// *.entrypoints label. Returns empty string for any other label.
func extractRouterFromEntryPointsLabel(key string) string {
	return extractRouterWithSuffix(key, routerEntryPointsSuffix)
}

func extractRouterWithSuffix(key, suffix string) string {
	if !strings.HasPrefix(key, routerLabelPrefix) {
		return ""
	}
	if !strings.HasSuffix(key, suffix) {
		return ""
	}
	withoutPrefix := strings.TrimPrefix(key, routerLabelPrefix)
	withoutSuffix := strings.TrimSuffix(withoutPrefix, suffix)
	if withoutSuffix == "" {
		return ""
	}
	// Reject keys that look like sub-properties: e.g.
	// `traefik.http.routers.myapp.tls.entrypoints` is not a router-level
	// entrypoints label.
	if strings.Contains(withoutSuffix, ".") {
		return ""
	}
	return withoutSuffix
}

// splitAndTrim splits a comma-separated label value, trimming whitespace
// from each entry and discarding empties.
func splitAndTrim(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
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
