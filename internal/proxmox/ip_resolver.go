package proxmox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
)

// Keys within a PVE LXC netN config value ("name=eth0,ip=10.0.0.5/24,...").
const (
	netKeyName = "name"
	netKeyIP   = "ip"
	netKeyIP6  = "ip6"
)

// ResolvedIPs holds the addresses resolved for a single Proxmox resource.
// Either field may be empty when the guest has no usable address of that family.
type ResolvedIPs struct {
	// V4 is the selected routable IPv4 address, or "" if none was found.
	V4 string

	// V6 is the selected routable IPv6 address, or "" if none was found.
	V6 string
}

// IsEmpty reports whether no address of any family was resolved.
func (r ResolvedIPs) IsEmpty() bool {
	return r.V4 == "" && r.V6 == ""
}

// ResolveOptions controls interface selection during IP resolution.
type ResolveOptions struct {
	// InterfacePreference names a specific guest interface to prefer.
	InterfacePreference string

	// AllowedInterfaces is a list of guest interface name prefixes to consider.
	AllowedInterfaces []string

	// WantV6 enables IPv6 resolution. When false the resolver skips the extra
	// work and the LXC interfaces fallback is only consulted for a missing V4.
	WantV6 bool
}

// ResolveIP returns the primary IPv4 address for a Proxmox resource.
//
// Deprecated: use ResolveIPs, which also returns IPv6. Retained so existing
// callers and tests keep working unchanged.
func ResolveIP(ctx context.Context, client *Client, resource ClusterResource, logger *slog.Logger) (string, error) {
	ips, err := ResolveIPs(ctx, client, resource, logger, ResolveOptions{})
	return ips.V4, err
}

// ResolveIPWithInterfacePreferences resolves the primary IPv4 address for a
// Proxmox resource using the supplied interface preference and allow-list.
//
// Deprecated: use ResolveIPs, which also returns IPv6.
func ResolveIPWithInterfacePreferences(ctx context.Context, client *Client, resource ClusterResource, logger *slog.Logger, interfacePreference string, allowedInterfaces []string) (string, error) {
	ips, err := ResolveIPs(ctx, client, resource, logger, ResolveOptions{
		InterfacePreference: interfacePreference,
		AllowedInterfaces:   allowedInterfaces,
	})
	return ips.V4, err
}

// ResolveIPs returns the routable addresses for a Proxmox resource.
//
// For LXC containers it reads the container config first (cheap, and already
// fetched historically), then falls back to the live interfaces endpoint for
// any family the config could not supply — which is how DHCP containers get
// resolved at all. For QEMU VMs it queries the qemu-guest-agent.
//
// A VM with no running guest agent yields empty addresses and a nil error,
// since that is a common and unremarkable homelab condition.
func ResolveIPs(ctx context.Context, client *Client, resource ClusterResource, logger *slog.Logger, opts ResolveOptions) (ResolvedIPs, error) {
	switch resource.Type {
	case resourceTypeLXC:
		return resolveLXCIPs(ctx, client, resource, logger, opts)
	case resourceTypeQEMU:
		return resolveVMIPs(ctx, client, resource, logger, opts)
	default:
		return ResolvedIPs{}, fmt.Errorf("proxmox: unsupported resource type %q", resource.Type)
	}
}

// resolveLXCIPs resolves container addresses from the static config, then
// consults the live interfaces endpoint for anything still missing.
func resolveLXCIPs(ctx context.Context, client *Client, resource ClusterResource, logger *slog.Logger, opts ResolveOptions) (ResolvedIPs, error) {
	cfg, err := client.GetLXCConfig(ctx, resource.Node, resource.VMID)
	if err != nil {
		return ResolvedIPs{}, fmt.Errorf("fetching config for LXC %d on %s: %w", resource.VMID, resource.Node, err)
	}

	resolved := resolveLXCFromConfig(cfg, opts)
	if !needsLiveLookup(resolved, opts) {
		return resolved, nil
	}

	// Config gave us nothing usable for at least one wanted family. This is
	// the normal case for DHCP containers, whose config records "ip=dhcp".
	ifaces, err := client.GetLXCInterfaces(ctx, resource.Node, resource.VMID)
	if err != nil {
		// The interfaces endpoint is missing on older PVE releases and absent
		// from the API index on some versions. Treat any failure as "no extra
		// information" rather than failing the whole resource — the statically
		// configured address, if there was one, is still good.
		logger.Debug("lxc interfaces lookup failed; using config-derived addresses only",
			slog.String("node", resource.Node),
			slog.Int("vmid", resource.VMID),
			slog.String("name", resource.Name),
			slog.String("error", err.Error()),
		)
		return resolved, nil
	}

	live := resolveLXCFromInterfaces(ifaces, opts)
	if resolved.V4 == "" {
		resolved.V4 = live.V4
	}
	if resolved.V6 == "" {
		resolved.V6 = live.V6
	}
	return resolved, nil
}

// needsLiveLookup reports whether the interfaces endpoint should be consulted
// to fill in addresses the container config did not provide.
func needsLiveLookup(resolved ResolvedIPs, opts ResolveOptions) bool {
	if resolved.V4 == "" {
		return true
	}
	return opts.WantV6 && resolved.V6 == ""
}

// resolveLXCFromConfig extracts statically configured addresses from every
// netN entry, honoring the interface preference and allow-list.
func resolveLXCFromConfig(cfg *LXCConfig, opts ResolveOptions) ResolvedIPs {
	type candidate struct {
		name string
		v4   string
		v6   string
	}

	var candidates []candidate
	for _, key := range cfg.SortedNetKeys() {
		name, v4, v6 := parseLXCNetEntry(cfg.Nets[key])
		if name == "" {
			// Fall back to the config key so an unnamed interface can still be
			// matched positionally by an allow-list entry such as "net".
			name = key
		}
		candidates = append(candidates, candidate{name: name, v4: v4, v6: v6})
	}

	names := make([]string, 0, len(candidates))
	for _, c := range candidates {
		names = append(names, c.name)
	}

	var out ResolvedIPs
	for _, idx := range interfaceOrder(names, opts) {
		c := candidates[idx]
		if out.V4 == "" && !isNonRoutableIP(c.v4) {
			out.V4 = c.v4
		}
		if opts.WantV6 && out.V6 == "" && !isNonRoutableIP(c.v6) {
			out.V6 = c.v6
		}
	}
	return out
}

// resolveLXCFromInterfaces picks addresses out of the live interfaces payload.
func resolveLXCFromInterfaces(ifaces []LXCInterface, opts ResolveOptions) ResolvedIPs {
	names := make([]string, 0, len(ifaces))
	for _, iface := range ifaces {
		names = append(names, iface.Name)
	}

	var out ResolvedIPs
	for _, idx := range interfaceOrder(names, opts) {
		iface := ifaces[idx]
		if isLoopback(iface.Name) {
			continue
		}
		for _, addr := range addressesOf(iface) {
			assignAddress(&out, addr, opts.WantV6)
		}
		if out.V4 != "" && (!opts.WantV6 || out.V6 != "") {
			break
		}
	}
	return out
}

// addressesOf flattens an LXC interface into bare address strings, preferring
// the modern ip-addresses array and falling back to the legacy inet/inet6
// fields that older PVE releases emit on their own.
func addressesOf(iface LXCInterface) []string {
	if len(iface.IPAddresses) > 0 {
		out := make([]string, 0, len(iface.IPAddresses))
		for _, addr := range iface.IPAddresses {
			out = append(out, addr.IPAddress)
		}
		return out
	}

	var out []string
	for _, legacy := range []string{iface.Inet, iface.Inet6} {
		if legacy == "" {
			continue
		}
		// Legacy fields carry a prefix length ("10.0.0.5/24").
		addr, _, _ := strings.Cut(legacy, "/")
		out = append(out, addr)
	}
	return out
}

// resolveVMIPs queries the qemu-guest-agent for the VM's network interfaces.
func resolveVMIPs(ctx context.Context, client *Client, resource ClusterResource, logger *slog.Logger, opts ResolveOptions) (ResolvedIPs, error) {
	ifaces, err := client.GetVMAgentNetworks(ctx, resource.Node, resource.VMID)
	if err != nil {
		var agentErr *ErrAgentNotRunning
		if errors.As(err, &agentErr) {
			logger.Warn("qemu-guest-agent not running; skipping IP resolution",
				slog.String("node", resource.Node),
				slog.Int("vmid", resource.VMID),
				slog.String("name", resource.Name),
			)
			return ResolvedIPs{}, nil
		}
		return ResolvedIPs{}, fmt.Errorf("querying guest agent for VM %d on %s: %w", resource.VMID, resource.Node, err)
	}

	names := make([]string, 0, len(ifaces))
	for _, iface := range ifaces {
		names = append(names, iface.Name)
	}

	var out ResolvedIPs
	for _, idx := range interfaceOrder(names, opts) {
		iface := ifaces[idx]
		if isLoopback(iface.Name) {
			continue
		}
		for _, addr := range iface.IPAddresses {
			assignAddress(&out, addr.IPAddress, opts.WantV6)
		}
		if out.V4 != "" && (!opts.WantV6 || out.V6 != "") {
			break
		}
	}
	return out, nil
}

// assignAddress files a single address into the first empty slot of its family.
//
// The address family is determined by parsing the address rather than by
// trusting the API's family label: the QEMU guest agent reports "ipv4"/"ipv6",
// while the LXC interfaces endpoint passes through the kernel's "inet"/"inet6"
// from `ip --json a`. Parsing is correct for both and for anything either API
// starts emitting later.
func assignAddress(out *ResolvedIPs, addr string, wantV6 bool) {
	if isNonRoutableIP(addr) {
		return
	}
	parsed := net.ParseIP(addr)
	if parsed == nil {
		return
	}
	if parsed.To4() != nil {
		if out.V4 == "" {
			out.V4 = addr
		}
		return
	}
	if wantV6 && out.V6 == "" {
		out.V6 = addr
	}
}

// interfaceOrder returns indices into names in the order they should be
// considered: the preferred interface first, then allow-listed interfaces in
// allow-list order, then everything else as a fallback.
//
// Every interface stays in the list so a guest whose allow-listed NIC has no
// usable address still resolves, matching the documented fallback behavior.
func interfaceOrder(names []string, opts ResolveOptions) []int {
	order := make([]int, 0, len(names))
	seen := make(map[int]bool, len(names))

	add := func(i int) {
		if !seen[i] {
			seen[i] = true
			order = append(order, i)
		}
	}

	if opts.InterfacePreference != "" {
		for i, name := range names {
			if name == opts.InterfacePreference {
				add(i)
			}
		}
	}

	// Interface order wins over allow-list order, matching the documented
	// behavior: "the first interface whose name matches one of the prefixes".
	for i, name := range names {
		for _, allowed := range opts.AllowedInterfaces {
			if strings.HasPrefix(name, allowed) {
				add(i)
				break
			}
		}
	}

	for i := range names {
		add(i)
	}
	return order
}

// parseLXCNetEntry parses a single PVE LXC netN config value, returning the
// guest interface name and any statically configured IPv4/IPv6 addresses.
//
// Example input:
//
//	"name=eth0,bridge=vmbr0,hwaddr=AA:BB:CC:DD:EE:FF,ip=192.0.2.50/24,ip6=auto"
//
// "dhcp", "auto", "manual", and empty values yield no address, since those
// mean the real address only exists inside the running container.
func parseLXCNetEntry(entry string) (name, v4, v6 string) {
	for _, part := range strings.Split(entry, ",") {
		key, value, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		switch key {
		case netKeyName:
			name = value
		case netKeyIP:
			v4 = staticAddress(value)
		case netKeyIP6:
			v6 = staticAddress(value)
		}
	}
	return name, v4, v6
}

// staticAddress strips the CIDR prefix from a PVE address value, returning ""
// for the dynamic keywords that carry no address.
func staticAddress(value string) string {
	switch value {
	case "", "dhcp", "auto", "manual":
		return ""
	}
	addr, _, _ := strings.Cut(value, "/")
	return addr
}

func parseInterfacePreferenceFromTags(tags string, prefix string) string {
	if prefix == "" {
		return ""
	}
	for _, tag := range strings.Split(tags, ";") {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if !strings.HasPrefix(tag, prefix+"+") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(tag, prefix+"+"))
	}
	return ""
}

// isLoopback returns true for known loopback interface names.
func isLoopback(name string) bool {
	return name == "lo" || name == "lo0"
}

// nonRoutableRanges contains CIDR blocks that are reserved or not suitable
// for use as DNS record targets. These supplement the ranges already handled by
// net.IP's built-in methods (IsLoopback, IsLinkLocalUnicast, IsUnspecified, IsMulticast).
var nonRoutableRanges = func() []*net.IPNet {
	cidrs := []string{
		// 100.64.0.0/10 (RFC 6598 CGNAT) is intentionally NOT filtered:
		// Tailscale assigns addresses from this range, and DNS records pointing
		// to a VM's Tailscale IP are a common and legitimate homelab use case.
		"192.0.0.0/24",       // IETF Protocol Assignments (RFC 6890)
		"192.0.2.0/24",       // TEST-NET-1 (RFC 5737)
		"198.18.0.0/15",      // Network interconnect benchmarking (RFC 2544)
		"198.51.100.0/24",    // TEST-NET-2 (RFC 5737)
		"203.0.113.0/24",     // TEST-NET-3 (RFC 5737)
		"240.0.0.0/4",        // Reserved for future use (RFC 1112)
		"255.255.255.255/32", // Limited broadcast

		// IPv6 equivalents. Unique local addresses (fc00::/7) are intentionally
		// NOT filtered, for the same reason RFC 1918 space is kept: they are
		// what homelab guests actually use.
		"2001:db8::/32", // Documentation (RFC 3849)
		"100::/64",      // Discard-only prefix (RFC 6666)
		"2001:20::/28",  // ORCHIDv2 (RFC 7343)
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
// It rejects addresses that are non-routable or not suitable for external resolution:
//   - Loopback (127.0.0.0/8, ::1)
//   - Link-local / APIPA (169.254.0.0/16, fe80::/10)
//   - Unspecified (0.0.0.0, ::)
//   - Multicast (224.0.0.0/4, ff00::/8)
//   - Reserved, documentation, and benchmarking ranges (see nonRoutableRanges)
//   - Any address that cannot be parsed
//
// RFC 1918 private addresses (10/8, 172.16/12, 192.168/16) and IPv6 unique
// local addresses (fc00::/7) are intentionally kept as valid targets — these
// are the addresses most homelab guests use.
//
// The CGNAT range (100.64.0.0/10) is also kept as a valid target because
// Tailscale assigns addresses from that range, and DNS records pointing to
// a VM's Tailscale IP are a common and legitimate homelab use case.
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
