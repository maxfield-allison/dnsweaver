// Package technitium implements the DNSWeaver provider interface for Technitium DNS Server.
package technitium

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// companionHTTPSSvcPriority is the SvcPriority used for auto-created companion HTTPS records.
// Priority 1 = ServiceMode (as opposed to 0 = AliasMode).
const companionHTTPSSvcPriority = 1

// companionHTTPSTargetName is the SvcTargetName for companion HTTPS records.
// "." means the record's owner name (self-referential), per RFC 9460 Section 2.5.
const companionHTTPSTargetName = "."

// needsCompanionHTTPS returns true if the given record type should trigger
// companion HTTPS record creation (A or AAAA).
//
// CNAME is deliberately excluded: a CNAME is exclusive and cannot share an
// owner name with an HTTPS record (RFC 1034 §3.6.2, RFC 2181 §10.1). Technitium
// rejects the companion outright in that case, so attempting it only produced a
// warning on every reconcile (issue #165). Hosts fronted by a CNAME inherit the
// HTTPS record of the name they point at.
func needsCompanionHTTPS(recordType provider.RecordType) bool {
	switch recordType {
	case provider.RecordTypeA, provider.RecordTypeAAAA:
		return true
	default:
		return false
	}
}

// hadCompanionHTTPS returns true if a record of the given type may have a
// companion HTTPS record left over from an earlier dnsweaver release, which
// created companions for CNAME records too. Deletion stays broader than
// creation so those leftovers are still cleaned up when the record is removed.
func hadCompanionHTTPS(recordType provider.RecordType) bool {
	return needsCompanionHTTPS(recordType) || recordType == provider.RecordTypeCNAME
}

// createCompanionHTTPS creates a companion HTTPS record for the given hostname.
// This prevents ECH (Encrypted Client Hello) fallback errors in split-horizon DNS
// environments by providing a local HTTPS record that overrides public ECH parameters.
//
// The companion record format: HTTPS 1 . alpn="<alpn>"
// Example: app.example.com 300 IN HTTPS 1 . alpn="h2"
//
// Skips creation if:
//   - Auto HTTPS records are disabled
//   - The record type doesn't need a companion (not A/AAAA)
//   - An HTTPS record already exists for the hostname (avoids overwriting manual records)
func (p *Provider) createCompanionHTTPS(ctx context.Context, hostname string, recordType provider.RecordType, ttl int) error {
	if !p.autoHTTPSRecords {
		return nil
	}
	if !needsCompanionHTTPS(recordType) {
		return nil
	}

	// Check if an HTTPS record already exists for this hostname
	// to avoid overwriting manually-created records
	exists, err := p.hasHTTPSRecord(ctx, hostname)
	if err != nil {
		p.logger.Warn("failed to check existing HTTPS records, skipping companion creation",
			slog.String("hostname", hostname),
			slog.String("error", err.Error()),
		)
		return nil
	}
	if exists {
		p.logger.Debug("HTTPS record already exists, skipping companion creation",
			slog.String("hostname", hostname),
		)
		return nil
	}

	svcParams := buildSvcParams(p.autoHTTPSALPN)
	if err := p.client.AddHTTPSRecord(ctx, p.zone, hostname, companionHTTPSSvcPriority, companionHTTPSTargetName, svcParams, ttl); err != nil {
		// If Technitium reports conflict, treat as non-fatal (record already exists)
		if strings.Contains(err.Error(), "record already exists") || strings.Contains(err.Error(), "Identical record") {
			p.logger.Debug("companion HTTPS record already exists (conflict)", slog.String("hostname", hostname))
			return nil
		}
		return fmt.Errorf("creating companion HTTPS record for %s: %w", hostname, err)
	}

	p.logger.Info("created companion HTTPS record",
		slog.String("hostname", hostname),
		slog.String("alpn", p.autoHTTPSALPN),
		slog.Int("ttl", ttl),
	)

	return nil
}

// deleteCompanionHTTPS removes the companion HTTPS record for the given hostname.
// Only deletes records that match the auto-created format (priority 1, target ".", same ALPN).
func (p *Provider) deleteCompanionHTTPS(ctx context.Context, hostname string, recordType provider.RecordType) error {
	if !p.autoHTTPSRecords {
		return nil
	}
	if !hadCompanionHTTPS(recordType) {
		return nil
	}

	svcParams := buildSvcParams(p.autoHTTPSALPN)
	if err := p.client.DeleteHTTPSRecord(ctx, p.zone, hostname, companionHTTPSSvcPriority, companionHTTPSTargetName, svcParams); err != nil {
		return fmt.Errorf("deleting companion HTTPS record for %s: %w", hostname, err)
	}

	p.logger.Info("deleted companion HTTPS record",
		slog.String("hostname", hostname),
		slog.String("alpn", p.autoHTTPSALPN),
	)

	return nil
}

// hasHTTPSRecord checks whether any HTTPS record exists for the given hostname.
// Used to avoid overwriting manually-created HTTPS records.
func (p *Provider) hasHTTPSRecord(ctx context.Context, hostname string) (bool, error) {
	records, err := p.client.GetRecords(ctx, p.zone, hostname)
	if err != nil {
		return false, fmt.Errorf("checking HTTPS records for %s: %w", hostname, err)
	}

	for _, r := range records {
		if r.Type == "HTTPS" {
			return true, nil
		}
	}
	return false, nil
}
