package rfc2136

import (
	"context"
	"log/slog"
	"testing"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/dnsupdate"
	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"

	"github.com/miekg/dns"
)

func TestProvider_Name(t *testing.T) {
	p := &Provider{name: "test-rfc2136"}
	if p.Name() != "test-rfc2136" {
		t.Errorf("Name() = %v, want %v", p.Name(), "test-rfc2136")
	}
}

func TestProvider_Type(t *testing.T) {
	p := &Provider{}
	if p.Type() != "rfc2136" {
		t.Errorf("Type() = %v, want %v", p.Type(), "rfc2136")
	}
}

func TestProvider_Zone(t *testing.T) {
	p := &Provider{zone: "example.com."}
	if p.Zone() != "example.com." {
		t.Errorf("Zone() = %v, want %v", p.Zone(), "example.com.")
	}
}

func TestProvider_Capabilities(t *testing.T) {
	p := &Provider{}
	caps := p.Capabilities()

	if !caps.SupportsOwnershipTXT {
		t.Error("Expected SupportsOwnershipTXT to be true")
	}

	if !caps.SupportsNativeUpdate {
		t.Error("Expected SupportsNativeUpdate to be true")
	}

	// Check for expected record types
	expectedTypes := []provider.RecordType{
		provider.RecordTypeA,
		provider.RecordTypeAAAA,
		provider.RecordTypeCNAME,
		provider.RecordTypeTXT,
		provider.RecordTypeSRV,
	}

	for _, rt := range expectedTypes {
		if !caps.SupportsRecordType(rt) {
			t.Errorf("Expected to support record type %s", rt)
		}
	}
}

func TestRecordTypeToUint16(t *testing.T) {
	tests := []struct {
		input    provider.RecordType
		expected uint16
	}{
		{provider.RecordTypeA, dns.TypeA},
		{provider.RecordTypeAAAA, dns.TypeAAAA},
		{provider.RecordTypeCNAME, dns.TypeCNAME},
		{provider.RecordTypeTXT, dns.TypeTXT},
		{provider.RecordTypeSRV, dns.TypeSRV},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			result := recordTypeToUint16(tt.input)
			if result != tt.expected {
				t.Errorf("recordTypeToUint16(%s) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestProvider_toRFC2136Record(t *testing.T) {
	p := &Provider{
		zone: "example.com.",
		ttl:  300,
	}

	tests := []struct {
		name     string
		record   provider.Record
		wantName string
		wantType uint16
		wantTTL  uint32
		wantErr  bool
	}{
		{
			name: "A record with relative hostname",
			record: provider.Record{
				Hostname: "app",
				Type:     provider.RecordTypeA,
				Target:   "10.0.0.1",
				TTL:      600,
			},
			wantName: "app.example.com.",
			wantType: dns.TypeA,
			wantTTL:  600,
		},
		{
			name: "A record with FQDN",
			record: provider.Record{
				Hostname: "app.example.com.",
				Type:     provider.RecordTypeA,
				Target:   "10.0.0.1",
			},
			wantName: "app.example.com.",
			wantType: dns.TypeA,
			wantTTL:  300, // Uses default
		},
		{
			name: "AAAA record",
			record: provider.Record{
				Hostname: "app.example.com",
				Type:     provider.RecordTypeAAAA,
				Target:   "2001:db8::1",
				TTL:      300,
			},
			wantName: "app.example.com.",
			wantType: dns.TypeAAAA,
			wantTTL:  300,
		},
		{
			name: "CNAME record",
			record: provider.Record{
				Hostname: "www.example.com",
				Type:     provider.RecordTypeCNAME,
				Target:   "app.example.com",
				TTL:      300,
			},
			wantName: "www.example.com.",
			wantType: dns.TypeCNAME,
			wantTTL:  300,
		},
		{
			name: "TXT record",
			record: provider.Record{
				Hostname: "_dnsweaver.app.example.com",
				Type:     provider.RecordTypeTXT,
				Target:   "heritage=dnsweaver",
				TTL:      300,
			},
			wantName: "_dnsweaver.app.example.com.",
			wantType: dns.TypeTXT,
			wantTTL:  300,
		},
		{
			name: "SRV record",
			record: provider.Record{
				Hostname: "_http._tcp.example.com",
				Type:     provider.RecordTypeSRV,
				Target:   "app.example.com",
				TTL:      300,
				SRV: &provider.SRVData{
					Priority: 10,
					Weight:   20,
					Port:     8080,
				},
			},
			wantName: "_http._tcp.example.com.",
			wantType: dns.TypeSRV,
			wantTTL:  300,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.toRFC2136Record(tt.record)
			if (err != nil) != tt.wantErr {
				t.Errorf("toRFC2136Record() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if result.Name != tt.wantName {
				t.Errorf("Name = %v, want %v", result.Name, tt.wantName)
			}
			if result.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", result.Type, tt.wantType)
			}
			if result.TTL != tt.wantTTL {
				t.Errorf("TTL = %v, want %v", result.TTL, tt.wantTTL)
			}

			// Check SRV fields
			if tt.record.Type == provider.RecordTypeSRV && tt.record.SRV != nil {
				if result.Priority != tt.record.SRV.Priority {
					t.Errorf("Priority = %v, want %v", result.Priority, tt.record.SRV.Priority)
				}
				if result.Weight != tt.record.SRV.Weight {
					t.Errorf("Weight = %v, want %v", result.Weight, tt.record.SRV.Weight)
				}
				if result.Port != tt.record.SRV.Port {
					t.Errorf("Port = %v, want %v", result.Port, tt.record.SRV.Port)
				}
			}
		})
	}
}

func TestProvider_List(t *testing.T) {
	p := &Provider{
		zone:   "example.com.",
		logger: slog.Default(),
	}

	// List should return empty slice when client is nil (no real DNS server)
	records, err := p.List(context.Background())
	if err != nil {
		t.Errorf("List() error = %v", err)
	}
	if len(records) != 0 {
		t.Errorf("List() returned %d records, want 0", len(records))
	}
}

func TestUint16ToRecordType(t *testing.T) {
	tests := []struct {
		name    string
		dnsType uint16
		want    provider.RecordType
		wantOK  bool
	}{
		{"A", dns.TypeA, provider.RecordTypeA, true},
		{"AAAA", dns.TypeAAAA, provider.RecordTypeAAAA, true},
		{"CNAME", dns.TypeCNAME, provider.RecordTypeCNAME, true},
		{"TXT", dns.TypeTXT, provider.RecordTypeTXT, true},
		{"SRV", dns.TypeSRV, provider.RecordTypeSRV, true},
		{"MX", dns.TypeMX, "", false},
		{"NS", dns.TypeNS, "", false},
		{"SOA", dns.TypeSOA, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := uint16ToRecordType(tt.dnsType)
			if ok != tt.wantOK {
				t.Errorf("uint16ToRecordType() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("uint16ToRecordType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProvider_fromRFC2136Record(t *testing.T) {
	p := &Provider{
		zone: "example.com.",
		ttl:  300,
	}

	tests := []struct {
		name    string
		record  dnsupdate.Record
		want    provider.Record
		wantErr bool
	}{
		{
			name: "A record",
			record: dnsupdate.Record{
				Name:  "test.example.com.",
				Type:  dns.TypeA,
				TTL:   300,
				RData: "192.168.1.1",
			},
			want: provider.Record{
				Hostname:   "test.example.com",
				Type:       provider.RecordTypeA,
				Target:     "192.168.1.1",
				TTL:        300,
				ProviderID: "test.example.com.:A:192.168.1.1",
			},
			wantErr: false,
		},
		{
			name: "CNAME record with trailing dot",
			record: dnsupdate.Record{
				Name:  "alias.example.com.",
				Type:  dns.TypeCNAME,
				TTL:   600,
				RData: "target.example.com.",
			},
			want: provider.Record{
				Hostname:   "alias.example.com",
				Type:       provider.RecordTypeCNAME,
				Target:     "target.example.com",
				TTL:        600,
				ProviderID: "alias.example.com.:CNAME:target.example.com.",
			},
			wantErr: false,
		},
		{
			name: "TXT ownership record",
			record: dnsupdate.Record{
				Name:  "_dnsweaver.test.example.com.",
				Type:  dns.TypeTXT,
				TTL:   300,
				RData: "heritage=dnsweaver",
			},
			want: provider.Record{
				Hostname:   "_dnsweaver.test.example.com",
				Type:       provider.RecordTypeTXT,
				Target:     "heritage=dnsweaver",
				TTL:        300,
				ProviderID: "_dnsweaver.test.example.com.:TXT:heritage=dnsweaver",
			},
			wantErr: false,
		},
		{
			name: "SRV record",
			record: dnsupdate.Record{
				Name:     "_http._tcp.example.com.",
				Type:     dns.TypeSRV,
				TTL:      300,
				RData:    "web.example.com.",
				Priority: 10,
				Weight:   20,
				Port:     80,
			},
			want: provider.Record{
				Hostname:   "_http._tcp.example.com",
				Type:       provider.RecordTypeSRV,
				Target:     "web.example.com",
				TTL:        300,
				ProviderID: "_http._tcp.example.com.:SRV:web.example.com.",
				SRV: &provider.SRVData{
					Priority: 10,
					Weight:   20,
					Port:     80,
				},
			},
			wantErr: false,
		},
		{
			name: "unsupported MX record",
			record: dnsupdate.Record{
				Name:     "example.com.",
				Type:     dns.TypeMX,
				TTL:      300,
				RData:    "mail.example.com.",
				Priority: 10,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := p.fromRFC2136Record(tt.record)
			if (err != nil) != tt.wantErr {
				t.Errorf("fromRFC2136Record() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.Hostname != tt.want.Hostname {
				t.Errorf("Hostname = %q, want %q", got.Hostname, tt.want.Hostname)
			}
			if got.Type != tt.want.Type {
				t.Errorf("Type = %v, want %v", got.Type, tt.want.Type)
			}
			if got.Target != tt.want.Target {
				t.Errorf("Target = %q, want %q", got.Target, tt.want.Target)
			}
			if got.TTL != tt.want.TTL {
				t.Errorf("TTL = %v, want %v", got.TTL, tt.want.TTL)
			}
			if got.ProviderID != tt.want.ProviderID {
				t.Errorf("ProviderID = %q, want %q", got.ProviderID, tt.want.ProviderID)
			}
			// Check SRV data
			if tt.want.SRV != nil {
				if got.SRV == nil {
					t.Error("SRV is nil, want non-nil")
				} else {
					if got.SRV.Priority != tt.want.SRV.Priority {
						t.Errorf("SRV.Priority = %v, want %v", got.SRV.Priority, tt.want.SRV.Priority)
					}
					if got.SRV.Weight != tt.want.SRV.Weight {
						t.Errorf("SRV.Weight = %v, want %v", got.SRV.Weight, tt.want.SRV.Weight)
					}
					if got.SRV.Port != tt.want.SRV.Port {
						t.Errorf("SRV.Port = %v, want %v", got.SRV.Port, tt.want.SRV.Port)
					}
				}
			}
		})
	}
}

// Interface compliance verification
func TestInterfaceCompliance(t *testing.T) {
	var _ provider.Provider = (*Provider)(nil)
	var _ provider.Updater = (*Provider)(nil)
}
