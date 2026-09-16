// Command govulncheck-gate validates govulncheck's streaming JSON output and
// applies dnsweaver's narrowly scoped vulnerability exceptions.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const expectedProtocol = "v1.0.0"

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type scanConfig struct {
	ProtocolVersion string `json:"protocol_version"`
	ScannerName     string `json:"scanner_name"`
	ScannerVersion  string `json:"scanner_version"`
	DB              string `json:"db"`
	DBLastModified  string `json:"db_last_modified"`
	GoVersion       string `json:"go_version"`
	ScanLevel       string `json:"scan_level"`
	ScanMode        string `json:"scan_mode"`
}

type scanSBOM struct {
	GoVersion string `json:"go_version"`
	Modules   []struct {
		Path    string `json:"path"`
		Version string `json:"version"`
	} `json:"modules"`
	Roots []string `json:"roots"`
}

type scanFinding struct {
	OSV   string `json:"osv"`
	Trace []struct {
		Module   string `json:"module"`
		Version  string `json:"version"`
		Package  string `json:"package"`
		Function string `json:"function"`
	} `json:"trace"`
}

type normalizedFinding struct {
	ID            string   `json:"id"`
	Module        string   `json:"module"`
	ModuleVersion string   `json:"module_version"`
	Reachability  string   `json:"reachability"`
	Packages      []string `json:"packages"`
}

type exceptionPolicy struct {
	SchemaVersion int         `json:"schema_version"`
	Exceptions    []exception `json:"exceptions"`
}

type exception struct {
	ID                string   `json:"id"`
	Module            string   `json:"module"`
	ModuleVersion     string   `json:"module_version"`
	Reachability      string   `json:"reachability"`
	Packages          []string `json:"packages"`
	Status            string   `json:"status"`
	Owner             string   `json:"owner"`
	Rationale         string   `json:"rationale"`
	EvidenceRevision  string   `json:"evidence_revision"`
	DecisionReference string   `json:"decision_reference"`
	DecisionDate      string   `json:"decision_date"`
	ReviewDate        string   `json:"review_date"`
	ExpiresAt         string   `json:"expires_at"`
	ReviewTriggers    []string `json:"review_triggers"`
}

type decision struct {
	Finding normalizedFinding `json:"finding"`
	Status  string            `json:"status"`
	Reason  string            `json:"reason"`
}

type gateReport struct {
	SchemaVersion int                 `json:"schema_version"`
	Passed        bool                `json:"passed"`
	Config        scanConfig          `json:"config"`
	SBOM          map[string]int      `json:"sbom"`
	OSVRecords    int                 `json:"osv_records"`
	Findings      []normalizedFinding `json:"findings"`
	Decisions     []decision          `json:"decisions"`
	Limitations   []string            `json:"limitations"`
}

type aggregateFinding struct {
	normalizedFinding
	rank     int
	packages map[string]struct{}
}

const (
	statusFail = "fail"
	statusPass = "pass"
)

func main() {
	input := flag.String("input", "", "govulncheck streaming JSON report")
	exceptions := flag.String("exceptions", "security/govulncheck-exceptions.json", "exception policy")
	output := flag.String("output", "", "normalized gate report")
	expectedVersion := flag.String("expected-scanner-version", "v1.8.0", "required govulncheck version")
	asOfText := flag.String("as-of", time.Now().UTC().Format("2006-01-02"), "policy evaluation date")
	flag.Parse()
	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "--input and --output are required")
		os.Exit(2)
	}
	asOf, err := time.Parse("2006-01-02", *asOfText)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid --as-of: %v\n", err)
		os.Exit(2)
	}
	report, err := run(*input, *exceptions, *expectedVersion, asOf)
	if err != nil {
		report.Passed = false
		report.Decisions = append(report.Decisions, decision{Status: statusFail, Reason: err.Error()})
	}
	if writeErr := writeJSONAtomic(*output, report); writeErr != nil {
		fmt.Fprintf(os.Stderr, "write normalized report: %v\n", writeErr)
		os.Exit(2)
	}
	for _, item := range report.Decisions {
		if item.Finding.ID == "" {
			fmt.Fprintf(os.Stderr, "govulncheck gate: %s\n", item.Reason)
			continue
		}
		fmt.Fprintf(os.Stderr, "govulncheck gate: %s %s@%s (%s): %s\n", item.Finding.ID, item.Finding.Module, item.Finding.ModuleVersion, item.Status, item.Reason)
	}
	if !report.Passed {
		os.Exit(1)
	}
	fmt.Printf("govulncheck gate passed: %d normalized finding(s)\n", len(report.Findings))
}

func run(inputPath, exceptionPath, expectedVersion string, asOf time.Time) (gateReport, error) {
	report := gateReport{SchemaVersion: 1, Limitations: []string{
		"govulncheck protocol v1.0.0 has no terminal completion record; completion is established by the pinned scanner process exiting zero plus a syntactically complete stream with required config and SBOM envelopes.",
	}}
	input, err := os.Open(inputPath)
	if err != nil {
		return report, fmt.Errorf("open raw report: %w", err)
	}
	config, sbom, osvCount, findings, err := parseStream(input, expectedVersion)
	closeErr := input.Close()
	report.Config = config
	report.SBOM = map[string]int{"modules": len(sbom.Modules), "roots": len(sbom.Roots)}
	report.OSVRecords = osvCount
	report.Findings = findings
	if err != nil {
		return report, err
	}
	if closeErr != nil {
		return report, fmt.Errorf("close raw report: %w", closeErr)
	}
	policy, err := readPolicy(exceptionPath)
	if err != nil {
		return report, err
	}
	report.Passed = true
	for _, finding := range findings {
		status, reason := decide(finding, policy, asOf)
		report.Decisions = append(report.Decisions, decision{Finding: finding, Status: status, Reason: reason})
		if status != statusPass {
			report.Passed = false
		}
	}
	return report, nil
}

func parseStream(reader io.Reader, expectedVersion string) (scanConfig, scanSBOM, int, []normalizedFinding, error) {
	decoder := json.NewDecoder(reader)
	var config scanConfig
	var sbom scanSBOM
	configCount, sbomCount, osvCount, messages := 0, 0, 0, 0
	osvIDs := make(map[string]struct{})
	aggregates := make(map[string]*aggregateFinding)
	for {
		var raw map[string]json.RawMessage
		err := decoder.Decode(&raw)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return config, sbom, osvCount, nil, fmt.Errorf("malformed or truncated govulncheck JSON stream: %w", err)
		}
		messages++
		if len(raw) != 1 {
			return config, sbom, osvCount, nil, fmt.Errorf("message %d has %d fields; expected exactly one", messages, len(raw))
		}
		for kind, payload := range raw {
			switch kind {
			case "config":
				if messages != 1 || configCount != 0 {
					return config, sbom, osvCount, nil, errors.New("config must be the first and only config message")
				}
				if err := json.Unmarshal(payload, &config); err != nil {
					return config, sbom, osvCount, nil, fmt.Errorf("invalid config: %w", err)
				}
				configCount++
			case "SBOM":
				if err := json.Unmarshal(payload, &sbom); err != nil {
					return config, sbom, osvCount, nil, fmt.Errorf("invalid SBOM: %w", err)
				}
				sbomCount++
			case "osv":
				var osv struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(payload, &osv); err != nil || osv.ID == "" {
					return config, sbom, osvCount, nil, fmt.Errorf("invalid OSV message")
				}
				osvIDs[osv.ID] = struct{}{}
				osvCount++
			case "finding":
				var finding scanFinding
				if err := json.Unmarshal(payload, &finding); err != nil {
					return config, sbom, osvCount, nil, fmt.Errorf("invalid finding: %w", err)
				}
				if err := addFinding(aggregates, finding); err != nil {
					return config, sbom, osvCount, nil, err
				}
			case "progress":
				// Informational by protocol definition.
			default:
				return config, sbom, osvCount, nil, fmt.Errorf("unsupported govulncheck message %q", kind)
			}
		}
	}
	if configCount != 1 || sbomCount != 1 {
		return config, sbom, osvCount, nil, fmt.Errorf("incomplete envelope: config=%d SBOM=%d", configCount, sbomCount)
	}
	if config.ProtocolVersion != expectedProtocol || config.ScannerName != "govulncheck" || config.ScannerVersion != expectedVersion {
		return config, sbom, osvCount, nil, fmt.Errorf("unexpected scanner identity: protocol=%q name=%q version=%q", config.ProtocolVersion, config.ScannerName, config.ScannerVersion)
	}
	if config.DB == "" || config.DBLastModified == "" || config.GoVersion == "" || config.ScanLevel != "symbol" || config.ScanMode != "source" {
		return config, sbom, osvCount, nil, errors.New("scanner config is missing DB/Go metadata or is not a symbol-level source scan")
	}
	if _, err := time.Parse(time.RFC3339, config.DBLastModified); err != nil {
		return config, sbom, osvCount, nil, fmt.Errorf("invalid database timestamp: %w", err)
	}
	if sbom.GoVersion == "" || len(sbom.Modules) == 0 || len(sbom.Roots) == 0 {
		return config, sbom, osvCount, nil, errors.New("SBOM envelope is missing Go version, modules, or roots")
	}
	findings := make([]normalizedFinding, 0, len(aggregates))
	for _, aggregate := range aggregates {
		if _, ok := osvIDs[aggregate.ID]; !ok {
			return config, sbom, osvCount, nil, fmt.Errorf("finding %s has no matching OSV record", aggregate.ID)
		}
		aggregate.Packages = sortedKeys(aggregate.packages)
		findings = append(findings, aggregate.normalizedFinding)
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].ID != findings[j].ID {
			return findings[i].ID < findings[j].ID
		}
		return findings[i].Module < findings[j].Module
	})
	return config, sbom, osvCount, findings, nil
}

func addFinding(aggregates map[string]*aggregateFinding, finding scanFinding) error {
	if finding.OSV == "" || len(finding.Trace) == 0 || finding.Trace[0].Module == "" {
		return errors.New("finding is missing an advisory ID or first trace frame module")
	}
	frame := finding.Trace[0]
	reachability, rank := "module", 1
	if frame.Package != "" {
		reachability, rank = "package", 2
	}
	if frame.Function != "" {
		reachability, rank = "symbol", 3
	}
	key := strings.Join([]string{finding.OSV, frame.Module, frame.Version}, "\x00")
	aggregate, ok := aggregates[key]
	if !ok {
		aggregate = &aggregateFinding{normalizedFinding: normalizedFinding{ID: finding.OSV, Module: frame.Module, ModuleVersion: frame.Version}, packages: make(map[string]struct{})}
		aggregates[key] = aggregate
	}
	if rank > aggregate.rank {
		aggregate.rank = rank
		aggregate.Reachability = reachability
		aggregate.packages = make(map[string]struct{})
	}
	if rank == aggregate.rank && frame.Package != "" {
		aggregate.packages[frame.Package] = struct{}{}
	}
	return nil
}

func readPolicy(path string) (exceptionPolicy, error) {
	var policy exceptionPolicy
	file, err := os.Open(path)
	if err != nil {
		return policy, fmt.Errorf("open exception policy: %w", err)
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return policy, fmt.Errorf("parse exception policy: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return policy, errors.New("parse exception policy: trailing JSON data")
	}
	if policy.SchemaVersion != 1 {
		return policy, fmt.Errorf("unsupported exception schema %d", policy.SchemaVersion)
	}
	seen := make(map[string]struct{})
	for _, item := range policy.Exceptions {
		if item.Status != "accepted" && item.Status != "proposed" {
			return policy, fmt.Errorf("exception %s has unsupported status %q", item.ID, item.Status)
		}
		key := strings.Join([]string{item.ID, item.Module, item.ModuleVersion, item.Reachability, strings.Join(item.Packages, "\x00")}, "\x01")
		if _, exists := seen[key]; exists {
			return policy, fmt.Errorf("duplicate exception scope for %s", item.ID)
		}
		seen[key] = struct{}{}
	}
	return policy, nil
}

func decide(finding normalizedFinding, policy exceptionPolicy, asOf time.Time) (string, string) {
	var sameID []exception
	for _, candidate := range policy.Exceptions {
		if candidate.ID == finding.ID {
			sameID = append(sameID, candidate)
		}
		if exceptionScopeEqual(candidate, finding) {
			if candidate.Status != "accepted" {
				return statusFail, fmt.Sprintf("matching exception is %q, not accepted", candidate.Status)
			}
			if err := validateAccepted(candidate, asOf); err != nil {
				return statusFail, err.Error()
			}
			return statusPass, "matched a current, explicitly accepted scoped exception"
		}
	}
	if len(sameID) > 0 {
		return statusFail, "advisory exists in policy but module, version, package set, or reachability changed"
	}
	return statusFail, "no accepted exception"
}

func exceptionScopeEqual(candidate exception, finding normalizedFinding) bool {
	return candidate.ID == finding.ID && candidate.Module == finding.Module && candidate.ModuleVersion == finding.ModuleVersion && candidate.Reachability == finding.Reachability && equalStrings(candidate.Packages, finding.Packages)
}

func validateAccepted(candidate exception, asOf time.Time) error {
	if candidate.Owner == "" || candidate.Rationale == "" || candidate.DecisionReference == "" || !revisionPattern.MatchString(candidate.EvidenceRevision) || len(candidate.ReviewTriggers) == 0 {
		return errors.New("accepted exception is missing owner, rationale, evidence revision, decision reference, or review triggers")
	}
	decisionDate, err := time.Parse("2006-01-02", candidate.DecisionDate)
	if err != nil || decisionDate.After(asOf) {
		return errors.New("accepted exception has an invalid or future decision date")
	}
	reviewDate, err := time.Parse("2006-01-02", candidate.ReviewDate)
	if err != nil || !asOf.Before(reviewDate) {
		return errors.New("accepted exception is missing review or its review date is due")
	}
	expiresAt, err := time.Parse("2006-01-02", candidate.ExpiresAt)
	if err != nil || !asOf.Before(expiresAt) {
		return errors.New("accepted exception is missing expiry or has expired")
	}
	if expiresAt.Before(reviewDate) {
		return errors.New("accepted exception expires before its review date")
	}
	return nil
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func equalStrings(left, right []string) bool {
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	sort.Strings(left)
	sort.Strings(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func writeJSONAtomic(path string, value any) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, ".govulncheck-gate-*.json")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()
	encoder := json.NewEncoder(temp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}
