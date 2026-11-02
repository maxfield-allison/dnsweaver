package provider

import (
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func TestDecodeTXTStrings_DNSCharacterEscapes(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  string
	}{
		{`"a\065b"`, "aAb"},
		{`"a\032b"`, "a b"},
		{`"a\12b"`, "a12b"},
		{`"a\x41b"`, "ax41b"},
		{`"a\b"`, "ab"},
		{`"a\\b"`, `a\b`},
		{`"one\032" "two"`, "one two"},
		{`"\000\255"`, string([]byte{0, 255})},
		{`""`, ""},
	} {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := DecodeTXTStrings(tt.input)
			if !ok || got != tt.want {
				t.Fatalf("decoded = %q, %v; want %q, true", got, ok, tt.want)
			}
			if wire := txtPayloadOnWire(t, tt.input); wire != tt.want {
				t.Fatalf("DNS wire payload = %q, want %q", wire, tt.want)
			}
		})
	}
	for _, invalid := range []string{`"\256"`, `"\999"`, `"trailing\`, `"unterminated`} {
		if _, ok := DecodeTXTStrings(invalid); ok {
			t.Errorf("accepted invalid TXT presentation %q", invalid)
		}
	}
}

func TestTXTPresentation_PreservesPayloadOnDNSWire(t *testing.T) {
	payloads := []string{"", `"`, `"open`, `"hello"`, "\"\x00\x7f\xff\""}
	for _, size := range []int{251, 252, 253, 254, 255, 256, 511} {
		payloads = append(payloads, `"`+strings.Repeat("a", size)+"é🙂"+`"`)
	}
	for _, payload := range payloads {
		t.Run(payload, func(t *testing.T) {
			presentation := TXTPresentation(payload)
			if got := txtPayloadOnWire(t, presentation); got != payload {
				t.Fatalf("wire payload = %q, want %q; presentation=%q", got, payload, presentation)
			}
			if decoded, ok := DecodeTXTStrings(presentation); !ok || decoded != payload {
				t.Fatalf("readback = %q, %v; want %q, true", decoded, ok, payload)
			}
		})
	}
}

// Decode the packed RDATA bytes, rather than dns.TXT.Txt, which retains
// presentation escapes. The independent DNS parser also enforces chunk sizes.
func txtPayloadOnWire(t *testing.T, presentation string) string {
	t.Helper()
	rr, err := dns.NewRR("txt.example. 300 IN TXT " + presentation)
	if err != nil {
		t.Fatal(err)
	}
	message := dns.Msg{Answer: []dns.RR{rr}}
	packet, err := message.Pack()
	if err != nil {
		t.Fatal(err)
	}
	_, offset, err := dns.UnpackDomainName(packet, 12)
	if err != nil {
		t.Fatal(err)
	}
	offset += 10 // type, class, TTL and RDLENGTH
	var payload strings.Builder
	for offset < len(packet) {
		size := int(packet[offset])
		offset++
		payload.Write(packet[offset : offset+size])
		offset += size
	}
	return payload.String()
}
