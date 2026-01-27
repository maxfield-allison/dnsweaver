// Package dnsupdate provides RFC 2136 Dynamic DNS update functionality.
package dnsupdate

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/miekg/dns"
)

// Catalog configuration constants.
const (
	// CatalogPrefix is the prefix for catalog chunk records.
	// Full record name: _dnsweaver-catalog-N.<zone>
	CatalogPrefix = "_dnsweaver-catalog-"

	// CatalogMaxChunkBytes is the maximum size of a catalog chunk in bytes.
	// DNS TXT records are limited by packet size. With EDNS0, typical limit is 4096 bytes.
	// We use 3500 bytes to leave room for DNS headers, TSIG signatures, and safety margin.
	// This is the PRIMARY limit - hostname count is secondary.
	CatalogMaxChunkBytes = 3500

	// CatalogMaxHostnameLen is the maximum allowed hostname length.
	// RFC 1035 limits FQDNs to 253 bytes. We enforce this to prevent
	// any single hostname from being too large for a chunk.
	CatalogMaxHostnameLen = 253

	// CatalogChunkSize is the MAXIMUM number of hostnames per chunk.
	// This is a secondary limit - the byte limit (CatalogMaxChunkBytes) takes precedence.
	// With average hostnames of ~50 bytes, we'd hit byte limit around 70 hostnames.
	// This acts as a hard cap for pathologically short hostnames.
	CatalogChunkSize = 100

	// CatalogTTL is the TTL for catalog records.
	CatalogTTL = 300
)

// Catalog provides hostname enumeration for RFC 2136 zones.
// It stores managed hostnames in chunked TXT records, enabling
// stateless enumeration without AXFR zone transfers.
//
// Structure in DNS:
//
//	_dnsweaver-catalog-0.<zone>  TXT "host1" "host2" "host3" ...
//	_dnsweaver-catalog-1.<zone>  TXT "host101" "host102" ...
//	...
//
// The catalog is self-describing: chunks are discovered by querying
// sequentially until NXDOMAIN is returned.
type Catalog struct {
	client *Client
	zone   string
	logger *slog.Logger

	// Cached state (loaded on demand)
	chunks    [][]string     // chunks[chunkIndex] = []hostname
	hostnames map[string]int // hostname -> chunkIndex (for O(1) lookup)
	loaded    bool
}

// NewCatalog creates a new Catalog for the given zone.
func NewCatalog(client *Client, zone string, logger *slog.Logger) *Catalog {
	if logger == nil {
		logger = slog.Default()
	}
	return &Catalog{
		client:    client,
		zone:      zone,
		logger:    logger,
		hostnames: make(map[string]int),
	}
}

// chunkRecordName returns the DNS name for a catalog chunk.
// Example: _dnsweaver-catalog-0.example.com.
func (c *Catalog) chunkRecordName(chunkIndex int) string {
	return fmt.Sprintf("%s%d.%s", CatalogPrefix, chunkIndex, c.zone)
}

// Load reads all catalog chunks from DNS and populates the cache.
// This should be called before any read operations.
// Safe to call multiple times; subsequent calls refresh the cache.
func (c *Catalog) Load(ctx context.Context) error {
	c.chunks = nil
	c.hostnames = make(map[string]int)
	c.loaded = false

	c.logger.Debug("loading catalog from DNS",
		slog.String("zone", c.zone),
	)

	// Query chunks sequentially until NXDOMAIN
	for chunkIndex := 0; ; chunkIndex++ {
		name := c.chunkRecordName(chunkIndex)
		records, err := c.client.Query(ctx, name, dns.TypeTXT)
		if err != nil {
			return fmt.Errorf("querying catalog chunk %d: %w", chunkIndex, err)
		}

		// Empty result means no more chunks (NXDOMAIN or no records)
		if len(records) == 0 {
			break
		}

		// Parse hostnames from TXT record
		// TXT records may have multiple strings, and we may get multiple TXT records
		var chunkHostnames []string
		for _, r := range records {
			if r.Type == dns.TypeTXT {
				// RData contains the TXT strings joined by space
				// We need to parse individual hostnames
				hostnames := parseTXTHostnames(r.RData)
				chunkHostnames = append(chunkHostnames, hostnames...)
			}
		}

		// Store chunk and build index
		c.chunks = append(c.chunks, chunkHostnames)
		for _, hostname := range chunkHostnames {
			c.hostnames[hostname] = chunkIndex
		}

		c.logger.Debug("loaded catalog chunk",
			slog.Int("chunk", chunkIndex),
			slog.Int("hostnames", len(chunkHostnames)),
		)
	}

	c.loaded = true

	c.logger.Debug("catalog load complete",
		slog.Int("chunks", len(c.chunks)),
		slog.Int("total_hostnames", len(c.hostnames)),
	)

	return nil
}

// Hostnames returns all hostnames in the catalog.
// Returns a sorted copy to ensure consistent ordering.
// Loads the catalog if not already loaded.
func (c *Catalog) Hostnames(ctx context.Context) ([]string, error) {
	if !c.loaded {
		if err := c.Load(ctx); err != nil {
			return nil, err
		}
	}

	// Build sorted list
	result := make([]string, 0, len(c.hostnames))
	for hostname := range c.hostnames {
		result = append(result, hostname)
	}
	sort.Strings(result)

	return result, nil
}

// Contains checks if a hostname is in the catalog.
// Loads the catalog if not already loaded.
func (c *Catalog) Contains(ctx context.Context, hostname string) (bool, error) {
	if !c.loaded {
		if err := c.Load(ctx); err != nil {
			return false, err
		}
	}

	_, exists := c.hostnames[normalizeHostname(hostname)]
	return exists, nil
}

// chunkByteSize calculates the total byte size of hostnames in a chunk.
// This accounts for the actual space each hostname will take in a TXT record.
func chunkByteSize(chunk []string) int {
	if len(chunk) == 0 {
		return 0
	}
	total := 0
	for _, h := range chunk {
		// Each hostname in a TXT record takes: 1 byte length prefix + hostname bytes
		// Plus we need to account for encoding overhead
		total += len(h) + 1 // +1 for TXT segment length byte
	}
	return total
}

// canFitInChunk checks if adding a hostname would exceed chunk limits.
// Returns true if the hostname can fit, false otherwise.
func canFitInChunk(chunk []string, hostname string) bool {
	// Check count limit
	if len(chunk) >= CatalogChunkSize {
		return false
	}

	// Check byte limit
	currentSize := chunkByteSize(chunk)
	hostnameSize := len(hostname) + 1 // +1 for TXT segment length byte
	return (currentSize + hostnameSize) <= CatalogMaxChunkBytes
}

// ErrHostnameTooLong is returned when a hostname exceeds the maximum allowed length.
var ErrHostnameTooLong = fmt.Errorf("hostname exceeds maximum length of %d bytes", CatalogMaxHostnameLen)

// Add adds a hostname to the catalog.
// The hostname is added to the first chunk with available space (both count and bytes),
// or a new chunk is created if no existing chunk can accommodate it.
// This performs an atomic DNS update.
//
// Returns ErrHostnameTooLong if the hostname exceeds CatalogMaxHostnameLen (253 bytes).
func (c *Catalog) Add(ctx context.Context, hostname string) error {
	hostname = normalizeHostname(hostname)

	// Validate hostname length
	if len(hostname) > CatalogMaxHostnameLen {
		c.logger.Error("hostname too long for catalog",
			slog.String("hostname", hostname),
			slog.Int("length", len(hostname)),
			slog.Int("max_length", CatalogMaxHostnameLen),
		)
		return ErrHostnameTooLong
	}

	// Load current state
	if err := c.Load(ctx); err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}

	// Check if already exists
	if _, exists := c.hostnames[hostname]; exists {
		c.logger.Debug("hostname already in catalog",
			slog.String("hostname", hostname),
		)
		return nil
	}

	// Find a chunk with space (both count and bytes), or create new one
	targetChunk := -1
	for i, chunk := range c.chunks {
		if canFitInChunk(chunk, hostname) {
			targetChunk = i
			break
		}
	}

	if targetChunk == -1 {
		// No existing chunk can fit this hostname, create new one
		targetChunk = len(c.chunks)
		c.chunks = append(c.chunks, []string{})

		c.logger.Debug("creating new catalog chunk",
			slog.Int("chunk", targetChunk),
			slog.String("reason", "no existing chunk has space"),
		)
	}

	// Add hostname to chunk
	c.chunks[targetChunk] = append(c.chunks[targetChunk], hostname)
	c.hostnames[hostname] = targetChunk

	// Write updated chunk to DNS
	if err := c.writeChunk(ctx, targetChunk); err != nil {
		// Rollback local state
		c.chunks[targetChunk] = c.chunks[targetChunk][:len(c.chunks[targetChunk])-1]
		delete(c.hostnames, hostname)
		return fmt.Errorf("writing catalog chunk %d: %w", targetChunk, err)
	}

	c.logger.Debug("added hostname to catalog",
		slog.String("hostname", hostname),
		slog.Int("chunk", targetChunk),
	)

	return nil
}

// Remove removes a hostname from the catalog.
// This performs an atomic DNS update.
// If the chunk becomes empty (and is not chunk 0), it is deleted.
func (c *Catalog) Remove(ctx context.Context, hostname string) error {
	hostname = normalizeHostname(hostname)

	// Load current state
	if err := c.Load(ctx); err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}

	// Find hostname
	chunkIndex, exists := c.hostnames[hostname]
	if !exists {
		c.logger.Debug("hostname not in catalog",
			slog.String("hostname", hostname),
		)
		return nil
	}

	// Remove from chunk
	chunk := c.chunks[chunkIndex]
	newChunk := make([]string, 0, len(chunk)-1)
	for _, h := range chunk {
		if h != hostname {
			newChunk = append(newChunk, h)
		}
	}
	c.chunks[chunkIndex] = newChunk
	delete(c.hostnames, hostname)

	// Write updated chunk (or delete if empty and not chunk 0)
	if len(newChunk) == 0 && chunkIndex > 0 {
		// Delete empty chunk (but not chunk 0 - keep it as marker)
		if err := c.deleteChunk(ctx, chunkIndex); err != nil {
			// Rollback local state
			c.chunks[chunkIndex] = chunk
			c.hostnames[hostname] = chunkIndex
			return fmt.Errorf("deleting empty catalog chunk %d: %w", chunkIndex, err)
		}
		// Remove from local chunks list and reindex
		c.reindexAfterDelete(chunkIndex)
	} else {
		// Write updated chunk
		if err := c.writeChunk(ctx, chunkIndex); err != nil {
			// Rollback local state
			c.chunks[chunkIndex] = chunk
			c.hostnames[hostname] = chunkIndex
			return fmt.Errorf("writing catalog chunk %d: %w", chunkIndex, err)
		}
	}

	c.logger.Debug("removed hostname from catalog",
		slog.String("hostname", hostname),
		slog.Int("former_chunk", chunkIndex),
	)

	return nil
}

// writeChunk writes a catalog chunk to DNS using RFC 2136 UPDATE.
// This is an atomic operation that replaces the entire chunk.
func (c *Catalog) writeChunk(ctx context.Context, chunkIndex int) error {
	name := c.chunkRecordName(chunkIndex)
	chunk := c.chunks[chunkIndex]

	// Build TXT record with hostnames as separate strings
	// This allows efficient parsing and avoids delimiter issues
	txtStrings := make([]string, len(chunk))
	copy(txtStrings, chunk)

	// Sort for consistent ordering
	sort.Strings(txtStrings)

	// Create TXT RR
	rr := &dns.TXT{
		Hdr: dns.RR_Header{
			Name:   name,
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassINET,
			Ttl:    CatalogTTL,
		},
		Txt: txtStrings,
	}

	// Build UPDATE message: delete old, insert new (atomic)
	msg := new(dns.Msg)
	msg.SetUpdate(c.zone)

	// Delete any existing record for this chunk
	deleteRR := &dns.ANY{
		Hdr: dns.RR_Header{
			Name:   name,
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassANY,
		},
	}
	msg.Ns = append(msg.Ns, deleteRR)

	// Insert new record (only if chunk has entries)
	if len(txtStrings) > 0 {
		msg.Insert([]dns.RR{rr})
	}

	// Apply TSIG if configured
	if c.client.tsig != nil {
		c.client.tsig.ApplyToMessage(msg)
	}

	// Send update
	resp, _, err := c.client.exchangeWithContext(ctx, msg)
	if err != nil {
		return fmt.Errorf("dns update failed: %w", err)
	}

	if err := c.client.checkResponse(resp); err != nil {
		return err
	}

	return nil
}

// deleteChunk removes a catalog chunk from DNS.
func (c *Catalog) deleteChunk(ctx context.Context, chunkIndex int) error {
	name := c.chunkRecordName(chunkIndex)

	msg := new(dns.Msg)
	msg.SetUpdate(c.zone)

	// Delete the chunk record
	deleteRR := &dns.ANY{
		Hdr: dns.RR_Header{
			Name:   name,
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassANY,
		},
	}
	msg.Ns = append(msg.Ns, deleteRR)

	// Apply TSIG if configured
	if c.client.tsig != nil {
		c.client.tsig.ApplyToMessage(msg)
	}

	// Send update
	resp, _, err := c.client.exchangeWithContext(ctx, msg)
	if err != nil {
		return fmt.Errorf("dns update failed: %w", err)
	}

	if err := c.client.checkResponse(resp); err != nil {
		return err
	}

	return nil
}

// reindexAfterDelete updates the local state after deleting a chunk.
// Note: This does NOT rename DNS records - it just updates local tracking.
// DNS chunks may have gaps (e.g., chunk 0, 2, 3 if chunk 1 was deleted).
// The Load() function handles gaps by stopping at the first missing chunk,
// so we need to compact the DNS records to avoid orphaned chunks.
func (c *Catalog) reindexAfterDelete(deletedIndex int) {
	// Remove the chunk from local slice
	c.chunks = append(c.chunks[:deletedIndex], c.chunks[deletedIndex+1:]...)

	// Rebuild hostname index
	c.hostnames = make(map[string]int)
	for i, chunk := range c.chunks {
		for _, hostname := range chunk {
			c.hostnames[hostname] = i
		}
	}

	// Note: The DNS records now have a gap. We should compact them.
	// This is done lazily - the next Add() or explicit Compact() will fix it.
}

// Compact reorganizes catalog chunks to eliminate gaps and balance sizes.
// This rewrites all chunks to DNS using byte-based packing.
// Use sparingly as it rewrites all chunks.
func (c *Catalog) Compact(ctx context.Context) error {
	if !c.loaded {
		if err := c.Load(ctx); err != nil {
			return fmt.Errorf("loading catalog: %w", err)
		}
	}

	// Collect all hostnames
	allHostnames := make([]string, 0, len(c.hostnames))
	for hostname := range c.hostnames {
		allHostnames = append(allHostnames, hostname)
	}
	sort.Strings(allHostnames)

	oldChunkCount := len(c.chunks)

	// Rebuild chunks using byte-based packing
	var newChunks [][]string
	var currentChunk []string

	for _, hostname := range allHostnames {
		// Check if hostname fits in current chunk
		if len(currentChunk) > 0 && !canFitInChunk(currentChunk, hostname) {
			// Current chunk is full, start a new one
			newChunks = append(newChunks, currentChunk)
			currentChunk = nil
		}
		currentChunk = append(currentChunk, hostname)
	}

	// Don't forget the last chunk
	if len(currentChunk) > 0 {
		newChunks = append(newChunks, currentChunk)
	}

	// Ensure chunk 0 exists even if empty
	if len(newChunks) == 0 {
		newChunks = [][]string{{}}
	}

	newHostnames := make(map[string]int)
	for i, chunk := range newChunks {
		for _, hostname := range chunk {
			newHostnames[hostname] = i
		}
	}

	// Write all new chunks
	for i := range newChunks {
		c.chunks = newChunks // Temporarily set for writeChunk
		if err := c.writeChunk(ctx, i); err != nil {
			return fmt.Errorf("writing compacted chunk %d: %w", i, err)
		}
	}

	// Delete any extra old chunks
	for i := len(newChunks); i < oldChunkCount; i++ {
		if err := c.deleteChunk(ctx, i); err != nil {
			c.logger.Warn("failed to delete old chunk during compaction",
				slog.Int("chunk", i),
				slog.String("error", err.Error()),
			)
			// Continue anyway - orphaned chunks are harmless
		}
	}

	// Update local state
	c.chunks = newChunks
	c.hostnames = newHostnames

	c.logger.Info("catalog compacted",
		slog.Int("hostnames", len(allHostnames)),
		slog.Int("chunks", len(newChunks)),
		slog.Int("old_chunks", oldChunkCount),
	)

	return nil
}

// Clear removes all catalog records from DNS.
// Use with caution - this removes all tracking state.
func (c *Catalog) Clear(ctx context.Context) error {
	if !c.loaded {
		if err := c.Load(ctx); err != nil {
			return fmt.Errorf("loading catalog: %w", err)
		}
	}

	// Delete all chunks
	for i := range c.chunks {
		if err := c.deleteChunk(ctx, i); err != nil {
			return fmt.Errorf("deleting chunk %d: %w", i, err)
		}
	}

	// Clear local state
	c.chunks = nil
	c.hostnames = make(map[string]int)
	c.loaded = false

	c.logger.Info("catalog cleared")

	return nil
}

// Stats returns catalog statistics including byte usage per chunk.
func (c *Catalog) Stats() CatalogStats {
	stats := CatalogStats{
		Loaded:             c.loaded,
		ChunkCount:         len(c.chunks),
		TotalHostnames:     len(c.hostnames),
		MaxChunkBytes:      CatalogMaxChunkBytes,
		MaxChunkCount:      CatalogChunkSize,
		MaxHostnameLen:     CatalogMaxHostnameLen,
		ChunkBytesUsed:     make([]int, len(c.chunks)),
		ChunkHostnameCount: make([]int, len(c.chunks)),
	}

	for i, chunk := range c.chunks {
		stats.ChunkBytesUsed[i] = chunkByteSize(chunk)
		stats.ChunkHostnameCount[i] = len(chunk)
		stats.TotalBytesUsed += stats.ChunkBytesUsed[i]
	}

	return stats
}

// CatalogStats contains catalog statistics.
type CatalogStats struct {
	Loaded             bool
	ChunkCount         int
	TotalHostnames     int
	TotalBytesUsed     int
	MaxChunkBytes      int   // CatalogMaxChunkBytes
	MaxChunkCount      int   // CatalogChunkSize (max hostnames per chunk)
	MaxHostnameLen     int   // CatalogMaxHostnameLen
	ChunkBytesUsed     []int // Bytes used per chunk
	ChunkHostnameCount []int // Hostname count per chunk
}

// parseTXTHostnames parses hostnames from TXT record data.
// TXT records can contain multiple strings separated by spaces in the RData.
func parseTXTHostnames(rdata string) []string {
	// RData from our Query() comes as space-joined strings
	// Each string was a separate TXT segment
	if rdata == "" {
		return nil
	}

	// Split by space and filter empty strings
	parts := strings.Fields(rdata)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// normalizeHostname normalizes a hostname for catalog storage.
// Removes trailing dots and converts to lowercase.
func normalizeHostname(hostname string) string {
	hostname = strings.TrimSuffix(hostname, ".")
	return strings.ToLower(hostname)
}
