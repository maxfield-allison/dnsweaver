// Package provider contains the provider registry for managing multiple provider instances.
package provider

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"gitlab.bluewillows.net/root/dnsweaver/internal/matcher"
)

// Factory is a function that creates a new provider instance from configuration.
type Factory func(name string, config map[string]string) (Provider, error)

// Registry manages provider type factories and active provider instances.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory           // type name -> factory function
	instances []*ProviderInstance          // instances in priority order
	byName    map[string]*ProviderInstance // instance name -> instance
	logger    *slog.Logger
}

// NewRegistry creates a new provider registry.
func NewRegistry(logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	return &Registry{
		factories: make(map[string]Factory),
		instances: make([]*ProviderInstance, 0),
		byName:    make(map[string]*ProviderInstance),
		logger:    logger,
	}
}

// RegisterFactory registers a provider factory for a given type.
func (r *Registry) RegisterFactory(typeName string, factory Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[typeName] = factory
	r.logger.Debug("registered provider factory", slog.String("type", typeName))
}

// CreateInstance creates and registers a provider instance from configuration.
func (r *Registry) CreateInstance(cfg ProviderInstanceConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration for provider %q: %w", cfg.Name, err)
	}

	// Check for duplicate names
	if _, exists := r.byName[cfg.Name]; exists {
		return fmt.Errorf("provider instance %q already exists", cfg.Name)
	}

	// Get factory for this provider type
	factory, ok := r.factories[cfg.TypeName]
	if !ok {
		return fmt.Errorf("unknown provider type: %s", cfg.TypeName)
	}

	// Create the underlying provider
	provider, err := factory(cfg.Name, cfg.ProviderConfig)
	if err != nil {
		return fmt.Errorf("creating provider %s: %w", cfg.Name, err)
	}

	// Create domain matcher
	matcherCfg := matcher.DomainMatcherConfig{
		Includes: cfg.GetIncludes(),
		Excludes: cfg.GetExcludes(),
		UseRegex: cfg.UseRegex(),
	}
	domainMatcher, err := matcher.NewDomainMatcher(matcherCfg)
	if err != nil {
		return fmt.Errorf("creating domain matcher for %s: %w", cfg.Name, err)
	}

	// Create provider instance
	instance := &ProviderInstance{
		Provider:   provider,
		Matcher:    domainMatcher,
		RecordType: cfg.RecordType,
		Target:     cfg.Target,
		TTL:        cfg.TTL,
		Mode:       cfg.Mode,
	}

	// Default to managed mode if not set
	if instance.Mode == "" {
		instance.Mode = ModeManaged
	}

	r.instances = append(r.instances, instance)
	r.byName[cfg.Name] = instance

	r.logger.Info("created provider instance",
		slog.String("name", cfg.Name),
		slog.String("type", cfg.TypeName),
		slog.String("record_type", string(cfg.RecordType)),
		slog.String("target", cfg.Target),
		slog.String("mode", string(instance.Mode)),
	)

	return nil
}

// Get returns a provider instance by name.
func (r *Registry) Get(name string) (*ProviderInstance, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byName[name]
	return p, ok
}

// All returns all provider instances in priority order.
func (r *Registry) All() []*ProviderInstance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make([]*ProviderInstance, len(r.instances))
	copy(result, r.instances)
	return result
}

// MatchingProviders returns all provider instances that match the given hostname.
// The order matches the priority order from DNSWEAVER_INSTANCES.
func (r *Registry) MatchingProviders(hostname string) []*ProviderInstance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matches []*ProviderInstance
	for _, inst := range r.instances {
		if inst.Matches(hostname) {
			matches = append(matches, inst)
		}
	}

	return matches
}

// FirstMatchingProvider returns the first provider instance that matches the hostname.
// Returns nil if no provider matches.
func (r *Registry) FirstMatchingProvider(hostname string) *ProviderInstance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, inst := range r.instances {
		if inst.Matches(hostname) {
			return inst
		}
	}

	return nil
}

// PingAll checks connectivity to all provider instances.
// Returns a map of instance name to error (nil if healthy).
func (r *Registry) PingAll(ctx context.Context) map[string]error {
	r.mu.RLock()
	instances := make([]*ProviderInstance, len(r.instances))
	copy(instances, r.instances)
	r.mu.RUnlock()

	results := make(map[string]error, len(instances))
	for _, inst := range instances {
		err := inst.Ping(ctx)
		results[inst.Name()] = err
		if err != nil {
			r.logger.Warn("provider ping failed",
				slog.String("provider", inst.Name()),
				slog.String("error", err.Error()),
			)
		}
	}

	return results
}

// Close cleanly shuts down all provider instances.
// This allows providers to release resources if needed.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for _, inst := range r.instances {
		// Providers may implement a Close method in the future
		// For now, just clear the registry
		r.logger.Debug("closing provider instance", slog.String("name", inst.Name()))
	}

	r.instances = nil
	r.byName = make(map[string]*ProviderInstance)

	return firstErr
}

// Count returns the number of registered provider instances.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.instances)
}
