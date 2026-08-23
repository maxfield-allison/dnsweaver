// Package cloudflare implements the DNSWeaver provider interface for Cloudflare DNS.
package cloudflare

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// Provider implements provider.Provider for Cloudflare DNS.
type Provider struct {
	name       string
	zone       string // Zone name (for display/logging)
	zoneID     string // Resolved zone ID
	ttl        int
	proxied    bool
	client     *Client
	httpClient *http.Client // Custom HTTP client (optional)
	logger     *slog.Logger

	// zoneIDOnce ensures zone ID lookup happens only once
	zoneIDOnce sync.Once
	zoneIDErr  error
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

// WithProviderHTTPClient sets a custom HTTP client for the provider.
// This allows the factory to pass in a pre-configured HTTP client with
// timeout, TLS settings, and user-agent already applied.
func WithProviderHTTPClient(client *http.Client) ProviderOption {
	return func(p *Provider) {
		if client != nil {
			p.httpClient = client
		}
	}
}

// New creates a new Cloudflare provider instance.
func New(name string, config *Config, opts ...ProviderOption) (*Provider, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	p := &Provider{
		name:    name,
		zone:    config.Zone,
		zoneID:  config.ZoneID,
		ttl:     config.TTL,
		proxied: config.Proxied,
		logger:  slog.Default(),
	}

	for _, opt := range opts {
		opt(p)
	}

	// Create the API client - use custom HTTP client if provided via options
	clientOpts := []ClientOption{WithLogger(p.logger)}
	if p.httpClient != nil {
		clientOpts = append(clientOpts, WithHTTPClient(p.httpClient))
	}
	p.client = NewClient(config.Token, clientOpts...)

	return p, nil
}

// NewFromEnv creates a new Cloudflare provider from environment variables.
// This is a convenience function for use with the provider registry.
func NewFromEnv(instanceName string, opts ...ProviderOption) (*Provider, error) {
	config, err := LoadConfig(instanceName)
	if err != nil {
		return nil, err
	}

	return New(instanceName, config, opts...)
}

// NewFromMap creates a new Cloudflare provider from a configuration map.
// This is used by the provider registry Factory pattern.
func NewFromMap(name string, config map[string]string) (*Provider, error) {
	cfg := &Config{
		Token:   config["TOKEN"],
		ZoneID:  config["ZONE_ID"],
		Zone:    config["ZONE"],
		TTL:     DefaultTTL,
		Proxied: true,
	}

	// Parse TTL if provided
	if ttlStr, ok := config["TTL"]; ok && ttlStr != "" {
		var ttl int
		if _, err := fmt.Sscanf(ttlStr, "%d", &ttl); err == nil {
			cfg.TTL = ttl
		}
	}

	// Parse PROXIED if provided
	if proxiedStr, ok := config["PROXIED"]; ok && proxiedStr != "" {
		cfg.Proxied = parseBool(proxiedStr)
	}

	return New(name, cfg)
}

// Name returns the provider instance name.
func (p *Provider) Name() string {
	return p.name
}

// Type returns "cloudflare".
func (p *Provider) Type() string {
	return "cloudflare"
}

// Identity returns the backend identity for this provider instance.
// Cloudflare zone IDs are globally unique across the Cloudflare API, so the
// zone alone is sufficient to disambiguate backends. When only a zone name
// was supplied, that is used instead (the lookup happens lazily on first
// API call). See provider.ProviderIdentity, issue #88.
func (p *Provider) Identity() provider.ProviderIdentity {
	zone := p.zoneID
	if zone == "" {
		zone = p.zone
	}
	return provider.ProviderIdentity{
		Type: "cloudflare",
		Zone: zone,
	}
}

// Capabilities returns the provider's feature support.
// Cloudflare supports all features: TXT ownership, native update, and all record types.
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

// Zone returns the configured DNS zone name.
func (p *Provider) Zone() string {
	return p.zone
}

// ZoneID returns the resolved zone ID, looking it up if necessary.
func (p *Provider) ZoneID(ctx context.Context) (string, error) {
	// If zone ID was explicitly configured, use it
	if p.zoneID != "" {
		return p.zoneID, nil
	}

	// Lazy lookup zone ID from zone name
	p.zoneIDOnce.Do(func() {
		p.zoneID, p.zoneIDErr = p.client.GetZoneID(ctx, p.zone)
	})

	if p.zoneIDErr != nil {
		return "", p.zoneIDErr
	}

	return p.zoneID, nil
}

// Ping checks connectivity to the Cloudflare API.
func (p *Provider) Ping(ctx context.Context) error {
	return p.client.Ping(ctx)
}

// List returns all managed records in the zone.
// Returns A, AAAA, CNAME, and TXT records.
func (p *Provider) List(ctx context.Context) ([]provider.Record, error) {
	zoneID, err := p.ZoneID(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting zone ID: %w", err)
	}

	var records []provider.Record

	// Fetch A records
	aRecords, err := p.client.ListRecords(ctx, zoneID, "A")
	if err != nil {
		return nil, fmt.Errorf("listing A records: %w", err)
	}
	for _, r := range aRecords {
		records = append(records, provider.Record{
			Hostname:   r.Name,
			Type:       provider.RecordTypeA,
			Target:     r.Content,
			TTL:        r.TTL,
			ProviderID: r.ID,
			Metadata:   proxiedMetadata(r.Proxied),
		})
	}

	// Fetch AAAA records
	aaaaRecords, err := p.client.ListRecords(ctx, zoneID, "AAAA")
	if err != nil {
		return nil, fmt.Errorf("listing AAAA records: %w", err)
	}
	for _, r := range aaaaRecords {
		records = append(records, provider.Record{
			Hostname:   r.Name,
			Type:       provider.RecordTypeAAAA,
			Target:     r.Content,
			TTL:        r.TTL,
			ProviderID: r.ID,
			Metadata:   proxiedMetadata(r.Proxied),
		})
	}

	// Fetch CNAME records
	cnameRecords, err := p.client.ListRecords(ctx, zoneID, "CNAME")
	if err != nil {
		return nil, fmt.Errorf("listing CNAME records: %w", err)
	}
	for _, r := range cnameRecords {
		records = append(records, provider.Record{
			Hostname:   r.Name,
			Type:       provider.RecordTypeCNAME,
			Target:     r.Content,
			TTL:        r.TTL,
			ProviderID: r.ID,
			Metadata:   proxiedMetadata(r.Proxied),
		})
	}

	// Fetch TXT records
	txtRecords, err := p.client.ListRecords(ctx, zoneID, "TXT")
	if err != nil {
		return nil, fmt.Errorf("listing TXT records: %w", err)
	}
	for _, r := range txtRecords {
		records = append(records, provider.Record{
			Hostname:   r.Name,
			Type:       provider.RecordTypeTXT,
			Target:     r.Content,
			TTL:        r.TTL,
			ProviderID: r.ID,
		})
	}

	// Fetch SRV records
	srvRecords, err := p.client.ListRecords(ctx, zoneID, "SRV")
	if err != nil {
		return nil, fmt.Errorf("listing SRV records: %w", err)
	}
	for _, r := range srvRecords {
		rec := provider.Record{
			Hostname:   r.Name,
			Type:       provider.RecordTypeSRV,
			TTL:        r.TTL,
			ProviderID: r.ID,
		}
		// Cloudflare returns SRV data in the Data field
		if r.Data != nil {
			rec.Target = r.Data.Target
			rec.SRV = &provider.SRVData{
				Priority: r.Data.Priority,
				Weight:   r.Data.Weight,
				Port:     r.Data.Port,
			}
		} else {
			// Fallback: parse Content if Data is not present
			rec.Target = r.Content
		}
		records = append(records, rec)
	}

	p.logger.Debug("listed records",
		slog.String("provider", p.name),
		slog.String("zone_id", zoneID),
		slog.Int("count", len(records)),
	)

	return records, nil
}

// Create adds a new DNS record.
func (p *Provider) Create(ctx context.Context, record provider.Record) error {
	zoneID, err := p.ZoneID(ctx)
	if err != nil {
		return fmt.Errorf("getting zone ID: %w", err)
	}

	ttl := record.TTL
	if ttl <= 0 {
		ttl = p.ttl
	}

	// Determine if record should be proxied
	// Respects per-record Metadata["proxied"] override, then provider default
	proxied := p.resolveProxied(record)

	// Cloudflare uses TTL=1 for "automatic" (when proxied)
	if proxied && ttl < 60 {
		ttl = 1
	}

	// SRV records require special handling
	if record.Type == provider.RecordTypeSRV {
		if record.SRV == nil {
			return fmt.Errorf("creating SRV record: SRV data is required")
		}
		err = p.client.CreateSRVRecord(ctx, zoneID, record.Hostname, record.SRV.Priority, record.SRV.Weight, record.SRV.Port, record.Target, ttl)
		if err != nil {
			return fmt.Errorf("creating SRV record: %w", err)
		}
	} else {
		recordType := string(record.Type)
		err = p.client.CreateRecord(ctx, zoneID, recordType, record.Hostname, record.Target, ttl, proxied)
		if err != nil {
			return fmt.Errorf("creating %s record: %w", recordType, err)
		}
	}

	p.logger.Info("created record",
		slog.String("provider", p.name),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", record.Target),
		slog.Int("ttl", ttl),
		slog.Bool("proxied", proxied),
	)

	return nil
}

// Delete removes a DNS record.
func (p *Provider) Delete(ctx context.Context, record provider.Record) error {
	zoneID, err := p.ZoneID(ctx)
	if err != nil {
		return fmt.Errorf("getting zone ID: %w", err)
	}

	// Find the record to get its ID
	apiRecord, err := p.client.FindRecord(ctx, zoneID, string(record.Type), record.Hostname)
	if err != nil {
		return fmt.Errorf("finding record: %w", err)
	}
	if apiRecord == nil {
		p.logger.Warn("record not found for deletion",
			slog.String("hostname", record.Hostname),
			slog.String("type", string(record.Type)),
		)
		return nil // Record doesn't exist, nothing to delete
	}

	err = p.client.DeleteRecord(ctx, zoneID, apiRecord.ID)
	if err != nil {
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

// Update modifies an existing DNS record in place.
// This implements the provider.Updater interface for native update support.
func (p *Provider) Update(ctx context.Context, existing, desired provider.Record) error {
	zoneID, err := p.ZoneID(ctx)
	if err != nil {
		return fmt.Errorf("getting zone ID: %w", err)
	}

	// Find the existing record to get its ID
	apiRecord, err := p.client.FindRecord(ctx, zoneID, string(existing.Type), existing.Hostname)
	if err != nil {
		return fmt.Errorf("finding record: %w", err)
	}
	if apiRecord == nil {
		return provider.ErrNotFound
	}

	ttl := desired.TTL
	if ttl <= 0 {
		ttl = p.ttl
	}

	// Cloudflare's update API takes the new values
	// Determine proxied state for the desired record
	proxied := p.resolveProxied(desired)

	// Cloudflare uses TTL=1 for "automatic" (when proxied)
	if proxied && ttl < 60 {
		ttl = 1
	}

	switch desired.Type {
	case provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME, provider.RecordTypeTXT:
		err = p.client.UpdateRecord(ctx, zoneID, apiRecord.ID, string(desired.Type), desired.Hostname, desired.Target, ttl, proxied)
		if err != nil {
			return fmt.Errorf("updating %s record: %w", desired.Type, err)
		}
	case provider.RecordTypeSRV:
		// SRV records need special handling - for now, fall back to delete+create
		// Cloudflare SRV updates require different API structure
		if existing.SRV == nil || desired.SRV == nil {
			return fmt.Errorf("updating SRV record: SRV data is required")
		}
		// Delete old record
		if err := p.client.DeleteRecord(ctx, zoneID, apiRecord.ID); err != nil {
			return fmt.Errorf("deleting old SRV record for update: %w", err)
		}
		// Create new record
		if err := p.client.CreateSRVRecord(ctx, zoneID, desired.Hostname, desired.SRV.Priority, desired.SRV.Weight, desired.SRV.Port, desired.Target, ttl); err != nil {
			return fmt.Errorf("creating new SRV record for update: %w", err)
		}
	default:
		return fmt.Errorf("unsupported record type: %s", desired.Type)
	}

	p.logger.Info("updated record",
		slog.String("provider", p.name),
		slog.String("hostname", desired.Hostname),
		slog.String("type", string(desired.Type)),
		slog.String("old_target", existing.Target),
		slog.String("new_target", desired.Target),
		slog.Int("ttl", ttl),
		slog.Bool("proxied", proxied),
	)

	return nil
}

// RecordNeedsUpdate implements provider.RecordComparer. It reports whether the
// proxied state this provider would write for desired differs from the state
// existing was listed with, so that a changed dnsweaver.proxied label or a
// changed PROXIED default reaches records that already exist (issue #170).
//
// Only the proxied flag is compared. TTL is deliberately left out: Cloudflare
// reports TTL 1 ("automatic") for every proxied record regardless of what was
// requested, so comparing it would rewrite each proxied record on every pass.
// Records List did not annotate (TXT, SRV) never need an update here.
func (p *Provider) RecordNeedsUpdate(existing, desired provider.Record) bool {
	current, ok := existing.Metadata["proxied"]
	if !ok {
		return false
	}
	want, _, _ := p.proxiedFor(desired)
	return parseBool(current) != want
}

// Ensure Provider implements provider.Provider at compile time.
var _ provider.Provider = (*Provider)(nil)

// Ensure Provider implements provider.Updater at compile time.
var _ provider.Updater = (*Provider)(nil)

// Ensure Provider implements provider.RecordComparer at compile time.
var _ provider.RecordComparer = (*Provider)(nil)

// resolveProxied determines the effective proxied state for a record and
// warns when a requested proxy is refused. Priority order:
//  1. TXT/SRV records are never proxied (Cloudflare limitation)
//  2. Per-record Metadata["proxied"] override, else provider-level default
//  3. A/AAAA records whose target is a non-routable IP are forced unproxied,
//     because Cloudflare rejects proxying such targets with error 9003. This
//     overrides both the default and an explicit proxied=true (a louder
//     warning is logged in the explicit case).
//
// It is called once per write. Comparisons run every reconcile pass and use
// proxiedFor directly so the demotion warning is not repeated every interval.
func (p *Provider) resolveProxied(record provider.Record) bool {
	proxied, demoted, explicit := p.proxiedFor(record)
	if !demoted {
		return proxied
	}

	// Cloudflare cannot proxy a non-routable target; honoring proxied=true here
	// can only ever produce a 9003 API error. Demote to unproxied and explain.
	if explicit {
		p.logger.Warn("overriding explicit proxied=true: Cloudflare cannot proxy a non-routable target; creating record unproxied",
			slog.String("provider", p.name),
			slog.String("hostname", record.Hostname),
			slog.String("type", string(record.Type)),
			slog.String("target", record.Target),
		)
	} else {
		p.logger.Warn("auto-disabling Cloudflare proxy: target is not publicly routable",
			slog.String("provider", p.name),
			slog.String("hostname", record.Hostname),
			slog.String("type", string(record.Type)),
			slog.String("target", record.Target),
		)
	}
	return proxied
}

// proxiedFor applies the rules documented on resolveProxied without logging.
// demoted reports that a requested proxy was refused because the target is
// not routable; explicit reports that the request came from a per-record
// Metadata["proxied"] hint rather than the provider default.
func (p *Provider) proxiedFor(record provider.Record) (proxied, demoted, explicit bool) {
	// TXT and SRV records cannot be proxied by Cloudflare
	if record.Type == provider.RecordTypeTXT || record.Type == provider.RecordTypeSRV {
		return false, false, false
	}

	// Determine the requested proxied state and whether it was set explicitly
	// via a per-record hint (vs inherited from the provider default).
	proxied = p.proxied
	if record.Metadata != nil {
		if v, ok := record.Metadata["proxied"]; ok {
			proxied = parseBool(v)
			explicit = true
		}
	}

	if proxied && isNonRoutableTarget(record) {
		return false, true, explicit
	}

	return proxied, false, explicit
}

// isNonRoutableTarget reports whether the record's target is a literal IP that
// Cloudflare cannot proxy. Only A and AAAA records are considered; CNAME
// targets are hostnames and are left to the explicit hint / provider default,
// since deciding their routability would require runtime DNS lookups.
//
// "Non-routable" here is Cloudflare-specific: RFC1918, loopback, link-local,
// the unspecified address, IPv6 ULA (fc00::/7), and IPv4 CGNAT
// (100.64.0.0/10, RFC 6598). CGNAT is intentionally included because it is not
// publicly reachable, even though other parts of dnsweaver (e.g. the Incus IP
// resolver) deliberately keep CGNAT for Tailscale targets — different context,
// different call.
func isNonRoutableTarget(record provider.Record) bool {
	if record.Type != provider.RecordTypeA && record.Type != provider.RecordTypeAAAA {
		return false
	}
	ip := net.ParseIP(strings.TrimSpace(record.Target))
	if ip == nil {
		return false
	}
	// IsPrivate covers IPv4 RFC1918 and IPv6 ULA (fc00::/7).
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return true
	}
	// IPv4 CGNAT 100.64.0.0/10 (RFC 6598) is not covered by IsPrivate().
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return true
	}
	return false
}

// proxiedMetadata returns a Metadata map with the proxied state.
// Used by List() to populate Record.Metadata from the Cloudflare API response.
func proxiedMetadata(proxied bool) map[string]string {
	return map[string]string{"proxied": strconv.FormatBool(proxied)}
}
