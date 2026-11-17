package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLoadVirtualizationEnvironmentWithFile(t *testing.T) {
	clearAllEnv(t)
	defer clearAllEnv(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("platform: none\nproviders:\n  - name: dns\n    type: technitium\n    target: 10.0.0.1\n    domains: ['*.example.com']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DNSWEAVER_CONFIG", path)
	t.Setenv("DNSWEAVER_PROXMOX_URL", "https://pve.example.com:8006")
	t.Setenv("DNSWEAVER_PROXMOX_TOKEN_ID", "user@pve!dns")
	secret := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(secret, []byte("dummy-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DNSWEAVER_PROXMOX_TOKEN_SECRET_FILE", secret)
	t.Setenv("DNSWEAVER_PROXMOX_DOMAIN_SUFFIX", "example.com")
	t.Setenv("DNSWEAVER_INCUS_SOCKET_PATH", "/run/incus/unix.socket")
	t.Setenv("DNSWEAVER_INCUS_PROJECTS", "alpha,beta")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.UseProxmox() || cfg.ProxmoxTokenID() != "user@pve!dns" || cfg.ProxmoxTokenSecret() != "dummy-token" || cfg.ProxmoxDomainSuffix() != "example.com" {
		t.Error("file configuration lost Proxmox environment settings")
	}
	if !cfg.UseIncus() || !reflect.DeepEqual(cfg.IncusProjects(), []string{"alpha", "beta"}) {
		t.Error("file configuration lost Incus environment settings")
	}
}

func TestGlobalEnvironmentOverridesFileSettings(t *testing.T) {
	clearAllEnv(t)
	defer clearAllEnv(t)
	t.Setenv("DNSWEAVER_LOG_FILE", "/env/dns.log")
	t.Setenv("DNSWEAVER_LOG_MAX_SIZE", "12")
	t.Setenv("DNSWEAVER_LOG_MAX_BACKUPS", "0")
	t.Setenv("DNSWEAVER_LOG_MAX_AGE", "0")
	t.Setenv("DNSWEAVER_LOG_COMPRESS", "false")
	t.Setenv("DNSWEAVER_SHUTDOWN_TIMEOUT", "12s")
	t.Setenv("DNSWEAVER_DOCKER_CONNECT_TIMEOUT", "0")
	base := (&FileConfig{}).ToGlobalConfig()
	base.LogFile = "/file/dns.log"
	got, errs := mergeGlobalConfig(base)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if got.LogFile != "/env/dns.log" || got.LogMaxSize != 12 || got.LogMaxBackups != 0 || got.LogMaxAge != 0 || got.LogCompress || got.ShutdownTimeout != 12*time.Second || got.DockerConnectTimeout != 0 {
		t.Error("file configuration suppressed logging or timeout environment overrides")
	}
	if base.LogFile != "/file/dns.log" || base.LogMaxSize != DefaultLogMaxSize {
		t.Error("merge mutated base")
	}
}

func TestGlobalFileEnvironmentValidationParity(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"DNSWEAVER_PROXMOX_TARGET_MODE", "invalid"},
		{"DNSWEAVER_PROXMOX_IP_VERSION", "invalid"},
		{"DNSWEAVER_INCUS_TARGET_MODE", "invalid"},
		{"DNSWEAVER_LOG_MAX_SIZE", "invalid"},
		{"DNSWEAVER_SHUTDOWN_TIMEOUT", "0"},
		{"DNSWEAVER_DOCKER_CONNECT_TIMEOUT", "-1s"},
		{"DNSWEAVER_PROXMOX_TOKEN_SECRET_FILE", "/does-not-exist/dummy-token"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			clearAllEnv(t)
			defer clearAllEnv(t)
			t.Setenv(tc.key, tc.value)
			_, envErrs := loadGlobalConfig()
			_, fileErrs := mergeGlobalConfig((&FileConfig{}).ToGlobalConfig())
			if len(envErrs) == 0 || len(fileErrs) == 0 {
				t.Fatalf("invalid setting accepted: env errors %d; file errors %d", len(envErrs), len(fileErrs))
			}
		})
	}
}

func TestVirtualizationEnvironmentMatrixWithFile(t *testing.T) {
	for _, tc := range []struct {
		key, field, value string
		want              any
	}{
		{"DNSWEAVER_PROXMOX_URL", "ProxmoxURL", "https://pve.example.com:8006", "https://pve.example.com:8006"},
		{"DNSWEAVER_PROXMOX_TOKEN_ID", "ProxmoxTokenID", "user@pve!dns", "user@pve!dns"},
		{"DNSWEAVER_PROXMOX_TOKEN_SECRET", "ProxmoxTokenSecret", "dummy-token", "dummy-token"},
		{"DNSWEAVER_PROXMOX_NODE_FILTER", "ProxmoxNodeFilter", "node-a", "node-a"},
		{"DNSWEAVER_PROXMOX_TAG_FILTER", "ProxmoxTagFilter", "publish", "publish"},
		{"DNSWEAVER_PROXMOX_STATE_FILTER", "ProxmoxStateFilter", "stopped", "stopped"},
		{"DNSWEAVER_PROXMOX_DOMAIN_SUFFIX", "ProxmoxDomainSuffix", "example.com", "example.com"},
		{"DNSWEAVER_PROXMOX_HOSTNAME_TAG_PREFIX", "ProxmoxHostnameTagPrefix", "dns", "dns"},
		{"DNSWEAVER_PROXMOX_INTERFACE_TAG_PREFIX", "ProxmoxInterfaceTagPrefix", "nic", "nic"},
		{"DNSWEAVER_PROXMOX_ALLOWED_INTERFACES", "ProxmoxAllowedInterfaces", "eth0,ens18", []string{"eth0", "ens18"}},
		{"DNSWEAVER_PROXMOX_TARGET_MODE", "ProxmoxTargetMode", "instance", "instance"},
		{"DNSWEAVER_PROXMOX_IP_VERSION", "ProxmoxIPVersion", "dual", "dual"},
		{"DNSWEAVER_PROXMOX_VERIFY_TLS", "ProxmoxVerifyTLS", "true", true},
		{"DNSWEAVER_PROXMOX_TLS_CA_FILE", "ProxmoxTLSCAFile", "/fixture/ca.pem", "/fixture/ca.pem"},
		{"DNSWEAVER_PROXMOX_TLS_CERT_FILE", "ProxmoxTLSCertFile", "/fixture/client.pem", "/fixture/client.pem"},
		{"DNSWEAVER_PROXMOX_TLS_KEY_FILE", "ProxmoxTLSKeyFile", "/fixture/client.key", "/fixture/client.key"},
		{"DNSWEAVER_PROXMOX_TLS_SERVER_NAME", "ProxmoxTLSServerName", "pve.example.com", "pve.example.com"},
		{"DNSWEAVER_PROXMOX_TLS_MIN_VERSION", "ProxmoxTLSMinVersion", "1.3", "1.3"},
		{"DNSWEAVER_PROXMOX_TLS_SKIP_VERIFY", "ProxmoxTLSSkipVerify", "true", true},
		{"DNSWEAVER_INCUS_URL", "IncusURL", "https://incus.example.com:8443", "https://incus.example.com:8443"},
		{"DNSWEAVER_INCUS_SOCKET_PATH", "IncusSocketPath", "/run/incus/unix.socket", "/run/incus/unix.socket"},
		{"DNSWEAVER_INCUS_PROJECT", "IncusProject", "alpha", "alpha"},
		{"DNSWEAVER_INCUS_PROJECTS", "IncusProjects", "alpha,beta", []string{"alpha", "beta"}},
		{"DNSWEAVER_INCUS_ALL_PROJECTS", "IncusAllProjects", "true", true},
		{"DNSWEAVER_INCUS_STATE_FILTER", "IncusStateFilter", "stopped", "stopped"},
		{"DNSWEAVER_INCUS_DOMAIN_SUFFIX", "IncusDomainSuffix", "example.com", "example.com"},
		{"DNSWEAVER_INCUS_TARGET_MODE", "IncusTargetMode", "instance", "instance"},
		{"DNSWEAVER_INCUS_TLS_CA_FILE", "IncusTLSCAFile", "/fixture/ca.pem", "/fixture/ca.pem"},
		{"DNSWEAVER_INCUS_TLS_CERT_FILE", "IncusTLSCertFile", "/fixture/client.pem", "/fixture/client.pem"},
		{"DNSWEAVER_INCUS_TLS_KEY_FILE", "IncusTLSKeyFile", "/fixture/client.key", "/fixture/client.key"},
		{"DNSWEAVER_INCUS_TLS_SERVER_NAME", "IncusTLSServerName", "incus.example.com", "incus.example.com"},
		{"DNSWEAVER_INCUS_TLS_MIN_VERSION", "IncusTLSMinVersion", "1.3", "1.3"},
		{"DNSWEAVER_INCUS_TLS_SKIP_VERIFY", "IncusTLSSkipVerify", "true", true},
		{"DNSWEAVER_INCUS_TLS_PIN_SHA256", "IncusTLSPinSHA256", "dummy-pin", "dummy-pin"},
		{"DNSWEAVER_INCUS_TRUST_TOKEN", "IncusTrustToken", "dummy-trust-token", "dummy-trust-token"},
		{"DNSWEAVER_INCUS_CERT_STORE", "IncusCertStore", "/fixture/cert-store", "/fixture/cert-store"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			clearAllEnv(t)
			defer clearAllEnv(t)
			t.Setenv(tc.key, tc.value)
			cfg, errs := mergeGlobalConfig((&FileConfig{}).ToGlobalConfig())
			if len(errs) > 0 {
				t.Fatal(errs)
			}
			got := reflect.ValueOf(cfg).Elem().FieldByName(tc.field).Interface()
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%s=%v, want %v", tc.field, got, tc.want)
			}
		})
	}
}
