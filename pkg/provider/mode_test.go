package provider

import "testing"

func TestParseOperationalMode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    OperationalMode
		wantErr bool
	}{
		{
			name:  "empty defaults to managed",
			input: "",
			want:  ModeManaged,
		},
		{
			name:  "managed lowercase",
			input: "managed",
			want:  ModeManaged,
		},
		{
			name:  "managed uppercase",
			input: "MANAGED",
			want:  ModeManaged,
		},
		{
			name:  "managed mixed case",
			input: "Managed",
			want:  ModeManaged,
		},
		{
			name:  "authoritative",
			input: "authoritative",
			want:  ModeAuthoritative,
		},
		{
			name:  "authoritative uppercase",
			input: "AUTHORITATIVE",
			want:  ModeAuthoritative,
		},
		{
			name:  "additive",
			input: "additive",
			want:  ModeAdditive,
		},
		{
			name:  "additive with whitespace",
			input: "  additive  ",
			want:  ModeAdditive,
		},
		{
			name:    "invalid mode",
			input:   "unknown",
			wantErr: true,
		},
		{
			name:    "typo",
			input:   "managd",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseOperationalMode(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseOperationalMode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseOperationalMode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOperationalMode_IsValid(t *testing.T) {
	tests := []struct {
		mode OperationalMode
		want bool
	}{
		{ModeManaged, true},
		{ModeAuthoritative, true},
		{ModeAdditive, true},
		{OperationalMode("unknown"), false},
		{OperationalMode(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOperationalMode_AllowsDelete(t *testing.T) {
	tests := []struct {
		mode OperationalMode
		want bool
	}{
		{ModeManaged, true},
		{ModeAuthoritative, true},
		{ModeAdditive, false}, // Never deletes
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.AllowsDelete(); got != tt.want {
				t.Errorf("AllowsDelete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOperationalMode_RequiresOwnership(t *testing.T) {
	tests := []struct {
		mode OperationalMode
		want bool
	}{
		{ModeManaged, true},         // Only deletes owned records
		{ModeAuthoritative, false},  // Deletes any in-scope record
		{ModeAdditive, false},       // Doesn't matter, never deletes
		{OperationalMode(""), true}, // Default treated as managed
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.RequiresOwnership(); got != tt.want {
				t.Errorf("RequiresOwnership() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOperationalMode_String(t *testing.T) {
	if ModeManaged.String() != "managed" {
		t.Errorf("ModeManaged.String() = %q, want %q", ModeManaged.String(), "managed")
	}
	if ModeAuthoritative.String() != "authoritative" {
		t.Errorf("ModeAuthoritative.String() = %q, want %q", ModeAuthoritative.String(), "authoritative")
	}
	if ModeAdditive.String() != "additive" {
		t.Errorf("ModeAdditive.String() = %q, want %q", ModeAdditive.String(), "additive")
	}
}
