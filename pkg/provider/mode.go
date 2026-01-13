// Package provider - mode.go defines operational modes for provider instances.
package provider

import (
	"fmt"
	"strings"
)

// OperationalMode defines how a provider instance manages DNS records.
type OperationalMode string

const (
	// ModeManaged is the default mode. Only touch records that dnsweaver created and owns.
	// - Creates records for discovered services (with ownership TXT)
	// - Updates records when source data changes
	// - Deletes orphaned records that have ownership TXT
	// - NEVER touches records without ownership TXT
	ModeManaged OperationalMode = "managed"

	// ModeAuthoritative gives full control over configured scope.
	// Orphan cleanup includes any unmatched record within scope.
	// - Creates records for discovered services
	// - Updates records when source data changes
	// - Deletes ANY in-scope record without active source (regardless of ownership TXT)
	// - Respects capabilities (only touches record types provider supports)
	// - Scope limited to: in-scope domains + supported record types (A, AAAA, CNAME, SRV)
	ModeAuthoritative OperationalMode = "authoritative"

	// ModeAdditive is write-only mode. Never deletes any records.
	// - Creates records for discovered services
	// - Updates existing records when source data changes (if Updater interface supported)
	// - NEVER deletes records, even orphans with ownership TXT
	// - Still creates ownership TXT for tracking
	ModeAdditive OperationalMode = "additive"
)

// ValidModes lists all valid operational modes.
var ValidModes = []OperationalMode{ModeManaged, ModeAuthoritative, ModeAdditive}

// ParseOperationalMode parses a string into an OperationalMode.
// Returns ModeManaged if the input is empty (default).
// Returns an error if the input is not a valid mode.
func ParseOperationalMode(s string) (OperationalMode, error) {
	if s == "" {
		return ModeManaged, nil
	}

	mode := OperationalMode(strings.ToLower(strings.TrimSpace(s)))

	switch mode {
	case ModeManaged, ModeAuthoritative, ModeAdditive:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid operational mode %q: must be one of managed, authoritative, additive", s)
	}
}

// IsValid returns true if the mode is a valid operational mode.
func (m OperationalMode) IsValid() bool {
	switch m {
	case ModeManaged, ModeAuthoritative, ModeAdditive:
		return true
	default:
		return false
	}
}

// String returns the string representation of the mode.
func (m OperationalMode) String() string {
	return string(m)
}

// AllowsDelete returns true if the mode allows deleting orphaned records.
// Additive mode never deletes; Managed and Authoritative do (with different scopes).
func (m OperationalMode) AllowsDelete() bool {
	return m != ModeAdditive
}

// RequiresOwnership returns true if the mode requires ownership TXT records
// to delete a record. Managed mode requires ownership; Authoritative does not.
func (m OperationalMode) RequiresOwnership() bool {
	return m == ModeManaged || m == ""
}
