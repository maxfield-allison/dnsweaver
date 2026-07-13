package target

import (
	"context"
	"fmt"
	"net"
)

// InterfaceResolver reads the primary IP address of a named local network
// interface. It performs no external calls, so it works inside containers as
// long as the interface is visible to the process.
type InterfaceResolver struct {
	name   string
	family Family
	// lookup is the interface-address lookup, overridable in tests. Defaults to
	// interfaceAddrs (backed by the stdlib net package).
	lookup func(name string) ([]net.Addr, error)
}

// NewInterfaceResolver creates an InterfaceResolver for the named interface and
// address family.
func NewInterfaceResolver(name string, family Family) *InterfaceResolver {
	return &InterfaceResolver{name: name, family: family, lookup: interfaceAddrs}
}

// Describe implements Resolver.
func (r *InterfaceResolver) Describe() string {
	return fmt.Sprintf("interface %s (%s)", r.name, r.family)
}

// Resolve implements Resolver. It returns the first global unicast address of
// the configured family on the interface.
func (r *InterfaceResolver) Resolve(_ context.Context) (string, error) {
	addrs, err := r.lookup(r.name)
	if err != nil {
		return "", fmt.Errorf("interface %q: %w", r.name, err)
	}

	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		default:
			continue
		}
		// Skip loopback, link-local, and non-global addresses so we pick a
		// routable address (mirrors the Incus IP resolver's selection).
		if !ip.IsGlobalUnicast() || ip.IsLinkLocalUnicast() {
			continue
		}
		if r.family.matches(ip) {
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("interface %q has no global %s address", r.name, r.family)
}

// interfaceAddrs returns the addresses assigned to the named interface.
func interfaceAddrs(name string) ([]net.Addr, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, err
	}
	return iface.Addrs()
}
