// Package ovh implements the dnsweaver provider interface for OVHcloud DNS.
package ovh

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// Provider implements provider.Provider for OVHcloud DNS.
type Provider struct {
	name       string
	zone       string
	endpoint   string
	ttl        int
	client     *Client
	httpClient *http.Client
	logger     *slog.Logger
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

// New creates a new OVH provider instance.
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
		endpoint: config.Endpoint,
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
	p.client = NewClient(config.EndpointURL(), config.ApplicationKey, config.ApplicationSecret, config.ConsumerKey, clientOpts...)

	return p, nil
}

// NewFromEnv creates a new OVH provider from environment variables.
func NewFromEnv(instanceName string, opts ...ProviderOption) (*Provider, error) {
	config, err := LoadConfig(instanceName)
	if err != nil {
		return nil, err
	}

	return New(instanceName, config, opts...)
}

// NewFromMap creates a new OVH provider from a configuration map.
// This is used by the provider registry Factory pattern.
func NewFromMap(name string, config map[string]string) (*Provider, error) {
	cfg, err := LoadConfigFromMap(name, config)
	if err != nil {
		return nil, err
	}

	return New(name, cfg)
}

// Name returns the provider instance name.
func (p *Provider) Name() string {
	return p.name
}

// Type returns "ovh".
func (p *Provider) Type() string {
	return "ovh"
}

// Identity returns the backend identity for this provider instance.
// An OVH zone is scoped to an API region, so the endpoint and zone together
// identify the backing record store. See provider.ProviderIdentity, issue #88.
func (p *Provider) Identity() provider.ProviderIdentity {
	return provider.ProviderIdentity{
		Type:     "ovh",
		Endpoint: p.endpoint,
		Zone:     p.zone,
	}
}

// Capabilities returns the provider's feature support.
// OVH supports TXT ownership records, native updates, and all common record types.
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

// Ping checks connectivity to the OVH API and authorization for the zone.
func (p *Provider) Ping(ctx context.Context) error {
	return p.client.Ping(ctx, p.zone)
}

// supportedFieldTypes lists the OVH field types this provider manages.
var supportedFieldTypes = []string{"A", "AAAA", "CNAME", "TXT", "SRV"}

// List returns all managed records in the zone.
func (p *Provider) List(ctx context.Context) ([]provider.Record, error) {
	var records []provider.Record

	for _, fieldType := range supportedFieldTypes {
		ids, err := p.client.ListRecordIDs(ctx, p.zone, fieldType, "")
		if err != nil {
			return nil, fmt.Errorf("listing %s record IDs: %w", fieldType, err)
		}

		for _, id := range ids {
			rec, err := p.client.GetRecord(ctx, p.zone, id)
			if err != nil {
				return nil, fmt.Errorf("getting %s record %d: %w", fieldType, id, err)
			}
			records = append(records, p.toProviderRecord(rec))
		}
	}

	p.logger.Debug("listed records",
		slog.String("provider", p.name),
		slog.String("zone", p.zone),
		slog.Int("count", len(records)),
	)

	return records, nil
}

// Create adds a new DNS record.
func (p *Provider) Create(ctx context.Context, record provider.Record) error {
	ttl := p.effectiveTTL(record.TTL)
	subDomain := p.toSubDomain(record.Hostname)

	target, err := p.toOVHTarget(record)
	if err != nil {
		return err
	}

	if _, err := p.client.CreateRecord(ctx, p.zone, string(record.Type), subDomain, target, ttl); err != nil {
		return fmt.Errorf("creating %s record: %w", record.Type, err)
	}

	if err := p.client.RefreshZone(ctx, p.zone); err != nil {
		return fmt.Errorf("refreshing zone after create: %w", err)
	}

	p.logger.Info("created record",
		slog.String("provider", p.name),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", target),
		slog.Int("ttl", ttl),
	)

	return nil
}

// Delete removes a DNS record.
func (p *Provider) Delete(ctx context.Context, record provider.Record) error {
	rec, err := p.findRecord(ctx, record)
	if err != nil {
		return err
	}
	if rec == nil {
		p.logger.Warn("record not found for deletion",
			slog.String("hostname", record.Hostname),
			slog.String("type", string(record.Type)),
		)
		return nil // Record doesn't exist, nothing to delete.
	}

	if err := p.client.DeleteRecord(ctx, p.zone, rec.ID); err != nil {
		return fmt.Errorf("deleting %s record: %w", record.Type, err)
	}

	if err := p.client.RefreshZone(ctx, p.zone); err != nil {
		return fmt.Errorf("refreshing zone after delete: %w", err)
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
	rec, err := p.findRecord(ctx, existing)
	if err != nil {
		return err
	}
	if rec == nil {
		return provider.ErrNotFound
	}

	ttl := p.effectiveTTL(desired.TTL)
	subDomain := p.toSubDomain(desired.Hostname)

	target, err := p.toOVHTarget(desired)
	if err != nil {
		return err
	}

	if err := p.client.UpdateRecord(ctx, p.zone, rec.ID, subDomain, target, ttl); err != nil {
		return fmt.Errorf("updating %s record: %w", desired.Type, err)
	}

	if err := p.client.RefreshZone(ctx, p.zone); err != nil {
		return fmt.Errorf("refreshing zone after update: %w", err)
	}

	p.logger.Info("updated record",
		slog.String("provider", p.name),
		slog.String("hostname", desired.Hostname),
		slog.String("type", string(desired.Type)),
		slog.String("old_target", existing.Target),
		slog.String("new_target", target),
		slog.Int("ttl", ttl),
	)

	return nil
}

// findRecord locates the OVH record matching the given record's hostname, type,
// and (when set) target. Returns nil if no match is found.
func (p *Provider) findRecord(ctx context.Context, record provider.Record) (*ovhRecord, error) {
	subDomain := p.toSubDomain(record.Hostname)
	ids, err := p.client.ListRecordIDs(ctx, p.zone, string(record.Type), subDomain)
	if err != nil {
		return nil, fmt.Errorf("finding record: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	wantTarget, err := p.toOVHTarget(record)
	if err != nil {
		return nil, fmt.Errorf("rendering record target: %w", err)
	}

	for _, id := range ids {
		rec, err := p.client.GetRecord(ctx, p.zone, id)
		if err != nil {
			return nil, fmt.Errorf("getting record %d: %w", id, err)
		}
		if ovhTargetsEqual(record.Type, rec.Target, wantTarget) {
			return rec, nil
		}
	}

	// A name and type may contain several record values. Falling back to the
	// first one would mutate an unrelated sibling when the requested value is
	// absent.
	return nil, nil
}

func ovhTargetsEqual(recordType provider.RecordType, actual, desired string) bool {
	switch recordType {
	case provider.RecordTypeTXT:
		return unquoteTXT(actual) == desired
	case provider.RecordTypeCNAME:
		return strings.EqualFold(strings.TrimSuffix(actual, "."), strings.TrimSuffix(desired, "."))
	case provider.RecordTypeSRV:
		actualSRV, actualTarget, actualOK := parseSRVTarget(actual)
		desiredSRV, desiredTarget, desiredOK := parseSRVTarget(desired)
		return actualOK && desiredOK &&
			actualSRV.Priority == desiredSRV.Priority &&
			actualSRV.Weight == desiredSRV.Weight &&
			actualSRV.Port == desiredSRV.Port &&
			strings.EqualFold(strings.TrimSuffix(actualTarget, "."), strings.TrimSuffix(desiredTarget, "."))
	default:
		return actual == desired
	}
}

// toProviderRecord converts an OVH API record into a provider.Record.
func (p *Provider) toProviderRecord(rec *ovhRecord) provider.Record {
	out := provider.Record{
		Hostname:   p.toFQDN(rec.SubDomain),
		Type:       provider.RecordType(rec.FieldType),
		TTL:        rec.TTL,
		ProviderID: strconv.FormatInt(rec.ID, 10),
	}

	switch out.Type {
	case provider.RecordTypeSRV:
		if srv, target, ok := parseSRVTarget(rec.Target); ok {
			out.SRV = srv
			out.Target = target
		} else {
			out.Target = rec.Target
		}
	case provider.RecordTypeTXT:
		out.Target = unquoteTXT(rec.Target)
	default:
		out.Target = rec.Target
	}

	return out
}

// toOVHTarget renders the OVH target string for a record.
// SRV records are encoded as "priority weight port target".
func (p *Provider) toOVHTarget(record provider.Record) (string, error) {
	if record.Type == provider.RecordTypeSRV {
		if record.SRV == nil {
			return "", fmt.Errorf("creating SRV record: SRV data is required")
		}
		return fmt.Sprintf("%d %d %d %s",
			record.SRV.Priority, record.SRV.Weight, record.SRV.Port, record.Target), nil
	}
	return record.Target, nil
}

// effectiveTTL returns the TTL to use, falling back to the provider default
// when the record does not specify one.
func (p *Provider) effectiveTTL(recordTTL int) int {
	if recordTTL > 0 {
		return recordTTL
	}
	return p.ttl
}

// toSubDomain converts an FQDN into an OVH subdomain relative to the zone.
// Example (zone "example.com"): "app.example.com" -> "app", "example.com" -> "".
func (p *Provider) toSubDomain(hostname string) string {
	host := strings.TrimSuffix(hostname, ".")
	zone := strings.TrimSuffix(p.zone, ".")

	if host == zone {
		return ""
	}
	if suffix := "." + zone; strings.HasSuffix(host, suffix) {
		return strings.TrimSuffix(host, suffix)
	}
	// Already relative (or outside the zone); return as-is.
	return host
}

// toFQDN converts an OVH subdomain into an FQDN within the zone.
// Example (zone "example.com"): "app" -> "app.example.com", "" -> "example.com".
func (p *Provider) toFQDN(subDomain string) string {
	zone := strings.TrimSuffix(p.zone, ".")
	if subDomain == "" {
		return zone
	}
	return subDomain + "." + zone
}

// parseSRVTarget parses an OVH SRV target string ("priority weight port target")
// into structured SRV data and the target hostname.
func parseSRVTarget(s string) (*provider.SRVData, string, bool) {
	fields := strings.Fields(s)
	if len(fields) != 4 {
		return nil, "", false
	}
	priority, err1 := strconv.ParseUint(fields[0], 10, 16)
	weight, err2 := strconv.ParseUint(fields[1], 10, 16)
	port, err3 := strconv.ParseUint(fields[2], 10, 16)
	if err1 != nil || err2 != nil || err3 != nil {
		return nil, "", false
	}
	return &provider.SRVData{
		Priority: uint16(priority),
		Weight:   uint16(weight),
		Port:     uint16(port),
	}, fields[3], true
}

// unquoteTXT strips a single layer of surrounding double quotes that OVH may
// add to TXT record values, so ownership records round-trip correctly.
func unquoteTXT(s string) string {
	if len(s) >= 2 && strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"") {
		return s[1 : len(s)-1]
	}
	return s
}

// Ensure Provider implements the expected interfaces at compile time.
var (
	_ provider.Provider = (*Provider)(nil)
	_ provider.Updater  = (*Provider)(nil)
)
