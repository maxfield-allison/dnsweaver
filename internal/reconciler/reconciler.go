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
		ReconcileInterval: 60 * time.Second,
		Enabled:           true,
	}
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
	docker    *docker.Client
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
//   - docker: Client for listing workloads
//   - sources: Registry of hostname extractors (Traefik, etc.)
//   - providers: Registry of DNS provider instances
func New(
	dockerClient *docker.Client,
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
	discoveredHostnames := make(map[string]struct{})

	for _, workload := range workloads {
		hostnames := r.sources.ExtractAll(ctx, workload.Labels)

		if len(hostnames) > 0 {
			r.logger.Debug("extracted hostnames from workload",
				slog.String("workload", workload.Name),
				slog.Int("count", len(hostnames)),
				slog.Any("hostnames", hostnames.Names()),
			)
		}

		for _, hostname := range hostnames {
			discoveredHostnames[hostname.Name] = struct{}{}
		}
	}

	// Step 2b: Discover hostnames from static config files (Traefik YAML, etc.)
	fileHostnames := r.sources.DiscoverAll(ctx)
	if len(fileHostnames) > 0 {
		r.logger.Debug("discovered hostnames from files",
			slog.Int("count", len(fileHostnames)),
			slog.Any("hostnames", fileHostnames.Names()),
		)
		for _, hostname := range fileHostnames {
			discoveredHostnames[hostname.Name] = struct{}{}
		}
	}

	result.HostnamesDiscovered = len(discoveredHostnames)

	r.logger.Info("hostname extraction complete",
		slog.Int("workloads", len(workloads)),
		slog.Int("hostnames", len(discoveredHostnames)),
	)

	// Step 3: Ensure records exist for all discovered hostnames
	for hostname := range discoveredHostnames {
		actions := r.ensureRecord(ctx, hostname)
		for _, action := range actions {
			result.AddAction(action)
		}
	}

	// Step 4: Orphan cleanup (if enabled)
	if r.config.CleanupOrphans {
		orphanActions := r.cleanupOrphans(ctx, discoveredHostnames)
		for _, action := range orphanActions {
			result.AddAction(action)
		}
	}

	// Update known hostnames for next orphan check
	r.mu.Lock()
	r.knownHostnames = discoveredHostnames
	r.mu.Unlock()

	result.Complete()

	// Record metrics
	r.recordMetrics(result)

	r.logger.Info("reconciliation complete",
		slog.Int("created", result.CreatedCount()),
		slog.Int("deleted", result.DeletedCount()),
		slog.Int("failed", result.FailedCount()),
		slog.Int("skipped", len(result.Skipped())),
		slog.Duration("duration", result.Duration()),
	)

	return result, nil
}

// ReconcileHostname performs reconciliation for a single hostname.
// This is useful for event-driven updates when a specific workload changes.
func (r *Reconciler) ReconcileHostname(ctx context.Context, hostname string) (*Result, error) {
	if !r.config.Enabled {
		r.logger.Debug("reconciliation disabled, skipping hostname",
			slog.String("hostname", hostname),
		)
		result := NewResult(r.config.DryRun)
		result.Complete()
		return result, nil
	}

	r.logger.Debug("reconciling single hostname",
		slog.String("hostname", hostname),
		slog.Bool("dry_run", r.config.DryRun),
	)

	result := NewResult(r.config.DryRun)
	result.HostnamesDiscovered = 1

	actions := r.ensureRecord(ctx, hostname)
	for _, action := range actions {
		result.AddAction(action)
	}

	// Track this hostname as known
	r.mu.Lock()
	r.knownHostnames[hostname] = struct{}{}
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

// ensureRecord creates DNS records for a hostname in all matching providers.
func (r *Reconciler) ensureRecord(ctx context.Context, hostname string) []Action {
	var actions []Action

	matchingProviders := r.providers.MatchingProviders(hostname)

	if len(matchingProviders) == 0 {
		r.logger.Debug("no matching providers for hostname",
			slog.String("hostname", hostname),
		)
		actions = append(actions, Action{
			Type:     ActionSkip,
			Status:   StatusSkipped,
			Hostname: hostname,
			Error:    "no matching provider",
		})
		return actions
	}

	for _, inst := range matchingProviders {
		action := Action{
			Type:       ActionCreate,
			Provider:   inst.Name(),
			Hostname:   hostname,
			RecordType: string(inst.RecordType),
			Target:     inst.Target,
		}

		if r.config.DryRun {
			action.Status = StatusSuccess
			r.logger.Info("would create record (dry-run)",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
				slog.String("type", string(inst.RecordType)),
				slog.String("target", inst.Target),
				slog.Bool("ownership_tracking", r.config.OwnershipTracking),
			)
		} else {
			err := inst.CreateRecord(ctx, hostname)
			if err != nil {
				// If record already exists, treat as skip (idempotent)
				if provider.IsConflict(err) {
					action.Type = ActionSkip
					action.Status = StatusSkipped
					action.Error = "record already exists"
					r.logger.Debug("record already exists, skipping",
						slog.String("hostname", hostname),
						slog.String("provider", inst.Name()),
					)
					// Still ensure ownership record exists for idempotency
					if r.config.OwnershipTracking {
						if ownerErr := inst.CreateOwnershipRecord(ctx, hostname); ownerErr != nil {
							r.logger.Warn("failed to create ownership record",
								slog.String("hostname", hostname),
								slog.String("provider", inst.Name()),
								slog.String("error", ownerErr.Error()),
							)
						}
					}
				} else {
					action.Status = StatusFailed
					action.Error = err.Error()
					r.logger.Error("failed to create record",
						slog.String("hostname", hostname),
						slog.String("provider", inst.Name()),
						slog.String("error", err.Error()),
					)
				}
			} else {
				action.Status = StatusSuccess
				r.logger.Info("created record",
					slog.String("hostname", hostname),
					slog.String("provider", inst.Name()),
					slog.String("type", string(inst.RecordType)),
					slog.String("target", inst.Target),
				)

				// Create ownership TXT record if tracking is enabled
				if r.config.OwnershipTracking {
					if ownerErr := inst.CreateOwnershipRecord(ctx, hostname); ownerErr != nil {
						r.logger.Warn("failed to create ownership record",
							slog.String("hostname", hostname),
							slog.String("provider", inst.Name()),
							slog.String("error", ownerErr.Error()),
						)
					} else {
						r.logger.Debug("created ownership record",
							slog.String("hostname", hostname),
							slog.String("provider", inst.Name()),
						)
					}
				}
			}
		}

		actions = append(actions, action)
	}

	return actions
}

// deleteRecord removes DNS records for a hostname from all matching providers.
// Also deletes ownership TXT records if ownership tracking is enabled.
func (r *Reconciler) deleteRecord(ctx context.Context, hostname string) []Action {
	var actions []Action

	matchingProviders := r.providers.MatchingProviders(hostname)

	for _, inst := range matchingProviders {
		action := Action{
			Type:       ActionDelete,
			Provider:   inst.Name(),
			Hostname:   hostname,
			RecordType: string(inst.RecordType),
			Target:     inst.Target,
		}

		if r.config.DryRun {
			action.Status = StatusSuccess
			r.logger.Info("would delete record (dry-run)",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
				slog.Bool("ownership_tracking", r.config.OwnershipTracking),
			)
		} else {
			err := inst.DeleteRecord(ctx, hostname)
			if err != nil {
				action.Status = StatusFailed
				action.Error = err.Error()
				r.logger.Error("failed to delete record",
					slog.String("hostname", hostname),
					slog.String("provider", inst.Name()),
					slog.String("error", err.Error()),
				)
			} else {
				action.Status = StatusSuccess
				r.logger.Info("deleted record",
					slog.String("hostname", hostname),
					slog.String("provider", inst.Name()),
				)

				// Also delete ownership TXT record if tracking is enabled
				if r.config.OwnershipTracking {
					if ownerErr := inst.DeleteOwnershipRecord(ctx, hostname); ownerErr != nil {
						r.logger.Warn("failed to delete ownership record",
							slog.String("hostname", hostname),
							slog.String("provider", inst.Name()),
							slog.String("error", ownerErr.Error()),
						)
					} else {
						r.logger.Debug("deleted ownership record",
							slog.String("hostname", hostname),
							slog.String("provider", inst.Name()),
						)
					}
				}
			}
		}

		actions = append(actions, action)
	}

	return actions
}

// deleteRecordWithOwnershipCheck removes DNS records only if we own them (have ownership TXT record).
// This prevents deletion of manually-created DNS records during orphan cleanup.
func (r *Reconciler) deleteRecordWithOwnershipCheck(ctx context.Context, hostname string) []Action {
	var actions []Action

	matchingProviders := r.providers.MatchingProviders(hostname)

	for _, inst := range matchingProviders {
		action := Action{
			Type:       ActionDelete,
			Provider:   inst.Name(),
			Hostname:   hostname,
			RecordType: string(inst.RecordType),
			Target:     inst.Target,
		}

		if r.config.DryRun {
			action.Status = StatusSuccess
			r.logger.Info("would delete record if owned (dry-run)",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
			)
			actions = append(actions, action)
			continue
		}

		// Check if we own this record
		hasOwnership, err := inst.HasOwnershipRecord(ctx, hostname)
		if err != nil {
			r.logger.Warn("failed to check ownership record, skipping deletion",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
				slog.String("error", err.Error()),
			)
			action.Type = ActionSkip
			action.Status = StatusSkipped
			action.Error = "failed to check ownership: " + err.Error()
			actions = append(actions, action)
			continue
		}

		if !hasOwnership {
			r.logger.Info("skipping orphan deletion - no ownership record (manually created?)",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
			)
			action.Type = ActionSkip
			action.Status = StatusSkipped
			action.Error = "no ownership record - may be manually created"
			actions = append(actions, action)
			continue
		}

		// We own this record, safe to delete
		err = inst.DeleteRecord(ctx, hostname)
		if err != nil {
			action.Status = StatusFailed
			action.Error = err.Error()
			r.logger.Error("failed to delete owned record",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
				slog.String("error", err.Error()),
			)
		} else {
			action.Status = StatusSuccess
			r.logger.Info("deleted owned record",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
			)

			// Also delete ownership TXT record
			if ownerErr := inst.DeleteOwnershipRecord(ctx, hostname); ownerErr != nil {
				r.logger.Warn("failed to delete ownership record",
					slog.String("hostname", hostname),
					slog.String("provider", inst.Name()),
					slog.String("error", ownerErr.Error()),
				)
			} else {
				r.logger.Debug("deleted ownership record",
					slog.String("hostname", hostname),
					slog.String("provider", inst.Name()),
				)
			}
		}

		actions = append(actions, action)
	}

	return actions
}

// cleanupOrphans removes records for hostnames that are no longer in any workload.
func (r *Reconciler) cleanupOrphans(ctx context.Context, currentHostnames map[string]struct{}) []Action {
	var actions []Action

	r.mu.RLock()
	previousHostnames := make(map[string]struct{}, len(r.knownHostnames))
	for h := range r.knownHostnames {
		previousHostnames[h] = struct{}{}
	}
	r.mu.RUnlock()

	// Find hostnames that were known before but are no longer present
	for hostname := range previousHostnames {
		if _, stillExists := currentHostnames[hostname]; !stillExists {
			r.logger.Info("detected orphan hostname",
				slog.String("hostname", hostname),
			)

			// If ownership tracking is enabled, only delete if we own the record
			if r.config.OwnershipTracking {
				deleteActions := r.deleteRecordWithOwnershipCheck(ctx, hostname)
				actions = append(actions, deleteActions...)
			} else {
				deleteActions := r.deleteRecord(ctx, hostname)
				actions = append(actions, deleteActions...)
			}
		}
	}

	return actions
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
