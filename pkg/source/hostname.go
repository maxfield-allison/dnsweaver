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

// isAlphanumeric returns true if the byte is a-z, A-Z, or 0-9.
func isAlphanumeric(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
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
	// May be empty if the source doesn't support this concept.
	Router string
}

// String returns a human-readable representation of the hostname.
func (h Hostname) String() string {
	if h.Router != "" {
		return h.Name + " (from " + h.Source + ":" + h.Router + ")"
	}
	return h.Name + " (from " + h.Source + ")"
}

// Validate checks if the hostname conforms to RFC 1123.
// Returns nil if valid, or a HostnameValidationError with details.
func (h Hostname) Validate() error {
	return ValidateHostname(h.Name)
}

// IsValid returns true if the hostname is valid according to RFC 1123.
func (h Hostname) IsValid() bool {
	return ValidateHostname(h.Name) == nil
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
func (hs Hostnames) Deduplicate() Hostnames {
	seen := make(map[string]struct{}, len(hs))
	result := make(Hostnames, 0, len(hs))

	for _, h := range hs {
		if _, exists := seen[h.Name]; !exists {
			seen[h.Name] = struct{}{}
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
