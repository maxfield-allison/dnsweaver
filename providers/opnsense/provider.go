package opnsense

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// Provider implements provider.Provider for OPNsense host overrides.
//
// One provider instance covers exactly one OPNsense resolver engine on one
// firewall. Deploy multiple instances (with the same URL, different names)
// if you want to write the same record set to both engines on one firewall.
type Provider struct {
	name   string
	url    string // recorded for Identity reporting
	zone   string
	ttl    int
	engine engine
	mode   ReconfigureMode
	client *Client
	logger *slog.Logger
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// ProviderOption configures a Provider at construction time.
type ProviderOption func(*Provider)

// WithProviderLogger sets a structured logger for the provider.
func WithProviderLogger(logger *slog.Logger) ProviderOption {
	return func(p *Provider) {
		if logger != nil {
			p.logger = logger
		}
	}
}

// WithProviderHTTPClient replaces the client's HTTP transport. Used by the
// factory to inject the framework's TLS-configured HTTP client, and by tests
// to point at an httptest.Server.
func WithProviderHTTPClient(h *http.Client) ProviderOption {
	return func(p *Provider) {
		if h != nil && p.client != nil {
			p.client.httpClient = h
		}
	}
}

// WithClient replaces the API client (test-only helper).
func WithClient(c *Client) ProviderOption {
	return func(p *Provider) { p.client = c }
}

// New constructs an OPNsense Provider. cfg must be validated by the caller
// (or via LoadConfig/LoadConfigFromMap, which validate on load).
func New(name string, cfg *Config, opts ...ProviderOption) (*Provider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	p := &Provider{
		name:   name,
		url:    cfg.URL,
		zone:   cfg.Zone,
		ttl:    cfg.TTL,
		engine: newEngine(cfg.Engine),
		mode:   cfg.ReconfigureMode,
		logger: slog.Default(),
		client: NewClient(cfg.URL, cfg.APIKey, cfg.APISecret),
	}
	for _, o := range opts {
		o(p)
	}
	return p, nil
}

// Name returns the provider instance name.
func (p *Provider) Name() string { return p.name }

// Type returns "opnsense".
func (p *Provider) Type() string { return "opnsense" }

// Identity uniquely identifies the backend this provider writes to. Two
// opnsense instances share an identity when they target the same URL, engine,
// and zone. Instances that share an identity also share a record set on
// OPNsense, so the reconciler treats them as first-match-wins to avoid
// competing writes (see pkg/provider docs).
func (p *Provider) Identity() provider.ProviderIdentity {
	return provider.ProviderIdentity{
		Type:     "opnsense/" + string(p.engine.Name()),
		Endpoint: p.url,
		Zone:     p.zone,
	}
}

// Capabilities describes what the OPNsense provider can do. Host overrides
// do not carry TXT records, so ownership TXT tracking is unavailable — the
// reconciler falls back to target-matching for orphan detection. Native
// update is unsupported in v1; the reconciler will use delete+create.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SupportsOwnershipTXT: false,
		SupportsNativeUpdate: false,
		SupportedRecordTypes: []provider.RecordType{
			provider.RecordTypeA,
			provider.RecordTypeAAAA,
		},
	}
}

// Ping checks connectivity to OPNsense by issuing a lightweight search
// against the configured engine. A successful search proves the API is
// reachable, credentials work, and the target engine is enabled — three
// failure modes that all matter for downstream operations.
func (p *Provider) Ping(ctx context.Context) error {
	body, status, err := p.client.do(ctx, p.engine.SearchPath(), newSearchRequest())
	if err != nil {
		return fmt.Errorf("%w: %w", provider.ErrProviderUnavailable, err)
	}
	if statusErr := mapStatusError(status, body); statusErr != nil {
		return statusErr
	}
	// Even a decode failure at this stage is a real signal: OPNsense is
	// reachable but returning something the engine can't parse, which
	// usually means the engine module is disabled or a version mismatch.
	if _, err := p.engine.DecodeSearchResponse(body); err != nil {
		return fmt.Errorf("opnsense reachable but %s search response is unparseable (is the %s module enabled?): %w",
			p.engine.Name(), p.engine.Name(), err)
	}
	return nil
}

// List returns all dnsweaver-managed host overrides in the configured zone.
// Records without the dnsweaver ownership marker in their description are
// intentionally omitted, so operator-managed host overrides cannot be
// accidentally deleted by orphan cleanup.
func (p *Provider) List(ctx context.Context) ([]provider.Record, error) {
	body, status, err := p.client.do(ctx, p.engine.SearchPath(), newSearchRequest())
	if err != nil {
		return nil, fmt.Errorf("listing records: %w", err)
	}
	if statusErr := mapStatusError(status, body); statusErr != nil {
		return nil, statusErr
	}
	rows, err := p.engine.DecodeSearchResponse(body)
	if err != nil {
		return nil, err
	}

	records := make([]provider.Record, 0, len(rows))
	for _, r := range rows {
		// Skip records dnsweaver didn't create. This is the load-bearing
		// safety check: without it, orphan cleanup would happily delete
		// every operator-managed host override in the zone.
		if !isOwnedBy(r.Description) {
			continue
		}

		fqdn := joinFQDN(r.Hostname, r.Domain)
		if p.zone != "" && !inZone(fqdn, p.zone) {
			continue
		}

		rt, ok := recordTypeFromString(r.Type)
		if !ok {
			p.logger.Debug("skipping record with unsupported type",
				slog.String("provider", p.name),
				slog.String("fqdn", fqdn),
				slog.String("type", r.Type),
			)
			continue
		}

		records = append(records, provider.Record{
			Hostname:   fqdn,
			Type:       rt,
			Target:     r.Target,
			TTL:        p.ttl,
			ProviderID: r.UUID,
		})
	}

	p.logger.Debug("listed records",
		slog.String("provider", p.name),
		slog.String("engine", string(p.engine.Name())),
		slog.Int("total_rows", len(rows)),
		slog.Int("matched", len(records)),
	)
	return records, nil
}

// Create adds a new host override. TXT records are silently skipped
// (OPNsense host overrides do not support TXT). Other unsupported types
// return an error so misconfiguration surfaces loudly.
func (p *Provider) Create(ctx context.Context, record provider.Record) error {
	switch record.Type {
	case provider.RecordTypeA, provider.RecordTypeAAAA:
		// supported
	case provider.RecordTypeTXT:
		p.logger.Debug("skipping TXT record (opnsense host overrides do not support TXT)",
			slog.String("provider", p.name),
			slog.String("hostname", record.Hostname))
		return nil
	default:
		return fmt.Errorf("%s records are not supported by the opnsense provider (v1: A/AAAA only)", record.Type)
	}

	if net.ParseIP(record.Target) == nil {
		return fmt.Errorf("opnsense provider requires an IP target for %s, got %q", record.Type, record.Target)
	}

	hostname, domain, ok := splitFQDN(record.Hostname)
	if !ok {
		return fmt.Errorf("hostname %q must be a fully qualified name with at least one dot", record.Hostname)
	}

	rec := hostRecord{
		Hostname:    hostname,
		Domain:      domain,
		Type:        string(record.Type),
		Target:      record.Target,
		Description: ownershipDescription(p.name),
		Enabled:     true,
	}

	payload, err := p.engine.EncodeAddPayload(rec)
	if err != nil {
		return fmt.Errorf("encoding add payload: %w", err)
	}

	body, status, err := p.client.do(ctx, p.engine.AddPath(), payload)
	if err != nil {
		return fmt.Errorf("creating record %s: %w", record.Hostname, err)
	}
	if statusErr := mapStatusError(status, body); statusErr != nil {
		return statusErr
	}
	if err := parseMutationResponse(body); err != nil {
		return fmt.Errorf("creating record %s: %w", record.Hostname, err)
	}

	if err := p.maybeReconfigure(ctx); err != nil {
		return fmt.Errorf("record %s created but reconfigure failed: %w", record.Hostname, err)
	}

	p.logger.Info("created DNS record",
		slog.String("provider", p.name),
		slog.String("engine", string(p.engine.Name())),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", record.Target),
	)
	return nil
}

// Delete removes a host override. The record's ProviderID (the OPNsense
// UUID) is preferred; if empty we fall back to a lookup by (hostname, type,
// target) so callers who construct records by hand still work.
func (p *Provider) Delete(ctx context.Context, record provider.Record) error {
	switch record.Type {
	case provider.RecordTypeA, provider.RecordTypeAAAA:
		// supported
	case provider.RecordTypeTXT:
		return nil
	default:
		return fmt.Errorf("%s records are not supported by the opnsense provider", record.Type)
	}

	uuid := record.ProviderID
	if uuid == "" {
		found, err := p.findUUID(ctx, record)
		if err != nil {
			return err
		}
		uuid = found
	}
	if uuid == "" {
		return provider.ErrNotFound
	}

	body, status, err := p.client.do(ctx, p.engine.DelPath(uuid), nil)
	if err != nil {
		return fmt.Errorf("deleting record %s: %w", record.Hostname, err)
	}
	if statusErr := mapStatusError(status, body); statusErr != nil {
		return statusErr
	}
	if err := parseMutationResponse(body); err != nil {
		return fmt.Errorf("deleting record %s: %w", record.Hostname, err)
	}

	if err := p.maybeReconfigure(ctx); err != nil {
		return fmt.Errorf("record %s deleted but reconfigure failed: %w", record.Hostname, err)
	}

	p.logger.Info("deleted DNS record",
		slog.String("provider", p.name),
		slog.String("engine", string(p.engine.Name())),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
	)
	return nil
}

// findUUID looks up a record by (hostname, type, target). Only searches
// dnsweaver-owned rows so we can never accidentally delete an operator's
// host override that happens to share hostname/target.
func (p *Provider) findUUID(ctx context.Context, record provider.Record) (string, error) {
	body, status, err := p.client.do(ctx, p.engine.SearchPath(), newSearchRequest())
	if err != nil {
		return "", fmt.Errorf("searching for %s: %w", record.Hostname, err)
	}
	if statusErr := mapStatusError(status, body); statusErr != nil {
		return "", statusErr
	}
	rows, err := p.engine.DecodeSearchResponse(body)
	if err != nil {
		return "", err
	}

	targetFQDN := strings.TrimSuffix(strings.ToLower(record.Hostname), ".")
	for _, r := range rows {
		if !ownedByInstance(r.Description, p.name) {
			continue
		}
		if !strings.EqualFold(joinFQDN(r.Hostname, r.Domain), targetFQDN) {
			continue
		}
		if !strings.EqualFold(r.Type, string(record.Type)) {
			continue
		}
		if record.Target != "" && !strings.EqualFold(r.Target, record.Target) {
			continue
		}
		return r.UUID, nil
	}
	return "", nil
}

// maybeReconfigure triggers a resolver reload when the configured mode calls
// for it. Errors are propagated so callers can decide whether to retry the
// underlying mutation.
func (p *Provider) maybeReconfigure(ctx context.Context) error {
	if p.mode != ReconfigureModePerWrite {
		return nil
	}
	body, status, err := p.client.do(ctx, p.engine.ReconfigurePath(), nil)
	if err != nil {
		return fmt.Errorf("reconfigure: %w", err)
	}
	if statusErr := mapStatusError(status, body); statusErr != nil {
		return fmt.Errorf("reconfigure: %w", statusErr)
	}
	// Reconfigure responses use a "status" field ("ok") rather than the
	// standard result envelope. Anything non-2xx has already been rejected
	// by mapStatusError, so we don't need to parse the body strictly here.
	return nil
}

// joinFQDN glues a hostname and domain into a fully qualified name, trimming
// trailing dots and lowercasing so comparisons are stable.
func joinFQDN(hostname, domain string) string {
	hostname = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hostname)), ".")
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	switch {
	case hostname == "" && domain == "":
		return ""
	case hostname == "":
		return domain
	case domain == "":
		return hostname
	default:
		return hostname + "." + domain
	}
}

// splitFQDN cleaves a FQDN into (hostname, domain). Returns ok=false if
// there is no dot to split on — OPNsense host overrides always require
// both a hostname and a domain.
func splitFQDN(fqdn string) (hostname, domain string, ok bool) {
	fqdn = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fqdn)), ".")
	idx := strings.Index(fqdn, ".")
	if idx <= 0 || idx == len(fqdn)-1 {
		return "", "", false
	}
	return fqdn[:idx], fqdn[idx+1:], true
}

// inZone returns true if fqdn is equal to or a subdomain of zone.
func inZone(fqdn, zone string) bool {
	fqdn = strings.TrimSuffix(strings.ToLower(fqdn), ".")
	zone = strings.TrimSuffix(strings.ToLower(zone), ".")
	if fqdn == zone {
		return true
	}
	return strings.HasSuffix(fqdn, "."+zone)
}

// recordTypeFromString maps OPNsense's RR strings to framework record types.
func recordTypeFromString(s string) (provider.RecordType, bool) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "A":
		return provider.RecordTypeA, true
	case "AAAA":
		return provider.RecordTypeAAAA, true
	default:
		return "", false
	}
}
