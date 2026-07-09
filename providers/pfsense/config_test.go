package pfsense

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
				URL:             "https://pfsense.test",
				APIKey:          "k",
				Engine:          EngineUnbound,
				ReconfigureMode: ReconfigureModePerWrite,
			},
		},
		{
			name: "valid dnsmasq never",
			cfg: Config{
				URL:             "http://pfsense.test",
				APIKey:          "k",
				Engine:          EngineDnsmasq,
				ReconfigureMode: ReconfigureModeNever,
			},
		},
		{
			name:    "missing url",
			cfg:     Config{APIKey: "k", Engine: EngineUnbound, ReconfigureMode: ReconfigureModePerWrite},
			wantErr: "URL is required",
		},
		{
			name:    "bad scheme",
			cfg:     Config{URL: "ftp://pfsense.test", APIKey: "k", Engine: EngineUnbound, ReconfigureMode: ReconfigureModePerWrite},
			wantErr: "URL must start with",
		},
		{
			name:    "embedded credentials",
			cfg:     Config{URL: "https://user:pass@pfsense.test", APIKey: "k", Engine: EngineUnbound, ReconfigureMode: ReconfigureModePerWrite},
			wantErr: "must not contain embedded credentials",
		},
		{
			name:    "missing api key",
			cfg:     Config{URL: "https://pfsense.test", Engine: EngineUnbound, ReconfigureMode: ReconfigureModePerWrite},
			wantErr: "API_KEY is required",
		},
		{
			name:    "bad engine",
			cfg:     Config{URL: "https://pfsense.test", APIKey: "k", Engine: "bind", ReconfigureMode: ReconfigureModePerWrite},
			wantErr: "ENGINE must be",
		},
		{
			name:    "bad reconfigure mode",
			cfg:     Config{URL: "https://pfsense.test", APIKey: "k", Engine: EngineUnbound, ReconfigureMode: "sometimes"},
			wantErr: "RECONFIGURE_MODE must be",
		},
		{
			name:    "negative ttl",
			cfg:     Config{URL: "https://pfsense.test", APIKey: "k", Engine: EngineUnbound, ReconfigureMode: ReconfigureModePerWrite, TTL: -1},
			wantErr: "TTL must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected success, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestLoadConfigFromMapDefaults(t *testing.T) {
	cfg, err := LoadConfigFromMap("pf", map[string]string{
		"URL":     "https://pfsense.test",
		"API_KEY": "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Engine != EngineUnbound {
		t.Errorf("Engine default = %q, want %q", cfg.Engine, EngineUnbound)
	}
	if cfg.ReconfigureMode != ReconfigureModePerWrite {
		t.Errorf("ReconfigureMode default = %q, want %q", cfg.ReconfigureMode, ReconfigureModePerWrite)
	}
	if cfg.TTL != DefaultTTL {
		t.Errorf("TTL default = %d, want %d", cfg.TTL, DefaultTTL)
	}
}

func TestLoadConfigFromMapInvalidTTL(t *testing.T) {
	_, err := LoadConfigFromMap("pf", map[string]string{
		"URL":     "https://pfsense.test",
		"API_KEY": "secret",
		"TTL":     "not-a-number",
	})
	if err == nil {
		t.Fatal("expected error for invalid TTL")
	}
}

func TestParsers(t *testing.T) {
	if got := engineFromString(""); got != EngineUnbound {
		t.Errorf("engineFromString(empty) = %q, want %q", got, EngineUnbound)
	}
	if got := engineFromString("DNSMASQ"); got != EngineDnsmasq {
		t.Errorf("engineFromString(DNSMASQ) = %q, want %q", got, EngineDnsmasq)
	}
	if got := reconfigureModeFromString(""); got != ReconfigureModePerWrite {
		t.Errorf("reconfigureModeFromString(empty) = %q, want %q", got, ReconfigureModePerWrite)
	}
	if got := reconfigureModeFromString("never"); got != ReconfigureModeNever {
		t.Errorf("reconfigureModeFromString(never) = %q, want %q", got, ReconfigureModeNever)
	}
}
