package reconciler

import (
	"context"
	"log/slog"
	"strings"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/pkg/source"
	"github.com/maxfield-allison/dnsweaver/pkg/workload"
)

type extractedClaims struct {
	Claims   []*source.Hostname
	Unique   map[string]*source.Hostname
	Complete bool
}

func (r *Reconciler) extractClaims(ctx context.Context, workloads []workload.Workload, result *Result) extractedClaims {
	extracted := extractedClaims{
		Unique:   make(map[string]*source.Hostname),
		Complete: true,
	}
	origins := make(map[string]string)
	reportedDuplicates := make(map[string]struct{})

	for _, currentWorkload := range workloads {
		hostnames, complete := r.sources.ExtractAllWithStatus(ctx, currentWorkload)
		if !complete {
			extracted.Complete = false
		}
		valid := r.validateClaims(hostnames, currentWorkload.Name, false, result)
		selected := selectClaimsWithinWorkload(valid)
		for _, claim := range selected {
			normalized := claim.NormalizedName()
			if firstWorkload, exists := origins[normalized]; exists && firstWorkload != currentWorkload.Name {
				duplicateKey := normalized + "\x00" + currentWorkload.Name
				if _, reported := reportedDuplicates[duplicateKey]; !reported {
					reportedDuplicates[duplicateKey] = struct{}{}
					result.HostnamesDuplicate++
					r.logger.Warn("duplicate hostname found in multiple workloads",
						slog.String("hostname", claim.Name),
						slog.String("first_workload", firstWorkload),
						slog.String("duplicate_workload", currentWorkload.Name),
					)
				}
			} else if !exists {
				origins[normalized] = currentWorkload.Name
			}
			extracted.Claims = append(extracted.Claims, claim)
			extracted.addUnique(claim)
		}
	}

	fileHostnames, complete := r.sources.DiscoverAllWithStatus(ctx)
	if !complete {
		extracted.Complete = false
	}
	validFiles := r.validateClaims(fileHostnames, "", true, result)
	for _, claim := range selectClaimsWithinWorkload(validFiles) {
		extracted.Claims = append(extracted.Claims, claim)
		extracted.addUnique(claim)
	}

	return extracted
}

func (r *Reconciler) validateClaims(hostnames source.Hostnames, workloadName string, fromFile bool, result *Result) source.Hostnames {
	validation := hostnames.ValidateAll()
	for _, invalid := range validation.Invalid {
		attrs := []any{
			slog.String("hostname", invalid.Hostname.Name),
			slog.String("source", invalid.Hostname.Source),
			slog.String("error", invalid.Error.Error()),
		}
		message := "skipping invalid hostname from workload"
		if fromFile {
			message = "skipping invalid hostname from file"
			attrs = append(attrs, slog.String("router", invalid.Hostname.Router))
		} else {
			attrs = append(attrs, slog.String("workload", workloadName))
		}
		r.logger.Warn(message, attrs...)
		result.HostnamesInvalid++
	}
	return validation.Valid
}

func (e *extractedClaims) addUnique(claim *source.Hostname) {
	normalized := claim.NormalizedName()
	if existing, found := e.Unique[normalized]; found {
		e.Unique[normalized] = mergeDuplicateHostname(existing, claim)
		return
	}
	e.Unique[normalized] = claim
}

// selectClaimsWithinWorkload preserves distinct record members declared by one
// workload while retaining the existing precedence rule between a bare source
// claim and a record-specific claim for the same hostname.
func selectClaimsWithinWorkload(hostnames source.Hostnames) []*source.Hostname {
	selected := make([]*source.Hostname, 0, len(hostnames))
	for i := range hostnames {
		candidate := &hostnames[i]
		var sameName []int
		for index, existing := range selected {
			if existing.NormalizedName() == candidate.NormalizedName() {
				sameName = append(sameName, index)
			}
		}
		if len(sameName) == 0 {
			selected = append(selected, candidate)
			continue
		}
		if candidate.RecordHints == nil {
			mergedIntoHinted := false
			for _, index := range sameName {
				if selected[index].RecordHints != nil {
					selected[index] = mergeDuplicateHostname(selected[index], candidate)
					mergedIntoHinted = true
				}
			}
			if mergedIntoHinted {
				continue
			}
		}

		merged := false
		for _, index := range sameName {
			existing := selected[index]
			switch {
			case sameDeclaredMember(existing, candidate):
				selected[index] = mergeDuplicateHostname(existing, candidate)
				merged = true
			case existing.RecordHints == nil && candidate.RecordHints != nil:
				selected[index] = mergeDuplicateHostname(existing, candidate)
				merged = true
			case existing.RecordHints != nil && candidate.RecordHints == nil:
				selected[index] = mergeDuplicateHostname(existing, candidate)
				merged = true
			}
			if merged {
				break
			}
		}
		if !merged {
			candidate.Metadata = mergeInstanceMetadata(candidate.Metadata, selected[sameName[0]].Metadata)
			selected = append(selected, candidate)
		}
	}
	return selected
}

func sameDeclaredMember(a, b *source.Hostname) bool {
	if a.RecordHints == nil || b.RecordHints == nil {
		return a.RecordHints == nil && b.RecordHints == nil
	}
	if a.RecordHints.Provider != b.RecordHints.Provider ||
		!strings.EqualFold(a.RecordHints.Type, b.RecordHints.Type) {
		return false
	}
	recordA := provider.Record{Type: provider.RecordType(strings.ToUpper(a.RecordHints.Type)), Target: a.RecordHints.Target}
	recordB := provider.Record{Type: provider.RecordType(strings.ToUpper(b.RecordHints.Type)), Target: b.RecordHints.Target}
	if a.RecordHints.SRV != nil {
		recordA.SRV = &provider.SRVData{Priority: a.RecordHints.SRV.Priority, Weight: a.RecordHints.SRV.Weight, Port: a.RecordHints.SRV.Port}
	}
	if b.RecordHints.SRV != nil {
		recordB.SRV = &provider.SRVData{Priority: b.RecordHints.SRV.Priority, Weight: b.RecordHints.SRV.Weight, Port: b.RecordHints.SRV.Port}
	}
	return provider.SameRecordMember(recordA, recordB)
}
