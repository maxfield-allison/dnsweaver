package reconciler

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

func TestReconcile_CloudflareTXTOwnershipStorageMigration(t *testing.T) {
	for _, from := range []string{"legacy", "compact"} {
		to := "compact"
		if from == "compact" {
			to = "legacy"
		}
		for _, declared := range []string{"v=spf1 -all", `"v=spf1 -all"`, `"v=spf1 " "-all"`, `"\"hello\""`, `""`, `"\"open"`, `"a\065b"`, `"\"` + strings.Repeat("a", 253) + `é\""`} {
			t.Run(from+"-to-"+to+"/"+declared, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				api := &fakeCloudflareAPI{quoteTXT: true}
				api.add("TXT", "mail.example.com", "manual verification", 300, false)
				original, originalLister := newCloudflareStorageFixture(t, api, false, "192.0.2.1", from)
				originalLister.AddWorkload("mail", cloudflareMigrationLabels(declared))
				if _, err := original.Reconcile(ctx); err != nil {
					t.Fatal(err)
				}
				before := api.txtContents("mail.example.com")
				if len(before) != 2 {
					t.Fatalf("initial data = %q, want managed and manual TXT", before)
				}

				migrated, migratedLister := newCloudflareStorageFixture(t, api, false, "192.0.2.1", to)
				migratedLister.AddWorkload("mail", cloudflareMigrationLabels(declared))
				if _, err := migrated.Reconcile(ctx); err != nil {
					t.Fatalf("migration: %v", err)
				}
				if got := api.txtContents("mail.example.com"); !slices.Equal(got, before) {
					t.Fatalf("migration changed data: before=%q after=%q", before, got)
				}
				assertCloudflareWithdrawalAfterMigration(ctx, t, api, to)
			})
		}
	}
}

func TestReconcile_CloudflareOldLegacyTXTClaimMigratesToCompact(t *testing.T) {
	for _, declared := range []string{`"v=spf1 -all"`, `"\"hello\""`} {
		t.Run(declared, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			api := &fakeCloudflareAPI{quoteTXT: true}
			member := provider.Record{Hostname: "mail.example.com", Type: provider.RecordTypeTXT, Target: declared, TTL: 300}
			marker := provider.MemberOwnershipRecord(member.Hostname, 300, "", member, nil)
			// Before payload conversion, markers recorded the declaration's
			// presentation spelling. Seed those bytes directly, bypassing the
			// new provider's marker writer.
			api.add("TXT", member.Hostname, declared, 300, false)
			api.add("TXT", marker.Hostname, marker.Target, 300, false)
			api.add("TXT", member.Hostname, "manual verification", 300, false)
			before := api.txtContents(member.Hostname)
			r, lister := newCloudflareStorageFixture(t, api, false, "192.0.2.1", "compact")
			lister.AddWorkload("mail", cloudflareMigrationLabels(declared))
			if _, err := r.Reconcile(ctx); err != nil {
				t.Fatalf("upgrade and migration: %v", err)
			}
			if got := api.txtContents(member.Hostname); !slices.Equal(got, before) {
				t.Fatalf("upgrade changed data: before=%q after=%q", before, got)
			}
			assertCloudflareWithdrawalAfterMigration(ctx, t, api, "compact")
		})
	}
}

func cloudflareMigrationLabels(target string) map[string]string {
	return map[string]string{
		"dnsweaver.records.txt.hostname": "mail.example.com",
		"dnsweaver.records.txt.type":     "TXT",
		"dnsweaver.records.txt.target":   target,
	}
}

func assertCloudflareWithdrawalAfterMigration(ctx context.Context, t *testing.T, api *fakeCloudflareAPI, storage string) {
	t.Helper()
	// A fresh provider/registry/reconciler must recover the migrated claim
	// from DNS alone. Its workload list is empty because the source was removed.
	restarted, _ := newCloudflareStorageFixture(t, api, false, "192.0.2.1", storage)
	if err := restarted.RecoverOwnership(ctx); err != nil {
		t.Fatalf("recover migrated ownership: %v", err)
	}
	if _, err := restarted.Reconcile(ctx); err != nil {
		t.Fatalf("withdraw after migration and restart: %v", err)
	}
	if got := api.txtContents("mail.example.com"); !slices.Equal(got, []string{`"manual verification"`}) {
		t.Fatalf("withdrawal left data = %q, want only the manual TXT", got)
	}
}
