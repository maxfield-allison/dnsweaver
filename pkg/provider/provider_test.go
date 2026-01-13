package provider

import "testing"

func TestRecordEquals(t *testing.T) {
	tests := []struct {
		name     string
		a        Record
		b        Record
		expected bool
	}{
		{
			name: "identical A records",
			a: Record{
				Hostname: "app.example.com",
				Type:     RecordTypeA,
				Target:   "10.0.0.1",
				TTL:      300,
			},
			b: Record{
				Hostname: "app.example.com",
				Type:     RecordTypeA,
				Target:   "10.0.0.1",
				TTL:      300,
			},
			expected: true,
		},
		{
			name: "different hostnames",
			a: Record{
				Hostname: "app1.example.com",
				Type:     RecordTypeA,
				Target:   "10.0.0.1",
				TTL:      300,
			},
			b: Record{
				Hostname: "app2.example.com",
				Type:     RecordTypeA,
				Target:   "10.0.0.1",
				TTL:      300,
			},
			expected: false,
		},
		{
			name: "different types",
			a: Record{
				Hostname: "app.example.com",
				Type:     RecordTypeA,
				Target:   "10.0.0.1",
				TTL:      300,
			},
			b: Record{
				Hostname: "app.example.com",
				Type:     RecordTypeAAAA,
				Target:   "::1",
				TTL:      300,
			},
			expected: false,
		},
		{
			name: "different TTL",
			a: Record{
				Hostname: "app.example.com",
				Type:     RecordTypeA,
				Target:   "10.0.0.1",
				TTL:      300,
			},
			b: Record{
				Hostname: "app.example.com",
				Type:     RecordTypeA,
				Target:   "10.0.0.1",
				TTL:      600,
			},
			expected: false,
		},
		{
			name: "identical SRV records",
			a: Record{
				Hostname: "_minecraft._tcp.example.com",
				Type:     RecordTypeSRV,
				Target:   "mc.example.com",
				TTL:      3600,
				SRV: &SRVData{
					Priority: 10,
					Weight:   5,
					Port:     25565,
				},
			},
			b: Record{
				Hostname: "_minecraft._tcp.example.com",
				Type:     RecordTypeSRV,
				Target:   "mc.example.com",
				TTL:      3600,
				SRV: &SRVData{
					Priority: 10,
					Weight:   5,
					Port:     25565,
				},
			},
			expected: true,
		},
		{
			name: "SRV records with different priority",
			a: Record{
				Hostname: "_minecraft._tcp.example.com",
				Type:     RecordTypeSRV,
				Target:   "mc.example.com",
				TTL:      3600,
				SRV: &SRVData{
					Priority: 10,
					Weight:   5,
					Port:     25565,
				},
			},
			b: Record{
				Hostname: "_minecraft._tcp.example.com",
				Type:     RecordTypeSRV,
				Target:   "mc.example.com",
				TTL:      3600,
				SRV: &SRVData{
					Priority: 20,
					Weight:   5,
					Port:     25565,
				},
			},
			expected: false,
		},
		{
			name: "SRV records with different weight",
			a: Record{
				Hostname: "_minecraft._tcp.example.com",
				Type:     RecordTypeSRV,
				Target:   "mc.example.com",
				TTL:      3600,
				SRV: &SRVData{
					Priority: 10,
					Weight:   5,
					Port:     25565,
				},
			},
			b: Record{
				Hostname: "_minecraft._tcp.example.com",
				Type:     RecordTypeSRV,
				Target:   "mc.example.com",
				TTL:      3600,
				SRV: &SRVData{
					Priority: 10,
					Weight:   10,
					Port:     25565,
				},
			},
			expected: false,
		},
		{
			name: "SRV records with different port",
			a: Record{
				Hostname: "_minecraft._tcp.example.com",
				Type:     RecordTypeSRV,
				Target:   "mc.example.com",
				TTL:      3600,
				SRV: &SRVData{
					Priority: 10,
					Weight:   5,
					Port:     25565,
				},
			},
			b: Record{
				Hostname: "_minecraft._tcp.example.com",
				Type:     RecordTypeSRV,
				Target:   "mc.example.com",
				TTL:      3600,
				SRV: &SRVData{
					Priority: 10,
					Weight:   5,
					Port:     25566,
				},
			},
			expected: false,
		},
		{
			name: "SRV record with nil vs non-nil SRV data",
			a: Record{
				Hostname: "_minecraft._tcp.example.com",
				Type:     RecordTypeSRV,
				Target:   "mc.example.com",
				TTL:      3600,
				SRV:      nil,
			},
			b: Record{
				Hostname: "_minecraft._tcp.example.com",
				Type:     RecordTypeSRV,
				Target:   "mc.example.com",
				TTL:      3600,
				SRV: &SRVData{
					Priority: 10,
					Weight:   5,
					Port:     25565,
				},
			},
			expected: false,
		},
		{
			name: "SRV records with both nil SRV data",
			a: Record{
				Hostname: "_minecraft._tcp.example.com",
				Type:     RecordTypeSRV,
				Target:   "mc.example.com",
				TTL:      3600,
				SRV:      nil,
			},
			b: Record{
				Hostname: "_minecraft._tcp.example.com",
				Type:     RecordTypeSRV,
				Target:   "mc.example.com",
				TTL:      3600,
				SRV:      nil,
			},
			expected: true,
		},
		{
			name: "provider ID should not affect equality",
			a: Record{
				Hostname:   "app.example.com",
				Type:       RecordTypeA,
				Target:     "10.0.0.1",
				TTL:        300,
				ProviderID: "record-123",
			},
			b: Record{
				Hostname:   "app.example.com",
				Type:       RecordTypeA,
				Target:     "10.0.0.1",
				TTL:        300,
				ProviderID: "record-456",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RecordEquals(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("RecordEquals() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestRecordTypeConstants(t *testing.T) {
	// Verify record type constants are correct
	if RecordTypeA != "A" {
		t.Errorf("RecordTypeA = %q, expected %q", RecordTypeA, "A")
	}
	if RecordTypeAAAA != "AAAA" {
		t.Errorf("RecordTypeAAAA = %q, expected %q", RecordTypeAAAA, "AAAA")
	}
	if RecordTypeCNAME != "CNAME" {
		t.Errorf("RecordTypeCNAME = %q, expected %q", RecordTypeCNAME, "CNAME")
	}
	if RecordTypeTXT != "TXT" {
		t.Errorf("RecordTypeTXT = %q, expected %q", RecordTypeTXT, "TXT")
	}
	if RecordTypeSRV != "SRV" {
		t.Errorf("RecordTypeSRV = %q, expected %q", RecordTypeSRV, "SRV")
	}
}

func TestCapabilities_SupportsRecordType(t *testing.T) {
	tests := []struct {
		name     string
		caps     Capabilities
		rt       RecordType
		expected bool
	}{
		{
			name: "full capabilities - A record",
			caps: Capabilities{
				SupportedRecordTypes: []RecordType{RecordTypeA, RecordTypeAAAA, RecordTypeCNAME, RecordTypeSRV, RecordTypeTXT},
			},
			rt:       RecordTypeA,
			expected: true,
		},
		{
			name: "full capabilities - SRV record",
			caps: Capabilities{
				SupportedRecordTypes: []RecordType{RecordTypeA, RecordTypeAAAA, RecordTypeCNAME, RecordTypeSRV, RecordTypeTXT},
			},
			rt:       RecordTypeSRV,
			expected: true,
		},
		{
			name: "limited capabilities - A only",
			caps: Capabilities{
				SupportedRecordTypes: []RecordType{RecordTypeA},
			},
			rt:       RecordTypeA,
			expected: true,
		},
		{
			name: "limited capabilities - missing AAAA",
			caps: Capabilities{
				SupportedRecordTypes: []RecordType{RecordTypeA, RecordTypeCNAME},
			},
			rt:       RecordTypeAAAA,
			expected: false,
		},
		{
			name: "empty capabilities",
			caps: Capabilities{
				SupportedRecordTypes: []RecordType{},
			},
			rt:       RecordTypeA,
			expected: false,
		},
		{
			name: "nil capabilities",
			caps: Capabilities{
				SupportedRecordTypes: nil,
			},
			rt:       RecordTypeA,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.caps.SupportsRecordType(tt.rt)
			if result != tt.expected {
				t.Errorf("SupportsRecordType(%s) = %v, expected %v", tt.rt, result, tt.expected)
			}
		})
	}
}

func TestCapabilities_Defaults(t *testing.T) {
	// Test that zero-value Capabilities are restrictive (safe defaults)
	var caps Capabilities

	if caps.SupportsOwnershipTXT {
		t.Error("zero-value SupportsOwnershipTXT should be false")
	}
	if caps.SupportsNativeUpdate {
		t.Error("zero-value SupportsNativeUpdate should be false")
	}
	if len(caps.SupportedRecordTypes) != 0 {
		t.Error("zero-value SupportedRecordTypes should be empty")
	}
	if caps.SupportsRecordType(RecordTypeA) {
		t.Error("zero-value caps should not support any record type")
	}
}
