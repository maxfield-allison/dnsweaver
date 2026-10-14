package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
)

// stateTestDNS models exact RR insertion/deletion and disposable process state.
type stateTestDNS struct {
	mu            sync.Mutex
	records       []Record
	createFailure func(Record) error
	deleteFailure func(Record) error
	limit         int
	writes        int
}

func (*stateTestDNS) Name() string               { return "dns" }
func (*stateTestDNS) Type() string               { return "test" }
func (*stateTestDNS) Ping(context.Context) error { return nil }
func (*stateTestDNS) Capabilities() Capabilities {
	return Capabilities{SupportsOwnershipTXT: true, SupportedRecordTypes: []RecordType{RecordTypeA, RecordTypeCNAME, RecordTypeTXT, RecordTypeSRV, RecordTypeHTTPS}}
}
func (d *stateTestDNS) List(context.Context) ([]Record, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.records), nil
}
func (d *stateTestDNS) Create(_ context.Context, r Record) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes++
	if d.createFailure != nil {
		if err := d.createFailure(r); err != nil {
			return err
		}
	}
	if exactOwnershipRecordPresent(d.records, r) {
		return ErrConflict
	}
	if d.limit > 0 && len(d.records) >= d.limit {
		return fmt.Errorf("zone quota exhausted")
	}
	d.records = append(d.records, r)
	return nil
}
func (d *stateTestDNS) Delete(_ context.Context, r Record) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes++
	if d.deleteFailure != nil {
		if err := d.deleteFailure(r); err != nil {
			return err
		}
	}
	for i, existing := range d.records {
		if normalizeOwnershipName(existing.Hostname) == normalizeOwnershipName(r.Hostname) && SameRecordMember(existing, r) {
			d.records = slices.Delete(d.records, i, i+1)
			return nil
		}
	}
	return ErrNotFound
}

func stateTestProvider(t *testing.T, dns *stateTestDNS, owner string) *CompactOwnershipProvider {
	t.Helper()
	p, err := NewCompactOwnershipProvider(dns, owner, "example.com", slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func stateTestMember(i int) Record {
	return Record{Hostname: fmt.Sprintf("app-%03d.example.com", i), Type: RecordTypeA, Target: fmt.Sprintf("192.0.2.%d", i%254+1), TTL: 300}
}
func stateTestMarker(owner string, r Record) Record {
	return MemberOwnershipRecord(r.Hostname, 300, owner, r, map[string]string{"proxied": "true", "note": "preserve"})
}

func TestCompactOwnershipMigrationAndDisposableRestart(t *testing.T) {
	ctx := context.Background()
	dns := &stateTestDNS{}
	for i := range 80 {
		member := stateTestMember(i)
		dns.records = append(dns.records, member, stateTestMarker("cluster", member))
	}
	foreign := stateTestMarker("docker", stateTestMember(90))
	user := Record{Hostname: "example.com", Type: RecordTypeTXT, Target: "v=spf1 -all"}
	dns.records = append(dns.records, foreign, user)
	p := stateTestProvider(t, dns, "cluster")
	if err := p.MigrateOwnership(ctx); err != nil {
		t.Fatal(err)
	}
	raw, _ := dns.List(ctx)
	stateCount := 0
	for _, r := range raw {
		if IsOwnershipStateRecord(r.Hostname) {
			stateCount++
		}
		if IsOwnershipRecord(r.Hostname) && MatchesOwnership(r.Target, "cluster") {
			t.Fatalf("legacy marker retained: %+v", r)
		}
	}
	if stateCount != 2 {
		t.Fatalf("80 claims use %d physical TXT records, want one page plus manifest", stateCount)
	}
	if !exactOwnershipRecordPresent(raw, foreign) || !exactOwnershipRecordPresent(raw, user) {
		t.Fatal("migration changed foreign or user TXT")
	}
	// No process cache, local files or original decorator survive this restart.
	restarted := stateTestProvider(t, dns, "cluster")
	logical, err := restarted.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 80 {
		if !exactOwnershipRecordPresent(logical, stateTestMarker("cluster", stateTestMember(i))) {
			t.Fatalf("missing recovered member %d or metadata", i)
		}
	}
	if err := restarted.Delete(ctx, stateTestMarker("cluster", stateTestMember(5))); err != nil {
		t.Fatal(err)
	}
	logical, err = restarted.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if exactOwnershipRecordPresent(logical, stateTestMarker("cluster", stateTestMember(5))) {
		t.Fatal("removed claim recovered")
	}
	if !exactOwnershipRecordPresent(logical, stateTestMarker("cluster", stateTestMember(6))) {
		t.Fatal("sibling claim lost")
	}
}

func TestCompactOwnershipConcurrentIndependentOwners(t *testing.T) {
	dns := &stateTestDNS{}
	ctx := context.Background()
	var wg sync.WaitGroup
	for owner := range 4 {
		wg.Go(func() {
			p := stateTestProvider(t, dns, fmt.Sprintf("owner-%d", owner))
			for member := range 5 {
				marker := stateTestMarker(p.owner, stateTestMember(owner*10+member))
				for attempt := range 100 {
					err := p.Create(ctx, marker)
					if err == nil {
						break
					}
					if !errors.Is(err, ErrOwnershipState) || attempt == 99 {
						t.Errorf("concurrent owner write: %v", err)
						return
					}
				}
			}
		})
	}
	wg.Wait()
	logical, err := stateTestProvider(t, dns, "replacement").List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for owner := range 4 {
		for member := range 5 {
			if !exactOwnershipRecordPresent(logical, stateTestMarker(fmt.Sprintf("owner-%d", owner), stateTestMember(owner*10+member))) {
				t.Fatalf("owner %d member %d lost", owner, member)
			}
		}
	}
}

func TestCompactOwnershipSharedReleaseAndForeignProtection(t *testing.T) {
	ctx := context.Background()
	dns := &stateTestDNS{}
	a, b := stateTestProvider(t, dns, "a"), stateTestProvider(t, dns, "b")
	member := stateTestMember(1)
	if err := a.Create(ctx, member); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*CompactOwnershipProvider{a, b} {
		if err := p.Create(ctx, stateTestMarker(p.owner, member)); err != nil {
			t.Fatal(err)
		}
	}
	if shared, err := b.CanShareOwnership(ctx, member, member); err != nil || !shared {
		t.Fatalf("compatible shared claim = %v, %v", shared, err)
	}
	if err := a.Delete(ctx, member); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign claimed delete = %v", err)
	}
	if released, err := a.ReleaseSharedOwnership(ctx, member); err != nil || !released {
		t.Fatalf("release = %v, %v", released, err)
	}
	raw, _ := dns.List(ctx)
	if !exactOwnershipRecordPresent(raw, member) {
		t.Fatal("shared DNS answer deleted")
	}
	logical, err := a.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if exactOwnershipRecordPresent(logical, stateTestMarker("a", member)) || !exactOwnershipRecordPresent(logical, stateTestMarker("b", member)) {
		t.Fatal("withdrawal did not preserve exactly the other claim")
	}
	if released, err := b.ReleaseSharedOwnership(ctx, member); err != nil || released {
		t.Fatalf("last claim must delete normally: %v, %v", released, err)
	}
	if err := b.Delete(ctx, member); err != nil {
		t.Fatal(err)
	}
	if err := b.Delete(ctx, stateTestMarker("b", member)); err != nil {
		t.Fatal(err)
	}
}

func TestCompactOwnershipQuotaAndInterruptedMigrationPreserveLegacy(t *testing.T) {
	for _, failure := range []string{"page", "manifest", "quota"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			marker := stateTestMarker("a", stateTestMember(1))
			dns := &stateTestDNS{records: []Record{marker}}
			if failure == "quota" {
				dns.limit = 1
			} else {
				dns.createFailure = func(r Record) error {
					if (failure == "page" && strings.HasPrefix(r.Hostname, "_dw-p.")) || (failure == "manifest" && strings.HasPrefix(r.Hostname, "_dnsweaver-state.")) {
						return fmt.Errorf("interrupted write")
					}
					return nil
				}
			}
			p := stateTestProvider(t, dns, "a")
			if err := p.MigrateOwnership(ctx); err == nil {
				t.Fatal("migration unexpectedly succeeded")
			}
			raw, _ := dns.List(ctx)
			if !exactOwnershipRecordPresent(raw, marker) {
				t.Fatal("failed migration deleted legacy marker")
			}
			dns.createFailure = nil
			dns.limit = 0
			if err := stateTestProvider(t, dns, "a").MigrateOwnership(ctx); err != nil {
				t.Fatalf("restart migration: %v", err)
			}
		})
	}
}

func TestCompactOwnershipPublicationUsesReadbackAfterAmbiguousWrite(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			ctx := context.Background()
			dns := &stateTestDNS{}
			p := stateTestProvider(t, dns, "a")
			dns.createFailure = func(record Record) error {
				if committed {
					dns.records = append(dns.records, record)
					return context.DeadlineExceeded
				}
				return ErrConflict
			}
			marker := stateTestMarker("a", stateTestMember(1))
			err := p.Create(ctx, marker)
			if !committed {
				if !errors.Is(err, ErrOwnershipState) || errors.Is(err, ErrConflict) {
					t.Fatalf("unverified conflict must not look successful to the legacy consumer: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal("matching committed readback did not resolve write timeout", err)
			}
			logical, err := p.List(ctx)
			if err != nil || !exactOwnershipRecordPresent(logical, marker) {
				t.Fatal("committed claim not recoverable", err)
			}
		})
	}
}

type rrsetReplacingTestDNS struct{ *stateTestDNS }

func (*rrsetReplacingTestDNS) OwnershipReplacesRRset() bool { return true }

func TestCompactOwnershipWholeRRsetAPIRejectsObservedForeignSiblingMutations(t *testing.T) {
	ctx := context.Background()
	dns := &stateTestDNS{}
	a := stateTestProvider(t, dns, "a")
	foreign := stateTestMember(1)
	if err := a.Create(ctx, stateTestMarker("a", foreign)); err != nil {
		t.Fatal(err)
	}
	b, err := NewCompactOwnershipProvider(&rrsetReplacingTestDNS{dns}, "b", "example.com", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sibling := foreign
	sibling.Target = "192.0.2.240"
	writes := dns.writes
	if err := b.Create(ctx, sibling); !errors.Is(err, ErrConflict) {
		t.Fatal("whole-RRset API allowed a foreign sibling mutation", err)
	}
	if writes != dns.writes {
		t.Fatal("unsafe data mutation reached backend")
	}
	// A compatible claim changes only b's isolated ownership namespace.
	if err := b.Create(ctx, stateTestMarker("b", foreign)); err != nil {
		t.Fatal(err)
	}
}

func TestCompactOwnershipCleanupRetryRetainsDurableQueue(t *testing.T) {
	ctx := context.Background()
	dns := &stateTestDNS{}
	p := stateTestProvider(t, dns, "a")
	if err := p.Create(ctx, stateTestMarker("a", stateTestMember(1))); err != nil {
		t.Fatal(err)
	}
	dns.deleteFailure = func(r Record) error {
		if IsOwnershipStateRecord(r.Hostname) {
			return fmt.Errorf("cleanup unavailable")
		}
		return nil
	}
	if err := p.Create(ctx, stateTestMarker("a", stateTestMember(2))); err != nil {
		t.Fatal(err)
	}
	if _, err := p.List(ctx); err != nil {
		t.Fatal("cleanup failure invalidated selected snapshot", err)
	}
	dns.deleteFailure = nil
	if err := stateTestProvider(t, dns, "a").MigrateOwnership(ctx); err != nil {
		t.Fatal(err)
	}
	raw, _ := dns.List(ctx)
	if len(raw) != 2 {
		t.Fatalf("cleanup retry left %d records, want page plus manifest", len(raw))
	}
}

func TestCompactOwnershipReverseMigrationPreservesClaimsOnQuotaFailure(t *testing.T) {
	ctx := context.Background()
	dns := &stateTestDNS{}
	p := stateTestProvider(t, dns, "a")
	for i := range 3 {
		if err := p.Create(ctx, stateTestMarker("a", stateTestMember(i))); err != nil {
			t.Fatal(err)
		}
	}
	legacy := stateTestProvider(t, dns, "a")
	legacy.legacy = true
	dns.limit = len(dns.records) + 1
	if err := legacy.MigrateOwnership(ctx); err == nil {
		t.Fatal("quota-limited reverse migration succeeded")
	}
	logical, err := p.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if !exactOwnershipRecordPresent(logical, stateTestMarker("a", stateTestMember(i))) {
			t.Fatal("partial reverse migration lost compact claim")
		}
	}
	dns.limit = 0
	if err := legacy.MigrateOwnership(ctx); err != nil {
		t.Fatal(err)
	}
	raw, _ := dns.List(ctx)
	for i := range 3 {
		if !exactOwnershipRecordPresent(raw, stateTestMarker("a", stateTestMember(i))) {
			t.Fatal("reverse migration did not restore legacy marker")
		}
	}
	for _, r := range raw {
		if strings.HasPrefix(r.Hostname, "_dw-p.") {
			t.Fatal("reverse migration retained compact pages")
		}
	}
}

func TestCompactOwnershipCorruptOrForkedStatePreventsDataMutations(t *testing.T) {
	for _, failure := range []string{"missing-page", "corrupt-page", "fork", "unknown-version"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			dns := &stateTestDNS{}
			p := stateTestProvider(t, dns, "a")
			member := stateTestMember(1)
			if err := p.Create(ctx, stateTestMarker("a", member)); err != nil {
				t.Fatal(err)
			}
			snapshot, err := p.snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "missing-page":
				dns.records = slices.DeleteFunc(dns.records, func(r Record) bool { return strings.HasPrefix(r.Hostname, "_dw-p.") })
			case "corrupt-page":
				for i := range dns.records {
					if strings.HasPrefix(dns.records[i].Hostname, "_dw-p.") {
						dns.records[i].Target = "corrupt"
					}
				}
			case "unknown-version":
				dns.records = append(dns.records, Record{Hostname: OwnershipManifestName("example.com"), Type: RecordTypeTXT, Target: "dnsweaver-state=v99:e30"})
			case "fork":
				head := snapshot.heads["a"]
				for _, count := range []int{0, 1} {
					m := head.manifest
					m.Revision++
					m.Previous = head.token
					if count == 0 {
						m.Pages = []string{}
						m.Entries = 0
					}
					value, err := encodeOwnershipManifest(m)
					if err != nil {
						t.Fatal(err)
					}
					dns.records = append(dns.records, Record{Hostname: OwnershipManifestName("example.com"), Type: RecordTypeTXT, Target: value})
				}
			}
			writes := dns.writes
			if _, err := p.List(ctx); !errors.Is(err, ErrOwnershipState) {
				t.Fatalf("corrupt listing = %v", err)
			}
			if err := p.Create(ctx, member); !errors.Is(err, ErrOwnershipState) {
				t.Fatalf("corrupt create = %v", err)
			}
			if err := p.Delete(ctx, member); !errors.Is(err, ErrOwnershipState) {
				t.Fatalf("corrupt delete = %v", err)
			}
			if writes != dns.writes {
				t.Fatal("mutated DNS using ambiguous state")
			}
		})
	}
}
