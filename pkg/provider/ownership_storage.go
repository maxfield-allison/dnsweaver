package provider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/maxfield-allison/dnsweaver/internal/metrics"
)

// OwnershipStateReader narrows rereads to ownership TXT when the provider API
// supports it. It must return a complete listing, including all owners' manifests.
type OwnershipStateReader interface {
	ListOwnershipState(context.Context) ([]Record, error)
}

// OwnershipRecordReader can read a staged page before a manifest references it.
type OwnershipRecordReader interface {
	ReadOwnershipRecord(context.Context, string) ([]Record, error)
}

// OwnershipDiscoveryProvider uses a known anchor when zone enumeration is absent.
// Its backend must preserve other TXT values during exact insert/delete operations.
type OwnershipDiscoveryProvider interface {
	OwnershipDiscoveryName() string
}

// OwnershipRRsetReplacer identifies APIs whose member operations replace a
// whole RRset. Observed foreign siblings make those data mutations unsafe.
type OwnershipRRsetReplacer interface {
	OwnershipReplacesRRset() bool
}

// OwnershipZoneResolver resolves the DNS zone name when backend identity uses
// a provider-specific zone ID. It uses the existing DNS credential only.
type OwnershipZoneResolver interface {
	OwnershipZone(context.Context) (string, error)
}

// OwnershipMigrator moves this owner's legacy markers into verified compact
// state. The reconciler invokes it only outside dry-run with tracking enabled.
type OwnershipMigrator interface {
	MigrateOwnership(context.Context) error
}

// SharedOwnership retains a DNS member while another logical owner claims it.
// ReleaseSharedOwnership withdraws only this owner's claim when retention applies.
type SharedOwnership interface {
	CanShareOwnership(context.Context, Record, Record) (bool, error)
	ReleaseSharedOwnership(context.Context, Record) (bool, error)
}

// CompactOwnershipProvider stores logical ownership TXT in owner-scoped pages.
// Its Provider interface presents logical markers to the existing reconciler.
// The mutex isolates aliases within one process; it is not a distributed lock.
type CompactOwnershipProvider struct {
	Provider
	owner    string
	zone     string
	mu       *sync.Mutex
	logger   *slog.Logger
	maxBytes int
	legacy   bool
	identity ProviderIdentity
}

// NewCompactOwnershipProvider decorates a TXT-capable, zone-enumerating provider.
// Callers sharing a physical backend and owner must supply the same local mutex.
// No state is persisted outside DNS. An empty owner retains single-instance semantics.
func NewCompactOwnershipProvider(backend Provider, owner, zone string, logger *slog.Logger, mutex *sync.Mutex) (*CompactOwnershipProvider, error) {
	if backend == nil || !backend.Capabilities().SupportsOwnershipTXT || len(owner) > 63 || strings.ContainsAny(owner, ",=\r\n") {
		return nil, fmt.Errorf("invalid compact ownership provider or owner")
	}
	if zone == "" {
		if _, ok := backend.(OwnershipZoneResolver); !ok {
			return nil, fmt.Errorf("compact ownership requires a DNS zone name")
		}
	}
	if mutex == nil {
		mutex = &sync.Mutex{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	if enabler, ok := backend.(interface{ EnableCompactOwnership() }); ok {
		enabler.EnableCompactOwnership()
	}
	return &CompactOwnershipProvider{Provider: backend, owner: owner, zone: normalizeOwnershipName(zone), mu: mutex, logger: logger, maxBytes: DefaultOwnershipPageBytes, identity: IdentityOf(backend)}, nil
}

// Identity preserves the physical backend rather than the decorator's name.
func (p *CompactOwnershipProvider) Identity() ProviderIdentity { return p.identity }

func (p *CompactOwnershipProvider) resolveZone(ctx context.Context) (string, error) {
	if p.zone != "" {
		return p.zone, nil
	}
	resolver, ok := p.Provider.(OwnershipZoneResolver)
	if !ok {
		return "", fmt.Errorf("%w: provider cannot resolve a DNS zone", ErrOwnershipState)
	}
	zone, err := resolver.OwnershipZone(ctx)
	if err != nil {
		return "", fmt.Errorf("resolving ownership zone: %w", err)
	}
	if zone == "" {
		return "", fmt.Errorf("%w: missing ownership zone", ErrOwnershipState)
	}
	return normalizeOwnershipName(zone), nil
}

func (p *CompactOwnershipProvider) stateRecords(ctx context.Context) ([]Record, error) {
	if reader, ok := p.Provider.(OwnershipStateReader); ok {
		return reader.ListOwnershipState(ctx)
	}
	return p.Provider.List(ctx)
}

func (p *CompactOwnershipProvider) snapshot(ctx context.Context) (*ownershipSnapshot, error) {
	metrics.OwnershipStateValid.WithLabelValues(p.Name()).Set(0)
	zone, err := p.resolveZone(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := p.stateRecords(ctx)
	if err != nil {
		return nil, err
	}
	snapshot, err := decodeOwnershipSnapshot(zone, raw)
	if err != nil {
		return nil, err
	}
	second, err := p.stateRecords(ctx)
	if err != nil {
		return nil, err
	}
	confirmed, err := decodeOwnershipSnapshot(zone, second)
	if err != nil {
		return nil, err
	}
	if !ownershipHeadsMatch(snapshot, confirmed) {
		return nil, fmt.Errorf("%w: ownership changed while reading", ErrOwnershipState)
	}
	p.observeSnapshot(confirmed)
	return confirmed, nil
}

func (p *CompactOwnershipProvider) observeSnapshot(snapshot *ownershipSnapshot) {
	mode := "compact"
	if p.legacy {
		mode = "legacy"
	}
	metrics.OwnershipStorageMode.WithLabelValues(p.Name(), mode).Set(1)
	metrics.OwnershipStateValid.WithLabelValues(p.Name()).Set(1)
	metrics.OwnershipEntries.WithLabelValues(p.Name()).Set(float64(len(snapshot.entries[p.owner])))
	count := 0
	ownerHash := ownershipDigest(p.owner)[:16]
	for _, record := range snapshot.raw {
		if record.Type != RecordTypeTXT {
			continue
		}
		if IsOwnershipRecord(record.Hostname) && MatchesOwnership(record.Target, p.owner) {
			count++
			continue
		}
		if strings.HasPrefix(normalizeOwnershipName(record.Hostname), ownershipPageLabel+"."+ownerHash+".") {
			count++
			continue
		}
		if IsOwnershipStateRecord(record.Hostname) && strings.HasPrefix(record.Target, ownershipManifestPrefix) {
			manifest, err := DecodeOwnershipManifest(record.Target)
			if err == nil && manifest.Owner == p.owner {
				count++
			}
		}
	}
	metrics.OwnershipStateRecords.WithLabelValues(p.Name()).Set(float64(count))
}

// List hides physical state records and exposes complete logical ownership.
func (p *CompactOwnershipProvider) List(ctx context.Context) ([]Record, error) {
	metrics.OwnershipStateValid.WithLabelValues(p.Name()).Set(0)
	zone, err := p.resolveZone(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := p.Provider.List(ctx)
	if err != nil {
		return nil, err
	}
	first, err := decodeOwnershipSnapshot(zone, raw)
	if err != nil {
		return nil, err
	}
	confirmed, err := p.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if !ownershipHeadsMatch(first, confirmed) {
		metrics.OwnershipStateValid.WithLabelValues(p.Name()).Set(0)
		return nil, fmt.Errorf("%w: ownership changed during record listing", ErrOwnershipState)
	}
	return ExpandOwnershipState(zone, raw)
}

// Create intercepts ownership markers; normal data still uses the provider API.
func (p *CompactOwnershipProvider) Create(ctx context.Context, record Record) error {
	if IsOwnershipStateRecord(record.Hostname) {
		return fmt.Errorf("reserved compact ownership name")
	}
	if record.Type == RecordTypeTXT && IsOwnershipRecord(record.Hostname) {
		if p.legacy {
			if err := validateOwnershipEntry(OwnershipEntry{normalizeOwnershipName(record.Hostname), record.Target}, p.owner); err != nil {
				return err
			}
			return p.Provider.Create(ctx, record)
		}
		return p.changeEntry(ctx, record, true)
	}
	if err := p.checkForeignMutation(ctx, record, nil, false); err != nil {
		return err
	}
	return p.Provider.Create(ctx, record)
}

// Delete removes logical ownership entries without exposing their physical pages.
func (p *CompactOwnershipProvider) Delete(ctx context.Context, record Record) error {
	if IsOwnershipStateRecord(record.Hostname) {
		return fmt.Errorf("reserved compact ownership name")
	}
	if record.Type == RecordTypeTXT && IsOwnershipRecord(record.Hostname) {
		if p.legacy {
			if !MatchesOwnership(record.Target, p.owner) {
				return fmt.Errorf("%w: cannot remove a foreign claim", ErrOwnershipState)
			}
			return p.Provider.Delete(ctx, record)
		}
		return p.changeEntry(ctx, record, false)
	}
	if err := p.checkForeignMutation(ctx, record, &record, false); err != nil {
		return err
	}
	return p.Provider.Delete(ctx, record)
}

// Update preserves provider-specific updates and rejects known foreign claims.
func (p *CompactOwnershipProvider) Update(ctx context.Context, existing, desired Record) error {
	if IsOwnershipStateRecord(existing.Hostname) || IsOwnershipStateRecord(desired.Hostname) {
		return fmt.Errorf("reserved compact ownership name")
	}
	if err := p.checkForeignMutation(ctx, desired, &existing, false); err != nil {
		return err
	}
	updater, ok := p.Provider.(Updater)
	if !ok {
		if err := p.Provider.Delete(ctx, existing); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		return p.Provider.Create(ctx, desired)
	}
	return updater.Update(ctx, existing, desired)
}

func (p *CompactOwnershipProvider) RecordNeedsUpdate(existing, desired Record) bool {
	if comparer, ok := p.Provider.(RecordComparer); ok {
		return comparer.RecordNeedsUpdate(existing, desired)
	}
	return false
}

func (p *CompactOwnershipProvider) Close() error {
	if closer, ok := p.Provider.(Closer); ok {
		return closer.Close()
	}
	return nil
}

func (p *CompactOwnershipProvider) changeEntry(ctx context.Context, record Record, add bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry := OwnershipEntry{normalizeOwnershipName(record.Hostname), record.Target}
	if err := validateOwnershipEntry(entry, p.owner); err != nil {
		return err
	}
	if add {
		_, _, member, _ := ParseMemberOwnershipValue(entry.Value)
		if member != nil {
			member.Hostname = ExtractHostnameFromOwnership(entry.Name)
			if err := p.checkForeignMutation(ctx, *member, nil, true); err != nil {
				return fmt.Errorf("%w: claim rejected: %s", ErrOwnershipState, err.Error())
			}
		}
	}
	snapshot, err := p.snapshot(ctx)
	if err != nil {
		return err
	}
	entries := append([]OwnershipEntry{}, snapshot.entries[p.owner]...)
	index := slices.Index(entries, entry)
	if add && index < 0 {
		entries = append(entries, entry)
	} else if !add && index >= 0 {
		entries = slices.Delete(entries, index, index+1)
	} else {
		return nil
	}
	return p.publish(ctx, snapshot, entries)
}

func (p *CompactOwnershipProvider) publish(ctx context.Context, previous *ownershipSnapshot, entries []OwnershipEntry) error {
	zone, err := p.resolveZone(ctx)
	if err != nil {
		return err
	}
	pages, err := packOwnershipPages(zone, p.owner, entries, p.maxBytes)
	if err != nil {
		return err
	}
	head := previous.heads[p.owner]
	if head.manifest.Revision == ^uint64(0) {
		return fmt.Errorf("%w: exhausted ownership revision", ErrOwnershipState)
	}
	manifest := OwnershipManifest{Version: 1, Owner: p.owner, Revision: head.manifest.Revision + 1, Previous: head.token, Pages: []string{}, Entries: len(entries)}
	for _, page := range pages {
		manifest.Pages = append(manifest.Pages, ownershipDigest(page.Target))
	}
	value, err := encodeOwnershipManifest(manifest)
	if err != nil {
		return err
	}
	name := OwnershipOwnerManifestName(zone, p.owner)
	if discovery, ok := p.Provider.(OwnershipDiscoveryProvider); ok {
		name = discovery.OwnershipDiscoveryName()
	}
	if !validOwnershipName(name) {
		return fmt.Errorf("%w: invalid ownership manifest DNS name", ErrOwnershipState)
	}
	// All size/shape checks precede the first provider mutation.
	for _, page := range pages {
		if pageErr := p.createVerified(ctx, page); pageErr != nil {
			return pageErr
		}
	}
	current, err := p.snapshot(ctx)
	if err != nil {
		return err
	}
	if current.heads[p.owner].token != head.token {
		return fmt.Errorf("%w: owner changed before publication", ErrOwnershipState)
	}
	record := Record{Hostname: name, Type: RecordTypeTXT, Target: value, TTL: 300}
	if publishErr := p.createVerified(ctx, record); publishErr != nil {
		return publishErr
	}
	committed, err := p.snapshot(ctx)
	if err != nil {
		return err
	}
	if committed.heads[p.owner].token != ownershipDigest(value) {
		return fmt.Errorf("%w: publication could not be confirmed", ErrOwnershipState)
	}
	p.cleanHistory(ctx, committed)
	p.logger.Debug("committed compact ownership", "provider", p.Name(), "owner", p.owner, "entries", len(entries), "pages", len(pages), "revision", manifest.Revision)
	return nil
}

func (p *CompactOwnershipProvider) createVerified(ctx context.Context, record Record) error {
	raw, err := p.readOwnershipRecord(ctx, record.Hostname)
	if err != nil {
		return err
	}
	if exactOwnershipRecordPresent(raw, record) {
		return nil
	}
	writeErr := p.Provider.Create(ctx, record)
	raw, err = p.readOwnershipRecord(ctx, record.Hostname)
	if err == nil && exactOwnershipRecordPresent(raw, record) {
		return nil
	}
	if writeErr != nil {
		if errors.Is(writeErr, ErrConflict) {
			return fmt.Errorf("%w: ownership conflict without matching readback", ErrOwnershipState)
		}
		return fmt.Errorf("publishing ownership record: %w", writeErr)
	}
	if err != nil {
		return fmt.Errorf("verifying ownership record: %w", err)
	}
	return fmt.Errorf("%w: ownership write readback mismatch", ErrOwnershipState)
}

func (p *CompactOwnershipProvider) readOwnershipRecord(ctx context.Context, name string) ([]Record, error) {
	if reader, ok := p.Provider.(OwnershipRecordReader); ok {
		return reader.ReadOwnershipRecord(ctx, name)
	}
	return p.stateRecords(ctx)
}

func exactOwnershipRecordPresent(records []Record, desired Record) bool {
	for _, record := range records {
		if record.Type == desired.Type && normalizeOwnershipName(record.Hostname) == normalizeOwnershipName(desired.Hostname) && record.Target == desired.Target {
			return true
		}
	}
	return false
}

func (p *CompactOwnershipProvider) cleanHistory(ctx context.Context, snapshot *ownershipSnapshot) {
	head := snapshot.heads[p.owner]
	zone, err := p.resolveZone(ctx)
	if err != nil {
		return // The next reconciliation reports zone-resolution failures.
	}
	keep := make(map[string]bool)
	for _, digest := range head.manifest.Pages {
		keep[OwnershipPageName(zone, p.owner, digest)] = true
	}
	history := append([]ownershipHead{}, snapshot.history[p.owner]...)
	slices.SortFunc(history, func(a, b ownershipHead) int {
		if a.manifest.Revision < b.manifest.Revision {
			return -1
		}
		if a.manifest.Revision > b.manifest.Revision {
			return 1
		}
		return 0
	})
	for _, old := range history {
		if old.token == head.token {
			continue
		}
		for _, digest := range old.manifest.Pages {
			name := OwnershipPageName(zone, p.owner, digest)
			if keep[name] {
				continue
			}
			current, err := p.snapshot(ctx)
			if err != nil || current.heads[p.owner].token != head.token {
				return
			}
			for _, record := range current.raw {
				if record.Type != RecordTypeTXT || normalizeOwnershipName(record.Hostname) != name || ownershipDigest(record.Target) != digest {
					continue
				}
				if err := p.Provider.Delete(ctx, record); err != nil && !errors.Is(err, ErrNotFound) {
					p.logger.Warn("compact ownership cleanup incomplete", "provider", p.Name(), "operation", "page", "error", err)
					return
				}
			}
		}
		current, err := p.snapshot(ctx)
		if err != nil || current.heads[p.owner].token != head.token {
			return
		}
		if err := p.Provider.Delete(ctx, old.record); err != nil && !errors.Is(err, ErrNotFound) {
			p.logger.Warn("compact ownership cleanup incomplete", "provider", p.Name(), "operation", "manifest", "error", err)
			return
		}
	}
}

// MigrateOwnership verifies a compact copy before deleting exact legacy markers.
// Legacy hostname-level markers retain their existing conservative semantics.
func (p *CompactOwnershipProvider) MigrateOwnership(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	snapshot, err := p.snapshot(ctx)
	if err != nil {
		return err
	}
	if p.legacy {
		return p.restoreLegacy(ctx, snapshot)
	}
	entries := append([]OwnershipEntry{}, snapshot.entries[p.owner]...)
	var legacy []Record
	for _, record := range snapshot.raw {
		if record.Type != RecordTypeTXT || !IsOwnershipRecord(record.Hostname) || !MatchesOwnership(record.Target, p.owner) {
			continue
		}
		entry := OwnershipEntry{normalizeOwnershipName(record.Hostname), record.Target}
		if !slices.Contains(entries, entry) {
			entries = append(entries, entry)
		}
		legacy = append(legacy, record)
	}
	if len(legacy) == 0 {
		p.cleanHistory(ctx, snapshot)
		return nil
	}
	if !slices.Equal(entries, snapshot.entries[p.owner]) {
		if err := p.publish(ctx, snapshot, entries); err != nil {
			return err
		}
	}
	for _, marker := range legacy {
		if err := p.Provider.Delete(ctx, marker); err != nil && !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("removing migrated ownership marker: %w", err)
		}
	}
	return nil
}

func (p *CompactOwnershipProvider) restoreLegacy(ctx context.Context, snapshot *ownershipSnapshot) error {
	for _, entry := range snapshot.entries[p.owner] {
		marker := Record{Hostname: entry.Name, Type: RecordTypeTXT, Target: entry.Value, TTL: 300}
		if err := p.createVerified(ctx, marker); err != nil {
			return fmt.Errorf("restoring legacy marker: %w", err)
		}
	}
	if len(snapshot.entries[p.owner]) > 0 {
		return p.publish(ctx, snapshot, nil)
	}
	p.cleanHistory(ctx, snapshot)
	return nil
}

func (p *CompactOwnershipProvider) foreignClaims(snapshot *ownershipSnapshot, record Record) []Record {
	var markers []Record
	for owner, entries := range snapshot.entries {
		if owner == p.owner {
			continue
		}
		for _, entry := range entries {
			markers = append(markers, Record{Hostname: entry.Name, Type: RecordTypeTXT, Target: entry.Value})
		}
	}
	for _, marker := range snapshot.raw {
		if marker.Type == RecordTypeTXT && IsOwnershipRecord(marker.Hostname) && !MatchesOwnership(marker.Target, p.owner) {
			markers = append(markers, marker)
		}
	}
	var claims []Record
	for _, marker := range markers {
		if normalizeOwnershipName(ExtractHostnameFromOwnership(marker.Hostname)) == normalizeOwnershipName(record.Hostname) && IsDnsweaverOwned(marker.Target) {
			claims = append(claims, marker)
		}
	}
	return claims
}

func (p *CompactOwnershipProvider) checkForeignMutation(ctx context.Context, desired Record, existing *Record, claimOnly bool) error {
	snapshot, err := p.snapshot(ctx)
	if err != nil {
		return err
	}
	for _, marker := range p.foreignClaims(snapshot, desired) {
		_, _, member, _ := ParseMemberOwnershipValue(marker.Target)
		if member != nil {
			member.Hostname = desired.Hostname
		}
		if replacer, ok := p.Provider.(OwnershipRRsetReplacer); !claimOnly && ok && replacer.OwnershipReplacesRRset() && member != nil && member.Type == desired.Type {
			return fmt.Errorf("%w: provider replaces an RRset with foreign members", ErrConflict)
		}
		if member == nil || existing != nil && SameRecordMember(*member, *existing) || (member.Type == RecordTypeCNAME || desired.Type == RecordTypeCNAME) && !SameRecordMember(*member, desired) {
			return fmt.Errorf("%w: DNS name is claimed by another owner", ErrConflict)
		}
	}
	return nil
}

// CanShareOwnership accepts only exact, unchanged members with an existing
// foreign member claim. It does not adopt unrelated or legacy hostname claims.
func (p *CompactOwnershipProvider) CanShareOwnership(ctx context.Context, existing, desired Record) (bool, error) {
	if !SameRecordMember(existing, desired) || existing.TTL != desired.TTL || p.RecordNeedsUpdate(existing, desired) {
		return false, nil
	}
	snapshot, err := p.snapshot(ctx)
	if err != nil {
		return false, err
	}
	shared := false
	for _, marker := range p.foreignClaims(snapshot, desired) {
		_, _, member, _ := ParseMemberOwnershipValue(marker.Target)
		if member == nil {
			return false, nil
		}
		member.Hostname = desired.Hostname
		shared = shared || SameRecordMember(*member, desired)
	}
	return shared, nil
}

// ReleaseSharedOwnership commits withdrawal before returning retention. It never
// deletes the physical answer, including when the other claim uses legacy TXT.
func (p *CompactOwnershipProvider) ReleaseSharedOwnership(ctx context.Context, record Record) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	snapshot, err := p.snapshot(ctx)
	if err != nil {
		return false, err
	}
	retain := false
	for _, marker := range p.foreignClaims(snapshot, record) {
		_, _, member, _ := ParseMemberOwnershipValue(marker.Target)
		if member != nil {
			member.Hostname = record.Hostname
		}
		retain = retain || member == nil || SameRecordMember(*member, record)
	}
	if !retain {
		return false, nil
	}
	entries := make([]OwnershipEntry, 0, len(snapshot.entries[p.owner]))
	for _, entry := range snapshot.entries[p.owner] {
		if normalizeOwnershipName(ExtractHostnameFromOwnership(entry.Name)) == normalizeOwnershipName(record.Hostname) && MatchesMemberOwnership(entry.Value, p.owner, record) {
			continue
		}
		entries = append(entries, entry)
	}
	if len(entries) != len(snapshot.entries[p.owner]) {
		if err := p.publish(ctx, snapshot, entries); err != nil {
			return false, err
		}
	}
	for _, marker := range snapshot.raw {
		if marker.Type == RecordTypeTXT && IsOwnershipRecord(marker.Hostname) && normalizeOwnershipName(ExtractHostnameFromOwnership(marker.Hostname)) == normalizeOwnershipName(record.Hostname) && MatchesMemberOwnership(marker.Target, p.owner, record) {
			if err := p.Provider.Delete(ctx, marker); err != nil && !errors.Is(err, ErrNotFound) {
				return false, err
			}
		}
	}
	return true, nil
}
