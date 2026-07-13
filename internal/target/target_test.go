package target

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		mode    string
		wantNil bool
		wantErr bool
		desc    string
	}{
		{"", true, false, ""},
		{"public", false, false, "public ipv4"},
		{"PUBLIC", false, false, "public ipv4"},
		{"interface:eth0", false, false, "interface eth0 (ipv4)"},
		{"interface: eth0 ", false, false, "interface eth0 (ipv4)"},
		{"interface:", true, true, ""},
		{"bogus", true, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			r, err := Parse(tt.mode, FamilyIPv4)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) expected error", tt.mode)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.mode, err)
			}
			if tt.wantNil {
				if r != nil {
					t.Errorf("Parse(%q) = %v, want nil", tt.mode, r)
				}
				return
			}
			if r == nil {
				t.Fatalf("Parse(%q) = nil, want resolver", tt.mode)
			}
			if got := r.Describe(); got != tt.desc {
				t.Errorf("Describe() = %q, want %q", got, tt.desc)
			}
		})
	}
}

func TestPublicResolver_Fallback(t *testing.T) {
	// First endpoint errors (500), second returns junk, third returns a valid IP.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	junk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-an-ip\n"))
	}))
	defer junk.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("203.0.113.7\n"))
	}))
	defer good.Close()

	r := NewPublicResolver(FamilyIPv4, []string{bad.URL, junk.URL, good.URL})
	// httptest servers listen on 127.0.0.1; force the default transport so the
	// test doesn't depend on tcp4-only pinning against loopback.
	r.client = good.Client()

	got, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "203.0.113.7" {
		t.Errorf("Resolve() = %q, want 203.0.113.7", got)
	}
}

func TestPublicResolver_AllFail(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer bad.Close()

	r := NewPublicResolver(FamilyIPv4, []string{bad.URL})
	r.client = bad.Client()
	if _, err := r.Resolve(context.Background()); err == nil {
		t.Fatal("expected error when all endpoints fail")
	}
}

func TestPublicResolver_FamilyMismatch(t *testing.T) {
	// Endpoint returns an IPv6 address but the resolver wants IPv4.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("2001:db8::1\n"))
	}))
	defer srv.Close()

	r := NewPublicResolver(FamilyIPv4, []string{srv.URL})
	r.client = srv.Client()
	if _, err := r.Resolve(context.Background()); err == nil {
		t.Fatal("expected error on family mismatch")
	}
}

func TestInterfaceResolver(t *testing.T) {
	r := NewInterfaceResolver("eth0", FamilyIPv4)
	r.lookup = func(_ string) ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("127.0.0.1")},                            // loopback, skipped
			&net.IPNet{IP: net.ParseIP("fe80::1")},                              // link-local, skipped
			&net.IPNet{IP: net.ParseIP("2001:db8::5")},                          // wrong family, skipped
			&net.IPNet{IP: net.ParseIP("10.0.0.5"), Mask: net.CIDRMask(24, 32)}, // match
		}, nil
	}
	got, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "10.0.0.5" {
		t.Errorf("Resolve() = %q, want 10.0.0.5", got)
	}
}

func TestInterfaceResolver_IPv6(t *testing.T) {
	r := NewInterfaceResolver("eth0", FamilyIPv6)
	r.lookup = func(_ string) ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("10.0.0.5")},    // wrong family
			&net.IPNet{IP: net.ParseIP("2001:db8::5")}, // match
		}, nil
	}
	got, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "2001:db8::5" {
		t.Errorf("Resolve() = %q, want 2001:db8::5", got)
	}
}

func TestInterfaceResolver_NoMatch(t *testing.T) {
	r := NewInterfaceResolver("eth0", FamilyIPv4)
	r.lookup = func(_ string) ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	if _, err := r.Resolve(context.Background()); err == nil {
		t.Fatal("expected error when no global address of family")
	}
}
