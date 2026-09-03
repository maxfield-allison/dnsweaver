package config

import (
	"os"
	"strings"
	"testing"
)

func boolPointer(value bool) *bool { return &value }

func TestLoadInstanceConfig_AdoptionPolicy(t *testing.T) {
	const instanceName = "adoption-test"
	prefix := envPrefix(instanceName)

	tests := []struct {
		name        string
		adopt       string
		allow       string
		wantAdopt   *bool
		wantAllow   bool
		wantErrPart string
	}{
		{name: "inherits global when unset"},
		{name: "instance enables adoption", adopt: "true", wantAdopt: boolPointer(true)},
		{name: "instance disables adoption", adopt: "false", wantAdopt: boolPointer(false)},
		{name: "enables workload override gate", allow: "true", wantAllow: true},
		{name: "invalid adoption fails", adopt: "sometimes", wantErrPart: "ADOPT_EXISTING"},
		{name: "invalid gate fails", allow: "sometimes", wantErrPart: "ADOPT_EXISTING_ALLOW_OVERRIDES"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearInstanceEnv(t, instanceName)
			defer clearInstanceEnv(t, instanceName)
			os.Setenv(prefix+"TYPE", "technitium")
			os.Setenv(prefix+"TARGET", "10.0.0.1")
			os.Setenv(prefix+"DOMAINS", "*.example.com")
			if tt.adopt != "" {
				os.Setenv(prefix+"ADOPT_EXISTING", tt.adopt)
			}
			if tt.allow != "" {
				os.Setenv(prefix+"ADOPT_EXISTING_ALLOW_OVERRIDES", tt.allow)
			}

			cfg, errs := loadInstanceConfig(instanceName, 300)
			if tt.wantErrPart != "" {
				if len(errs) == 0 || !strings.Contains(errs[0].Error(), tt.wantErrPart) {
					t.Fatalf("errors = %v, want one containing %q", errs, tt.wantErrPart)
				}
				return
			}
			if len(errs) != 0 {
				t.Fatalf("unexpected errors: %v", errs)
			}
			if (cfg.AdoptExisting == nil) != (tt.wantAdopt == nil) {
				t.Fatalf("AdoptExisting = %v, want %v", cfg.AdoptExisting, tt.wantAdopt)
			}
			if cfg.AdoptExisting != nil && *cfg.AdoptExisting != *tt.wantAdopt {
				t.Errorf("AdoptExisting = %v, want %v", *cfg.AdoptExisting, *tt.wantAdopt)
			}
			if cfg.AdoptExistingAllowOverrides != tt.wantAllow {
				t.Errorf("AdoptExistingAllowOverrides = %v, want %v", cfg.AdoptExistingAllowOverrides, tt.wantAllow)
			}
		})
	}
}

func TestConvertFileProvider_AdoptionPolicy(t *testing.T) {
	adopt := false
	cfg, errs := convertFileProvider(FileProviderConfig{
		Name:                        "internal",
		Type:                        "technitium",
		Domains:                     []string{"*.example.com"},
		Target:                      "10.0.0.1",
		AdoptExisting:               &adopt,
		AdoptExistingAllowOverrides: true,
	}, 300)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if cfg.AdoptExisting == nil || *cfg.AdoptExisting {
		t.Fatalf("AdoptExisting = %v, want explicit false", cfg.AdoptExisting)
	}
	if !cfg.AdoptExistingAllowOverrides {
		t.Error("AdoptExistingAllowOverrides = false, want true")
	}
	providerCfg := cfg.ToProviderConfig()
	if providerCfg.AdoptExisting == nil || *providerCfg.AdoptExisting {
		t.Fatalf("provider AdoptExisting = %v, want explicit false", providerCfg.AdoptExisting)
	}
	if !providerCfg.AdoptExistingAllowOverrides {
		t.Error("provider AdoptExistingAllowOverrides = false, want true")
	}
}

func TestMergeProviderEnvOverrides_AdoptionPolicy(t *testing.T) {
	const instanceName = "yaml-adoption"
	prefix := envPrefix(instanceName)
	clearInstanceEnv(t, instanceName)
	defer clearInstanceEnv(t, instanceName)

	fromFile := true
	cfg := &ProviderInstanceConfig{Name: instanceName, AdoptExisting: &fromFile}
	os.Setenv(prefix+"ADOPT_EXISTING", "false")
	os.Setenv(prefix+"ADOPT_EXISTING_ALLOW_OVERRIDES", "true")

	if errs := mergeProviderEnvOverrides(cfg); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if cfg.AdoptExisting == nil || *cfg.AdoptExisting {
		t.Fatalf("AdoptExisting = %v, want env override false", cfg.AdoptExisting)
	}
	if !cfg.AdoptExistingAllowOverrides {
		t.Error("AdoptExistingAllowOverrides = false, want env override true")
	}
}
