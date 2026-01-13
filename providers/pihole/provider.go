// Package pihole implements the DNSWeaver provider interface for Pi-hole DNS.
package pihole

import (
	"context"
	"fmt"
	"log/slog"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
	"gitlab.bluewillows.net/root/dnsweaver/providers/dnsmasq"
)

// Provider implements provider.Provider for Pi-hole DNS.
// It supports two modes:
// - API mode: Uses Pi-hole's Admin API (recommended for Pi-hole v5+)
// - File mode: Uses dnsmasq-style config files (for containerized Pi-hole)
type Provider struct {
	name   string
	zone   string
	ttl    int
	mode   Mode
	logger *slog.Logger

	// API mode client
	apiClient *APIClient

	// File mode provider (wraps dnsmasq)
	fileProvider *dnsmasq.Provider
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

// WithAPIClient sets a custom API client (for testing).
func WithAPIClient(client *APIClient) ProviderOption {
	return func(p *Provider) {
		p.apiClient = client
	}
}

// WithFileProvider sets a custom file provider (for testing).
func WithFileProvider(fp *dnsmasq.Provider) ProviderOption {
	return func(p *Provider) {
		p.fileProvider = fp
	}
}

// New creates a new Pi-hole provider instance.
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
		mode:   config.Mode,
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(p)
	}

	// Initialize the appropriate client based on mode
	switch config.Mode {
	case ModeAPI:
		if p.apiClient == nil {
			p.apiClient = NewAPIClient(
				config.URL,
				config.Password,
				config.Zone,
				WithAPILogger(p.logger),
			)
		}
	case ModeFile:
		if p.fileProvider == nil {
			// Create a dnsmasq provider for file-based operations
			dnsmasqConfig := &dnsmasq.Config{
				ConfigDir:     config.ConfigDir,
				ConfigFile:    config.ConfigFile,
				ReloadCommand: config.ReloadCommand,
				Zone:          config.Zone,
				TTL:           config.TTL,
			}
			fp, err := dnsmasq.New(name, dnsmasqConfig, dnsmasq.WithProviderLogger(p.logger))
			if err != nil {
				return nil, fmt.Errorf("creating dnsmasq provider for file mode: %w", err)
			}
			p.fileProvider = fp
		}
	}

	return p, nil
}

// NewFromEnv creates a new Pi-hole provider from environment variables.
func NewFromEnv(instanceName string, opts ...ProviderOption) (*Provider, error) {
	config, err := LoadConfig(instanceName)
	if err != nil {
		return nil, err
	}

	return New(instanceName, config, opts...)
}

// NewFromMap creates a new Pi-hole provider from a configuration map.
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

// Type returns "pihole".
func (p *Provider) Type() string {
	return "pihole"
}

// Capabilities returns the provider's feature support.
// Pi-hole capabilities depend on the operating mode:
// - API mode: full TXT support and native update via the Pi-hole API
// - File mode: no TXT ownership (uses dnsmasq file format), no native update
func (p *Provider) Capabilities() provider.Capabilities {
	switch p.mode {
	case ModeAPI:
		return provider.Capabilities{
			SupportsOwnershipTXT: true,
			SupportsNativeUpdate: true,
			SupportedRecordTypes: []provider.RecordType{
				provider.RecordTypeA,
				provider.RecordTypeCNAME,
			},
		}
	case ModeFile:
		// File mode uses dnsmasq underneath - same limitations
		return provider.Capabilities{
			SupportsOwnershipTXT: false,
			SupportsNativeUpdate: false,
			SupportedRecordTypes: []provider.RecordType{
				provider.RecordTypeA,
				provider.RecordTypeCNAME,
			},
		}
	default:
		// Fallback to most restrictive
		return provider.Capabilities{
			SupportsOwnershipTXT: false,
			SupportsNativeUpdate: false,
			SupportedRecordTypes: []provider.RecordType{
				provider.RecordTypeA,
			},
		}
	}
}

// Zone returns the configured DNS zone.
func (p *Provider) Zone() string {
	return p.zone
}

// Mode returns the provider's operating mode.
func (p *Provider) Mode() Mode {
	return p.mode
}

// Ping checks connectivity to Pi-hole.
func (p *Provider) Ping(ctx context.Context) error {
	switch p.mode {
	case ModeAPI:
		return p.apiClient.Ping(ctx)
	case ModeFile:
		return p.fileProvider.Ping(ctx)
	default:
		return fmt.Errorf("unknown mode: %s", p.mode)
	}
}

// List returns all managed records from Pi-hole.
func (p *Provider) List(ctx context.Context) ([]provider.Record, error) {
	switch p.mode {
	case ModeAPI:
		return p.listAPI(ctx)
	case ModeFile:
		return p.fileProvider.List(ctx)
	default:
		return nil, fmt.Errorf("unknown mode: %s", p.mode)
	}
}

// listAPI retrieves records via the Pi-hole API.
func (p *Provider) listAPI(ctx context.Context) ([]provider.Record, error) {
	piholeRecords, err := p.apiClient.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing records: %w", err)
	}

	var records []provider.Record
	for _, r := range piholeRecords {
		records = append(records, provider.Record{
			Hostname:   r.Hostname,
			Type:       r.Type,
			Target:     r.Target,
			TTL:        p.ttl,
			ProviderID: fmt.Sprintf("%s:%s:%s", r.Hostname, r.Type, r.Target),
		})
	}

	p.logger.Debug("listed records",
		slog.String("provider", p.name),
		slog.String("mode", string(p.mode)),
		slog.Int("count", len(records)),
	)

	return records, nil
}

// Create adds a new DNS record.
func (p *Provider) Create(ctx context.Context, record provider.Record) error {
	// Validate record type
	switch record.Type {
	case provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME:
		// Supported
	case provider.RecordTypeTXT:
		// Pi-hole doesn't support TXT records; skip silently
		p.logger.Debug("skipping TXT record (not supported by Pi-hole provider)",
			slog.String("hostname", record.Hostname))
		return nil
	case provider.RecordTypeSRV:
		// Pi-hole doesn't support SRV records
		return fmt.Errorf("SRV records not supported by Pi-hole provider")
	default:
		return fmt.Errorf("unsupported record type: %s", record.Type)
	}

	switch p.mode {
	case ModeAPI:
		return p.createAPI(ctx, record)
	case ModeFile:
		return p.fileProvider.Create(ctx, record)
	default:
		return fmt.Errorf("unknown mode: %s", p.mode)
	}
}

// createAPI creates a record via the Pi-hole API.
func (p *Provider) createAPI(ctx context.Context, record provider.Record) error {
	rec := piholeRecord{
		Hostname: record.Hostname,
		Type:     record.Type,
		Target:   record.Target,
	}

	if err := p.apiClient.Create(ctx, rec); err != nil {
		return fmt.Errorf("creating %s record: %w", record.Type, err)
	}

	p.logger.Info("created record",
		slog.String("provider", p.name),
		slog.String("mode", string(p.mode)),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", record.Target),
	)

	return nil
}

// Delete removes a DNS record.
func (p *Provider) Delete(ctx context.Context, record provider.Record) error {
	// Skip TXT records (not supported)
	if record.Type == provider.RecordTypeTXT {
		p.logger.Debug("skipping TXT record deletion (not supported by Pi-hole provider)",
			slog.String("hostname", record.Hostname))
		return nil
	}

	switch p.mode {
	case ModeAPI:
		return p.deleteAPI(ctx, record)
	case ModeFile:
		return p.fileProvider.Delete(ctx, record)
	default:
		return fmt.Errorf("unknown mode: %s", p.mode)
	}
}

// deleteAPI deletes a record via the Pi-hole API.
func (p *Provider) deleteAPI(ctx context.Context, record provider.Record) error {
	rec := piholeRecord{
		Hostname: record.Hostname,
		Type:     record.Type,
		Target:   record.Target,
	}

	if err := p.apiClient.Delete(ctx, rec); err != nil {
		return fmt.Errorf("deleting %s record: %w", record.Type, err)
	}

	p.logger.Info("deleted record",
		slog.String("provider", p.name),
		slog.String("mode", string(p.mode)),
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
