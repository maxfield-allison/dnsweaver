package proxmox

import "testing"

func TestParseLXCNet0IP(t *testing.T) {
	tests := []struct {
		name   string
		net0   string
		wantIP string
	}{
		{
			name:   "static IP with CIDR",
			net0:   "name=eth0,bridge=vmbr0,hwaddr=AA:BB:CC:DD:EE:FF,ip=192.0.2.50/24,ip6=auto",
			wantIP: "192.0.2.50",
		},
		{
			name:   "DHCP returns empty",
			net0:   "name=eth0,bridge=vmbr0,ip=dhcp",
			wantIP: "",
		},
		{
			name:   "no ip field returns empty",
			net0:   "name=eth0,bridge=vmbr0",
			wantIP: "",
		},
		{
			name:   "IP without CIDR",
			net0:   "name=eth0,bridge=vmbr0,ip=192.168.1.100",
			wantIP: "192.168.1.100",
		},
		{
			name:   "empty net0",
			net0:   "",
			wantIP: "",
		},
		{
			name:   "multiple network params, ip first",
			net0:   "ip=172.16.0.5/16,name=eth0,bridge=vmbr1",
			wantIP: "172.16.0.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLXCNet0IP(tt.net0)
			if got != tt.wantIP {
				t.Errorf("parseLXCNet0IP(%q) = %q, want %q", tt.net0, got, tt.wantIP)
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

func TestSelectInterfaceForIP(t *testing.T) {
	ifaces := []AgentNetworkInterface{
		{Name: "lo", IPAddresses: []AgentIPAddress{{IPAddressType: "ipv4", IPAddress: "127.0.0.1"}}},
		{Name: "eth0", IPAddresses: []AgentIPAddress{{IPAddressType: "ipv4", IPAddress: "10.0.0.10"}}},
		{Name: "docker0", IPAddresses: []AgentIPAddress{{IPAddressType: "ipv4", IPAddress: "172.17.0.1"}}},
	}

	if got := selectInterfaceForIP(ifaces, "", []string{"eth0", "docker0"}); got != "eth0" {
		t.Fatalf("selectInterfaceForIP() = %q, want %q", got, "eth0")
	}
	if got := selectInterfaceForIP(ifaces, "docker0", nil); got != "docker0" {
		t.Fatalf("selectInterfaceForIP() = %q, want %q", got, "docker0")
	}
	if got := selectInterfaceForIP(ifaces, "eth1", nil); got != "eth1" {
		t.Fatalf("selectInterfaceForIP() = %q, want %q", got, "eth1")
	}
	if got := selectInterfaceForIP(ifaces, "", []string{"eth"}); got != "eth0" {
		t.Fatalf("selectInterfaceForIP() = %q, want %q", got, "eth0")
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
