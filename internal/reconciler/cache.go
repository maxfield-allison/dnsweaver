// Package reconciler implements the core logic for comparing desired DNS state
// (from sources) with actual DNS state (from providers) and applying changes.
package reconciler

import (
	"context"
	"log/slog"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// recordCache holds a snapshot of DNS records from all providers.
// It is built once at the start of each reconciliation cycle and used
// to avoid repeated List() API calls when checking existing records.
type recordCache struct {
	// records maps provider name -> hostname -> list of records
	records map[string]map[string][]provider.Record
	logger  *slog.Logger
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

		// Index records by hostname for fast lookup
		byHostname := make(map[string][]provider.Record)
		for _, r := range providerRecords {
			byHostname[r.Hostname] = append(byHostname[r.Hostname], r)
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
func (c *recordCache) getExistingRecords(providerName, hostname string) ([]provider.Record, bool) {
	byHostname, exists := c.records[providerName]
	if !exists || byHostname == nil {
		// Provider not cached or failed to load
		return nil, false
	}

	records := byHostname[hostname]

	// Filter to DNS data records (exclude TXT ownership markers)
	var filtered []provider.Record
	for _, r := range records {
		switch r.Type {
		case provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME, provider.RecordTypeSRV:
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
func (c *recordCache) getAllRecordsForHostname(providerName, hostname string) ([]provider.Record, bool) {
	byHostname, exists := c.records[providerName]
	if !exists || byHostname == nil {
		// Provider not cached or failed to load
		return nil, false
	}

	records := byHostname[hostname]

	// Filter to data records (A, AAAA, CNAME, SRV) - exclude TXT ownership records
	var filtered []provider.Record
	for _, r := range records {
		switch r.Type {
		case provider.RecordTypeA, provider.RecordTypeAAAA, provider.RecordTypeCNAME, provider.RecordTypeSRV:
			filtered = append(filtered, r)
		case provider.RecordTypeTXT:
			// Skip TXT records (ownership markers)
		}
	}

	return filtered, true
}

// hasOwnershipRecord checks if an ownership TXT record exists for the given hostname.
// Returns false if the provider cache is unavailable.
func (c *recordCache) hasOwnershipRecord(providerName, hostname string) bool {
	byHostname, exists := c.records[providerName]
	if !exists || byHostname == nil {
		return false
	}

	ownershipName := provider.OwnershipRecordName(hostname)
	records := byHostname[ownershipName]

	for _, r := range records {
		if r.Type == provider.RecordTypeTXT && r.Target == provider.OwnershipValue {
			return true
		}
	}

	return false
}
