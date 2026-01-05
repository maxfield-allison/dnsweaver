package source

import (
	"context"
	"log/slog"
	"sync"
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
// Each source is queried with the provided labels. Results are aggregated
// in source registration order. Duplicate hostnames are NOT removed to
// preserve source attribution - use Hostnames.Deduplicate() if needed.
//
// If a source returns an error, extraction continues with remaining sources.
// Errors are logged but not returned to allow partial results.
func (r *Registry) ExtractAll(ctx context.Context, labels map[string]string) Hostnames {
	r.mu.RLock()
	sources := make([]Source, len(r.sources))
	copy(sources, r.sources)
	r.mu.RUnlock()

	var allHostnames Hostnames

	for _, src := range sources {
		hostnames, err := src.Extract(ctx, labels)
		if err != nil {
			r.logger.Warn("source extraction failed",
				slog.String("source", src.Name()),
				slog.String("error", err.Error()),
			)
			continue
		}

		if len(hostnames) > 0 {
			r.logger.Debug("source extracted hostnames",
				slog.String("source", src.Name()),
				slog.Int("count", len(hostnames)),
			)
			allHostnames = append(allHostnames, hostnames...)
		}
	}

	return allHostnames
}

// ExtractFrom queries a specific source by name.
// Returns an error if the source is not found.
func (r *Registry) ExtractFrom(ctx context.Context, sourceName string, labels map[string]string) (Hostnames, error) {
	r.mu.RLock()
	src, exists := r.byName[sourceName]
	r.mu.RUnlock()

	if !exists {
		return nil, ErrSourceNotFound(sourceName)
	}

	return src.Extract(ctx, labels)
}
