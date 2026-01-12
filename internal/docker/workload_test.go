package docker

import (
	"testing"
)

// TestWorkloadTypeConstants verifies workload type constants are correctly defined.
func TestWorkloadTypeConstants(t *testing.T) {
	tests := []struct {
		wt       WorkloadType
		expected string
	}{
		{WorkloadTypeService, "service"},
		{WorkloadTypeContainer, "container"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if tt.wt.String() != tt.expected {
				t.Errorf("WorkloadType.String() = %q, want %q", tt.wt.String(), tt.expected)
			}
		})
	}
}

// TestWorkloadStruct verifies the Workload struct can hold expected data.
func TestWorkloadStruct(t *testing.T) {
	tests := []struct {
		name     string
		workload Workload
		wantType WorkloadType
	}{
		{
			name: "service workload",
			workload: Workload{
				ID:     "svc-123",
				Name:   "my-service",
				Labels: map[string]string{"env": "prod"},
				Type:   WorkloadTypeService,
			},
			wantType: WorkloadTypeService,
		},
		{
			name: "container workload",
			workload: Workload{
				ID:     "ctr-456",
				Name:   "my-container",
				Labels: map[string]string{"env": "dev"},
				Type:   WorkloadTypeContainer,
			},
			wantType: WorkloadTypeContainer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.workload.Type != tt.wantType {
				t.Errorf("Workload.Type = %s, want %s", tt.workload.Type, tt.wantType)
			}
		})
	}
}

// TestWorkloadString tests the String() method.
func TestWorkloadString(t *testing.T) {
	tests := []struct {
		workload Workload
		expected string
	}{
		{
			workload: Workload{Name: "my-service", Type: WorkloadTypeService},
			expected: "service:my-service",
		},
		{
			workload: Workload{Name: "my-container", Type: WorkloadTypeContainer},
			expected: "container:my-container",
		},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if tt.workload.String() != tt.expected {
				t.Errorf("Workload.String() = %q, want %q", tt.workload.String(), tt.expected)
			}
		})
	}
}

// TestWorkloadIsService tests the IsService() method.
func TestWorkloadIsService(t *testing.T) {
	tests := []struct {
		wt       WorkloadType
		expected bool
	}{
		{WorkloadTypeService, true},
		{WorkloadTypeContainer, false},
	}

	for _, tt := range tests {
		t.Run(tt.wt.String(), func(t *testing.T) {
			w := Workload{Type: tt.wt}
			if w.IsService() != tt.expected {
				t.Errorf("IsService() = %v, want %v", w.IsService(), tt.expected)
			}
		})
	}
}

// TestWorkloadIsContainer tests the IsContainer() method.
func TestWorkloadIsContainer(t *testing.T) {
	tests := []struct {
		wt       WorkloadType
		expected bool
	}{
		{WorkloadTypeService, false},
		{WorkloadTypeContainer, true},
	}

	for _, tt := range tests {
		t.Run(tt.wt.String(), func(t *testing.T) {
			w := Workload{Type: tt.wt}
			if w.IsContainer() != tt.expected {
				t.Errorf("IsContainer() = %v, want %v", w.IsContainer(), tt.expected)
			}
		})
	}
}

// TestWorkloadHasLabel tests the HasLabel() method.
func TestWorkloadHasLabel(t *testing.T) {
	w := Workload{
		Labels: map[string]string{
			"traefik.enable": "true",
			"empty.label":    "",
		},
	}

	tests := []struct {
		label    string
		expected bool
	}{
		{"traefik.enable", true},
		{"empty.label", true}, // exists even if empty
		{"missing.label", false},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			if w.HasLabel(tt.label) != tt.expected {
				t.Errorf("HasLabel(%q) = %v, want %v", tt.label, w.HasLabel(tt.label), tt.expected)
			}
		})
	}
}

// TestWorkloadGetLabel tests the GetLabel() method.
func TestWorkloadGetLabel(t *testing.T) {
	w := Workload{
		Labels: map[string]string{
			"traefik.enable": "true",
			"empty.label":    "",
		},
	}

	tests := []struct {
		label    string
		expected string
	}{
		{"traefik.enable", "true"},
		{"empty.label", ""},
		{"missing.label", ""},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			if w.GetLabel(tt.label) != tt.expected {
				t.Errorf("GetLabel(%q) = %q, want %q", tt.label, w.GetLabel(tt.label), tt.expected)
			}
		})
	}
}

// TestWorkloadGetLabelOr tests the GetLabelOr() method.
func TestWorkloadGetLabelOr(t *testing.T) {
	w := Workload{
		Labels: map[string]string{
			"traefik.enable": "true",
			"empty.label":    "",
		},
	}

	tests := []struct {
		label        string
		defaultValue string
		expected     string
	}{
		{"traefik.enable", "default", "true"},
		{"empty.label", "default", ""},          // empty string is still a valid value
		{"missing.label", "default", "default"}, // uses default when missing
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			result := w.GetLabelOr(tt.label, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("GetLabelOr(%q, %q) = %q, want %q",
					tt.label, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

// TestWorkloadsIDs tests the IDs() method.
func TestWorkloadsIDs(t *testing.T) {
	ws := Workloads{
		{ID: "id1", Name: "name1"},
		{ID: "id2", Name: "name2"},
		{ID: "id3", Name: "name3"},
	}

	ids := ws.IDs()
	expected := []string{"id1", "id2", "id3"}

	if len(ids) != len(expected) {
		t.Fatalf("IDs() returned %d elements, want %d", len(ids), len(expected))
	}

	for i, id := range ids {
		if id != expected[i] {
			t.Errorf("IDs()[%d] = %q, want %q", i, id, expected[i])
		}
	}
}

// TestWorkloadsNames tests the Names() method.
func TestWorkloadsNames(t *testing.T) {
	ws := Workloads{
		{ID: "id1", Name: "alpha"},
		{ID: "id2", Name: "beta"},
		{ID: "id3", Name: "gamma"},
	}

	names := ws.Names()
	expected := []string{"alpha", "beta", "gamma"}

	if len(names) != len(expected) {
		t.Fatalf("Names() returned %d elements, want %d", len(names), len(expected))
	}

	for i, name := range names {
		if name != expected[i] {
			t.Errorf("Names()[%d] = %q, want %q", i, name, expected[i])
		}
	}
}

// TestWorkloadsFilter tests the Filter() method.
func TestWorkloadsFilter(t *testing.T) {
	ws := Workloads{
		{ID: "1", Name: "svc1", Type: WorkloadTypeService},
		{ID: "2", Name: "ctr1", Type: WorkloadTypeContainer},
		{ID: "3", Name: "svc2", Type: WorkloadTypeService},
		{ID: "4", Name: "ctr2", Type: WorkloadTypeContainer},
	}

	services := ws.Filter(func(w Workload) bool {
		return w.IsService()
	})

	if len(services) != 2 {
		t.Errorf("Filter for services returned %d elements, want 2", len(services))
	}

	for _, w := range services {
		if !w.IsService() {
			t.Errorf("Filter returned non-service: %s", w.Name)
		}
	}
}

// TestWorkloadsWithLabel tests the WithLabel() method.
func TestWorkloadsWithLabel(t *testing.T) {
	ws := Workloads{
		{ID: "1", Name: "w1", Labels: map[string]string{"traefik.enable": "true"}},
		{ID: "2", Name: "w2", Labels: map[string]string{"other": "value"}},
		{ID: "3", Name: "w3", Labels: map[string]string{"traefik.enable": "false"}},
		{ID: "4", Name: "w4", Labels: map[string]string{}},
	}

	result := ws.WithLabel("traefik.enable")

	if len(result) != 2 {
		t.Errorf("WithLabel returned %d elements, want 2", len(result))
	}

	for _, w := range result {
		if !w.HasLabel("traefik.enable") {
			t.Errorf("WithLabel returned workload without label: %s", w.Name)
		}
	}
}

// TestWorkloadsWithLabelValue tests the WithLabelValue() method.
func TestWorkloadsWithLabelValue(t *testing.T) {
	ws := Workloads{
		{ID: "1", Name: "w1", Labels: map[string]string{"traefik.enable": "true"}},
		{ID: "2", Name: "w2", Labels: map[string]string{"traefik.enable": "false"}},
		{ID: "3", Name: "w3", Labels: map[string]string{"traefik.enable": "true"}},
		{ID: "4", Name: "w4", Labels: map[string]string{}},
	}

	result := ws.WithLabelValue("traefik.enable", "true")

	if len(result) != 2 {
		t.Errorf("WithLabelValue returned %d elements, want 2", len(result))
	}

	for _, w := range result {
		if w.GetLabel("traefik.enable") != "true" {
			t.Errorf("WithLabelValue returned workload with wrong value: %s", w.Name)
		}
	}
}

// TestWorkloadsServices tests the Services() method.
func TestWorkloadsServices(t *testing.T) {
	ws := Workloads{
		{ID: "1", Name: "svc1", Type: WorkloadTypeService},
		{ID: "2", Name: "ctr1", Type: WorkloadTypeContainer},
		{ID: "3", Name: "svc2", Type: WorkloadTypeService},
	}

	result := ws.Services()

	if len(result) != 2 {
		t.Errorf("Services() returned %d elements, want 2", len(result))
	}

	for _, w := range result {
		if !w.IsService() {
			t.Errorf("Services() returned non-service: %s", w.Name)
		}
	}
}

// TestWorkloadsContainers tests the Containers() method.
func TestWorkloadsContainers(t *testing.T) {
	ws := Workloads{
		{ID: "1", Name: "svc1", Type: WorkloadTypeService},
		{ID: "2", Name: "ctr1", Type: WorkloadTypeContainer},
		{ID: "3", Name: "ctr2", Type: WorkloadTypeContainer},
	}

	result := ws.Containers()

	if len(result) != 2 {
		t.Errorf("Containers() returned %d elements, want 2", len(result))
	}

	for _, w := range result {
		if !w.IsContainer() {
			t.Errorf("Containers() returned non-container: %s", w.Name)
		}
	}
}

// TestWorkloadsEmpty tests methods with empty slices.
func TestWorkloadsEmpty(t *testing.T) {
	ws := Workloads{}

	if len(ws.IDs()) != 0 {
		t.Error("IDs() on empty slice should return empty slice")
	}

	if len(ws.Names()) != 0 {
		t.Error("Names() on empty slice should return empty slice")
	}

	if len(ws.Filter(func(Workload) bool { return true })) != 0 {
		t.Error("Filter() on empty slice should return empty slice")
	}

	if len(ws.Services()) != 0 {
		t.Error("Services() on empty slice should return empty slice")
	}

	if len(ws.Containers()) != 0 {
		t.Error("Containers() on empty slice should return empty slice")
	}
}

// TestWorkloadNilLabels tests behavior with nil labels map.
func TestWorkloadNilLabels(t *testing.T) {
	w := Workload{
		ID:     "test",
		Labels: nil,
	}

	if w.HasLabel("any") {
		t.Error("HasLabel should return false for nil labels")
	}

	if w.GetLabel("any") != "" {
		t.Error("GetLabel should return empty string for nil labels")
	}

	if w.GetLabelOr("any", "default") != "default" {
		t.Error("GetLabelOr should return default for nil labels")
	}
}
