package source

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/maxfield-allison/dnsweaver/internal/metrics"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

// Registry manages source implementations and coordinates hostname extraction.
//
// The registry maintains an ordered list of sources. When extracting hostnames,
// it queries all registered sources and aggregates the results.
//
// Registry is safe for concurrent use.
type Registry struct {
	mu      sync.RWMutex
	sources []Source
	byName  map[string]Source
	logger  *slog.Logger
}

// NewRegistry creates a new source registry.
func NewRegistry(logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	return &Registry{
		sources: make([]Source, 0),
		byName:  make(map[string]Source),
		logger:  logger,
	}
}

// Register adds a source to the registry.
// Returns an error if a source with the same name is already registered.
func (r *Registry) Register(source Source) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := source.Name()
	if _, exists := r.byName[name]; exists {
		return ErrDuplicateSource(name)
	}

	r.sources = append(r.sources, source)
	r.byName[name] = source

	r.logger.Debug("registered source",
		slog.String("source", name),
	)

	return nil
}

// Get returns a source by name.
// Returns nil if not found.
func (r *Registry) Get(name string) Source {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byName[name]
}

// All returns all registered sources in registration order.
func (r *Registry) All() []Source {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make([]Source, len(r.sources))
	copy(result, r.sources)
	return result
}

// Count returns the number of registered sources.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sources)
}

// ExtractAll queries all registered sources and returns all discovered hostnames.
//
// Each source is queried with the provided workload. Results are aggregated
// in source registration order. Duplicate hostnames are NOT removed to
// preserve source attribution - use Hostnames.Deduplicate() if needed.
//
// Workloads with dnsweaver.enabled=false (Docker label) or
// dnsweaver.dev/enabled=false (K8s annotation) are skipped entirely —
// no sources are queried.
//
// Sources that declare SupportedPlatforms() are only queried if the workload's
// platform matches. Sources with empty SupportedPlatforms() are always queried.
//
// If a source returns an error, extraction continues with remaining sources.
// Errors are logged but not returned to allow partial results.
func (r *Registry) ExtractAll(ctx context.Context, w workload.Workload) Hostnames {
	hostnames, _ := r.ExtractAllWithStatus(ctx, w)
	return hostnames
}

// ExtractAllWithStatus is ExtractAll plus a completeness signal. complete is
// false when any applicable source failed, even when other sources returned
// usable claims. Reconciliation uses this to suppress removals from a partial
// desired-state snapshot.
func (r *Registry) ExtractAllWithStatus(ctx context.Context, w workload.Workload) (hostnames Hostnames, complete bool) {
	// Global opt-out: dnsweaver.enabled=false skips ALL sources.
	// Check Docker labels first, then K8s annotations.
	if isWorkloadDisabled(w) {
		r.logger.Debug("workload disabled via dnsweaver.enabled=false, skipping all sources",
			slog.String("workload", w.Name),
		)
		return nil, true
	}

	workloadAdopt, adoptKey, adoptValid := workloadAdoptExistingHint(w)
	if !adoptValid {
		r.logger.Warn("invalid workload adoption hint (must be true/false); ignoring",
			slog.String("workload", w.Name),
			slog.String("key", adoptKey),
		)
	}

	r.mu.RLock()
	sources := make([]Source, len(r.sources))
	copy(sources, r.sources)
	r.mu.RUnlock()

	var allHostnames Hostnames
	complete = true

	for _, src := range sources {
		// Skip sources that don't support this workload's platform
		if !sourceSupports(src, w.Platform) {
			continue
		}

		hostnames, err := src.Extract(ctx, w)
		if err != nil {
			complete = false
			r.logger.Warn("source extraction failed",
				slog.String("source", src.Name()),
				slog.String("error", err.Error()),
			)
			continue
		}

		if len(hostnames) > 0 {
			applyWorkloadAdoptionHint(hostnames, workloadAdopt)
			r.logger.Debug("source extracted hostnames",
				slog.String("source", src.Name()),
				slog.Int("count", len(hostnames)),
			)
			metrics.HostnamesExtractedTotal.WithLabelValues(src.Name(), "labels").Add(float64(len(hostnames)))
			allHostnames = append(allHostnames, hostnames...)
		}
	}

	return allHostnames, complete
}

// sourceSupports returns true if the source supports the given platform.
// Sources with empty SupportedPlatforms() support all platforms.
func sourceSupports(src Source, platform workload.Platform) bool {
	platforms := src.SupportedPlatforms()
	if len(platforms) == 0 {
		return true // empty means all platforms
	}
	for _, p := range platforms {
		if p == platform {
			return true
		}
	}
	return false
}

// isWorkloadDisabled returns true if the workload has opted out of DNS management.
//
// Checks both Docker labels ("dnsweaver.enabled") and Kubernetes annotations
// ("dnsweaver.dev/enabled"). The value must be "false" (case-insensitive,
// whitespace-trimmed) to disable.
func isWorkloadDisabled(w workload.Workload) bool {
	// Docker labels: dnsweaver.enabled=false
	if v, ok := w.Labels["dnsweaver.enabled"]; ok {
		if strings.EqualFold(strings.TrimSpace(v), "false") {
			return true
		}
	}

	// K8s annotations: dnsweaver.dev/enabled=false
	if v, ok := w.Annotations["dnsweaver.dev/enabled"]; ok {
		if strings.EqualFold(strings.TrimSpace(v), "false") {
			return true
		}
	}

	return false
}

// DiscoverAll queries all sources that support file-based discovery.
//
// Each source with SupportsDiscovery() == true is queried via Discover().
// Results are aggregated in source registration order. Duplicate hostnames
// are NOT removed to preserve source attribution - use Hostnames.Deduplicate()
// if needed.
//
// If a source returns an error, discovery continues with remaining sources.
// Errors are logged but not returned to allow partial results.
func (r *Registry) DiscoverAll(ctx context.Context) Hostnames {
	hostnames, _ := r.DiscoverAllWithStatus(ctx)
	return hostnames
}

// DiscoverAllWithStatus is DiscoverAll plus a completeness signal. complete
// is false when any discoverable source failed.
func (r *Registry) DiscoverAllWithStatus(ctx context.Context) (hostnames Hostnames, complete bool) {
	r.mu.RLock()
	sources := make([]Source, len(r.sources))
	copy(sources, r.sources)
	r.mu.RUnlock()

	var allHostnames Hostnames
	complete = true

	for _, src := range sources {
		if !src.SupportsDiscovery() {
			continue
		}

		hostnames, err := src.Discover(ctx)
		if err != nil {
			complete = false
			r.logger.Warn("source file discovery failed",
				slog.String("source", src.Name()),
				slog.String("error", err.Error()),
			)
			continue
		}

		if len(hostnames) > 0 {
			r.logger.Debug("source discovered hostnames from files",
				slog.String("source", src.Name()),
				slog.Int("count", len(hostnames)),
			)
			metrics.HostnamesExtractedTotal.WithLabelValues(src.Name(), "files").Add(float64(len(hostnames)))
			allHostnames = append(allHostnames, hostnames...)
		}
	}

	return allHostnames, complete
}

// DiscoverableSources returns sources that have file discovery configured.
func (r *Registry) DiscoverableSources() []Source {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var discoverable []Source
	for _, src := range r.sources {
		if src.SupportsDiscovery() {
			discoverable = append(discoverable, src)
		}
	}
	return discoverable
}

// ExtractFrom queries a specific source by name.
// Returns an error if the source is not found.
func (r *Registry) ExtractFrom(ctx context.Context, sourceName string, w workload.Workload) (Hostnames, error) {
	r.mu.RLock()
	src, exists := r.byName[sourceName]
	r.mu.RUnlock()

	if !exists {
		return nil, ErrSourceNotFound(sourceName)
	}

	hostnames, err := src.Extract(ctx, w)
	if err != nil {
		return nil, err
	}
	workloadAdopt, adoptKey, valid := workloadAdoptExistingHint(w)
	if !valid {
		r.logger.Warn("invalid workload adoption hint (must be true/false); ignoring",
			slog.String("workload", w.Name),
			slog.String("key", adoptKey),
		)
	} else {
		applyWorkloadAdoptionHint(hostnames, workloadAdopt)
	}
	return hostnames, nil
}

// workloadAdoptExistingHint reads the workload-wide adoption hint. Docker
// labels take precedence over Kubernetes annotations, matching native-label
// conversion behavior. The third return value reports whether a present value
// was valid; absence is valid and returns nil.
func workloadAdoptExistingHint(w workload.Workload) (*bool, string, bool) {
	const (
		dockerKey = "dnsweaver.adopt"
		k8sKey    = "dnsweaver.dev/adopt"
	)

	value, ok := w.Labels[dockerKey]
	key := dockerKey
	if !ok {
		value, ok = w.Annotations[k8sKey]
		key = k8sKey
	}
	if !ok {
		return nil, "", true
	}

	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true":
		parsed := true
		return &parsed, key, true
	case "false":
		parsed := false
		return &parsed, key, true
	default:
		return nil, key, false
	}
}

// applyWorkloadAdoptionHint applies a workload-wide hint to hostnames found by
// every source, including Traefik. A record-specific hint already attached by
// the native source takes precedence.
func applyWorkloadAdoptionHint(hostnames Hostnames, adopt *bool) {
	if adopt == nil {
		return
	}
	for i := range hostnames {
		if hostnames[i].AdoptExisting == nil {
			value := *adopt
			hostnames[i].AdoptExisting = &value
		}
	}
}

// DiscoverFrom queries a specific source by name for file-based discovery.
// Returns an error if the source is not found or doesn't support discovery.
func (r *Registry) DiscoverFrom(ctx context.Context, sourceName string) (Hostnames, error) {
	r.mu.RLock()
	src, exists := r.byName[sourceName]
	r.mu.RUnlock()

	if !exists {
		return nil, ErrSourceNotFound(sourceName)
	}

	if !src.SupportsDiscovery() {
		return nil, nil // Not an error, just no discovery configured
	}

	return src.Discover(ctx)
}
