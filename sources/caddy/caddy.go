// Package caddy provides a Source implementation for extracting hostnames
// from Caddy-style Docker container labels as used by caddy-docker-proxy.
//
// This package parses container labels in the scheme used by
// lucaslorentz/caddy-docker-proxy, where hostnames are declared as bare
// values on labels named either "caddy" or "caddy_<n>".
//
// Example labels:
//
//	caddy=app.example.com
//	caddy_0=app.example.com
//	caddy_1=www.example.com
//
// Multiple hostnames on a single label are also supported as a
// comma- or whitespace-separated list (a common community extension):
//
//	caddy=app.example.com, www.example.com
package caddy

import (
	"context"
	"log/slog"

	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

const sourceName = "caddy"

// Caddy implements the source.Source interface for extracting hostnames
// from Caddy-style Docker container labels.
//
// Caddy does not currently support static-file discovery in dnsweaver —
// Caddyfile parsing is tracked separately.
type Caddy struct {
	parser *Parser
	logger *slog.Logger
}

// Option is a functional option for configuring Caddy.
type Option func(*Caddy)

// WithLogger sets a custom logger.
func WithLogger(logger *slog.Logger) Option {
	return func(c *Caddy) {
		c.logger = logger
	}
}

// New creates a new Caddy source.
func New(opts ...Option) *Caddy {
	c := &Caddy{
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	c.parser = NewParser(WithParserLogger(c.logger))

	return c
}

// Name returns the source identifier.
func (c *Caddy) Name() string {
	return sourceName
}

// Extract parses Caddy-style labels and returns discovered hostnames.
//
// Recognizes labels named "caddy" or "caddy_<n>" (any suffix following an
// underscore, without a dot). Each label value may contain one or more hostnames
// separated by commas or whitespace. Hostnames are returned in label-key order with
// duplicates removed.
//
// Returns an empty slice if no recognized labels are found.
// Never returns an error — malformed values are logged and skipped.
func (c *Caddy) Extract(_ context.Context, w workload.Workload) ([]source.Hostname, error) {
	if len(w.Labels) == 0 {
		return nil, nil
	}

	extractions := c.parser.ExtractHostnames(w.Labels)
	if len(extractions) == 0 {
		return nil, nil
	}

	hostnames := make([]source.Hostname, 0, len(extractions))
	for _, e := range extractions {
		hostnames = append(hostnames, source.Hostname{
			Name:   e.Hostname,
			Source: sourceName,
			Router: e.Router,
		})
	}

	c.logger.Debug("extracted hostnames from caddy labels",
		slog.Int("count", len(hostnames)),
	)

	return hostnames, nil
}

// Discover is a no-op for Caddy — Caddyfile parsing is not implemented.
func (c *Caddy) Discover(_ context.Context) ([]source.Hostname, error) {
	return nil, nil
}

// SupportsDiscovery always returns false — no static-file discovery.
func (c *Caddy) SupportsDiscovery() bool {
	return false
}

// SupportedPlatforms returns the platforms this source handles.
//
// An empty slice means "any platform exposing labels" — matching the
// behavior of the traefik source.
func (c *Caddy) SupportedPlatforms() []workload.Platform {
	return nil
}
