package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestConfigPathFromArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "long flag",
			args: []string{"/usr/local/bin/dnsweaver", "--config", "/etc/dnsweaver/config.yml"},
			want: "/etc/dnsweaver/config.yml",
		},
		{
			name: "long flag with equals",
			args: []string{"/usr/local/bin/dnsweaver", "--config=/etc/dnsweaver/config.yml"},
			want: "/etc/dnsweaver/config.yml",
		},
		{
			name: "single-dash flag",
			args: []string{"/usr/local/bin/dnsweaver", "-config", "/etc/dnsweaver/config.yml"},
			want: "/etc/dnsweaver/config.yml",
		},
		{
			name: "missing value",
			args: []string{"/usr/local/bin/dnsweaver", "--config"},
		},
		{
			name: "no config flag",
			args: []string{"/usr/local/bin/dnsweaver"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := configPathFromArgs(tc.args); got != tc.want {
				t.Errorf("configPathFromArgs() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunReadinessCheckUsesConfiguredLocalListener(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, portString, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split listener address: %v", err)
	}
	if _, err := strconv.Atoi(portString); err != nil {
		t.Fatalf("invalid listener port: %v", err)
	}
	t.Setenv("DNSWEAVER_HEALTH_ADDRESS", "127.0.0.1")
	t.Setenv("DNSWEAVER_HEALTH_ALLOW_NETWORK", "false")
	t.Setenv("DNSWEAVER_HEALTH_PORT", portString)

	if err := runReadinessCheck(); err != nil {
		t.Fatalf("runReadinessCheck() error = %v", err)
	}
	if gotPath != "/ready" {
		t.Fatalf("readiness path = %q, want /ready", gotPath)
	}
}
