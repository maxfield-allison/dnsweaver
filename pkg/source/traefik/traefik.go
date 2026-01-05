// Package traefik provides a Source implementation for extracting hostnames
// from Traefik reverse proxy labels.
//
// This package parses Docker container labels in the format used by Traefik v2/v3
// to configure HTTP routers. It extracts hostnames from the Host() matcher in
// router rules.
//
// Example labels:
//
//	traefik.http.routers.myapp.rule=Host(`app.example.com`)
//	traefik.http.routers.myapp.rule=Host(`app.example.com`) || Host(`www.example.com`)
//	traefik.http.routers.myapp.rule=Host(`app.example.com`) && PathPrefix(`/api`)
package traefik

import (
	"context"
	"log/slog"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/source"
)

const sourceName = "traefik"

// Traefik implements the source.Source interface for extracting hostnames
// from Traefik container labels.
type Traefik struct {
	parser *Parser
	logger *slog.Logger
}

// Option is a functional option for configuring Traefik.
type Option func(*Traefik)

// WithLogger sets a custom logger.
func WithLogger(logger *slog.Logger) Option {
	return func(t *Traefik) {
		t.logger = logger
	}
}

// New creates a new Traefik source.
func New(opts ...Option) *Traefik {
	t := &Traefik{
		logger: slog.Default(),
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

// Ensure Traefik implements source.Source
var _ source.Source = (*Traefik)(nil)
