// Package dnsmasq implements the DNSWeaver provider interface for dnsmasq DNS server.
package dnsmasq

import (
	"context"
	"fmt"
	"log/slog"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// Provider implements provider.Provider for dnsmasq DNS server.
type Provider struct {
	name          string
	zone          string
	ttl           int
	reloadOnWrite bool
	client        *Client
	logger        *slog.Logger
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

// WithReloadOnWrite enables automatic dnsmasq reload after writes.
// Default is true.
func WithReloadOnWrite(reload bool) ProviderOption {
	return func(p *Provider) {
		p.reloadOnWrite = reload
	}
}

// WithClient sets a custom client (for testing).
func WithClient(client *Client) ProviderOption {
	return func(p *Provider) {
		p.client = client
	}
}

// New creates a new dnsmasq provider instance.
func New(name string, config *Config, opts ...ProviderOption) (*Provider, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	p := &Provider{
		name:          name,
		zone:          config.Zone,
		ttl:           config.TTL,
		reloadOnWrite: true, // Default: reload after writes
		logger:        slog.Default(),
	}

	for _, opt := range opts {
		opt(p)
	}

	// Create client if not provided via options (testing)
	if p.client == nil {
		p.client = NewClient(
			config.ConfigDir,
			config.ConfigFile,
			config.ReloadCommand,
			config.Zone,
			WithLogger(p.logger),
		)
	}

	return p, nil
}

// NewFromEnv creates a new dnsmasq provider from environment variables.
// This is a convenience function for use with the provider registry.
func NewFromEnv(instanceName string, opts ...ProviderOption) (*Provider, error) {
	config, err := LoadConfig(instanceName)
	if err != nil {
		return nil, err
	}

	return New(instanceName, config, opts...)
}

// NewFromMap creates a new dnsmasq provider from a configuration map.
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

// Type returns "dnsmasq".
func (p *Provider) Type() string {
	return "dnsmasq"
}

// Zone returns the configured DNS zone.
func (p *Provider) Zone() string {
	return p.zone
}

// Ping checks connectivity to the dnsmasq configuration.
func (p *Provider) Ping(ctx context.Context) error {
	return p.client.Ping(ctx)
}

// List returns all managed records from the dnsmasq config file.
func (p *Provider) List(ctx context.Context) ([]provider.Record, error) {
	dnsmasqRecords, err := p.client.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing records: %w", err)
	}

	var records []provider.Record
	for _, r := range dnsmasqRecords {
		records = append(records, provider.Record{
			Hostname:   r.Hostname,
			Type:       r.Type,
			Target:     r.Target,
			TTL:        p.ttl, // dnsmasq doesn't use TTL, but we track it for consistency
			ProviderID: fmt.Sprintf("%s:%s:%s", r.Hostname, r.Type, r.Target),
		})
	}

	p.logger.Debug("listed records",
		slog.String("provider", p.name),
		slog.Int("count", len(records)),
	)

	return records, nil
}

// Create adds a new DNS record to the dnsmasq config.
func (p *Provider) Create(ctx context.Context, record provider.Record) error {
	// Validate record type
	switch record.Type {
	case provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME:
		// Supported
	case provider.RecordTypeTXT:
		// dnsmasq supports txt-record= directive, but it's rarely needed
		// For now, skip TXT records (ownership tracking uses different mechanism)
		p.logger.Debug("skipping TXT record (not supported by dnsmasq provider)",
			slog.String("hostname", record.Hostname))
		return nil
	case provider.RecordTypeSRV:
		// dnsmasq supports srv-host= directive
		// TODO: implement SRV support in a future version
		return fmt.Errorf("SRV records not yet supported by dnsmasq provider")
	default:
		return fmt.Errorf("unsupported record type: %s", record.Type)
	}

	dnsmasqRecord := dnsmasqRecord{
		Hostname: record.Hostname,
		Type:     record.Type,
		Target:   record.Target,
	}

	if err := p.client.Create(ctx, dnsmasqRecord); err != nil {
		return fmt.Errorf("creating %s record: %w", record.Type, err)
	}

	// Reload dnsmasq if configured
	if p.reloadOnWrite {
		if err := p.client.Reload(ctx); err != nil {
			p.logger.Warn("failed to reload dnsmasq",
				slog.String("error", err.Error()))
			// Don't fail the create, just warn
		}
	}

	p.logger.Info("created record",
		slog.String("provider", p.name),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", record.Target),
	)

	return nil
}

// Delete removes a DNS record from the dnsmasq config.
func (p *Provider) Delete(ctx context.Context, record provider.Record) error {
	// Skip TXT records (not supported)
	if record.Type == provider.RecordTypeTXT {
		p.logger.Debug("skipping TXT record deletion (not supported by dnsmasq provider)",
			slog.String("hostname", record.Hostname))
		return nil
	}

	dnsmasqRecord := dnsmasqRecord{
		Hostname: record.Hostname,
		Type:     record.Type,
		Target:   record.Target,
	}

	if err := p.client.Delete(ctx, dnsmasqRecord); err != nil {
		return fmt.Errorf("deleting %s record: %w", record.Type, err)
	}

	// Reload dnsmasq if configured
	if p.reloadOnWrite {
		if err := p.client.Reload(ctx); err != nil {
			p.logger.Warn("failed to reload dnsmasq",
				slog.String("error", err.Error()))
			// Don't fail the delete, just warn
		}
	}

	p.logger.Info("deleted record",
		slog.String("provider", p.name),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
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
