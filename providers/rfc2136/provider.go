package rfc2136

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/dnsupdate"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"

	"github.com/miekg/dns"
)

// Provider implements provider.Provider for RFC 2136 Dynamic DNS servers.
type Provider struct {
	name    string
	zone    string
	ttl     int
	client  *dnsupdate.Client
	catalog *dnsupdate.Catalog
	logger  *slog.Logger
}

// ProviderOption is a functional option for configuring the Provider.
type ProviderOption func(*Provider)

// WithProviderLogger sets a custom logger for the provider.
func WithProviderLogger(logger *slog.Logger) ProviderOption {
	return func(p *Provider) {
		if logger != nil {
			p.logger = logger
		}
	}
}

// New creates a new RFC 2136 provider instance.
func New(name string, config *Config, opts ...ProviderOption) (*Provider, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	p := &Provider{
		name:   name,
		zone:   config.Zone,
		ttl:    config.TTL,
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(p)
	}

	// Create the dnsupdate client with the same logger
	client, err := dnsupdate.NewClient(config.ToDNSUpdateConfig(), dnsupdate.WithLogger(p.logger))
	if err != nil {
		return nil, fmt.Errorf("creating dnsupdate client: %w", err)
	}

	p.client = client

	// Create the catalog for hostname enumeration
	p.catalog = dnsupdate.NewCatalog(client, config.Zone, p.logger)

	return p, nil
}

// NewFromEnv creates a new RFC 2136 provider from environment variables.
// This is a convenience function for use with the provider registry.
func NewFromEnv(instanceName string, opts ...ProviderOption) (*Provider, error) {
	config, err := LoadConfig(instanceName)
	if err != nil {
		return nil, err
	}

	return New(instanceName, config, opts...)
}

// Name returns the provider instance name.
func (p *Provider) Name() string {
	return p.name
}

// Type returns "rfc2136".
func (p *Provider) Type() string {
	return "rfc2136"
}

// Capabilities returns the provider's feature support.
// RFC 2136 supports all features: TXT ownership, native update, and most record types.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SupportsOwnershipTXT: true,
		SupportsNativeUpdate: true,
		SupportedRecordTypes: []provider.RecordType{
			provider.RecordTypeA,
			provider.RecordTypeAAAA,
			provider.RecordTypeCNAME,
			provider.RecordTypeTXT,
			provider.RecordTypeSRV,
			// Note: MX and PTR are supported by pkg/dnsupdate but not yet
			// in the provider interface. See issue #133.
		},
	}
}

// Zone returns the configured DNS zone.
func (p *Provider) Zone() string {
	return p.zone
}

// Ping checks connectivity to the DNS server.
func (p *Provider) Ping(ctx context.Context) error {
	return p.client.Ping(ctx)
}

// List returns all managed records in the zone.
// This uses the catalog to enumerate hostnames, then queries each one
// to build the complete record list. The catalog is maintained automatically
// when Create/Delete are called.
//
// The catalog stores managed hostnames in chunked TXT records:
//
//	_dnsweaver-catalog-0.<zone>  TXT "host1" "host2" ...
//	_dnsweaver-catalog-1.<zone>  TXT "host101" "host102" ...
//
// This approach works with any RFC 2136 server without requiring AXFR.
func (p *Provider) List(ctx context.Context) ([]provider.Record, error) {
	// Handle nil client/catalog (e.g., in tests)
	if p.client == nil || p.catalog == nil {
		p.logger.Debug("RFC 2136 List() called - no client/catalog configured, returning empty",
			slog.String("zone", p.zone),
		)
		return []provider.Record{}, nil
	}

	p.logger.Debug("RFC 2136 List() called - loading from catalog",
		slog.String("zone", p.zone),
	)

	// Get all hostnames from catalog
	hostnames, err := p.catalog.Hostnames(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading catalog: %w", err)
	}

	if len(hostnames) == 0 {
		p.logger.Debug("catalog is empty",
			slog.String("zone", p.zone),
		)
		return []provider.Record{}, nil
	}

	// Query each hostname for its records
	var records []provider.Record
	for _, hostname := range hostnames {
		// Query ownership TXT record to verify we still own it
		ownershipName := provider.OwnershipRecordName(hostname)
		ownershipFQDN := p.ensureFQDN(ownershipName)
		ownershipRecords, err := p.client.Query(ctx, ownershipFQDN, dns.TypeTXT)
		if err != nil {
			p.logger.Warn("failed to query ownership record",
				slog.String("hostname", hostname),
				slog.String("error", err.Error()),
			)
			continue
		}

		// Check if ownership TXT exists with our marker
		hasOwnership := false
		for _, r := range ownershipRecords {
			if r.Type == dns.TypeTXT && strings.Contains(r.RData, provider.OwnershipValue) {
				hasOwnership = true
				// Add ownership record to results
				records = append(records, provider.Record{
					Hostname:   ownershipName,
					Type:       provider.RecordTypeTXT,
					Target:     provider.OwnershipValue,
					TTL:        int(r.TTL),
					ProviderID: fmt.Sprintf("%s:TXT:%s", ownershipFQDN, r.RData),
				})
				break
			}
		}

		if !hasOwnership {
			p.logger.Debug("hostname in catalog but no ownership record - may need cleanup",
				slog.String("hostname", hostname),
			)
			// Still continue to check for data records
		}

		// Query common record types for this hostname
		hostnameFQDN := p.ensureFQDN(hostname)
		for _, qtype := range []uint16{dns.TypeA, dns.TypeAAAA, dns.TypeCNAME, dns.TypeSRV} {
			dnsRecords, err := p.client.Query(ctx, hostnameFQDN, qtype)
			if err != nil {
				p.logger.Debug("query failed for hostname",
					slog.String("hostname", hostname),
					slog.String("type", dns.TypeToString[qtype]),
					slog.String("error", err.Error()),
				)
				continue
			}

			for _, r := range dnsRecords {
				record, err := p.fromRFC2136Record(r)
				if err != nil {
					continue
				}
				records = append(records, record)
			}
		}
	}

	stats := p.catalog.Stats()
	p.logger.Debug("RFC 2136 List() complete",
		slog.String("zone", p.zone),
		slog.Int("catalog_hostnames", stats.TotalHostnames),
		slog.Int("catalog_chunks", stats.ChunkCount),
		slog.Int("records_returned", len(records)),
	)

	return records, nil
}

// Create adds a new DNS record.
// For non-TXT records (data records), the hostname is also added to the catalog
// for enumeration. Ownership TXT records are not added to the catalog.
func (p *Provider) Create(ctx context.Context, record provider.Record) error {
	dnsRecord, err := p.toRFC2136Record(record)
	if err != nil {
		return fmt.Errorf("converting record: %w", err)
	}

	if err := p.client.Create(ctx, dnsRecord); err != nil {
		return fmt.Errorf("creating record %s: %w", record.Hostname, err)
	}

	p.logger.Info("RFC 2136 record created",
		slog.String("name", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", record.Target),
	)

	// Add to catalog if this is NOT an ownership TXT record
	// Ownership records have format "_dnsweaver.<hostname>"
	// We only catalog the actual data hostnames, not the ownership markers
	if p.catalog != nil && !provider.IsOwnershipRecord(record.Hostname) {
		if err := p.catalog.Add(ctx, record.Hostname); err != nil {
			// Log but don't fail - the DNS record was created successfully
			// Catalog can be repaired later
			p.logger.Warn("failed to add hostname to catalog",
				slog.String("hostname", record.Hostname),
				slog.String("error", err.Error()),
			)
		}
	}

	return nil
}

// Delete removes a DNS record.
// For non-TXT records (data records), the hostname is also removed from the catalog.
// Ownership TXT records are not tracked in the catalog.
func (p *Provider) Delete(ctx context.Context, record provider.Record) error {
	dnsRecord, err := p.toRFC2136Record(record)
	if err != nil {
		return fmt.Errorf("converting record: %w", err)
	}

	if err := p.client.Delete(ctx, dnsRecord); err != nil {
		return fmt.Errorf("deleting record %s: %w", record.Hostname, err)
	}

	// Remove from catalog if this is NOT an ownership TXT record
	if p.catalog != nil && !provider.IsOwnershipRecord(record.Hostname) {
		if err := p.catalog.Remove(ctx, record.Hostname); err != nil {
			// Log but don't fail - the DNS record was deleted successfully
			// Catalog can be repaired later
			p.logger.Warn("failed to remove hostname from catalog",
				slog.String("hostname", record.Hostname),
				slog.String("error", err.Error()),
			)
		}
	}

	p.logger.Info("RFC 2136 record deleted",
		slog.String("name", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", record.Target),
	)

	return nil
}

// Update modifies an existing DNS record in place.
// RFC 2136 supports atomic update by combining delete and insert in a single message.
func (p *Provider) Update(ctx context.Context, existing, desired provider.Record) error {
	oldRecord, err := p.toRFC2136Record(existing)
	if err != nil {
		return fmt.Errorf("converting existing record: %w", err)
	}

	newRecord, err := p.toRFC2136Record(desired)
	if err != nil {
		return fmt.Errorf("converting desired record: %w", err)
	}

	if err := p.client.Update(ctx, oldRecord, newRecord); err != nil {
		return fmt.Errorf("updating record %s: %w", existing.Hostname, err)
	}

	p.logger.Info("RFC 2136 record updated",
		slog.String("name", existing.Hostname),
		slog.String("type", string(existing.Type)),
		slog.String("old_target", existing.Target),
		slog.String("new_target", desired.Target),
	)

	return nil
}

// toRFC2136Record converts a provider.Record to dnsupdate.Record.
func (p *Provider) toRFC2136Record(record provider.Record) (dnsupdate.Record, error) {
	// Ensure FQDN format with trailing dot
	name := record.Hostname
	if strings.HasSuffix(name, ".") {
		// Already FQDN, use as-is
	} else {
		// Check if hostname already contains the zone (without trailing dot)
		zoneWithoutDot := strings.TrimSuffix(p.zone, ".")
		if strings.HasSuffix(name, zoneWithoutDot) || strings.HasSuffix(name, "."+zoneWithoutDot) {
			// Hostname includes zone, just add trailing dot
			name += "."
		} else {
			// Simple hostname or partial FQDN, append zone
			name += "." + zoneWithoutDot + "."
		}
	}

	// Determine TTL
	ttl := uint32(p.ttl)
	if record.TTL > 0 {
		ttl = uint32(record.TTL)
	}

	r := dnsupdate.Record{
		Name: name,
		Type: recordTypeToUint16(record.Type),
		TTL:  ttl,
	}

	// Set RData based on record type
	switch record.Type {
	case provider.RecordTypeA, provider.RecordTypeAAAA:
		r.RData = record.Target

	case provider.RecordTypeCNAME:
		// CNAME target should be FQDN
		target := record.Target
		if !strings.HasSuffix(target, ".") {
			target += "."
		}
		r.RData = target

	case provider.RecordTypeTXT:
		r.RData = record.Target

	case provider.RecordTypeSRV:
		// SRV target should be FQDN
		target := record.Target
		if !strings.HasSuffix(target, ".") {
			target += "."
		}
		r.RData = target

		// Copy SRV-specific fields
		if record.SRV != nil {
			r.Priority = record.SRV.Priority
			r.Weight = record.SRV.Weight
			r.Port = record.SRV.Port
		}

	default:
		return r, fmt.Errorf("unsupported record type: %s", record.Type)
	}

	return r, nil
}

// recordTypeToUint16 converts provider.RecordType to dns.Type.
func recordTypeToUint16(rt provider.RecordType) uint16 {
	switch rt {
	case provider.RecordTypeA:
		return dns.TypeA
	case provider.RecordTypeAAAA:
		return dns.TypeAAAA
	case provider.RecordTypeCNAME:
		return dns.TypeCNAME
	case provider.RecordTypeTXT:
		return dns.TypeTXT
	case provider.RecordTypeSRV:
		return dns.TypeSRV
	default:
		return dns.TypeA
	}
}

// uint16ToRecordType converts dns.Type to provider.RecordType.
func uint16ToRecordType(t uint16) (provider.RecordType, bool) {
	switch t {
	case dns.TypeA:
		return provider.RecordTypeA, true
	case dns.TypeAAAA:
		return provider.RecordTypeAAAA, true
	case dns.TypeCNAME:
		return provider.RecordTypeCNAME, true
	case dns.TypeTXT:
		return provider.RecordTypeTXT, true
	case dns.TypeSRV:
		return provider.RecordTypeSRV, true
	default:
		return "", false
	}
}

// fromRFC2136Record converts a dnsupdate.Record to provider.Record.
// Returns an error for unsupported record types.
func (p *Provider) fromRFC2136Record(r dnsupdate.Record) (provider.Record, error) {
	recordType, ok := uint16ToRecordType(r.Type)
	if !ok {
		return provider.Record{}, fmt.Errorf("unsupported record type: %s", r.TypeString())
	}

	// Normalize hostname: remove trailing dot and zone suffix for internal use
	hostname := strings.TrimSuffix(r.Name, ".")

	record := provider.Record{
		Hostname:   hostname,
		Type:       recordType,
		Target:     strings.TrimSuffix(r.RData, "."), // Remove trailing dot from targets
		TTL:        int(r.TTL),
		ProviderID: fmt.Sprintf("%s:%s:%s", r.Name, r.TypeString(), r.RData),
	}

	// Handle SRV-specific fields
	if recordType == provider.RecordTypeSRV {
		record.SRV = &provider.SRVData{
			Priority: r.Priority,
			Weight:   r.Weight,
			Port:     r.Port,
		}
	}

	return record, nil
}

// ensureFQDN ensures a hostname is fully qualified with a trailing dot.
// If the hostname doesn't include the zone, it is appended.
func (p *Provider) ensureFQDN(hostname string) string {
	name := hostname

	if strings.HasSuffix(name, ".") {
		// Already FQDN
		return name
	}

	// Check if hostname already contains the zone (without trailing dot)
	zoneWithoutDot := strings.TrimSuffix(p.zone, ".")
	if strings.HasSuffix(name, zoneWithoutDot) || strings.HasSuffix(name, "."+zoneWithoutDot) {
		// Hostname includes zone, just add trailing dot
		return name + "."
	}

	// Simple hostname or partial FQDN, append zone
	return name + "." + zoneWithoutDot + "."
}

// Verify interface compliance at compile time.
var (
	_ provider.Provider = (*Provider)(nil)
	_ provider.Updater  = (*Provider)(nil)
)
