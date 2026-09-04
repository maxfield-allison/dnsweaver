// Package reconciler implements the core logic for comparing desired DNS state
// (from sources) with actual DNS state (from providers) and applying changes.
package reconciler

import (
	"context"
	"log/slog"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
)

// recordCache holds a snapshot of DNS records from all providers.
// It is built once at the start of each reconciliation cycle and used
// to avoid repeated List() API calls when checking existing records.
// All hostname keys are normalized to lowercase for case-insensitive lookups.
type recordCache struct {
	// records maps provider name -> normalized hostname -> list of records
	records map[string]map[string][]provider.Record
	logger  *slog.Logger
}

type cachedOwnedMember struct {
	Member   provider.Record
	Marker   provider.Record
	Metadata map[string]string
}

// newRecordCache creates a new record cache by querying all providers.
// Failed providers are logged but don't prevent caching other providers.
func newRecordCache(ctx context.Context, providers *provider.Registry, logger *slog.Logger) *recordCache {
	cache := &recordCache{
		records: make(map[string]map[string][]provider.Record),
		logger:  logger,
	}

	for _, inst := range providers.All() {
		providerRecords, err := inst.Provider.List(ctx)
		if err != nil {
			logger.Warn("failed to cache records for provider",
				slog.String("provider", inst.Name()),
				slog.String("error", err.Error()),
			)
			// Store empty map so we know we tried but failed
			cache.records[inst.Name()] = nil
			continue
		}

		// Index records by normalized hostname for case-insensitive lookup (RFC 1035)
		byHostname := make(map[string][]provider.Record)
		for _, r := range providerRecords {
			normalized := source.NormalizeHostname(r.Hostname)
			byHostname[normalized] = append(byHostname[normalized], r)
		}

		cache.records[inst.Name()] = byHostname
		logger.Debug("cached records for provider",
			slog.String("provider", inst.Name()),
			slog.Int("total_records", len(providerRecords)),
			slog.Int("unique_hostnames", len(byHostname)),
		)
	}

	return cache
}

// getExistingRecords returns cached DNS records for a hostname from a specific provider.
// Returns A, AAAA, CNAME, and SRV records (excludes TXT ownership records).
// Returns nil if the provider cache is unavailable (failed to load).
// Returns empty slice if cached but no records exist for this hostname.
// Hostname lookup is case-insensitive per RFC 1035.
func (c *recordCache) getExistingRecords(providerName, hostname string) ([]provider.Record, bool) {
	byHostname, exists := c.records[providerName]
	if !exists || byHostname == nil {
		// Provider not cached or failed to load
		return nil, false
	}

	normalized := source.NormalizeHostname(hostname)
	records := byHostname[normalized]

	// Filter to DNS data records (exclude TXT ownership markers)
	var filtered []provider.Record
	for _, r := range records {
		switch r.Type {
		case provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME, provider.RecordTypeSRV, provider.RecordTypeHTTPS:
			filtered = append(filtered, r)
		case provider.RecordTypeTXT:
			// Skip TXT records (ownership markers)
		}
	}

	return filtered, true
}

// getAllRecordsForHostname returns all cached records (A, AAAA, CNAME, SRV) for a hostname.
// This is used during orphan cleanup to know what record types actually exist.
// Returns nil if the provider cache is unavailable (failed to load).
// Returns empty slice if cached but no records exist for this hostname.
// Hostname lookup is case-insensitive per RFC 1035.
func (c *recordCache) getAllRecordsForHostname(providerName, hostname string) ([]provider.Record, bool) {
	byHostname, exists := c.records[providerName]
	if !exists || byHostname == nil {
		// Provider not cached or failed to load
		return nil, false
	}

	normalized := source.NormalizeHostname(hostname)
	records := byHostname[normalized]

	// Filter to data records (A, AAAA, CNAME, SRV) - exclude TXT ownership records
	var filtered []provider.Record
	for _, r := range records {
		switch r.Type {
		case provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME, provider.RecordTypeSRV, provider.RecordTypeHTTPS:
			filtered = append(filtered, r)
		case provider.RecordTypeTXT:
			// Skip TXT records (ownership markers)
		}
	}

	return filtered, true
}

// hasOwnershipRecord checks if an ownership TXT record exists for the given hostname
// that matches the specified instance ID.
// Returns false if the provider cache is unavailable.
// Hostname lookup is case-insensitive per RFC 1035.
func (c *recordCache) hasOwnershipRecord(providerName, hostname, instanceID string) bool {
	byHostname, exists := c.records[providerName]
	if !exists || byHostname == nil {
		return false
	}

	ownershipName := provider.OwnershipRecordName(hostname)
	normalized := source.NormalizeHostname(ownershipName)
	records := byHostname[normalized]

	for _, r := range records {
		if r.Type == provider.RecordTypeTXT && provider.MatchesOwnership(r.Target, instanceID) {
			return true
		}
	}

	return false
}

func (c *recordCache) memberOwnershipRecord(providerName string, member provider.Record, instanceID string) (provider.Record, bool) {
	for _, ownership := range c.ownershipRecords(providerName, member.Hostname) {
		if provider.MatchesMemberOwnership(ownership.Target, instanceID, member) {
			return ownership, true
		}
	}
	return provider.Record{}, false
}

func (c *recordCache) legacyOwnershipRecord(providerName, hostname, instanceID string) (provider.Record, bool) {
	for _, ownership := range c.ownershipRecords(providerName, hostname) {
		if provider.MatchesLegacyOwnership(ownership.Target, instanceID) {
			return ownership, true
		}
	}
	return provider.Record{}, false
}

func (c *recordCache) ownershipRecords(providerName, hostname string) []provider.Record {
	byHostname, exists := c.records[providerName]
	if !exists || byHostname == nil {
		return nil
	}
	normalized := source.NormalizeHostname(provider.OwnershipRecordName(hostname))
	var ownership []provider.Record
	for _, record := range byHostname[normalized] {
		if record.Type == provider.RecordTypeTXT {
			ownership = append(ownership, record)
		}
	}
	return ownership
}

func (c *recordCache) providerAvailable(providerName string) bool {
	records, exists := c.records[providerName]
	return exists && records != nil
}

func (c *recordCache) ownedMembers(providerName, instanceID string) []cachedOwnedMember {
	byHostname, exists := c.records[providerName]
	if !exists || byHostname == nil {
		return nil
	}
	var owned []cachedOwnedMember
	for _, records := range byHostname {
		for _, marker := range records {
			if marker.Type != provider.RecordTypeTXT || !provider.IsOwnershipRecord(marker.Hostname) {
				continue
			}
			isOwned, markerInstance, member, metadata := provider.ParseMemberOwnershipValue(marker.Target)
			if !isOwned || markerInstance != instanceID || member == nil {
				continue
			}
			member.Hostname = provider.ExtractHostnameFromOwnership(marker.Hostname)
			if member.Hostname == "" {
				continue
			}
			owned = append(owned, cachedOwnedMember{Member: *member, Marker: marker, Metadata: metadata})
		}
	}
	return owned
}
