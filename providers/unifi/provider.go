package unifi

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

// maxDomainLength is the longest domain the Integration API accepts on any
// DNS policy type.
const maxDomainLength = 127

// Minimum UniFi Network version that exposes DNS policies in the Integration API.
const (
	minMajorVersion = 10
	minMinorVersion = 1
)

// Provider implements provider.Provider and provider.Updater for UniFi Network.
// It manages DNS records through the console's Integration API DNS policies.
type Provider struct {
	name   string
	url    string // Console URL (recorded for Identity reporting)
	site   string // Site internalReference or UUID as configured
	zone   string
	ttl    int
	client *Client
	logger *slog.Logger

	// siteID caches the resolved site UUID. It is resolved lazily on first
	// use so construction stays network-free and an unreachable console is
	// retried by the manager rather than failing configuration.
	siteMu sync.Mutex
	siteID string
}

// Compile-time check that Provider implements the required interfaces.
var (
	_ provider.Provider     = (*Provider)(nil)
	_ provider.Updater      = (*Provider)(nil)
	_ provider.Identifiable = (*Provider)(nil)
)

// ProviderOption is a functional option for configuring the Provider.
type ProviderOption func(*Provider)

// WithProviderLogger sets a custom logger for the provider and its API client.
func WithProviderLogger(logger *slog.Logger) ProviderOption {
	return func(p *Provider) {
		if logger != nil {
			p.logger = logger
			WithLogger(logger)(p.client)
		}
	}
}

// WithProviderHTTPClient sets a custom HTTP client for the provider.
// This allows the factory to pass in a pre-configured HTTP client with
// timeout, TLS settings, and user-agent already applied.
func WithProviderHTTPClient(client *http.Client) ProviderOption {
	return func(p *Provider) {
		WithHTTPClient(client)(p.client)
	}
}

// New creates a new UniFi provider instance.
func New(name string, config *Config, opts ...ProviderOption) (*Provider, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	p := &Provider{
		name:   name,
		url:    config.URL,
		site:   config.Site,
		zone:   config.Zone,
		ttl:    config.TTL,
		logger: slog.Default(),
		client: NewClient(config.URL, config.APIKey),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p, nil
}

// NewFromEnv creates a new UniFi provider from environment variables.
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

// Type returns "unifi".
func (p *Provider) Type() string {
	return "unifi"
}

// Identity returns the backend identity for this provider instance.
// Two unifi instances are considered the same backend when they target the
// same console, site and zone (see provider.ProviderIdentity, issue #88).
func (p *Provider) Identity() provider.ProviderIdentity {
	return provider.ProviderIdentity{
		Type:     "unifi",
		Endpoint: p.url + "/sites/" + p.site,
		Zone:     p.zone,
	}
}

// Capabilities returns the provider's feature support.
// The Integration API stores TXT records, so ownership tracking is available,
// and policies are replaced in place via PUT, so native update is supported.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SupportsOwnershipTXT: true,
		SupportsNativeUpdate: true,
		SupportedRecordTypes: []provider.RecordType{
			provider.RecordTypeA,
			provider.RecordTypeAAAA,
			provider.RecordTypeCNAME,
			provider.RecordTypeSRV,
			provider.RecordTypeTXT,
		},
	}
}

// Zone returns the configured DNS zone.
func (p *Provider) Zone() string {
	return p.zone
}

// Ping checks connectivity and authentication, verifies the console runs a
// Network version with DNS policies, and verifies the configured site exists
// so a typo surfaces at startup rather than on first write.
func (p *Provider) Ping(ctx context.Context) error {
	version, err := p.client.Ping(ctx)
	if err != nil {
		return err
	}

	if !meetsMinimumVersion(version) {
		return fmt.Errorf("UniFi Network %s does not support DNS policies; version %d.%d or later is required",
			version, minMajorVersion, minMinorVersion)
	}

	_, err = p.resolveSiteID(ctx)
	return err
}

// meetsMinimumVersion reports whether a version string such as "10.6.106" is
// at least minMajorVersion.minMinorVersion. Versions that cannot be parsed
// are allowed through so an unexpected format never blocks a working console.
func meetsMinimumVersion(version string) bool {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return true
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	if errMajor != nil || errMinor != nil {
		return true
	}
	return major > minMajorVersion || (major == minMajorVersion && minor >= minMinorVersion)
}

// resolveSiteID returns the site UUID, resolving and caching it on first use.
func (p *Provider) resolveSiteID(ctx context.Context) (string, error) {
	p.siteMu.Lock()
	defer p.siteMu.Unlock()

	if p.siteID != "" {
		return p.siteID, nil
	}

	id, err := p.client.ResolveSiteID(ctx, p.site)
	if err != nil {
		return "", fmt.Errorf("resolving site: %w", err)
	}

	p.logger.Debug("resolved UniFi site",
		slog.String("provider", p.name),
		slog.String("site", p.site),
		slog.String("site_id", id),
	)

	p.siteID = id
	return id, nil
}

// List returns all managed records in the site.
// Only enabled, user-defined A, AAAA, CNAME, TXT and SRV policies are returned.
func (p *Provider) List(ctx context.Context) ([]provider.Record, error) {
	siteID, err := p.resolveSiteID(ctx)
	if err != nil {
		return nil, err
	}

	policies, err := p.client.ListDNSPolicies(ctx, siteID)
	if err != nil {
		return nil, fmt.Errorf("listing records: %w", err)
	}

	var records []provider.Record
	for _, pol := range policies {
		if !isManaged(pol) {
			continue
		}

		record, ok := policyToRecord(pol, p.ttl)
		if !ok {
			continue
		}

		if !p.inZone(record.Hostname) {
			continue
		}

		records = append(records, record)
	}

	p.logger.Debug("listed records",
		slog.String("provider", p.name),
		slog.Int("total_policies", len(policies)),
		slog.Int("matched", len(records)),
	)

	return records, nil
}

// Create adds a new DNS record.
func (p *Provider) Create(ctx context.Context, record provider.Record) error {
	siteID, err := p.resolveSiteID(ctx)
	if err != nil {
		return err
	}

	pol, err := recordToPolicy(record, p.effectiveTTL(record))
	if err != nil {
		return fmt.Errorf("creating %s record: %w", record.Type, err)
	}

	if _, err := p.client.CreateDNSPolicy(ctx, siteID, pol); err != nil {
		return fmt.Errorf("creating %s record: %w", record.Type, err)
	}

	p.logger.Info("created record",
		slog.String("provider", p.name),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", record.Target),
	)

	return nil
}

// Delete removes a DNS record. Deleting a record that is already gone is a no-op.
func (p *Provider) Delete(ctx context.Context, record provider.Record) error {
	siteID, err := p.resolveSiteID(ctx)
	if err != nil {
		return err
	}

	policyID, err := p.policyIDFor(ctx, siteID, record)
	if err != nil {
		return fmt.Errorf("deleting %s record: %w", record.Type, err)
	}
	if policyID == "" {
		p.logger.Debug("record already absent, nothing to delete",
			slog.String("provider", p.name),
			slog.String("hostname", record.Hostname),
			slog.String("type", string(record.Type)),
		)
		return nil
	}

	if err := p.client.DeleteDNSPolicy(ctx, siteID, policyID); err != nil {
		if !provider.IsNotFound(err) {
			return fmt.Errorf("deleting %s record: %w", record.Type, err)
		}
		p.logger.Debug("record already deleted on the console",
			slog.String("provider", p.name),
			slog.String("hostname", record.Hostname),
			slog.String("type", string(record.Type)),
		)
		return nil
	}

	p.logger.Info("deleted record",
		slog.String("provider", p.name),
		slog.String("hostname", record.Hostname),
		slog.String("type", string(record.Type)),
		slog.String("target", record.Target),
	)

	return nil
}

// Update modifies an existing DNS record in place.
// This implements the provider.Updater interface for native update support.
func (p *Provider) Update(ctx context.Context, existing, desired provider.Record) error {
	siteID, err := p.resolveSiteID(ctx)
	if err != nil {
		return err
	}

	policyID, err := p.policyIDFor(ctx, siteID, existing)
	if err != nil {
		return fmt.Errorf("updating %s record: %w", desired.Type, err)
	}
	if policyID == "" {
		return fmt.Errorf("updating %s record %s: %w", desired.Type, existing.Hostname, provider.ErrNotFound)
	}

	pol, err := recordToPolicy(desired, p.effectiveTTL(desired))
	if err != nil {
		return fmt.Errorf("updating %s record: %w", desired.Type, err)
	}

	if err := p.client.UpdateDNSPolicy(ctx, siteID, policyID, pol); err != nil {
		return fmt.Errorf("updating %s record: %w", desired.Type, err)
	}

	p.logger.Info("updated record",
		slog.String("provider", p.name),
		slog.String("hostname", desired.Hostname),
		slog.String("type", string(desired.Type)),
		slog.String("old_target", existing.Target),
		slog.String("new_target", desired.Target),
	)

	return nil
}

// policyIDFor returns the policy id for a record. The id recorded by List is
// used when present; otherwise the site's policies are searched for a member
// matching the record. An empty id with a nil error means no policy matches.
func (p *Provider) policyIDFor(ctx context.Context, siteID string, record provider.Record) (string, error) {
	if record.ProviderID != "" {
		return record.ProviderID, nil
	}

	policies, err := p.client.ListDNSPolicies(ctx, siteID)
	if err != nil {
		return "", fmt.Errorf("looking up policy: %w", err)
	}

	for _, pol := range policies {
		if !isManaged(pol) {
			continue
		}
		candidate, ok := policyToRecord(pol, p.ttl)
		if !ok {
			continue
		}
		if sameHostname(candidate.Hostname, record.Hostname) && provider.SameRecordMember(candidate, record) {
			return pol.ID, nil
		}
	}

	return "", nil
}

// effectiveTTL returns the record's TTL, falling back to the instance default.
func (p *Provider) effectiveTTL(record provider.Record) int {
	if record.TTL > 0 {
		return record.TTL
	}
	return p.ttl
}

// inZone reports whether a hostname falls within the configured zone.
// With no zone configured every hostname matches. DNS names compare
// case-insensitively and a trailing dot is ignored.
func (p *Provider) inZone(hostname string) bool {
	if p.zone == "" {
		return true
	}
	name := normalizeHostname(hostname)
	zone := normalizeHostname(p.zone)
	return name == zone || strings.HasSuffix(name, "."+zone)
}

// isManaged reports whether a policy is one dnsweaver may read and write:
// enabled, and created by a user or the API rather than derived by the console
// from clients, devices or other settings. Consoles older than 10.3 omit
// metadata; those policies are treated as user-defined.
func isManaged(pol dnsPolicy) bool {
	if !pol.Enabled {
		return false
	}
	return pol.Metadata == nil || pol.Metadata.Origin == originUserDefined
}

// policyToRecord converts a DNS policy into a provider.Record. The second
// return value is false for policy types dnsweaver does not manage.
// TXT and SRV policies carry no TTL, so they report defaultTTL.
func policyToRecord(pol dnsPolicy, defaultTTL int) (provider.Record, bool) {
	record := provider.Record{
		Hostname:   pol.Domain,
		TTL:        defaultTTL,
		ProviderID: pol.ID,
	}
	if pol.TTLSeconds != nil && *pol.TTLSeconds > 0 {
		record.TTL = *pol.TTLSeconds
	}

	switch pol.Type {
	case policyTypeA:
		record.Type = provider.RecordTypeA
		record.Target = pol.IPv4Address
	case policyTypeAAAA:
		record.Type = provider.RecordTypeAAAA
		record.Target = pol.IPv6Address
	case policyTypeCNAME:
		record.Type = provider.RecordTypeCNAME
		record.Target = pol.TargetDomain
	case policyTypeTXT:
		record.Type = provider.RecordTypeTXT
		record.Target = unquoteTXT(pol.Text)
		record.TTL = defaultTTL
	case policyTypeSRV:
		record.Type = provider.RecordTypeSRV
		record.Hostname = joinSRVHostname(pol.Service, pol.Protocol, pol.Domain)
		record.Target = pol.ServerDomain
		record.TTL = defaultTTL
		record.SRV = &provider.SRVData{
			Priority: clampUint16(pol.Priority),
			Weight:   clampUint16(pol.Weight),
			Port:     clampUint16(pol.Port),
		}
	default:
		// MX, FORWARD_DOMAIN and any future types are not managed.
		return provider.Record{}, false
	}

	return record, true
}

// recordToPolicy converts a provider.Record into the DNS policy body sent on
// create and update. ttl applies to A, AAAA and CNAME only; the API has no
// TTL for TXT and SRV policies.
func recordToPolicy(record provider.Record, ttl int) (dnsPolicy, error) {
	pol := dnsPolicy{
		Enabled: true,
		Domain:  record.Hostname,
	}

	switch record.Type {
	case provider.RecordTypeA:
		ip := net.ParseIP(record.Target)
		if ip == nil || ip.To4() == nil {
			return dnsPolicy{}, fmt.Errorf("target %q for A record is not an IPv4 address", record.Target)
		}
		pol.Type = policyTypeA
		pol.IPv4Address = record.Target
		pol.TTLSeconds = &ttl
	case provider.RecordTypeAAAA:
		ip := net.ParseIP(record.Target)
		if ip == nil || ip.To4() != nil {
			return dnsPolicy{}, fmt.Errorf("target %q for AAAA record is not an IPv6 address", record.Target)
		}
		pol.Type = policyTypeAAAA
		pol.IPv6Address = record.Target
		pol.TTLSeconds = &ttl
	case provider.RecordTypeCNAME:
		pol.Type = policyTypeCNAME
		pol.TargetDomain = record.Target
		pol.TTLSeconds = &ttl
	case provider.RecordTypeTXT:
		if isQuoted(record.Target) && strings.Contains(record.Target, ",") {
			return dnsPolicy{}, fmt.Errorf("TXT value %q is both double-quoted and contains a comma, which the UniFi API cannot store losslessly; remove the surrounding quotes", record.Target)
		}
		pol.Type = policyTypeTXT
		pol.Text = quoteTXT(record.Target)
	case provider.RecordTypeSRV:
		if record.SRV == nil {
			return dnsPolicy{}, fmt.Errorf("SRV data is required")
		}
		service, protocol, domain, err := splitSRVHostname(record.Hostname)
		if err != nil {
			return dnsPolicy{}, err
		}
		port, priority, weight := int(record.SRV.Port), int(record.SRV.Priority), int(record.SRV.Weight)
		pol.Type = policyTypeSRV
		pol.Domain = domain
		pol.Service = service
		pol.Protocol = protocol
		pol.ServerDomain = record.Target
		pol.Port = &port
		pol.Priority = &priority
		pol.Weight = &weight
	default:
		return dnsPolicy{}, fmt.Errorf("unsupported record type: %s", record.Type)
	}

	if len(pol.Domain) > maxDomainLength {
		return dnsPolicy{}, fmt.Errorf("domain %q exceeds the %d character limit of the UniFi API", pol.Domain, maxDomainLength)
	}

	return pol, nil
}

// quoteTXT prepares TXT text for the Integration API, which requires values
// containing commas to be enclosed in double quotes. Text without commas is
// returned unchanged.
//
// quoteTXT and unquoteTXT are exact inverses for every value except one that
// is already double-quoted and contains a comma; recordToPolicy rejects that
// value so it can never be stored ambiguously.
func quoteTXT(text string) string {
	if !strings.Contains(text, ",") {
		return text
	}
	return `"` + text + `"`
}

// unquoteTXT reverses quoteTXT: quotes are stripped only when the enclosed
// text contains a comma, so a value that was never quoted by quoteTXT
// round-trips unchanged.
func unquoteTXT(text string) string {
	if !isQuoted(text) {
		return text
	}
	inner := text[1 : len(text)-1]
	if !strings.Contains(inner, ",") {
		return text
	}
	return inner
}

func isQuoted(text string) bool {
	return len(text) >= 2 && strings.HasPrefix(text, `"`) && strings.HasSuffix(text, `"`)
}

// splitSRVHostname splits an SRV owner name such as "_ldap._tcp.example.com"
// into the service ("_ldap"), protocol ("_tcp") and domain ("example.com")
// fields the Integration API expects.
func splitSRVHostname(hostname string) (service, protocol, domain string, err error) {
	parts := strings.SplitN(hostname, ".", 3)
	if len(parts) != 3 || parts[2] == "" ||
		!strings.HasPrefix(parts[0], "_") || !strings.HasPrefix(parts[1], "_") {
		return "", "", "", fmt.Errorf("SRV hostname %q must have the form _service._proto.domain", hostname)
	}
	return parts[0], parts[1], parts[2], nil
}

// joinSRVHostname is the inverse of splitSRVHostname.
func joinSRVHostname(service, protocol, domain string) string {
	return service + "." + protocol + "." + domain
}

// sameHostname compares owner names case-insensitively, ignoring a trailing dot.
func sameHostname(a, b string) bool {
	return normalizeHostname(a) == normalizeHostname(b)
}

// normalizeHostname lowercases a DNS name and strips a trailing dot.
func normalizeHostname(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}

// clampUint16 converts an optional API integer into the uint16 range used by
// provider.SRVData. A nil pointer yields zero.
func clampUint16(v *int) uint16 {
	if v == nil {
		return 0
	}
	return uint16(min(max(0, *v), math.MaxUint16))
}
