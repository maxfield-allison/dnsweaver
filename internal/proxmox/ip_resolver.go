package proxmox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
)

const (
	resourceTypeQEMU = "qemu"

	ipAddressTypeIPv4 = "ipv4"
)

// ResolveIP returns the primary IPv4 address for a Proxmox resource.
//
// For LXC containers (type "lxc"), it reads the net0 config field and parses
// the ip= component. For QEMU VMs (type "qemu"), it queries the qemu-guest-agent
// and returns the first non-loopback IPv4 address found on any interface.
//
// If the VM has no guest agent running, the function logs a warning and returns
// an empty string (no error), since this is a common condition in homelabs
// where not all VMs run the agent.
//
// Returns an empty string if no IP address can be determined.
func ResolveIP(ctx context.Context, client *Client, resource ClusterResource, logger *slog.Logger) (string, error) {
	switch resource.Type {
	case resourceTypeLXC:
		return resolveLXCIP(ctx, client, resource)
	case resourceTypeQEMU:
		return resolveVMIP(ctx, client, resource, logger)
	default:
		return "", fmt.Errorf("proxmox: unsupported resource type %q", resource.Type)
	}
}

// resolveLXCIP reads the LXC container config and extracts the IP from the net0 field.
func resolveLXCIP(ctx context.Context, client *Client, resource ClusterResource) (string, error) {
	cfg, err := client.GetLXCConfig(ctx, resource.Node, resource.VMID)
	if err != nil {
		return "", fmt.Errorf("fetching config for LXC %d on %s: %w", resource.VMID, resource.Node, err)
	}

	if cfg.Net0 == "" {
		return "", nil
	}

	ip := parseLXCNet0IP(cfg.Net0)
	if isNonRoutableIP(ip) {
		return "", nil
	}
	return ip, nil
}

// parseLXCNet0IP parses the ip= component from a Proxmox LXC net0 config value.
// Example input: "name=eth0,bridge=vmbr0,hwaddr=AA:BB:CC:DD:EE:FF,ip=192.0.2.50/24,ip6=auto"
// Returns the IP address without the CIDR prefix, or empty string if not found.
func parseLXCNet0IP(net0 string) string {
	for _, part := range strings.Split(net0, ",") {
		key, value, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		if key != "ip" {
			continue
		}
		if value == "dhcp" || value == "" {
			// DHCP — no static IP to return; caller can skip this resource.
			return ""
		}
		// Strip CIDR prefix length (e.g. "192.0.2.50/24" → "192.0.2.50").
		ip, _, _ := strings.Cut(value, "/")
		return ip
	}
	return ""
}

// resolveVMIP queries the qemu-guest-agent for the VM's network interfaces and
// returns the first non-loopback IPv4 address found.
func resolveVMIP(ctx context.Context, client *Client, resource ClusterResource, logger *slog.Logger) (string, error) {
	ifaces, err := client.GetVMAgentNetworks(ctx, resource.Node, resource.VMID)
	if err != nil {
		var agentErr *ErrAgentNotRunning
		if errors.As(err, &agentErr) {
			logger.Warn("qemu-guest-agent not running; skipping IP resolution",
				slog.String("node", resource.Node),
				slog.Int("vmid", resource.VMID),
				slog.String("name", resource.Name),
			)
			return "", nil
		}
		return "", fmt.Errorf("querying guest agent for VM %d on %s: %w", resource.VMID, resource.Node, err)
	}

	for _, iface := range ifaces {
		if isLoopback(iface.Name) {
			continue
		}
		for _, addr := range iface.IPAddresses {
			if addr.IPAddressType == ipAddressTypeIPv4 && !isNonRoutableIP(addr.IPAddress) {
				return addr.IPAddress, nil
			}
		}
	}

	return "", nil
}

// isLoopback returns true for known loopback interface names.
func isLoopback(name string) bool {
	return name == "lo" || name == "lo0"
}

// nonRoutableRanges contains IPv4 CIDR blocks that are reserved or not suitable
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
//   - Loopback (127.0.0.0/8)
//   - Link-local / APIPA (169.254.0.0/16)
//   - Unspecified (0.0.0.0)
//   - Multicast (224.0.0.0/4)
//   - Reserved, documentation, and benchmarking ranges (see nonRoutableRanges)
//   - Any address that cannot be parsed
//
// RFC 1918 private addresses (10/8, 172.16/12, 192.168/16) are intentionally
// kept as valid targets — these are the addresses most homelab VMs use.
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
