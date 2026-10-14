package dnsupdate

import (
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func TestOwnershipTXTWireChunksPreservePayloadAndLegacyCatalog(t *testing.T) {
	value := strings.Repeat("ownership-payload", 180)
	for _, name := range []string{"_dnsweaver-state.example.com", "_dw-p.owner.digest.example.com", "_dnsweaver.app.example.com"} {
		rr, err := (Record{Name: name, Type: dns.TypeTXT, TTL: 300, RData: value}).ToRR()
		if err != nil {
			t.Fatal(err)
		}
		message := new(dns.Msg)
		message.Answer = []dns.RR{rr}
		wire, err := message.Pack()
		if err != nil {
			t.Fatal("ownership TXT exceeds a wire string", err)
		}
		var received dns.Msg
		if err := received.Unpack(wire); err != nil {
			t.Fatal(err)
		}
		record, err := RecordFromRR(received.Answer[0])
		if err != nil || record.RData != value {
			t.Fatal("ownership wire chunks changed the payload", err)
		}
	}
	catalog := &dns.TXT{Hdr: dns.RR_Header{Name: "_dnsweaver-catalog-0.example.com.", Rrtype: dns.TypeTXT}, Txt: []string{"one.example.com", "two.example.com"}}
	record, err := RecordFromRR(catalog)
	if err != nil || record.RData != "one.example.com two.example.com" {
		t.Fatal("legacy catalog hostname delimiters changed", err)
	}
	user := &dns.TXT{Hdr: dns.RR_Header{Name: "app.example.com.", Rrtype: dns.TypeTXT}, Txt: []string{"user text", "other text"}}
	record, err = RecordFromRR(user)
	if err != nil || record.RData != "user text other text" {
		t.Fatal("ordinary user TXT behavior changed", err)
	}
}
