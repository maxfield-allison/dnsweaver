package provider

import (
	"context"
	"testing"

	"github.com/maxfield-allison/dnsweaver/internal/matcher"
)

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
		Target:         "10.0.0.100",
		TTL:            300,
		Domains:        []string{"*.local.example.net"},
		ExcludeDomains: []string{"admin.*"},
		ProviderConfig: map[string]string{
			"url":  "http://dns:5380",
			"zone": "local.example.net",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}
}

func TestProviderInstanceConfig_Validate_TargetMode_NoTarget(t *testing.T) {
	// Regression for #130: when a dynamic TARGET_MODE is set, TARGET is
	// resolved at runtime and must not be required at validation time.
	cfg := ProviderInstanceConfig{
		Name:       "cloudflare",
		TypeName:   "cloudflare",
		RecordType: RecordTypeA,
		Target:     "", // resolved dynamically via TargetMode
		TargetMode: "public",
		TTL:        300,
		Domains:    []string{"*.example.com"},
		ProviderConfig: map[string]string{
			"zone_id": "abc123",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config with TargetMode and no Target, got error: %v", err)
	}
}

func TestProviderInstanceConfig_Validate_TargetMode_WithFallback(t *testing.T) {
	// A static fallback Target alongside a mode is still type-checked.
	valid := ProviderInstanceConfig{
		Name:           "cloudflare",
		TypeName:       "cloudflare",
		RecordType:     RecordTypeA,
		Target:         "203.0.113.10", // valid IPv4 fallback
		TargetMode:     "public",
		TTL:            300,
		Domains:        []string{"*.example.com"},
		ProviderConfig: map[string]string{"zone_id": "abc123"},
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("expected valid config with fallback Target, got error: %v", err)
	}

	invalid := valid
	invalid.Target = "not-an-ip" // fallback must still match record type
	if err := invalid.Validate(); err == nil {
		t.Error("expected error for non-IPv4 fallback Target on an A record, got nil")
	}
}

func TestProviderInstanceConfig_Validate_NoTargetNoMode(t *testing.T) {
	// Without a mode, TARGET remains required.
	cfg := ProviderInstanceConfig{
		Name:           "cloudflare",
		TypeName:       "cloudflare",
		RecordType:     RecordTypeA,
		TTL:            300,
		Domains:        []string{"*.example.com"},
		ProviderConfig: map[string]string{"zone_id": "abc123"},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected 'target required' error when neither Target nor TargetMode is set, got nil")
	}
}

func TestProviderInstanceConfig_Validate_CNAME_Complete(t *testing.T) {
	// Test a complete valid CNAME configuration
	cfg := ProviderInstanceConfig{
		Name:           "public-dns",
		TypeName:       "cloudflare",
		RecordType:     RecordTypeCNAME,
		Target:         "example.net",
		TTL:            300,
		Domains:        []string{"*.example.net"},
		ExcludeDomains: []string{"*.local.example.net"},
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
		Domains:        []string{"*.local.example.net"},
		ExcludeDomains: []string{"admin.*"},
		ProviderConfig: map[string]string{
			"url":  "http://dns:5380",
			"zone": "local.example.net",
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

// --- MetadataFilters / MatchesWithMetadata (#178) ---

// newTestInstance builds a ProviderInstance with a glob domain matcher and
// optional metadata filters. Test-only helper.
func newTestInstance(t *testing.T, includes []string, filters map[string][]string) *ProviderInstance {
	t.Helper()
	m, err := matcher.NewDomainMatcher(matcher.DomainMatcherConfig{Includes: includes})
	if err != nil {
		t.Fatalf("NewDomainMatcher: %v", err)
	}
	return &ProviderInstance{
		Matcher:         m,
		MetadataFilters: filters,
	}
}

func TestMatchesWithMetadata_NoFilters_DomainOnlyDecides(t *testing.T) {
	inst := newTestInstance(t, []string{"*.example.com"}, nil)

	if !inst.MatchesWithMetadata("app.example.com", nil) {
		t.Error("expected match: domain matches, no filters")
	}
	if inst.MatchesWithMetadata("app.other.com", nil) {
		t.Error("expected no match: domain mismatch")
	}
}

func TestMatchesWithMetadata_FilterMatches(t *testing.T) {
	inst := newTestInstance(t, []string{"*.example.com"}, map[string][]string{
		"traefik.entrypoint": {"webA"},
	})

	if !inst.MatchesWithMetadata("app.example.com", map[string]string{"traefik.entrypoint": "webA"}) {
		t.Error("expected match: filter satisfied")
	}
	if inst.MatchesWithMetadata("app.example.com", map[string]string{"traefik.entrypoint": "webB"}) {
		t.Error("expected no match: filter rejects webB")
	}
}

func TestMatchesWithMetadata_MissingKey_IsWildcard(t *testing.T) {
	// A hostname missing the filtered key must match — preserves backward
	// compat for sources/routers that don't surface the metadata.
	inst := newTestInstance(t, []string{"*.example.com"}, map[string][]string{
		"traefik.entrypoint": {"webA"},
	})

	if !inst.MatchesWithMetadata("app.example.com", nil) {
		t.Error("expected match: missing key is wildcard")
	}
	if !inst.MatchesWithMetadata("app.example.com", map[string]string{"unrelated": "x"}) {
		t.Error("expected match: filter key absent in metadata")
	}
}

func TestMatchesWithMetadata_MultipleFilters_AllMustMatch(t *testing.T) {
	inst := newTestInstance(t, []string{"*.example.com"}, map[string][]string{
		"traefik.entrypoint": {"webA"},
		"k8s.ingressClass":   {"public"},
	})

	// Both keys present and allowed — match.
	if !inst.MatchesWithMetadata("app.example.com", map[string]string{
		"traefik.entrypoint": "webA",
		"k8s.ingressClass":   "public",
	}) {
		t.Error("expected match: all filters satisfied")
	}

	// One key fails — no match.
	if inst.MatchesWithMetadata("app.example.com", map[string]string{
		"traefik.entrypoint": "webA",
		"k8s.ingressClass":   "internal",
	}) {
		t.Error("expected no match: ingressClass not allowed")
	}

	// Missing one key — wildcard for that filter, other still applies.
	if !inst.MatchesWithMetadata("app.example.com", map[string]string{
		"traefik.entrypoint": "webA",
	}) {
		t.Error("expected match: missing ingressClass is wildcard")
	}
}

func TestMatchesWithMetadata_DomainMismatch_FilterIrrelevant(t *testing.T) {
	inst := newTestInstance(t, []string{"*.example.com"}, map[string][]string{
		"traefik.entrypoint": {"webA"},
	})
	// Even with matching metadata, domain miss = no match.
	if inst.MatchesWithMetadata("app.other.com", map[string]string{"traefik.entrypoint": "webA"}) {
		t.Error("expected no match: domain miss short-circuits")
	}
}

func TestMatches_LegacyDomainOnly_IgnoresFilters(t *testing.T) {
	// Matches(string) is the pre-#178 signature and must keep ignoring
	// MetadataFilters so existing callers (orphan detection, legacy paths)
	// behave exactly as before.
	inst := newTestInstance(t, []string{"*.example.com"}, map[string][]string{
		"traefik.entrypoint": {"webA"},
	})

	if !inst.Matches("app.example.com") {
		t.Error("expected match: legacy Matches ignores filters")
	}
}

// comparingMockProvider is a mockProvider that also implements RecordComparer
// with a fixed answer, recording how often it was asked.
type comparingMockProvider struct {
	*mockProvider
	needsUpdate bool
	calls       int
}

func (m *comparingMockProvider) RecordNeedsUpdate(_, _ Record) bool {
	m.calls++
	return m.needsUpdate
}

func TestProviderInstance_RecordNeedsUpdate(t *testing.T) {
	existing := Record{Hostname: "app.example.com", Type: RecordTypeA, Target: "203.0.113.10", TTL: 1, Metadata: map[string]string{"proxied": "true"}}
	desired := Record{Hostname: "app.example.com", Type: RecordTypeA, Target: "203.0.113.10", TTL: 300, Metadata: map[string]string{"proxied": "false"}}

	tests := []struct {
		name      string
		prov      Provider
		want      bool
		wantCalls int
	}{
		{
			name: "provider without RecordComparer never reports a difference",
			prov: &mockProvider{name: "plain", typeName: "mock"},
			want: false,
		},
		{
			name:      "RecordComparer reporting a change is honored",
			prov:      &comparingMockProvider{mockProvider: &mockProvider{name: "cmp", typeName: "mock"}, needsUpdate: true},
			want:      true,
			wantCalls: 1,
		},
		{
			name:      "RecordComparer reporting no change is honored",
			prov:      &comparingMockProvider{mockProvider: &mockProvider{name: "cmp", typeName: "mock"}, needsUpdate: false},
			want:      false,
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := &ProviderInstance{Provider: tt.prov}
			if got := inst.RecordNeedsUpdate(existing, desired); got != tt.want {
				t.Errorf("RecordNeedsUpdate() = %v, want %v", got, tt.want)
			}
			if cmp, ok := tt.prov.(*comparingMockProvider); ok && cmp.calls != tt.wantCalls {
				t.Errorf("comparer consulted %d times, want %d", cmp.calls, tt.wantCalls)
			}
		})
	}
}

func TestProviderInstance_MemberOwnershipLifecycle(t *testing.T) {
	mock := &mockProvider{name: "mock", typeName: "mock"}
	inst := &ProviderInstance{Provider: mock, TTL: 300, InstanceID: "instance-a"}
	member := Record{Hostname: "app.example.com", Type: RecordTypeA, Target: "192.0.2.10"}
	metadata := map[string]string{"proxied": "false"}

	if err := inst.CreateMemberOwnershipRecord(context.Background(), member, metadata); err != nil {
		t.Fatalf("CreateMemberOwnershipRecord() error = %v", err)
	}
	if len(mock.created) != 1 || !MatchesMemberOwnership(mock.created[0].Target, "instance-a", member) {
		t.Fatalf("created records = %+v", mock.created)
	}
	owned, instanceID, createdMember, createdMetadata := ParseMemberOwnershipValue(mock.created[0].Target)
	if !owned || instanceID != "instance-a" || createdMember == nil {
		t.Fatalf("created marker did not parse: owned=%v instance=%q member=%+v", owned, instanceID, createdMember)
	}
	if createdMetadata["proxied"] != "false" {
		t.Fatalf("created metadata = %v", createdMetadata)
	}

	if err := inst.DeleteMemberOwnershipRecord(context.Background(), member, metadata); err != nil {
		t.Fatalf("DeleteMemberOwnershipRecord() error = %v", err)
	}
	if len(mock.deleted) != 1 || mock.deleted[0].Target != mock.created[0].Target {
		t.Fatalf("deleted marker = %+v, want exact created marker %+v", mock.deleted, mock.created)
	}
}

func TestProviderInstance_HasAndRecoversExactMemberOwnership(t *testing.T) {
	member := Record{Hostname: "app.example.com", Type: RecordTypeA, Target: "192.0.2.10"}
	other := Record{Hostname: "app.example.com", Type: RecordTypeA, Target: "192.0.2.11"}
	mock := &mockProvider{
		name:     "mock",
		typeName: "mock",
		records: []Record{
			MemberOwnershipRecord(member.Hostname, 300, "instance-a", member, map[string]string{"source": "native"}),
			MemberOwnershipRecord(other.Hostname, 300, "instance-b", other, nil),
			OwnershipRecord(member.Hostname, 300, "instance-a", nil),
		},
	}
	inst := &ProviderInstance{Provider: mock, InstanceID: "instance-a"}

	hasMember, err := inst.HasMemberOwnershipRecord(context.Background(), member)
	if err != nil || !hasMember {
		t.Fatalf("HasMemberOwnershipRecord(member) = %v, %v", hasMember, err)
	}
	hasOther, err := inst.HasMemberOwnershipRecord(context.Background(), other)
	if err != nil || hasOther {
		t.Fatalf("HasMemberOwnershipRecord(other) = %v, %v", hasOther, err)
	}

	recovered, err := inst.RecoverOwnedMembers(context.Background())
	if err != nil {
		t.Fatalf("RecoverOwnedMembers() error = %v", err)
	}
	if len(recovered) != 1 || !SameRecordMember(recovered[0].Record, member) {
		t.Fatalf("recovered = %+v, want only exact owned member", recovered)
	}
	if recovered[0].Record.Hostname != member.Hostname || recovered[0].Metadata["source"] != "native" {
		t.Fatalf("recovered = %+v", recovered[0])
	}
}
