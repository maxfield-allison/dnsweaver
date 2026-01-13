package reconciler

import (
	"testing"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

func TestCompareRecordSets_Empty(t *testing.T) {
	diff := CompareRecordSets(nil, nil)

	if diff.HasChanges() {
		t.Error("expected no changes for empty sets")
	}
	if len(diff.ToCreate) != 0 {
		t.Errorf("expected 0 ToCreate, got %d", len(diff.ToCreate))
	}
	if len(diff.ToUpdate) != 0 {
		t.Errorf("expected 0 ToUpdate, got %d", len(diff.ToUpdate))
	}
	if len(diff.ToDelete) != 0 {
		t.Errorf("expected 0 ToDelete, got %d", len(diff.ToDelete))
	}
	if len(diff.Unchanged) != 0 {
		t.Errorf("expected 0 Unchanged, got %d", len(diff.Unchanged))
	}
}

func TestCompareRecordSets_AllNew(t *testing.T) {
	desired := []provider.Record{
		{Hostname: "app1.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300},
		{Hostname: "app2.example.com", Type: provider.RecordTypeA, Target: "10.0.0.2", TTL: 300},
	}

	diff := CompareRecordSets(nil, desired)

	if !diff.HasChanges() {
		t.Error("expected changes")
	}
	if len(diff.ToCreate) != 2 {
		t.Errorf("expected 2 ToCreate, got %d", len(diff.ToCreate))
	}
	if len(diff.ToDelete) != 0 {
		t.Errorf("expected 0 ToDelete, got %d", len(diff.ToDelete))
	}
	if diff.TotalChanges() != 2 {
		t.Errorf("expected 2 total changes, got %d", diff.TotalChanges())
	}
}

func TestCompareRecordSets_AllDelete(t *testing.T) {
	existing := []provider.Record{
		{Hostname: "app1.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300},
		{Hostname: "app2.example.com", Type: provider.RecordTypeA, Target: "10.0.0.2", TTL: 300},
	}

	diff := CompareRecordSets(existing, nil)

	if !diff.HasChanges() {
		t.Error("expected changes")
	}
	if len(diff.ToCreate) != 0 {
		t.Errorf("expected 0 ToCreate, got %d", len(diff.ToCreate))
	}
	if len(diff.ToDelete) != 2 {
		t.Errorf("expected 2 ToDelete, got %d", len(diff.ToDelete))
	}
}

func TestCompareRecordSets_AllUnchanged(t *testing.T) {
	records := []provider.Record{
		{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300},
	}

	diff := CompareRecordSets(records, records)

	if diff.HasChanges() {
		t.Error("expected no changes")
	}
	if len(diff.Unchanged) != 1 {
		t.Errorf("expected 1 Unchanged, got %d", len(diff.Unchanged))
	}
}

func TestCompareRecordSets_TTLChange(t *testing.T) {
	existing := []provider.Record{
		{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300},
	}
	desired := []provider.Record{
		{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 600},
	}

	diff := CompareRecordSets(existing, desired)

	if !diff.HasChanges() {
		t.Error("expected changes for TTL difference")
	}
	if len(diff.ToUpdate) != 1 {
		t.Errorf("expected 1 ToUpdate, got %d", len(diff.ToUpdate))
	}
	if diff.ToUpdate[0].Existing.TTL != 300 {
		t.Error("expected existing TTL to be 300")
	}
	if diff.ToUpdate[0].Desired.TTL != 600 {
		t.Error("expected desired TTL to be 600")
	}
}

func TestCompareRecordSets_TargetChange(t *testing.T) {
	existing := []provider.Record{
		{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300},
	}
	desired := []provider.Record{
		{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.2", TTL: 300},
	}

	diff := CompareRecordSets(existing, desired)

	if !diff.HasChanges() {
		t.Error("expected changes for target difference")
	}
	// Target change means old is deleted, new is created
	if len(diff.ToCreate) != 1 {
		t.Errorf("expected 1 ToCreate (new target), got %d", len(diff.ToCreate))
	}
	if len(diff.ToDelete) != 1 {
		t.Errorf("expected 1 ToDelete (old target), got %d", len(diff.ToDelete))
	}
}

func TestCompareRecordSets_MixedOperations(t *testing.T) {
	existing := []provider.Record{
		{Hostname: "keep.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300},
		{Hostname: "delete.example.com", Type: provider.RecordTypeA, Target: "10.0.0.2", TTL: 300},
		{Hostname: "update.example.com", Type: provider.RecordTypeA, Target: "10.0.0.3", TTL: 300},
	}
	desired := []provider.Record{
		{Hostname: "keep.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300},
		{Hostname: "create.example.com", Type: provider.RecordTypeA, Target: "10.0.0.4", TTL: 300},
		{Hostname: "update.example.com", Type: provider.RecordTypeA, Target: "10.0.0.3", TTL: 600}, // TTL changed
	}

	diff := CompareRecordSets(existing, desired)

	if !diff.HasChanges() {
		t.Error("expected changes")
	}
	if len(diff.Unchanged) != 1 {
		t.Errorf("expected 1 Unchanged, got %d", len(diff.Unchanged))
	}
	if len(diff.ToCreate) != 1 {
		t.Errorf("expected 1 ToCreate, got %d", len(diff.ToCreate))
	}
	if len(diff.ToDelete) != 1 {
		t.Errorf("expected 1 ToDelete, got %d", len(diff.ToDelete))
	}
	if len(diff.ToUpdate) != 1 {
		t.Errorf("expected 1 ToUpdate, got %d", len(diff.ToUpdate))
	}
}

func TestCompareRecordSets_CaseInsensitive(t *testing.T) {
	existing := []provider.Record{
		{Hostname: "APP.EXAMPLE.COM", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300},
	}
	desired := []provider.Record{
		{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300},
	}

	diff := CompareRecordSets(existing, desired)

	// Should match case-insensitively
	if diff.HasChanges() {
		t.Error("expected no changes - hostnames should match case-insensitively")
	}
	if len(diff.Unchanged) != 1 {
		t.Errorf("expected 1 Unchanged, got %d", len(diff.Unchanged))
	}
}

func TestCompareRecordSets_SRVRecords(t *testing.T) {
	existing := []provider.Record{
		{
			Hostname: "_http._tcp.example.com",
			Type:     provider.RecordTypeSRV,
			Target:   "server.example.com",
			TTL:      300,
			SRV:      &provider.SRVData{Priority: 10, Weight: 5, Port: 80},
		},
	}
	desired := []provider.Record{
		{
			Hostname: "_http._tcp.example.com",
			Type:     provider.RecordTypeSRV,
			Target:   "server.example.com",
			TTL:      300,
			SRV:      &provider.SRVData{Priority: 10, Weight: 5, Port: 80},
		},
	}

	diff := CompareRecordSets(existing, desired)

	if diff.HasChanges() {
		t.Error("expected no changes for identical SRV records")
	}
}

func TestCompareRecordSets_SRVPriorityChange(t *testing.T) {
	existing := []provider.Record{
		{
			Hostname: "_http._tcp.example.com",
			Type:     provider.RecordTypeSRV,
			Target:   "server.example.com",
			TTL:      300,
			SRV:      &provider.SRVData{Priority: 10, Weight: 5, Port: 80},
		},
	}
	desired := []provider.Record{
		{
			Hostname: "_http._tcp.example.com",
			Type:     provider.RecordTypeSRV,
			Target:   "server.example.com",
			TTL:      300,
			SRV:      &provider.SRVData{Priority: 20, Weight: 5, Port: 80}, // Priority changed
		},
	}

	diff := CompareRecordSets(existing, desired)

	if !diff.HasChanges() {
		t.Error("expected changes for SRV priority difference")
	}
	// Different priority = different record key, so create new + delete old
	if len(diff.ToCreate) != 1 {
		t.Errorf("expected 1 ToCreate, got %d", len(diff.ToCreate))
	}
	if len(diff.ToDelete) != 1 {
		t.Errorf("expected 1 ToDelete, got %d", len(diff.ToDelete))
	}
}

func TestCompareForHostname_FiltersCorrectly(t *testing.T) {
	existing := []provider.Record{
		{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 300},
		{Hostname: "other.example.com", Type: provider.RecordTypeA, Target: "10.0.0.2", TTL: 300},
	}
	desired := []provider.Record{
		{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1", TTL: 600},
		{Hostname: "new.example.com", Type: provider.RecordTypeA, Target: "10.0.0.3", TTL: 300},
	}

	diff := CompareForHostname(existing, desired, "app.example.com")

	// Should only consider records for app.example.com
	if len(diff.ToUpdate) != 1 {
		t.Errorf("expected 1 ToUpdate for app.example.com, got %d", len(diff.ToUpdate))
	}
	// other.example.com and new.example.com should not be in the diff
	if len(diff.ToDelete) != 0 {
		t.Errorf("expected 0 ToDelete (other.example.com filtered out), got %d", len(diff.ToDelete))
	}
	if len(diff.ToCreate) != 0 {
		t.Errorf("expected 0 ToCreate (new.example.com filtered out), got %d", len(diff.ToCreate))
	}
}

func TestCategorizeSameHostnameRecords(t *testing.T) {
	records := []provider.Record{
		{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"},
		{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.2"},
		{Hostname: "app.example.com", Type: provider.RecordTypeCNAME, Target: "other.example.com"},
	}

	sameType, differentType := CategorizeSameHostnameRecords(records, provider.RecordTypeA)

	if len(sameType) != 2 {
		t.Errorf("expected 2 same type records, got %d", len(sameType))
	}
	if len(differentType) != 1 {
		t.Errorf("expected 1 different type record, got %d", len(differentType))
	}
}

func TestFindExactMatch(t *testing.T) {
	records := []provider.Record{
		{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"},
		{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.2"},
	}

	// Should find exact match
	record, found := FindExactMatch(records, "10.0.0.1", provider.RecordTypeA, nil)
	if !found {
		t.Error("expected to find exact match")
	}
	if record.Target != "10.0.0.1" {
		t.Errorf("expected target 10.0.0.1, got %s", record.Target)
	}

	// Should not find non-existent
	_, found = FindExactMatch(records, "10.0.0.3", provider.RecordTypeA, nil)
	if found {
		t.Error("expected not to find non-existent record")
	}

	// Should not match wrong type
	_, found = FindExactMatch(records, "10.0.0.1", provider.RecordTypeCNAME, nil)
	if found {
		t.Error("expected not to find wrong type")
	}
}

func TestFindStaleSRVRecords(t *testing.T) {
	records := []provider.Record{
		{
			Hostname: "_http._tcp.example.com",
			Type:     provider.RecordTypeSRV,
			Target:   "server.example.com",
			SRV:      &provider.SRVData{Priority: 10, Weight: 5, Port: 80},
		},
		{
			Hostname: "_http._tcp.example.com",
			Type:     provider.RecordTypeSRV,
			Target:   "server.example.com",
			SRV:      &provider.SRVData{Priority: 20, Weight: 5, Port: 80}, // Different priority
		},
	}

	// Find records with same target but different SRV data
	desiredSRV := &provider.SRVData{Priority: 10, Weight: 5, Port: 80}
	stale := FindStaleSRVRecords(records, "server.example.com", desiredSRV)

	if len(stale) != 1 {
		t.Errorf("expected 1 stale record, got %d", len(stale))
	}
	if stale[0].SRV.Priority != 20 {
		t.Error("expected stale record to be the one with priority 20")
	}
}

func TestRecordDiff_HasChanges(t *testing.T) {
	tests := []struct {
		name       string
		diff       RecordDiff
		hasChanges bool
	}{
		{"empty", RecordDiff{}, false},
		{"has create", RecordDiff{ToCreate: []provider.Record{{}}}, true},
		{"has update", RecordDiff{ToUpdate: []RecordPair{{}}}, true},
		{"has delete", RecordDiff{ToDelete: []provider.Record{{}}}, true},
		{"only unchanged", RecordDiff{Unchanged: []provider.Record{{}}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.diff.HasChanges(); got != tt.hasChanges {
				t.Errorf("HasChanges() = %v, want %v", got, tt.hasChanges)
			}
		})
	}
}
