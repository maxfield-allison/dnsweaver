// Package provider contains the provider registry for managing multiple provider instances.
package provider

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/maxfield-allison/dnsweaver/internal/matcher"
	"github.com/maxfield-allison/dnsweaver/pkg/httputil"
)

// HTTPConfig contains HTTP client configuration passed from the framework to providers.
// This allows centralized HTTP settings (timeouts, TLS, user-agent) to be applied
// consistently across all HTTP-based providers.
type HTTPConfig struct {
	// Timeout is the HTTP client timeout.
	Timeout time.Duration

	// TLS is the unified per-instance TLS configuration (custom CA, mTLS,
	// SNI override, min version, skip-verify). Nil or zero means "use
	// stdlib defaults". Providers should pass this verbatim to
	// httputil.NewClient — no per-provider TLS interpretation should be
	// required.
	TLS *httputil.TLSConfig

	// TLSSkipVerify is a legacy convenience shortcut for TLS.InsecureSkip.
	// Retained for back-compat with provider factories that have not yet
	// migrated to the unified TLS struct. Deprecated: prefer TLS.
	TLSSkipVerify bool

	// UserAgent is the User-Agent header to use for requests.
	UserAgent string

	// Logger is the logger to use for HTTP debug logging.
	Logger *slog.Logger
}

// FactoryConfig contains all configuration needed to create a provider instance.
// This is passed to Factory functions and includes both provider-specific config
// and shared HTTP configuration.
type FactoryConfig struct {
	// Name is the unique instance name for this provider.
	Name string

	// ProviderConfig contains provider-specific key-value configuration.
	ProviderConfig map[string]string

	// HTTP contains shared HTTP client configuration.
	HTTP HTTPConfig
}

// Factory is a function that creates a new provider instance from configuration.
type Factory func(cfg FactoryConfig) (Provider, error)

// Registry manages provider type factories and active provider instances.
type Registry struct {
	mu         sync.RWMutex
	factories  map[string]Factory           // type name -> factory function
	instances  []*ProviderInstance          // instances in priority order
	byName     map[string]*ProviderInstance // instance name -> instance
	instanceID string                       // dnsweaver instance ID for multi-instance mode
	logger     *slog.Logger
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

// SetInstanceID sets the dnsweaver instance ID for multi-instance coordination.
// This must be called before creating any provider instances.
func (r *Registry) SetInstanceID(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.instanceID = id
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

	// Build FactoryConfig with HTTP settings, lifting any per-instance TLS
	// settings out of ProviderConfig so every HTTP-based factory consumes
	// them through the same shared HTTPConfig.TLS field rather than each
	// re-parsing the map. The TLS_* keys are added to providerConfigFields
	// in internal/config and so always live in the map when set.
	tlsCfg := extractTLSConfig(cfg.ProviderConfig, r.logger, cfg.Name)
	factoryCfg := FactoryConfig{
		Name:           cfg.Name,
		ProviderConfig: cfg.ProviderConfig,
		HTTP: HTTPConfig{
			TLS:           tlsCfg,
			TLSSkipVerify: tlsCfg != nil && tlsCfg.InsecureSkip,
			Logger:        r.logger,
		},
	}

	// Create the underlying provider
	provider, err := factory(factoryCfg)
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
		Provider:                    provider,
		Matcher:                     domainMatcher,
		RecordType:                  cfg.RecordType,
		Target:                      cfg.Target,
		TTL:                         cfg.TTL,
		Mode:                        cfg.Mode,
		AdoptExisting:               cfg.AdoptExisting,
		AdoptExistingAllowOverrides: cfg.AdoptExistingAllowOverrides,
		InstanceID:                  r.instanceID,
		MetadataFilters:             cfg.MetadataFilters,
		Identity:                    IdentityOf(provider),
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
//
// This domain-only variant ignores MetadataFilters. Prefer
// MatchingProvidersForHostname when full hostname metadata is available so
// that DNSWEAVER_{NAME}_ENTRYPOINTS-style scoping takes effect.
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

// MatchingProvidersForHostname returns all provider instances that match
// the given hostname AND satisfy any configured metadata filters. The order
// matches the priority order from DNSWEAVER_INSTANCES, so callers can pick
// the first match for first-match-wins precedence.
func (r *Registry) MatchingProvidersForHostname(hostname string, metadata map[string]string) []*ProviderInstance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matches []*ProviderInstance
	for _, inst := range r.instances {
		if inst.MatchesWithMetadata(hostname, metadata) {
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

// Remove removes a provider instance by name.
// Returns true if the provider was found and removed, false otherwise.
func (r *Registry) Remove(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	inst, ok := r.byName[name]
	if !ok {
		return false
	}

	// Remove from byName map
	delete(r.byName, name)

	// Remove from instances slice
	for i, p := range r.instances {
		if p == inst {
			r.instances = append(r.instances[:i], r.instances[i+1:]...)
			break
		}
	}

	r.logger.Debug("removed provider instance", slog.String("name", name))
	return true
}

// Close cleanly shuts down all provider instances.
// This allows providers to release resources if needed.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for _, inst := range r.instances {
		// Providers holding long-lived resources (e.g. SSH/SFTP sessions)
		// implement Closer; release them on shutdown.
		if closer, ok := inst.Provider.(Closer); ok {
			if err := closer.Close(); err != nil {
				r.logger.Warn("error closing provider instance",
					slog.String("name", inst.Name()),
					slog.String("error", err.Error()),
				)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}
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

// WarnDuplicateIdentities scans all registered instances and emits one WARN
// log entry per group of instances that share the same backend identity AND
// RecordType. Such instances will collide when both match the same hostname
// (one will overwrite the other every reconciliation, see #86), so the
// reconciler resolves the collision via first-match-wins. Surfacing the
// collision once at startup makes the misconfiguration visible without
// spamming a warning on every reconcile.
//
// Intended to be called from main() once after all providers are loaded.
func (r *Registry) WarnDuplicateIdentities() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	type identityKey struct {
		Identity   ProviderIdentity
		RecordType RecordType
	}
	groups := make(map[identityKey][]string, len(r.instances))
	order := make([]identityKey, 0)
	for _, inst := range r.instances {
		k := identityKey{Identity: inst.Identity, RecordType: inst.RecordType}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], inst.Name())
	}

	for _, k := range order {
		names := groups[k]
		if len(names) < 2 {
			continue
		}
		r.logger.Warn("multiple provider instances share the same backend identity; only the first to match a hostname will write",
			slog.String("provider_type", k.Identity.Type),
			slog.String("endpoint", k.Identity.Endpoint),
			slog.String("zone", k.Identity.Zone),
			slog.String("record_type", string(k.RecordType)),
			slog.Any("instances", names),
			slog.String("winner", names[0]),
			slog.String("hint", "ensure each backend (type+endpoint+zone+record_type) is referenced by at most one DNSWEAVER_INSTANCES entry, or use DNSWEAVER_{NAME}_ENTRYPOINTS / metadata filters to make scopes mutually exclusive"),
		)
	}
}
