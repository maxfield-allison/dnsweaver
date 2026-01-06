// Package traefik provides a Source implementation for extracting hostnames
// from Traefik reverse proxy labels and static configuration files.
//
// This package parses Docker container labels in the format used by Traefik v2/v3
// to configure HTTP routers, as well as static YAML/TOML configuration files.
//
// Example labels:
//
//	traefik.http.routers.myapp.rule=Host(`app.example.com`)
//	traefik.http.routers.myapp.rule=Host(`app.example.com`) || Host(`www.example.com`)
//	traefik.http.routers.myapp.rule=Host(`app.example.com`) && PathPrefix(`/api`)
//
// Example static file (YAML):
//
//	http:
//	  routers:
//	    myapp:
//	      rule: "Host(`app.example.com`)"
package traefik

import (
	"context"
	"log/slog"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/source"
)

const sourceName = "traefik"

// DefaultFilePattern is the default glob pattern for Traefik config files.
const DefaultFilePattern = "*.yml,*.yaml"

// Traefik implements the source.Source interface for extracting hostnames
// from Traefik container labels and static configuration files.
type Traefik struct {
	parser     *Parser
	logger     *slog.Logger
	fileConfig source.FileDiscoveryConfig
}

// Option is a functional option for configuring Traefik.
type Option func(*Traefik)

// WithLogger sets a custom logger.
func WithLogger(logger *slog.Logger) Option {
	return func(t *Traefik) {
		t.logger = logger
	}
}

// WithFileDiscovery configures file-based discovery.
func WithFileDiscovery(config source.FileDiscoveryConfig) Option {
	return func(t *Traefik) {
		t.fileConfig = config
		// Apply default file pattern if not set
		if t.fileConfig.FilePattern == "" {
			t.fileConfig.FilePattern = DefaultFilePattern
		}
	}
}

// New creates a new Traefik source.
func New(opts ...Option) *Traefik {
	t := &Traefik{
		logger:     slog.Default(),
		fileConfig: source.DefaultFileDiscoveryConfig(),
	}

	for _, opt := range opts {
		opt(t)
	}

	t.parser = NewParser(WithParserLogger(t.logger))

	return t
}

// Name returns the source identifier.
func (t *Traefik) Name() string {
	return sourceName
}

// Extract parses Traefik labels and returns discovered hostnames.
//
// This method looks for traefik.http.routers.*.rule labels and extracts
// all Host() patterns from the rule values. Multiple hostnames from the
// same rule are returned as separate Hostname entries.
//
// Returns an empty slice if no Traefik labels are found.
// Never returns an error - malformed rules are logged and skipped.
func (t *Traefik) Extract(ctx context.Context, labels map[string]string) ([]source.Hostname, error) {
	if len(labels) == 0 {
		return nil, nil
	}

	extractions := t.parser.ExtractHostnames(labels)

	hostnames := make([]source.Hostname, 0, len(extractions))
	for _, e := range extractions {
		hostnames = append(hostnames, source.Hostname{
			Name:   e.Hostname,
			Source: sourceName,
			Router: e.Router,
		})
	}

	if len(hostnames) > 0 {
		t.logger.Debug("extracted hostnames from traefik labels",
			slog.Int("count", len(hostnames)),
		)
	}

	return hostnames, nil
}

// Discover finds hostnames from configured Traefik static configuration files.
//
// This method parses Traefik YAML/TOML files for http.routers.*.rule entries,
// extracting Host() patterns. It ONLY parses router rules - middleware files,
// service definitions, and other config sections are safely ignored.
//
// Returns nil, nil if file discovery is not configured for this source.
func (t *Traefik) Discover(ctx context.Context) ([]source.Hostname, error) {
	if !t.SupportsDiscovery() {
		return nil, nil
	}

	t.logger.Debug("discovering hostnames from traefik files",
		slog.Any("paths", t.fileConfig.FilePaths),
		slog.String("pattern", t.fileConfig.FilePattern),
	)

	hostnames, err := t.parser.DiscoverFromFiles(ctx, t.fileConfig.FilePaths, t.fileConfig.FilePattern)
	if err != nil {
		return nil, err
	}

	// Convert to source.Hostname
	result := make([]source.Hostname, 0, len(hostnames))
	for _, e := range hostnames {
		result = append(result, source.Hostname{
			Name:   e.Hostname,
			Source: sourceName,
			Router: e.Router,
		})
	}

	if len(result) > 0 {
		t.logger.Debug("discovered hostnames from traefik files",
			slog.Int("count", len(result)),
		)
	}

	return result, nil
}

// SupportsDiscovery returns true if file paths are configured.
func (t *Traefik) SupportsDiscovery() bool {
	return t.fileConfig.IsEnabled()
}

// FileConfig returns the file discovery configuration.
func (t *Traefik) FileConfig() source.FileDiscoveryConfig {
	return t.fileConfig
}

// Ensure Traefik implements source.Source
var _ source.Source = (*Traefik)(nil)
