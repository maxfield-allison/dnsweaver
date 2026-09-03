package dnsweaver

import "testing"

func TestParser_AdoptionHintPrecedence(t *testing.T) {
	parser := NewParser(WithParserLogger(testLogger()))
	extractions := parser.ExtractHostnames(map[string]string{
		"dnsweaver.hostname":               "simple.example.com",
		"dnsweaver.records.named.hostname": "named.example.com",
		"dnsweaver.records.named.adopt":    "false",
	})

	got := make(map[string]*bool, len(extractions))
	for i := range extractions {
		got[extractions[i].Hostname] = extractions[i].AdoptExisting
	}
	if got["simple.example.com"] != nil {
		t.Error("workload adoption should be applied by the source registry, not stored as a record hint")
	}
	if got["named.example.com"] == nil || *got["named.example.com"] {
		t.Error("named adopt=false was not parsed")
	}
}
