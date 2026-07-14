// Package proxmox provides a Source implementation for extracting hostnames
// from Proxmox VE workloads (VMs and LXC containers).
//
// This source constructs DNS A records mapping each running Proxmox resource
// to its resolved IP address. Two hostname strategies are supported:
//
//  1. Domain suffix (recommended): Set a domain via WithDomain("home.example.com").
//     Each resource is registered as "<vm-name>.<domain>" (e.g., webserver.home.example.com).
//
//  2. Bare FQDN: If the VM name already contains a dot, it is used directly
//     as the hostname without appending a domain suffix.
//
// The resolved IP (populated in w.Metadata["ip"] by the adapter) is used as
// the A record target by default. When WithTargetMode(TargetModeInstance) is
// set, the source emits the hostname only and defers record type and target
// to the matching provider instance — useful for pointing all VMs at a
// reverse proxy via CNAME. Resources with no resolved IP are skipped in both
// modes (the IP acts as a liveness gate).
//
// Supported workload platforms: [PlatformProxmox]
package proxmox

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

const sourceName = "proxmox"

// TargetMode controls how the Proxmox source builds DNS record targets.
type TargetMode string

const (
	// TargetModeGuestIP emits an A record per workload pointing at the VM's
	// resolved IP. This is the default and preserves historical behavior.
	TargetModeGuestIP TargetMode = "guest-ip"

	// TargetModeInstance defers record type and target to the matching
	// provider instance. The source emits the hostname only (no RecordHints),
	// so DNSWEAVER_{INSTANCE}_RECORD_TYPE and DNSWEAVER_{INSTANCE}_TARGET
	// drive the resulting record. This enables pointing all Proxmox-discovered
	// hostnames at a reverse proxy via CNAME or A records.
	TargetModeInstance TargetMode = "instance"
)

// ParseTargetMode parses a string into a TargetMode value. Empty input maps to
// the default (TargetModeGuestIP). Returns an error for unrecognized values.
func ParseTargetMode(s string) (TargetMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", string(TargetModeGuestIP):
		return TargetModeGuestIP, nil
	case string(TargetModeInstance):
		return TargetModeInstance, nil
	default:
		return "", fmt.Errorf("invalid proxmox target mode %q (must be one of: guest-ip, instance)", s)
	}
}

// Proxmox implements the source.Source interface for Proxmox VE workloads.
type Proxmox struct {
	domain            string
	targetMode        TargetMode
	hostnameTagPrefix string
	logger            *slog.Logger
}

// Option is a functional option for configuring Proxmox.
type Option func(*Proxmox)

// WithLogger sets a custom logger.
func WithLogger(logger *slog.Logger) Option {
	return func(p *Proxmox) {
		p.logger = logger
	}
}

// WithDomain sets the DNS domain suffix used when constructing hostnames from
// VM names. For example, WithDomain("home.example.com") causes a VM named
// "webserver" to be registered as "webserver.home.example.com".
//
// If not set (or set to empty string), only VM names that already contain a dot
// are used as hostnames. Plain VM names without a domain suffix are skipped.
func WithDomain(domain string) Option {
	return func(p *Proxmox) {
		p.domain = strings.TrimPrefix(strings.TrimSpace(domain), ".")
	}
}

// WithHostnameTagPrefix sets an optional tag prefix used to derive an explicit
// hostname from Proxmox tags. Tags matching "<prefix>+<hostname>" are treated
// as hostname overrides. Explicit FQDN values are used verbatim, and bare
// hostnames are appended with the configured domain suffix when present.
func WithHostnameTagPrefix(prefix string) Option {
	return func(p *Proxmox) {
		p.hostnameTagPrefix = strings.TrimSpace(prefix)
	}
}

// WithTargetMode sets the target resolution strategy. See TargetMode for
// supported values. Defaults to TargetModeGuestIP.
func WithTargetMode(mode TargetMode) Option {
	return func(p *Proxmox) {
		p.targetMode = mode
	}
}

// New creates a new Proxmox source.
func New(opts ...Option) *Proxmox {
	p := &Proxmox{
		logger:     slog.Default(),
		targetMode: TargetModeGuestIP,
	}
	for _, opt := range opts {
		opt(p)
	}
	if p.targetMode == "" {
		p.targetMode = TargetModeGuestIP
	}
	return p
}

// Name returns the source identifier.
func (p *Proxmox) Name() string {
	return sourceName
}

// SupportedPlatforms returns the platforms this source handles.
// The Proxmox source only processes workloads with PlatformProxmox.
func (p *Proxmox) SupportedPlatforms() []workload.Platform {
	return []workload.Platform{workload.PlatformProxmox}
}

// SupportsDiscovery returns false — the Proxmox source does not support
// static file discovery. All hostnames come from live workload extraction.
func (p *Proxmox) SupportsDiscovery() bool {
	return false
}

// Discover is a no-op for the Proxmox source.
func (p *Proxmox) Discover(_ context.Context) ([]source.Hostname, error) {
	return nil, nil
}

// Extract builds DNS A records for a Proxmox workload.
//
// Extraction logic:
//  1. Skip non-Proxmox workloads (wrong platform).
//  2. Look up the resolved IP in w.Metadata["ip"]. Skip if empty.
//  3. Determine the hostname:
//     a. If w.Name contains a dot, use it directly (already FQDN).
//     b. If a domain suffix is configured, append: "<name>.<domain>".
//     c. Otherwise, skip (no valid hostname can be determined).
//  4. Return a single source.Hostname with an A record hint.
//
// Each returned Hostname has RecordHints.Type="A" and RecordHints.Target set to
// the resolved IP, allowing providers to create or update A records.
func (p *Proxmox) Extract(_ context.Context, w workload.Workload) ([]source.Hostname, error) {
	if !w.Platform.IsProxmox() {
		return nil, nil
	}

	ip := w.Metadata["ip"]
	if ip == "" {
		p.logger.Debug("proxmox workload has no resolved IP; skipping",
			slog.String("workload", w.Name),
			slog.String("node", w.Metadata["node"]),
		)
		return nil, nil
	}

	hostname, err := p.resolveHostname(w)
	if err != nil {
		p.logger.Debug("could not determine hostname for proxmox workload; skipping",
			slog.String("workload", w.Name),
			slog.String("reason", err.Error()),
		)
		return nil, nil //nolint:nilerr // not a configuration error; silently skip
	}

	h := source.Hostname{
		Name:   hostname,
		Source: sourceName,
		Router: fmt.Sprintf("%s/%s", w.Metadata["node"], w.Metadata["vmid"]),
	}

	// In guest-ip mode (default) emit an A record hint targeting the VM's IP.
	// In instance mode, leave RecordHints nil so the matching provider
	// instance's RECORD_TYPE and TARGET drive the resulting record. The IP
	// lookup above still acts as a liveness gate — workloads with no
	// resolved IP are skipped in both modes.
	if p.targetMode == TargetModeGuestIP {
		h.RecordHints = &source.RecordHints{
			Type:   "A",
			Target: ip,
		}
	}

	return []source.Hostname{h}, nil
}

// resolveHostname determines the FQDN for a given VM name.
// Returns an error (logged as debug, not returned to caller) if no hostname can be determined.
func (p *Proxmox) resolveHostname(w workload.Workload) (string, error) {
	if tagHostname := p.resolveHostnameFromTags(w); tagHostname != "" {
		return tagHostname, nil
	}
	if strings.Contains(w.Name, ".") {
		// VM name already looks like an FQDN — use it directly.
		return w.Name, nil
	}
	if p.domain != "" {
		return w.Name + "." + p.domain, nil
	}
	return "", fmt.Errorf("VM name %q is not an FQDN and no domain suffix is configured", w.Name)
}

func (p *Proxmox) resolveHostnameFromTags(w workload.Workload) string {
	if p.hostnameTagPrefix == "" {
		return ""
	}

	tags := w.Metadata["tags"]
	if tags == "" {
		return ""
	}

	prefix := p.hostnameTagPrefix + "+"
	var firstMatch string
	for _, tag := range strings.Split(tags, ";") {
		tag = strings.TrimSpace(tag)
		if !strings.HasPrefix(tag, prefix) {
			continue
		}
		override := strings.TrimSpace(strings.TrimPrefix(tag, prefix))
		if override == "" {
			continue
		}
		if firstMatch == "" {
			firstMatch = override
			continue
		}
		if p.logger != nil {
			p.logger.Debug("multiple hostname override tags found; using first match",
				"tags", tags,
				"prefix", p.hostnameTagPrefix,
				"first", firstMatch,
				"next", override)
		}
		break
	}

	if firstMatch == "" {
		return ""
	}
	if strings.Contains(firstMatch, ".") {
		return firstMatch
	}
	if p.domain != "" {
		return firstMatch + "." + p.domain
	}
	return firstMatch
}
