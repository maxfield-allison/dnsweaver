// Package cloudflare implements the DNSWeaver provider interface for Cloudflare DNS.
package cloudflare

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// Provider implements provider.Provider for Cloudflare DNS.
type Provider struct {
	name    string
	zone    string // Zone name (for display/logging)
	zoneID  string // Resolved zone ID
	ttl     int
	proxied bool
	client  *Client
	logger  *slog.Logger

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

	// Create the API client with the same logger
	p.client = NewClient(config.Token, WithLogger(p.logger))

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
		Proxied: false,
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
// Returns A and CNAME records.
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
		})
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
	// Cloudflare uses TTL=1 for "automatic" (when proxied)
	if p.proxied && ttl < 60 {
		ttl = 1
	}

	recordType := string(record.Type)
	err = p.client.CreateRecord(ctx, zoneID, recordType, record.Hostname, record.Target, ttl, p.proxied)
	if err != nil {
		return fmt.Errorf("creating %s record: %w", recordType, err)
	}

	p.logger.Info("created record",
		slog.String("provider", p.name),
		slog.String("hostname", record.Hostname),
		slog.String("type", recordType),
		slog.String("target", record.Target),
		slog.Int("ttl", ttl),
		slog.Bool("proxied", p.proxied),
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

// Factory returns a provider.Factory function for use with the provider registry.
func Factory() provider.Factory {
	return func(name string, config map[string]string) (provider.Provider, error) {
		return NewFromMap(name, config)
	}
}

// Ensure Provider implements provider.Provider at compile time.
var _ provider.Provider = (*Provider)(nil)
