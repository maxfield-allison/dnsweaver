// Package incus provides a Source implementation for extracting hostnames from
// Incus workloads (system containers and virtual machines).
//
// This source constructs DNS A records mapping each running Incus instance to
// its resolved IP address. Three hostname strategies are supported, in order of
// precedence:
//
//  1. Per-instance override: If the instance has a "user.dnsweaver.hostname"
//     config key (surfaced as a workload label), its value is used verbatim as
//     the hostname. This lets operators pin an arbitrary FQDN to an instance.
//
//  2. Domain suffix (recommended): Set a domain via WithDomain("home.example.com").
//     Each instance is registered as "<instance-name>.<domain>"
//     (e.g., webserver.home.example.com).
//
//  3. Bare FQDN: If the instance name already contains a dot, it is used
//     directly as the hostname without appending a domain suffix.
//
// The resolved IP (populated in w.Metadata["ip"] by the adapter) is used as the
// A record target by default. When WithTargetMode(TargetModeInstance) is set,
// the source emits the hostname only and defers record type and target to the
// matching provider instance — useful for pointing all instances at a reverse
// proxy via CNAME. Instances with no resolved IP are skipped in both modes (the
// IP acts as a liveness gate).
//
// Supported workload platforms: [PlatformIncus]
package incus

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

const sourceName = "incus"

// hostnameLabel is the instance config key (surfaced as a workload label) that
// overrides the derived hostname with an explicit value.
const hostnameLabel = "user.dnsweaver.hostname"

// composeHostnameLabel is the incus-compose form of the hostname override.
// incus-compose (https://github.com/lxc/incus-compose) stores Compose labels as
// "user.label.<key>" config keys, which the Incus adapter surfaces de-prefixed.
// A Compose "dnsweaver.hostname" label therefore appears as "dnsweaver.hostname"
// — matching the native dnsweaver source's label. Checked after the native
// "user.dnsweaver.hostname" key.
const composeHostnameLabel = "dnsweaver.hostname"

// TargetMode controls how the Incus source builds DNS record targets.
type TargetMode string

const (
	// TargetModeGuestIP emits an A record per workload pointing at the
	// instance's resolved IP. This is the default.
	TargetModeGuestIP TargetMode = "guest-ip"

	// TargetModeInstance defers record type and target to the matching
	// provider instance. The source emits the hostname only (no RecordHints),
	// so DNSWEAVER_{INSTANCE}_RECORD_TYPE and DNSWEAVER_{INSTANCE}_TARGET
	// drive the resulting record. This enables pointing all Incus-discovered
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
		return "", fmt.Errorf("invalid incus target mode %q (must be one of: guest-ip, instance)", s)
	}
}

// Incus implements the source.Source interface for Incus workloads.
type Incus struct {
	domain     string
	targetMode TargetMode
	logger     *slog.Logger
}

// Option is a functional option for configuring Incus.
type Option func(*Incus)

// WithLogger sets a custom logger.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Incus) {
		s.logger = logger
	}
}

// WithDomain sets the DNS domain suffix used when constructing hostnames from
// instance names. For example, WithDomain("home.example.com") causes an
// instance named "webserver" to be registered as "webserver.home.example.com".
//
// If not set (or set to empty string), only instance names that already contain
// a dot are used as hostnames. Plain instance names without a domain suffix are
// skipped (unless overridden by the "user.dnsweaver.hostname" config key).
func WithDomain(domain string) Option {
	return func(s *Incus) {
		s.domain = strings.TrimPrefix(strings.TrimSpace(domain), ".")
	}
}

// WithTargetMode sets the target resolution strategy. See TargetMode for
// supported values. Defaults to TargetModeGuestIP.
func WithTargetMode(mode TargetMode) Option {
	return func(s *Incus) {
		s.targetMode = mode
	}
}

// New creates a new Incus source.
func New(opts ...Option) *Incus {
	s := &Incus{
		logger:     slog.Default(),
		targetMode: TargetModeGuestIP,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.targetMode == "" {
		s.targetMode = TargetModeGuestIP
	}
	return s
}

// Name returns the source identifier.
func (s *Incus) Name() string {
	return sourceName
}

// SupportedPlatforms returns the platforms this source handles.
// The Incus source only processes workloads with PlatformIncus.
func (s *Incus) SupportedPlatforms() []workload.Platform {
	return []workload.Platform{workload.PlatformIncus}
}

// SupportsDiscovery returns false — the Incus source does not support static
// file discovery. All hostnames come from live workload extraction.
func (s *Incus) SupportsDiscovery() bool {
	return false
}

// Discover is a no-op for the Incus source.
func (s *Incus) Discover(_ context.Context) ([]source.Hostname, error) {
	return nil, nil
}

// Extract builds DNS A records for an Incus workload.
//
// Extraction logic:
//  1. Skip non-Incus workloads (wrong platform).
//  2. Look up the resolved IP in w.Metadata["ip"]. Skip if empty.
//  3. Determine the hostname:
//     a. If the "user.dnsweaver.hostname" label is set, use it verbatim.
//     b. If w.Name contains a dot, use it directly (already FQDN).
//     c. If a domain suffix is configured, append: "<name>.<domain>".
//     d. Otherwise, skip (no valid hostname can be determined).
//  4. Return a single source.Hostname with an A record hint (guest-ip mode).
func (s *Incus) Extract(_ context.Context, w workload.Workload) ([]source.Hostname, error) {
	if !w.Platform.IsIncus() {
		return nil, nil
	}

	ip := w.Metadata["ip"]
	if ip == "" {
		s.logger.Debug("incus workload has no resolved IP; skipping",
			slog.String("workload", w.Name),
			slog.String("project", w.Metadata["project"]),
		)
		return nil, nil
	}

	hostname, err := s.resolveHostname(w)
	if err != nil {
		s.logger.Debug("could not determine hostname for incus workload; skipping",
			slog.String("workload", w.Name),
			slog.String("reason", err.Error()),
		)
		return nil, nil //nolint:nilerr // not a configuration error; silently skip
	}

	h := source.Hostname{
		Name:   hostname,
		Source: sourceName,
		Router: fmt.Sprintf("%s/%s", w.Metadata["project"], w.Name),
	}

	// In guest-ip mode (default) emit an A record hint targeting the instance's
	// IP. In instance mode, leave RecordHints nil so the matching provider
	// instance's RECORD_TYPE and TARGET drive the resulting record. The IP
	// lookup above still acts as a liveness gate — workloads with no resolved
	// IP are skipped in both modes.
	if s.targetMode == TargetModeGuestIP {
		h.RecordHints = &source.RecordHints{
			Type:   "A",
			Target: ip,
		}
	}

	return []source.Hostname{h}, nil
}

// resolveHostname determines the FQDN for an Incus workload.
//
// Precedence: explicit "user.dnsweaver.hostname" label > incus-compose
// "dnsweaver.hostname" label > FQDN instance name > "<name>.<domain>". Returns
// an error (logged as debug, not surfaced to the caller) when no hostname can
// be determined.
func (s *Incus) resolveHostname(w workload.Workload) (string, error) {
	if override := strings.TrimSpace(w.GetLabel(hostnameLabel)); override != "" {
		return override, nil
	}
	if override := strings.TrimSpace(w.GetLabel(composeHostnameLabel)); override != "" {
		return override, nil
	}
	if strings.Contains(w.Name, ".") {
		// Instance name already looks like an FQDN — use it directly.
		return w.Name, nil
	}
	if s.domain != "" {
		return w.Name + "." + s.domain, nil
	}
	return "", fmt.Errorf("instance name %q is not an FQDN and no domain suffix is configured", w.Name)
}
