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
	errRecordAlreadyExists  = "record already exists"
	errRecordTypeConflict   = "record type conflict"
	errNoMatchingProvider   = "no matching provider"
	errSelfReferentialCNAME = "self-referential CNAME (target equals hostname)"
	errMultipleCNAMEMembers = "CNAME record set cannot contain multiple members"
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

	// mu protects knownHostnames, recoveredMetadata, and warnedOnce during
	// concurrent access
	mu sync.RWMutex
	// knownHostnames tracks hostnames discovered in the last reconciliation.
	// Used for orphan detection.
	knownHostnames map[string]struct{}

	// warnedOnce tracks conditions already reported at warn level, keyed by
	// "<reason>|<provider>|<hostname>", so a problem that cannot resolve on its
	// own (a self-referential CNAME, a type conflict dnsweaver may not replace)
	// is logged once rather than on every reconciliation interval. Populated
	// lazily; see warnOnce.
	warnedOnce map[string]struct{}

	// hostnameProviders tracks which provider(s) each hostname was routed to
	// in the previous reconciliation. Used by orphan cleanup to delete records
	// from the correct provider even when domain patterns have changed (#51).
	hostnameProviders map[string][]string

	// previousDesired retains the last complete provider-routed member set.
	// Providers without durable TXT ownership use it to make safe removals
	// during this process lifetime; it is deliberately empty after restart.
	previousDesired map[desiredSetKey]previousDesiredSet

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
		previousDesired:   make(map[desiredSetKey]previousDesiredSet),
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

	// Step 2: Extract source claims without collapsing distinct DNS members.
	extracted := r.extractClaims(ctx, allWorkloads, result)
	result.HostnamesDiscovered = len(extracted.Unique)

	r.logger.Info("hostname extraction complete",
		slog.Int("workloads", len(allWorkloads)),
		slog.Int("hostnames", len(extracted.Unique)),
		slog.Int("claims", len(extracted.Claims)),
		slog.Bool("complete", extracted.Complete),
	)

	// Step 3: Build record cache for all providers (single List() call per provider).
	// Built even in dry-run mode so orphan cleanup can report accurate record counts.
	cache := newRecordCache(ctx, r.providers, r.logger)

	// Step 4: Compile provider-routed desired sets, then reconcile every set
	// once. Previous in-memory sets and durable member markers contribute empty
	// desired sets so removed routes and post-restart orphans are handled by the
	// same exact-member diff.
	compiled := r.compileDesiredRecordSets(extracted.Claims)
	result.DesiredMembers = desiredMemberCount(compiled)
	for _, action := range compiled.Skipped {
		result.AddAction(action)
	}
	sets, previous := r.reconciliationSets(compiled, cache)
	nextPrevious := make(map[desiredSetKey]previousDesiredSet, len(previous)+len(compiled.Sets))
	for key, prior := range previous {
		nextPrevious[key] = prior
	}
	allowRemovals := r.config.CleanupOrphans && extracted.Complete
	if allowRemovals {
		allowRemovals = r.memberRemovalsAllowed(compiled, cache, previous)
	} else if r.config.CleanupOrphans && !extracted.Complete {
		r.logger.Warn("desired-state snapshot is partial; suppressing member removals")
	}
	for _, set := range sets {
		prior := previous[set.Key]
		actions, managed := r.reconcileDesiredSetWithState(ctx, set, cache, prior.Records, allowRemovals)
		for _, action := range actions {
			result.AddAction(action)
		}
		if !dryRun {
			if len(managed) == 0 {
				delete(nextPrevious, set.Key)
			} else {
				nextPrevious[set.Key] = previousDesiredSet{
					ProviderName: set.Instance.Name(),
					Records:      managed,
				}
			}
		}
	}

	// Update known hostnames and provider mapping for next orphan check
	r.mu.Lock()
	r.knownHostnames = make(map[string]struct{}, len(extracted.Unique))
	for name := range extracted.Unique {
		r.knownHostnames[name] = struct{}{}
	}
	r.hostnameProviders = compiled.HostnameProviders
	r.mu.Unlock()
	if extracted.Complete && !dryRun {
		r.rememberDesired(nextPrevious)
	}

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

// mergeDuplicateHostname resolves a collision between an already-selected
// hostname and a newly discovered duplicate of the same (normalized) name.
//
// Precedence is explicit and independent of source registration order: the
// candidate carrying per-record hints (RecordHints, e.g. a native dnsweaver
// record with proxied=false) wins over one without (e.g. a Traefik router rule
// that only yields a bare hostname). When neither or both carry hints, the
// existing selection is kept for determinism. Instance-selection metadata from
// the losing candidate is preserved on the winner so provider-instance
// filtering (e.g. Traefik entrypoints) still applies. See issue #159.
func mergeDuplicateHostname(existing, candidate *source.Hostname) *source.Hostname {
	winner, loser := existing, candidate
	if existing.RecordHints == nil && candidate.RecordHints != nil {
		winner, loser = candidate, existing
	}
	winner.Metadata = mergeInstanceMetadata(winner.Metadata, loser.Metadata)
	return winner
}

// mergeInstanceMetadata returns the union of two instance-selection metadata
// maps. The winner's existing keys take precedence; the loser only fills gaps.
// The winner map is returned (possibly newly allocated); neither input's
// existing values are overwritten.
func mergeInstanceMetadata(winner, loser map[string]string) map[string]string {
	if len(loser) == 0 {
		return winner
	}
	if winner == nil {
		winner = make(map[string]string, len(loser))
	}
	for k, v := range loser {
		if _, ok := winner[k]; !ok {
			winner[k] = v
		}
	}
	return winner
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
	metrics.DesiredRecordMembers.Set(float64(result.DesiredMembers))

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
