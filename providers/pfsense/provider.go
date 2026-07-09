package pfsense

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// Provider implements provider.Provider for pfSense host overrides.
//
// One provider instance covers exactly one pfSense resolver engine on one
// firewall. Deploy multiple instances (same URL, different names) to write the
// same record set to both engines.
type Provider struct {
	name   string
	url    string // recorded for Identity reporting
	zone   string
	ttl    int
	res    resolver
	mode   ReconfigureMode
	client *client
	logger *slog.Logger

	// construction-time only
	httpClient *http.Client
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

// WithProviderHTTPClient sets the HTTP transport used by the API client. Used
// by the factory to inject the framework's TLS-configured HTTP client, and by
// tests to point at an httptest.Server.
func WithProviderHTTPClient(h *http.Client) ProviderOption {
	return func(p *Provider) {
		if h != nil {
			p.httpClient = h
		}
	}
}

// New constructs a pfSense Provider. cfg must be validated by the caller (or
// via LoadConfig/LoadConfigFromMap, which validate on load).
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
		res:    resolverFor(cfg.Engine),
		mode:   cfg.ReconfigureMode,
		logger: slog.Default(),
	}
	for _, o := range opts {
		o(p)
	}

	p.client = newClient(cfg.URL, cfg.APIKey, p.res,
		withLogger(p.logger), withHTTPClient(p.httpClient))
	return p, nil
}

// Name returns the provider instance name.
func (p *Provider) Name() string { return p.name }

// Type returns "pfsense".
func (p *Provider) Type() string { return "pfsense" }

// Identity uniquely identifies the backend this provider writes to. Two
// pfsense instances share an identity when they target the same URL, engine,
// and zone, so the reconciler treats them as first-match-wins to avoid
// competing writes.
func (p *Provider) Identity() provider.ProviderIdentity {
	return provider.ProviderIdentity{
		Type:     "pfsense/" + string(p.res.name),
		Endpoint: p.url,
		Zone:     p.zone,
	}
}

// Capabilities describes what the pfSense provider can do. Host overrides do
// not carry TXT records, so ownership TXT tracking is unavailable — the
// reconciler falls back to target-matching for orphan detection. Native update
// is unsupported in v1; the reconciler uses delete+create.
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

// Ping checks connectivity by listing host overrides for the configured
// resolver. A successful list proves the API is reachable, the key works, and
// the REST API package exposes the resolver's endpoints — three failure modes
// that all matter for downstream operations.
func (p *Provider) Ping(ctx context.Context) error {
	if _, err := p.client.list(ctx); err != nil {
		if errors.Is(err, provider.ErrNotFound) {
			return fmt.Errorf("%w: pfsense reachable but the %s host-override endpoint was not found (is the REST API package installed and the %s resolver enabled?)",
				provider.ErrProviderUnavailable, p.res.name, p.res.name)
		}
		return err
	}
	return nil
}

// List returns all dnsweaver-managed host overrides in the configured zone.
// Records without the dnsweaver ownership marker are omitted so operator-managed
// host overrides cannot be deleted by orphan cleanup. Each IP on an override is
// expanded into its own A/AAAA record.
func (p *Provider) List(ctx context.Context) ([]provider.Record, error) {
	overrides, err := p.client.list(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing records: %w", err)
	}

	records := make([]provider.Record, 0, len(overrides))
	for _, ho := range overrides {
		if !isOwnedBy(ho.Descr) {
			continue
		}
		fqdn := joinFQDN(ho.Host, ho.Domain)
		if p.zone != "" && !inZone(fqdn, p.zone) {
			continue
		}
		for _, ip := range ho.IPs {
			rt, ok := recordTypeForIP(ip)
			if !ok {
				p.logger.Debug("skipping host override IP that is not a valid address",
					slog.String("provider", p.name),
					slog.String("fqdn", fqdn),
					slog.String("ip", ip),
				)
				continue
			}
			records = append(records, provider.Record{
				Hostname:   fqdn,
				Type:       rt,
				Target:     ip,
				TTL:        p.ttl,
				ProviderID: ho.ID,
			})
		}
	}

	p.logger.Debug("listed records",
		slog.String("provider", p.name),
		slog.String("engine", string(p.res.name)),
		slog.Int("overrides", len(overrides)),
		slog.Int("matched", len(records)),
	)
	return records, nil
}

// Create adds or extends a host override. TXT records are silently skipped
// (pfSense host overrides do not support TXT). On the Unbound resolver a new
// IP is merged into an existing (host, domain) override; on the Dnsmasq
// forwarder, which stores a single IP per override, a conflicting second IP is
// reported as an error.
func (p *Provider) Create(ctx context.Context, record provider.Record) error {
	switch record.Type {
	case provider.RecordTypeA, provider.RecordTypeAAAA:
		// supported
	case provider.RecordTypeTXT:
		p.logger.Debug("skipping TXT record (pfsense host overrides do not support TXT)",
			slog.String("provider", p.name),
			slog.String("hostname", record.Hostname))
		return nil
	default:
		return fmt.Errorf("%s records are not supported by the pfsense provider (v1: A/AAAA only)", record.Type)
	}

	if net.ParseIP(record.Target) == nil {
		return fmt.Errorf("pfsense provider requires an IP target for %s, got %q", record.Type, record.Target)
	}

	host, domain, ok := splitFQDN(record.Hostname)
	if !ok {
		return fmt.Errorf("hostname %q must be a fully qualified name with at least one dot", record.Hostname)
	}

	existing, found, err := p.findOverride(ctx, host, domain)
	if err != nil {
		return fmt.Errorf("creating record %s: %w", record.Hostname, err)
	}

	switch {
	case found && !isOwnedBy(existing.Descr):
		return fmt.Errorf("refusing to modify host override %s: it exists on pfsense but is not managed by dnsweaver (description %q)",
			record.Hostname, existing.Descr)
	case found && existing.containsIP(record.Target):
		// Already present — nothing to do.
		return nil
	case found && !p.res.multiIP:
		return fmt.Errorf("the dnsmasq engine (DNS Forwarder) stores one IP per host override; %s already resolves to %v. Use the unbound engine for dual-stack (A+AAAA)",
			record.Hostname, existing.IPs)
	case found:
		existing.IPs = append(existing.IPs, record.Target)
		existing.Descr = ownershipDescription(p.name)
		if err := p.client.update(ctx, existing); err != nil {
			return fmt.Errorf("creating record %s: %w", record.Hostname, err)
		}
	default:
		newHO := hostOverride{
			Host:   host,
			Domain: domain,
			IPs:    []string{record.Target},
			Descr:  ownershipDescription(p.name),
		}
		if err := p.client.create(ctx, newHO); err != nil {
			return fmt.Errorf("creating record %s: %w", record.Hostname, err)
		}
	}

	if err := p.maybeApply(ctx); err != nil {
		return fmt.Errorf("record %s created but apply failed: %w", record.Hostname, err)
	}

	p.logger.Info("created DNS record",
		slog.String("provider", p.name),
		slog.String("engine", string(p.res.name)),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", record.Target),
	)
	return nil
}

// Delete removes a host override, or a single IP from a multi-IP override. Only
// records owned by this instance are touched, so operator-managed host
// overrides are never deleted.
func (p *Provider) Delete(ctx context.Context, record provider.Record) error {
	switch record.Type {
	case provider.RecordTypeA, provider.RecordTypeAAAA:
		// supported
	case provider.RecordTypeTXT:
		return nil
	default:
		return fmt.Errorf("%s records are not supported by the pfsense provider", record.Type)
	}

	host, domain, ok := splitFQDN(record.Hostname)
	if !ok {
		return fmt.Errorf("hostname %q must be a fully qualified name with at least one dot", record.Hostname)
	}

	existing, found, err := p.findOverride(ctx, host, domain)
	if err != nil {
		return fmt.Errorf("deleting record %s: %w", record.Hostname, err)
	}
	if !found || !ownedByInstance(existing.Descr, p.name) {
		return provider.ErrNotFound
	}
	if record.Target != "" && !existing.containsIP(record.Target) {
		return provider.ErrNotFound
	}

	remaining := existing.withoutIP(record.Target)
	if p.res.multiIP && record.Target != "" && len(remaining) > 0 {
		existing.IPs = remaining
		if err := p.client.update(ctx, existing); err != nil {
			return fmt.Errorf("deleting record %s: %w", record.Hostname, err)
		}
	} else {
		if err := p.client.remove(ctx, existing.ID); err != nil {
			return fmt.Errorf("deleting record %s: %w", record.Hostname, err)
		}
	}

	if err := p.maybeApply(ctx); err != nil {
		return fmt.Errorf("record %s deleted but apply failed: %w", record.Hostname, err)
	}

	p.logger.Info("deleted DNS record",
		slog.String("provider", p.name),
		slog.String("engine", string(p.res.name)),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
	)
	return nil
}

// findOverride looks up the host override for a (host, domain) pair. It returns
// every match regardless of ownership so callers can refuse to touch
// operator-managed rows.
func (p *Provider) findOverride(ctx context.Context, host, domain string) (hostOverride, bool, error) {
	overrides, err := p.client.list(ctx)
	if err != nil {
		return hostOverride{}, false, err
	}
	for _, ho := range overrides {
		if joinFQDN(ho.Host, ho.Domain) == joinFQDN(host, domain) {
			return ho, true, nil
		}
	}
	return hostOverride{}, false, nil
}

// maybeApply triggers a resolver reload when the configured mode calls for it.
func (p *Provider) maybeApply(ctx context.Context) error {
	if p.mode != ReconfigureModePerWrite {
		return nil
	}
	return p.client.apply(ctx)
}
