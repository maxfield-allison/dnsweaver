package pfsense

import "testing"

func TestOwnershipDescription(t *testing.T) {
	got := ownershipDescription("edge-fw")
	want := "dnsweaver:edge-fw"
	if got != want {
		t.Fatalf("ownershipDescription = %q, want %q", got, want)
	}
}

func TestIsOwnedBy(t *testing.T) {
	tests := []struct {
		desc string
		want bool
	}{
		{"dnsweaver:edge-fw", true},
		{"  dnsweaver:edge-fw  ", true},
		{"dnsweaver:other | note", true},
		{"managed by ops", false},
		{"", false},
		{"prefix dnsweaver:edge-fw", false},
	}
	for _, tt := range tests {
		if got := isOwnedBy(tt.desc); got != tt.want {
			t.Errorf("isOwnedBy(%q) = %v, want %v", tt.desc, got, tt.want)
		}
	}
}

func TestOwnedByInstance(t *testing.T) {
	tests := []struct {
		desc     string
		instance string
		want     bool
	}{
		{"dnsweaver:edge-fw", "edge-fw", true},
		{"dnsweaver:edge-fw | user note", "edge-fw", true},
		{"dnsweaver:edge-fw\nmore", "edge-fw", true},
		{"dnsweaver:edge-fw2", "edge-fw", false},
		{"dnsweaver:other", "edge-fw", false},
		{"unmanaged", "edge-fw", false},
	}
	for _, tt := range tests {
		if got := ownedByInstance(tt.desc, tt.instance); got != tt.want {
			t.Errorf("ownedByInstance(%q, %q) = %v, want %v", tt.desc, tt.instance, got, tt.want)
		}
	}
}

func TestHostOverrideHelpers(t *testing.T) {
	ho := hostOverride{IPs: []string{"10.0.0.1", "2001:db8::1"}}
	if !ho.containsIP("10.0.0.1") {
		t.Error("containsIP(10.0.0.1) = false, want true")
	}
	if !ho.containsIP("2001:DB8::1") {
		t.Error("containsIP is not case-insensitive for IPv6")
	}
	if ho.containsIP("10.0.0.2") {
		t.Error("containsIP(10.0.0.2) = true, want false")
	}
	got := ho.withoutIP("10.0.0.1")
	if len(got) != 1 || got[0] != "2001:db8::1" {
		t.Errorf("withoutIP = %v, want [2001:db8::1]", got)
	}
}

func TestRecordTypeForIP(t *testing.T) {
	if rt, ok := recordTypeForIP("10.0.0.1"); !ok || rt != "A" {
		t.Errorf("recordTypeForIP(10.0.0.1) = %q,%v want A,true", rt, ok)
	}
	if rt, ok := recordTypeForIP("2001:db8::1"); !ok || rt != "AAAA" {
		t.Errorf("recordTypeForIP(2001:db8::1) = %q,%v want AAAA,true", rt, ok)
	}
	if _, ok := recordTypeForIP("not-an-ip"); ok {
		t.Error("recordTypeForIP(not-an-ip) ok = true, want false")
	}
}

func TestFQDNHelpers(t *testing.T) {
	h, d, ok := splitFQDN("web.example.com")
	if !ok || h != "web" || d != "example.com" {
		t.Errorf("splitFQDN = %q,%q,%v", h, d, ok)
	}
	if _, _, ok := splitFQDN("nodot"); ok {
		t.Error("splitFQDN(nodot) ok = true, want false")
	}
	if got := joinFQDN("web", "example.com"); got != "web.example.com" {
		t.Errorf("joinFQDN = %q", got)
	}
	if !inZone("web.example.com", "example.com") {
		t.Error("inZone should match subdomain")
	}
	if inZone("web.other.com", "example.com") {
		t.Error("inZone should not match different zone")
	}
}
