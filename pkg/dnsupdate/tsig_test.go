package dnsupdate

import (
	"testing"

	"github.com/miekg/dns"
)

func TestNewTSIG(t *testing.T) {
	tests := []struct {
		name      string
		keyName   string
		secret    string
		algorithm string
		wantErr   bool
		wantName  string
	}{
		{
			name:      "valid TSIG with dot",
			keyName:   "dnsweaver.",
			secret:    "c2VjcmV0", // base64 of "secret"
			algorithm: "hmac-sha256",
			wantErr:   false,
			wantName:  "dnsweaver.",
		},
		{
			name:      "valid TSIG without dot (gets added)",
			keyName:   "dnsweaver",
			secret:    "c2VjcmV0",
			algorithm: "hmac-sha256",
			wantErr:   false,
			wantName:  "dnsweaver.",
		},
		{
			name:      "invalid base64 secret",
			keyName:   "dnsweaver.",
			secret:    "not-valid-base64!!!",
			algorithm: "hmac-sha256",
			wantErr:   true,
		},
		{
			name:      "unsupported algorithm",
			keyName:   "dnsweaver.",
			secret:    "c2VjcmV0",
			algorithm: "invalid-algo",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tsig, err := NewTSIG(tt.keyName, tt.secret, tt.algorithm)
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
			if tsig.Name != tt.wantName {
				t.Errorf("Name = %v, want %v", tsig.Name, tt.wantName)
			}
		})
	}
}

func TestTSIGFromConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantNil bool
		wantErr bool
	}{
		{
			name: "config with valid TSIG",
			config: &Config{
				Server:        "ns1.example.com",
				Zone:          "example.com.",
				TSIGKeyName:   "dnsweaver.",
				TSIGSecret:    "c2VjcmV0",
				TSIGAlgorithm: "hmac-sha256",
			},
			wantNil: false,
			wantErr: false,
		},
		{
			name: "config without TSIG",
			config: &Config{
				Server: "ns1.example.com",
				Zone:   "example.com.",
			},
			wantNil: true,
			wantErr: false,
		},
		{
			name: "config with invalid TSIG secret",
			config: &Config{
				Server:        "ns1.example.com",
				Zone:          "example.com.",
				TSIGKeyName:   "dnsweaver.",
				TSIGSecret:    "invalid!!!",
				TSIGAlgorithm: "hmac-sha256",
			},
			wantNil: false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tsig, err := TSIGFromConfig(tt.config)
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
			if tt.wantNil && tsig != nil {
				t.Error("expected nil TSIG, got non-nil")
			}
			if !tt.wantNil && tsig == nil {
				t.Error("expected non-nil TSIG, got nil")
			}
		})
	}
}

func TestTSIGApplyToMessage(t *testing.T) {
	tsig := &TSIG{
		Name:      "dnsweaver.",
		Secret:    "c2VjcmV0",
		Algorithm: dns.HmacSHA256,
	}

	msg := new(dns.Msg)
	msg.SetUpdate("example.com.")

	tsig.ApplyToMessage(msg)

	if len(msg.Extra) == 0 {
		t.Fatal("expected TSIG RR in Extra section")
	}

	tsigRR, ok := msg.Extra[len(msg.Extra)-1].(*dns.TSIG)
	if !ok {
		t.Fatal("expected last Extra record to be TSIG")
	}

	if tsigRR.Hdr.Name != "dnsweaver." {
		t.Errorf("TSIG name = %v, want dnsweaver.", tsigRR.Hdr.Name)
	}
}

func TestNilTSIGApply(t *testing.T) {
	// Nil TSIG should not panic
	var tsig *TSIG

	msg := new(dns.Msg)
	msg.SetUpdate("example.com.")

	// Should not panic
	tsig.ApplyToMessage(msg)

	// Extra should not have TSIG
	for _, rr := range msg.Extra {
		if _, ok := rr.(*dns.TSIG); ok {
			t.Error("expected no TSIG when tsig is nil")
		}
	}
}

func TestAlgorithmName(t *testing.T) {
	tests := []struct {
		algorithm string
		want      string
	}{
		{dns.HmacSHA256, "HMAC-SHA256"},
		{dns.HmacSHA512, "HMAC-SHA512"},
		{dns.HmacMD5, "HMAC-MD5"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.algorithm, func(t *testing.T) {
			if got := AlgorithmName(tt.algorithm); got != tt.want {
				t.Errorf("AlgorithmName(%v) = %v, want %v", tt.algorithm, got, tt.want)
			}
		})
	}
}

func TestSupportedAlgorithms(t *testing.T) {
	algs := SupportedAlgorithms()
	if len(algs) != 3 {
		t.Errorf("expected 3 supported algorithms, got %d", len(algs))
	}
}
