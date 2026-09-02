package main

import "testing"

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
