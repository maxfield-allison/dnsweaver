// Package reconciler implements the core logic for comparing desired DNS state
// (from sources) with actual DNS state (from providers) and applying changes.
package reconciler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maxfield-allison/dnsweaver/internal/metrics"
	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

// Common error messages used in reconciliation actions.
const (
	errRecordAlreadyExists = "record already exists"
	errRecordTypeConflict  = "record type conflict"
	errNoMatchingProvider  = "no matching provider"
)

// Reconciliation-level outcome labels used for the reconciliations_total metric.
const (
	reconciliationStatusSuccess = "success"
	reconciliationStatusError   = "error"
)

// Config holds reconciler configuration options.
type Config struct {
	// DryRun if true, logs changes without applying them.
	DryRun bool

	// CleanupOrphans if true, removes DNS records for missing workloads.
	CleanupOrphans bool

	// OwnershipTracking if true, creates TXT records to mark ownership of DNS records.
	// When orphan cleanup runs, only records with ownership markers will be deleted.
	// This prevents deletion of manually-created DNS records.
	OwnershipTracking bool

	// AdoptExisting if true, creates ownership TXT records for existing DNS records
	// that have matching targets. If false, existing records are left unmanaged.
	AdoptExisting bool

	// ReconcileInterval is the interval between full reconciliation runs.
	// Zero means no automatic reconciliation (only on-demand).
	ReconcileInterval time.Duration

	// Enabled controls whether reconciliation is active.
	// When false, Reconcile() returns immediately without doing anything.
	Enabled bool

	// InstanceID is the unique identifier for this dnsweaver instance.
	// Used for multi-instance coordination to scope ownership records.
	// Empty string means single-instance mode (legacy behavior).
	InstanceID string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		DryRun:            false,
		CleanupOrphans:    true,
		OwnershipTracking: true,
		AdoptExisting:     false,
		ReconcileInterval: 60 * time.Second,
		Enabled:           true,
	}
}

// Reconciler coordinates DNS record synchronization between sources and providers.
//
// The reconciler:
//  1. Scans workloads from all registered platform listers (Docker, Kubernetes, etc.)
//  2. Extracts hostnames from each workload using registered sources
//  3. For each hostname, finds matching provider(s) based on domain patterns
//  4. Ensures DNS records exist for discovered hostnames
//  5. Optionally removes orphan records (hostnames no longer in workloads)
type Reconciler struct {
	listers   []workload.Lister
	sources   *source.Registry
	providers *provider.Registry
	config    Config
	logger    *slog.Logger

	// enabled and dryRun are the runtime-mutable copies of config.Enabled
	// and config.DryRun. They use atomic.Bool for safe concurrent access
	// from SetEnabled/SetDryRun (API handlers) and Reconcile (periodic timer,
	// Docker/K8s event handlers).
	enabled atomic.Bool
	dryRun  atomic.Bool

	// mu protects knownHostnames and recoveredMetadata during concurrent access
	mu sync.RWMutex
	// knownHostnames tracks hostnames discovered in the last reconciliation.
	// Used for orphan detection.
	knownHostnames map[string]struct{}

	// hostnameProviders tracks which provider(s) each hostname was routed to
	// in the previous reconciliation. Used by orphan cleanup to delete records
	// from the correct provider even when domain patterns have changed (#51).
	hostnameProviders map[string][]string

	// reconcileMu serializes full Reconcile() calls to prevent concurrent
	// reconciliation cycles from interleaving (e.g., periodic timer firing
	// while a Docker/K8s event-triggered reconciliation is still running).
	reconcileMu sync.Mutex

	// recoveredMetadata stores per-hostname metadata recovered from ownership
	// TXT records on startup. This is populated by RecoverOwnership and consumed
	// by the first reconciliation cycle. After the first cycle completes, this
	// map is cleared — sources become the authoritative metadata source.
	recoveredMetadata map[string]map[string]string
}

// Option is a functional option for configuring the Reconciler.
type Option func(*Reconciler)

// WithLogger sets a custom logger for the reconciler.
func WithLogger(logger *slog.Logger) Option {
	return func(r *Reconciler) {
		r.logger = logger
	}
}

// WithConfig sets the reconciler configuration.
func WithConfig(cfg Config) Option {
	return func(r *Reconciler) {
		r.config = cfg
	}
}

// New creates a new Reconciler with the given dependencies.
//
// The reconciler requires:
//   - listers: one or more workload.Lister implementations (Docker, Kubernetes, etc.)
//   - sources: Registry of hostname extractors (Traefik, etc.)
//   - providers: Registry of DNS provider instances
func New(
	listers []workload.Lister,
	sources *source.Registry,
	providers *provider.Registry,
	opts ...Option,
) *Reconciler {
	r := &Reconciler{
		listers:           listers,
		sources:           sources,
		providers:         providers,
		config:            DefaultConfig(),
		logger:            slog.Default(),
		knownHostnames:    make(map[string]struct{}),
		hostnameProviders: make(map[string][]string),
	}

	for _, opt := range opts {
		opt(r)
	}

	// Initialize atomic fields from config
	r.syncAtomics()

	return r
}

// syncAtomics initializes the atomic bool fields from the current config values.
// Called by New() during construction. Tests that construct Reconciler structs
// directly (bypassing New) must call this after setting config.
func (r *Reconciler) syncAtomics() {
	r.enabled.Store(r.config.Enabled)
	r.dryRun.Store(r.config.DryRun)
}

// Reconcile performs a full reconciliation of DNS records.
//
// This method:
//  1. Lists all Docker workloads
//  2. Extracts hostnames from each workload's labels
//  3. Creates DNS records for new hostnames
//  4. Optionally deletes records for removed hostnames (orphan cleanup)
//
// Returns a Result containing details of all actions taken.
// The result includes timing, counts, and any errors encountered.
func (r *Reconciler) Reconcile(ctx context.Context) (*Result, error) {
	if !r.enabled.Load() {
		r.logger.Debug("reconciliation disabled, skipping")
		result := NewResult(r.dryRun.Load())
		result.Complete()
		return result, nil
	}

	// Serialize full reconciliation cycles to prevent concurrent runs from
	// interleaving (e.g., periodic timer + Docker/K8s event handler).
	r.reconcileMu.Lock()
	defer r.reconcileMu.Unlock()

	dryRun := r.dryRun.Load()

	r.logger.Info("starting reconciliation",
		slog.Bool("dry_run", dryRun),
		slog.Bool("cleanup_orphans", r.config.CleanupOrphans),
	)

	result := NewResult(dryRun)

	// Step 1: List all workloads from all platform listers
	var allWorkloads []workload.Workload
	for _, lister := range r.listers {
		workloads, err := lister.ListWorkloads(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing workloads from %s: %w", lister.Platform(), err)
		}
		r.logger.Debug("scanned workloads from platform",
			slog.String("platform", string(lister.Platform())),
			slog.Int("count", len(workloads)),
		)
		allWorkloads = append(allWorkloads, workloads...)
	}
	result.WorkloadsScanned = len(allWorkloads)

	// Step 2: Extract hostnames from each workload
	discoveredHostnames := r.extractHostnames(ctx, allWorkloads, result)

	result.HostnamesDiscovered = len(discoveredHostnames)

	r.logger.Info("hostname extraction complete",
		slog.Int("workloads", len(allWorkloads)),
		slog.Int("hostnames", len(discoveredHostnames)),
	)

	// Step 3: Build record cache for all providers (single List() call per provider).
	// Built even in dry-run mode so orphan cleanup can report accurate record counts.
	cache := newRecordCache(ctx, r.providers, r.logger)

	// Step 4: Ensure records exist for all discovered hostnames.
	// Track which providers each hostname is routed to for orphan cleanup (#51).
	currentProviderMapping := make(map[string][]string, len(discoveredHostnames))
	for _, hostname := range discoveredHostnames {
		actions := r.ensureRecord(ctx, hostname, cache)
		for _, action := range actions {
			result.AddAction(action)
			if action.Provider != "" && action.Type != ActionSkip {
				currentProviderMapping[hostname.Name] = appendUnique(currentProviderMapping[hostname.Name], action.Provider)
			}
		}
	}

	// Step 5: Orphan cleanup (if enabled)
	if r.config.CleanupOrphans {
		orphanActions := r.cleanupOrphans(ctx, discoveredHostnames, cache)
		for _, action := range orphanActions {
			result.AddAction(action)
		}
	}

	// Update known hostnames and provider mapping for next orphan check
	r.mu.Lock()
	r.knownHostnames = make(map[string]struct{}, len(discoveredHostnames))
	for name := range discoveredHostnames {
		r.knownHostnames[name] = struct{}{}
	}
	r.hostnameProviders = currentProviderMapping
	r.mu.Unlock()

	result.Complete()

	// Record metrics
	r.recordMetrics(result)

	r.logger.Info("reconciliation complete",
		slog.Int("created", result.CreatedCount()),
		slog.Int("updated", result.UpdatedCount()),
		slog.Int("deleted", result.DeletedCount()),
		slog.Int("failed", result.FailedCount()),
		slog.Int("skipped", len(result.Skipped())),
		slog.Duration("duration", result.Duration()),
	)

	return result, nil
}

// extractHostnames extracts hostnames from workloads and file sources.
// Returns a map of normalized hostname -> source.Hostname.
func (r *Reconciler) extractHostnames(ctx context.Context, workloads []workload.Workload, result *Result) map[string]*source.Hostname {
	// Track hostname -> first workload that defined it (for duplicate detection)
	// Use map to source.Hostname to preserve RecordHints from native labels
	discoveredHostnames := make(map[string]*source.Hostname)
	hostnameOrigins := make(map[string]string) // hostname -> workload name

	for _, w := range workloads {
		hostnames := r.sources.ExtractAll(ctx, w)

		// Validate hostnames and log warnings for invalid ones
		validation := hostnames.ValidateAll()
		for _, inv := range validation.Invalid {
			r.logger.Warn("skipping invalid hostname from workload",
				slog.String("workload", w.Name),
				slog.String("hostname", inv.Hostname.Name),
				slog.String("source", inv.Hostname.Source),
				slog.String("error", inv.Error.Error()),
			)
			result.HostnamesInvalid++
		}
		hostnames = validation.Valid

		if len(hostnames) > 0 {
			r.logger.Debug("extracted hostnames from workload",
				slog.String("workload", w.Name),
				slog.Int("count", len(hostnames)),
				slog.Any("hostnames", hostnames.Names()),
			)
		}

		for i := range hostnames {
			hostname := &hostnames[i]
			// Use normalized (lowercase) name as key for case-insensitive comparison (RFC 1035)
			normalizedName := hostname.NormalizedName()
			if existingWorkload, exists := hostnameOrigins[normalizedName]; exists {
				// Duplicate hostname detected
				r.logger.Warn("duplicate hostname found in multiple workloads",
					slog.String("hostname", hostname.Name),
					slog.String("first_workload", existingWorkload),
					slog.String("duplicate_workload", w.Name),
				)
				result.HostnamesDuplicate++
				// First workload wins - don't update hostnameOrigins
			} else {
				hostnameOrigins[normalizedName] = w.Name
				discoveredHostnames[normalizedName] = hostname
			}
		}
	}

	// Discover hostnames from static config files (Traefik YAML, etc.)
	fileHostnames := r.sources.DiscoverAll(ctx)
	if len(fileHostnames) > 0 {
		// Validate file-discovered hostnames
		validation := fileHostnames.ValidateAll()
		for _, inv := range validation.Invalid {
			r.logger.Warn("skipping invalid hostname from file",
				slog.String("hostname", inv.Hostname.Name),
				slog.String("source", inv.Hostname.Source),
				slog.String("router", inv.Hostname.Router),
				slog.String("error", inv.Error.Error()),
			)
			result.HostnamesInvalid++
		}
		fileHostnames = validation.Valid

		r.logger.Debug("discovered hostnames from files",
			slog.Int("count", len(fileHostnames)),
			slog.Any("hostnames", fileHostnames.Names()),
		)
		for i := range fileHostnames {
			hostname := &fileHostnames[i]
			// Use normalized (lowercase) name as key for case-insensitive comparison (RFC 1035)
			normalizedName := hostname.NormalizedName()
			if _, exists := discoveredHostnames[normalizedName]; !exists {
				discoveredHostnames[normalizedName] = hostname
			}
		}
	}

	return discoveredHostnames
}

// ReconcileHostname performs reconciliation for a single hostname.
// This is useful for event-driven updates when a specific workload changes.
// Note: This does not use the record cache since it's a single hostname operation.
func (r *Reconciler) ReconcileHostname(ctx context.Context, hostnameStr string) (*Result, error) {
	if !r.enabled.Load() {
		r.logger.Debug("reconciliation disabled, skipping hostname",
			slog.String("hostname", hostnameStr),
		)
		result := NewResult(r.dryRun.Load())
		result.Complete()
		return result, nil
	}

	r.logger.Debug("reconciling single hostname",
		slog.String("hostname", hostnameStr),
		slog.Bool("dry_run", r.dryRun.Load()),
	)

	result := NewResult(r.dryRun.Load())
	result.HostnamesDiscovered = 1

	// No cache for single-hostname reconciliation (not worth it for one query)
	// Create a hostname without hints since we only have the name
	hostname := &source.Hostname{Name: hostnameStr, Source: "api"}
	actions := r.ensureRecord(ctx, hostname, nil)
	for _, action := range actions {
		result.AddAction(action)
	}

	// Track this hostname as known (normalized for case-insensitive comparison)
	normalizedHostname := source.NormalizeHostname(hostnameStr)
	r.mu.Lock()
	r.knownHostnames[normalizedHostname] = struct{}{}
	r.mu.Unlock()

	result.Complete()
	return result, nil
}

// RemoveHostname removes DNS records for a hostname that is no longer needed.
// This is useful for event-driven cleanup when a workload is removed.
func (r *Reconciler) RemoveHostname(ctx context.Context, hostname string) (*Result, error) {
	if !r.enabled.Load() {
		result := NewResult(r.dryRun.Load())
		result.Complete()
		return result, nil
	}

	hostname = source.NormalizeHostname(hostname)

	r.logger.Debug("removing hostname",
		slog.String("hostname", hostname),
		slog.Bool("dry_run", r.isDryRun()),
	)

	result := NewResult(r.isDryRun())

	actions := r.deleteRecord(ctx, hostname)
	for _, action := range actions {
		result.AddAction(action)
	}

	// Remove from known hostnames
	r.mu.Lock()
	delete(r.knownHostnames, hostname)
	r.mu.Unlock()

	result.Complete()
	return result, nil
}

// Config returns the current reconciler configuration.
func (r *Reconciler) Config() Config {
	return r.config
}

// isDryRun returns the current dry-run state (thread-safe).
func (r *Reconciler) isDryRun() bool {
	return r.dryRun.Load()
}

// isEnabled returns the current enabled state (thread-safe).
func (r *Reconciler) isEnabled() bool {
	return r.enabled.Load()
}

// SetEnabled enables or disables reconciliation.
func (r *Reconciler) SetEnabled(enabled bool) {
	r.enabled.Store(enabled)
	r.logger.Info("reconciliation enabled state changed",
		slog.Bool("enabled", enabled),
	)
}

// SetDryRun enables or disables dry-run mode.
func (r *Reconciler) SetDryRun(dryRun bool) {
	r.dryRun.Store(dryRun)
	r.logger.Info("dry-run mode changed",
		slog.Bool("dry_run", dryRun),
	)
}

// KnownHostnames returns a copy of the currently known hostnames.
// This is primarily useful for debugging and testing.
func (r *Reconciler) KnownHostnames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	hostnames := make([]string, 0, len(r.knownHostnames))
	for h := range r.knownHostnames {
		hostnames = append(hostnames, h)
	}
	return hostnames
}

// RecoveredMetadata returns a copy of the recovered metadata map.
// This is primarily useful for debugging and testing.
func (r *Reconciler) RecoveredMetadata() map[string]map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.recoveredMetadata == nil {
		return nil
	}

	// Return a shallow copy to prevent mutation
	result := make(map[string]map[string]string, len(r.recoveredMetadata))
	for k, v := range r.recoveredMetadata {
		result[k] = v
	}
	return result
}

// getRecoveredMetadata returns recovered metadata for a hostname and clears
// it from the map. This implements "use once" semantics — after the first
// reconciliation consumes the recovered metadata, it's gone.
func (r *Reconciler) getRecoveredMetadata(hostname string) map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.recoveredMetadata == nil {
		return nil
	}

	normalized := source.NormalizeHostname(hostname)
	meta, ok := r.recoveredMetadata[normalized]
	if !ok {
		return nil
	}
	delete(r.recoveredMetadata, normalized)
	return meta
}

// RecoverOwnership scans all providers for ownership TXT records and populates
// the knownHostnames map. This should be called once on startup before the first
// reconciliation to enable orphan cleanup for records created before a restart.
//
// Only runs if both CleanupOrphans and OwnershipTracking are enabled.
func (r *Reconciler) RecoverOwnership(ctx context.Context) error {
	if !r.config.CleanupOrphans || !r.config.OwnershipTracking {
		r.logger.Debug("ownership recovery skipped",
			slog.Bool("cleanup_orphans", r.config.CleanupOrphans),
			slog.Bool("ownership_tracking", r.config.OwnershipTracking),
		)
		return nil
	}

	r.logger.Info("recovering ownership state from DNS providers")

	totalRecovered := 0
	recoveredMeta := make(map[string]map[string]string)
	var failedProviders []string

	for _, inst := range r.providers.All() {
		recovered, err := inst.RecoverOwnedHostnames(ctx)
		if err != nil {
			r.logger.Warn("failed to recover ownership from provider",
				slog.String("provider", inst.Name()),
				slog.String("error", err.Error()),
			)
			failedProviders = append(failedProviders, inst.Name())
			continue
		}

		if len(recovered) > 0 {
			r.mu.Lock()
			for _, rh := range recovered {
				// Normalize hostname for case-insensitive comparison (RFC 1035)
				normalized := source.NormalizeHostname(rh.Hostname)
				r.knownHostnames[normalized] = struct{}{}
				// Track which provider this hostname was recovered from (#51).
				// This seeds the provider mapping so orphan cleanup can target
				// the correct provider even on the first reconciliation after restart.
				r.hostnameProviders[normalized] = appendUnique(r.hostnameProviders[normalized], inst.Name())
				// Store recovered metadata if present
				if len(rh.Metadata) > 0 {
					recoveredMeta[normalized] = rh.Metadata
				}
			}
			r.mu.Unlock()

			r.logger.Info("recovered ownership records",
				slog.String("provider", inst.Name()),
				slog.Int("count", len(recovered)),
			)
			totalRecovered += len(recovered)
		}
	}

	// Store recovered metadata for use in first reconciliation cycle
	r.mu.Lock()
	r.recoveredMetadata = recoveredMeta
	r.mu.Unlock()

	r.logger.Info("ownership recovery complete",
		slog.Int("total_hostnames", totalRecovered),
		slog.Int("hostnames_with_metadata", len(recoveredMeta)),
		slog.Int("failed_providers", len(failedProviders)),
	)

	if len(failedProviders) > 0 {
		return fmt.Errorf("ownership recovery failed for %d provider(s): %v", len(failedProviders), failedProviders)
	}

	return nil
}

// recordMetrics records Prometheus metrics from a reconciliation result.
func (r *Reconciler) recordMetrics(result *Result) {
	// Record reconciliation outcome
	status := reconciliationStatusSuccess
	if result.HasErrors() {
		status = reconciliationStatusError
	}
	metrics.ReconciliationsTotal.WithLabelValues(status).Inc()

	// Record duration
	metrics.ReconciliationDuration.Observe(result.Duration().Seconds())

	// Record workload and hostname counts
	metrics.WorkloadsScanned.Set(float64(result.WorkloadsScanned))
	metrics.HostnamesDiscovered.Set(float64(result.HostnamesDiscovered))

	// Record per-action metrics
	for _, action := range result.Actions {
		switch action.Type {
		case ActionCreate:
			switch action.Status {
			case StatusSuccess:
				metrics.RecordsCreatedTotal.WithLabelValues(action.Provider).Inc()
			case StatusFailed:
				metrics.RecordsFailedTotal.WithLabelValues(action.Provider, "create").Inc()
			default:
			}
		case ActionDelete:
			switch action.Status {
			case StatusSuccess:
				metrics.RecordsDeletedTotal.WithLabelValues(action.Provider).Inc()
			case StatusFailed:
				metrics.RecordsFailedTotal.WithLabelValues(action.Provider, "delete").Inc()
			default:
			}
		case ActionUpdate:
			// Update actions are currently not emitted, but handle for completeness
			if action.Status == StatusFailed {
				metrics.RecordsFailedTotal.WithLabelValues(action.Provider, "update").Inc()
			}
		case ActionSkip:
			reason := "unknown"
			if action.Error != "" {
				reason = action.Error
			}
			// Normalize common skip reasons
			if reason == errNoMatchingProvider {
				reason = "no_provider"
			}
			metrics.RecordsSkippedTotal.WithLabelValues(reason).Inc()
		}
	}
}

// appendUnique appends value to slice if not already present.
func appendUnique(slice []string, value string) []string {
	for _, v := range slice {
		if v == value {
			return slice
		}
	}
	return append(slice, value)
}
