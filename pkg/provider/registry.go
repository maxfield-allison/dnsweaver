// Package provider contains the provider registry for managing multiple provider instances.
package provider

import (
	"fmt"
	"sync"
)

// Factory is a function that creates a new provider instance from configuration.
type Factory func(name string, config map[string]string) (Provider, error)

// Registry manages provider type factories and active provider instances.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory   // type name -> factory function
	instances map[string]Provider  // instance name -> provider
	order     []string             // instance names in priority order
}

// NewRegistry creates a new provider registry.
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]Factory),
		instances: make(map[string]Provider),
		order:     make([]string, 0),
	}
}

// RegisterFactory registers a provider factory for a given type.
func (r *Registry) RegisterFactory(typeName string, factory Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[typeName] = factory
}

// CreateInstance creates and registers a provider instance.
func (r *Registry) CreateInstance(name, typeName string, config map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	factory, ok := r.factories[typeName]
	if !ok {
		return fmt.Errorf("unknown provider type: %s", typeName)
	}

	provider, err := factory(name, config)
	if err != nil {
		return fmt.Errorf("creating provider %s: %w", name, err)
	}

	r.instances[name] = provider
	r.order = append(r.order, name)
	return nil
}

// Get returns a provider instance by name.
func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.instances[name]
	return p, ok
}

// All returns all provider instances in priority order.
func (r *Registry) All() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providers := make([]Provider, 0, len(r.order))
	for _, name := range r.order {
		if p, ok := r.instances[name]; ok {
			providers = append(providers, p)
		}
	}
	return providers
}

// TODO: Implement in Issue #21 - Multi-provider design
// - Integration with matcher for domain routing
// - Batch operations across providers
