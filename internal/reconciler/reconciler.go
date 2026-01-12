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
			if existingWorkload, exists := hostnameOrigins[hostname.Name]; exists {
				// Duplicate hostname detected
				r.logger.Warn("duplicate hostname found in multiple workloads",
					slog.String("hostname", hostname.Name),
					slog.String("first_workload", existingWorkload),
					slog.String("duplicate_workload", workload.Name),
				)
				result.HostnamesDuplicate++
				// First workload wins - don't update hostnameOrigins
			} else {
				hostnameOrigins[hostname.Name] = workload.Name
				discoveredHostnames[hostname.Name] = hostname
			}
		}
	}

	// Step 2b: Discover hostnames from static config files (Traefik YAML, etc.)
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
			if _, exists := discoveredHostnames[hostname.Name]; !exists {
				discoveredHostnames[hostname.Name] = hostname
			}
		}
	}

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

	// Step 4: Orphan cleanup (if enabled)
	if r.config.CleanupOrphans {
		orphanActions := r.cleanupOrphans(ctx, discoveredHostnames, cache)
		for _, action := range orphanActions {
			result.AddAction(action)
		}
	}

	// Update known hostnames for next orphan check
	// Convert from map[string]*source.Hostname to map[string]struct{}
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

	// Track this hostname as known
	r.mu.Lock()
	r.knownHostnames[hostnameStr] = struct{}{}
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
// It uses a List+Compare approach to handle IP changes and type conflicts:
// 1. Check if record exists for hostname
// 2. If exists with same target → skip (idempotent)
// 3. If exists with different target (same type) → delete old, create new
// 4. If exists with different type → log warning, skip (don't delete manual records)
//
// When hostname has RecordHints, they override provider defaults:
// - RecordHints.Provider: route directly to named provider instead of domain matching
// - RecordHints.Type/Target/TTL: override provider instance defaults
func (r *Reconciler) ensureRecord(ctx context.Context, hostname *source.Hostname, cache *recordCache) []Action {
	var actions []Action

	// Check for explicit provider targeting via RecordHints
	if hostname.RecordHints != nil && hostname.RecordHints.Provider != "" {
		targetProvider := hostname.RecordHints.Provider
		inst, exists := r.providers.Get(targetProvider)
		if !exists {
			r.logger.Warn("explicit provider not found",
				slog.String("hostname", hostname.Name),
				slog.String("target_provider", targetProvider),
			)
			actions = append(actions, Action{
				Type:     ActionSkip,
				Status:   StatusSkipped,
				Hostname: hostname.Name,
				Error:    fmt.Sprintf("explicit provider %q not found", targetProvider),
			})
			return actions
		}
		// Route to explicit provider, bypassing domain matching
		action := r.ensureRecordForProvider(ctx, hostname, inst, cache)
		return append(actions, action)
	}

	// Standard domain-based matching
	matchingProviders := r.providers.MatchingProviders(hostname.Name)

	if len(matchingProviders) == 0 {
		r.logger.Debug("no matching providers for hostname",
			slog.String("hostname", hostname.Name),
		)
		actions = append(actions, Action{
			Type:     ActionSkip,
			Status:   StatusSkipped,
			Hostname: hostname.Name,
			Error:    "no matching provider",
		})
		return actions
	}

	for _, inst := range matchingProviders {
		action := r.ensureRecordForProvider(ctx, hostname, inst, cache)
		actions = append(actions, action)
	}

	return actions
}

// ensureRecordForProvider handles record creation for a single provider with List+Compare logic.
// When hostname has RecordHints, they override provider instance defaults.
func (r *Reconciler) ensureRecordForProvider(ctx context.Context, hostname *source.Hostname, inst *provider.ProviderInstance, cache *recordCache) Action {
	// Determine effective record type, target, and TTL
	// RecordHints override provider defaults when present
	recordType := inst.RecordType
	target := inst.Target
	ttl := inst.TTL
	var srvData *provider.SRVData

	if hints := hostname.RecordHints; hints != nil {
		if hints.Type != "" {
			recordType = provider.RecordType(hints.Type)
		}
		if hints.Target != "" {
			target = hints.Target
		}
		if hints.TTL > 0 {
			ttl = hints.TTL
		}
		// Extract SRV-specific data for SRV records
		if hints.SRV != nil {
			srvData = &provider.SRVData{
				Priority: hints.SRV.Priority,
				Weight:   hints.SRV.Weight,
				Port:     hints.SRV.Port,
			}
		}
	}

	action := Action{
		Type:       ActionCreate,
		Provider:   inst.Name(),
		Hostname:   hostname.Name,
		RecordType: string(recordType),
		Target:     target,
	}

	if r.config.DryRun {
		action.Status = StatusSuccess
		r.logger.Info("would create record (dry-run)",
			slog.String("hostname", hostname.Name),
			slog.String("provider", inst.Name()),
			slog.String("type", string(recordType)),
			slog.String("target", target),
			slog.Bool("ownership_tracking", r.config.OwnershipTracking),
			slog.Bool("has_hints", hostname.HasRecordHints()),
		)
		return action
	}

	// Step 1: Get existing records from cache (or fetch if cache unavailable)
	var existingRecords []provider.Record
	if cache != nil {
		var cached bool
		existingRecords, cached = cache.getExistingRecords(inst.Name(), hostname.Name)
		if !cached {
			// Cache miss (provider failed to load) - fall back to direct query
			r.logger.Debug("cache miss, querying provider directly",
				slog.String("hostname", hostname.Name),
				slog.String("provider", inst.Name()),
			)
			var err error
			existingRecords, err = inst.GetExistingRecords(ctx, hostname.Name)
			if err != nil {
				r.logger.Warn("failed to list existing records, proceeding with create",
					slog.String("hostname", hostname.Name),
					slog.String("provider", inst.Name()),
					slog.String("error", err.Error()),
				)
				existingRecords = nil
			}
		}
	}

	// Step 2: Analyze existing records
	var sameTypeRecords []provider.Record
	var conflictingTypeRecords []provider.Record

	for _, existing := range existingRecords {
		if existing.Type == recordType {
			sameTypeRecords = append(sameTypeRecords, existing)
		} else {
			conflictingTypeRecords = append(conflictingTypeRecords, existing)
		}
	}

	// Step 3: Handle type conflicts (A vs CNAME)
	if len(conflictingTypeRecords) > 0 {
		conflictTypes := make([]string, 0, len(conflictingTypeRecords))
		for _, rec := range conflictingTypeRecords {
			conflictTypes = append(conflictTypes, string(rec.Type))
		}
		action.Type = ActionSkip
		action.Status = StatusSkipped
		action.Error = fmt.Sprintf("type conflict: existing %v record(s) conflict with %s",
			conflictTypes, recordType)
		r.logger.Warn("skipping due to record type conflict",
			slog.String("hostname", hostname.Name),
			slog.String("provider", inst.Name()),
			slog.String("desired_type", string(recordType)),
			slog.Any("existing_types", conflictTypes),
		)
		return action
	}

	// Step 4: Check if record with correct target already exists
	// For SRV records, we need to handle multiple records with the same target but different SRV data
	var exactMatchFound bool
	var staleSrvRecords []provider.Record
	for _, existing := range sameTypeRecords {
		if existing.Target == target {
			// For SRV records, check if SRV-specific data matches
			if recordType == provider.RecordTypeSRV {
				if srvDataEquals(existing.SRV, srvData) {
					// Perfect match for SRV record
					exactMatchFound = true
				} else {
					// Same target but different SRV data - this is a stale record
					staleSrvRecords = append(staleSrvRecords, existing)
				}
			} else {
				// Non-SRV record with matching target - exact match
				exactMatchFound = true
			}
		}
	}

	// Step 4a: Delete stale SRV records (same target, different priority/weight/port)
	for _, stale := range staleSrvRecords {
		r.logger.Info("deleting stale SRV record with outdated data",
			slog.String("hostname", hostname.Name),
			slog.String("provider", inst.Name()),
			slog.String("target", stale.Target),
			slog.Int("old_priority", int(stale.SRV.Priority)),
			slog.Int("old_port", int(stale.SRV.Port)),
		)
		if err := inst.DeleteSRVRecord(ctx, hostname.Name, stale.Target, stale.SRV); err != nil {
			r.logger.Error("failed to delete stale SRV record",
				slog.String("hostname", hostname.Name),
				slog.String("provider", inst.Name()),
				slog.String("error", err.Error()),
			)
			// Continue trying other deletes
		}
	}

	// Step 4b: If exact match exists, skip creation
	if exactMatchFound {
		action.Type = ActionSkip
		action.Status = StatusSkipped
		action.Error = "record already exists"

		// Check if we already own this record
		hasOwnership := false
		if cache != nil {
			hasOwnership = cache.hasOwnershipRecord(inst.Name(), hostname.Name)
		}

		if hasOwnership {
			r.logger.Debug("record already exists with correct target",
				slog.String("hostname", hostname.Name),
				slog.String("provider", inst.Name()),
				slog.String("target", target),
			)
			r.ensureOwnershipRecord(ctx, hostname.Name, inst)
		} else if r.config.AdoptExisting {
			r.logger.Info("adopting existing record",
				slog.String("hostname", hostname.Name),
				slog.String("provider", inst.Name()),
				slog.String("target", target),
			)
			r.ensureOwnershipRecord(ctx, hostname.Name, inst)
		} else {
			r.logger.Info("existing record found, skipping adoption (set ADOPT_EXISTING=true to manage)",
				slog.String("hostname", hostname.Name),
				slog.String("provider", inst.Name()),
				slog.String("target", target),
			)
		}
		return action
	}

	// Step 5: Delete records with wrong targets (IP/hostname changed)
	for _, existing := range sameTypeRecords {
		r.logger.Info("target changed, deleting old record",
			slog.String("hostname", hostname.Name),
			slog.String("provider", inst.Name()),
			slog.String("old_target", existing.Target),
			slog.String("new_target", target),
		)
		if err := inst.DeleteRecordByTarget(ctx, hostname.Name, existing.Type, existing.Target); err != nil {
			r.logger.Error("failed to delete old record before update",
				slog.String("hostname", hostname.Name),
				slog.String("provider", inst.Name()),
				slog.String("target", existing.Target),
				slog.String("error", err.Error()),
			)
			// Continue anyway - try to create the new record
		}
	}

	// Step 6: Create the record with the desired target
	// Use CreateRecordWithValues to respect RecordHints overrides
	if err := inst.CreateRecordWithValues(ctx, hostname.Name, recordType, target, ttl, srvData); err != nil {
		// Handle conflict error (shouldn't happen after our checks, but be safe)
		if provider.IsConflict(err) {
			action.Type = ActionSkip
			action.Status = StatusSkipped
			action.Error = "record already exists"
			r.logger.Debug("record already exists, skipping",
				slog.String("hostname", hostname.Name),
				slog.String("provider", inst.Name()),
			)
			r.ensureOwnershipRecord(ctx, hostname.Name, inst)
		} else if provider.IsTypeConflict(err) {
			action.Type = ActionSkip
			action.Status = StatusSkipped
			action.Error = "record type conflict"
			r.logger.Warn("record type conflict detected",
				slog.String("hostname", hostname.Name),
				slog.String("provider", inst.Name()),
				slog.String("type", string(recordType)),
			)
		} else {
			action.Status = StatusFailed
			action.Error = err.Error()
			r.logger.Error("failed to create record",
				slog.String("hostname", hostname.Name),
				slog.String("provider", inst.Name()),
				slog.String("error", err.Error()),
			)
		}
	} else {
		// Determine if this was an update (we deleted old records) or new create
		if len(sameTypeRecords) > 0 {
			action.Type = ActionUpdate
			r.logger.Info("updated record",
				slog.String("hostname", hostname.Name),
				slog.String("provider", inst.Name()),
				slog.String("type", string(recordType)),
				slog.String("target", target),
			)
		} else {
			r.logger.Info("created record",
				slog.String("hostname", hostname.Name),
				slog.String("provider", inst.Name()),
				slog.String("type", string(recordType)),
				slog.String("target", target),
			)
		}
		action.Status = StatusSuccess
		r.ensureOwnershipRecord(ctx, hostname.Name, inst)
	}

	return action
}

// ensureOwnershipRecord creates the ownership TXT record if tracking is enabled.
func (r *Reconciler) ensureOwnershipRecord(ctx context.Context, hostname string, inst *provider.ProviderInstance) {
	if !r.config.OwnershipTracking {
		return
	}

	if err := inst.CreateOwnershipRecord(ctx, hostname); err != nil {
		// Don't warn if ownership record already exists
		if !provider.IsConflict(err) {
			r.logger.Warn("failed to create ownership record",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
				slog.String("error", err.Error()),
			)
		}
	} else {
		r.logger.Debug("created ownership record",
			slog.String("hostname", hostname),
			slog.String("provider", inst.Name()),
		)
	}
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

// deleteRecordFromCache removes DNS records using the cache to determine actual record types.
// This is used during orphan cleanup when ownership tracking is disabled.
func (r *Reconciler) deleteRecordFromCache(ctx context.Context, hostname string, cache *recordCache) []Action {
	var actions []Action

	matchingProviders := r.providers.MatchingProviders(hostname)

	for _, inst := range matchingProviders {
		if r.config.DryRun {
			action := Action{
				Type:       ActionDelete,
				Provider:   inst.Name(),
				Hostname:   hostname,
				RecordType: string(inst.RecordType),
				Target:     inst.Target,
				Status:     StatusSuccess,
			}
			r.logger.Info("would delete record (dry-run)",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
			)
			actions = append(actions, action)
			continue
		}

		// Get actual records from cache to know what types to delete
		var recordsToDelete []provider.Record
		if cache != nil {
			cachedRecords, ok := cache.getAllRecordsForHostname(inst.Name(), hostname)
			if ok && len(cachedRecords) > 0 {
				recordsToDelete = cachedRecords
			}
		}

		// If no cached records found, fall back to querying the provider
		if len(recordsToDelete) == 0 {
			allRecords, err := inst.Provider.List(ctx)
			if err != nil {
				r.logger.Warn("failed to list records for deletion",
					slog.String("hostname", hostname),
					slog.String("provider", inst.Name()),
					slog.String("error", err.Error()),
				)
				action := Action{
					Type:       ActionDelete,
					Provider:   inst.Name(),
					Hostname:   hostname,
					RecordType: string(inst.RecordType),
					Target:     inst.Target,
					Status:     StatusFailed,
					Error:      "failed to list records: " + err.Error(),
				}
				actions = append(actions, action)
				continue
			}
			for _, r := range allRecords {
				if r.Hostname == hostname {
					switch r.Type {
					case provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME, provider.RecordTypeSRV:
						recordsToDelete = append(recordsToDelete, r)
					case provider.RecordTypeTXT:
						// Skip TXT records (ownership markers)
					}
				}
			}
		}

		// Delete each record found
		for _, record := range recordsToDelete {
			action := Action{
				Type:       ActionDelete,
				Provider:   inst.Name(),
				Hostname:   hostname,
				RecordType: string(record.Type),
				Target:     record.Target,
			}

			var err error
			if record.Type == provider.RecordTypeSRV {
				err = inst.DeleteSRVRecord(ctx, hostname, record.Target, record.SRV)
			} else {
				err = inst.DeleteRecordByTarget(ctx, hostname, record.Type, record.Target)
			}

			if err != nil {
				action.Status = StatusFailed
				action.Error = err.Error()
				r.logger.Error("failed to delete record",
					slog.String("hostname", hostname),
					slog.String("provider", inst.Name()),
					slog.String("type", string(record.Type)),
					slog.String("error", err.Error()),
				)
			} else {
				action.Status = StatusSuccess
				r.logger.Info("deleted record",
					slog.String("hostname", hostname),
					slog.String("provider", inst.Name()),
					slog.String("type", string(record.Type)),
					slog.String("target", record.Target),
				)
			}
			actions = append(actions, action)
		}
	}

	return actions
}

// deleteRecordWithOwnershipCheck removes DNS records only if we own them (have ownership TXT record).
// This prevents deletion of manually-created DNS records during orphan cleanup.
// It uses the cache to determine actual record types (A, AAAA, SRV, etc.) to delete.
func (r *Reconciler) deleteRecordWithOwnershipCheck(ctx context.Context, hostname string, cache *recordCache) []Action {
	var actions []Action

	matchingProviders := r.providers.MatchingProviders(hostname)

	for _, inst := range matchingProviders {
		if r.config.DryRun {
			action := Action{
				Type:       ActionDelete,
				Provider:   inst.Name(),
				Hostname:   hostname,
				RecordType: string(inst.RecordType),
				Target:     inst.Target,
				Status:     StatusSuccess,
			}
			r.logger.Info("would delete record if owned (dry-run)",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
			)
			actions = append(actions, action)
			continue
		}

		// Check if we own this record (using cache if available)
		var hasOwnership bool
		if cache != nil {
			hasOwnership = cache.hasOwnershipRecord(inst.Name(), hostname)
		} else {
			var err error
			hasOwnership, err = inst.HasOwnershipRecord(ctx, hostname)
			if err != nil {
				r.logger.Warn("failed to check ownership record, skipping deletion",
					slog.String("hostname", hostname),
					slog.String("provider", inst.Name()),
					slog.String("error", err.Error()),
				)
				action := Action{
					Type:       ActionSkip,
					Provider:   inst.Name(),
					Hostname:   hostname,
					RecordType: string(inst.RecordType),
					Target:     inst.Target,
					Status:     StatusSkipped,
					Error:      "failed to check ownership: " + err.Error(),
				}
				actions = append(actions, action)
				continue
			}
		}

		if !hasOwnership {
			r.logger.Info("skipping orphan deletion - no ownership record (manually created?)",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
			)
			action := Action{
				Type:       ActionSkip,
				Provider:   inst.Name(),
				Hostname:   hostname,
				RecordType: string(inst.RecordType),
				Target:     inst.Target,
				Status:     StatusSkipped,
				Error:      "no ownership record - may be manually created",
			}
			actions = append(actions, action)
			continue
		}

		// We own this record - get actual records from cache to know what types to delete
		var recordsToDelete []provider.Record
		if cache != nil {
			cachedRecords, ok := cache.getAllRecordsForHostname(inst.Name(), hostname)
			if ok && len(cachedRecords) > 0 {
				recordsToDelete = cachedRecords
			}
		}

		// If no cached records found, fall back to querying the provider
		if len(recordsToDelete) == 0 {
			allRecords, err := inst.Provider.List(ctx)
			if err != nil {
				r.logger.Warn("failed to list records for deletion",
					slog.String("hostname", hostname),
					slog.String("provider", inst.Name()),
					slog.String("error", err.Error()),
				)
				action := Action{
					Type:       ActionDelete,
					Provider:   inst.Name(),
					Hostname:   hostname,
					RecordType: string(inst.RecordType),
					Target:     inst.Target,
					Status:     StatusFailed,
					Error:      "failed to list records: " + err.Error(),
				}
				actions = append(actions, action)
				continue
			}
			for _, r := range allRecords {
				if r.Hostname == hostname {
					switch r.Type {
					case provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME, provider.RecordTypeSRV:
						recordsToDelete = append(recordsToDelete, r)
					case provider.RecordTypeTXT:
						// Skip TXT records (ownership markers)
					}
				}
			}
		}

		// Delete each record found
		for _, record := range recordsToDelete {
			action := Action{
				Type:       ActionDelete,
				Provider:   inst.Name(),
				Hostname:   hostname,
				RecordType: string(record.Type),
				Target:     record.Target,
			}

			var err error
			if record.Type == provider.RecordTypeSRV {
				err = inst.DeleteSRVRecord(ctx, hostname, record.Target, record.SRV)
			} else {
				err = inst.DeleteRecordByTarget(ctx, hostname, record.Type, record.Target)
			}

			if err != nil {
				action.Status = StatusFailed
				action.Error = err.Error()
				r.logger.Error("failed to delete owned record",
					slog.String("hostname", hostname),
					slog.String("provider", inst.Name()),
					slog.String("type", string(record.Type)),
					slog.String("error", err.Error()),
				)
			} else {
				action.Status = StatusSuccess
				r.logger.Info("deleted owned record",
					slog.String("hostname", hostname),
					slog.String("provider", inst.Name()),
					slog.String("type", string(record.Type)),
					slog.String("target", record.Target),
				)
			}
			actions = append(actions, action)
		}

		// Delete ownership TXT record
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

	return actions
}

// cleanupOrphans removes records for hostnames that are no longer in any workload.
func (r *Reconciler) cleanupOrphans(ctx context.Context, currentHostnames map[string]*source.Hostname, cache *recordCache) []Action {
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
				deleteActions := r.deleteRecordWithOwnershipCheck(ctx, hostname, cache)
				actions = append(actions, deleteActions...)
			} else {
				deleteActions := r.deleteRecordFromCache(ctx, hostname, cache)
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
				r.knownHostnames[hostname] = struct{}{}
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

// srvDataEquals compares two SRVData structs for equality.
// Returns true if both are nil or have identical priority, weight, and port.
func srvDataEquals(a, b *provider.SRVData) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Priority == b.Priority && a.Weight == b.Weight && a.Port == b.Port
}
