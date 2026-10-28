package reconciler

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
)

type memberOwnershipRepairKey struct {
	Identity provider.ProviderIdentity
	Member   string
}

type memberOwnershipRepair struct {
	Instance *provider.ProviderInstance
	Record   provider.Record
}

// Retain proof only after a confirmed local answer write, never after a create
// conflict or from an unreferenced page. It grants no authority after restart.
func (r *Reconciler) rememberMemberOwnershipRepair(instance *provider.ProviderInstance, record provider.Record) {
	if _, supported := instance.Provider.(provider.SharedOwnership); !supported || !r.config.OwnershipTracking || !instance.Provider.Capabilities().SupportsOwnershipTXT {
		return
	}
	record.Metadata = maps.Clone(record.Metadata)
	key := memberOwnershipRepairKey{provider.IdentityOf(instance.Provider), recordKey(record)}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pendingMemberOwnership == nil {
		r.pendingMemberOwnership = make(map[memberOwnershipRepairKey]memberOwnershipRepair)
	}
	r.pendingMemberOwnership[key] = memberOwnershipRepair{instance, record}
}

// Repair at a held name stays pending without writing.
func (r *Reconciler) retryMemberOwnership(ctx context.Context, cache *recordCache, result *Result, held map[replacementKey]struct{}) map[replacementKey]bool {
	blocked := make(map[replacementKey]bool)
	r.mu.Lock()
	pending := maps.Clone(r.pendingMemberOwnership)
	r.mu.Unlock()
	for key, repair := range pending {
		instance, record := repair.Instance, repair.Record
		// Desired-set and replacement keys use the normalized hostname.
		route := replacementKey{key.Identity, source.NormalizeHostname(record.Hostname)}
		if _, isHeld := held[route]; isHeld {
			blocked[route] = true
			result.AddAction(memberAction(ActionSkip, StatusSkipped, instance, record, "member ownership repair held: "+errCNAMEWithOtherTypes))
			continue
		}
		if r.isDryRun() {
			blocked[route] = true
			result.AddAction(memberAction(ActionSkip, StatusSkipped, instance, record, "member ownership repair pending during dry-run"))
			continue
		}
		if !cache.providerAvailable(instance.Name()) {
			blocked[route] = true
			result.AddAction(memberAction(ActionCreate, StatusFailed, instance, record, "provider record snapshot unavailable; member ownership repair pending"))
			continue
		}
		records, _ := cache.getExistingRecords(instance.Name(), record.Hostname, record.Type)
		index := slices.IndexFunc(records, func(existing provider.Record) bool {
			return provider.SameRecordMember(existing, record)
		})
		// External deletion or a changed member/options invalidates local proof.
		// The normal adoption policy decides what may happen to the new state.
		if index < 0 || records[index].TTL != record.TTL || instance.RecordNeedsUpdate(records[index], record) {
			r.forgetMemberOwnershipRepair(key)
			continue
		}
		sharing, supported := instance.Provider.(provider.SharedOwnership)
		if !supported {
			r.forgetMemberOwnershipRepair(key)
			continue
		}
		var err error
		for _, marker := range cache.ownershipRecords(instance.Name(), record.Hostname) {
			owned, owner, member, _ := provider.ParseMemberOwnershipValue(marker.Target)
			if owned && owner != instance.InstanceID && (member == nil ||
				(record.Type == provider.RecordTypeCNAME || member.Type == provider.RecordTypeCNAME) && !provider.SameRecordMember(*member, record)) {
				err = fmt.Errorf("%w: member ownership repair conflicts with a foreign claim", provider.ErrConflict)
				break
			}
		}
		if err == nil {
			_, err = sharing.CanShareOwnership(ctx, records[index], record)
		}
		if err == nil {
			err = r.ensureMemberOwnership(ctx, instance, record, cache)
		}
		if err != nil {
			blocked[route] = true
			result.AddAction(memberAction(ActionCreate, StatusFailed, instance, record, fmt.Sprintf("repairing member ownership: %v", err)))
			continue
		}
		r.forgetMemberOwnershipRepair(key)
		result.AddAction(memberAction(ActionSkip, StatusSuccess, instance, record, "member ownership repaired"))
	}
	return blocked
}

func (r *Reconciler) forgetMemberOwnershipRepair(key memberOwnershipRepairKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pendingMemberOwnership, key)
}
