// Package technitium implements the DNSWeaver provider interface for Technitium DNS Server.
package technitium

import (
	"context"
	"fmt"
	"log/slog"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// Provider implements provider.Provider for Technitium DNS Server.
type Provider struct {
	name   string
	zone   string
	ttl    int
	client *Client
	logger *slog.Logger
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
		name:   name,
		zone:   config.Zone,
		ttl:    config.TTL,
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(p)
	}

	// Create the API client with the same logger
	p.client = NewClient(config.URL, config.Token, WithLogger(p.logger))

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
					Priority: uint16(r.RData.Priority),
					Weight:   uint16(r.RData.Weight),
					Port:     uint16(r.RData.Port),
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
	default:
		return fmt.Errorf("unsupported record type: %s", record.Type)
	}

	p.logger.Info("deleted record",
		slog.String("provider", p.name),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", record.Target),
	)

	return nil
}

// Ensure Provider implements provider.Provider at compile time.
var _ provider.Provider = (*Provider)(nil)
