// Package target provides resolvers that determine a DNS record's target value
// at runtime instead of requiring a literal IP in configuration.
//
// A provider instance's target is normally a literal IP (A/AAAA) or hostname
// (CNAME). When DNSWEAVER_{NAME}_TARGET_MODE is set to a known keyword, the
// target is resolved dynamically:
//
//   - "public": the host's public IP, discovered via HTTP echo endpoints
//     (checkip.amazonaws.com, api.ipify.org, ...). Doubles as a dynamic-DNS
//     use case.
//   - "interface:<name>": the primary IP of a named network interface, read
//     from the local network stack (no external calls).
//
// Resolvers are family-aware: an A record resolves an IPv4 address and an AAAA
// record resolves an IPv6 address. Resolution failures are surfaced to the
// caller, which is expected to keep the last known-good value (see the refresh
// manager in cmd/dnsweaver).
package target

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// Family is the IP address family a resolver should return.
type Family int

const (
	// FamilyIPv4 selects IPv4 addresses (A records).
	FamilyIPv4 Family = iota
	// FamilyIPv6 selects IPv6 addresses (AAAA records).
	FamilyIPv6
)

// String returns a human-readable family name.
func (f Family) String() string {
	if f == FamilyIPv6 {
		return "ipv6"
	}
	return "ipv4"
}

// matches reports whether ip belongs to this family.
func (f Family) matches(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if f == FamilyIPv6 {
		return ip.To4() == nil
	}
	return ip.To4() != nil
}

// Resolver determines a DNS record target at runtime.
type Resolver interface {
	// Resolve returns the current target value, or an error if it cannot be
	// determined. Callers should keep the last known-good value on error.
	Resolve(ctx context.Context) (string, error)
	// Describe returns a short human-readable description of the resolver for
	// logging (e.g. "public IP" or "interface eth0").
	Describe() string
}

// Parse builds a Resolver from a DNSWEAVER_{NAME}_TARGET_MODE value. An empty
// value returns a nil Resolver and nil error, meaning the literal TARGET should
// be used. Unknown values return an error.
//
// Supported values:
//   - "public"            -> PublicResolver with default endpoints
//   - "interface:<name>"  -> InterfaceResolver for the named NIC
func Parse(mode string, family Family) (Resolver, error) {
	m := strings.TrimSpace(mode)
	switch {
	case m == "":
		return nil, nil
	case strings.EqualFold(m, "public"):
		return NewPublicResolver(family, nil), nil
	case strings.HasPrefix(strings.ToLower(m), "interface:"):
		name := strings.TrimSpace(m[len("interface:"):])
		if name == "" {
			return nil, fmt.Errorf("target mode %q: interface name is required (e.g. interface:eth0)", mode)
		}
		return NewInterfaceResolver(name, family), nil
	default:
		return nil, fmt.Errorf("invalid target mode %q (must be one of: public, interface:<name>)", mode)
	}
}
