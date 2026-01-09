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
			name:         "ownership record exists",
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

			got := cache.hasOwnershipRecord(tt.providerName, tt.hostname)
			if got != tt.want {
				t.Errorf("hasOwnershipRecord(%q, %q) = %v, want %v",
					tt.providerName, tt.hostname, got, tt.want)
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
			records:     map[string]map[string][]provider.Record{},
			wantRecords: 0,
			wantCached:  false,
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
