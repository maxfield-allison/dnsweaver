package dnsupdate

import (
	"testing"

	"github.com/miekg/dns"
)

func TestRecordToRR(t *testing.T) {
	tests := []struct {
		name    string
		record  Record
		wantErr bool
		check   func(dns.RR) bool
	}{
		{
			name: "A record",
			record: Record{
				Name:  "host.example.com",
				Type:  dns.TypeA,
				TTL:   300,
				RData: "192.168.1.100",
			},
			wantErr: false,
			check: func(rr dns.RR) bool {
				a, ok := rr.(*dns.A)
				return ok && a.A.String() == "192.168.1.100"
			},
		},
		{
			name: "A record with FQDN",
			record: Record{
				Name:  "host.example.com.",
				Type:  dns.TypeA,
				TTL:   300,
				RData: "192.168.1.100",
			},
			wantErr: false,
			check: func(rr dns.RR) bool {
				return rr.Header().Name == "host.example.com."
			},
		},
		{
			name: "A record invalid IP",
			record: Record{
				Name:  "host.example.com",
				Type:  dns.TypeA,
				TTL:   300,
				RData: "not-an-ip",
			},
			wantErr: true,
		},
		{
			name: "A record with IPv6 (invalid)",
			record: Record{
				Name:  "host.example.com",
				Type:  dns.TypeA,
				TTL:   300,
				RData: "2001:db8::1",
			},
			wantErr: true,
		},
		{
			name: "AAAA record",
			record: Record{
				Name:  "host.example.com",
				Type:  dns.TypeAAAA,
				TTL:   300,
				RData: "2001:db8::1",
			},
			wantErr: false,
			check: func(rr dns.RR) bool {
				aaaa, ok := rr.(*dns.AAAA)
				return ok && aaaa.AAAA.String() == "2001:db8::1"
			},
		},
		{
			name: "CNAME record",
			record: Record{
				Name:  "alias.example.com",
				Type:  dns.TypeCNAME,
				TTL:   300,
				RData: "target.example.com",
			},
			wantErr: false,
			check: func(rr dns.RR) bool {
				cname, ok := rr.(*dns.CNAME)
				return ok && cname.Target == "target.example.com."
			},
		},
		{
			name: "TXT record",
			record: Record{
				Name:  "example.com",
				Type:  dns.TypeTXT,
				TTL:   300,
				RData: "v=spf1 include:_spf.example.com ~all",
			},
			wantErr: false,
			check: func(rr dns.RR) bool {
				txt, ok := rr.(*dns.TXT)
				return ok && len(txt.Txt) > 0
			},
		},
		{
			name: "MX record",
			record: Record{
				Name:     "example.com",
				Type:     dns.TypeMX,
				TTL:      300,
				RData:    "mail.example.com",
				Priority: 10,
			},
			wantErr: false,
			check: func(rr dns.RR) bool {
				mx, ok := rr.(*dns.MX)
				return ok && mx.Preference == 10 && mx.Mx == "mail.example.com."
			},
		},
		{
			name: "SRV record",
			record: Record{
				Name:     "_http._tcp.example.com",
				Type:     dns.TypeSRV,
				TTL:      300,
				RData:    "server.example.com",
				Priority: 10,
				Weight:   20,
				Port:     80,
			},
			wantErr: false,
			check: func(rr dns.RR) bool {
				srv, ok := rr.(*dns.SRV)
				return ok && srv.Priority == 10 && srv.Weight == 20 && srv.Port == 80
			},
		},
		{
			name: "PTR record",
			record: Record{
				Name:  "100.1.168.192.in-addr.arpa",
				Type:  dns.TypePTR,
				TTL:   300,
				RData: "host.example.com",
			},
			wantErr: false,
			check: func(rr dns.RR) bool {
				ptr, ok := rr.(*dns.PTR)
				return ok && ptr.Ptr == "host.example.com."
			},
		},
		{
			name: "NS record",
			record: Record{
				Name:  "example.com",
				Type:  dns.TypeNS,
				TTL:   300,
				RData: "ns1.example.com",
			},
			wantErr: false,
			check: func(rr dns.RR) bool {
				ns, ok := rr.(*dns.NS)
				return ok && ns.Ns == "ns1.example.com."
			},
		},
		{
			name: "CAA record",
			record: Record{
				Name:  "example.com",
				Type:  dns.TypeCAA,
				TTL:   300,
				RData: "0 issue letsencrypt.org",
			},
			wantErr: false,
			check: func(rr dns.RR) bool {
				caa, ok := rr.(*dns.CAA)
				return ok && caa.Flag == 0 && caa.Tag == "issue" && caa.Value == "letsencrypt.org"
			},
		},
		{
			name: "CAA record invalid format",
			record: Record{
				Name:  "example.com",
				Type:  dns.TypeCAA,
				TTL:   300,
				RData: "invalid",
			},
			wantErr: true,
		},
		{
			name: "unsupported type",
			record: Record{
				Name:  "example.com",
				Type:  dns.TypeSOA, // SOA not supported for updates
				TTL:   300,
				RData: "data",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr, err := tt.record.ToRR()
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
			if tt.check != nil && !tt.check(rr) {
				t.Errorf("check failed for record: %v", rr)
			}
		})
	}
}

func TestRecordFromRR(t *testing.T) {
	tests := []struct {
		name    string
		rr      dns.RR
		wantErr bool
		check   func(Record) bool
	}{
		{
			name: "A record",
			rr: &dns.A{
				Hdr: dns.RR_Header{Name: "host.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
				A:   []byte{192, 168, 1, 100},
			},
			wantErr: false,
			check: func(r Record) bool {
				return r.Type == dns.TypeA && r.RData == "192.168.1.100"
			},
		},
		{
			name: "CNAME record",
			rr: &dns.CNAME{
				Hdr:    dns.RR_Header{Name: "alias.example.com.", Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300},
				Target: "target.example.com.",
			},
			wantErr: false,
			check: func(r Record) bool {
				return r.Type == dns.TypeCNAME && r.RData == "target.example.com."
			},
		},
		{
			name: "MX record",
			rr: &dns.MX{
				Hdr:        dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeMX, Class: dns.ClassINET, Ttl: 300},
				Preference: 10,
				Mx:         "mail.example.com.",
			},
			wantErr: false,
			check: func(r Record) bool {
				return r.Type == dns.TypeMX && r.Priority == 10
			},
		},
		{
			name: "SRV record",
			rr: &dns.SRV{
				Hdr:      dns.RR_Header{Name: "_http._tcp.example.com.", Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: 300},
				Priority: 10,
				Weight:   20,
				Port:     80,
				Target:   "server.example.com.",
			},
			wantErr: false,
			check: func(r Record) bool {
				return r.Type == dns.TypeSRV && r.Priority == 10 && r.Weight == 20 && r.Port == 80
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record, err := RecordFromRR(tt.rr)
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
			if tt.check != nil && !tt.check(record) {
				t.Errorf("check failed for record: %+v", record)
			}
		})
	}
}

func TestRecordTypeString(t *testing.T) {
	tests := []struct {
		recordType uint16
		want       string
	}{
		{dns.TypeA, "A"},
		{dns.TypeAAAA, "AAAA"},
		{dns.TypeCNAME, "CNAME"},
		{dns.TypeTXT, "TXT"},
		{dns.TypeMX, "MX"},
		{dns.TypeSRV, "SRV"},
		{65534, "TYPE65534"}, // Unknown type
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			r := Record{Type: tt.recordType}
			if got := r.TypeString(); got != tt.want {
				t.Errorf("TypeString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewRecordHelpers(t *testing.T) {
	t.Run("NewARecord", func(t *testing.T) {
		r := NewARecord("host.example.com", "192.168.1.100", 300)
		if r.Type != dns.TypeA || r.RData != "192.168.1.100" {
			t.Errorf("NewARecord created incorrect record: %+v", r)
		}
	})

	t.Run("NewAAAARecord", func(t *testing.T) {
		r := NewAAAARecord("host.example.com", "2001:db8::1", 300)
		if r.Type != dns.TypeAAAA || r.RData != "2001:db8::1" {
			t.Errorf("NewAAAARecord created incorrect record: %+v", r)
		}
	})

	t.Run("NewCNAMERecord", func(t *testing.T) {
		r := NewCNAMERecord("alias.example.com", "target.example.com", 300)
		if r.Type != dns.TypeCNAME || r.RData != "target.example.com" {
			t.Errorf("NewCNAMERecord created incorrect record: %+v", r)
		}
	})

	t.Run("NewTXTRecord", func(t *testing.T) {
		r := NewTXTRecord("example.com", "v=spf1", 300)
		if r.Type != dns.TypeTXT || r.RData != "v=spf1" {
			t.Errorf("NewTXTRecord created incorrect record: %+v", r)
		}
	})

	t.Run("NewMXRecord", func(t *testing.T) {
		r := NewMXRecord("example.com", "mail.example.com", 10, 300)
		if r.Type != dns.TypeMX || r.Priority != 10 {
			t.Errorf("NewMXRecord created incorrect record: %+v", r)
		}
	})

	t.Run("NewSRVRecord", func(t *testing.T) {
		r := NewSRVRecord("_http._tcp.example.com", "server.example.com", 10, 20, 80, 300)
		if r.Type != dns.TypeSRV || r.Priority != 10 || r.Weight != 20 || r.Port != 80 {
			t.Errorf("NewSRVRecord created incorrect record: %+v", r)
		}
	})

	t.Run("NewPTRRecord", func(t *testing.T) {
		r := NewPTRRecord("100.1.168.192.in-addr.arpa", "host.example.com", 300)
		if r.Type != dns.TypePTR || r.RData != "host.example.com" {
			t.Errorf("NewPTRRecord created incorrect record: %+v", r)
		}
	})

	t.Run("NewNSRecord", func(t *testing.T) {
		r := NewNSRecord("example.com", "ns1.example.com", 300)
		if r.Type != dns.TypeNS || r.RData != "ns1.example.com" {
			t.Errorf("NewNSRecord created incorrect record: %+v", r)
		}
	})
}

func TestStringToType(t *testing.T) {
	tests := []struct {
		input   string
		want    uint16
		wantErr bool
	}{
		{"A", dns.TypeA, false},
		{"a", dns.TypeA, false}, // case insensitive
		{"AAAA", dns.TypeAAAA, false},
		{"CNAME", dns.TypeCNAME, false},
		{"TXT", dns.TypeTXT, false},
		{"MX", dns.TypeMX, false},
		{"SRV", dns.TypeSRV, false},
		{"  A  ", dns.TypeA, false}, // with whitespace
		{"INVALID", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := StringToType(tt.input)
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
			if got != tt.want {
				t.Errorf("StringToType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSupportedTypes(t *testing.T) {
	types := SupportedTypes()
	if len(types) < 9 {
		t.Errorf("expected at least 9 supported types, got %d", len(types))
	}

	// Check that common types are included
	typeMap := make(map[uint16]bool)
	for _, typ := range types {
		typeMap[typ] = true
	}

	required := []uint16{dns.TypeA, dns.TypeAAAA, dns.TypeCNAME, dns.TypeTXT, dns.TypeMX, dns.TypeSRV}
	for _, typ := range required {
		if !typeMap[typ] {
			t.Errorf("expected %s to be in SupportedTypes()", dns.TypeToString[typ])
		}
	}
}

func TestIsTypeSupported(t *testing.T) {
	tests := []struct {
		recordType uint16
		want       bool
	}{
		{dns.TypeA, true},
		{dns.TypeAAAA, true},
		{dns.TypeCNAME, true},
		{dns.TypeSOA, false}, // SOA is not supported
		{dns.TypeAXFR, false},
	}

	for _, tt := range tests {
		name := dns.TypeToString[tt.recordType]
		t.Run(name, func(t *testing.T) {
			if got := IsTypeSupported(tt.recordType); got != tt.want {
				t.Errorf("IsTypeSupported(%s) = %v, want %v", name, got, tt.want)
			}
		})
	}
}
