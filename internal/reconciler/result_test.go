package reconciler

import (
	"testing"
	"time"
)

func TestActionType_String(t *testing.T) {
	tests := []struct {
		action ActionType
		want   string
	}{
		{ActionCreate, "create"},
		{ActionDelete, "delete"},
		{ActionSkip, "skip"},
	}

	for _, tt := range tests {
		if got := string(tt.action); got != tt.want {
			t.Errorf("ActionType %v = %q, want %q", tt.action, got, tt.want)
		}
	}
}

func TestActionStatus_String(t *testing.T) {
	tests := []struct {
		status ActionStatus
		want   string
	}{
		{StatusPending, "pending"},
		{StatusSuccess, "success"},
		{StatusFailed, "failed"},
		{StatusSkipped, "skipped"},
	}

	for _, tt := range tests {
		if got := string(tt.status); got != tt.want {
			t.Errorf("ActionStatus %v = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestAction_String(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		want   string
	}{
		{
			name: "successful create",
			action: Action{
				Type:       ActionCreate,
				Status:     StatusSuccess,
				Provider:   "internal-dns",
				Hostname:   "app.example.com",
				RecordType: "A",
				Target:     "10.0.0.1",
			},
			want: "[success] create app.example.com -> 10.0.0.1 (A internal-dns)",
		},
		{
			name: "failed delete",
			action: Action{
				Type:       ActionDelete,
				Status:     StatusFailed,
				Provider:   "cloudflare",
				Hostname:   "web.example.com",
				RecordType: "CNAME",
				Target:     "origin.example.com",
				Error:      "connection refused",
			},
			want: "[failed] delete web.example.com -> origin.example.com (CNAME cloudflare): connection refused",
		},
		{
			name: "dry-run create",
			action: Action{
				Type:       ActionCreate,
				Status:     StatusSuccess,
				Provider:   "internal-dns",
				Hostname:   "app.example.com",
				RecordType: "A",
				Target:     "10.0.0.1",
				DryRun:     true,
			},
			want: "[dry-run] create app.example.com -> 10.0.0.1 (A internal-dns)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.action.String(); got != tt.want {
				t.Errorf("Action.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewResult(t *testing.T) {
	result := NewResult(false)

	if result.DryRun {
		t.Error("NewResult(false) should have DryRun=false")
	}
	if result.StartTime.IsZero() {
		t.Error("NewResult should set StartTime")
	}
	if result.Actions == nil {
		t.Error("NewResult should initialize Actions slice")
	}

	dryRunResult := NewResult(true)
	if !dryRunResult.DryRun {
		t.Error("NewResult(true) should have DryRun=true")
	}
}

func TestResult_Complete(t *testing.T) {
	result := NewResult(false)
	time.Sleep(10 * time.Millisecond)
	result.Complete()

	if result.EndTime.IsZero() {
		t.Error("Complete() should set EndTime")
	}
	if result.Duration() < 10*time.Millisecond {
		t.Errorf("Duration() = %v, want >= 10ms", result.Duration())
	}
}

func TestResult_Duration_Incomplete(t *testing.T) {
	result := NewResult(false)
	time.Sleep(10 * time.Millisecond)

	// Duration should work even without Complete()
	if result.Duration() < 10*time.Millisecond {
		t.Errorf("Duration() on incomplete result = %v, want >= 10ms", result.Duration())
	}
}

func TestResult_AddAction(t *testing.T) {
	result := NewResult(true) // dry-run

	action := Action{
		Type:     ActionCreate,
		Status:   StatusSuccess,
		Hostname: "app.example.com",
	}

	result.AddAction(action)

	if len(result.Actions) != 1 {
		t.Fatalf("AddAction: got %d actions, want 1", len(result.Actions))
	}

	// DryRun flag should be propagated to the action
	if !result.Actions[0].DryRun {
		t.Error("AddAction should set DryRun flag on action")
	}
}

func TestResult_Filtering(t *testing.T) {
	result := NewResult(false)

	// Add various actions
	result.AddAction(Action{Type: ActionCreate, Status: StatusSuccess, Hostname: "app1.example.com"})
	result.AddAction(Action{Type: ActionCreate, Status: StatusSuccess, Hostname: "app2.example.com"})
	result.AddAction(Action{Type: ActionCreate, Status: StatusFailed, Hostname: "app3.example.com", Error: "error"})
	result.AddAction(Action{Type: ActionDelete, Status: StatusSuccess, Hostname: "old.example.com"})
	result.AddAction(Action{Type: ActionSkip, Status: StatusSkipped, Hostname: "skip.example.com"})

	tests := []struct {
		name string
		fn   func() []Action
		want int
	}{
		{"Created", result.Created, 2},
		{"Deleted", result.Deleted, 1},
		{"Failed", result.Failed, 1},
		{"Skipped", result.Skipped, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(tt.fn()); got != tt.want {
				t.Errorf("%s() returned %d actions, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func TestResult_Counts(t *testing.T) {
	result := NewResult(false)

	result.AddAction(Action{Type: ActionCreate, Status: StatusSuccess})
	result.AddAction(Action{Type: ActionCreate, Status: StatusSuccess})
	result.AddAction(Action{Type: ActionDelete, Status: StatusSuccess})
	result.AddAction(Action{Type: ActionCreate, Status: StatusFailed, Error: "error"})

	if got := result.CreatedCount(); got != 2 {
		t.Errorf("CreatedCount() = %d, want 2", got)
	}
	if got := result.DeletedCount(); got != 1 {
		t.Errorf("DeletedCount() = %d, want 1", got)
	}
	if got := result.FailedCount(); got != 1 {
		t.Errorf("FailedCount() = %d, want 1", got)
	}
}

func TestResult_HasErrors(t *testing.T) {
	result := NewResult(false)

	if result.HasErrors() {
		t.Error("Empty result should not have errors")
	}

	result.AddAction(Action{Type: ActionCreate, Status: StatusSuccess})
	if result.HasErrors() {
		t.Error("Result with only successes should not have errors")
	}

	result.AddAction(Action{Type: ActionCreate, Status: StatusFailed, Error: "error"})
	if !result.HasErrors() {
		t.Error("Result with failures should have errors")
	}
}

func TestResult_Summary(t *testing.T) {
	result := NewResult(false)
	result.WorkloadsScanned = 10
	result.HostnamesDiscovered = 5

	result.AddAction(Action{Type: ActionCreate, Status: StatusSuccess, Hostname: "app.example.com"})
	result.AddAction(Action{Type: ActionDelete, Status: StatusSuccess, Hostname: "old.example.com"})
	result.AddAction(Action{Type: ActionSkip, Status: StatusSkipped, Hostname: "skip.example.com"})
	result.Complete()

	summary := result.Summary()

	// Check key information is present
	if !contains(summary, "applied") {
		t.Error("Summary should mention 'applied' mode")
	}
	if !contains(summary, "Workloads scanned: 10") {
		t.Error("Summary should show workloads scanned")
	}
	if !contains(summary, "Hostnames discovered: 5") {
		t.Error("Summary should show hostnames discovered")
	}
	if !contains(summary, "Records created: 1") {
		t.Error("Summary should show records created")
	}
	if !contains(summary, "Records deleted: 1") {
		t.Error("Summary should show records deleted")
	}
}

func TestResult_Summary_DryRun(t *testing.T) {
	result := NewResult(true)
	result.Complete()

	summary := result.Summary()

	if !contains(summary, "dry-run") {
		t.Error("Dry-run summary should mention 'dry-run'")
	}
}

func TestResult_Summary_WithErrors(t *testing.T) {
	result := NewResult(false)
	result.AddAction(Action{
		Type:     ActionCreate,
		Status:   StatusFailed,
		Hostname: "fail.example.com",
		Provider: "test-provider",
		Error:    "connection refused",
	})
	result.Complete()

	summary := result.Summary()

	if !contains(summary, "Failed: 1") {
		t.Error("Summary should show failed count")
	}
	if !contains(summary, "fail.example.com") {
		t.Error("Summary should list failed hostnames")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
