// Package reconciler implements the core logic for comparing desired DNS state
// (from sources) with actual DNS state (from providers) and applying changes.
package reconciler

import (
	"context"
	"fmt"
	"log/slog"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/source"
)

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
		action.Error = errRecordAlreadyExists

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
			action.Error = errRecordAlreadyExists
			r.logger.Debug("record already exists, skipping",
				slog.String("hostname", hostname.Name),
				slog.String("provider", inst.Name()),
			)
			r.ensureOwnershipRecord(ctx, hostname.Name, inst)
		} else if provider.IsTypeConflict(err) {
			action.Type = ActionSkip
			action.Status = StatusSkipped
			action.Error = errRecordTypeConflict
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
