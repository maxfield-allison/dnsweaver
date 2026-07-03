package opnsense

import (
	"strings"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string // empty = expect success
	}{
		{
			name: "valid unbound",
			cfg: Config{
				URL:             "https://opnsense.test",
				APIKey:          "k",
				APISecret:       "s",
				Engine:          EngineUnbound,
				ReconfigureMode: ReconfigureModePerWrite,
			},
		},
		{
			name: "valid dnsmasq",
			cfg: Config{
				URL:             "http://opnsense.test",
				APIKey:          "k",
				APISecret:       "s",
				Engine:          EngineDnsmasq,
				ReconfigureMode: ReconfigureModeNever,
			},
		},
		{
			name:    "missing url",
			cfg:     Config{APIKey: "k", APISecret: "s", Engine: EngineUnbound, ReconfigureMode: ReconfigureModePerWrite},
			wantErr: "URL is required",
		},
		{
			name:    "bad scheme",
			cfg:     Config{URL: "ftp://x", APIKey: "k", APISecret: "s", Engine: EngineUnbound, ReconfigureMode: ReconfigureModePerWrite},
			wantErr: "URL must start with http",
		},
		{
			name:    "embedded credentials rejected",
			cfg:     Config{URL: "https://u:p@opnsense.test", APIKey: "k", APISecret: "s", Engine: EngineUnbound, ReconfigureMode: ReconfigureModePerWrite},
			wantErr: "embedded credentials",
		},
		{
			name:    "missing api key",
			cfg:     Config{URL: "https://x", APISecret: "s", Engine: EngineUnbound, ReconfigureMode: ReconfigureModePerWrite},
			wantErr: "API_KEY is required",
		},
		{
			name:    "missing api secret",
			cfg:     Config{URL: "https://x", APIKey: "k", Engine: EngineUnbound, ReconfigureMode: ReconfigureModePerWrite},
			wantErr: "API_SECRET is required",
		},
		{
			name:    "unknown engine",
			cfg:     Config{URL: "https://x", APIKey: "k", APISecret: "s", Engine: Engine("bind"), ReconfigureMode: ReconfigureModePerWrite},
			wantErr: "ENGINE must be",
		},
		{
			name:    "unknown reconfigure mode",
			cfg:     Config{URL: "https://x", APIKey: "k", APISecret: "s", Engine: EngineUnbound, ReconfigureMode: ReconfigureMode("cron")},
			wantErr: "RECONFIGURE_MODE",
		},
		{
			name:    "negative ttl",
			cfg:     Config{URL: "https://x", APIKey: "k", APISecret: "s", Engine: EngineUnbound, ReconfigureMode: ReconfigureModePerWrite, TTL: -1},
			wantErr: "TTL must be non-negative",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestLoadConfigFromMap_Defaults(t *testing.T) {
	m := map[string]string{
		"URL":        "https://opnsense.test",
		"API_KEY":    "k",
		"API_SECRET": "s",
	}
	cfg, err := LoadConfigFromMap("opns", m)
	if err != nil {
		t.Fatalf("LoadConfigFromMap error: %v", err)
	}
	if cfg.Engine != EngineUnbound {
		t.Errorf("default engine = %q, want %q", cfg.Engine, EngineUnbound)
	}
	if cfg.ReconfigureMode != ReconfigureModePerWrite {
		t.Errorf("default reconfigure mode = %q, want %q", cfg.ReconfigureMode, ReconfigureModePerWrite)
	}
	if cfg.TTL != DefaultTTL {
		t.Errorf("default TTL = %d, want %d", cfg.TTL, DefaultTTL)
	}
}

func TestLoadConfigFromMap_AllFields(t *testing.T) {
	m := map[string]string{
		"URL":              "https://opnsense.test",
		"API_KEY":          "abc",
		"API_SECRET":       "xyz",
		"ENGINE":           "dnsmasq",
		"ZONE":             "example.com",
		"TTL":              "600",
		"RECONFIGURE_MODE": "never",
	}
	cfg, err := LoadConfigFromMap("opns", m)
	if err != nil {
		t.Fatalf("LoadConfigFromMap error: %v", err)
	}
	if cfg.Engine != EngineDnsmasq {
		t.Errorf("engine = %q, want %q", cfg.Engine, EngineDnsmasq)
	}
	if cfg.Zone != "example.com" {
		t.Errorf("zone = %q", cfg.Zone)
	}
	if cfg.TTL != 600 {
		t.Errorf("ttl = %d", cfg.TTL)
	}
	if cfg.ReconfigureMode != ReconfigureModeNever {
		t.Errorf("mode = %q", cfg.ReconfigureMode)
	}
}

func TestLoadConfigFromMap_InvalidTTL(t *testing.T) {
	m := map[string]string{
		"URL":        "https://opnsense.test",
		"API_KEY":    "k",
		"API_SECRET": "s",
		"TTL":        "abc",
	}
	_, err := LoadConfigFromMap("opns", m)
	if err == nil || !strings.Contains(err.Error(), "invalid TTL") {
		t.Fatalf("expected invalid TTL error, got %v", err)
	}
}

func TestLoadConfigFromMap_InvalidEngineSurfaces(t *testing.T) {
	m := map[string]string{
		"URL":        "https://opnsense.test",
		"API_KEY":    "k",
		"API_SECRET": "s",
		"ENGINE":     "powerdns",
	}
	_, err := LoadConfigFromMap("opns", m)
	if err == nil || !strings.Contains(err.Error(), "ENGINE must be") {
		t.Fatalf("expected engine validation error, got %v", err)
	}
}

func TestEngineFromString(t *testing.T) {
	cases := []struct {
		in   string
		want Engine
	}{
		{"", EngineUnbound},
		{"unbound", EngineUnbound},
		{"UNBOUND", EngineUnbound},
		{" dnsmasq", EngineDnsmasq}, // leading whitespace must be trimmed
		{"dnsmasq", EngineDnsmasq},
	}
	for _, tc := range cases {
		if got := engineFromString(tc.in); got != tc.want {
			t.Errorf("engineFromString(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// Unknown values surface verbatim so Validate() reports them.
	if got := engineFromString("bind"); got != Engine("bind") {
		t.Errorf("engineFromString(bind) = %q, want %q", got, Engine("bind"))
	}
}
