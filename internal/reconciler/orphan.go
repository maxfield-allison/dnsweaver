// Package reconciler implements the core logic for comparing desired DNS state
// (from sources) with actual DNS state (from providers) and applying changes.
package reconciler

import (
	"context"
	"log/slog"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/source"
)

// cleanupOrphans removes records for hostnames that are no longer in any workload.
// Respects each provider instance's operational mode:
//   - additive: Never delete, skip this hostname for this provider
//   - managed (default): Only delete if ownership tracking confirms we own it
//   - authoritative: Delete any in-scope record without requiring ownership
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

			// Process each matching provider with its own mode
			matchingProviders := r.providers.MatchingProviders(hostname)
			for _, inst := range matchingProviders {
				deleteActions := r.deleteOrphanForProvider(ctx, hostname, inst, cache)
				actions = append(actions, deleteActions...)
			}
		}
	}

	return actions
}

// deleteOrphanForProvider handles orphan deletion for a single provider instance,
// respecting that provider's operational mode.
func (r *Reconciler) deleteOrphanForProvider(ctx context.Context, hostname string, inst *provider.ProviderInstance, cache *recordCache) []Action {
	// Check operational mode
	mode := inst.Mode
	if mode == "" {
		mode = provider.ModeManaged // default
	}

	// Additive mode: never delete
	if !mode.AllowsDelete() {
		r.logger.Info("skipping orphan deletion - additive mode",
			slog.String("hostname", hostname),
			slog.String("provider", inst.Name()),
			slog.String("mode", string(mode)),
		)
		action := Action{
			Type:       ActionSkip,
			Provider:   inst.Name(),
			Hostname:   hostname,
			RecordType: string(inst.RecordType),
			Target:     inst.Target,
			Status:     StatusSkipped,
			Error:      "additive mode - deletions disabled",
		}
		return []Action{action}
	}

	// Authoritative mode: delete without ownership check (but only supported types in scope)
	if !mode.RequiresOwnership() {
		return r.deleteAuthoritativeForProvider(ctx, hostname, inst, cache)
	}

	// Managed mode: use ownership-based deletion
	if r.config.OwnershipTracking {
		return r.deleteManagedForProvider(ctx, hostname, inst, cache)
	}

	// Managed mode without ownership tracking: use cache-based deletion
	return r.deleteCacheOnlyForProvider(ctx, hostname, inst, cache)
}

// deleteAuthoritativeForProvider deletes orphan records in authoritative mode.
// This mode deletes any in-scope record without requiring ownership, but only
// touches record types that the provider supports (via Capabilities).
func (r *Reconciler) deleteAuthoritativeForProvider(ctx context.Context, hostname string, inst *provider.ProviderInstance, cache *recordCache) []Action {
	if r.config.DryRun {
		action := Action{
			Type:       ActionDelete,
			Provider:   inst.Name(),
			Hostname:   hostname,
			RecordType: string(inst.RecordType),
			Target:     inst.Target,
			Status:     StatusSuccess,
		}
		r.logger.Info("would delete record in authoritative mode (dry-run)",
			slog.String("hostname", hostname),
			slog.String("provider", inst.Name()),
		)
		return []Action{action}
	}

	// Get capabilities to know which record types are safe to delete
	caps := inst.Provider.Capabilities()

	// Get actual records from cache
	var recordsToDelete []provider.Record
	if cache != nil {
		cachedRecords, ok := cache.getAllRecordsForHostname(inst.Name(), hostname)
		if ok && len(cachedRecords) > 0 {
			recordsToDelete = cachedRecords
		}
	}

	// If no cached records, query the provider
	if len(recordsToDelete) == 0 {
		allRecords, err := inst.Provider.List(ctx)
		if err != nil {
			r.logger.Warn("failed to list records for authoritative deletion",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
				slog.String("error", err.Error()),
			)
			return []Action{{
				Type:       ActionDelete,
				Provider:   inst.Name(),
				Hostname:   hostname,
				RecordType: string(inst.RecordType),
				Target:     inst.Target,
				Status:     StatusFailed,
				Error:      "failed to list records: " + err.Error(),
			}}
		}
		for _, rec := range allRecords {
			if rec.Hostname == hostname {
				recordsToDelete = append(recordsToDelete, rec)
			}
		}
	}

	var actions []Action
	for _, record := range recordsToDelete {
		// Skip record types we don't support
		if !caps.SupportsRecordType(record.Type) {
			r.logger.Debug("skipping unsupported record type in authoritative mode",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
				slog.String("type", string(record.Type)),
			)
			continue
		}

		// Skip ownership TXT records (we manage those separately)
		if record.Type == provider.RecordTypeTXT {
			continue
		}

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
			r.logger.Error("failed to delete record in authoritative mode",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
				slog.String("type", string(record.Type)),
				slog.String("error", err.Error()),
			)
		} else {
			action.Status = StatusSuccess
			r.logger.Info("deleted record in authoritative mode",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
				slog.String("type", string(record.Type)),
				slog.String("target", record.Target),
			)
		}
		actions = append(actions, action)
	}

	// Also delete ownership TXT record if we have one
	if r.config.OwnershipTracking {
		if ownerErr := inst.DeleteOwnershipRecord(ctx, hostname); ownerErr != nil {
			r.logger.Debug("failed to delete ownership record (may not exist)",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
			)
		}
	}

	return actions
}

// deleteManagedForProvider deletes orphan records in managed mode with ownership tracking.
// Only deletes records that have an ownership TXT marker.
func (r *Reconciler) deleteManagedForProvider(ctx context.Context, hostname string, inst *provider.ProviderInstance, cache *recordCache) []Action {
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
		return []Action{action}
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
			return []Action{{
				Type:       ActionSkip,
				Provider:   inst.Name(),
				Hostname:   hostname,
				RecordType: string(inst.RecordType),
				Target:     inst.Target,
				Status:     StatusSkipped,
				Error:      "failed to check ownership: " + err.Error(),
			}}
		}
	}

	if !hasOwnership {
		r.logger.Info("skipping orphan deletion - no ownership record (manually created?)",
			slog.String("hostname", hostname),
			slog.String("provider", inst.Name()),
		)
		return []Action{{
			Type:       ActionSkip,
			Provider:   inst.Name(),
			Hostname:   hostname,
			RecordType: string(inst.RecordType),
			Target:     inst.Target,
			Status:     StatusSkipped,
			Error:      "no ownership record - may be manually created",
		}}
	}

	// We own this record - get actual records from cache
	var recordsToDelete []provider.Record
	if cache != nil {
		cachedRecords, ok := cache.getAllRecordsForHostname(inst.Name(), hostname)
		if ok && len(cachedRecords) > 0 {
			recordsToDelete = cachedRecords
		}
	}

	// If no cached records, query the provider
	if len(recordsToDelete) == 0 {
		allRecords, err := inst.Provider.List(ctx)
		if err != nil {
			r.logger.Warn("failed to list records for managed deletion",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
				slog.String("error", err.Error()),
			)
			return []Action{{
				Type:       ActionDelete,
				Provider:   inst.Name(),
				Hostname:   hostname,
				RecordType: string(inst.RecordType),
				Target:     inst.Target,
				Status:     StatusFailed,
				Error:      "failed to list records: " + err.Error(),
			}}
		}
		for _, rec := range allRecords {
			if rec.Hostname == hostname {
				switch rec.Type {
				case provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME, provider.RecordTypeSRV:
					recordsToDelete = append(recordsToDelete, rec)
				case provider.RecordTypeTXT:
					// Skip TXT records (ownership markers handled separately)
				}
			}
		}
	}

	var actions []Action
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

	return actions
}

// deleteCacheOnlyForProvider deletes orphan records in managed mode without ownership tracking.
// Uses the cache to determine what record types exist.
func (r *Reconciler) deleteCacheOnlyForProvider(ctx context.Context, hostname string, inst *provider.ProviderInstance, cache *recordCache) []Action {
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
		return []Action{action}
	}

	// Get actual records from cache
	var recordsToDelete []provider.Record
	if cache != nil {
		cachedRecords, ok := cache.getAllRecordsForHostname(inst.Name(), hostname)
		if ok && len(cachedRecords) > 0 {
			recordsToDelete = cachedRecords
		}
	}

	// If no cached records, query the provider
	if len(recordsToDelete) == 0 {
		allRecords, err := inst.Provider.List(ctx)
		if err != nil {
			r.logger.Warn("failed to list records for deletion",
				slog.String("hostname", hostname),
				slog.String("provider", inst.Name()),
				slog.String("error", err.Error()),
			)
			return []Action{{
				Type:       ActionDelete,
				Provider:   inst.Name(),
				Hostname:   hostname,
				RecordType: string(inst.RecordType),
				Target:     inst.Target,
				Status:     StatusFailed,
				Error:      "failed to list records: " + err.Error(),
			}}
		}
		for _, rec := range allRecords {
			if rec.Hostname == hostname {
				switch rec.Type {
				case provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME, provider.RecordTypeSRV:
					recordsToDelete = append(recordsToDelete, rec)
				case provider.RecordTypeTXT:
					// Skip TXT records
				}
			}
		}
	}

	var actions []Action
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

// deleteFromCache removes DNS records using the cache to determine actual record types.
// This is used during orphan cleanup when ownership tracking is disabled.
// Renamed from deleteRecordFromCache for clarity.
func (r *Reconciler) deleteFromCache(ctx context.Context, hostname string, cache *recordCache) []Action {
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
			for _, rec := range allRecords {
				if rec.Hostname == hostname {
					switch rec.Type {
					case provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME, provider.RecordTypeSRV:
						recordsToDelete = append(recordsToDelete, rec)
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

// deleteWithOwnership removes DNS records only if we own them (have ownership TXT record).
// This prevents deletion of manually-created DNS records during orphan cleanup.
// It uses the cache to determine actual record types (A, AAAA, SRV, etc.) to delete.
// Renamed from deleteRecordWithOwnershipCheck for clarity.
func (r *Reconciler) deleteWithOwnership(ctx context.Context, hostname string, cache *recordCache) []Action {
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
			for _, rec := range allRecords {
				if rec.Hostname == hostname {
					switch rec.Type {
					case provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME, provider.RecordTypeSRV:
						recordsToDelete = append(recordsToDelete, rec)
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
