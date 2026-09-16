package reconciler

import (
	"context"
	"errors"
	"fmt"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

type replacementKey struct {
	Identity provider.ProviderIdentity
	Hostname string
}

// Recovery is deliberately process-local. Provider CRUD is not a transaction:
// a crash or a provider that applies a write then returns an error needs an
// operator to inspect the backend. Old ownership markers stay in place until
// the entire replacement has succeeded.
type replacementRecovery struct {
	Instance *provider.ProviderInstance
	Removed  []provider.Record
	Created  []provider.Record
}

func (r *Reconciler) reconcileCrossTypeSet(ctx context.Context, set *desiredRecordSet, cache *recordCache, previous, conflicts []provider.Record, allowRemovals bool) ([]Action, []provider.Record) {
	inst := set.Instance
	for _, old := range conflicts {
		if !allowRemovals || !r.mayDeleteMember(inst, old, cache, previous, setAllowsAdoption(r, set)) {
			return []Action{memberAction(ActionSkip, StatusSkipped, inst, set.Members[0].Record, errRecordTypeConflict)}, previous
		}
	}
	if r.isDryRun() {
		var actions []Action
		for _, old := range conflicts {
			actions = append(actions, memberAction(ActionDelete, StatusSuccess, inst, old, ""))
		}
		for _, member := range set.Members {
			actions = append(actions, memberAction(ActionCreate, StatusSuccess, inst, member.Record, ""))
		}
		return actions, previous
	}
	before, _ := cache.getExistingRecords(inst.Name(), set.Key.Hostname, set.Key.RecordType)
	recovery := replacementRecovery{Instance: inst}
	var actions []Action
	for _, old := range conflicts {
		action := memberAction(ActionDelete, StatusSuccess, inst, old, "")
		if err := inst.DeleteMember(ctx, old); err != nil && !errors.Is(err, provider.ErrNotFound) {
			action.Status = StatusFailed
			action.Error = err.Error()
			actions = append(actions, action)
			return r.failReplacement(ctx, set, cache, recovery, actions, previous)
		}
		recovery.Removed = append(recovery.Removed, old)
		cache.removeRecord(inst.Name(), old)
		actions = append(actions, action)
	}
	// The conflicts are absent from the updated snapshot. Normal exact-member
	// creation now handles the desired set without recursively replacing it.
	nextActions, managed := r.reconcileDesiredSetWithState(ctx, set, cache, previous, allowRemovals)
	actions = append(actions, nextActions...)
	failed := false
	for _, action := range nextActions {
		if action.Status == StatusFailed {
			failed = true
		}
	}
	if failed {
		for _, member := range set.Members {
			records, _ := cache.getExistingRecords(inst.Name(), member.Record.Hostname, member.Record.Type)
			if recordMemberPresent(records, member.Record) && !recordMemberPresent(before, member.Record) {
				// Cross-type exclusivity implies these desired members were newly
				// created. Only these members, never unrelated siblings, are removed.
				recovery.Created = append(recovery.Created, member.Record)
			}
		}
		return r.failReplacement(ctx, set, cache, recovery, actions, previous)
	}
	for _, old := range recovery.Removed {
		if marker, ok := cache.memberOwnershipRecord(inst.Name(), old, inst.InstanceID); ok {
			if err := inst.DeleteMember(ctx, marker); err != nil && !errors.Is(err, provider.ErrNotFound) {
				actions = append(actions, memberAction(ActionDelete, StatusFailed, inst, marker, fmt.Sprintf("replacement succeeded; old ownership cleanup failed: %v", err)))
			} else {
				cache.removeRecord(inst.Name(), marker)
			}
		}
		managed = forgetRecordMember(managed, old)
	}
	return actions, managed
}

func (r *Reconciler) failReplacement(ctx context.Context, set *desiredRecordSet, cache *recordCache, recovery replacementRecovery, actions []Action, previous []provider.Record) ([]Action, []provider.Record) {
	restored, ok := r.restoreReplacement(ctx, cache, recovery)
	actions = append(actions, restored...)
	if !ok {
		r.mu.Lock()
		if r.pendingReplacements == nil {
			r.pendingReplacements = make(map[replacementKey]replacementRecovery)
		}
		r.pendingReplacements[replacementKey{set.Key.Identity, set.Key.Hostname}] = recovery
		r.mu.Unlock()
	}
	return actions, previous
}

func (r *Reconciler) restoreReplacement(ctx context.Context, cache *recordCache, recovery replacementRecovery) ([]Action, bool) {
	inst := recovery.Instance
	var actions []Action
	ok := true
	dataRemoved := true

	for _, created := range recovery.Created {
		records, _ := cache.getExistingRecords(inst.Name(), created.Hostname, created.Type)
		if recordMemberPresent(records, created) {
			action := memberAction(ActionDelete, StatusSuccess, inst, created, "")
			if err := inst.DeleteMember(ctx, created); err != nil && !errors.Is(err, provider.ErrNotFound) {
				ok = false
				action.Status = StatusFailed
				dataRemoved = false
				action.Error = fmt.Sprintf("rollback removal failed: %v", err)
			} else {
				cache.removeRecord(inst.Name(), created)
			}
			actions = append(actions, action)
			if action.Status == StatusFailed {
				continue
			}
		}
		if marker, exists := cache.memberOwnershipRecord(inst.Name(), created, inst.InstanceID); exists {
			action := memberAction(ActionDelete, StatusSuccess, inst, marker, "")
			if err := inst.DeleteMember(ctx, marker); err != nil && !errors.Is(err, provider.ErrNotFound) {
				ok = false
				action.Status = StatusFailed
				action.Error = fmt.Sprintf("rollback ownership cleanup failed: %v", err)
			} else {
				cache.removeRecord(inst.Name(), marker)
			}
			actions = append(actions, action)
		}
	}

	// Never restore a CNAME alongside a replacement whose removal failed.
	if !dataRemoved {
		return actions, false
	}
	for _, old := range recovery.Removed {
		records, _ := cache.getExistingRecords(inst.Name(), old.Hostname, old.Type)
		if recordMemberPresent(records, old) {
			continue
		}
		action := memberAction(ActionCreate, StatusSuccess, inst, old, "")
		restore := old
		restore.ProviderID = "" // IDs identify deleted objects and cannot be reused.
		if err := inst.Provider.Create(ctx, restore); err != nil {
			ok = false
			action.Status = StatusFailed
			action.Error = fmt.Sprintf("rollback restoration failed: %v", err)
		} else {
			cache.addRecord(inst.Name(), old)
		}
		actions = append(actions, action)
	}
	return actions, ok
}

// Recovery precedes normal desired-state work and blocks that backend/name
// for the entire cycle, including orphan sets for the old record type.
func (r *Reconciler) retryReplacements(ctx context.Context, cache *recordCache, result *Result) map[replacementKey]bool {
	blocked := make(map[replacementKey]bool)
	r.mu.Lock()
	pending := make(map[replacementKey]replacementRecovery, len(r.pendingReplacements))
	for key, recovery := range r.pendingReplacements {
		pending[key] = recovery
	}
	r.mu.Unlock()
	for key, recovery := range pending {
		blocked[key] = true

		if r.isDryRun() {
			result.AddAction(memberAction(ActionSkip, StatusSkipped, recovery.Instance, recovery.Removed[0], "replacement recovery pending during dry-run"))
			continue
		}
		if !cache.providerAvailable(recovery.Instance.Name()) {
			result.AddAction(memberAction(ActionCreate, StatusFailed, recovery.Instance, recovery.Removed[0], "provider record snapshot unavailable; replacement recovery pending"))
			continue
		}
		actions, ok := r.restoreReplacement(ctx, cache, recovery)
		for _, action := range actions {
			result.AddAction(action)
		}
		if ok {
			r.mu.Lock()
			delete(r.pendingReplacements, key)
			r.mu.Unlock()
		}
	}
	return blocked
}
