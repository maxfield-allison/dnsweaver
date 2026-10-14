package testutil

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// OwnershipProcessFactory returns a fresh provider process over one fixture.
// Empty storage selects the undecorated legacy reader, as in an older binary.
type OwnershipProcessFactory func(t *testing.T, owner, storage string) provider.Provider

// OwnershipRegistryProvider exercises application registry wiring for a fixture.
func OwnershipRegistryProvider(t *testing.T, owner, storage, kind string, factory provider.Factory, config map[string]string) provider.Provider {
	t.Helper()
	registry := provider.NewRegistry(QuietLogger())
	registry.SetInstanceID(owner)
	registry.SetOwnershipStorage(storage)
	registry.RegisterFactory(kind, factory)
	RequireNoError(t, registry.CreateInstance(provider.ProviderInstanceConfig{
		Name: "dns", TypeName: kind, RecordType: provider.RecordTypeA, Target: "192.0.2.1",
		TTL: 300, Domains: []string{"*.example.com"}, ProviderConfig: config,
	}))
	instance, _ := registry.Get("dns")
	return instance.Provider
}

// RunOwnershipMigrationConformance checks the serialized provider boundary:
// migration, DNS-only restart, exact metadata, sibling preservation, and reversal
// to a fresh legacy reader. It does not qualify a real provider deployment.
func RunOwnershipMigrationConformance(t *testing.T, factory OwnershipProcessFactory) {
	t.Helper()
	ctx := context.Background()
	const owner = "migration-test"
	native := factory(t, owner, "")
	var members, markers []provider.Record
	for i := range 16 {
		members = append(members, provider.Record{
			Hostname: fmt.Sprintf("app-%02d.example.com", i), Type: provider.RecordTypeA,
			Target: fmt.Sprintf("192.0.2.%d", i+1), TTL: 300,
		})
	}
	members = append(members,
		provider.Record{Hostname: "ipv6.example.com", Type: provider.RecordTypeAAAA, Target: "2001:db8::1", TTL: 300},
		provider.Record{Hostname: "alias.example.com", Type: provider.RecordTypeCNAME, Target: "app-00.example.com", TTL: 300},
		provider.Record{Hostname: "_sip._tcp.example.com", Type: provider.RecordTypeSRV, Target: "app-00.example.com", TTL: 300, SRV: &provider.SRVData{Priority: 10, Weight: 5, Port: 5060}},
	)
	for i, member := range members {
		RequireNoError(t, native.Create(ctx, member))
		var opaque strings.Builder
		for part := range 8 {
			fmt.Fprintf(&opaque, "%x", sha256.Sum256(fmt.Appendf(nil, "member-%d-part-%d", i, part)))
		}
		marker := provider.MemberOwnershipRecord(member.Hostname, 300, owner, member,
			map[string]string{"note": fmt.Sprintf("member %d with spaces and \"quotes\"", i), "opaque": opaque.String()})
		markers = append(markers, marker)
		RequireNoError(t, native.Create(ctx, marker))
	}
	foreign := provider.MemberOwnershipRecord(members[0].Hostname, 300, "offline-owner", members[0], nil)
	user := provider.Record{Hostname: markers[0].Hostname, Type: provider.RecordTypeTXT, Target: "operator-note", TTL: 300}
	RequireNoError(t, native.Create(ctx, foreign))
	RequireNoError(t, native.Create(ctx, user))
	compact := factory(t, owner, "compact")
	migrator, ok := compact.(provider.OwnershipMigrator)
	if !ok {
		t.Fatal("registry did not enable compact ownership")
	}
	RequireNoError(t, migrator.MigrateOwnership(ctx))
	physical, err := factory(t, owner, "").List(ctx)
	RequireNoError(t, err)
	stateCount, largestPage := 0, 0
	for _, record := range physical {
		if provider.IsOwnershipStateRecord(record.Hostname) {
			stateCount++
			if strings.HasPrefix(record.Hostname, "_dw-p.") {
				largestPage = max(largestPage, len(record.Target))
			}
		}
		if provider.IsOwnershipRecord(record.Hostname) && provider.MatchesOwnership(record.Target, owner) {
			t.Fatal("migration retained an own legacy marker")
		}
	}
	if stateCount < 3 || stateCount >= len(markers) || largestPage < 3000 || largestPage > provider.DefaultOwnershipPageBytes {
		t.Fatalf("migration count=%d largest page=%d; expected multiple pages near the byte bound", stateCount, largestPage)
	}
	t.Logf("%d claims: %d compact TXT records; largest page %d bytes", len(markers), stateCount, largestPage)
	restarted, err := factory(t, owner, "compact").List(ctx)
	RequireNoError(t, err)
	assertRecords := func(records, expected []provider.Record) {
		t.Helper()
		for _, want := range expected {
			found := false
			for _, got := range records {
				if strings.EqualFold(strings.TrimSuffix(got.Hostname, "."), want.Hostname) && provider.SameRecordMember(got, want) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("missing record or metadata: %s %s %s", want.Hostname, want.Type, want.Target)
			}
		}
	}
	assertRecords(restarted, members)
	assertRecords(restarted, markers)
	assertRecords(restarted, []provider.Record{foreign, user})
	legacy := factory(t, owner, "legacy")
	legacyMigrator, ok := legacy.(provider.OwnershipMigrator)
	if !ok {
		t.Fatal("registry did not enable compact-aware legacy storage")
	}
	RequireNoError(t, legacyMigrator.MigrateOwnership(ctx))
	// A fresh old-style reader must recover all claims without compact decoding.
	reversed, err := factory(t, owner, "").List(ctx)
	RequireNoError(t, err)
	assertRecords(reversed, members)
	assertRecords(reversed, markers)
	assertRecords(reversed, []provider.Record{foreign, user})
	for _, record := range reversed {
		if strings.HasPrefix(record.Hostname, "_dw-p.") {
			t.Fatal("reverse migration retained obsolete compact pages")
		}
	}
}
