package powerdns

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// Provider implements provider.Provider for the PowerDNS Authoritative API.
type Provider struct {
	name       string
	zone       string
	serverID   string
	ttl        int
	client     *Client
	logger     *slog.Logger
	httpClient *http.Client
}

// ProviderOption is a functional option for configuring the Provider.
type ProviderOption func(*Provider)

// WithProviderLogger sets a custom logger for the provider.
func WithProviderLogger(logger *slog.Logger) ProviderOption {
	return func(p *Provider) {
		if logger != nil {
			p.logger = logger
		}
	}
}

// WithProviderHTTPClient sets a pre-configured HTTP client (shared TLS/timeout/UA).
func WithProviderHTTPClient(client *http.Client) ProviderOption {
	return func(p *Provider) {
		if client != nil {
			p.httpClient = client
		}
	}
}

// New creates a new PowerDNS provider instance.
func New(name string, config *Config, opts ...ProviderOption) (*Provider, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	p := &Provider{
		name:     name,
		zone:     config.Zone,
		serverID: config.ServerID,
		ttl:      config.TTL,
		logger:   slog.Default(),
	}
	for _, opt := range opts {
		opt(p)
	}

	clientOpts := []ClientOption{WithLogger(p.logger)}
	if p.httpClient != nil {
		clientOpts = append(clientOpts, WithHTTPClient(p.httpClient))
	}
	p.client = NewClient(config.URL, config.APIKey, config.ServerID, clientOpts...)
	return p, nil
}

// NewFromEnv creates a PowerDNS provider from environment variables.
// Convenience constructor matching the provider-adding guide convention.
func NewFromEnv(instanceName string, opts ...ProviderOption) (*Provider, error) {
	config, err := LoadConfig(instanceName)
	if err != nil {
		return nil, err
	}
	return New(instanceName, config, opts...)
}

// NewFromMap creates a PowerDNS provider from a configuration map.
// Convenience constructor matching the provider-adding guide convention. The
// registry path uses Factory (which injects the shared HTTP client) instead.
func NewFromMap(name string, config map[string]string) (*Provider, error) {
	cfg, err := LoadConfigFromMap(name, config)
	if err != nil {
		return nil, err
	}
	return New(name, cfg)
}

// Name returns the provider instance name.
func (p *Provider) Name() string { return p.name }

// Type returns "powerdns".
func (p *Provider) Type() string { return "powerdns" }

// Capabilities returns the provider's feature support.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SupportsOwnershipTXT: true,
		SupportsNativeUpdate: true,
		SupportedRecordTypes: []provider.RecordType{
			provider.RecordTypeA,
			provider.RecordTypeAAAA,
			provider.RecordTypeCNAME,
			provider.RecordTypeSRV,
			provider.RecordTypeTXT,
		},
	}
}

// Identity returns the backend identity for multi-instance collision detection.
func (p *Provider) Identity() provider.ProviderIdentity {
	return provider.ProviderIdentity{
		Type:     "powerdns",
		Endpoint: p.client.baseURL,
		Zone:     p.zone,
	}
}

// Ping checks API connectivity/auth (via the client) and enforces that the
// configured zone pre-exists. dnsweaver never creates zones.
func (p *Provider) Ping(ctx context.Context) error {
	if err := p.client.Ping(ctx); err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}
	if _, err := p.client.GetZone(ctx, p.zone); err != nil {
		if errors.Is(err, errZoneNotFound) {
			return fmt.Errorf("zone %q not found on PowerDNS server %q; create the zone before using it with dnsweaver", p.zone, p.serverID)
		}
		return fmt.Errorf("ping failed: %w", err)
	}
	return nil
}

// List returns all managed records of supported types in the configured zone.
func (p *Provider) List(ctx context.Context) ([]provider.Record, error) {
	zone, err := p.client.GetZone(ctx, p.zone)
	if err != nil {
		return nil, fmt.Errorf("listing records: %w", err)
	}
	caps := p.Capabilities()
	var records []provider.Record
	for _, rs := range zone.RRsets {
		rt := provider.RecordType(rs.Type)
		if !caps.SupportsRecordType(rt) {
			continue
		}
		hostname := stripDot(rs.Name)
		for _, ar := range rs.Records {
			if ar.Disabled {
				continue
			}
			target, srv, derr := decodeContent(rt, ar.Content)
			if derr != nil {
				p.logger.Warn("skipping undecodable record",
					slog.String("provider", p.name),
					slog.String("name", rs.Name),
					slog.String("type", rs.Type),
					slog.String("error", derr.Error()),
				)
				continue
			}
			records = append(records, provider.Record{
				Hostname:   hostname,
				Type:       rt,
				Target:     target,
				TTL:        rs.TTL,
				SRV:        srv,
				ProviderID: hostname + "|" + rs.Type,
			})
		}
	}
	p.logger.Debug("listed records",
		slog.String("provider", p.name),
		slog.String("zone", p.zone),
		slog.Int("count", len(records)),
	)
	return records, nil
}

// currentRRset returns the existing rrset for hostname+type, or nil if absent.
func (p *Provider) currentRRset(ctx context.Context, hostname string, rt provider.RecordType) (*rrset, error) {
	zone, err := p.client.GetZone(ctx, p.zone)
	if err != nil {
		return nil, err
	}
	name := canonicalize(hostname)
	for i := range zone.RRsets {
		if zone.RRsets[i].Name == name && zone.RRsets[i].Type == string(rt) {
			return &zone.RRsets[i], nil
		}
	}
	return nil, nil
}

// Create adds a DNS record using read-modify-write: it merges the new content
// into the existing rrset (preserving siblings) and REPLACEs the rrset.
// If the content already exists and is disabled, it re-enables it.
func (p *Provider) Create(ctx context.Context, record provider.Record) error {
	content, err := recordContent(record)
	if err != nil {
		return fmt.Errorf("encoding %s record: %w", record.Type, err)
	}
	ttl := record.TTL
	if ttl <= 0 {
		ttl = p.ttl
	}

	rs, err := p.currentRRset(ctx, record.Hostname, record.Type)
	if err != nil {
		return fmt.Errorf("reading existing rrset: %w", err)
	}
	var existing []apiRecord
	if rs != nil {
		existing = rs.Records
	}

	// Locate a content match. If it exists AND is enabled, nothing to do.
	matchIdx := -1
	for i, ar := range existing {
		if ar.Content == content {
			matchIdx = i
			break
		}
	}
	if matchIdx >= 0 && !existing[matchIdx].Disabled {
		return nil // already present and active — idempotent no-op
	}

	// Build the replacement set: copy siblings verbatim (preserving each one's
	// Disabled flag), then either re-enable the matched record or append ours.
	merged := make([]apiRecord, len(existing))
	copy(merged, existing)
	if matchIdx >= 0 {
		merged[matchIdx].Disabled = false // re-enable a disabled managed record
	} else {
		merged = append(merged, apiRecord{Content: content})
	}

	out := rrset{
		Name:       canonicalize(record.Hostname),
		Type:       string(record.Type),
		TTL:        ttl,
		ChangeType: "REPLACE",
		Records:    merged,
	}
	if err := p.client.PatchRRsets(ctx, p.zone, []rrset{out}); err != nil {
		return fmt.Errorf("creating %s record: %w", record.Type, err)
	}
	p.logger.Info("created record",
		slog.String("provider", p.name),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", record.Target),
		slog.Int("ttl", ttl),
	)
	return nil
}

// OwnershipReplacesRRset marks the API's read-modify-write member operations.
// Compact ownership uses separate owner names; shared data RRsets remain guarded.
func (p *Provider) OwnershipReplacesRRset() bool { return true }

// Delete removes a DNS record using read-modify-write: it drops the matching
// content and REPLACEs the rrset with the remainder, or DELETEs the rrset when
// no records remain. Absent records are a no-op. Survivors' TTL and Disabled
// state are preserved verbatim from the existing rrset.
func (p *Provider) Delete(ctx context.Context, record provider.Record) error {
	content, err := recordContent(record)
	if err != nil {
		return fmt.Errorf("encoding %s record: %w", record.Type, err)
	}
	rs, err := p.currentRRset(ctx, record.Hostname, record.Type)
	if err != nil {
		return fmt.Errorf("reading existing rrset: %w", err)
	}
	if rs == nil || len(rs.Records) == 0 {
		return nil // rrset absent — nothing to delete
	}

	remaining := make([]apiRecord, 0, len(rs.Records))
	found := false
	for _, ar := range rs.Records {
		if ar.Content == content {
			found = true
			continue
		}
		remaining = append(remaining, ar) // preserve sibling verbatim (incl. Disabled)
	}
	if !found {
		return nil // content not present — no-op
	}

	var out rrset
	if len(remaining) == 0 {
		out = rrset{
			Name:       canonicalize(record.Hostname),
			Type:       string(record.Type),
			ChangeType: "DELETE",
		}
	} else {
		out = rrset{
			Name:       canonicalize(record.Hostname),
			Type:       string(record.Type),
			TTL:        rs.TTL, // preserve the existing rrset TTL — survivors are untouched
			ChangeType: "REPLACE",
			Records:    remaining,
		}
	}
	if err := p.client.PatchRRsets(ctx, p.zone, []rrset{out}); err != nil {
		return fmt.Errorf("deleting %s record: %w", record.Type, err)
	}
	p.logger.Info("deleted record",
		slog.String("provider", p.name),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", record.Target),
	)
	return nil
}

// Update modifies a record in place by swapping existing content for desired
// content within the rrset (preserving siblings) and REPLACEing it. Implements
// provider.Updater. Returns provider.ErrNotFound if the rrset, or the specific
// existing record within it, is not present.
// Siblings' Disabled state is preserved; only the written record is enabled.
func (p *Provider) Update(ctx context.Context, existing, desired provider.Record) error {
	desiredContent, err := recordContent(desired)
	if err != nil {
		return fmt.Errorf("encoding desired %s record: %w", desired.Type, err)
	}
	existingContent, err := recordContent(existing)
	if err != nil {
		return fmt.Errorf("encoding existing %s record: %w", existing.Type, err)
	}
	ttl := desired.TTL
	if ttl <= 0 {
		ttl = p.ttl
	}

	rs, err := p.currentRRset(ctx, desired.Hostname, desired.Type)
	if err != nil {
		return fmt.Errorf("reading existing rrset: %w", err)
	}
	if rs == nil {
		return provider.ErrNotFound
	}

	// The record being updated is identified by its current content. If it is
	// not present in the rrset, there is nothing to update in place; report
	// not-found per the Updater contract (the reconciler will recreate it).
	hasExisting := false
	for _, ar := range rs.Records {
		if ar.Content == existingContent {
			hasExisting = true
			break
		}
	}
	if !hasExisting {
		return provider.ErrNotFound
	}

	// Build the replacement set: collapse existingContent and any pre-existing
	// copy of desiredContent into a single ENABLED desired record (so the active
	// replacement always wins over a stale disabled copy), and preserve every
	// other sibling verbatim including its Disabled flag.
	out := make([]apiRecord, 0, len(rs.Records))
	desiredWritten := false
	for _, ar := range rs.Records {
		if ar.Content == existingContent || ar.Content == desiredContent {
			if !desiredWritten {
				out = append(out, apiRecord{Content: desiredContent}) // Disabled:false
				desiredWritten = true
			}
			continue
		}
		out = append(out, ar) // preserve sibling verbatim (incl. Disabled)
	}

	patch := rrset{
		Name:       canonicalize(desired.Hostname),
		Type:       string(desired.Type),
		TTL:        ttl,
		ChangeType: "REPLACE",
		Records:    out,
	}
	if err := p.client.PatchRRsets(ctx, p.zone, []rrset{patch}); err != nil {
		return fmt.Errorf("updating %s record: %w", desired.Type, err)
	}
	p.logger.Info("updated record",
		slog.String("provider", p.name),
		slog.String("hostname", desired.Hostname),
		slog.String("type", string(desired.Type)),
		slog.String("old_target", existing.Target),
		slog.String("new_target", desired.Target),
		slog.Int("ttl", ttl),
	)
	return nil
}

// Ensure Provider implements the required interfaces at compile time.
var _ provider.Provider = (*Provider)(nil)
var _ provider.Updater = (*Provider)(nil)
