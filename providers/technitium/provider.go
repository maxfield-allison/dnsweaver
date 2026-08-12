// Package technitium implements the DNSWeaver provider interface for Technitium DNS Server.
package technitium

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// Provider implements provider.Provider for Technitium DNS Server.
type Provider struct {
	name             string
	url              string // Technitium API URL (recorded for Identity reporting)
	zone             string
	ttl              int
	autoHTTPSRecords bool   // Create companion HTTPS records for A/AAAA records
	autoHTTPSALPN    string // ALPN value for companion HTTPS records (e.g., "h2")
	client           *Client
	logger           *slog.Logger
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

// New creates a new Technitium provider instance.
func New(name string, config *Config, opts ...ProviderOption) (*Provider, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	p := &Provider{
		name:             name,
		url:              config.URL,
		zone:             config.Zone,
		ttl:              config.TTL,
		autoHTTPSRecords: config.AutoHTTPSRecords,
		autoHTTPSALPN:    config.AutoHTTPSALPN,
		logger:           slog.Default(),
	}

	for _, opt := range opts {
		opt(p)
	}

	if p.autoHTTPSRecords {
		p.logger.Info("companion HTTPS records enabled (disable with AUTO_HTTPS_RECORDS=false)",
			slog.String("provider", name),
			slog.String("alpn", p.autoHTTPSALPN),
		)
	} else {
		p.logger.Debug("companion HTTPS records disabled",
			slog.String("provider", name),
		)
	}

	// Build client options
	clientOpts := []ClientOption{WithLogger(p.logger)}

	// Create the API client with the same logger.
	// NOTE: TLS settings (custom CA, mTLS, skip-verify) are configured at
	// the framework level via FactoryConfig.HTTP.TLS and applied to the
	// shared HTTP client constructed in the factory. The legacy path
	// (calling New() directly with InsecureSkipVerify in Config) used to
	// override the client here; that bespoke knob was removed in v1.5 in
	// favor of the unified httputil.TLSConfig.
	p.client = NewClient(config.URL, config.Token, clientOpts...)

	return p, nil
}

// NewFromEnv creates a new Technitium provider from environment variables.
// This is a convenience function for use with the provider registry.
func NewFromEnv(instanceName string, opts ...ProviderOption) (*Provider, error) {
	config, err := LoadConfig(instanceName)
	if err != nil {
		return nil, err
	}

	return New(instanceName, config, opts...)
}

// Name returns the provider instance name.
func (p *Provider) Name() string {
	return p.name
}

// Type returns "technitium".
func (p *Provider) Type() string {
	return "technitium"
}

// Identity returns the backend identity for this provider instance.
// Two technitium instances are considered the same backend when they target
// the same API URL and zone (see provider.ProviderIdentity, issue #88).
func (p *Provider) Identity() provider.ProviderIdentity {
	return provider.ProviderIdentity{
		Type:     "technitium",
		Endpoint: p.url,
		Zone:     p.zone,
	}
}

// Capabilities returns the provider's feature support.
// Technitium supports all features: TXT ownership, native update, and all record types.
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
			provider.RecordTypeHTTPS,
		},
	}
}

// Zone returns the configured DNS zone.
func (p *Provider) Zone() string {
	return p.zone
}

// Ping checks connectivity to the Technitium server.
func (p *Provider) Ping(ctx context.Context) error {
	return p.client.Ping(ctx)
}

// List returns all managed records in the zone.
// Currently returns A, CNAME, and TXT records.
func (p *Provider) List(ctx context.Context) ([]provider.Record, error) {
	apiRecords, err := p.client.ListZoneRecords(ctx, p.zone)
	if err != nil {
		return nil, fmt.Errorf("listing records: %w", err)
	}

	var records []provider.Record
	for _, r := range apiRecords {
		// Only return A, AAAA, CNAME, TXT, and SRV records (the types we manage)
		switch r.Type {
		case "A":
			records = append(records, provider.Record{
				Hostname:   r.Name,
				Type:       provider.RecordTypeA,
				Target:     r.RData.IPAddress,
				TTL:        r.TTL,
				ProviderID: fmt.Sprintf("%s:%s:%s", r.Name, r.Type, r.RData.IPAddress),
			})
		case "AAAA":
			records = append(records, provider.Record{
				Hostname:   r.Name,
				Type:       provider.RecordTypeAAAA,
				Target:     r.RData.IPAddress,
				TTL:        r.TTL,
				ProviderID: fmt.Sprintf("%s:%s:%s", r.Name, r.Type, r.RData.IPAddress),
			})
		case "CNAME":
			records = append(records, provider.Record{
				Hostname:   r.Name,
				Type:       provider.RecordTypeCNAME,
				Target:     r.RData.CName,
				TTL:        r.TTL,
				ProviderID: fmt.Sprintf("%s:%s:%s", r.Name, r.Type, r.RData.CName),
			})
		case "TXT":
			records = append(records, provider.Record{
				Hostname:   r.Name,
				Type:       provider.RecordTypeTXT,
				Target:     r.RData.Text,
				TTL:        r.TTL,
				ProviderID: fmt.Sprintf("%s:%s:%s", r.Name, r.Type, r.RData.Text),
			})
		case "SRV":
			records = append(records, provider.Record{
				Hostname:   r.Name,
				Type:       provider.RecordTypeSRV,
				Target:     r.RData.SrvTarget,
				TTL:        r.TTL,
				ProviderID: fmt.Sprintf("%s:%s:%d:%d:%d:%s", r.Name, r.Type, r.RData.Priority, r.RData.Weight, r.RData.Port, r.RData.SrvTarget),
				SRV: &provider.SRVData{
					Priority: uint16(min(max(0, r.RData.Priority), math.MaxUint16)),
					Weight:   uint16(min(max(0, r.RData.Weight), math.MaxUint16)),
					Port:     uint16(min(max(0, r.RData.Port), math.MaxUint16)),
				},
			})
		case "HTTPS":
			records = append(records, provider.Record{
				Hostname:   r.Name,
				Type:       provider.RecordTypeHTTPS,
				Target:     r.RData.SvcTargetName,
				TTL:        r.TTL,
				ProviderID: fmt.Sprintf("%s:%s:%d:%s:%s", r.Name, r.Type, r.RData.SvcPriority, r.RData.SvcTargetName, r.RData.SvcParams),
				HTTPS: &provider.HTTPSData{
					Priority:   uint16(min(max(0, r.RData.SvcPriority), math.MaxUint16)),
					TargetName: r.RData.SvcTargetName,
					ALPN:       extractALPN(string(r.RData.SvcParams)),
				},
			})
		}
		// Skip other record types (NS, SOA, etc.)
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
	ttl := record.TTL
	if ttl <= 0 {
		ttl = p.ttl
	}

	switch record.Type {
	case provider.RecordTypeA:
		if err := p.client.AddARecord(ctx, p.zone, record.Hostname, record.Target, ttl); err != nil {
			return fmt.Errorf("creating A record: %w", err)
		}
	case provider.RecordTypeAAAA:
		if err := p.client.AddAAAARecord(ctx, p.zone, record.Hostname, record.Target, ttl); err != nil {
			return fmt.Errorf("creating AAAA record: %w", err)
		}
	case provider.RecordTypeCNAME:
		if err := p.client.AddCNAMERecord(ctx, p.zone, record.Hostname, record.Target, ttl); err != nil {
			return fmt.Errorf("creating CNAME record: %w", err)
		}
	case provider.RecordTypeTXT:
		if err := p.client.AddTXTRecord(ctx, p.zone, record.Hostname, record.Target, ttl); err != nil {
			return fmt.Errorf("creating TXT record: %w", err)
		}
	case provider.RecordTypeSRV:
		if record.SRV == nil {
			return fmt.Errorf("creating SRV record: SRV data is required")
		}
		if err := p.client.AddSRVRecord(ctx, p.zone, record.Hostname, int(record.SRV.Priority), int(record.SRV.Weight), int(record.SRV.Port), record.Target, ttl); err != nil {
			return fmt.Errorf("creating SRV record: %w", err)
		}
	case provider.RecordTypeHTTPS:
		if record.HTTPS == nil {
			return fmt.Errorf("creating HTTPS record: HTTPS data is required")
		}
		svcParams := buildSvcParams(record.HTTPS.ALPN)
		if err := p.client.AddHTTPSRecord(ctx, p.zone, record.Hostname, int(record.HTTPS.Priority), record.HTTPS.TargetName, svcParams, ttl); err != nil {
			return fmt.Errorf("creating HTTPS record: %w", err)
		}
	default:
		return fmt.Errorf("unsupported record type: %s", record.Type)
	}

	p.logger.Info("created record",
		slog.String("provider", p.name),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", record.Target),
		slog.Int("ttl", ttl),
	)

	// Auto-create companion HTTPS record for A/AAAA records when enabled.
	// This prevents ECH fallback errors in split-horizon DNS environments.
	if err := p.createCompanionHTTPS(ctx, record.Hostname, record.Type, ttl); err != nil {
		p.logger.Warn("companion HTTPS record creation failed (non-fatal)",
			slog.String("hostname", record.Hostname),
			slog.String("error", err.Error()),
		)
	}

	return nil
}

// Delete removes a DNS record.
func (p *Provider) Delete(ctx context.Context, record provider.Record) error {
	switch record.Type {
	case provider.RecordTypeA:
		if err := p.client.DeleteARecord(ctx, p.zone, record.Hostname, record.Target); err != nil {
			return fmt.Errorf("deleting A record: %w", err)
		}
	case provider.RecordTypeAAAA:
		if err := p.client.DeleteAAAARecord(ctx, p.zone, record.Hostname, record.Target); err != nil {
			return fmt.Errorf("deleting AAAA record: %w", err)
		}
	case provider.RecordTypeCNAME:
		if err := p.client.DeleteCNAMERecord(ctx, p.zone, record.Hostname, record.Target); err != nil {
			return fmt.Errorf("deleting CNAME record: %w", err)
		}
	case provider.RecordTypeTXT:
		if err := p.client.DeleteTXTRecord(ctx, p.zone, record.Hostname, record.Target); err != nil {
			return fmt.Errorf("deleting TXT record: %w", err)
		}
	case provider.RecordTypeSRV:
		if record.SRV == nil {
			return fmt.Errorf("deleting SRV record: SRV data is required")
		}
		if err := p.client.DeleteSRVRecord(ctx, p.zone, record.Hostname, int(record.SRV.Priority), int(record.SRV.Weight), int(record.SRV.Port), record.Target); err != nil {
			return fmt.Errorf("deleting SRV record: %w", err)
		}
	case provider.RecordTypeHTTPS:
		if record.HTTPS == nil {
			return fmt.Errorf("deleting HTTPS record: HTTPS data is required")
		}
		svcParams := buildSvcParams(record.HTTPS.ALPN)
		if err := p.client.DeleteHTTPSRecord(ctx, p.zone, record.Hostname, int(record.HTTPS.Priority), record.HTTPS.TargetName, svcParams); err != nil {
			return fmt.Errorf("deleting HTTPS record: %w", err)
		}
	default:
		return fmt.Errorf("unsupported record type: %s", record.Type)
	}

	p.logger.Info("deleted record",
		slog.String("provider", p.name),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", record.Target),
	)

	// Auto-delete companion HTTPS record when an A/AAAA record is removed.
	// Uses best-effort: companion may already be gone or manually deleted.
	if err := p.deleteCompanionHTTPS(ctx, record.Hostname, record.Type); err != nil {
		p.logger.Warn("companion HTTPS record deletion failed (non-fatal)",
			slog.String("hostname", record.Hostname),
			slog.String("error", err.Error()),
		)
	}

	return nil
}

// Update modifies an existing DNS record in place.
// This implements the provider.Updater interface for native update support.
func (p *Provider) Update(ctx context.Context, existing, desired provider.Record) error {
	ttl := desired.TTL
	if ttl <= 0 {
		ttl = p.ttl
	}

	// Technitium's update API requires identifying the old record and specifying new values
	switch desired.Type {
	case provider.RecordTypeA:
		if err := p.client.UpdateARecord(ctx, p.zone, existing.Hostname, existing.Target, desired.Target, ttl); err != nil {
			return fmt.Errorf("updating A record: %w", err)
		}
	case provider.RecordTypeAAAA:
		if err := p.client.UpdateAAAARecord(ctx, p.zone, existing.Hostname, existing.Target, desired.Target, ttl); err != nil {
			return fmt.Errorf("updating AAAA record: %w", err)
		}
	case provider.RecordTypeCNAME:
		if err := p.client.UpdateCNAMERecord(ctx, p.zone, existing.Hostname, existing.Target, desired.Target, ttl); err != nil {
			return fmt.Errorf("updating CNAME record: %w", err)
		}
	case provider.RecordTypeSRV:
		// SRV records need special handling - for now, fall back to delete+create
		// Technitium doesn't have a straightforward SRV update API
		if existing.SRV == nil || desired.SRV == nil {
			return fmt.Errorf("updating SRV record: SRV data is required")
		}
		// Delete old record
		if err := p.client.DeleteSRVRecord(ctx, p.zone, existing.Hostname, int(existing.SRV.Priority), int(existing.SRV.Weight), int(existing.SRV.Port), existing.Target); err != nil {
			return fmt.Errorf("deleting old SRV record for update: %w", err)
		}
		// Create new record
		if err := p.client.AddSRVRecord(ctx, p.zone, desired.Hostname, int(desired.SRV.Priority), int(desired.SRV.Weight), int(desired.SRV.Port), desired.Target, ttl); err != nil {
			return fmt.Errorf("creating new SRV record for update: %w", err)
		}
	case provider.RecordTypeHTTPS:
		// HTTPS records: delete+create (Technitium has no native HTTPS update API)
		if existing.HTTPS == nil || desired.HTTPS == nil {
			return fmt.Errorf("updating HTTPS record: HTTPS data is required")
		}
		oldParams := buildSvcParams(existing.HTTPS.ALPN)
		if err := p.client.DeleteHTTPSRecord(ctx, p.zone, existing.Hostname, int(existing.HTTPS.Priority), existing.HTTPS.TargetName, oldParams); err != nil {
			return fmt.Errorf("deleting old HTTPS record for update: %w", err)
		}
		newParams := buildSvcParams(desired.HTTPS.ALPN)
		if err := p.client.AddHTTPSRecord(ctx, p.zone, desired.Hostname, int(desired.HTTPS.Priority), desired.HTTPS.TargetName, newParams, ttl); err != nil {
			return fmt.Errorf("creating new HTTPS record for update: %w", err)
		}
	case provider.RecordTypeTXT:
		// TXT records (ownership markers) don't typically need updates
		// If value changes, delete and recreate
		if err := p.client.DeleteTXTRecord(ctx, p.zone, existing.Hostname, existing.Target); err != nil {
			return fmt.Errorf("deleting old TXT record for update: %w", err)
		}
		if err := p.client.AddTXTRecord(ctx, p.zone, desired.Hostname, desired.Target, ttl); err != nil {
			return fmt.Errorf("creating new TXT record for update: %w", err)
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
	)

	return nil
}

// extractALPN extracts the ALPN value from Technitium's svcParams format.
// Input: "alpn|h2" or "alpn|h2,h3" → Output: "h2" or "h2,h3"
// Returns empty string if no ALPN parameter found.
func extractALPN(svcParams string) string {
	for _, param := range strings.Split(svcParams, " ") {
		parts := strings.SplitN(param, "|", 2)
		if len(parts) == 2 && parts[0] == "alpn" {
			return parts[1]
		}
	}
	return ""
}

// buildSvcParams constructs Technitium's svcParams format from an ALPN value.
// Input: "h2" → Output: "alpn|h2"
// Returns empty string if alpn is empty.
func buildSvcParams(alpn string) string {
	if alpn == "" {
		return ""
	}
	return "alpn|" + alpn
}

// Ensure Provider implements provider.Provider at compile time.
var _ provider.Provider = (*Provider)(nil)

// Ensure Provider implements provider.Updater at compile time.
var _ provider.Updater = (*Provider)(nil)
