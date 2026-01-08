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

// getExistingRecords returns cached A/CNAME records for a hostname from a specific provider.
// Returns nil if the provider cache is unavailable (failed to load).
// Returns empty slice if cached but no records exist for this hostname.
func (c *recordCache) getExistingRecords(providerName, hostname string) ([]provider.Record, bool) {
	byHostname, exists := c.records[providerName]
	if !exists || byHostname == nil {
		// Provider not cached or failed to load
		return nil, false
	}

	records := byHostname[hostname]

	// Filter to only A and CNAME records (exclude TXT, etc.)
	var filtered []provider.Record
	for _, r := range records {
		if r.Type == provider.RecordTypeA || r.Type == provider.RecordTypeCNAME {
			filtered = append(filtered, r)
		}
	}

	return filtered, true
}
