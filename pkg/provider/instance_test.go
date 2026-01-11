package provider

import "testing"

func TestIsIPAddress(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Valid IPv4
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"255.255.255.255", true},
		{"0.0.0.0", true},

		// Valid IPv6
		{"::1", true},
		{"fe80::1", true},
		{"2001:db8::1", true},
		{"::ffff:192.168.1.1", true},

		// Invalid - hostnames
		{"example.com", false},
		{"app.example.com", false},
		{"subdomain.app.example.com", false},
		{"localhost", false},

		// Invalid - malformed
		{"10.0.0.256", false},
		{"10.0.0", false},
		{"10.0.0.1.1", false},
		{"", false},
		{"not-an-ip", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isIPAddress(tt.input)
			if got != tt.want {
				t.Errorf("isIPAddress(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsIPv4Address(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Valid IPv4
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"255.255.255.255", true},
		{"0.0.0.0", true},

		// IPv6 should return false
		{"::1", false},
		{"fe80::1", false},
		{"2001:db8::1", false},

		// Note: IPv4-mapped IPv6 addresses return true for To4()
		// This is correct behavior for our use case

		// Invalid
		{"example.com", false},
		{"", false},
		{"10.0.0.256", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isIPv4Address(tt.input)
			if got != tt.want {
				t.Errorf("isIPv4Address(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsIPv6Address(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Valid IPv6
		{"::1", true},
		{"fe80::1", true},
		{"2001:db8::1", true},

		// IPv4 should return false
		{"10.0.0.1", false},
		{"192.168.1.1", false},
		{"0.0.0.0", false},

		// Invalid
		{"example.com", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isIPv6Address(tt.input)
			if got != tt.want {
				t.Errorf("isIPv6Address(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestProviderInstanceConfig_Validate_RecordTypeTargetMismatch(t *testing.T) {
	tests := []struct {
		name       string
		recordType RecordType
		target     string
		wantErr    bool
		errContain string
	}{
		// Valid combinations
		{
			name:       "A record with IPv4",
			recordType: RecordTypeA,
			target:     "10.0.0.1",
			wantErr:    false,
		},
		{
			name:       "AAAA record with IPv6",
			recordType: RecordTypeAAAA,
			target:     "2001:db8::1",
			wantErr:    false,
		},
		{
			name:       "AAAA record with loopback IPv6",
			recordType: RecordTypeAAAA,
			target:     "::1",
			wantErr:    false,
		},
		{
			name:       "CNAME with hostname",
			recordType: RecordTypeCNAME,
			target:     "example.com",
			wantErr:    false,
		},
		{
			name:       "CNAME with subdomain",
			recordType: RecordTypeCNAME,
			target:     "tunnel.cloudflare.com",
			wantErr:    false,
		},

		// Invalid combinations
		{
			name:       "CNAME with IPv4 target",
			recordType: RecordTypeCNAME,
			target:     "10.0.0.1",
			wantErr:    true,
			errContain: "CNAME records cannot point to IP addresses",
		},
		{
			name:       "CNAME with IPv6 target",
			recordType: RecordTypeCNAME,
			target:     "::1",
			wantErr:    true,
			errContain: "CNAME records cannot point to IP addresses",
		},
		{
			name:       "A record with hostname target",
			recordType: RecordTypeA,
			target:     "example.com",
			wantErr:    true,
			errContain: "A records must point to IPv4 addresses",
		},
		{
			name:       "A record with IPv6 target",
			recordType: RecordTypeA,
			target:     "2001:db8::1",
			wantErr:    true,
			errContain: "A records must point to IPv4 addresses",
		},
		{
			name:       "AAAA record with IPv4 target",
			recordType: RecordTypeAAAA,
			target:     "10.0.0.1",
			wantErr:    true,
			errContain: "AAAA records must point to IPv6 addresses",
		},
		{
			name:       "AAAA record with hostname target",
			recordType: RecordTypeAAAA,
			target:     "example.com",
			wantErr:    true,
			errContain: "AAAA records must point to IPv6 addresses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ProviderInstanceConfig{
				Name:       "test-instance",
				TypeName:   "technitium",
				RecordType: tt.recordType,
				Target:     tt.target,
				TTL:        300,
				Domains:    []string{"*.example.com"},
			}

			err := cfg.Validate()

			if tt.wantErr {
				if err == nil {
					t.Error("expected validation error, got nil")
				} else if tt.errContain != "" {
					if !containsString(err.Error(), tt.errContain) {
						t.Errorf("error %q should contain %q", err.Error(), tt.errContain)
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected validation error: %v", err)
				}
			}
		})
	}
}

func TestProviderInstanceConfig_Validate_Complete(t *testing.T) {
	// Test a complete valid configuration
	cfg := ProviderInstanceConfig{
		Name:           "internal-dns",
		TypeName:       "technitium",
		RecordType:     RecordTypeA,
		Target:         "10.1.20.210",
		TTL:            300,
		Domains:        []string{"*.local.bluewillows.net"},
		ExcludeDomains: []string{"admin.*"},
		ProviderConfig: map[string]string{
			"url":  "http://dns:5380",
			"zone": "local.bluewillows.net",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}
}

func TestProviderInstanceConfig_Validate_CNAME_Complete(t *testing.T) {
	// Test a complete valid CNAME configuration
	cfg := ProviderInstanceConfig{
		Name:           "public-dns",
		TypeName:       "cloudflare",
		RecordType:     RecordTypeCNAME,
		Target:         "bluewillows.net",
		TTL:            300,
		Domains:        []string{"*.bluewillows.net"},
		ExcludeDomains: []string{"*.local.bluewillows.net"},
		ProviderConfig: map[string]string{
			"zone_id": "abc123",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}
}

func TestProviderInstanceConfig_Validate_AAAA_Complete(t *testing.T) {
	// Test a complete valid AAAA (IPv6) configuration
	cfg := ProviderInstanceConfig{
		Name:           "ipv6-dns",
		TypeName:       "technitium",
		RecordType:     RecordTypeAAAA,
		Target:         "2001:db8::1",
		TTL:            300,
		Domains:        []string{"*.local.bluewillows.net"},
		ExcludeDomains: []string{"admin.*"},
		ProviderConfig: map[string]string{
			"url":  "http://dns:5380",
			"zone": "local.bluewillows.net",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}
}

func TestOwnershipRecordName(t *testing.T) {
	tests := []struct {
		hostname string
		want     string
	}{
		{"app.example.com", "_dnsweaver.app.example.com"},
		{"subdomain.app.example.com", "_dnsweaver.subdomain.app.example.com"},
		{"example.com", "_dnsweaver.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			got := OwnershipRecordName(tt.hostname)
			if got != tt.want {
				t.Errorf("OwnershipRecordName(%q) = %q, want %q", tt.hostname, got, tt.want)
			}
		})
	}
}

func TestIsOwnershipRecord(t *testing.T) {
	tests := []struct {
		hostname string
		want     bool
	}{
		{"_dnsweaver.app.example.com", true},
		{"_dnsweaver.example.com", true},
		{"_dnsweaver.sub.app.example.com", true},
		{"app.example.com", false},
		{"example.com", false},
		{"_dnsweaver", false},
		{"_dnsweaver.", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			got := IsOwnershipRecord(tt.hostname)
			if got != tt.want {
				t.Errorf("IsOwnershipRecord(%q) = %v, want %v", tt.hostname, got, tt.want)
			}
		})
	}
}

func TestExtractHostnameFromOwnership(t *testing.T) {
	tests := []struct {
		ownershipName string
		want          string
	}{
		{"_dnsweaver.app.example.com", "app.example.com"},
		{"_dnsweaver.subdomain.app.example.com", "subdomain.app.example.com"},
		{"_dnsweaver.example.com", "example.com"},
		// Non-ownership records should return empty
		{"app.example.com", ""},
		{"example.com", ""},
		{"_dnsweaver", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.ownershipName, func(t *testing.T) {
			got := ExtractHostnameFromOwnership(tt.ownershipName)
			if got != tt.want {
				t.Errorf("ExtractHostnameFromOwnership(%q) = %q, want %q", tt.ownershipName, got, tt.want)
			}
		})
	}
}

// containsString checks if s contains substr (simple helper to avoid importing strings).
func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
