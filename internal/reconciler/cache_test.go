package reconciler

import (
	"log/slog"
	"testing"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
)

func TestRecordCache_HasOwnershipRecord(t *testing.T) {
	tests := []struct {
		name         string
		records      map[string]map[string][]provider.Record
		providerName string
		hostname     string
		instanceID   string
		want         bool
	}{
		{
			name:         "no ownership record exists",
			providerName: "test-provider",
			hostname:     "app.example.com",
			records: map[string]map[string][]provider.Record{
				"test-provider": {
					"app.example.com": {
						{Hostname: "app.example.com", Type: provider.RecordTypeCNAME, Target: "host.example.com"},
					},
				},
			},
			want: false,
		},
		{
			name:         "ownership record exists - legacy format",
			providerName: "test-provider",
			hostname:     "app.example.com",
			records: map[string]map[string][]provider.Record{
				"test-provider": {
					"app.example.com": {
						{Hostname: "app.example.com", Type: provider.RecordTypeCNAME, Target: "host.example.com"},
					},
					"_dnsweaver.app.example.com": {
						{Hostname: "_dnsweaver.app.example.com", Type: provider.RecordTypeTXT, Target: "heritage=dnsweaver"},
					},
				},
			},
			want: true,
		},
		{
			name:         "ownership record with instance ID matches",
			providerName: "test-provider",
			hostname:     "app.example.com",
			instanceID:   "pi5-dns",
			records: map[string]map[string][]provider.Record{
				"test-provider": {
					"_dnsweaver.app.example.com": {
						{Hostname: "_dnsweaver.app.example.com", Type: provider.RecordTypeTXT, Target: "heritage=dnsweaver,instance=pi5-dns"},
					},
				},
			},
			want: true,
		},
		{
			name:         "ownership record with wrong instance ID",
			providerName: "test-provider",
			hostname:     "app.example.com",
			instanceID:   "pi5-dns",
			records: map[string]map[string][]provider.Record{
				"test-provider": {
					"_dnsweaver.app.example.com": {
						{Hostname: "_dnsweaver.app.example.com", Type: provider.RecordTypeTXT, Target: "heritage=dnsweaver,instance=k8s-node"},
					},
				},
			},
			want: false,
		},
		{
			name:         "legacy record does not match when instance ID set",
			providerName: "test-provider",
			hostname:     "app.example.com",
			instanceID:   "pi5-dns",
			records: map[string]map[string][]provider.Record{
				"test-provider": {
					"_dnsweaver.app.example.com": {
						{Hostname: "_dnsweaver.app.example.com", Type: provider.RecordTypeTXT, Target: "heritage=dnsweaver"},
					},
				},
			},
			want: false,
		},
		{
			name:         "TXT record with wrong value",
			providerName: "test-provider",
			hostname:     "app.example.com",
			records: map[string]map[string][]provider.Record{
				"test-provider": {
					"_dnsweaver.app.example.com": {
						{Hostname: "_dnsweaver.app.example.com", Type: provider.RecordTypeTXT, Target: "wrong-value"},
					},
				},
			},
			want: false,
		},
		{
			name:         "ownership record but wrong type",
			providerName: "test-provider",
			hostname:     "app.example.com",
			records: map[string]map[string][]provider.Record{
				"test-provider": {
					"_dnsweaver.app.example.com": {
						{Hostname: "_dnsweaver.app.example.com", Type: provider.RecordTypeA, Target: "heritage=dnsweaver"},
					},
				},
			},
			want: false,
		},
		{
			name:         "provider not in cache",
			providerName: "missing-provider",
			hostname:     "app.example.com",
			records: map[string]map[string][]provider.Record{
				"other-provider": {},
			},
			want: false,
		},
		{
			name:         "provider cache failed (nil)",
			providerName: "test-provider",
			hostname:     "app.example.com",
			records: map[string]map[string][]provider.Record{
				"test-provider": nil,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &recordCache{
				records: tt.records,
				logger:  slog.Default(),
			}

			got := cache.hasOwnershipRecord(tt.providerName, tt.hostname, tt.instanceID)
			if got != tt.want {
				t.Errorf("hasOwnershipRecord(%q, %q, %q) = %v, want %v",
					tt.providerName, tt.hostname, tt.instanceID, got, tt.want)
			}
		})
	}
}

func TestRecordCache_GetExistingRecords(t *testing.T) {
	tests := []struct {
		name         string
		records      map[string]map[string][]provider.Record
		providerName string
		hostname     string
		wantRecords  int
		wantCached   bool
	}{
		{
			name:         "returns A and CNAME records",
			providerName: "test-provider",
			hostname:     "app.example.com",
			records: map[string]map[string][]provider.Record{
				"test-provider": {
					"app.example.com": {
						{Hostname: "app.example.com", Type: provider.RecordTypeCNAME, Target: "host.example.com"},
						{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.0.0.1"},
					},
				},
			},
			wantRecords: 2,
			wantCached:  true,
		},
		{
			name:         "filters out TXT records",
			providerName: "test-provider",
			hostname:     "app.example.com",
			records: map[string]map[string][]provider.Record{
				"test-provider": {
					"app.example.com": {
						{Hostname: "app.example.com", Type: provider.RecordTypeCNAME, Target: "host.example.com"},
						{Hostname: "app.example.com", Type: provider.RecordTypeTXT, Target: "some-txt-value"},
					},
				},
			},
			wantRecords: 1,
			wantCached:  true,
		},
		{
			name:         "provider not cached returns false",
			providerName: "missing-provider",
			hostname:     "app.example.com",
			records:      map[string]map[string][]provider.Record{},
			wantRecords:  0,
			wantCached:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &recordCache{
				records: tt.records,
				logger:  slog.Default(),
			}

			records, cached := cache.getExistingRecords(tt.providerName, tt.hostname)
			if cached != tt.wantCached {
				t.Errorf("getExistingRecords cached = %v, want %v", cached, tt.wantCached)
			}
			if len(records) != tt.wantRecords {
				t.Errorf("getExistingRecords returned %d records, want %d", len(records), tt.wantRecords)
			}
		})
	}
}

func TestRecordCache_DistinguishesMemberAndLegacyOwnership(t *testing.T) {
	member := provider.Record{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "192.0.2.10"}
	memberMarker := provider.MemberOwnershipRecord(member.Hostname, 300, "instance-a", member, map[string]string{"source": "native"})
	legacyMarker := provider.OwnershipRecord(member.Hostname, 300, "instance-a", nil)
	cache := &recordCache{
		records: map[string]map[string][]provider.Record{
			"dns": {
				"_dnsweaver.app.example.com": {memberMarker, legacyMarker},
			},
		},
		logger: slog.Default(),
	}

	gotMember, found := cache.memberOwnershipRecord("dns", member, "instance-a")
	if !found || gotMember.Target != memberMarker.Target {
		t.Fatalf("member marker = %+v, %v", gotMember, found)
	}
	gotLegacy, found := cache.legacyOwnershipRecord("dns", member.Hostname, "instance-a")
	if !found || gotLegacy.Target != legacyMarker.Target {
		t.Fatalf("legacy marker = %+v, %v", gotLegacy, found)
	}
	if _, found := cache.memberOwnershipRecord("dns", provider.Record{Hostname: member.Hostname, Type: member.Type, Target: "192.0.2.11"}, "instance-a"); found {
		t.Fatal("member marker matched another target")
	}
}
