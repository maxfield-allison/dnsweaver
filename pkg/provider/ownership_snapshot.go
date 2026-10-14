package provider

import (
	"fmt"
	"sort"
	"strings"
)

type ownershipHead struct {
	manifest OwnershipManifest
	record   Record
	token    string
}

type ownershipSnapshot struct {
	heads   map[string]ownershipHead
	history map[string][]ownershipHead
	entries map[string][]OwnershipEntry
	raw     []Record
}

// ExpandOwnershipState exposes committed compact entries as logical legacy TXT
// markers. Physical pages and manifests are excluded; ordinary records and legacy
// markers remain visible. An invalid owner snapshot makes the entire read fail.
func ExpandOwnershipState(zone string, records []Record) ([]Record, error) {
	snapshot, err := decodeOwnershipSnapshot(zone, records)
	if err != nil {
		return nil, err
	}
	result := make([]Record, 0, len(records))
	for _, record := range records {
		if !IsOwnershipStateRecord(record.Hostname) {
			result = append(result, record)
		}
	}
	owners := make([]string, 0, len(snapshot.entries))
	for owner := range snapshot.entries {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		for _, entry := range snapshot.entries[owner] {
			result = append(result, Record{Hostname: entry.Name, Type: RecordTypeTXT, Target: entry.Value, TTL: 300})
		}
	}
	return result, nil
}

func decodeOwnershipSnapshot(zone string, records []Record) (*ownershipSnapshot, error) {
	snapshot := &ownershipSnapshot{
		heads: make(map[string]ownershipHead), history: make(map[string][]ownershipHead),
		entries: make(map[string][]OwnershipEntry), raw: records,
	}
	anchor := OwnershipManifestName(zone)
	seen := make(map[string]bool)
	for _, record := range records {
		name := normalizeOwnershipName(record.Hostname)
		if record.Type != RecordTypeTXT || !strings.HasPrefix(name, ownershipManifestLabel+".") || !strings.HasSuffix(name, "."+normalizeOwnershipName(zone)) {
			continue
		}
		manifest, err := DecodeOwnershipManifest(record.Target)
		if err != nil {
			return nil, err
		}
		if name != anchor && name != OwnershipOwnerManifestName(zone, manifest.Owner) {
			return nil, fmt.Errorf("%w: manifest name does not match owner", ErrOwnershipState)
		}
		token := ownershipDigest(record.Target)
		if seen[token] {
			continue
		}
		seen[token] = true
		snapshot.history[manifest.Owner] = append(snapshot.history[manifest.Owner], ownershipHead{manifest, record, token})
	}
	for owner, history := range snapshot.history {
		head, err := selectOwnershipHead(history)
		if err != nil {
			return nil, err
		}
		entries, err := readOwnershipPages(zone, head.manifest, records)
		if err != nil {
			return nil, err
		}
		snapshot.heads[owner] = head
		snapshot.entries[owner] = entries
	}
	return snapshot, nil
}

func selectOwnershipHead(history []ownershipHead) (ownershipHead, error) {
	byToken := make(map[string]ownershipHead, len(history))
	parents := make(map[string]bool, len(history))
	for _, head := range history {
		byToken[head.token] = head
		parents[head.manifest.Previous] = true
	}
	var leaves []ownershipHead
	for _, head := range history {
		if parent, found := byToken[head.manifest.Previous]; found && (parent.manifest.Revision == ^uint64(0) || head.manifest.Revision != parent.manifest.Revision+1) {
			return ownershipHead{}, fmt.Errorf("%w: ownership revision gap", ErrOwnershipState)
		}
		if !parents[head.token] {
			leaves = append(leaves, head)
		}
	}
	if len(leaves) != 1 {
		return ownershipHead{}, fmt.Errorf("%w: competing histories for one owner", ErrOwnershipState)
	}
	// Every visible predecessor must be on the selected leaf's chain. A
	// disconnected older branch is not harmless merely because its revision is lower.
	visited := make(map[string]bool, len(history))
	for head := leaves[0]; ; {
		if visited[head.token] {
			return ownershipHead{}, fmt.Errorf("%w: cyclic ownership history", ErrOwnershipState)
		}
		visited[head.token] = true
		parent, found := byToken[head.manifest.Previous]
		if !found {
			break
		}
		head = parent
	}
	if len(visited) != len(history) {
		return ownershipHead{}, fmt.Errorf("%w: disconnected ownership history", ErrOwnershipState)
	}
	return leaves[0], nil
}

func readOwnershipPages(zone string, manifest OwnershipManifest, records []Record) ([]OwnershipEntry, error) {
	entries := make([]OwnershipEntry, 0, manifest.Entries)
	for _, digest := range manifest.Pages {
		name := OwnershipPageName(zone, manifest.Owner, digest)
		value := ""
		for _, record := range records {
			if record.Type != RecordTypeTXT || normalizeOwnershipName(record.Hostname) != name {
				continue
			}
			if value != "" && value != record.Target {
				return nil, fmt.Errorf("%w: multiple values for an immutable page", ErrOwnershipState)
			}
			value = record.Target
		}
		if value == "" || ownershipDigest(value) != digest {
			return nil, fmt.Errorf("%w: missing or corrupt ownership page", ErrOwnershipState)
		}
		page, err := decodeOwnershipPage(value, manifest.Owner, DefaultOwnershipPageBytes)
		if err != nil {
			return nil, err
		}
		for _, entry := range page {
			host := ExtractHostnameFromOwnership(entry.Name)
			zone = normalizeOwnershipName(zone)
			if host != zone && !strings.HasSuffix(host, "."+zone) {
				return nil, fmt.Errorf("%w: ownership entry is outside the zone", ErrOwnershipState)
			}
		}
		entries = append(entries, page...)
	}
	if len(entries) != manifest.Entries {
		return nil, fmt.Errorf("%w: ownership entry count mismatch", ErrOwnershipState)
	}
	return canonicalOwnershipEntries(entries, manifest.Owner)
}

func ownershipHeadsMatch(a, b *ownershipSnapshot) bool {
	if len(a.heads) != len(b.heads) {
		return false
	}
	for owner, head := range a.heads {
		if b.heads[owner].token != head.token {
			return false
		}
	}
	return true
}
