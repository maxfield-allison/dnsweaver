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
		{
			name: "metadata should not affect equality",
			a: Record{
				Hostname: "app.example.com",
				Type:     RecordTypeA,
				Target:   "10.0.0.1",
				TTL:      300,
				Metadata: map[string]string{"proxied": "true"},
			},
			b: Record{
				Hostname: "app.example.com",
				Type:     RecordTypeA,
				Target:   "10.0.0.1",
				TTL:      300,
				Metadata: nil,
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

func TestMakeOwnershipValue(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		metadata   map[string]string
		expected   string
	}{
		{
			name:       "empty instance ID returns legacy format",
			instanceID: "",
			metadata:   nil,
			expected:   "heritage=dnsweaver",
		},
		{
			name:       "instance ID is included",
			instanceID: "pi5-dns",
			metadata:   nil,
			expected:   "heritage=dnsweaver,instance=pi5-dns",
		},
		{
			name:       "instance ID with dots and underscores",
			instanceID: "k8s_node.01",
			metadata:   nil,
			expected:   "heritage=dnsweaver,instance=k8s_node.01",
		},
		{
			name:       "with metadata",
			instanceID: "pi5-dns",
			metadata:   map[string]string{"proxied": "true"},
			expected:   "heritage=dnsweaver,instance=pi5-dns,proxied=true",
		},
		{
			name:       "metadata keys sorted",
			instanceID: "pi5-dns",
			metadata:   map[string]string{"source": "traefik", "proxied": "false"},
			expected:   "heritage=dnsweaver,instance=pi5-dns,proxied=false,source=traefik",
		},
		{
			name:       "reserved keys in metadata are ignored",
			instanceID: "pi5-dns",
			metadata:   map[string]string{"heritage": "evil", "instance": "evil", "proxied": "true"},
			expected:   "heritage=dnsweaver,instance=pi5-dns,proxied=true",
		},
		{
			name:       "metadata without instance ID",
			instanceID: "",
			metadata:   map[string]string{"proxied": "true"},
			expected:   "heritage=dnsweaver,proxied=true",
		},
		{
			name:       "empty metadata map same as nil",
			instanceID: "pi5-dns",
			metadata:   map[string]string{},
			expected:   "heritage=dnsweaver,instance=pi5-dns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MakeOwnershipValue(tt.instanceID, tt.metadata)
			if result != tt.expected {
				t.Errorf("MakeOwnershipValue(%q, %v) = %q, expected %q", tt.instanceID, tt.metadata, result, tt.expected)
			}
		})
	}
}

func TestParseOwnershipValue(t *testing.T) {
	tests := []struct {
		name           string
		value          string
		wantOwned      bool
		wantInstanceID string
		wantMetadata   map[string]string
	}{
		{
			name:           "legacy format",
			value:          "heritage=dnsweaver",
			wantOwned:      true,
			wantInstanceID: "",
			wantMetadata:   nil,
		},
		{
			name:           "with instance ID",
			value:          "heritage=dnsweaver,instance=pi5-dns",
			wantOwned:      true,
			wantInstanceID: "pi5-dns",
			wantMetadata:   nil,
		},
		{
			name:           "with complex instance ID",
			value:          "heritage=dnsweaver,instance=k8s-node_01.prod",
			wantOwned:      true,
			wantInstanceID: "k8s-node_01.prod",
			wantMetadata:   nil,
		},
		{
			name:           "not a dnsweaver record",
			value:          "some-other-value",
			wantOwned:      false,
			wantInstanceID: "",
			wantMetadata:   nil,
		},
		{
			name:           "partial heritage match",
			value:          "heritage=dnsweaver-fork",
			wantOwned:      false,
			wantInstanceID: "",
			wantMetadata:   nil,
		},
		{
			name:           "empty value",
			value:          "",
			wantOwned:      false,
			wantInstanceID: "",
			wantMetadata:   nil,
		},
		{
			name:           "heritage prefix only with comma but unknown field",
			value:          "heritage=dnsweaver,other=field",
			wantOwned:      true,
			wantInstanceID: "",
			wantMetadata:   map[string]string{"other": "field"},
		},
		{
			name:           "with instance and metadata",
			value:          "heritage=dnsweaver,instance=pi5-dns,proxied=true",
			wantOwned:      true,
			wantInstanceID: "pi5-dns",
			wantMetadata:   map[string]string{"proxied": "true"},
		},
		{
			name:           "with instance and multiple metadata",
			value:          "heritage=dnsweaver,instance=pi5-dns,proxied=false,source=traefik",
			wantOwned:      true,
			wantInstanceID: "pi5-dns",
			wantMetadata:   map[string]string{"proxied": "false", "source": "traefik"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOwned, gotInstanceID, gotMetadata := ParseOwnershipValue(tt.value)
			if gotOwned != tt.wantOwned {
				t.Errorf("ParseOwnershipValue(%q) isOwned = %v, want %v", tt.value, gotOwned, tt.wantOwned)
			}
			if gotInstanceID != tt.wantInstanceID {
				t.Errorf("ParseOwnershipValue(%q) instanceID = %q, want %q", tt.value, gotInstanceID, tt.wantInstanceID)
			}
			if tt.wantMetadata == nil {
				if gotMetadata != nil {
					t.Errorf("ParseOwnershipValue(%q) metadata = %v, want nil", tt.value, gotMetadata)
				}
			} else {
				if gotMetadata == nil {
					t.Errorf("ParseOwnershipValue(%q) metadata = nil, want %v", tt.value, tt.wantMetadata)
				} else {
					for k, v := range tt.wantMetadata {
						if gotMetadata[k] != v {
							t.Errorf("ParseOwnershipValue(%q) metadata[%q] = %q, want %q", tt.value, k, gotMetadata[k], v)
						}
					}
					if len(gotMetadata) != len(tt.wantMetadata) {
						t.Errorf("ParseOwnershipValue(%q) metadata len = %d, want %d", tt.value, len(gotMetadata), len(tt.wantMetadata))
					}
				}
			}
		})
	}
}

func TestMatchesOwnership(t *testing.T) {
	tests := []struct {
		name          string
		value         string
		ourInstanceID string
		expected      bool
	}{
		{
			name:          "legacy record matches empty instance ID",
			value:         "heritage=dnsweaver",
			ourInstanceID: "",
			expected:      true,
		},
		{
			name:          "legacy record does NOT match non-empty instance ID",
			value:         "heritage=dnsweaver",
			ourInstanceID: "pi5-dns",
			expected:      false,
		},
		{
			name:          "instance record matches same instance ID",
			value:         "heritage=dnsweaver,instance=pi5-dns",
			ourInstanceID: "pi5-dns",
			expected:      true,
		},
		{
			name:          "instance record does NOT match different instance ID",
			value:         "heritage=dnsweaver,instance=pi5-dns",
			ourInstanceID: "k8s-node",
			expected:      false,
		},
		{
			name:          "instance record does NOT match empty instance ID",
			value:         "heritage=dnsweaver,instance=pi5-dns",
			ourInstanceID: "",
			expected:      false,
		},
		{
			name:          "not a dnsweaver record",
			value:         "some-other-value",
			ourInstanceID: "",
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchesOwnership(tt.value, tt.ourInstanceID)
			if result != tt.expected {
				t.Errorf("MatchesOwnership(%q, %q) = %v, expected %v", tt.value, tt.ourInstanceID, result, tt.expected)
			}
		})
	}
}

func TestIsDnsweaverOwned(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		{
			name:     "legacy format is owned",
			value:    "heritage=dnsweaver",
			expected: true,
		},
		{
			name:     "instance format is owned",
			value:    "heritage=dnsweaver,instance=pi5-dns",
			expected: true,
		},
		{
			name:     "other value is not owned",
			value:    "something-else",
			expected: false,
		},
		{
			name:     "empty is not owned",
			value:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsDnsweaverOwned(tt.value)
			if result != tt.expected {
				t.Errorf("IsDnsweaverOwned(%q) = %v, expected %v", tt.value, result, tt.expected)
			}
		})
	}
}

func TestOwnershipRecord_WithInstanceID(t *testing.T) {
	// Legacy (no instance ID, no metadata)
	r := OwnershipRecord("app.example.com", 300, "", nil)
	if r.Hostname != "_dnsweaver.app.example.com" {
		t.Errorf("expected hostname '_dnsweaver.app.example.com', got %q", r.Hostname)
	}
	if r.Target != "heritage=dnsweaver" {
		t.Errorf("expected target 'heritage=dnsweaver', got %q", r.Target)
	}

	// With instance ID, no metadata
	r2 := OwnershipRecord("app.example.com", 300, "pi5-dns", nil)
	if r2.Hostname != "_dnsweaver.app.example.com" {
		t.Errorf("expected hostname '_dnsweaver.app.example.com', got %q", r2.Hostname)
	}
	if r2.Target != "heritage=dnsweaver,instance=pi5-dns" {
		t.Errorf("expected target 'heritage=dnsweaver,instance=pi5-dns', got %q", r2.Target)
	}

	// With instance ID and metadata
	r3 := OwnershipRecord("app.example.com", 300, "pi5-dns", map[string]string{"proxied": "true"})
	if r3.Target != "heritage=dnsweaver,instance=pi5-dns,proxied=true" {
		t.Errorf("expected target 'heritage=dnsweaver,instance=pi5-dns,proxied=true', got %q", r3.Target)
	}
}

func TestOwnershipRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		metadata   map[string]string
	}{
		{"nil metadata", "pi5-dns", nil},
		{"empty metadata", "pi5-dns", map[string]string{}},
		{"single key", "pi5-dns", map[string]string{"proxied": "true"}},
		{"multiple keys", "pi5-dns", map[string]string{"proxied": "false", "source": "traefik"}},
		{"no instance", "", map[string]string{"proxied": "true"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := MakeOwnershipValue(tt.instanceID, tt.metadata)
			gotOwned, gotID, gotMeta := ParseOwnershipValue(value)

			if !gotOwned {
				t.Fatal("round-trip: isOwned = false")
			}
			if gotID != tt.instanceID {
				t.Errorf("round-trip: instanceID = %q, want %q", gotID, tt.instanceID)
			}

			// nil and empty metadata should both round-trip to nil
			if len(tt.metadata) == 0 {
				if gotMeta != nil {
					t.Errorf("round-trip: metadata = %v, want nil", gotMeta)
				}
			} else {
				for k, v := range tt.metadata {
					if k == "heritage" || k == "instance" {
						continue // reserved keys not round-tripped
					}
					if gotMeta[k] != v {
						t.Errorf("round-trip: metadata[%q] = %q, want %q", k, gotMeta[k], v)
					}
				}
			}
		})
	}
}

func TestMemberOwnershipRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		record Record
	}{
		{name: "A", record: Record{Type: RecordTypeA, Target: "192.0.2.10"}},
		{name: "AAAA", record: Record{Type: RecordTypeAAAA, Target: "2001:db8::10"}},
		{name: "CNAME", record: Record{Type: RecordTypeCNAME, Target: "target.example.com."}},
		{name: "TXT with delimiter", record: Record{Type: RecordTypeTXT, Target: "one,two=three"}},
		{name: "SRV", record: Record{Type: RecordTypeSRV, Target: "sip.example.com", SRV: &SRVData{Priority: 10, Weight: 20, Port: 5060}}},
		{name: "HTTPS", record: Record{Type: RecordTypeHTTPS, Target: "", HTTPS: &HTTPSData{Priority: 1, TargetName: ".", ALPN: "h2,h3"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := MakeMemberOwnershipValue("instance-a", tt.record, map[string]string{"source": "traefik"})
			owned, instanceID, member, metadata := ParseMemberOwnershipValue(value)
			if !owned || instanceID != "instance-a" {
				t.Fatalf("ownership parse = owned:%v instance:%q", owned, instanceID)
			}
			if member == nil || !SameRecordMember(*member, tt.record) {
				t.Fatalf("member = %+v, want %+v", member, tt.record)
			}
			if metadata["source"] != "traefik" {
				t.Fatalf("metadata = %v, want source=traefik", metadata)
			}
		})
	}
}

func TestParseMemberOwnershipValue_LegacyIsAmbiguous(t *testing.T) {
	owned, instanceID, member, metadata := ParseMemberOwnershipValue("heritage=dnsweaver,instance=instance-a,proxied=true")
	if !owned || instanceID != "instance-a" {
		t.Fatalf("ownership parse = owned:%v instance:%q", owned, instanceID)
	}
	if member != nil {
		t.Fatalf("legacy marker produced member %+v", member)
	}
	if metadata["proxied"] != "true" {
		t.Fatalf("metadata = %v, want proxied=true", metadata)
	}
}

func TestParseMemberOwnershipValue_MalformedV2IsAmbiguous(t *testing.T) {
	value := "heritage=dnsweaver,instance=instance-a,record-target=not+base64,record-type=A,record-version=2"
	owned, instanceID, member, _ := ParseMemberOwnershipValue(value)
	if !owned || instanceID != "instance-a" {
		t.Fatalf("ownership parse = owned:%v instance:%q", owned, instanceID)
	}
	if member != nil {
		t.Fatalf("malformed marker produced member %+v", member)
	}
}

func TestMatchesMemberOwnership(t *testing.T) {
	record := Record{Type: RecordTypeA, Target: "192.0.2.10"}
	value := MakeMemberOwnershipValue("instance-a", record, nil)
	if !MatchesMemberOwnership(value, "instance-a", record) {
		t.Fatal("exact member ownership did not match")
	}
	if MatchesMemberOwnership(value, "instance-b", record) {
		t.Fatal("member ownership matched another instance")
	}
	if MatchesMemberOwnership(value, "instance-a", Record{Type: RecordTypeA, Target: "192.0.2.11"}) {
		t.Fatal("member ownership matched another target")
	}
	if MatchesMemberOwnership("heritage=dnsweaver,instance=instance-a", "instance-a", record) {
		t.Fatal("legacy ownership marker matched an exact member")
	}
}

func TestSameRecordMemberCanonicalizesDNSValues(t *testing.T) {
	if !SameRecordMember(
		Record{Type: RecordTypeAAAA, Target: "2001:0db8:0:0:0:0:0:1"},
		Record{Type: RecordTypeAAAA, Target: "2001:db8::1"},
	) {
		t.Fatal("equivalent IPv6 values did not match")
	}
	if !SameRecordMember(
		Record{Type: RecordTypeCNAME, Target: "Target.Example.COM."},
		Record{Type: RecordTypeCNAME, Target: "target.example.com"},
	) {
		t.Fatal("equivalent CNAME values did not match")
	}
	if SameRecordMember(
		Record{Type: RecordTypeSRV, Target: "sip.example.com", SRV: &SRVData{Priority: 10, Weight: 20, Port: 5060}},
		Record{Type: RecordTypeSRV, Target: "sip.example.com", SRV: &SRVData{Priority: 20, Weight: 20, Port: 5060}},
	) {
		t.Fatal("different SRV members matched")
	}
}

func TestMemberOwnershipRecord(t *testing.T) {
	member := Record{Hostname: "app.example.com", Type: RecordTypeA, Target: "192.0.2.10"}
	ownership := MemberOwnershipRecord(member.Hostname, 300, "instance-a", member, nil)
	if ownership.Hostname != "_dnsweaver.app.example.com" || ownership.Type != RecordTypeTXT || ownership.TTL != 300 {
		t.Fatalf("ownership record = %+v", ownership)
	}
	if !MatchesMemberOwnership(ownership.Target, "instance-a", member) {
		t.Fatalf("ownership target does not identify member: %q", ownership.Target)
	}
}
