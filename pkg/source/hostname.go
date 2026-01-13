package source

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Hostname validation constants per RFC 1123.
const (
	// MaxHostnameLength is the maximum length of a full hostname (253 chars).
	MaxHostnameLength = 253

	// MaxLabelLength is the maximum length of a single label (63 chars).
	MaxLabelLength = 63
)

// Common hostname validation errors.
var (
	// ErrHostnameEmpty indicates an empty hostname.
	ErrHostnameEmpty = errors.New("hostname is empty")

	// ErrHostnameTooLong indicates hostname exceeds 253 characters.
	ErrHostnameTooLong = errors.New("hostname exceeds 253 characters")

	// ErrLabelTooLong indicates a single label exceeds 63 characters.
	ErrLabelTooLong = errors.New("hostname label exceeds 63 characters")

	// ErrLabelEmpty indicates an empty label (e.g., "app..example.com").
	ErrLabelEmpty = errors.New("hostname contains empty label")

	// ErrInvalidCharacters indicates hostname contains invalid characters.
	ErrInvalidCharacters = errors.New("hostname contains invalid characters")

	// ErrInvalidLabelStart indicates label starts with invalid character.
	ErrInvalidLabelStart = errors.New("hostname label must start with alphanumeric character")

	// ErrInvalidLabelEnd indicates label ends with invalid character.
	ErrInvalidLabelEnd = errors.New("hostname label must end with alphanumeric character")
)

// labelRegex matches valid hostname labels (RFC 1123).
// Labels must start and end with alphanumeric, can contain hyphens in the middle.
var labelRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?$`)

// singleCharLabelRegex matches single-character labels (valid: just alphanumeric).
var singleCharLabelRegex = regexp.MustCompile(`^[a-zA-Z0-9]$`)

// srvLabelRegex matches SRV record service/protocol labels (RFC 2782).
// These labels start with underscore followed by alphanumeric (e.g., _minecraft, _tcp, _udp).
var srvLabelRegex = regexp.MustCompile(`^_[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?$`)

// NormalizeHostname returns the canonical lowercase form of a hostname.
// DNS is case-insensitive per RFC 1035 Section 2.3.3, so this ensures
// consistent comparison and map key usage.
func NormalizeHostname(hostname string) string {
	return strings.ToLower(strings.TrimSuffix(hostname, "."))
}

// HostnameValidationError provides detailed information about validation failures.
type HostnameValidationError struct {
	Hostname string
	Label    string // The specific label that failed (if applicable)
	Err      error  // The underlying error
}

func (e *HostnameValidationError) Error() string {
	if e.Label != "" {
		return fmt.Sprintf("invalid hostname %q: label %q: %v", e.Hostname, e.Label, e.Err)
	}
	return fmt.Sprintf("invalid hostname %q: %v", e.Hostname, e.Err)
}

func (e *HostnameValidationError) Unwrap() error {
	return e.Err
}

// ValidateHostname validates a hostname according to RFC 1123.
//
// Rules:
//   - Total length <= 253 characters
//   - Each label (dot-separated part) <= 63 characters
//   - Labels must start and end with alphanumeric (a-z, A-Z, 0-9)
//   - Labels can contain hyphens in the middle
//   - No empty labels (no ".." or leading/trailing dots after normalization)
//   - Case insensitive (validation accepts both upper and lower case)
//
// Special handling:
//   - Trailing dots are stripped (DNS FQDN format)
//   - Wildcards (*.example.com) are accepted for the first label only
//
// Returns nil if valid, or a HostnameValidationError with details.
func ValidateHostname(hostname string) error {
	// Normalize: remove trailing dot (FQDN format)
	hostname = strings.TrimSuffix(hostname, ".")

	// Check empty
	if hostname == "" {
		return &HostnameValidationError{Hostname: hostname, Err: ErrHostnameEmpty}
	}

	// Check total length
	if len(hostname) > MaxHostnameLength {
		return &HostnameValidationError{Hostname: hostname, Err: ErrHostnameTooLong}
	}

	// Split into labels
	labels := strings.Split(hostname, ".")

	for i, label := range labels {
		// Check empty label
		if label == "" {
			return &HostnameValidationError{Hostname: hostname, Label: label, Err: ErrLabelEmpty}
		}

		// Check label length
		if len(label) > MaxLabelLength {
			return &HostnameValidationError{Hostname: hostname, Label: label, Err: ErrLabelTooLong}
		}

		// Special case: wildcard in first label
		if i == 0 && label == "*" {
			continue
		}

		// Validate label format
		if len(label) == 1 {
			if !singleCharLabelRegex.MatchString(label) {
				return &HostnameValidationError{Hostname: hostname, Label: label, Err: ErrInvalidCharacters}
			}
		} else {
			if !labelRegex.MatchString(label) {
				// Provide more specific error
				if !isAlphanumeric(label[0]) {
					return &HostnameValidationError{Hostname: hostname, Label: label, Err: ErrInvalidLabelStart}
				}
				if !isAlphanumeric(label[len(label)-1]) {
					return &HostnameValidationError{Hostname: hostname, Label: label, Err: ErrInvalidLabelEnd}
				}
				return &HostnameValidationError{Hostname: hostname, Label: label, Err: ErrInvalidCharacters}
			}
		}
	}

	return nil
}

// ValidateSRVHostname validates an SRV record hostname according to RFC 2782.
//
// SRV hostnames have the format: _service._protocol.name.domain.tld
// The first two labels (_service and _protocol) must start with underscore.
// Remaining labels follow standard RFC 1123 rules.
//
// Examples:
//   - _minecraft._tcp.mc.example.com
//   - _http._tcp.www.example.com
//   - _sip._udp.voip.example.com
//
// Returns nil if valid, or a HostnameValidationError with details.
func ValidateSRVHostname(hostname string) error {
	// Normalize: remove trailing dot (FQDN format)
	hostname = strings.TrimSuffix(hostname, ".")

	// Check empty
	if hostname == "" {
		return &HostnameValidationError{Hostname: hostname, Err: ErrHostnameEmpty}
	}

	// Check total length
	if len(hostname) > MaxHostnameLength {
		return &HostnameValidationError{Hostname: hostname, Err: ErrHostnameTooLong}
	}

	// Split into labels
	labels := strings.Split(hostname, ".")

	// SRV hostnames must have at least 3 labels: _service._proto.name
	if len(labels) < 3 {
		return &HostnameValidationError{
			Hostname: hostname,
			Err:      errors.New("SRV hostname must have at least 3 labels (_service._proto.name)"),
		}
	}

	for i, label := range labels {
		// Check empty label
		if label == "" {
			return &HostnameValidationError{Hostname: hostname, Label: label, Err: ErrLabelEmpty}
		}

		// Check label length
		if len(label) > MaxLabelLength {
			return &HostnameValidationError{Hostname: hostname, Label: label, Err: ErrLabelTooLong}
		}

		// First two labels must be SRV-style (underscore prefix)
		if i < 2 {
			if !srvLabelRegex.MatchString(label) {
				return &HostnameValidationError{
					Hostname: hostname,
					Label:    label,
					Err:      errors.New("SRV service/protocol label must start with underscore"),
				}
			}
			continue
		}

		// Remaining labels follow RFC 1123 rules
		if len(label) == 1 {
			if !singleCharLabelRegex.MatchString(label) {
				return &HostnameValidationError{Hostname: hostname, Label: label, Err: ErrInvalidCharacters}
			}
		} else {
			if !labelRegex.MatchString(label) {
				if !isAlphanumeric(label[0]) {
					return &HostnameValidationError{Hostname: hostname, Label: label, Err: ErrInvalidLabelStart}
				}
				if !isAlphanumeric(label[len(label)-1]) {
					return &HostnameValidationError{Hostname: hostname, Label: label, Err: ErrInvalidLabelEnd}
				}
				return &HostnameValidationError{Hostname: hostname, Label: label, Err: ErrInvalidCharacters}
			}
		}
	}

	return nil
}

// isAlphanumeric returns true if the byte is a-z, A-Z, or 0-9.
func isAlphanumeric(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// SRVHints contains SRV record-specific hints from source labels.
type SRVHints struct {
	Priority uint16 // Lower values = higher priority (0-65535)
	Weight   uint16 // Load balancing among same-priority servers (0-65535)
	Port     uint16 // TCP/UDP port number (1-65535)
}

// RecordHints contains optional hints for DNS record creation.
// These allow sources (particularly native dnsweaver labels) to specify
// record details that override provider defaults.
//
// All fields are optional - nil/zero values mean "use provider defaults".
type RecordHints struct {
	// Type overrides the record type (A, AAAA, CNAME, SRV, PTR, TXT).
	// Empty means use provider default.
	Type string

	// Target overrides the record target (IP for A/AAAA, hostname for CNAME/SRV).
	// Empty means use provider default.
	Target string

	// TTL overrides the record TTL.
	// Zero means use provider default.
	TTL int

	// Provider targets a specific provider instance by name.
	// Empty means use domain matching as usual.
	Provider string

	// SRV contains SRV-specific fields when Type is "SRV".
	SRV *SRVHints
}

// Hostname represents a hostname extracted from container labels.
//
// Each hostname carries context about where it was discovered, which is
// useful for logging, debugging, and potentially for determining which
// DNS provider should handle it.
type Hostname struct {
	// Name is the fully qualified hostname (e.g., "app.example.com").
	Name string

	// Source identifies which source extracted this hostname (e.g., "traefik").
	// This matches the value returned by Source.Name().
	Source string

	// Router is an optional identifier for the router/upstream that defined this hostname.
	// For Traefik, this would be the router name (e.g., "myapp@docker").
	// For dnsweaver labels, this is the record name (e.g., "myapp" from dnsweaver.records.myapp.*).
	// May be empty if the source doesn't support this concept.
	Router string

	// RecordHints contains optional hints for DNS record creation.
	// These allow per-hostname overrides for record type, target, TTL, and provider.
	// nil means use provider defaults for everything.
	RecordHints *RecordHints
}

// HasRecordHints returns true if this hostname has any record hints set.
func (h Hostname) HasRecordHints() bool {
	return h.RecordHints != nil
}

// String returns a human-readable representation of the hostname.
func (h Hostname) String() string {
	if h.Router != "" {
		return h.Name + " (from " + h.Source + ":" + h.Router + ")"
	}
	return h.Name + " (from " + h.Source + ")"
}

// Validate checks if the hostname conforms to the appropriate RFC.
// For SRV records (RecordHints.Type == "SRV"), uses RFC 2782 validation.
// For all other records, uses RFC 1123 validation.
// Returns nil if valid, or a HostnameValidationError with details.
func (h Hostname) Validate() error {
	if h.RecordHints != nil && h.RecordHints.Type == "SRV" {
		return ValidateSRVHostname(h.Name)
	}
	return ValidateHostname(h.Name)
}

// IsValid returns true if the hostname is valid according to the appropriate RFC.
// For SRV records (RecordHints.Type == "SRV"), uses RFC 2782 validation.
// For all other records, uses RFC 1123 validation.
func (h Hostname) IsValid() bool {
	if h.RecordHints != nil && h.RecordHints.Type == "SRV" {
		return ValidateSRVHostname(h.Name) == nil
	}
	return ValidateHostname(h.Name) == nil
}

// NormalizedName returns the canonical lowercase form of this hostname.
// DNS is case-insensitive per RFC 1035 Section 2.3.3, so use this for
// map keys and comparisons where case-insensitive semantics are required.
func (h Hostname) NormalizedName() string {
	return NormalizeHostname(h.Name)
}

// Hostnames is a slice of Hostname with helper methods.
type Hostnames []Hostname

// Names returns just the hostname strings from the slice.
func (hs Hostnames) Names() []string {
	names := make([]string, len(hs))
	for i, h := range hs {
		names[i] = h.Name
	}
	return names
}

// Deduplicate returns a new slice with duplicate hostnames removed.
// The first occurrence of each hostname is kept.
// Comparison is case-insensitive per DNS RFC 1035 Section 2.3.3.
func (hs Hostnames) Deduplicate() Hostnames {
	seen := make(map[string]struct{}, len(hs))
	result := make(Hostnames, 0, len(hs))

	for _, h := range hs {
		normalized := h.NormalizedName()
		if _, exists := seen[normalized]; !exists {
			seen[normalized] = struct{}{}
			result = append(result, h)
		}
	}

	return result
}

// Filter returns a new slice containing only hostnames where the predicate returns true.
func (hs Hostnames) Filter(predicate func(Hostname) bool) Hostnames {
	result := make(Hostnames, 0)
	for _, h := range hs {
		if predicate(h) {
			result = append(result, h)
		}
	}
	return result
}

// FromSource returns a new slice containing only hostnames from the specified source.
func (hs Hostnames) FromSource(sourceName string) Hostnames {
	return hs.Filter(func(h Hostname) bool {
		return h.Source == sourceName
	})
}

// ValidHostnames returns a new slice containing only valid hostnames.
// Invalid hostnames are filtered out (use ValidateAll to get errors).
func (hs Hostnames) ValidHostnames() Hostnames {
	return hs.Filter(func(h Hostname) bool {
		return h.IsValid()
	})
}

// ValidationResult contains the results of validating a set of hostnames.
type ValidationResult struct {
	Valid   Hostnames                  // Hostnames that passed validation
	Invalid []HostnameValidationResult // Hostnames that failed validation with error details
}

// HostnameValidationResult pairs a hostname with its validation error.
type HostnameValidationResult struct {
	Hostname Hostname
	Error    error
}

// ValidateAll validates all hostnames and returns valid and invalid lists.
// This is useful for logging invalid hostnames while still processing valid ones.
func (hs Hostnames) ValidateAll() ValidationResult {
	result := ValidationResult{
		Valid:   make(Hostnames, 0, len(hs)),
		Invalid: make([]HostnameValidationResult, 0),
	}

	for _, h := range hs {
		if err := h.Validate(); err != nil {
			result.Invalid = append(result.Invalid, HostnameValidationResult{
				Hostname: h,
				Error:    err,
			})
		} else {
			result.Valid = append(result.Valid, h)
		}
	}

	return result
}
