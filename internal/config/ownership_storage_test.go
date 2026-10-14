package config

import "testing"

func TestOwnershipStorageDefaultAndEnvironmentValidation(t *testing.T) {
	for _, mode := range []string{"", "compact", "legacy", "postgres", "unexpected"} {
		t.Run(mode, func(t *testing.T) {
			clearGlobalEnv(t)
			t.Setenv("DNSWEAVER_OWNERSHIP_STORAGE", mode)
			cfg, errs := loadGlobalConfig()
			if mode == "postgres" || mode == "unexpected" {
				if len(errs) == 0 {
					t.Fatal("accepted unsupported ownership backend")
				}
				return
			}
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			want := mode
			if want == "" {
				want = "legacy"
			}
			if cfg.OwnershipStorage != want {
				t.Fatalf("storage=%q want=%q", cfg.OwnershipStorage, want)
			}
		})
	}
}

func TestOwnershipStorageFileAndEnvironmentPrecedence(t *testing.T) {
	clearGlobalEnv(t)
	file := &FileConfig{Reconciler: &FileReconcilerConfig{OwnershipStorage: "legacy"}}
	t.Setenv("OWNERSHIP_MODE", "legacy")
	file.Reconciler.OwnershipStorage = "${OWNERSHIP_MODE}"
	file.interpolateEnvVars()
	cfg := file.ToGlobalConfig()
	if cfg.OwnershipStorage != "legacy" {
		t.Fatal("file ownership storage ignored")
	}
	t.Setenv("DNSWEAVER_OWNERSHIP_STORAGE", "compact")
	cfg, errs := mergeGlobalConfig(cfg)
	if len(errs) != 0 || cfg.OwnershipStorage != "compact" {
		t.Fatal("environment did not override file", errs)
	}
}
