package opnsense

import "testing"

func TestOwnershipDescription(t *testing.T) {
	got := ownershipDescription("opns-fw")
	want := "dnsweaver:opns-fw"
	if got != want {
		t.Errorf("ownershipDescription = %q, want %q", got, want)
	}
}

func TestIsOwnedBy(t *testing.T) {
	cases := map[string]bool{
		"dnsweaver:foo":              true,
		"  dnsweaver:foo":            true,
		"dnsweaver:foo | edited":     true,
		"manual entry":               false,
		"":                           false,
		"legacy dnsweaver placement": false,
	}
	for input, want := range cases {
		if got := isOwnedBy(input); got != want {
			t.Errorf("isOwnedBy(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestOwnedByInstance(t *testing.T) {
	cases := []struct {
		desc     string
		instance string
		want     bool
	}{
		{"dnsweaver:opns", "opns", true},
		{"dnsweaver:opns | manual note", "opns", true},
		{"dnsweaver:opns\ncomment", "opns", true},
		{"dnsweaver:opns\tcomment", "opns", true},
		{"dnsweaver:opns-other", "opns", false},
		{"dnsweaver:other", "opns", false},
		{"manual", "opns", false},
		{"", "opns", false},
	}
	for _, tc := range cases {
		if got := ownedByInstance(tc.desc, tc.instance); got != tc.want {
			t.Errorf("ownedByInstance(%q, %q) = %v, want %v", tc.desc, tc.instance, got, tc.want)
		}
	}
}
