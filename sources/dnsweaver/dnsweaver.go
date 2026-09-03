// Package dnsweaver provides a Source implementation for extracting hostnames
// from native dnsweaver labels on Docker containers/services and Kubernetes
// annotations.
//
// This package parses labels/annotations in two formats:
//
// 1. Simple hostname (uses provider defaults for type/target):
//
//	Docker:  dnsweaver.hostname=app.example.com
//	K8s:     dnsweaver.dev/hostname: app.example.com
//
// 2. Named records (explicit control per record):
//
//	Docker:  dnsweaver.records.myapp.hostname=app.example.com
//	K8s:     dnsweaver.dev/records.myapp.hostname: app.example.com
//
// 3. Named record fields (all fields supported in both formats):
//
//	dnsweaver.records.myapp.type=A
//	dnsweaver.records.myapp.target=192.0.2.100
//	dnsweaver.records.myapp.provider=internal-dns
//	dnsweaver.records.myapp.ttl=300
//
// For Kubernetes, annotations with the "dnsweaver.dev/" prefix are automatically
// converted to the "dnsweaver." label format before parsing.
package dnsweaver

import (
	"context"
	"log/slog"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
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

// Extract parses dnsweaver labels/annotations and returns discovered hostnames.
//
// This method looks for:
//   - dnsweaver.hostname=<hostname> (simple format, Docker labels)
//   - dnsweaver.records.<name>.hostname=<hostname> (named record format, Docker labels)
//   - dnsweaver.dev/hostname (K8s annotations, converted to label format)
//   - dnsweaver.dev/records.<name>.hostname (K8s annotations, converted to label format)
//
// For Kubernetes workloads, annotations with the "dnsweaver.dev/" prefix are
// converted to the standard "dnsweaver." label format before parsing. This allows
// K8s users to use annotations while sharing the same parser logic as Docker.
//
// Returns an empty slice if no dnsweaver labels/annotations are found.
// Malformed entries are logged and skipped.
func (d *DNSWeaver) Extract(ctx context.Context, w workload.Workload) ([]source.Hostname, error) {
	effective := d.effectiveLabels(w)
	if len(effective) == 0 {
		return nil, nil
	}

	extractions := d.parser.ExtractHostnames(effective)

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
				Type:          e.Type,
				Target:        e.Target,
				TTL:           e.TTL,
				Provider:      e.Provider,
				AdoptExisting: e.AdoptExisting,
				Metadata:      e.Metadata,
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

// SupportedPlatforms returns an empty slice, meaning the dnsweaver source works
// with all platforms. Both Docker labels and K8s annotations use the same format.
func (d *DNSWeaver) SupportedPlatforms() []workload.Platform {
	return nil
}

// k8sAnnotationPrefix is the prefix used by dnsweaver annotations in Kubernetes.
// Annotations like "dnsweaver.dev/hostname" are converted to the standard label
// format "dnsweaver.hostname" for parser compatibility.
const k8sAnnotationPrefix = "dnsweaver.dev/"

// labelPrefix is the prefix for dnsweaver Docker labels.
const labelPrefix = "dnsweaver."

// effectiveLabels builds a merged label map from both Docker labels and
// K8s annotations. For Kubernetes workloads, annotations starting with
// "dnsweaver.dev/" are converted to the "dnsweaver." label format.
//
// Conversion: "dnsweaver.dev/records.myapp.hostname" → "dnsweaver.records.myapp.hostname"
// Labels take precedence over annotations if both exist for the same key.
func (d *DNSWeaver) effectiveLabels(w workload.Workload) map[string]string {
	// Fast path: Docker workloads only have labels.
	if !w.IsKubernetes() || len(w.Annotations) == 0 {
		return w.Labels
	}

	// Convert K8s annotations with dnsweaver.dev/ prefix to label format.
	var converted map[string]string
	for key, value := range w.Annotations {
		if !strings.HasPrefix(key, k8sAnnotationPrefix) {
			continue
		}
		// dnsweaver.dev/hostname → dnsweaver.hostname
		// dnsweaver.dev/records.myapp.hostname → dnsweaver.records.myapp.hostname
		labelKey := labelPrefix + strings.TrimPrefix(key, k8sAnnotationPrefix)
		if converted == nil {
			converted = make(map[string]string)
		}
		converted[labelKey] = value
	}

	if converted == nil {
		// No dnsweaver.dev/ annotations found, just use labels.
		return w.Labels
	}

	// Merge: labels take precedence over converted annotations.
	merged := make(map[string]string, len(w.Labels)+len(converted))
	for k, v := range converted {
		merged[k] = v
	}
	for k, v := range w.Labels {
		merged[k] = v // Labels win
	}

	return merged
}

// Ensure DNSWeaver implements source.Source
var _ source.Source = (*DNSWeaver)(nil)
