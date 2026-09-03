package reconciler

import (
	"fmt"
	"log/slog"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
)

type desiredSetKey struct {
	Identity   provider.ProviderIdentity
	Hostname   string
	RecordType provider.RecordType
}

type desiredRecordMember struct {
	Record     provider.Record
	Claim      *source.Hostname
	ClaimCount int
}

type desiredRecordSet struct {
	Instance *provider.ProviderInstance
	Key      desiredSetKey
	Members  []desiredRecordMember
}

type desiredCompilation struct {
	Sets              []*desiredRecordSet
	Skipped           []Action
	HostnameProviders map[string][]string
}

type previousDesiredSet struct {
	ProviderName string
	Records      []provider.Record
}

// compileDesiredRecordSets routes source claims to physical provider backends,
// resolves instance defaults, and deduplicates identical DNS members while
// retaining how many claims want each one.
func (r *Reconciler) compileDesiredRecordSets(claims []*source.Hostname) desiredCompilation {
	compiled := desiredCompilation{HostnameProviders: make(map[string][]string)}
	sets := make(map[desiredSetKey]*desiredRecordSet)

	for _, claim := range claims {
		candidates, skip := r.providersForClaim(claim)
		if skip != nil {
			compiled.Skipped = append(compiled.Skipped, *skip)
			continue
		}

		seenBackends := make(map[desiredSetKey]struct{}, len(candidates))
		for _, instance := range candidates {
			record := desiredRecordForClaim(claim, instance)
			key := desiredSetKey{
				Identity:   instance.Identity,
				Hostname:   source.NormalizeHostname(record.Hostname),
				RecordType: record.Type,
			}
			if _, duplicate := seenBackends[key]; duplicate {
				continue
			}
			seenBackends[key] = struct{}{}

			set := sets[key]
			if set == nil {
				set = &desiredRecordSet{Instance: instance, Key: key}
				sets[key] = set
				compiled.Sets = append(compiled.Sets, set)
			}
			compiled.HostnameProviders[key.Hostname] = appendUnique(compiled.HostnameProviders[key.Hostname], set.Instance.Name())

			memberIndex := -1
			for i := range set.Members {
				if provider.SameRecordMember(set.Members[i].Record, record) {
					memberIndex = i
					break
				}
			}
			if memberIndex < 0 {
				set.Members = append(set.Members, desiredRecordMember{Record: record, Claim: claim, ClaimCount: 1})
				continue
			}

			member := &set.Members[memberIndex]
			member.ClaimCount++
			// A record-specific declaration carries more intent than a bare
			// hostname that happened to resolve to the same member.
			if member.Claim.RecordHints == nil && claim.RecordHints != nil {
				member.Record = record
				member.Claim = claim
			}
		}
	}

	return compiled
}

func (r *Reconciler) providersForClaim(claim *source.Hostname) ([]*provider.ProviderInstance, *Action) {
	if claim.RecordHints != nil && claim.RecordHints.Provider != "" {
		name := claim.RecordHints.Provider
		instance, exists := r.providers.Get(name)
		if !exists {
			return nil, &Action{
				Type:     ActionSkip,
				Status:   StatusSkipped,
				Hostname: claim.Name,
				Error:    fmt.Sprintf("explicit provider %q not found", name),
			}
		}
		return []*provider.ProviderInstance{instance}, nil
	}

	instances := r.providers.MatchingProvidersForHostname(claim.Name, claim.Metadata)
	if len(instances) == 0 {
		return nil, &Action{
			Type:     ActionSkip,
			Status:   StatusSkipped,
			Hostname: claim.Name,
			Error:    errNoMatchingProvider,
		}
	}
	return instances, nil
}

func desiredRecordForClaim(claim *source.Hostname, instance *provider.ProviderInstance) provider.Record {
	record := provider.Record{
		Hostname: claim.Name,
		Type:     instance.RecordType,
		Target:   instance.EffectiveTarget(),
		TTL:      instance.TTL,
	}
	if hints := claim.RecordHints; hints != nil {
		if hints.Type != "" {
			record.Type = provider.RecordType(hints.Type)
		}
		if hints.Target != "" {
			record.Target = hints.Target
		}
		if hints.TTL > 0 {
			record.TTL = hints.TTL
		}
		if hints.SRV != nil {
			record.SRV = &provider.SRVData{
				Priority: hints.SRV.Priority,
				Weight:   hints.SRV.Weight,
				Port:     hints.SRV.Port,
			}
		}
		if len(hints.Metadata) > 0 {
			record.Metadata = hints.Metadata
		}
	}
	return record
}

func (r *Reconciler) reconciliationSets(compiled desiredCompilation, cache *recordCache) ([]*desiredRecordSet, map[desiredSetKey]previousDesiredSet) {
	sets := append([]*desiredRecordSet(nil), compiled.Sets...)
	byKey := make(map[desiredSetKey]*desiredRecordSet, len(sets))
	for _, set := range sets {
		byKey[set.Key] = set
	}

	previous := r.previousDesiredSnapshot()
	for key, prior := range previous {
		if byKey[key] != nil {
			continue
		}
		instance, exists := r.providers.Get(prior.ProviderName)
		if !exists {
			continue
		}
		set := &desiredRecordSet{Instance: instance, Key: key}
		sets = append(sets, set)
		byKey[key] = set
	}

	if cache == nil {
		return sets, previous
	}
	for _, instance := range r.providers.All() {
		for _, owned := range cache.ownedMembers(instance.Name(), instance.InstanceID) {
			key := desiredSetKey{
				Identity:   instance.Identity,
				Hostname:   source.NormalizeHostname(owned.Member.Hostname),
				RecordType: owned.Member.Type,
			}
			if byKey[key] != nil {
				continue
			}
			set := &desiredRecordSet{Instance: instance, Key: key}
			sets = append(sets, set)
			byKey[key] = set
		}
	}
	return sets, previous
}

func (r *Reconciler) previousDesiredSnapshot() map[desiredSetKey]previousDesiredSet {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snapshot := make(map[desiredSetKey]previousDesiredSet, len(r.previousDesired))
	for key, set := range r.previousDesired {
		snapshot[key] = previousDesiredSet{
			ProviderName: set.ProviderName,
			Records:      append([]provider.Record(nil), set.Records...),
		}
	}
	return snapshot
}

func (r *Reconciler) rememberDesired(next map[desiredSetKey]previousDesiredSet) {
	r.mu.Lock()
	r.previousDesired = next
	r.mu.Unlock()
}

func (r *Reconciler) memberRemovalsAllowed(compiled desiredCompilation, cache *recordCache, previous map[desiredSetKey]previousDesiredSet) bool {
	current := make(map[desiredSetKey][]provider.Record, len(compiled.Sets))
	currentHostnames := make(map[string]struct{}, len(compiled.Sets))
	for _, set := range compiled.Sets {
		currentHostnames[set.Key.Hostname] = struct{}{}
		for _, member := range set.Members {
			current[set.Key] = append(current[set.Key], member.Record)
		}
	}

	type baselineMember struct {
		key    desiredSetKey
		record provider.Record
	}
	var baseline []baselineMember
	addBaseline := func(key desiredSetKey, record provider.Record) {
		for _, existing := range baseline {
			if existing.key == key && provider.SameRecordMember(existing.record, record) {
				return
			}
		}
		baseline = append(baseline, baselineMember{key: key, record: record})
	}
	for key, set := range previous {
		for _, record := range set.Records {
			addBaseline(key, record)
		}
	}
	if cache != nil {
		for _, instance := range r.providers.All() {
			for _, owned := range cache.ownedMembers(instance.Name(), instance.InstanceID) {
				key := desiredSetKey{Identity: instance.Identity, Hostname: source.NormalizeHostname(owned.Member.Hostname), RecordType: owned.Member.Type}
				addBaseline(key, owned.Member)
			}
		}
	}
	if len(baseline) == 0 {
		return true
	}

	removed := 0
	for _, old := range baseline {
		// A desired hostname moving away from this provider/type is an explicit
		// route retirement, not evidence that discovery lost the hostname. The
		// circuit breaker must not make multi-member route changes permanent by
		// retaining every member on the old backend.
		if _, stillRouted := current[old.key]; !stillRouted {
			if _, hostnameStillDesired := currentHostnames[old.key.Hostname]; hostnameStillDesired {
				continue
			}
		}

		found := false
		for _, record := range current[old.key] {
			if provider.SameRecordMember(old.record, record) {
				found = true
				break
			}
		}
		if !found {
			removed++
		}
	}
	const massDeleteThreshold = 0.5
	ratio := float64(removed) / float64(len(baseline))
	if removed > 1 && ratio > massDeleteThreshold {
		r.logger.Error("member deletion circuit breaker triggered; skipping removals",
			slog.Int("previous_members", len(baseline)),
			slog.Int("removed_members", removed),
			slog.Float64("removal_ratio", ratio),
			slog.Float64("threshold", massDeleteThreshold),
		)
		return false
	}
	return true
}

func desiredMemberCount(compiled desiredCompilation) int {
	count := 0
	for _, set := range compiled.Sets {
		count += len(set.Members)
	}
	return count
}
