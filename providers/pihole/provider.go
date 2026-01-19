// Package pihole implements the DNSWeaver provider interface for Pi-hole DNS.
package pihole

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
	"gitlab.bluewillows.net/root/dnsweaver/providers/dnsmasq"
)

// Provider implements provider.Provider for Pi-hole DNS.
// It supports two modes:
// - API mode: Uses Pi-hole's Admin API (supports both v5 and v6)
// - File mode: Uses dnsmasq-style config files (for containerized Pi-hole)
type Provider struct {
	name       string
	zone       string
	ttl        int
	mode       Mode
	apiVersion APIVersion   // Detected or configured API version
	httpClient *http.Client // Custom HTTP client (optional, API mode only)
	logger     *slog.Logger

	// API mode client (implements DNSClient interface)
	dnsClient DNSClient

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

// WithProviderHTTPClient sets a custom HTTP client for the provider.
// This allows the factory to pass in a pre-configured HTTP client with
// timeout, TLS settings, and user-agent already applied.
// Only used in API mode; file mode does not use HTTP.
func WithProviderHTTPClient(client *http.Client) ProviderOption {
	return func(p *Provider) {
		if client != nil {
			p.httpClient = client
		}
	}
}

// WithAPIClient sets a custom API client (for testing).
// The client must implement the DNSClient interface.
func WithAPIClient(client DNSClient) ProviderOption {
	return func(p *Provider) {
		p.dnsClient = client
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
		if p.dnsClient == nil {
			// Determine API version (detect or use configured)
			apiVersion, err := p.resolveAPIVersion(config)
			if err != nil {
				return nil, fmt.Errorf("determining Pi-hole API version: %w", err)
			}
			p.apiVersion = apiVersion

			// Create the appropriate client based on version
			switch apiVersion {
			case APIVersionV5:
				apiOpts := []APIClientOption{WithAPILogger(p.logger)}
				if p.httpClient != nil {
					apiOpts = append(apiOpts, WithHTTPClient(p.httpClient))
				}
				p.dnsClient = NewAPIClient(
					config.URL,
					config.Password,
					config.Zone,
					apiOpts...,
				)
			case APIVersionV6:
				v6Opts := []V6APIClientOption{WithV6Logger(p.logger)}
				if p.httpClient != nil {
					v6Opts = append(v6Opts, WithV6HTTPClient(p.httpClient))
				}
				p.dnsClient = NewV6APIClient(
					config.URL,
					config.Password,
					config.Zone,
					v6Opts...,
				)
			default:
				return nil, fmt.Errorf("unsupported API version: %s", apiVersion)
			}
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
		// For API mode, try to list records to verify connectivity
		_, err := p.dnsClient.List(ctx)
		return err
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
	piholeRecords, err := p.dnsClient.List(ctx)
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

	if err := p.dnsClient.Create(ctx, rec); err != nil {
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

	if err := p.dnsClient.Delete(ctx, rec); err != nil {
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

// resolveAPIVersion determines which Pi-hole API version to use.
// If API_VERSION is set to "v5" or "v6", that version is used.
// Otherwise, the version is auto-detected by probing the Pi-hole instance.
func (p *Provider) resolveAPIVersion(config *Config) (APIVersion, error) {
	// Check for explicit version configuration
	if config.APIVersion != "" && config.APIVersion != "auto" {
		switch strings.ToLower(config.APIVersion) {
		case "v5":
			p.logger.Info("using configured Pi-hole API version",
				slog.String("version", "v5"))
			return APIVersionV5, nil
		case "v6":
			p.logger.Info("using configured Pi-hole API version",
				slog.String("version", "v6"))
			return APIVersionV6, nil
		}
	}

	// Auto-detect version by probing the Pi-hole instance
	detector := NewVersionDetector(config.URL, p.httpClient, p.logger)
	version, versionStr, err := detector.Detect(context.Background())
	if err != nil {
		return APIVersionUnknown, err
	}

	p.logger.Info("auto-detected Pi-hole API version",
		slog.String("version", version.String()),
		slog.String("pihole_version", versionStr))

	return version, nil
}

// APIVersion returns the detected or configured API version.
// Returns APIVersionUnknown if the provider is in file mode.
func (p *Provider) APIVersion() APIVersion {
	return p.apiVersion
}

// Ensure Provider implements provider.Provider at compile time.
var _ provider.Provider = (*Provider)(nil)
