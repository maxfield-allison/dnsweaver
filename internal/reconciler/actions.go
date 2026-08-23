// Package reconciler implements the core logic for comparing desired DNS state
// (from sources) with actual DNS state (from providers) and applying changes.
package reconciler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
)

// ensureRecord creates DNS records for a hostname in all matching providers.
// It uses a List+Compare approach to handle IP changes and type conflicts:
//  1. Check if record exists for hostname
//  2. If exists with same target → skip (idempotent), unless the provider
//     reports it would write different state for it (provider.RecordComparer,
//     e.g. Cloudflare proxied) → update in place
//  3. If exists with different target (same type) → delete old, create new
//  4. If exists with a type that cannot coexist (CNAME) → replace when allowed
//     (see canReplaceConflicting), else warn once and skip
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
		return append(actions, r.ensureRecordForProvider(ctx, hostname, inst, cache)...)
	}

	// Standard domain-based matching (with optional metadata-filter scoping
	// — see ProviderInstance.MetadataFilters and DNSWEAVER_{NAME}_ENTRYPOINTS).
	matchingProviders := r.providers.MatchingProvidersForHostname(hostname.Name, hostname.Metadata)

	if len(matchingProviders) == 0 {
		r.logger.Debug("no matching providers for hostname",
			slog.String("hostname", hostname.Name),
		)
		actions = append(actions, Action{
			Type:     ActionSkip,
			Status:   StatusSkipped,
			Hostname: hostname.Name,
			Error:    errNoMatchingProvider,
		})
		return actions
	}

	// Per-identity first-match-wins: when two instances share the same
	// backend identity (Provider.Identity()) AND RecordType, they resolve
	// to the same physical record store and would race if both wrote
	// (issue #86 \u2014 each instance's cached view is stale relative to the
	// other's writes, producing record flap). Within such a group, the
	// instance declared earliest in DNSWEAVER_INSTANCES owns the record.
	//
	// Across distinct backends, every matching instance still writes \u2014
	// publishing the same hostname into multiple actual DNS systems
	// (e.g. Cloudflare + internal Technitium) is the supported use case
	// dnsweaver was built for (issue #88).
	//
	// Identity collisions are also reported once at startup via
	// Registry.WarnDuplicateIdentities; per-reconcile logging happens at
	// DEBUG so we don't spam an already-reported config issue.
	type writeKey struct {
		Identity   provider.ProviderIdentity
		RecordType provider.RecordType
	}
	writers := make([]*provider.ProviderInstance, 0, len(matchingProviders))
	seen := make(map[writeKey]*provider.ProviderInstance, len(matchingProviders))
	for _, inst := range matchingProviders {
		k := writeKey{Identity: inst.Identity, RecordType: inst.RecordType}
		if winner, exists := seen[k]; exists {
			r.logger.Debug("skipping provider with duplicate backend identity; earlier instance owns this record",
				slog.String("hostname", hostname.Name),
				slog.String("skipped", inst.Name()),
				slog.String("winner", winner.Name()),
				slog.String("provider_type", k.Identity.Type),
				slog.String("endpoint", k.Identity.Endpoint),
				slog.String("zone", k.Identity.Zone),
				slog.String("record_type", string(k.RecordType)),
			)
			continue
		}
		seen[k] = inst
		writers = append(writers, inst)
	}

	for _, inst := range writers {
		actions = append(actions, r.ensureRecordForProvider(ctx, hostname, inst, cache)...)
	}
	return actions
}

// typesConflict reports whether a record of type desired can be created at a
// name that already holds a record of type existing.
//
// DNS only forbids one combination: a CNAME is exclusive and cannot share an
// owner name with any other data (RFC 1034 §3.6.2, RFC 2181 §10.1). Every other
// pairing is legal and common in practice — A + AAAA for dual-stack, either of
// them alongside an HTTPS/SVCB record, SRV alongside address records. Treating
// those as conflicts made dnsweaver skip its own hostnames forever once a second
// record type appeared, including the companion HTTPS record the Technitium
// provider creates itself (issue #165).
//
// Callers handle the same-type case before reaching here; TXT ownership markers
// are filtered out upstream and never reach this comparison.
func typesConflict(desired, existing provider.RecordType) bool {
	return desired == provider.RecordTypeCNAME || existing == provider.RecordTypeCNAME
}

// isSelfReferential reports whether target names the record's own hostname,
// which would create a CNAME pointing at itself (a resolution loop). This
// happens when the configured TARGET is itself a discovered hostname — e.g. a
// reverse proxy that also has a router rule of its own.
func isSelfReferential(hostname, target string, recordType provider.RecordType) bool {
	if recordType != provider.RecordTypeCNAME {
		return false
	}
	return source.NormalizeHostname(hostname) == source.NormalizeHostname(target)
}

// warnOnce logs msg at warn level the first time key is seen and at debug
// level after that. It is for conditions that cannot resolve on their own, where
// repeating the warning every interval is pure noise (issue #165).
func (r *Reconciler) warnOnce(key, msg string, attrs ...any) {
	r.mu.Lock()
	_, seen := r.warnedOnce[key]
	if !seen {
		if r.warnedOnce == nil {
			r.warnedOnce = make(map[string]struct{})
		}
		r.warnedOnce[key] = struct{}{}
	}
	r.mu.Unlock()

	if seen {
		r.logger.Debug(msg, attrs...)
		return
	}
	r.logger.Warn(msg, attrs...)
}

// warnSelfReferentialOnce reports a self-referential CNAME once per
// provider/hostname pair.
func (r *Reconciler) warnSelfReferentialOnce(hostname, providerName, target string) {
	r.warnOnce("self-cname|"+providerName+"|"+hostname,
		"skipping self-referential CNAME (target is the hostname itself)",
		slog.String("hostname", hostname),
		slog.String("provider", providerName),
		slog.String("target", target),
	)
}

// warnTypeConflictOnce reports a type conflict dnsweaver is not allowed to
// resolve, once per provider/hostname pair, with the setting that would let it.
func (r *Reconciler) warnTypeConflictOnce(hostname string, inst *provider.ProviderInstance, desiredType string, existingTypes []string) {
	msg := "skipping due to record type conflict; set DNSWEAVER_ADOPT_EXISTING=true or use authoritative mode to let dnsweaver replace the existing record(s)"
	if !inst.Mode.AllowsDelete() {
		msg = "skipping due to record type conflict; additive mode never deletes, so the existing record(s) must be removed by hand"
	}
	r.warnOnce("type-conflict|"+inst.Name()+"|"+hostname, msg,
		slog.String("hostname", hostname),
		slog.String("provider", inst.Name()),
		slog.String("mode", string(inst.Mode)),
		slog.String("desired_type", desiredType),
		slog.Any("existing_types", existingTypes),
	)
}

// canReplaceConflicting reports whether inst may delete records of another
// type that occupy hostname so the desired record can take their place.
// Additive mode never deletes. Authoritative mode owns everything in scope and
// ADOPT_EXISTING is the operator saying pre-existing records are dnsweaver's to
// manage; otherwise only a record this instance already owns (left over from
// an earlier configuration) is replaced (issue #171).
func (r *Reconciler) canReplaceConflicting(inst *provider.ProviderInstance, hostname string, cache *recordCache) bool {
	switch {
	case !inst.Mode.AllowsDelete():
		return false
	case !inst.Mode.RequiresOwnership(), r.config.AdoptExisting:
		return true
	}
	return cache != nil && cache.hasOwnershipRecord(inst.Name(), hostname, r.config.InstanceID)
}

// replaceConflictingRecords deletes the records at hostname whose type cannot
// coexist with the desired record, returning a delete action per record removed
// (or that would be removed, in dry-run). It stops at the first failure and
// returns the error, leaving the remaining records in place: creating on top of
// a half-cleared name would fail anyway, and the caller must not try.
func (r *Reconciler) replaceConflictingRecords(ctx context.Context, hostname string, inst *provider.ProviderInstance, conflicting []provider.Record) ([]Action, error) {
	actions := make([]Action, 0, len(conflicting))
	for _, rec := range conflicting {
		action := Action{
			Type:       ActionDelete,
			Provider:   inst.Name(),
			Hostname:   hostname,
			RecordType: string(rec.Type),
			Target:     rec.Target,
		}
		attrs := []any{
			slog.String("hostname", hostname),
			slog.String("provider", inst.Name()),
			slog.String("type", string(rec.Type)),
			slog.String("target", rec.Target),
		}

		if r.isDryRun() {
			action.Status = StatusSuccess
			r.logger.Info("would replace conflicting record (dry-run)", attrs...)
			actions = append(actions, action)
			continue
		}

		var err error
		if rec.Type == provider.RecordTypeSRV {
			err = inst.DeleteSRVRecord(ctx, hostname, rec.Target, rec.SRV)
		} else {
			err = inst.DeleteRecordByTarget(ctx, hostname, rec.Type, rec.Target)
		}
		// A record that vanished between List and Delete is the outcome we wanted.
		if err != nil && !errors.Is(err, provider.ErrNotFound) {
			r.logger.Error("failed to replace conflicting record", append(attrs, slog.String("error", err.Error()))...)
			return actions, fmt.Errorf("replacing conflicting %s record %s: %w", rec.Type, rec.Target, err)
		}

		action.Status = StatusSuccess
		r.logger.Info("replaced conflicting record", attrs...)
		actions = append(actions, action)
	}
	return actions, nil
}

// ensureRecordForProvider handles record creation for a single provider with List+Compare logic.
// When hostname has RecordHints, they override provider instance defaults.
// It returns a delete action for each conflicting record it replaced, followed
// by the action for the desired record itself.
func (r *Reconciler) ensureRecordForProvider(ctx context.Context, hostname *source.Hostname, inst *provider.ProviderInstance, cache *recordCache) []Action {
	// Determine effective record type, target, and TTL
	// RecordHints override provider defaults when present
	recordType := inst.RecordType
	target := inst.EffectiveTarget()
	ttl := inst.TTL
	var srvData *provider.SRVData
	var metadata map[string]string

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
		// Pass through metadata from source hints to provider
		if len(hints.Metadata) > 0 {
			metadata = hints.Metadata
		}
	}

	// desired is what the sources declare right now plus the instance defaults.
	// An existing record is compared against it and, when it differs, updated
	// to it.
	desired := provider.Record{
		Hostname: hostname.Name,
		Type:     recordType,
		Target:   target,
		TTL:      ttl,
		SRV:      srvData,
		Metadata: metadata,
	}

	// If source didn't provide metadata, check for recovered metadata from
	// ownership TXT records (populated on startup by RecoverOwnership).
	// This bridges the gap between restart and the source re-asserting metadata.
	// It informs creation and the ownership record only and is kept out of
	// desired on purpose: the TXT copy is not refreshed when a label changes,
	// so comparing an existing record against it would flip the record back to
	// its original state on every restart.
	if len(metadata) == 0 {
		if recovered := r.getRecoveredMetadata(hostname.Name); len(recovered) > 0 {
			metadata = recovered
			r.logger.Debug("using recovered metadata from ownership record",
				slog.String("hostname", hostname.Name),
				slog.Int("metadata_keys", len(recovered)),
			)
		}
	}

	action := Action{
		Type:       ActionCreate,
		Provider:   inst.Name(),
		Hostname:   hostname.Name,
		RecordType: string(recordType),
		Target:     target,
	}

	// A CNAME pointing at its own name is a resolution loop; no provider will
	// accept it and retrying every interval only produces noise.
	if isSelfReferential(hostname.Name, target, recordType) {
		action.Type = ActionSkip
		action.Status = StatusSkipped
		action.Error = errSelfReferentialCNAME
		r.warnSelfReferentialOnce(hostname.Name, inst.Name(), target)
		return []Action{action}
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
	// Records of a different type only conflict when DNS itself forbids the
	// coexistence — see typesConflict. Types that may legally share a name
	// (A + AAAA + HTTPS + SRV) are ignored here: they belong to another
	// instance, another address family, or to our own companion HTTPS record.
	var sameTypeRecords []provider.Record
	var conflictingTypeRecords []provider.Record

	for _, existing := range existingRecords {
		switch {
		case existing.Type == recordType:
			sameTypeRecords = append(sameTypeRecords, existing)
		case typesConflict(recordType, existing.Type):
			conflictingTypeRecords = append(conflictingTypeRecords, existing)
		}
	}

	// Step 3: Handle type conflicts (CNAME vs everything else). A conflicting
	// record is replaced when this instance may delete it; otherwise the desired
	// record can never be created and the hostname is skipped (issue #171).
	var replaced []Action
	if len(conflictingTypeRecords) > 0 {
		conflictTypes := make([]string, 0, len(conflictingTypeRecords))
		for _, rec := range conflictingTypeRecords {
			conflictTypes = append(conflictTypes, string(rec.Type))
		}
		if !r.canReplaceConflicting(inst, hostname.Name, cache) {
			action.Type = ActionSkip
			action.Status = StatusSkipped
			action.Error = fmt.Sprintf("type conflict: existing %v record(s) conflict with %s",
				conflictTypes, recordType)
			r.warnTypeConflictOnce(hostname.Name, inst, string(recordType), conflictTypes)
			return []Action{action}
		}

		var err error
		replaced, err = r.replaceConflictingRecords(ctx, hostname.Name, inst, conflictingTypeRecords)
		if err != nil {
			action.Status = StatusFailed
			action.Error = err.Error()
			return append(replaced, action)
		}
	}

	if r.isDryRun() {
		action.Status = StatusSuccess
		r.logger.Info("would create record (dry-run)",
			slog.String("hostname", hostname.Name),
			slog.String("provider", inst.Name()),
			slog.String("type", string(recordType)),
			slog.String("target", target),
			slog.Bool("ownership_tracking", r.config.OwnershipTracking),
			slog.Bool("has_hints", hostname.HasRecordHints()),
		)
		return append(replaced, action)
	}

	// Step 4: Check if record with correct target already exists
	// For SRV records, we need to handle multiple records with the same target but different SRV data
	var exactMatch provider.Record
	var exactMatchFound bool
	var staleSrvRecords []provider.Record
	for _, existing := range sameTypeRecords {
		if existing.Target != target {
			continue
		}
		// Same target but different SRV data - this is a stale record
		if recordType == provider.RecordTypeSRV && !srvDataEquals(existing.SRV, srvData) {
			staleSrvRecords = append(staleSrvRecords, existing)
			continue
		}
		if !exactMatchFound {
			exactMatch = existing
			exactMatchFound = true
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

	// Step 4b: If exact match exists, skip creation. The one exception is a
	// record dnsweaver manages for which the provider would still write
	// different state (Cloudflare proxied, issue #170); that falls through to
	// the in-place update in Step 5. Records dnsweaver does not manage are left
	// exactly as found.
	if exactMatchFound {
		// Check if we already own this record
		hasOwnership := false
		if cache != nil {
			hasOwnership = cache.hasOwnershipRecord(inst.Name(), hostname.Name, r.config.InstanceID)
		}
		managed := hasOwnership || r.config.AdoptExisting || !r.config.OwnershipTracking

		if !managed || !inst.RecordNeedsUpdate(exactMatch, desired) {
			action.Type = ActionSkip
			action.Status = StatusSkipped
			action.Error = errRecordAlreadyExists

			if hasOwnership {
				r.logger.Debug("record already exists with correct target",
					slog.String("hostname", hostname.Name),
					slog.String("provider", inst.Name()),
					slog.String("target", target),
				)
				r.ensureOwnershipRecord(ctx, hostname.Name, inst, metadata, cache)
			} else if r.config.AdoptExisting {
				r.logger.Info("adopting existing record",
					slog.String("hostname", hostname.Name),
					slog.String("provider", inst.Name()),
					slog.String("target", target),
				)
				r.ensureOwnershipRecord(ctx, hostname.Name, inst, metadata, cache)
			} else {
				r.logger.Info("existing record found, skipping adoption (set ADOPT_EXISTING=true to manage)",
					slog.String("hostname", hostname.Name),
					slog.String("provider", inst.Name()),
					slog.String("target", target),
				)
			}
			return append(replaced, action)
		}
	}

	// Step 5: Update or create records as needed
	// If we have existing records with wrong targets, update the first one in place
	// (duplicates with wrong targets should be cleaned up separately)
	// If the exact match only differs in provider state, update that one in place
	// If no existing records, create new ones

	if len(sameTypeRecords) > 0 {
		// Use UpdateRecord which handles native update vs fallback
		existing := sameTypeRecords[0]
		if exactMatchFound {
			existing = exactMatch
			r.logger.Info("record state changed, updating record",
				slog.String("hostname", hostname.Name),
				slog.String("provider", inst.Name()),
				slog.String("target", target),
			)
		} else {
			r.logger.Info("target changed, updating record",
				slog.String("hostname", hostname.Name),
				slog.String("provider", inst.Name()),
				slog.String("old_target", existing.Target),
				slog.String("new_target", target),
			)
		}

		if err := inst.UpdateRecord(ctx, existing, desired); err != nil {
			action.Status = StatusFailed
			action.Error = err.Error()
			r.logger.Error("failed to update record",
				slog.String("hostname", hostname.Name),
				slog.String("provider", inst.Name()),
				slog.String("error", err.Error()),
			)
			return append(replaced, action)
		}

		action.Type = ActionUpdate
		action.Status = StatusSuccess
		r.logger.Info("updated record",
			slog.String("hostname", hostname.Name),
			slog.String("provider", inst.Name()),
			slog.String("type", string(recordType)),
			slog.String("target", target),
		)
		r.ensureOwnershipRecord(ctx, hostname.Name, inst, metadata, cache)
		return append(replaced, action)
	}

	// Step 6: Create the record (no existing records)
	// Use CreateRecordWithValues to respect RecordHints overrides
	if err := inst.CreateRecordWithValues(ctx, hostname.Name, recordType, target, ttl, srvData, metadata); err != nil {
		// Handle conflict error (shouldn't happen after our checks, but be safe)
		if provider.IsConflict(err) {
			action.Type = ActionSkip
			action.Status = StatusSkipped
			action.Error = errRecordAlreadyExists
			r.logger.Debug("record already exists, skipping",
				slog.String("hostname", hostname.Name),
				slog.String("provider", inst.Name()),
			)
			r.ensureOwnershipRecord(ctx, hostname.Name, inst, metadata, cache)
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
		// This is now always a new create (updates are handled in Step 5)
		r.logger.Info("created record",
			slog.String("hostname", hostname.Name),
			slog.String("provider", inst.Name()),
			slog.String("type", string(recordType)),
			slog.String("target", target),
		)
		action.Status = StatusSuccess
		r.ensureOwnershipRecord(ctx, hostname.Name, inst, metadata, cache)
	}

	return append(replaced, action)
}

// ensureOwnershipRecord creates the ownership TXT record if tracking is enabled.
// If the cache already shows an ownership record for this instance, the create
// call is skipped to avoid generating an exception/log entry on the upstream
// DNS server (e.g. Technitium logs every "record already exists" as an error).
// See issue #87.
func (r *Reconciler) ensureOwnershipRecord(ctx context.Context, hostname string, inst *provider.ProviderInstance, metadata map[string]string, cache *recordCache) {
	if !r.config.OwnershipTracking {
		return
	}

	// Short-circuit when the cache confirms our ownership record already exists.
	// Without this, every reconciliation cycle posts a duplicate-create that
	// dnsweaver swallows but the DNS server logs as an error.
	if cache != nil && cache.hasOwnershipRecord(inst.Name(), hostname, r.config.InstanceID) {
		return
	}

	if err := inst.CreateOwnershipRecord(ctx, hostname, metadata); err != nil {
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
