// Package reconciler implements the core logic for comparing desired DNS state
// (from sources) with actual DNS state (from providers) and applying changes.
package reconciler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"gitlab.bluewillows.net/root/dnsweaver/internal/docker"
	"gitlab.bluewillows.net/root/dnsweaver/internal/metrics"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/source"
)

// Common error messages used in reconciliation actions.
const (
	errRecordAlreadyExists = "record already exists"
	errRecordTypeConflict  = "record type conflict"
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

// WorkloadLister is the interface required for listing Docker workloads.
// This abstraction enables testing without a real Docker connection.
type WorkloadLister interface {
	// ListWorkloads returns all workloads (services in Swarm, containers in standalone).
	ListWorkloads(ctx context.Context) ([]docker.Workload, error)
	// Mode returns the Docker operating mode (swarm or standalone).
	Mode() docker.Mode
}

// Reconciler coordinates DNS record synchronization between sources and providers.
//
// The reconciler:
//  1. Scans Docker workloads (services in Swarm, containers in standalone)
//  2. Extracts hostnames from workload labels using registered sources
//  3. For each hostname, finds matching provider(s) based on domain patterns
//  4. Ensures DNS records exist for discovered hostnames
//  5. Optionally removes orphan records (hostnames no longer in workloads)
type Reconciler struct {
	docker    WorkloadLister
	sources   *source.Registry
	providers *provider.Registry
	config    Config
	logger    *slog.Logger

	// mu protects knownHostnames during concurrent access
	mu sync.RWMutex
	// knownHostnames tracks hostnames discovered in the last reconciliation.
	// Used for orphan detection.
	knownHostnames map[string]struct{}
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
//   - docker: WorkloadLister for listing workloads (typically *docker.Client)
//   - sources: Registry of hostname extractors (Traefik, etc.)
//   - providers: Registry of DNS provider instances
func New(
	dockerClient WorkloadLister,
	sources *source.Registry,
	providers *provider.Registry,
	opts ...Option,
) *Reconciler {
	r := &Reconciler{
		docker:         dockerClient,
		sources:        sources,
		providers:      providers,
		config:         DefaultConfig(),
		logger:         slog.Default(),
		knownHostnames: make(map[string]struct{}),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
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
	if !r.config.Enabled {
		r.logger.Debug("reconciliation disabled, skipping")
		result := NewResult(r.config.DryRun)
		result.Complete()
		return result, nil
	}

	r.logger.Info("starting reconciliation",
		slog.Bool("dry_run", r.config.DryRun),
		slog.Bool("cleanup_orphans", r.config.CleanupOrphans),
	)

	result := NewResult(r.config.DryRun)

	// Step 1: List all workloads
	workloads, err := r.docker.ListWorkloads(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing workloads: %w", err)
	}
	result.WorkloadsScanned = len(workloads)

	r.logger.Debug("scanned workloads",
		slog.Int("count", len(workloads)),
		slog.String("mode", r.docker.Mode().String()),
	)

	// Step 2: Extract hostnames from each workload
	discoveredHostnames := r.extractHostnames(ctx, workloads, result)

	result.HostnamesDiscovered = len(discoveredHostnames)

	r.logger.Info("hostname extraction complete",
		slog.Int("workloads", len(workloads)),
		slog.Int("hostnames", len(discoveredHostnames)),
	)

	// Step 3: Build record cache for all providers (single List() call per provider)
	var cache *recordCache
	if !r.config.DryRun {
		cache = newRecordCache(ctx, r.providers, r.logger)
	}

	// Step 4: Ensure records exist for all discovered hostnames
	for _, hostname := range discoveredHostnames {
		actions := r.ensureRecord(ctx, hostname, cache)
		for _, action := range actions {
			result.AddAction(action)
		}
	}

	// Step 5: Orphan cleanup (if enabled)
	if r.config.CleanupOrphans {
		orphanActions := r.cleanupOrphans(ctx, discoveredHostnames, cache)
		for _, action := range orphanActions {
			result.AddAction(action)
		}
	}

	// Update known hostnames for next orphan check
	r.mu.Lock()
	r.knownHostnames = make(map[string]struct{}, len(discoveredHostnames))
	for name := range discoveredHostnames {
		r.knownHostnames[name] = struct{}{}
	}
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
func (r *Reconciler) extractHostnames(ctx context.Context, workloads []docker.Workload, result *Result) map[string]*source.Hostname {
	// Track hostname -> first workload that defined it (for duplicate detection)
	// Use map to source.Hostname to preserve RecordHints from native labels
	discoveredHostnames := make(map[string]*source.Hostname)
	hostnameOrigins := make(map[string]string) // hostname -> workload name

	for _, workload := range workloads {
		hostnames := r.sources.ExtractAll(ctx, workload.Labels)

		// Validate hostnames and log warnings for invalid ones
		validation := hostnames.ValidateAll()
		for _, inv := range validation.Invalid {
			r.logger.Warn("skipping invalid hostname from workload",
				slog.String("workload", workload.Name),
				slog.String("hostname", inv.Hostname.Name),
				slog.String("source", inv.Hostname.Source),
				slog.String("error", inv.Error.Error()),
			)
			result.HostnamesInvalid++
		}
		hostnames = validation.Valid

		if len(hostnames) > 0 {
			r.logger.Debug("extracted hostnames from workload",
				slog.String("workload", workload.Name),
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
					slog.String("duplicate_workload", workload.Name),
				)
				result.HostnamesDuplicate++
				// First workload wins - don't update hostnameOrigins
			} else {
				hostnameOrigins[normalizedName] = workload.Name
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
	if !r.config.Enabled {
		r.logger.Debug("reconciliation disabled, skipping hostname",
			slog.String("hostname", hostnameStr),
		)
		result := NewResult(r.config.DryRun)
		result.Complete()
		return result, nil
	}

	r.logger.Debug("reconciling single hostname",
		slog.String("hostname", hostnameStr),
		slog.Bool("dry_run", r.config.DryRun),
	)

	result := NewResult(r.config.DryRun)
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
	if !r.config.Enabled {
		result := NewResult(r.config.DryRun)
		result.Complete()
		return result, nil
	}

	r.logger.Debug("removing hostname",
		slog.String("hostname", hostname),
		slog.Bool("dry_run", r.config.DryRun),
	)

	result := NewResult(r.config.DryRun)

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

// SetEnabled enables or disables reconciliation.
func (r *Reconciler) SetEnabled(enabled bool) {
	r.config.Enabled = enabled
	r.logger.Info("reconciliation enabled state changed",
		slog.Bool("enabled", enabled),
	)
}

// SetDryRun enables or disables dry-run mode.
func (r *Reconciler) SetDryRun(dryRun bool) {
	r.config.DryRun = dryRun
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
	for _, inst := range r.providers.All() {
		hostnames, err := inst.RecoverOwnedHostnames(ctx)
		if err != nil {
			r.logger.Warn("failed to recover ownership from provider",
				slog.String("provider", inst.Name()),
				slog.String("error", err.Error()),
			)
			continue
		}

		if len(hostnames) > 0 {
			r.mu.Lock()
			for _, hostname := range hostnames {
				// Normalize hostname for case-insensitive comparison (RFC 1035)
				normalized := source.NormalizeHostname(hostname)
				r.knownHostnames[normalized] = struct{}{}
			}
			r.mu.Unlock()

			r.logger.Info("recovered ownership records",
				slog.String("provider", inst.Name()),
				slog.Int("count", len(hostnames)),
			)
			totalRecovered += len(hostnames)
		}
	}

	r.logger.Info("ownership recovery complete",
		slog.Int("total_hostnames", totalRecovered),
	)

	return nil
}

// recordMetrics records Prometheus metrics from a reconciliation result.
func (r *Reconciler) recordMetrics(result *Result) {
	// Record reconciliation outcome
	status := "success"
	if result.HasErrors() {
		status = "error"
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
			if action.Status == StatusSuccess {
				metrics.RecordsCreatedTotal.WithLabelValues(action.Provider).Inc()
			} else if action.Status == StatusFailed {
				metrics.RecordsFailedTotal.WithLabelValues(action.Provider, "create").Inc()
			}
		case ActionDelete:
			if action.Status == StatusSuccess {
				metrics.RecordsDeletedTotal.WithLabelValues(action.Provider).Inc()
			} else if action.Status == StatusFailed {
				metrics.RecordsFailedTotal.WithLabelValues(action.Provider, "delete").Inc()
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
			if reason == "no matching provider" {
				reason = "no_provider"
			}
			metrics.RecordsSkippedTotal.WithLabelValues(reason).Inc()
		}
	}
}
