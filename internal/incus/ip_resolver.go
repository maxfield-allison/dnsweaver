package incus

import (
	"net"
	"sort"
)

const (
	addressFamilyInet  = "inet"
	addressScopeGlobal = "global"
)

// ResolveIP returns the primary global-scope IPv4 address for an instance,
// read from its runtime network state.
//
// Interface names are iterated in sorted order so the result is deterministic
// across reconciles even when an instance has multiple interfaces — a map's
// non-deterministic iteration order would otherwise cause DNS records to flap.
// Loopback interfaces, non-global scopes, and non-routable addresses are
// skipped.
//
// Returns an empty string if no suitable address is found. The empty string
// acts as a liveness gate: the source skips instances with no resolvable IP.
func ResolveIP(inst Instance) string {
	if inst.State == nil {
		return ""
	}

	names := make([]string, 0, len(inst.State.Network))
	for name := range inst.State.Network {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if isLoopback(name) {
			continue
		}
		for _, addr := range inst.State.Network[name].Addresses {
			if addr.Family != addressFamilyInet {
				continue
			}
			// Incus reports scope as "global", "link", or "local". Treat an
			// empty scope as acceptable for forward compatibility.
			if addr.Scope != "" && addr.Scope != addressScopeGlobal {
				continue
			}
			if isNonRoutableIP(addr.Address) {
				continue
			}
			return addr.Address
		}
	}

	return ""
}

// isLoopback returns true for known loopback interface names.
func isLoopback(name string) bool {
	return name == "lo" || name == "lo0"
}

// nonRoutableRanges contains IPv4 CIDR blocks that are reserved or not suitable
// for use as DNS record targets. These supplement the ranges already handled by
// net.IP's built-in methods (IsLoopback, IsLinkLocalUnicast, IsUnspecified,
// IsMulticast).
var nonRoutableRanges = func() []*net.IPNet {
	cidrs := []string{
		// 100.64.0.0/10 (RFC 6598 CGNAT) is intentionally NOT filtered:
		// Tailscale assigns addresses from this range, and DNS records pointing
		// to an instance's Tailscale IP are a common, legitimate homelab use case.
		"192.0.0.0/24",       // IETF Protocol Assignments (RFC 6890)
		"192.0.2.0/24",       // TEST-NET-1 (RFC 5737)
		"198.18.0.0/15",      // Network interconnect benchmarking (RFC 2544)
		"198.51.100.0/24",    // TEST-NET-2 (RFC 5737)
		"203.0.113.0/24",     // TEST-NET-3 (RFC 5737)
		"240.0.0.0/4",        // Reserved for future use (RFC 1112)
		"255.255.255.255/32", // Limited broadcast
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, n, _ := net.ParseCIDR(cidr)
		nets = append(nets, n)
	}
	return nets
}()

// isNonRoutableIP reports whether ip should be skipped as a DNS record target.
//
// It rejects addresses that are non-routable or not suitable for external
// resolution:
//   - Loopback (127.0.0.0/8)
//   - Link-local / APIPA (169.254.0.0/16)
//   - Unspecified (0.0.0.0)
//   - Multicast (224.0.0.0/4)
//   - Reserved, documentation, and benchmarking ranges (see nonRoutableRanges)
//   - Any address that cannot be parsed
//
// RFC 1918 private addresses (10/8, 172.16/12, 192.168/16) are intentionally
// kept as valid targets — these are the addresses most homelab instances use.
func isNonRoutableIP(ip string) bool {
	if ip == "" {
		return true
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return true
	}
	if parsed.IsLoopback() || parsed.IsLinkLocalUnicast() || parsed.IsUnspecified() || parsed.IsMulticast() {
		return true
	}
	for _, n := range nonRoutableRanges {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}
