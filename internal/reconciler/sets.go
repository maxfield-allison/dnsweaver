package reconciler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

func (r *Reconciler) reconcileDesiredSet(ctx context.Context, set *desiredRecordSet, cache *recordCache, previous []provider.Record) []Action {
	actions, _ := r.reconcileDesiredSetWithState(ctx, set, cache, previous, true)
	return actions
}

// reconcileDesiredSetWithState also returns the members this process can
// still prove it manages. That state is the fallback for providers that
// cannot persist ownership TXT records; it must describe successful writes
// and adopted records, not merely whatever appeared in desired state.
func (r *Reconciler) reconcileDesiredSetWithState(ctx context.Context, set *desiredRecordSet, cache *recordCache, previous []provider.Record, allowRemovals bool) ([]Action, []provider.Record) {
	instance := set.Instance
	managed := append([]provider.Record(nil), previous...)
	if !instance.Provider.Capabilities().SupportsRecordType(set.Key.RecordType) {
		actions := make([]Action, 0, len(set.Members))
		for _, member := range set.Members {
			actions = append(actions, memberAction(ActionSkip, StatusSkipped, instance, member.Record,
				fmt.Sprintf("record type %s is not supported by provider", member.Record.Type)))
		}
		return actions, managed
	}
	if cache == nil || !cache.providerAvailable(instance.Name()) {
		actions := make([]Action, 0, len(set.Members))
		for _, member := range set.Members {
			actions = append(actions, memberAction(ActionCreate, StatusFailed, instance, member.Record,
				"provider record snapshot unavailable"))
		}
		return actions, managed
	}

	existingRecords, _ := cache.getExistingRecords(instance.Name(), set.Key.Hostname)
	var sameType, conflicting []provider.Record
	for _, existing := range existingRecords {
		switch {
		case existing.Type == set.Key.RecordType:
			sameType = append(sameType, existing)
		case typesConflict(set.Key.RecordType, existing.Type):
			conflicting = append(conflicting, existing)
		}
	}

	actions := make([]Action, 0, len(set.Members)+len(sameType)+len(conflicting))
	if len(conflicting) > 0 {
		adopt := setAllowsAdoption(r, set)
		for _, record := range conflicting {
			if !allowRemovals || !r.mayDeleteMember(instance, record, cache, previous, adopt) {
				for _, member := range set.Members {
					actions = append(actions, memberAction(ActionSkip, StatusSkipped, instance, member.Record, errRecordTypeConflict))
				}
				return actions, managed
			}
		}
		for _, record := range conflicting {
			action, ok := r.deleteSetMember(ctx, instance, record, cache)
			actions = append(actions, action)
			if !ok {
				return actions, managed
			}
			if !r.isDryRun() {
				managed = forgetRecordMember(managed, record)
			}
		}
	}

	matchedExisting := make([]bool, len(sameType))
	_, hasLegacyOwnership := cache.legacyOwnershipRecord(instance.Name(), set.Key.Hostname, instance.InstanceID)
	legacyUpgradeComplete := hasLegacyOwnership && instance.Provider.Capabilities().SupportsOwnershipTXT
	desiredWritesSucceeded := true

	// CNAME is single-valued in practice. A target change must update the one
	// exact existing CNAME instead of trying to create a second member first.
	// Multiple desired or existing CNAMEs are ambiguous and are left alone.
	if set.Key.RecordType == provider.RecordTypeCNAME {
		if len(set.Members) > 1 || len(sameType) > 1 {
			for _, member := range set.Members {
				actions = append(actions, memberAction(ActionSkip, StatusSkipped, instance, member.Record, errMultipleCNAMEMembers))
			}
			return actions, managed
		}
		if len(set.Members) == 1 && len(sameType) == 1 && !provider.SameRecordMember(sameType[0], set.Members[0].Record) {
			return r.updateSingleValueMember(ctx, set, sameType[0], cache, previous, actions, managed, hasLegacyOwnership)
		}
	}

	for _, desiredMember := range set.Members {
		record := desiredMember.Record
		existingIndex := -1
		for i := range sameType {
			if !matchedExisting[i] && provider.SameRecordMember(sameType[i], record) {
				existingIndex = i
				break
			}
		}

		if existingIndex < 0 {
			action := memberAction(ActionCreate, StatusSuccess, instance, record, "")
			if r.isDryRun() {
				actions = append(actions, action)
				continue
			}
			if err := instance.CreateRecordWithValues(ctx, record.Hostname, record.Type, record.Target, record.TTL, record.SRV, record.Metadata); err != nil {
				action.Status = StatusFailed
				action.Error = err.Error()
				legacyUpgradeComplete = false
				desiredWritesSucceeded = false
			} else {
				managed = rememberRecordMember(managed, record)
				if err := r.ensureMemberOwnership(ctx, instance, record, cache); err != nil {
					r.logger.Warn("failed to create member ownership record",
						slog.String("provider", instance.Name()),
						slog.String("hostname", record.Hostname),
						slog.String("target", record.Target),
						slog.String("error", err.Error()),
					)
					legacyUpgradeComplete = false
					desiredWritesSucceeded = false
				}
			}
			actions = append(actions, action)
			continue
		}

		matchedExisting[existingIndex] = true
		existing := sameType[existingIndex]
		_, hasMemberOwnership := cache.memberOwnershipRecord(instance.Name(), record, instance.InstanceID)
		adopt := r.effectiveAdoptExisting(desiredMember.Claim, instance)
		mayManage := !r.config.OwnershipTracking || !instance.Mode.RequiresOwnership() ||
			hasMemberOwnership || adopt || hasLegacyOwnership
		if mayManage && !r.isDryRun() {
			managed = rememberRecordMember(managed, existing)
		}

		if instance.RecordNeedsUpdate(existing, record) {
			canUpdate := mayManage
			if instance.Mode == provider.ModeAdditive && !instance.Provider.Capabilities().SupportsNativeUpdate {
				canUpdate = false
			}
			if !canUpdate {
				actions = append(actions, memberAction(ActionSkip, StatusSkipped, instance, record, errRecordAlreadyExists))
				legacyUpgradeComplete = false
				continue
			}

			action := memberAction(ActionUpdate, StatusSuccess, instance, record, "")
			if !r.isDryRun() {
				if err := instance.UpdateRecord(ctx, existing, record); err != nil {
					action.Status = StatusFailed
					action.Error = err.Error()
					legacyUpgradeComplete = false
					desiredWritesSucceeded = false
				} else if err := r.ensureMemberOwnership(ctx, instance, record, cache); err != nil {
					legacyUpgradeComplete = false
					desiredWritesSucceeded = false
					r.logger.Warn("failed to create member ownership record",
						slog.String("provider", instance.Name()),
						slog.String("hostname", record.Hostname),
						slog.String("target", record.Target),
						slog.String("error", err.Error()),
					)
				} else {
					managed = rememberRecordMember(managed, record)
				}
			}
			actions = append(actions, action)
			continue
		}

		actions = append(actions, memberAction(ActionSkip, StatusSkipped, instance, record, errRecordAlreadyExists))
		if mayManage && !r.isDryRun() {
			if err := r.ensureMemberOwnership(ctx, instance, record, cache); err != nil {
				legacyUpgradeComplete = false
				r.logger.Warn("failed to create member ownership record",
					slog.String("provider", instance.Name()),
					slog.String("hostname", record.Hostname),
					slog.String("target", record.Target),
					slog.String("error", err.Error()),
				)
			}
		} else if hasLegacyOwnership {
			legacyUpgradeComplete = false
		}
	}

	if allowRemovals && desiredWritesSucceeded && instance.Mode.AllowsDelete() {
		for i, existing := range sameType {
			if matchedExisting[i] || !r.mayDeleteMember(instance, existing, cache, previous, false) {
				continue
			}
			action, _ := r.deleteSetMember(ctx, instance, existing, cache)
			actions = append(actions, action)
			if action.Status == StatusSuccess && !r.isDryRun() {
				managed = forgetRecordMember(managed, existing)
			}
		}
	}

	if legacyUpgradeComplete && !r.isDryRun() {
		legacy, found := cache.legacyOwnershipRecord(instance.Name(), set.Key.Hostname, instance.InstanceID)
		if found {
			if err := instance.DeleteMember(ctx, legacy); err != nil {
				r.logger.Warn("failed to remove upgraded legacy ownership record",
					slog.String("provider", instance.Name()),
					slog.String("hostname", set.Key.Hostname),
					slog.String("error", err.Error()),
				)
			}
		}
	}

	return actions, managed
}

func rememberRecordMember(records []provider.Record, record provider.Record) []provider.Record {
	for _, existing := range records {
		if provider.SameRecordMember(existing, record) {
			return records
		}
	}
	return append(records, record)
}

func forgetRecordMember(records []provider.Record, record provider.Record) []provider.Record {
	kept := records[:0]
	for _, existing := range records {
		if !provider.SameRecordMember(existing, record) {
			kept = append(kept, existing)
		}
	}
	return kept
}

func (r *Reconciler) mayDeleteMember(instance *provider.ProviderInstance, record provider.Record, cache *recordCache, previous []provider.Record, adopt bool) bool {
	if !instance.Mode.AllowsDelete() {
		return false
	}
	if !instance.Mode.RequiresOwnership() || !r.config.OwnershipTracking || adopt {
		return true
	}
	if _, owned := cache.memberOwnershipRecord(instance.Name(), record, instance.InstanceID); owned {
		return true
	}
	if instance.Provider.Capabilities().SupportsOwnershipTXT {
		return false
	}
	for _, prior := range previous {
		if provider.SameRecordMember(prior, record) {
			return true
		}
	}
	return false
}

func (r *Reconciler) deleteSetMember(ctx context.Context, instance *provider.ProviderInstance, record provider.Record, cache *recordCache) (Action, bool) {
	action := memberAction(ActionDelete, StatusSuccess, instance, record, "")
	if r.isDryRun() {
		return action, true
	}
	if err := instance.DeleteMember(ctx, record); err != nil && !errors.Is(err, provider.ErrNotFound) {
		action.Status = StatusFailed
		action.Error = err.Error()
		return action, false
	}
	if marker, owned := cache.memberOwnershipRecord(instance.Name(), record, instance.InstanceID); owned {
		if err := instance.DeleteMember(ctx, marker); err != nil && !errors.Is(err, provider.ErrNotFound) {
			r.logger.Warn("failed to delete member ownership record",
				slog.String("provider", instance.Name()),
				slog.String("hostname", record.Hostname),
				slog.String("target", record.Target),
				slog.String("error", err.Error()),
			)
		}
	}
	return action, true
}

func (r *Reconciler) ensureMemberOwnership(ctx context.Context, instance *provider.ProviderInstance, record provider.Record, cache *recordCache) error {
	if !r.config.OwnershipTracking || !instance.Provider.Capabilities().SupportsOwnershipTXT {
		return nil
	}
	desired := provider.MemberOwnershipRecord(record.Hostname, instance.TTL, instance.InstanceID, record, record.Metadata)
	existing, found := cache.memberOwnershipRecord(instance.Name(), record, instance.InstanceID)
	if found && existing.Target == desired.Target {
		return nil
	}
	if err := instance.CreateMemberOwnershipRecord(ctx, record, record.Metadata); err != nil {
		return err
	}
	if found {
		if err := instance.DeleteMember(ctx, existing); err != nil && !errors.Is(err, provider.ErrNotFound) {
			return fmt.Errorf("deleting stale member ownership marker: %w", err)
		}
	}
	return nil
}

func memberAction(actionType ActionType, status ActionStatus, instance *provider.ProviderInstance, record provider.Record, actionError string) Action {
	return Action{
		Type:       actionType,
		Status:     status,
		Provider:   instance.Name(),
		Hostname:   record.Hostname,
		RecordType: string(record.Type),
		Target:     record.Target,
		Error:      actionError,
	}
}

func setAllowsAdoption(r *Reconciler, set *desiredRecordSet) bool {
	for _, member := range set.Members {
		if r.effectiveAdoptExisting(member.Claim, set.Instance) {
			return true
		}
	}
	return false
}

func (r *Reconciler) updateSingleValueMember(
	ctx context.Context,
	set *desiredRecordSet,
	existing provider.Record,
	cache *recordCache,
	previous []provider.Record,
	actions []Action,
	managed []provider.Record,
	hasLegacyOwnership bool,
) ([]Action, []provider.Record) {
	instance := set.Instance
	desiredMember := set.Members[0]
	desired := desiredMember.Record
	_, hasMemberOwnership := cache.memberOwnershipRecord(instance.Name(), existing, instance.InstanceID)
	managedInMemory := recordMemberPresent(previous, existing)
	adopt := r.effectiveAdoptExisting(desiredMember.Claim, instance)
	mayManage := !r.config.OwnershipTracking || !instance.Mode.RequiresOwnership() ||
		hasMemberOwnership || adopt || hasLegacyOwnership ||
		(!instance.Provider.Capabilities().SupportsOwnershipTXT && managedInMemory)
	if !mayManage || (instance.Mode == provider.ModeAdditive && !instance.Provider.Capabilities().SupportsNativeUpdate) {
		return append(actions, memberAction(ActionSkip, StatusSkipped, instance, desired, errRecordAlreadyExists)), managed
	}

	action := memberAction(ActionUpdate, StatusSuccess, instance, desired, "")
	if r.isDryRun() {
		return append(actions, action), managed
	}

	var newMarker provider.Record
	markerCreated := false
	if r.config.OwnershipTracking && instance.Provider.Capabilities().SupportsOwnershipTXT {
		if _, exists := cache.memberOwnershipRecord(instance.Name(), desired, instance.InstanceID); !exists {
			newMarker = provider.MemberOwnershipRecord(desired.Hostname, instance.TTL, instance.InstanceID, desired, desired.Metadata)
			if err := instance.CreateMemberOwnershipRecord(ctx, desired, desired.Metadata); err != nil {
				action.Status = StatusFailed
				action.Error = fmt.Sprintf("creating replacement ownership marker: %v", err)
				return append(actions, action), managed
			}
			markerCreated = true
		}
	}

	if err := instance.UpdateRecord(ctx, existing, desired); err != nil {
		action.Status = StatusFailed
		action.Error = err.Error()
		if markerCreated {
			if cleanupErr := instance.DeleteMember(ctx, newMarker); cleanupErr != nil && !errors.Is(cleanupErr, provider.ErrNotFound) {
				r.logger.Warn("failed to roll back replacement ownership marker",
					slog.String("provider", instance.Name()),
					slog.String("hostname", desired.Hostname),
					slog.String("error", cleanupErr.Error()),
				)
			}
		}
		return append(actions, action), managed
	}

	managed = forgetRecordMember(managed, existing)
	managed = rememberRecordMember(managed, desired)
	if oldMarker, exists := cache.memberOwnershipRecord(instance.Name(), existing, instance.InstanceID); exists {
		if err := instance.DeleteMember(ctx, oldMarker); err != nil && !errors.Is(err, provider.ErrNotFound) {
			r.logger.Warn("failed to remove replaced member ownership record",
				slog.String("provider", instance.Name()),
				slog.String("hostname", desired.Hostname),
				slog.String("error", err.Error()),
			)
		}
	} else if legacy, exists := cache.legacyOwnershipRecord(instance.Name(), existing.Hostname, instance.InstanceID); exists {
		if err := instance.DeleteMember(ctx, legacy); err != nil && !errors.Is(err, provider.ErrNotFound) {
			r.logger.Warn("failed to remove upgraded legacy ownership record",
				slog.String("provider", instance.Name()),
				slog.String("hostname", desired.Hostname),
				slog.String("error", err.Error()),
			)
		}
	}
	return append(actions, action), managed
}

func recordMemberPresent(records []provider.Record, record provider.Record) bool {
	for _, existing := range records {
		if provider.SameRecordMember(existing, record) {
			return true
		}
	}
	return false
}
