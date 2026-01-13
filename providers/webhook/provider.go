// Package webhook implements the DNSWeaver provider interface for webhook-based DNS integrations.
package webhook

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// Provider implements provider.Provider for webhook-based DNS.
type Provider struct {
	name   string
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

// New creates a new webhook provider instance.
func New(name string, config *Config, opts ...ProviderOption) (*Provider, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	p := &Provider{
		name:   name,
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(p)
	}

	// Create the HTTP client with the same logger
	p.client = NewClient(
		config.URL,
		config.Timeout,
		config.AuthHeader,
		config.AuthToken,
		WithLogger(p.logger),
		WithRetries(config.Retries),
		WithRetryDelay(config.RetryDelay),
	)

	return p, nil
}

// NewFromEnv creates a new webhook provider from environment variables.
// This is a convenience function for use with the provider registry.
func NewFromEnv(instanceName string, opts ...ProviderOption) (*Provider, error) {
	config, err := LoadConfig(instanceName)
	if err != nil {
		return nil, err
	}

	return New(instanceName, config, opts...)
}

// NewFromMap creates a new webhook provider from a configuration map.
// This is used by the provider registry Factory pattern.
func NewFromMap(name string, config map[string]string) (*Provider, error) {
	cfg := &Config{
		URL:        config["URL"],
		Timeout:    DefaultTimeout,
		AuthHeader: config["AUTH_HEADER"],
		AuthToken:  config["AUTH_TOKEN"],
		Retries:    DefaultRetries,
		RetryDelay: DefaultRetryDelay,
	}

	// Parse TIMEOUT if provided
	if timeoutStr, ok := config["TIMEOUT"]; ok && timeoutStr != "" {
		if timeout, err := parseDuration(timeoutStr); err == nil {
			cfg.Timeout = timeout
		}
	}

	// Parse RETRIES if provided
	if retriesStr, ok := config["RETRIES"]; ok && retriesStr != "" {
		var retries int
		if _, err := fmt.Sscanf(retriesStr, "%d", &retries); err == nil {
			cfg.Retries = retries
		}
	}

	// Parse RETRY_DELAY if provided
	if delayStr, ok := config["RETRY_DELAY"]; ok && delayStr != "" {
		if delay, err := parseDuration(delayStr); err == nil {
			cfg.RetryDelay = delay
		}
	}

	return New(name, cfg)
}

// parseDuration parses a duration string, returning the duration or an error.
func parseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

// Name returns the provider instance name.
func (p *Provider) Name() string {
	return p.name
}

// Type returns "webhook".
func (p *Provider) Type() string {
	return "webhook"
}

// Capabilities returns the provider's feature support.
// Webhook providers are assumed to have full capabilities since
// the actual DNS backend is abstracted. The remote webhook endpoint
// is responsible for handling all record types and operations.
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

// Ping checks connectivity to the webhook endpoint.
func (p *Provider) Ping(ctx context.Context) error {
	return p.client.Ping(ctx)
}

// List returns all managed records from the webhook.
func (p *Provider) List(ctx context.Context) ([]provider.Record, error) {
	webhookRecords, err := p.client.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing records: %w", err)
	}

	var records []provider.Record
	for _, r := range webhookRecords {
		var recordType provider.RecordType
		switch r.Type {
		case "A":
			recordType = provider.RecordTypeA
		case "AAAA":
			recordType = provider.RecordTypeAAAA
		case "CNAME":
			recordType = provider.RecordTypeCNAME
		case "TXT":
			recordType = provider.RecordTypeTXT
		case "SRV":
			recordType = provider.RecordTypeSRV
		default:
			// Skip unsupported record types
			continue
		}

		rec := provider.Record{
			Hostname:   r.Hostname,
			Type:       recordType,
			Target:     r.Value,
			TTL:        r.TTL,
			ProviderID: r.ID,
		}

		// Handle SRV-specific data
		if recordType == provider.RecordTypeSRV && r.SRV != nil {
			rec.SRV = &provider.SRVData{
				Priority: r.SRV.Priority,
				Weight:   r.SRV.Weight,
				Port:     r.SRV.Port,
			}
		}

		records = append(records, rec)
	}

	p.logger.Debug("listed records",
		slog.String("provider", p.name),
		slog.Int("count", len(records)),
	)

	return records, nil
}

// Create adds a new DNS record via the webhook.
func (p *Provider) Create(ctx context.Context, record provider.Record) error {
	var err error

	// SRV records require special handling
	if record.Type == provider.RecordTypeSRV {
		if record.SRV == nil {
			return fmt.Errorf("creating SRV record: SRV data is required")
		}
		err = p.client.CreateSRV(ctx, record.Hostname, record.SRV.Priority, record.SRV.Weight, record.SRV.Port, record.Target, record.TTL)
	} else {
		err = p.client.Create(ctx, record.Hostname, string(record.Type), record.Target, record.TTL)
	}

	if err != nil {
		return fmt.Errorf("creating %s record: %w", record.Type, err)
	}

	p.logger.Info("created record",
		slog.String("provider", p.name),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", record.Target),
		slog.Int("ttl", record.TTL),
	)

	return nil
}

// Delete removes a DNS record via the webhook.
func (p *Provider) Delete(ctx context.Context, record provider.Record) error {
	err := p.client.Delete(ctx, record.Hostname, string(record.Type))
	if err != nil {
		return fmt.Errorf("deleting %s record: %w", record.Type, err)
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
