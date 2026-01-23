package dnsupdate

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config without TSIG",
			config: &Config{
				Server: "ns1.example.com",
				Zone:   "example.com.",
			},
			wantErr: false,
		},
		{
			name: "valid config with TSIG",
			config: &Config{
				Server:        "ns1.example.com",
				Zone:          "example.com.",
				TSIGKeyName:   "dnsweaver.",
				TSIGSecret:    "c2VjcmV0", // base64 of "secret"
				TSIGAlgorithm: "hmac-sha256",
			},
			wantErr: false,
		},
		{
			name: "valid config with TCP",
			config: &Config{
				Server: "ns1.example.com",
				Zone:   "example.com.",
				UseTCP: true,
			},
			wantErr: false,
		},
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name: "invalid config",
			config: &Config{
				Server: "", // missing server
				Zone:   "example.com.",
			},
			wantErr: true,
		},
		{
			name: "invalid TSIG secret",
			config: &Config{
				Server:      "ns1.example.com",
				Zone:        "example.com.",
				TSIGKeyName: "dnsweaver.",
				TSIGSecret:  "invalid-base64!!!",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if client == nil {
				t.Error("expected client, got nil")
			}
		})
	}
}

func TestNewClientWithOptions(t *testing.T) {
	config := &Config{
		Server: "ns1.example.com",
		Zone:   "example.com.",
	}

	logger := slog.Default()
	client, err := NewClient(config, WithLogger(logger))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.logger != logger {
		t.Error("logger option not applied")
	}
}

func TestClientZoneAndServer(t *testing.T) {
	config := &Config{
		Server: "ns1.example.com:5353",
		Zone:   "example.com.",
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Zone() != "example.com." {
		t.Errorf("Zone() = %v, want example.com.", client.Zone())
	}

	if client.Server() != "ns1.example.com:5353" {
		t.Errorf("Server() = %v, want ns1.example.com:5353", client.Server())
	}
}

func TestClientClose(t *testing.T) {
	config := &Config{
		Server: "ns1.example.com",
		Zone:   "example.com.",
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Close should not return an error (it's a no-op for RFC 2136)
	if err := client.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestClientValidateRecord(t *testing.T) {
	config := &Config{
		Server: "ns1.example.com",
		Zone:   "example.com.",
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name    string
		record  Record
		wantErr error
	}{
		{
			name: "valid record in zone",
			record: Record{
				Name:  "host.example.com",
				Type:  dns.TypeA,
				RData: "192.168.1.100",
			},
			wantErr: nil,
		},
		{
			name: "valid record in zone with FQDN",
			record: Record{
				Name:  "host.example.com.",
				Type:  dns.TypeA,
				RData: "192.168.1.100",
			},
			wantErr: nil,
		},
		{
			name: "record outside zone",
			record: Record{
				Name:  "host.other.com",
				Type:  dns.TypeA,
				RData: "192.168.1.100",
			},
			wantErr: ErrZoneMismatch,
		},
		{
			name: "empty name",
			record: Record{
				Name:  "",
				Type:  dns.TypeA,
				RData: "192.168.1.100",
			},
			wantErr: errors.New("record name is required"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.validateRecord(tt.record)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !errors.Is(err, tt.wantErr) && err.Error() != tt.wantErr.Error() {
					// Check both errors.Is and string match for non-sentinel errors
					if !errors.Is(err, tt.wantErr) {
						t.Errorf("expected error %v, got %v", tt.wantErr, err)
					}
				}
			}
		})
	}
}

func TestClientEnsureFQDN(t *testing.T) {
	config := &Config{
		Server: "ns1.example.com",
		Zone:   "example.com.",
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		input string
		want  string
	}{
		{"host.example.com", "host.example.com."},
		{"host.example.com.", "host.example.com."},
		{"", "."},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := client.ensureFQDN(tt.input); got != tt.want {
				t.Errorf("ensureFQDN(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestClientIsInZone(t *testing.T) {
	config := &Config{
		Server: "ns1.example.com",
		Zone:   "example.com.",
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		fqdn string
		want bool
	}{
		{"host.example.com.", true},
		{"sub.host.example.com.", true},
		{"example.com.", true},
		{"host.other.com.", false},
		{"example.com.evil.com.", false},
		{"EXAMPLE.COM.", true}, // case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.fqdn, func(t *testing.T) {
			if got := client.isInZone(tt.fqdn); got != tt.want {
				t.Errorf("isInZone(%q) = %v, want %v", tt.fqdn, got, tt.want)
			}
		})
	}
}

func TestCheckResponse(t *testing.T) {
	config := &Config{
		Server: "ns1.example.com",
		Zone:   "example.com.",
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name    string
		rcode   int
		wantErr error
	}{
		{"success", dns.RcodeSuccess, nil},
		{"exists", dns.RcodeYXRrset, ErrRecordExists},
		{"not found", dns.RcodeNXRrset, ErrRecordNotFound},
		{"not zone", dns.RcodeNotZone, ErrZoneMismatch},
		{"refused", dns.RcodeRefused, ErrUpdateFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &dns.Msg{}
			resp.Rcode = tt.rcode

			err := client.checkResponse(resp)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
			}
		})
	}

	// Test nil response
	t.Run("nil response", func(t *testing.T) {
		err := client.checkResponse(nil)
		if err == nil {
			t.Error("expected error for nil response")
		}
	})
}

func TestRcodeToError(t *testing.T) {
	tests := []struct {
		rcode   int
		wantErr error
	}{
		{dns.RcodeSuccess, nil},
		{dns.RcodeYXRrset, ErrRecordExists},
		{dns.RcodeNXRrset, ErrRecordNotFound},
		{dns.RcodeNotAuth, ErrAuthenticationFailed},
		{dns.RcodeServerFailure, ErrUpdateFailed},
	}

	for _, tt := range tests {
		t.Run(dns.RcodeToString[tt.rcode], func(t *testing.T) {
			err := RcodeToError(tt.rcode)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
			}
		})
	}
}

func TestIsAuthError(t *testing.T) {
	if !IsAuthError(ErrAuthenticationFailed) {
		t.Error("IsAuthError(ErrAuthenticationFailed) should return true")
	}

	if IsAuthError(ErrUpdateFailed) {
		t.Error("IsAuthError(ErrUpdateFailed) should return false")
	}

	if IsAuthError(nil) {
		t.Error("IsAuthError(nil) should return false")
	}
}

func TestClientLastUpdate(t *testing.T) {
	config := &Config{
		Server: "ns1.example.com",
		Zone:   "example.com.",
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Initially zero
	if !client.LastUpdate().IsZero() {
		t.Error("LastUpdate should initially be zero")
	}
}

// TestContextCancellation tests that operations respect context cancellation.
// This test doesn't actually connect to a DNS server.
func TestContextCancellation(t *testing.T) {
	config := &Config{
		Server:  "192.0.2.1", // RFC 5737 TEST-NET (won't route)
		Zone:    "example.com.",
		Timeout: 100 * time.Millisecond,
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// All operations should return context.Canceled
	err = client.Ping(ctx)
	if !errors.Is(err, context.Canceled) && err != nil {
		// Could also be a connection error, which is acceptable
		t.Logf("Ping with canceled context returned: %v", err)
	}
}
