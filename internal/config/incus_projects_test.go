package config

import (
	"reflect"
	"testing"
)

func TestIncusProjectModes(t *testing.T) {
	tests := []struct {
		name            string
		project         string
		projects        []string
		allProjectsFlag bool
		wantAll         bool
		wantProjects    []string
	}{
		{
			name:         "single default",
			project:      "",
			wantAll:      false,
			wantProjects: []string{},
		},
		{
			name:         "single named",
			project:      "prod",
			wantAll:      false,
			wantProjects: []string{},
		},
		{
			name:         "explicit list",
			projects:     []string{"a", "b", "c"},
			wantAll:      false,
			wantProjects: []string{"a", "b", "c"},
		},
		{
			name:            "all-projects flag",
			allProjectsFlag: true,
			wantAll:         true,
			wantProjects:    nil,
		},
		{
			name:         "wildcard star in list means all",
			projects:     []string{"a", "*"},
			wantAll:      true,
			wantProjects: nil,
		},
		{
			name:         "wildcard all keyword means all",
			projects:     []string{"all"},
			wantAll:      true,
			wantProjects: nil,
		},
		{
			name:            "flag wins over list",
			projects:        []string{"a", "b"},
			allProjectsFlag: true,
			wantAll:         true,
			wantProjects:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{Global: &GlobalConfig{
				IncusProject:     tt.project,
				IncusProjects:    tt.projects,
				IncusAllProjects: tt.allProjectsFlag,
			}}
			if got := c.IncusAllProjects(); got != tt.wantAll {
				t.Errorf("IncusAllProjects() = %v, want %v", got, tt.wantAll)
			}
			if got := c.IncusProjects(); !reflect.DeepEqual(got, tt.wantProjects) {
				t.Errorf("IncusProjects() = %v, want %v", got, tt.wantProjects)
			}
			if got := c.IncusProject(); got != tt.project {
				t.Errorf("IncusProject() = %q, want %q", got, tt.project)
			}
		})
	}
}

func TestSplitCommaList(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ,, c ", []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		if got := splitCommaList(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("splitCommaList(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
