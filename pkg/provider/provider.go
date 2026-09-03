// Package provider defines the interface that all DNS providers must implement.
package provider

import (
	"context"
	"encoding/base64"
	"net"
	"sort"
	"strconv"
	"strings"
)

// RecordType represents the type of DNS record.
type RecordType string

const (
	RecordTypeA     RecordType = "A"
	RecordTypeAAAA  RecordType = "AAAA"
	RecordTypeCNAME RecordType = "CNAME"
	RecordTypeTXT   RecordType = "TXT"
	RecordTypeSRV   RecordType = "SRV"
	RecordTypeHTTPS RecordType = "HTTPS"
)

// OwnershipPrefix is the default prefix for ownership TXT records.
const OwnershipPrefix = "_dnsweaver"

// OwnershipValue is the content of ownership TXT records when no instance ID is set.
// For backward compatibility, this is the legacy format used in single-instance mode.
const OwnershipValue = "heritage=dnsweaver"

// ownershipHeritage is the base heritage value used in all ownership records.
const ownershipHeritage = "heritage=dnsweaver"

// Reserved ownership keys that are not included in metadata.
const (
	keyHeritage = "heritage"
	keyInstance = "instance"

	memberOwnershipVersion   = "2"
	keyRecordVersion         = "record-version"
	keyRecordType            = "record-type"
	keyRecordTarget          = "record-target"
	keyRecordSRVPriority     = "record-srv-priority"
	keyRecordSRVWeight       = "record-srv-weight"
	keyRecordSRVPort         = "record-srv-port"
	keyRecordHTTPSPriority   = "record-https-priority"
	keyRecordHTTPSTargetName = "record-https-target-name"
	keyRecordHTTPSALPN       = "record-https-alpn"
)

// MakeOwnershipValue returns the ownership TXT record value for the given instance ID and metadata.
// If instanceID is empty, uses the legacy format "heritage=dnsweaver".
// If instanceID is set, returns "heritage=dnsweaver,instance=<id>".
// If metadata is non-empty, appends sorted key=value pairs.
// Reserved keys ("heritage", "instance") in metadata are silently ignored.
func MakeOwnershipValue(instanceID string, metadata map[string]string) string {
	var b strings.Builder
	b.WriteString(ownershipHeritage)

	if instanceID != "" {
		b.WriteString("," + keyInstance + "=")
		b.WriteString(instanceID)
	}

	if len(metadata) > 0 {
		// Sort keys for deterministic output
		keys := make([]string, 0, len(metadata))
		for k := range metadata {
			// Skip reserved keys
			if k == keyHeritage || k == keyInstance {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			b.WriteByte(',')
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(metadata[k])
		}
	}

	return b.String()
}

// ParseOwnershipValue parses an ownership TXT record value.
// Returns whether the record is a dnsweaver ownership record, the instance ID (if present),
// and any additional metadata key-value pairs.
// Examples:
//
//	"heritage=dnsweaver"                            -> (true, "", nil)
//	"heritage=dnsweaver,instance=pi5-dns"            -> (true, "pi5-dns", nil)
//	"heritage=dnsweaver,instance=pi5-dns,proxied=true" -> (true, "pi5-dns", {"proxied": "true"})
//	"some other value"                               -> (false, "", nil)
func ParseOwnershipValue(value string) (isOwned bool, instanceID string, metadata map[string]string) {
	if !strings.HasPrefix(value, ownershipHeritage) {
		return false, "", nil
	}
	rest := value[len(ownershipHeritage):]
	if rest == "" {
		return true, "", nil
	}
	if !strings.HasPrefix(rest, ",") {
		return false, "", nil
	}
	// Parse comma-separated key=value pairs
	for _, part := range strings.Split(rest[1:], ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case keyInstance:
			instanceID = kv[1]
		case keyHeritage:
			// Skip reserved key
		default:
			if metadata == nil {
				metadata = make(map[string]string)
			}
			metadata[kv[0]] = kv[1]
		}
	}
	return true, instanceID, metadata
}

// MatchesOwnership checks if a TXT record value matches our instance's ownership.
// An empty ourInstanceID matches only legacy records (no instance tag).
// A non-empty ourInstanceID matches only records with that specific instance tag.
func MatchesOwnership(value, ourInstanceID string) bool {
	isOwned, recordInstanceID, _ := ParseOwnershipValue(value)
	if !isOwned {
		return false
	}
	return recordInstanceID == ourInstanceID
}

// IsDnsweaverOwned checks if a TXT record value indicates ownership by any dnsweaver instance.
// This is used for discovery/recovery regardless of which instance created the record.
func IsDnsweaverOwned(value string) bool {
	isOwned, _, _ := ParseOwnershipValue(value)
	return isOwned
}

// MakeMemberOwnershipValue returns a versioned ownership marker for one DNS
// record member. Target-like strings are base64url encoded so commas and other
// TXT metadata delimiters cannot make the marker ambiguous.
func MakeMemberOwnershipValue(instanceID string, record Record, metadata map[string]string) string {
	fields := make(map[string]string, len(metadata)+8)
	for key, value := range metadata {
		fields[key] = value
	}
	fields[keyRecordVersion] = memberOwnershipVersion
	fields[keyRecordType] = string(record.Type)
	fields[keyRecordTarget] = encodeOwnershipField(record.Target)

	if record.Type == RecordTypeSRV && record.SRV != nil {
		fields[keyRecordSRVPriority] = strconv.FormatUint(uint64(record.SRV.Priority), 10)
		fields[keyRecordSRVWeight] = strconv.FormatUint(uint64(record.SRV.Weight), 10)
		fields[keyRecordSRVPort] = strconv.FormatUint(uint64(record.SRV.Port), 10)
	}
	if record.Type == RecordTypeHTTPS && record.HTTPS != nil {
		fields[keyRecordHTTPSPriority] = strconv.FormatUint(uint64(record.HTTPS.Priority), 10)
		fields[keyRecordHTTPSTargetName] = encodeOwnershipField(record.HTTPS.TargetName)
		fields[keyRecordHTTPSALPN] = encodeOwnershipField(record.HTTPS.ALPN)
	}

	return MakeOwnershipValue(instanceID, fields)
}

// ParseMemberOwnershipValue parses a versioned member marker. Legacy or
// malformed markers remain recognizable as dnsweaver ownership but return a
// nil member, which forces callers to treat them as ambiguous.
func ParseMemberOwnershipValue(value string) (isOwned bool, instanceID string, member *Record, metadata map[string]string) {
	isOwned, instanceID, fields := ParseOwnershipValue(value)
	if !isOwned {
		return false, "", nil, nil
	}
	if fields[keyRecordVersion] != memberOwnershipVersion {
		return true, instanceID, nil, fields
	}

	recordTypeValue, hasRecordType := fields[keyRecordType]
	recordType := RecordType(recordTypeValue)
	if !hasRecordType || !validOwnershipRecordType(recordType) {
		return true, instanceID, nil, stripMemberOwnershipFields(fields)
	}
	encodedTarget, hasTarget := fields[keyRecordTarget]
	if !hasTarget {
		return true, instanceID, nil, stripMemberOwnershipFields(fields)
	}
	target, err := decodeOwnershipField(encodedTarget)
	if err != nil {
		return true, instanceID, nil, stripMemberOwnershipFields(fields)
	}

	record := &Record{Type: recordType, Target: target}
	switch recordType {
	case RecordTypeSRV:
		priority, priorityOK := parseOwnershipUint16(fields[keyRecordSRVPriority])
		weight, weightOK := parseOwnershipUint16(fields[keyRecordSRVWeight])
		port, portOK := parseOwnershipUint16(fields[keyRecordSRVPort])
		if !priorityOK || !weightOK || !portOK {
			return true, instanceID, nil, stripMemberOwnershipFields(fields)
		}
		record.SRV = &SRVData{Priority: priority, Weight: weight, Port: port}
	case RecordTypeHTTPS:
		priority, priorityOK := parseOwnershipUint16(fields[keyRecordHTTPSPriority])
		targetName, targetErr := decodeOwnershipField(fields[keyRecordHTTPSTargetName])
		alpn, alpnErr := decodeOwnershipField(fields[keyRecordHTTPSALPN])
		if !priorityOK || targetErr != nil || alpnErr != nil {
			return true, instanceID, nil, stripMemberOwnershipFields(fields)
		}
		record.HTTPS = &HTTPSData{Priority: priority, TargetName: targetName, ALPN: alpn}
	}

	return true, instanceID, record, stripMemberOwnershipFields(fields)
}

func validOwnershipRecordType(recordType RecordType) bool {
	switch recordType {
	case RecordTypeA, RecordTypeAAAA, RecordTypeCNAME, RecordTypeTXT, RecordTypeSRV, RecordTypeHTTPS:
		return true
	default:
		return false
	}
}

// MatchesMemberOwnership reports whether value is a valid member marker for
// the requested dnsweaver instance and logical DNS record member.
func MatchesMemberOwnership(value, instanceID string, record Record) bool {
	isOwned, markerInstance, member, _ := ParseMemberOwnershipValue(value)
	return isOwned && markerInstance == instanceID && member != nil && SameRecordMember(*member, record)
}

// SameRecordMember compares the fields that identify a member of an RRset.
// Hostname and TTL are intentionally excluded: ownership record names carry
// the hostname, and TTL is mutable state rather than member identity.
func SameRecordMember(a, b Record) bool {
	if a.Type != b.Type || canonicalRecordTarget(a) != canonicalRecordTarget(b) {
		return false
	}
	if a.Type == RecordTypeSRV {
		return sameSRVData(a.SRV, b.SRV)
	}
	if a.Type == RecordTypeHTTPS {
		return sameHTTPSData(a.HTTPS, b.HTTPS)
	}
	return true
}

func canonicalRecordTarget(record Record) string {
	if record.Type == RecordTypeA || record.Type == RecordTypeAAAA {
		if ip := net.ParseIP(strings.TrimSpace(record.Target)); ip != nil {
			return ip.String()
		}
	}
	if record.Type == RecordTypeCNAME || record.Type == RecordTypeSRV {
		return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(record.Target), "."))
	}
	return record.Target
}

func sameSRVData(a, b *SRVData) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Priority == b.Priority && a.Weight == b.Weight && a.Port == b.Port
}

func sameHTTPSData(a, b *HTTPSData) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Priority == b.Priority &&
		strings.EqualFold(strings.TrimSuffix(a.TargetName, "."), strings.TrimSuffix(b.TargetName, ".")) &&
		a.ALPN == b.ALPN
}

func encodeOwnershipField(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeOwnershipField(value string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return string(decoded), err
}

func parseOwnershipUint16(value string) (uint16, bool) {
	parsed, err := strconv.ParseUint(value, 10, 16)
	return uint16(parsed), err == nil
}

func stripMemberOwnershipFields(fields map[string]string) map[string]string {
	if len(fields) == 0 {
		return nil
	}
	metadata := make(map[string]string, len(fields))
	for key, value := range fields {
		switch key {
		case keyRecordVersion, keyRecordType, keyRecordTarget,
			keyRecordSRVPriority, keyRecordSRVWeight, keyRecordSRVPort,
			keyRecordHTTPSPriority, keyRecordHTTPSTargetName, keyRecordHTTPSALPN:
			continue
		default:
			metadata[key] = value
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

// SRVData contains SRV record-specific fields.
// Used when Type is RecordTypeSRV.
type SRVData struct {
	Priority uint16 // Lower values = higher priority (0-65535)
	Weight   uint16 // Load balancing among same-priority servers (0-65535)
	Port     uint16 // TCP/UDP port number (1-65535)
}

// HTTPSData contains HTTPS (SVCB Type 65) record-specific fields.
// Used when Type is RecordTypeHTTPS.
// See RFC 9460 for the HTTPS/SVCB record specification.
type HTTPSData struct {
	// Priority is the SvcPriority value. 0 = AliasMode, 1+ = ServiceMode.
	// For ECH override records, typically 1.
	Priority uint16

	// TargetName is the SvcTargetName. "." means the record's owner name (self-referential).
	// For ECH override records, typically ".".
	TargetName string

	// ALPN is the application-layer protocol negotiation value (e.g., "h2", "h3").
	// Sent as svcParams "alpn|h2" in Technitium's API format.
	ALPN string
}

// Record represents a DNS record to be managed.
type Record struct {
	Hostname   string
	Type       RecordType
	Target     string // IP for A/AAAA, hostname for CNAME/SRV target
	TTL        int
	ProviderID string   // Provider-specific record identifier
	SRV        *SRVData // SRV-specific data (only set when Type is SRV)

	// HTTPS holds HTTPS/SVCB record-specific data (only set when Type is HTTPS).
	HTTPS *HTTPSData

	// Metadata carries provider-specific key-value pairs through the reconciliation pipeline.
	// Providers read actionable keys (e.g., "proxied" for Cloudflare) during Create/Update.
	// nil means no metadata (Go zero value, non-breaking addition).
	Metadata map[string]string
}

// Capabilities describes a provider's feature support.
// Used by the reconciler to adapt behavior based on provider limitations.
type Capabilities struct {
	// SupportsOwnershipTXT indicates if the provider can create TXT records
	// for ownership tracking. File-based providers (dnsmasq) typically cannot.
	SupportsOwnershipTXT bool

	// SupportsNativeUpdate indicates if the provider has a native update operation.
	// If false, updates require delete+create. Providers with native update should
	// also implement the Updater interface.
	SupportsNativeUpdate bool

	// SupportedRecordTypes lists the DNS record types this provider can manage.
	// Used to filter operations in authoritative mode and validate requested records.
	SupportedRecordTypes []RecordType
}

// SupportsRecordType returns true if the provider supports the given record type.
func (c Capabilities) SupportsRecordType(rt RecordType) bool {
	for _, t := range c.SupportedRecordTypes {
		if t == rt {
			return true
		}
	}
	return false
}

// Provider defines the interface for DNS providers.
// Each provider implementation (Technitium, Cloudflare, etc.) must satisfy this interface.
type Provider interface {
	// Name returns the provider instance name (e.g., "internal-dns").
	Name() string

	// Type returns the provider type (e.g., "technitium", "cloudflare").
	Type() string

	// Ping checks connectivity to the provider.
	Ping(ctx context.Context) error

	// Capabilities returns the provider's feature support.
	// Used by the reconciler to adapt behavior based on provider limitations.
	Capabilities() Capabilities

	// List returns all managed records in the configured zone.
	List(ctx context.Context) ([]Record, error)

	// Create adds a new DNS record.
	Create(ctx context.Context, record Record) error

	// Delete removes a DNS record.
	Delete(ctx context.Context, record Record) error
}

// Updater is an optional interface that providers can implement to support
// native in-place record updates. This is more efficient than delete+create
// and avoids brief DNS gaps when changing record values.
//
// The reconciler will check if a provider implements Updater and use it when
// available. If not, the reconciler falls back to delete+create.
//
// Providers that implement Updater should also set Capabilities().SupportsNativeUpdate = true.
type Updater interface {
	// Update modifies an existing DNS record in place.
	// The existing record is identified by its current values (hostname, type, target).
	// The desired record contains the new values to apply.
	//
	// Implementations should:
	// - Only modify fields that differ between existing and desired
	// - Return ErrRecordNotFound if the existing record doesn't exist
	// - Be idempotent (calling with identical records is a no-op)
	Update(ctx context.Context, existing, desired Record) error
}

// RecordComparer is an optional interface for providers whose records carry
// state that hostname, type, target, TTL and SRV data do not capture (for
// Cloudflare, the proxied flag). When an existing record already matches a
// desired one on those fields, the reconciler asks a RecordComparer whether it
// would nevertheless write something different, and updates the record in
// place if so.
//
// Implementations should resolve desired exactly as Create and Update do,
// applying the instance default and any per-record Metadata override, so that
// a changed default is detected as well as a changed override. They should
// compare only state they themselves report on List, and return false when
// existing carries none, so a record the provider cannot tell apart from the
// desired one is never rewritten.
//
// Providers that do not implement RecordComparer are compared on the generic
// fields only.
type RecordComparer interface {
	// RecordNeedsUpdate reports whether existing must be rewritten to match
	// what the provider would write for desired. Hostname, type and target
	// are already known to match when this is called.
	RecordNeedsUpdate(existing, desired Record) bool
}

// Closer is an optional interface that providers can implement to release
// resources held for the lifetime of the instance (e.g. SSH/SFTP sessions or
// database connections). The registry calls Close on shutdown for any provider
// that implements it. Implementations must be safe to call multiple times.
type Closer interface {
	Close() error
}

// RecordEquals returns true if two records are logically equal.
// Provider-specific IDs are not compared.
func RecordEquals(a, b Record) bool {
	if a.Hostname != b.Hostname || a.Type != b.Type || a.Target != b.Target || a.TTL != b.TTL {
		return false
	}

	// For SRV records, also compare SRV-specific data
	if a.Type == RecordTypeSRV {
		if a.SRV == nil && b.SRV == nil {
			return true
		}
		if a.SRV == nil || b.SRV == nil {
			return false
		}
		return a.SRV.Priority == b.SRV.Priority &&
			a.SRV.Weight == b.SRV.Weight &&
			a.SRV.Port == b.SRV.Port
	}

	// For HTTPS records, also compare HTTPS-specific data
	if a.Type == RecordTypeHTTPS {
		if a.HTTPS == nil && b.HTTPS == nil {
			return true
		}
		if a.HTTPS == nil || b.HTTPS == nil {
			return false
		}
		return a.HTTPS.Priority == b.HTTPS.Priority &&
			a.HTTPS.TargetName == b.HTTPS.TargetName &&
			a.HTTPS.ALPN == b.HTTPS.ALPN
	}

	return true
}

// OwnershipRecordName returns the TXT record name for ownership tracking.
// Example: "app.example.com" -> "_dnsweaver.app.example.com"
func OwnershipRecordName(hostname string) string {
	return OwnershipPrefix + "." + hostname
}

// IsOwnershipRecord returns true if the hostname is an ownership TXT record.
func IsOwnershipRecord(hostname string) bool {
	return len(hostname) > len(OwnershipPrefix)+1 &&
		hostname[:len(OwnershipPrefix)+1] == OwnershipPrefix+"."
}

// ExtractHostnameFromOwnership extracts the original hostname from an ownership record name.
// Example: "_dnsweaver.app.example.com" -> "app.example.com"
// Returns empty string if the hostname is not an ownership record.
func ExtractHostnameFromOwnership(ownershipName string) string {
	if !IsOwnershipRecord(ownershipName) {
		return ""
	}
	return ownershipName[len(OwnershipPrefix)+1:]
}

// OwnershipRecord creates a TXT record for ownership tracking.
// If instanceID is empty, uses the legacy format for backward compatibility.
// If metadata is non-nil, it is serialized into the TXT value.
func OwnershipRecord(hostname string, ttl int, instanceID string, metadata map[string]string) Record {
	return Record{
		Hostname: OwnershipRecordName(hostname),
		Type:     RecordTypeTXT,
		Target:   MakeOwnershipValue(instanceID, metadata),
		TTL:      ttl,
	}
}

// MemberOwnershipRecord creates a versioned TXT ownership record for one
// logical DNS record member.
func MemberOwnershipRecord(hostname string, ttl int, instanceID string, member Record, metadata map[string]string) Record {
	return Record{
		Hostname: OwnershipRecordName(hostname),
		Type:     RecordTypeTXT,
		Target:   MakeMemberOwnershipValue(instanceID, member, metadata),
		TTL:      ttl,
	}
}
