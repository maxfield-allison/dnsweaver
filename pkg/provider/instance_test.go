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
			name:       "A record with IPv6",
			recordType: RecordTypeA,
			target:     "2001:db8::1",
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
			errContain: "A records must point to IP addresses",
		},
		{
			name:       "A record with subdomain target",
			recordType: RecordTypeA,
			target:     "app.example.com",
			wantErr:    true,
			errContain: "A records must point to IP addresses",
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

// containsString checks if s contains substr (simple helper to avoid importing strings).
func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
