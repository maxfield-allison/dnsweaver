package provider

import "testing"

func TestDecodeTXTStrings(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{"one string", `"v=spf1 -all"`, "v=spf1 -all", true},
		{"split strings", `"p=MIIB" "IjAN"`, "p=MIIBIjAN", true},
		{"escaped quote", `"say \"hi\""`, `say "hi"`, true},
		{"unquoted", "v=spf1 -all", "", false},
		{"unterminated", `"open`, "", false},
		{"text after a string", `"a" b`, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DecodeTXTStrings(tt.input)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("DecodeTXTStrings(%q) = %q, %v; want %q, %v", tt.input, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestTXTPayloadAndPresentation(t *testing.T) {
	tests := []struct {
		name         string
		declared     string
		payload      string
		presentation string
	}{
		{"plain", "v=spf1 -all", "v=spf1 -all", "v=spf1 -all"},
		{"quoted", `"v=spf1 -all"`, "v=spf1 -all", "v=spf1 -all"},
		{"split", `"p=MIIB" "IjAN"`, "p=MIIBIjAN", "p=MIIBIjAN"},
		{"data containing quotes", `"\"hello\""`, `"hello"`, `"\"hello\""`},
		{"malformed quoting is data", `"open`, `"open`, `"open`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TXTPayload(tt.declared); got != tt.payload {
				t.Errorf("TXTPayload(%q) = %q, want %q", tt.declared, got, tt.payload)
			}
			if got := TXTPresentation(tt.payload); got != tt.presentation {
				t.Errorf("TXTPresentation(%q) = %q, want %q", tt.payload, got, tt.presentation)
			}
			if got := TXTPayload(TXTPresentation(tt.payload)); got != tt.payload {
				t.Errorf("round trip of %q = %q", tt.payload, got)
			}
		})
	}
}

func TestMapMemberOwnershipTXTTarget(t *testing.T) {
	txt := func(target string) Record { return Record{Type: RecordTypeTXT, Target: target} }
	quotedData := MakeMemberOwnershipValue("x", txt(`"hello"`), map[string]string{"source": "native"})

	presented := MapMemberOwnershipTXTTarget(quotedData, TXTPresentation)
	if !MatchesMemberOwnership(presented, "x", txt(`"\"hello\""`)) {
		t.Errorf("presented marker %q does not record the presentation text", presented)
	}
	if got := MapMemberOwnershipTXTTarget(presented, TXTPayload); got != quotedData {
		t.Errorf("round trip = %q, want %q", got, quotedData)
	}
	plain := MakeMemberOwnershipValue("x", txt("v=spf1 -all"), nil)
	if got := MapMemberOwnershipTXTTarget(plain, TXTPresentation); got != plain {
		t.Errorf("unchanged target rewrote the marker: %q", got)
	}
	a := MakeMemberOwnershipValue("x", Record{Type: RecordTypeA, Target: "192.0.2.1"}, nil)
	if got := MapMemberOwnershipTXTTarget(a, TXTPayload); got != a {
		t.Errorf("A marker changed: %q", got)
	}
	// Without a provider mapping, a marker owns exactly the value it records.
	if MatchesMemberOwnership(MakeMemberOwnershipValue("x", txt(`"hello"`), nil), "x", txt("hello")) {
		t.Error("marker for the data \"hello\" matched the data hello")
	}
}

func TestDeclaredTXT(t *testing.T) {
	if got := DeclaredTXT(presentationProvider{}, `"\"hello\""`); got != `"hello"` {
		t.Errorf("presentation provider: DeclaredTXT = %q, want %q", got, `"hello"`)
	}
	if got := DeclaredTXT(nil, `"hello"`); got != `"hello"` {
		t.Errorf("verbatim provider: DeclaredTXT = %q, want the declared value", got)
	}
}

type presentationProvider struct{ Provider }

func (presentationProvider) TXTPresentationText() {}
