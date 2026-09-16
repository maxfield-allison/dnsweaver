package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeMessage(t *testing.T, buffer *bytes.Buffer, value any) {
	t.Helper()
	if err := json.NewEncoder(buffer).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func validStream(t *testing.T, includeFinding bool) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writeMessage(t, &buffer, map[string]any{"config": scanConfig{
		ProtocolVersion: expectedProtocol,
		ScannerName:     "govulncheck",
		ScannerVersion:  "v1.8.0",
		DB:              "https://vuln.go.dev",
		DBLastModified:  "2026-09-15T18:39:25Z",
		GoVersion:       "go1.26.8",
		ScanLevel:       "symbol",
		ScanMode:        "source",
	}})
	writeMessage(t, &buffer, map[string]any{"SBOM": map[string]any{
		"go_version": "go1.26.8",
		"modules":    []map[string]string{{"path": "example.test/app"}},
		"roots":      []string{"example.test/app"},
	}})
	if includeFinding {
		writeMessage(t, &buffer, map[string]any{"osv": map[string]string{"id": "GO-2099-0001"}})
		writeMessage(t, &buffer, map[string]any{"finding": map[string]any{
			"osv": "GO-2099-0001",
			"trace": []map[string]string{{
				"module": "example.test/dependency", "version": "v1.2.3",
				"package": "example.test/dependency/client", "function": "Open",
			}},
		}})
	}
	return buffer.Bytes()
}

func TestParseStreamCleanAndFinding(t *testing.T) {
	for _, includeFinding := range []bool{false, true} {
		_, _, _, findings, err := parseStream(bytes.NewReader(validStream(t, includeFinding)), "v1.8.0")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := len(findings), 0; includeFinding {
			want = 1
			if got != want {
				t.Fatalf("findings=%d, want %d", got, want)
			}
		} else if got != want {
			t.Fatalf("findings=%d, want %d", got, want)
		}
	}
}

func TestParseStreamRejectsInvalidCompletionEvidence(t *testing.T) {
	tests := map[string][]byte{
		"truncated":        append(validStream(t, false), []byte(`{"finding":`)...),
		"missing-sbom":     bytes.Split(validStream(t, false), []byte("\n"))[0],
		"unsupported-kind": append(validStream(t, false), []byte("{\"complete\":{}}\n")...),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, _, _, err := parseStream(bytes.NewReader(input), "v1.8.0"); err == nil {
				t.Fatal("expected invalid stream to fail")
			}
		})
	}

	wrongVersion := strings.Replace(string(validStream(t, false)), "v1.8.0", "v1.8.1", 1)
	if _, _, _, _, err := parseStream(strings.NewReader(wrongVersion), "v1.8.0"); err == nil {
		t.Fatal("expected scanner version mismatch to fail")
	}
}

func TestExceptionDecisionMatrix(t *testing.T) {
	finding := normalizedFinding{
		ID:            "GO-2099-0001",
		Module:        "example.test/dependency",
		ModuleVersion: "v1.2.3",
		Reachability:  "symbol",
		Packages:      []string{"example.test/dependency/client"},
	}
	accepted := exception{
		ID: finding.ID, Module: finding.Module, ModuleVersion: finding.ModuleVersion,
		Reachability: finding.Reachability, Packages: finding.Packages,
		Status: "accepted", Owner: "maintainer", Rationale: "bounded test rationale",
		EvidenceRevision: strings.Repeat("a", 64), DecisionReference: "decision-1",
		DecisionDate: "2026-09-15", ReviewDate: "2026-10-16", ExpiresAt: "2026-12-16",
		ReviewTriggers: []string{"dependency or reachability changes"},
	}
	asOf := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		exceptions []exception
		want       string
	}{
		{name: "missing", want: "fail"},
		{name: "accepted", exceptions: []exception{accepted}, want: "pass"},
		{name: "proposed", exceptions: []exception{func() exception { value := accepted; value.Status = "proposed"; return value }()}, want: "fail"},
		{name: "wrong-package", exceptions: []exception{func() exception {
			value := accepted
			value.Packages = []string{"example.test/dependency/other"}
			return value
		}()}, want: "fail"},
		{name: "expired", exceptions: []exception{func() exception { value := accepted; value.ExpiresAt = "2026-09-16"; return value }()}, want: "fail"},
		{name: "review-due", exceptions: []exception{func() exception { value := accepted; value.ReviewDate = "2026-09-16"; return value }()}, want: "fail"},
		{name: "missing-authority", exceptions: []exception{func() exception { value := accepted; value.DecisionReference = ""; return value }()}, want: "fail"},
		{name: "legacy-revision", exceptions: []exception{func() exception { value := accepted; value.EvidenceRevision = strings.Repeat("a", 40); return value }()}, want: "fail"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _ := decide(finding, exceptionPolicy{SchemaVersion: 1, Exceptions: test.exceptions}, asOf)
			if got != test.want {
				t.Fatalf("decision=%q, want %q", got, test.want)
			}
		})
	}
}

func TestApprovedPolicyIsExactAndCurrent(t *testing.T) {
	dockerPackages := []string{
		"github.com/docker/docker/api",
		"github.com/docker/docker/api/types",
		"github.com/docker/docker/api/types/blkiodev",
		"github.com/docker/docker/api/types/build",
		"github.com/docker/docker/api/types/checkpoint",
		"github.com/docker/docker/api/types/common",
		"github.com/docker/docker/api/types/container",
		"github.com/docker/docker/api/types/events",
		"github.com/docker/docker/api/types/filters",
		"github.com/docker/docker/api/types/image",
		"github.com/docker/docker/api/types/mount",
		"github.com/docker/docker/api/types/network",
		"github.com/docker/docker/api/types/registry",
		"github.com/docker/docker/api/types/storage",
		"github.com/docker/docker/api/types/strslice",
		"github.com/docker/docker/api/types/swarm",
		"github.com/docker/docker/api/types/swarm/runtime",
		"github.com/docker/docker/api/types/system",
		"github.com/docker/docker/api/types/time",
		"github.com/docker/docker/api/types/versions",
		"github.com/docker/docker/api/types/volume",
		"github.com/docker/docker/client",
	}
	dockerSymbolTriggers := []string{
		"advisory or Go vulnerability database symbol metadata changes",
		"dnsweaver imports Docker daemon, plugin, authorization, or server code",
		"dnsweaver Docker access becomes mutating",
		"a supported fixed release on the current Docker module path becomes available",
		"a stable github.com/moby/moby/v2 client module becomes available",
	}
	dockerModuleTriggers := []string{
		"dnsweaver imports a Docker daemon package",
		"dnsweaver adds a Docker copy or archive operation",
		"advisory or Go vulnerability database scope changes",
		"a supported fixed release on the current Docker module path becomes available",
		"a stable github.com/moby/moby/v2 client module becomes available",
	}
	cryptoModuleTriggers := []string{
		"dnsweaver imports any x/crypto/openpgp package",
		"advisory or Go vulnerability database scope changes",
		"the dependency graph changes package reachability",
		"a supported fixed x/crypto release becomes available",
	}
	const dockerSymbolRationale = "Advisory metadata describes Docker daemon plugin and authorization behavior; the inspected dnsweaver paths use the Docker client and do not import daemon, plugin, or server code. Acceptance is limited to this exact scanner scope and retains the residual metadata ambiguity."
	const dockerModuleRationale = "The scanner reports only module reachability. The affected unexported Docker daemon archive and copy functions are not imported or called by dnsweaver."
	const cryptoModuleRationale = "The scanner reports only module reachability for x/crypto/openpgp. dnsweaver imports x/crypto/ssh and ssh/knownhosts, not openpgp."
	type scope struct {
		module       string
		version      string
		reachability string
		packages     []string
		rationale    string
		triggers     []string
	}
	expected := map[string]scope{
		"GO-2026-4883": {"github.com/docker/docker", "v28.5.2+incompatible", "symbol", dockerPackages, dockerSymbolRationale, dockerSymbolTriggers},
		"GO-2026-4887": {"github.com/docker/docker", "v28.5.2+incompatible", "symbol", dockerPackages, dockerSymbolRationale, dockerSymbolTriggers},
		"GO-2026-5617": {"github.com/docker/docker", "v28.5.2+incompatible", "module", nil, dockerModuleRationale, dockerModuleTriggers},
		"GO-2026-5668": {"github.com/docker/docker", "v28.5.2+incompatible", "module", nil, dockerModuleRationale, dockerModuleTriggers},
		"GO-2026-5746": {"github.com/docker/docker", "v28.5.2+incompatible", "module", nil, dockerModuleRationale, dockerModuleTriggers},
		"GO-2026-5932": {"golang.org/x/crypto", "v0.57.0", "module", nil, cryptoModuleRationale, cryptoModuleTriggers},
	}

	reviewPath := filepath.Join("..", "..", "security", "govulncheck-exception-review.md")
	review, err := os.ReadFile(reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	reviewDigest := sha256.Sum256(review)
	evidenceRevision := hex.EncodeToString(reviewDigest[:])
	const decisionReference = "security/govulncheck-exception-review.md#maintainer-decision"

	policyPath := filepath.Join("..", "..", "security", "govulncheck-exceptions.json")
	policy, err := readPolicy(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Exceptions) != len(expected) {
		t.Fatalf("approved exceptions = %d, want %d", len(policy.Exceptions), len(expected))
	}
	asOf := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	for _, item := range policy.Exceptions {
		want, ok := expected[item.ID]
		if !ok {
			t.Fatalf("unexpected approved exception %q", item.ID)
		}
		if item.Module != want.module || item.ModuleVersion != want.version || item.Reachability != want.reachability || !equalStrings(item.Packages, want.packages) {
			t.Fatalf("scope for %s = %s@%s %s %v, want %s@%s %s %v", item.ID, item.Module, item.ModuleVersion, item.Reachability, item.Packages, want.module, want.version, want.reachability, want.packages)
		}
		if item.Status != "accepted" || item.Owner != "Max Allison" || item.Rationale != want.rationale || item.EvidenceRevision != evidenceRevision || item.DecisionReference != decisionReference || item.DecisionDate != "2026-09-16" || item.ReviewDate != "2026-10-16" || item.ExpiresAt != "2026-12-16" {
			t.Fatalf("authority metadata for %s changed: %+v", item.ID, item)
		}
		if !equalStrings(item.ReviewTriggers, want.triggers) {
			t.Fatalf("review triggers for %s = %v, want %v", item.ID, item.ReviewTriggers, want.triggers)
		}
		finding := normalizedFinding{ID: item.ID, Module: item.Module, ModuleVersion: item.ModuleVersion, Reachability: item.Reachability, Packages: item.Packages}
		if got, _ := decide(finding, policy, asOf); got != statusPass {
			t.Fatalf("exact approved scope for %s = %q, want pass", item.ID, got)
		}
		finding.ModuleVersion += ".changed"
		if got, _ := decide(finding, policy, asOf); got != statusFail {
			t.Fatalf("changed scope for %s = %q, want fail", item.ID, got)
		}
	}
}

func TestParseStreamRequiresFindingOSV(t *testing.T) {
	input := strings.Replace(string(validStream(t, true)), "{\"osv\":{\"id\":\"GO-2099-0001\"}}\n", "", 1)
	if _, _, _, _, err := parseStream(strings.NewReader(input), "v1.8.0"); err == nil {
		t.Fatal("finding without OSV record passed")
	}
}
