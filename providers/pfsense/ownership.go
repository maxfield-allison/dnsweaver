package pfsense

import "strings"

// ownershipPrefix marks a host override description as dnsweaver-managed.
// Format: "dnsweaver:{instance}[ | user-supplied text]"
//
// pfSense host overrides expose no TXT records, so we piggy-back ownership
// signaling on the description field. The prefix lets operators see at a glance
// which pfSense records dnsweaver owns, and lets List() ignore operator-managed
// host overrides so orphan cleanup can never delete them.
const ownershipPrefix = "dnsweaver:"

// ownershipDescription returns the description string to write on records this
// instance manages.
func ownershipDescription(instanceName string) string {
	return ownershipPrefix + instanceName
}

// isOwnedBy returns true if a description was written by any dnsweaver instance.
// Used by List() to filter to dnsweaver-managed records only.
func isOwnedBy(description string) bool {
	return strings.HasPrefix(strings.TrimSpace(description), ownershipPrefix)
}

// ownedByInstance returns true if a description was written by the given
// dnsweaver instance specifically.
func ownedByInstance(description, instanceName string) bool {
	trimmed := strings.TrimSpace(description)
	if !strings.HasPrefix(trimmed, ownershipPrefix) {
		return false
	}
	rest := trimmed[len(ownershipPrefix):]
	if rest == instanceName {
		return true
	}
	for _, sep := range []string{" ", "|", "\t", "\n"} {
		if strings.HasPrefix(rest, instanceName+sep) {
			return true
		}
	}
	return false
}
