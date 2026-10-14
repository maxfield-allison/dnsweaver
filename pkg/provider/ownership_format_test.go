package provider

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestOwnershipFormatReadsPublishedV1Fixture(t *testing.T) {
	payload, err := os.ReadFile("testdata/ownership-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Records []Record `json:"records"`
		Marker  Record   `json:"marker"`
	}
	if err := json.Unmarshal(payload, &fixture); err != nil {
		t.Fatal(err)
	}
	logical, err := ExpandOwnershipState("example.com", fixture.Records)
	if err != nil || len(logical) != 1 || !exactOwnershipRecordPresent(logical, fixture.Marker) {
		t.Fatalf("v1 persisted wire compatibility: %+v, %v", logical, err)
	}
	owned, owner, member, metadata := ParseMemberOwnershipValue(logical[0].Target)
	if !owned || owner != "fixture" || member == nil || member.Type != RecordTypeA || member.Target != "192.0.2.1" || metadata["note"] != "golden" {
		t.Fatal("v1 member or metadata semantics changed")
	}
}

func TestOwnershipFormatDeterministicPaginationAndMemberMetadata(t *testing.T) {
	var entries []OwnershipEntry
	for i := range 120 {
		r := stateTestMember(i)
		r.Type = RecordTypeSRV
		r.Target = "backend.example.com"
		r.SRV = &SRVData{Priority: 10, Weight: uint16(i), Port: 443}
		marker := stateTestMarker("a", r)
		entries = append(entries, OwnershipEntry{marker.Hostname, marker.Target})
	}
	pages, err := packOwnershipPages("example.com", "a", entries, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) < 2 {
		t.Fatal("test needs multiple byte-bounded pages")
	}
	var recovered []OwnershipEntry
	for _, page := range pages {
		if len(page.Target) > 500 {
			t.Fatal("oversized page")
		}
		decoded, err := decodeOwnershipPage(page.Target, "a", 500)
		if err != nil {
			t.Fatal(err)
		}
		recovered = append(recovered, decoded...)
	}
	if len(recovered) != len(entries) {
		t.Fatal("pagination lost members")
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	reordered, err := packOwnershipPages("example.com", "a", entries, 500)
	if err != nil || fmt.Sprint(pages) != fmt.Sprint(reordered) {
		t.Fatal("encoding depends on input order", err)
	}
}

func TestOwnershipFormatRejectsUnsupportedMalformedAndOversizedInputs(t *testing.T) {
	manifest := OwnershipManifest{Version: 1, Owner: "a", Revision: 1, Pages: []string{}, Entries: 0}
	valid, err := encodeOwnershipManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := DecodeOwnershipManifest(valid); err != nil || decoded.Owner != "a" {
		t.Fatal("valid roundtrip", err)
	}
	for _, payload := range []string{
		`{"version":2,"owner":"a","revision":1,"previous":"","pages":[],"entries":0}`,
		`{"version":1,"owner":"a","owner":"b","revision":1,"previous":"","pages":[],"entries":0}`,
		`{"version":1,"owner":"a","revision":1,"previous":"","pages":[],"entries":0,"unknown":true}`,
		`{"version":1,"owner":"a","revision":1,"previous":"","pages":[],"entries":0} {}`,
		`{"version":1,"owner":"a","revision":2,"previous":"","pages":[],"entries":0}`,
		`{"version":1,"revision":1,"previous":"","pages":[],"entries":0}`,
		`{"version":1,"owner":null,"revision":1,"previous":"","pages":[],"entries":0}`,
	} {
		encoded := ownershipManifestPrefix + base64.RawURLEncoding.EncodeToString([]byte(payload))
		if _, err := DecodeOwnershipManifest(encoded); !errors.Is(err, ErrOwnershipState) {
			t.Fatalf("accepted malformed manifest %s: %v", payload, err)
		}
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte(strings.Repeat(" ", maxOwnershipExpandedBytes+1))); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	bomb := ownershipPagePrefix + base64.RawURLEncoding.EncodeToString(compressed.Bytes())
	if _, err := decodeOwnershipPage(bomb, "a", DefaultOwnershipPageBytes); !errors.Is(err, ErrOwnershipState) {
		t.Fatal("accepted expanded page overflow", err)
	}
	if OwnershipPageName("example.com", "a", "short") != "" {
		t.Fatal("invalid digest produced a page name")
	}
	entry := OwnershipEntry{OwnershipRecordName("app.other.com"), stateTestMarker("a", stateTestMember(1)).Target}
	if _, err := packOwnershipPages("example.com", "a", []OwnershipEntry{entry}, 500); !errors.Is(err, ErrOwnershipState) {
		t.Fatal("accepted outside-zone ownership", err)
	}
}
