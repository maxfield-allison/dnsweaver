// Package technitium implements the DNSWeaver provider interface for Technitium DNS Server.
package technitium

import (
	"context"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// Provider implements provider.Provider for Technitium DNS Server.
type Provider struct {
	name  string
	url   string
	token string
	zone  string
}

// Config holds Technitium-specific configuration.
type Config struct {
	URL   string // Technitium API URL
	Token string // API token
	Zone  string // DNS zone to manage
	TTL   int    // Record TTL (optional, defaults to 300)
}

// New creates a new Technitium provider instance.
func New(name string, config Config) (*Provider, error) {
	return &Provider{
		name:  name,
		url:   config.URL,
		token: config.Token,
		zone:  config.Zone,
	}, nil
}

// Name returns the provider instance name.
func (p *Provider) Name() string {
	return p.name
}

// Type returns "technitium".
func (p *Provider) Type() string {
	return "technitium"
}

// Ping checks connectivity to the Technitium server.
func (p *Provider) Ping(ctx context.Context) error {
	// TODO: Implement in Issue #10 - Technitium provider
	return nil
}

// List returns all managed records in the zone.
func (p *Provider) List(ctx context.Context) ([]provider.Record, error) {
	// TODO: Implement in Issue #10 - Technitium provider
	return nil, nil
}

// Create adds a new DNS record.
func (p *Provider) Create(ctx context.Context, record provider.Record) error {
	// TODO: Implement in Issue #10 - Technitium provider
	return nil
}

// Delete removes a DNS record.
func (p *Provider) Delete(ctx context.Context, record provider.Record) error {
	// TODO: Implement in Issue #10 - Technitium provider
	return nil
}

// Ensure Provider implements provider.Provider
var _ provider.Provider = (*Provider)(nil)
