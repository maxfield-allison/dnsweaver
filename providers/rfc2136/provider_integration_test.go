//go:build integration

package rfc2136

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

func integrationConfig(t *testing.T) *Config {
	t.Helper()
	server := os.Getenv("DNSUPDATE_TEST_SERVER")
	zone := os.Getenv("DNSUPDATE_TEST_ZONE")
	if server == "" || zone == "" {
		t.Skip("DNSUPDATE_TEST_SERVER and DNSUPDATE_TEST_ZONE must be set")
	}
	if !strings.HasSuffix(zone, ".") {
		zone += "."
	}
	return &Config{
		Server:        server,
		Zone:          zone,
		TSIGKeyName:   os.Getenv("DNSUPDATE_TEST_TSIG_NAME"),
		TSIGSecret:    os.Getenv("DNSUPDATE_TEST_TSIG_SECRET"),
		TSIGAlgorithm: os.Getenv("DNSUPDATE_TEST_TSIG_ALGORITHM"),
		Timeout:       DefaultTimeout,
		UseTCP:        os.Getenv("DNSUPDATE_TEST_TCP") == "true",
		TTL:           60,
	}
}

func TestIntegration_DeleteKeepsCatalogUntilFinalMember(t *testing.T) {
	cfg := integrationConfig(t)
	hostname := fmt.Sprintf("dnsweaver-rfc2136-set-%d.%s", time.Now().UnixNano(), strings.TrimSuffix(cfg.Zone, "."))
	first := provider.Record{Hostname: hostname, Type: provider.RecordTypeA, Target: "192.0.2.10", TTL: 60}
	second := provider.Record{Hostname: hostname, Type: provider.RecordTypeA, Target: "192.0.2.11", TTL: 60}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	p, err := New("integration", cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = p.Delete(cleanupCtx, first)
		_ = p.Delete(cleanupCtx, second)
	})

	if err := p.Create(ctx, first); err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	if err := p.Create(ctx, second); err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	if err := p.Delete(ctx, first); err != nil {
		t.Fatalf("Delete(first) error = %v", err)
	}

	// Constructing a new provider simulates a dnsweaver restart. It has to
	// reload the catalog from DNS and still discover the surviving member.
	restarted, err := New("integration-restarted", cfg)
	if err != nil {
		t.Fatalf("New(restarted) error = %v", err)
	}
	records, err := restarted.List(ctx)
	if err != nil {
		t.Fatalf("List(after restart) error = %v", err)
	}
	if !containsRecord(records, second) {
		t.Fatalf("surviving member missing after restart: records=%+v", records)
	}
	if containsRecord(records, first) {
		t.Fatalf("deleted member returned after restart: records=%+v", records)
	}

	if err := restarted.Delete(ctx, second); err != nil {
		t.Fatalf("Delete(second) error = %v", err)
	}
	final, err := New("integration-final", cfg)
	if err != nil {
		t.Fatalf("New(final) error = %v", err)
	}
	records, err = final.List(ctx)
	if err != nil {
		t.Fatalf("List(after final delete) error = %v", err)
	}
	if containsRecord(records, first) || containsRecord(records, second) {
		t.Fatalf("members remain after final delete: records=%+v", records)
	}
}

func containsRecord(records []provider.Record, want provider.Record) bool {
	for _, record := range records {
		if record.Hostname == want.Hostname && record.Type == want.Type && record.Target == want.Target {
			return true
		}
	}
	return false
}
