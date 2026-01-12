// Package reconciler implements the core logic for comparing desired DNS state
// (from sources) with actual DNS state (from providers) and applying changes.
package reconciler

import (
	"fmt"
	"strings"
	"time"
)

// ActionType represents the type of reconciliation action.
type ActionType string

const (
	// ActionCreate indicates a record will be/was created.
	ActionCreate ActionType = "create"
	// ActionUpdate indicates a record will be/was updated (target changed).
	ActionUpdate ActionType = "update"
	// ActionDelete indicates a record will be/was deleted.
	ActionDelete ActionType = "delete"
	// ActionSkip indicates a record was skipped (already exists/not found).
	ActionSkip ActionType = "skip"
)

// ActionStatus represents the outcome of an action.
type ActionStatus string

const (
	// StatusPending indicates the action has not been executed yet.
	StatusPending ActionStatus = "pending"
	// StatusSuccess indicates the action completed successfully.
	StatusSuccess ActionStatus = "success"
	// StatusFailed indicates the action failed.
	StatusFailed ActionStatus = "failed"
	// StatusSkipped indicates the action was skipped (dry-run or already in desired state).
	StatusSkipped ActionStatus = "skipped"
)

// Action represents a single reconciliation action on a DNS record.
type Action struct {
	// Type is the action type (create, delete, skip).
	Type ActionType

	// Status is the outcome of the action.
	Status ActionStatus

	// Provider is the provider instance name that handles this record.
	Provider string

	// Hostname is the DNS hostname being affected.
	Hostname string

	// RecordType is "A" or "CNAME".
	RecordType string

	// Target is the record value (IP or hostname).
	Target string

	// Error contains the error message if Status is StatusFailed.
	Error string

	// DryRun indicates this action was not actually executed.
	DryRun bool
}

// String returns a human-readable representation of the action.
func (a Action) String() string {
	status := string(a.Status)
	if a.DryRun && a.Status == StatusSuccess {
		status = "dry-run"
	}

	if a.Error != "" {
		return fmt.Sprintf("[%s] %s %s -> %s (%s %s): %s",
			status, a.Type, a.Hostname, a.Target, a.RecordType, a.Provider, a.Error)
	}

	return fmt.Sprintf("[%s] %s %s -> %s (%s %s)",
		status, a.Type, a.Hostname, a.Target, a.RecordType, a.Provider)
}

// Result holds the complete result of a reconciliation run.
type Result struct {
	// StartTime is when reconciliation started.
	StartTime time.Time

	// EndTime is when reconciliation completed.
	EndTime time.Time

	// WorkloadsScanned is the number of Docker workloads examined.
	WorkloadsScanned int

	// HostnamesDiscovered is the number of unique valid hostnames found in labels.
	HostnamesDiscovered int

	// HostnamesInvalid is the number of hostnames that failed validation.
	HostnamesInvalid int

	// HostnamesDuplicate is the number of hostnames that appeared in multiple workloads.
	// Only the first occurrence is processed; duplicates are logged and skipped.
	HostnamesDuplicate int

	// Actions contains all reconciliation actions taken (or planned in dry-run).
	Actions []Action

	// DryRun indicates if this was a dry-run (no changes applied).
	DryRun bool
}

// NewResult creates a new Result with the start time set to now.
func NewResult(dryRun bool) *Result {
	return &Result{
		StartTime: time.Now(),
		Actions:   make([]Action, 0),
		DryRun:    dryRun,
	}
}

// Complete marks the result as complete with the end time set to now.
func (r *Result) Complete() {
	r.EndTime = time.Now()
}

// Duration returns the total reconciliation duration.
func (r *Result) Duration() time.Duration {
	if r.EndTime.IsZero() {
		return time.Since(r.StartTime)
	}
	return r.EndTime.Sub(r.StartTime)
}

// AddAction adds an action to the result.
func (r *Result) AddAction(action Action) {
	action.DryRun = r.DryRun
	r.Actions = append(r.Actions, action)
}

// Created returns all successful create actions.
func (r *Result) Created() []Action {
	return r.filterActions(ActionCreate, StatusSuccess)
}

// Updated returns all successful update actions.
func (r *Result) Updated() []Action {
	return r.filterActions(ActionUpdate, StatusSuccess)
}

// Deleted returns all successful delete actions.
func (r *Result) Deleted() []Action {
	return r.filterActions(ActionDelete, StatusSuccess)
}

// Failed returns all failed actions.
func (r *Result) Failed() []Action {
	var failed []Action
	for _, a := range r.Actions {
		if a.Status == StatusFailed {
			failed = append(failed, a)
		}
	}
	return failed
}

// Skipped returns all skipped actions.
func (r *Result) Skipped() []Action {
	var skipped []Action
	for _, a := range r.Actions {
		if a.Status == StatusSkipped || a.Type == ActionSkip {
			skipped = append(skipped, a)
		}
	}
	return skipped
}

func (r *Result) filterActions(actionType ActionType, status ActionStatus) []Action {
	var filtered []Action
	for _, a := range r.Actions {
		if a.Type == actionType && a.Status == status {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

// CreatedCount returns the number of records created (or would be in dry-run).
func (r *Result) CreatedCount() int {
	return len(r.Created())
}

// UpdatedCount returns the number of records updated (target changed).
func (r *Result) UpdatedCount() int {
	return len(r.Updated())
}

// DeletedCount returns the number of records deleted (or would be in dry-run).
func (r *Result) DeletedCount() int {
	return len(r.Deleted())
}

// FailedCount returns the number of failed actions.
func (r *Result) FailedCount() int {
	return len(r.Failed())
}

// HasErrors returns true if any actions failed.
func (r *Result) HasErrors() bool {
	return r.FailedCount() > 0
}

// Summary returns a human-readable summary of the reconciliation.
func (r *Result) Summary() string {
	var sb strings.Builder

	mode := "applied"
	if r.DryRun {
		mode = "dry-run"
	}

	fmt.Fprintf(&sb, "Reconciliation complete (%s) in %s\n", mode, r.Duration().Round(time.Millisecond))
	fmt.Fprintf(&sb, "  Workloads scanned: %d\n", r.WorkloadsScanned)
	fmt.Fprintf(&sb, "  Hostnames discovered: %d\n", r.HostnamesDiscovered)
	fmt.Fprintf(&sb, "  Records created: %d\n", r.CreatedCount())
	fmt.Fprintf(&sb, "  Records updated: %d\n", r.UpdatedCount())
	fmt.Fprintf(&sb, "  Records deleted: %d\n", r.DeletedCount())
	fmt.Fprintf(&sb, "  Skipped: %d\n", len(r.Skipped()))

	if r.HasErrors() {
		fmt.Fprintf(&sb, "  Failed: %d\n", r.FailedCount())
		for _, a := range r.Failed() {
			fmt.Fprintf(&sb, "    - %s\n", a.String())
		}
	}

	return sb.String()
}
