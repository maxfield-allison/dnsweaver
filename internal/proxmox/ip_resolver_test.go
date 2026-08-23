package proxmox

import (
	"reflect"
	"testing"
)

func TestParseLXCNetEntry(t *testing.T) {
	tests := []struct {
		name     string
		entry    string
		wantName string
		wantV4   string
		wantV6   string
	}{
		{
			name:     "static IP with CIDR",
			entry:    "name=eth0,bridge=vmbr0,hwaddr=AA:BB:CC:DD:EE:FF,ip=192.0.2.50/24,ip6=auto",
			wantName: "eth0",
			wantV4:   "192.0.2.50",
			wantV6:   "",
		},
		{
			name:     "DHCP returns empty",
			entry:    "name=eth0,bridge=vmbr0,ip=dhcp",
			wantName: "eth0",
		},
		{
			name:     "no ip field returns empty",
			entry:    "name=eth0,bridge=vmbr0",
			wantName: "eth0",
		},
		{
			name:     "IP without CIDR",
			entry:    "name=eth0,bridge=vmbr0,ip=192.168.1.100",
			wantName: "eth0",
			wantV4:   "192.168.1.100",
		},
		{
			name:  "empty entry",
			entry: "",
		},
		{
			name:     "multiple network params, ip first",
			entry:    "ip=172.16.0.5/16,name=eth0,bridge=vmbr1",
			wantName: "eth0",
			wantV4:   "172.16.0.5",
		},
		{
			name:     "static dual-stack",
			entry:    "name=eth0,bridge=vmbr0,ip=10.0.0.5/24,ip6=fd00::5/64",
			wantName: "eth0",
			wantV4:   "10.0.0.5",
			wantV6:   "fd00::5",
		},
		{
			name:     "ipv6 manual keyword yields no address",
			entry:    "name=eth0,ip=10.0.0.5/24,ip6=manual",
			wantName: "eth0",
			wantV4:   "10.0.0.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotV4, gotV6 := parseLXCNetEntry(tt.entry)
			if gotName != tt.wantName || gotV4 != tt.wantV4 || gotV6 != tt.wantV6 {
				t.Errorf("parseLXCNetEntry(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.entry, gotName, gotV4, gotV6, tt.wantName, tt.wantV4, tt.wantV6)
			}
		})
	}
}

func TestResolveLXCFromConfig_MultiNIC(t *testing.T) {
	cfg := &LXCConfig{Nets: map[string]string{
		"net0": "name=eth0,bridge=vmbr0,ip=dhcp",
		"net1": "name=eth1,bridge=vmbr1,ip=10.20.0.7/24",
	}}

	// net0 is DHCP so it contributes nothing; net1 must still be found.
	got := resolveLXCFromConfig(cfg, ResolveOptions{})
	if got.V4 != "10.20.0.7" {
		t.Errorf("V4 = %q, want %q — a second NIC must not be ignored", got.V4, "10.20.0.7")
	}
}

func TestResolveLXCFromConfig_AllowListPrefersMatchingNIC(t *testing.T) {
	cfg := &LXCConfig{Nets: map[string]string{
		"net0": "name=docker0,ip=172.17.0.1/16",
		"net1": "name=eth0,ip=10.20.0.7/24",
	}}

	got := resolveLXCFromConfig(cfg, ResolveOptions{AllowedInterfaces: []string{"eth"}})
	if got.V4 != "10.20.0.7" {
		t.Errorf("V4 = %q, want %q", got.V4, "10.20.0.7")
	}
}

func TestResolveLXCFromConfig_DualStackOnlyWhenRequested(t *testing.T) {
	cfg := &LXCConfig{Nets: map[string]string{
		"net0": "name=eth0,ip=10.0.0.5/24,ip6=fd00::5/64",
	}}

	if got := resolveLXCFromConfig(cfg, ResolveOptions{}); got.V6 != "" {
		t.Errorf("V6 = %q, want empty when WantV6 is false", got.V6)
	}
	got := resolveLXCFromConfig(cfg, ResolveOptions{WantV6: true})
	if got.V4 != "10.0.0.5" || got.V6 != "fd00::5" {
		t.Errorf("got (%q, %q), want (10.0.0.5, fd00::5)", got.V4, got.V6)
	}
}

func TestResolveLXCFromInterfaces(t *testing.T) {
	// PVE passes through the kernel's inet/inet6 family labels here, unlike the
	// QEMU agent's ipv4/ipv6. Resolution must not depend on the label.
	ifaces := []LXCInterface{
		{Name: "lo", IPAddresses: []AgentIPAddress{
			{IPAddressType: "inet", IPAddress: "127.0.0.1"},
		}},
		{Name: "eth0", IPAddresses: []AgentIPAddress{
			{IPAddressType: "inet", IPAddress: "10.30.0.42"},
			{IPAddressType: "inet6", IPAddress: "fe80::1"},
			{IPAddressType: "inet6", IPAddress: "fd00::42"},
		}},
	}

	got := resolveLXCFromInterfaces(ifaces, ResolveOptions{WantV6: true})
	if got.V4 != "10.30.0.42" {
		t.Errorf("V4 = %q, want %q", got.V4, "10.30.0.42")
	}
	if got.V6 != "fd00::42" {
		t.Errorf("V6 = %q, want %q (link-local must be skipped)", got.V6, "fd00::42")
	}
}

func TestResolveLXCFromInterfaces_LegacyFields(t *testing.T) {
	// Older PVE releases emit only inet/inet6, without the ip-addresses array.
	ifaces := []LXCInterface{
		{Name: "eth0", Inet: "10.30.0.42/24", Inet6: "fd00::42/64"},
	}

	got := resolveLXCFromInterfaces(ifaces, ResolveOptions{WantV6: true})
	if got.V4 != "10.30.0.42" || got.V6 != "fd00::42" {
		t.Errorf("got (%q, %q), want (10.30.0.42, fd00::42)", got.V4, got.V6)
	}
}

func TestNeedsLiveLookup(t *testing.T) {
	tests := []struct {
		name     string
		resolved ResolvedIPs
		opts     ResolveOptions
		want     bool
	}{
		{"no v4", ResolvedIPs{}, ResolveOptions{}, true},
		{"v4 present, v6 not wanted", ResolvedIPs{V4: "10.0.0.1"}, ResolveOptions{}, false},
		{"v4 present, v6 wanted but missing", ResolvedIPs{V4: "10.0.0.1"}, ResolveOptions{WantV6: true}, true},
		{"both present", ResolvedIPs{V4: "10.0.0.1", V6: "fd00::1"}, ResolveOptions{WantV6: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsLiveLookup(tt.resolved, tt.opts); got != tt.want {
				t.Errorf("needsLiveLookup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsLoopback(t *testing.T) {
	if !isLoopback("lo") {
		t.Error("expected lo to be loopback")
	}
	if !isLoopback("lo0") {
		t.Error("expected lo0 to be loopback")
	}
	if isLoopback("eth0") {
		t.Error("expected eth0 to not be loopback")
	}
}

func TestParseInterfacePreferenceFromTags(t *testing.T) {
	if got := parseInterfacePreferenceFromTags("dnsweaver+eth1;other:keep", "dnsweaver"); got != "eth1" {
		t.Fatalf("parseInterfacePreferenceFromTags() = %q, want %q", got, "eth1")
	}
	if got := parseInterfacePreferenceFromTags("other;dnsweaver+eth0", "dnsweaver"); got != "eth0" {
		t.Fatalf("parseInterfacePreferenceFromTags() = %q, want %q", got, "eth0")
	}
	if got := parseInterfacePreferenceFromTags("dnsweaver+", "dnsweaver"); got != "" {
		t.Fatalf("parseInterfacePreferenceFromTags() = %q, want empty", got)
	}
	if got := parseInterfacePreferenceFromTags("+eth0", ""); got != "" {
		t.Fatalf("parseInterfacePreferenceFromTags() = %q, want empty", got)
	}
}

func TestInterfaceOrder(t *testing.T) {
	names := []string{"lo", "eth0", "docker0"}

	tests := []struct {
		name string
		opts ResolveOptions
		want []int
	}{
		{
			name: "no preference keeps natural order",
			opts: ResolveOptions{},
			want: []int{0, 1, 2},
		},
		{
			name: "explicit preference goes first",
			opts: ResolveOptions{InterfacePreference: "docker0"},
			want: []int{2, 0, 1},
		},
		{
			name: "allow-list respects interface order, not allow-list order",
			opts: ResolveOptions{AllowedInterfaces: []string{"docker0", "eth0"}},
			want: []int{1, 2, 0},
		},
		{
			name: "prefix match",
			opts: ResolveOptions{AllowedInterfaces: []string{"eth"}},
			want: []int{1, 0, 2},
		},
		{
			name: "unknown preference still yields every interface as fallback",
			opts: ResolveOptions{InterfacePreference: "eth1"},
			want: []int{0, 1, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := interfaceOrder(names, tt.opts)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("interfaceOrder() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAssignAddress_ClassifiesByParsingNotLabel(t *testing.T) {
	var out ResolvedIPs
	assignAddress(&out, "10.0.0.5", true)
	assignAddress(&out, "fd00::5", true)
	if out.V4 != "10.0.0.5" || out.V6 != "fd00::5" {
		t.Fatalf("got (%q, %q), want (10.0.0.5, fd00::5)", out.V4, out.V6)
	}

	// First address of each family wins.
	assignAddress(&out, "10.0.0.9", true)
	if out.V4 != "10.0.0.5" {
		t.Errorf("V4 = %q, want the first address to be retained", out.V4)
	}

	// V6 is not recorded when it was not requested.
	var v4Only ResolvedIPs
	assignAddress(&v4Only, "fd00::5", false)
	if v4Only.V6 != "" {
		t.Errorf("V6 = %q, want empty", v4Only.V6)
	}
}

func TestIsNonRoutableIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		// Loopback (127.0.0.0/8)
		{"loopback 127.0.0.1", "127.0.0.1", true},
		{"loopback 127.1.2.3", "127.1.2.3", true},

		// Link-local / APIPA (169.254.0.0/16) — the bug this fix addresses
		{"link-local 169.254.0.1", "169.254.0.1", true},
		{"link-local 169.254.253.1", "169.254.253.1", true},
		{"link-local 169.254.255.255", "169.254.255.255", true},

		// Unspecified (0.0.0.0)
		{"unspecified 0.0.0.0", "0.0.0.0", true},

		// Multicast (224.0.0.0/4)
		{"multicast 224.0.0.1", "224.0.0.1", true},
		{"multicast 239.255.255.255", "239.255.255.255", true},

		// Documentation ranges (RFC 5737)
		{"TEST-NET-1 192.0.2.1", "192.0.2.1", true},
		{"TEST-NET-2 198.51.100.1", "198.51.100.1", true},
		{"TEST-NET-3 203.0.113.1", "203.0.113.1", true},

		// IETF Protocol Assignments (192.0.0.0/24, RFC 6890)
		{"IETF 192.0.0.1", "192.0.0.1", true},

		// Benchmarking (198.18.0.0/15, RFC 2544)
		{"benchmarking 198.18.0.1", "198.18.0.1", true},
		{"benchmarking 198.19.255.255", "198.19.255.255", true},

		// Reserved for future use (240.0.0.0/4, RFC 1112)
		{"reserved 240.0.0.1", "240.0.0.1", true},
		{"reserved 254.255.255.255", "254.255.255.255", true},

		// Limited broadcast
		{"broadcast 255.255.255.255", "255.255.255.255", true},

		// Unparseable / empty
		{"empty string", "", true},
		{"garbage", "not-an-ip", true},

		// Valid routable private addresses (RFC 1918) — must NOT be filtered
		{"RFC1918 10.0.0.1", "10.0.0.1", false},
		{"RFC1918 10.1.20.5", "10.1.20.5", false},
		{"RFC1918 172.16.0.1", "172.16.0.1", false},
		{"RFC1918 172.31.255.255", "172.31.255.255", false},
		{"RFC1918 192.168.0.1", "192.168.0.1", false},
		{"RFC1918 192.168.1.100", "192.168.1.100", false},

		// CGNAT range (100.64.0.0/10) — Tailscale uses this; must NOT be filtered
		{"tailscale 100.64.0.1", "100.64.0.1", false},
		{"tailscale 100.127.255.255", "100.127.255.255", false},
		{"tailscale 100.100.100.100", "100.100.100.100", false},

		// Valid public addresses — must NOT be filtered
		{"public 1.1.1.1", "1.1.1.1", false},
		{"public 8.8.8.8", "8.8.8.8", false},

		// IPv6 that must be filtered
		{"v6 loopback ::1", "::1", true},
		{"v6 unspecified ::", "::", true},
		{"v6 link-local fe80::1", "fe80::1", true},
		{"v6 multicast ff02::1", "ff02::1", true},
		{"v6 documentation 2001:db8::1", "2001:db8::1", true},
		{"v6 discard 100::1", "100::1", true},
		{"v6 orchid 2001:20::1", "2001:20::1", true},

		// IPv6 that must NOT be filtered — ULA is the homelab equivalent of
		// RFC 1918 and is exactly what dual-stack guests use.
		{"v6 ULA fd00::42", "fd00::42", false},
		{"v6 ULA fc00::1", "fc00::1", false},
		{"v6 GUA 2606:4700::1111", "2606:4700::1111", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNonRoutableIP(tt.ip)
			if got != tt.want {
				t.Errorf("isNonRoutableIP(%q) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}
