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
	zoneIDOnce        sync.Once
	zoneIDErr         error
	ownershipZoneMu   sync.Mutex
	ownershipZoneName string
}

// Live qualification found content-dependent rejection of large encoded TXT
// values despite acceptance of repetitive 3,500-byte probes. Keep new ownership
// pages and manifests within a conservative bound; readers still accept v1.
const ownershipTXTValueBytes = 2000

// OwnershipTXTValueBytes returns the qualified bound for new ownership values.
func (p *Provider) OwnershipTXTValueBytes() int { return ownershipTXTValueBytes }

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
			provider.RecordTypeMX,
			provider.RecordTypeNS,
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

// Ping checks connectivity to the Cloudflare API against the configured zone.
func (p *Provider) Ping(ctx context.Context) error {
	zoneID, err := p.ZoneID(ctx)
	if err != nil {
		return fmt.Errorf("ping failed: getting zone ID: %w", err)
	}
	return p.client.Ping(ctx, zoneID)
}

// List returns all managed records in the zone.
// Returns A, AAAA, CNAME, TXT, SRV, MX, and NS records.
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
		content, contentErr := listedTXTContent(r.Name, r.Content)
		if contentErr != nil {
			return nil, contentErr
		}
		records = append(records, provider.Record{
			Hostname:   r.Name,
			Type:       provider.RecordTypeTXT,
			Target:     content,
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

	// Fetch MX records
	mxRecords, err := p.client.ListRecords(ctx, zoneID, "MX")
	if err != nil {
		return nil, fmt.Errorf("listing MX records: %w", err)
	}
	for _, r := range mxRecords {
		rec := provider.Record{
			Hostname:   r.Name,
			Type:       provider.RecordTypeMX,
			Target:     r.Content,
			TTL:        r.TTL,
			ProviderID: r.ID,
			MX:         &provider.MXData{},
		}
		if r.Priority != nil {
			rec.MX.Preference = *r.Priority
		}
		records = append(records, rec)
	}

	// Fetch NS records
	nsRecords, err := p.client.ListRecords(ctx, zoneID, "NS")
	if err != nil {
		return nil, fmt.Errorf("listing NS records: %w", err)
	}
	for _, r := range nsRecords {
		records = append(records, provider.Record{
			Hostname:   r.Name,
			Type:       provider.RecordTypeNS,
			Target:     r.Content,
			TTL:        r.TTL,
			ProviderID: r.ID,
		})
	}

	p.logger.Debug("listed records",
		slog.String("provider", p.name),
		slog.String("zone_id", zoneID),
		slog.Int("count", len(records)),
	)

	return records, nil
}

// OwnershipZone resolves the name even when configuration supplies only an ID.
func (p *Provider) OwnershipZone(ctx context.Context) (string, error) {
	p.ownershipZoneMu.Lock()
	defer p.ownershipZoneMu.Unlock()
	if p.ownershipZoneName != "" {
		return p.ownershipZoneName, nil
	}
	zoneID, err := p.ZoneID(ctx)
	if err != nil {
		return "", err
	}
	zone, err := p.client.GetZoneName(ctx, zoneID)
	if err == nil {
		p.ownershipZoneName = zone
	}
	return zone, err
}

// ListOwnershipState uses the paginated TXT endpoint for publication readbacks.
func (p *Provider) ListOwnershipState(ctx context.Context) ([]provider.Record, error) {
	zoneID, err := p.ZoneID(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := p.client.ListRecords(ctx, zoneID, "TXT")
	if err != nil {
		return nil, err
	}
	records := make([]provider.Record, 0, len(raw))
	for _, r := range raw {
		content, err := compactTXTContent(r.Name, r.Content)
		if err != nil {
			return nil, err
		}
		records = append(records, provider.Record{Hostname: r.Name, Type: provider.RecordTypeTXT, Target: content, TTL: r.TTL, ProviderID: r.ID})
	}
	return records, nil
}

// listedTXTContent returns the TXT value as it was declared. Cloudflare reads
// TXT content back as quoted wire strings even when it was written unquoted,
// so data TXT is compared on its decoded strings. Content that is not a
// well-formed sequence of quoted strings is kept as listed.
func listedTXTContent(name, value string) (string, error) {
	if provider.IsOwnershipStateRecord(name) {
		return compactTXTContent(name, value)
	}
	if provider.IsOwnershipRecord(name) {
		content, err := compactTXTContent(name, value)
		if err != nil {
			return "", err
		}
		return provider.MapMemberOwnershipTXTTarget(content, provider.TXTPayload), nil
	}
	if decoded, ok := provider.DecodeTXTStrings(value); ok {
		return decoded, nil
	}
	return value, nil
}

// apiContent is the content field for record. Data TXT is written so that
// listedTXTContent reads back the same data: Cloudflare takes a value that
// starts with a quote as presentation text, so such data is sent quoted.
func apiContent(record provider.Record) string {
	switch {
	case record.Type != provider.RecordTypeTXT || provider.IsOwnershipStateRecord(record.Hostname):
		return record.Target
	case provider.IsOwnershipRecord(record.Hostname):
		// Member markers keep a TXT target in the same presentation form as the
		// declared content, which is also how earlier releases recorded it.
		return provider.MapMemberOwnershipTXTTarget(record.Target, provider.TXTPresentation)
	default:
		return provider.TXTPresentation(record.Target)
	}
}

// TXTPresentationText implements provider.TXTPresentationText: Cloudflare
// stores TXT content as quoted presentation text.
func (p *Provider) TXTPresentationText() {}

// Cloudflare can return one TXT RR as quoted wire strings. Compact payloads
// concatenate those strings; unrelated user TXT retains its existing meaning.
func compactTXTContent(name, value string) (string, error) {
	state := provider.IsOwnershipStateRecord(name)
	if (!state && !provider.IsOwnershipRecord(name)) || !strings.HasPrefix(value, "\"") {
		return value, nil
	}
	decoded, ok := provider.DecodeTXTStrings(value)
	if !ok {
		return "", fmt.Errorf("%w: invalid quoted ownership TXT", provider.ErrOwnershipState)
	}
	if !state && !provider.IsDnsweaverOwned(decoded) {
		return value, nil
	}
	return decoded, nil
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

	// SRV and MX carry structured data beyond the target and need their own
	// request shapes; every other type is content-only.
	switch record.Type {
	case provider.RecordTypeSRV:
		if record.SRV == nil {
			return fmt.Errorf("creating SRV record: SRV data is required")
		}
		err = p.client.CreateSRVRecord(ctx, zoneID, record.Hostname, record.SRV.Priority, record.SRV.Weight, record.SRV.Port, record.Target, ttl)
		if err != nil {
			return fmt.Errorf("creating SRV record: %w", err)
		}
	case provider.RecordTypeMX:
		if record.MX == nil {
			return fmt.Errorf("creating MX record: MX data is required")
		}
		err = p.client.CreateMXRecord(ctx, zoneID, record.Hostname, record.Target, record.MX.Preference, ttl)
		if err != nil {
			return fmt.Errorf("creating MX record: %w", err)
		}
	default:
		recordType := string(record.Type)
		err = p.client.CreateRecord(ctx, zoneID, recordType, record.Hostname, apiContent(record), ttl, proxied)
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

	// Resolve the exact member. A hostname and type can have multiple values,
	// so selecting the first API result can delete an unrelated sibling.
	apiRecord, err := p.findRecord(ctx, zoneID, record)
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

	// Resolve the exact existing member before updating it.
	apiRecord, err := p.findRecord(ctx, zoneID, existing)
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
	case provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME, provider.RecordTypeTXT, provider.RecordTypeNS:
		err = p.client.UpdateRecord(ctx, zoneID, apiRecord.ID, string(desired.Type), desired.Hostname, apiContent(desired), ttl, proxied)
		if err != nil {
			return fmt.Errorf("updating %s record: %w", desired.Type, err)
		}
	case provider.RecordTypeMX:
		if desired.MX == nil {
			return fmt.Errorf("updating MX record: MX data is required")
		}
		if err := p.client.UpdateMXRecord(ctx, zoneID, apiRecord.ID, desired.Hostname, desired.Target, desired.MX.Preference, ttl); err != nil {
			return fmt.Errorf("updating MX record: %w", err)
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

// findRecord resolves the Cloudflare API record for one logical DNS member.
// ProviderID is authoritative when the record came from List. Records built by
// cleanup paths may not carry an ID, so those are matched by their full value.
func (p *Provider) findRecord(ctx context.Context, zoneID string, record provider.Record) (*dnsRecord, error) {
	if record.ProviderID != "" {
		return &dnsRecord{ID: record.ProviderID}, nil
	}

	records, err := p.client.FindRecords(ctx, zoneID, string(record.Type), record.Hostname)
	if err != nil {
		return nil, err
	}
	for i := range records {
		if cloudflareRecordMatches(records[i], record) {
			return &records[i], nil
		}
	}
	return nil, nil
}

func cloudflareRecordMatches(apiRecord dnsRecord, record provider.Record) bool {
	if !strings.EqualFold(strings.TrimSuffix(apiRecord.Name, "."), strings.TrimSuffix(record.Hostname, ".")) ||
		!strings.EqualFold(apiRecord.Type, string(record.Type)) {
		return false
	}

	if record.Type == provider.RecordTypeSRV {
		if apiRecord.Data == nil || record.SRV == nil {
			return false
		}
		return strings.EqualFold(strings.TrimSuffix(apiRecord.Data.Target, "."), strings.TrimSuffix(record.Target, ".")) &&
			apiRecord.Data.Priority == record.SRV.Priority &&
			apiRecord.Data.Weight == record.SRV.Weight &&
			apiRecord.Data.Port == record.SRV.Port
	}

	if record.Type == provider.RecordTypeA || record.Type == provider.RecordTypeAAAA {
		apiIP, recordIP := net.ParseIP(apiRecord.Content), net.ParseIP(record.Target)
		return apiIP != nil && recordIP != nil && apiIP.Equal(recordIP)
	}

	if record.Type == provider.RecordTypeMX {
		var preference uint16
		if record.MX != nil {
			preference = record.MX.Preference
		}
		return apiRecord.Priority != nil && *apiRecord.Priority == preference &&
			strings.EqualFold(strings.TrimSuffix(apiRecord.Content, "."), strings.TrimSuffix(record.Target, "."))
	}

	if record.Type == provider.RecordTypeCNAME || record.Type == provider.RecordTypeNS {
		return strings.EqualFold(strings.TrimSuffix(apiRecord.Content, "."), strings.TrimSuffix(record.Target, "."))
	}

	if record.Type == provider.RecordTypeTXT {
		content, err := listedTXTContent(apiRecord.Name, apiRecord.Content)
		return err == nil && content == record.Target
	}

	return apiRecord.Content == record.Target
}

// RecordNeedsUpdate implements provider.RecordComparer. It reports whether the
// proxied state this provider would write for desired differs from the state
// existing was listed with, so that a changed dnsweaver.proxied label or a
// changed PROXIED default reaches records that already exist (issue #170).
//
// Only the proxied flag is compared. TTL is deliberately left out: Cloudflare
// reports TTL 1 ("automatic") for every proxied record regardless of what was
// requested, so comparing it would rewrite each proxied record on every pass.
// Records List did not annotate (TXT, SRV, MX, NS) never need an update here.
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

// Ensure Provider implements provider.TXTPresentationText at compile time.
var _ provider.TXTPresentationText = (*Provider)(nil)

// resolveProxied determines the effective proxied state for a record and
// warns when a requested proxy is refused. Priority order:
//  1. TXT/SRV/MX/NS records are never proxied (Cloudflare limitation)
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
	if !isProxiableType(record.Type) {
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

// isProxiableType reports whether Cloudflare can serve a record type through
// its proxy. TXT, SRV, MX and NS are always answered straight from DNS.
func isProxiableType(rt provider.RecordType) bool {
	switch rt {
	case provider.RecordTypeTXT, provider.RecordTypeSRV, provider.RecordTypeMX, provider.RecordTypeNS:
		return false
	default:
		return true
	}
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
