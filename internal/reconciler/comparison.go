// Package reconciler implements the core logic for comparing desired DNS state
// (from sources) with actual DNS state (from providers) and applying changes.
package reconciler

import (
	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/source"
)

// RecordPair represents an existing record and its desired replacement.
// Used in RecordDiff.ToUpdate to show what needs to change.
type RecordPair struct {
	Existing provider.Record
	Desired  provider.Record
}

// RecordDiff represents the differences between existing and desired records.
// This is the output of CompareRecordSets() and can be used by providers
// or the reconciler to understand what changes need to be made.
type RecordDiff struct {
	// ToCreate contains records that exist in desired but not in existing.
	ToCreate []provider.Record

	// ToUpdate contains pairs of records where the target or other fields changed.
	// Each pair has the existing record and the desired record.
	ToUpdate []RecordPair

	// ToDelete contains records that exist in existing but not in desired.
	ToDelete []provider.Record

	// Unchanged contains records that are the same in both sets.
	Unchanged []provider.Record
}

// HasChanges returns true if there are any records to create, update, or delete.
func (d *RecordDiff) HasChanges() bool {
	return len(d.ToCreate) > 0 || len(d.ToUpdate) > 0 || len(d.ToDelete) > 0
}

// TotalChanges returns the total number of changes (create + update + delete).
func (d *RecordDiff) TotalChanges() int {
	return len(d.ToCreate) + len(d.ToUpdate) + len(d.ToDelete)
}

// CompareRecordSets compares existing and desired records and returns a diff.
// This is the core comparison logic that providers and the reconciler use
// instead of implementing their own comparison.
//
// Records are matched by hostname (case-insensitive) and type.
// For non-SRV records, only one record per hostname+type is expected.
// For SRV records, multiple records with the same hostname but different targets are allowed.
//
// Comparison rules:
// - Same hostname+type+target → unchanged
// - Same hostname+type, different target → update
// - In desired but not existing → create
// - In existing but not desired → delete
func CompareRecordSets(existing, desired []provider.Record) RecordDiff {
	diff := RecordDiff{}

	// Build a map of existing records by normalized hostname + type + target
	existingMap := make(map[string]provider.Record)
	for _, r := range existing {
		key := recordKey(r)
		existingMap[key] = r
	}

	// Build a map of desired records
	desiredMap := make(map[string]provider.Record)
	for _, r := range desired {
		key := recordKey(r)
		desiredMap[key] = r
	}

	// Find records to create or update
	for key, desiredRecord := range desiredMap {
		if existingRecord, exists := existingMap[key]; exists {
			// Record exists - check if it needs updating
			if recordNeedsUpdate(existingRecord, desiredRecord) {
				diff.ToUpdate = append(diff.ToUpdate, RecordPair{
					Existing: existingRecord,
					Desired:  desiredRecord,
				})
			} else {
				diff.Unchanged = append(diff.Unchanged, existingRecord)
			}
		} else {
			// Record doesn't exist - need to create
			diff.ToCreate = append(diff.ToCreate, desiredRecord)
		}
	}

	// Find records to delete (exist but not desired)
	for key, existingRecord := range existingMap {
		if _, exists := desiredMap[key]; !exists {
			diff.ToDelete = append(diff.ToDelete, existingRecord)
		}
	}

	return diff
}

// CompareForHostname compares records for a single hostname and returns a diff.
// This is a convenience wrapper around CompareRecordSets for single-hostname operations.
func CompareForHostname(existing, desired []provider.Record, hostname string) RecordDiff {
	// Filter both sets to only include records for this hostname
	normalizedHostname := source.NormalizeHostname(hostname)

	var filteredExisting, filteredDesired []provider.Record
	for _, r := range existing {
		if source.NormalizeHostname(r.Hostname) == normalizedHostname {
			filteredExisting = append(filteredExisting, r)
		}
	}
	for _, r := range desired {
		if source.NormalizeHostname(r.Hostname) == normalizedHostname {
			filteredDesired = append(filteredDesired, r)
		}
	}

	return CompareRecordSets(filteredExisting, filteredDesired)
}

// recordKey generates a unique key for a record based on hostname, type, and target.
// For SRV records, also includes priority/weight/port to handle multiple SRV records.
func recordKey(r provider.Record) string {
	// Normalize hostname for case-insensitive comparison
	normalized := source.NormalizeHostname(r.Hostname)
	key := normalized + "|" + string(r.Type) + "|" + r.Target

	// For SRV records, include the SRV-specific data in the key
	if r.Type == provider.RecordTypeSRV && r.SRV != nil {
		key += "|" + formatSRVKey(r.SRV)
	}

	return key
}

// formatSRVKey creates a string key from SRV data for map lookups.
func formatSRVKey(srv *provider.SRVData) string {
	if srv == nil {
		return ""
	}
	return string(rune(srv.Priority)) + ":" + string(rune(srv.Weight)) + ":" + string(rune(srv.Port))
}

// recordNeedsUpdate checks if an existing record needs to be updated to match desired.
// Records are considered needing update if TTL differs.
// Target differences are already handled by the key comparison.
func recordNeedsUpdate(existing, desired provider.Record) bool {
	// TTL difference requires update
	if existing.TTL != desired.TTL {
		return true
	}

	// For SRV records, check SRV-specific data
	if existing.Type == provider.RecordTypeSRV {
		if !srvDataEquals(existing.SRV, desired.SRV) {
			return true
		}
	}

	return false
}

// CategorizeSameHostnameRecords groups records by whether they match the desired type.
// Returns (sameType, differentType) slices.
// This is used when checking for type conflicts before creating a record.
func CategorizeSameHostnameRecords(records []provider.Record, desiredType provider.RecordType) (sameType, differentType []provider.Record) {
	for _, r := range records {
		if r.Type == desiredType {
			sameType = append(sameType, r)
		} else {
			differentType = append(differentType, r)
		}
	}
	return
}

// FindExactMatch finds a record with matching target (and SRV data if applicable).
// Returns the matching record and true if found, or empty record and false if not.
func FindExactMatch(records []provider.Record, target string, recordType provider.RecordType, srvData *provider.SRVData) (provider.Record, bool) {
	for _, r := range records {
		if r.Type != recordType {
			continue
		}
		if r.Target != target {
			continue
		}

		// For SRV records, also check SRV-specific data
		if recordType == provider.RecordTypeSRV {
			if srvDataEquals(r.SRV, srvData) {
				return r, true
			}
		} else {
			return r, true
		}
	}
	return provider.Record{}, false
}

// FindStaleSRVRecords finds SRV records with matching target but different priority/weight/port.
// These are records that need to be deleted and recreated with new SRV data.
func FindStaleSRVRecords(records []provider.Record, target string, desiredSRV *provider.SRVData) []provider.Record {
	var stale []provider.Record
	for _, r := range records {
		if r.Type != provider.RecordTypeSRV {
			continue
		}
		if r.Target != target {
			continue
		}
		// Same target but different SRV data = stale
		if !srvDataEquals(r.SRV, desiredSRV) {
			stale = append(stale, r)
		}
	}
	return stale
}
