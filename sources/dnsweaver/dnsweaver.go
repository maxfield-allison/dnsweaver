// Package dnsweaver provides a Source implementation for extracting hostnames
// from native dnsweaver labels on Docker containers/services.
//
// This package parses Docker container labels in two formats:
//
// 1. Simple hostname (uses provider defaults for type/target):
//
//	dnsweaver.hostname=app.example.com
//
// 2. Named records (explicit control per record):
//
//	dnsweaver.records.myapp.hostname=app.example.com
//	dnsweaver.records.myapp.type=A
//	dnsweaver.records.myapp.target=192.0.2.100
//	dnsweaver.records.myapp.provider=internal-dns
//	dnsweaver.records.myapp.ttl=300
//
// For SRV records:
//
//	dnsweaver.records.mc.hostname=_minecraft._tcp.mc.example.com
//	dnsweaver.records.mc.type=SRV
//	dnsweaver.records.mc.target=mc-server.example.com
//	dnsweaver.records.mc.port=25565
//	dnsweaver.records.mc.priority=0
//	dnsweaver.records.mc.weight=5
package dnsweaver

import (
	"context"
	"log/slog"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/source"
)

const sourceName = "dnsweaver"

// DNSWeaver implements the source.Source interface for extracting hostnames
// from native dnsweaver container labels.
type DNSWeaver struct {
	parser *Parser
	logger *slog.Logger
}

// Option is a functional option for configuring DNSWeaver.
type Option func(*DNSWeaver)

// WithLogger sets a custom logger.
func WithLogger(logger *slog.Logger) Option {
	return func(d *DNSWeaver) {
		d.logger = logger
	}
}

// New creates a new DNSWeaver source.
func New(opts ...Option) *DNSWeaver {
	d := &DNSWeaver{
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(d)
	}

	d.parser = NewParser(WithParserLogger(d.logger))

	return d
}

// Name returns the source identifier.
func (d *DNSWeaver) Name() string {
	return sourceName
}

// Extract parses dnsweaver labels and returns discovered hostnames.
//
// This method looks for:
//   - dnsweaver.hostname=<hostname> (simple format)
//   - dnsweaver.records.<name>.hostname=<hostname> (named record format)
//
// Returns an empty slice if no dnsweaver labels are found.
// Malformed labels are logged and skipped.
func (d *DNSWeaver) Extract(ctx context.Context, labels map[string]string) ([]source.Hostname, error) {
	if len(labels) == 0 {
		return nil, nil
	}

	extractions := d.parser.ExtractHostnames(labels)

	hostnames := make([]source.Hostname, 0, len(extractions))
	for _, e := range extractions {
		h := source.Hostname{
			Name:   e.Hostname,
			Source: sourceName,
			Router: e.RecordName, // Use record name as router identifier
		}

		// Copy record hints if present
		if e.HasHints() {
			h.RecordHints = &source.RecordHints{
				Type:     e.Type,
				Target:   e.Target,
				TTL:      e.TTL,
				Provider: e.Provider,
			}
			if e.SRV != nil {
				h.RecordHints.SRV = &source.SRVHints{
					Port:     e.SRV.Port,
					Priority: e.SRV.Priority,
					Weight:   e.SRV.Weight,
				}
			}
		}

		hostnames = append(hostnames, h)
	}

	if len(hostnames) > 0 {
		d.logger.Debug("extracted hostnames from dnsweaver labels",
			slog.Int("count", len(hostnames)),
		)
	}

	return hostnames, nil
}

// Discover is not supported for native labels.
// Native dnsweaver labels only come from container labels, not static files.
func (d *DNSWeaver) Discover(ctx context.Context) ([]source.Hostname, error) {
	return nil, nil
}

// SupportsDiscovery returns false since native labels don't support file discovery.
func (d *DNSWeaver) SupportsDiscovery() bool {
	return false
}

// Ensure DNSWeaver implements source.Source
var _ source.Source = (*DNSWeaver)(nil)
