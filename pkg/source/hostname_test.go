package source

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateHostname_Valid(t *testing.T) {
	validHostnames := []string{
		"example.com",
		"app.example.com",
		"sub.domain.example.com",
		"a.b.c.d.e.example.com",
		"app-name.example.com",
		"app123.example.com",
		"123.example.com",
		"a.example.com",
		"x",
		"example.com.",      // trailing dot (FQDN)
		"*.example.com",     // wildcard
		"*.sub.example.com", // wildcard with subdomain
		"APP.EXAMPLE.COM",   // uppercase (valid, DNS is case-insensitive)
		"App.Example.Com",   // mixed case
		"xn--nxasmq5b.com",  // punycode (internationalized domain)
		"a-b-c.example.com",
		"a1.example.com",
		"1a.example.com",
	}

	for _, hostname := range validHostnames {
		t.Run(hostname, func(t *testing.T) {
			err := ValidateHostname(hostname)
			if err != nil {
				t.Errorf("ValidateHostname(%q) returned error: %v", hostname, err)
			}
		})
	}
}

func TestValidateHostname_Invalid(t *testing.T) {
	tests := []struct {
		name        string
		hostname    string
		wantErr     error
		wantErrText string
	}{
		{
			name:     "empty",
			hostname: "",
			wantErr:  ErrHostnameEmpty,
		},
		{
			name:     "just dot",
			hostname: ".",
			wantErr:  ErrHostnameEmpty, // becomes empty after trimming trailing dot
		},
		{
			name:     "double dot",
			hostname: "app..example.com",
			wantErr:  ErrLabelEmpty,
		},
		{
			name:     "leading dot",
			hostname: ".example.com",
			wantErr:  ErrLabelEmpty,
		},
		{
			name:     "underscore",
			hostname: "app_name.example.com",
			wantErr:  ErrInvalidCharacters,
		},
		{
			name:     "space",
			hostname: "app name.example.com",
			wantErr:  ErrInvalidCharacters,
		},
		{
			name:     "leading hyphen in label",
			hostname: "-app.example.com",
			wantErr:  ErrInvalidLabelStart,
		},
		{
			name:     "trailing hyphen in label",
			hostname: "app-.example.com",
			wantErr:  ErrInvalidLabelEnd,
		},
		{
			name:     "port number",
			hostname: "app.example.com:8080",
			wantErr:  ErrInvalidCharacters,
		},
		{
			name:     "protocol prefix",
			hostname: "https://app.example.com",
			wantErr:  ErrInvalidCharacters,
		},
		{
			name:     "hostname too long",
			hostname: strings.Repeat("a", 64) + "." + strings.Repeat("b", 64) + "." + strings.Repeat("c", 64) + "." + strings.Repeat("d", 64),
			wantErr:  ErrHostnameTooLong,
		},
		{
			name:     "label too long",
			hostname: strings.Repeat("a", 64) + ".example.com",
			wantErr:  ErrLabelTooLong,
		},
		{
			name:     "wildcard not first",
			hostname: "app.*.example.com",
			wantErr:  ErrInvalidCharacters,
		},
		{
			name:     "multiple wildcards",
			hostname: "*.*.example.com",
			wantErr:  ErrInvalidCharacters,
		},
		{
			name:     "special characters",
			hostname: "app@example.com",
			wantErr:  ErrInvalidCharacters,
		},
		{
			name:     "slash",
			hostname: "app/path.example.com",
			wantErr:  ErrInvalidCharacters,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHostname(tt.hostname)
			if err == nil {
				t.Errorf("ValidateHostname(%q) expected error, got nil", tt.hostname)
				return
			}

			var validationErr *HostnameValidationError
			if !errors.As(err, &validationErr) {
				t.Errorf("ValidateHostname(%q) expected HostnameValidationError, got %T", tt.hostname, err)
				return
			}

			if !errors.Is(validationErr.Err, tt.wantErr) {
				t.Errorf("ValidateHostname(%q) expected %v, got %v", tt.hostname, tt.wantErr, validationErr.Err)
			}
		})
	}
}

func TestHostname_Validate(t *testing.T) {
	h := Hostname{Name: "valid.example.com", Source: "traefik"}
	if err := h.Validate(); err != nil {
		t.Errorf("Validate() for valid hostname returned error: %v", err)
	}

	invalid := Hostname{Name: "invalid_hostname.example.com", Source: "traefik"}
	if err := invalid.Validate(); err == nil {
		t.Error("Validate() for invalid hostname expected error, got nil")
	}
}

func TestHostname_IsValid(t *testing.T) {
	valid := Hostname{Name: "valid.example.com", Source: "traefik"}
	if !valid.IsValid() {
		t.Error("IsValid() for valid hostname returned false")
	}

	invalid := Hostname{Name: "invalid_hostname.example.com", Source: "traefik"}
	if invalid.IsValid() {
		t.Error("IsValid() for invalid hostname returned true")
	}
}

func TestValidateSRVHostname_Valid(t *testing.T) {
	validSRVHostnames := []string{
		"_minecraft._tcp.mc.example.com",
		"_http._tcp.www.example.com",
		"_sip._udp.voip.example.com",
		"_ldap._tcp.dc1.example.com",
		"_kerberos._tcp.kdc.example.com",
		"_imaps._tcp.mail.example.com",
		"_xmpp-client._tcp.chat.example.com",
		"_minecraft._tcp.example.com", // minimal 3 labels after prefix
	}

	for _, hostname := range validSRVHostnames {
		t.Run(hostname, func(t *testing.T) {
			err := ValidateSRVHostname(hostname)
			if err != nil {
				t.Errorf("ValidateSRVHostname(%q) returned error: %v", hostname, err)
			}
		})
	}
}

func TestValidateSRVHostname_Invalid(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
	}{
		{"no_underscore_prefix", "minecraft.tcp.example.com"},
		{"only_one_underscore", "_minecraft.tcp.example.com"},
		{"too_few_labels", "_minecraft._tcp"},
		{"empty", ""},
		{"invalid_service_label", "_min@craft._tcp.example.com"},
		{"invalid_target_label", "_minecraft._tcp.exam!ple.com"},
		{"empty_label", "_minecraft._tcp..example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSRVHostname(tt.hostname)
			if err == nil {
				t.Errorf("ValidateSRVHostname(%q) expected error, got nil", tt.hostname)
			}
		})
	}
}

func TestHostname_Validate_SRV(t *testing.T) {
	// SRV hostname with SRV RecordHints should use SRV validation
	srvHostname := Hostname{
		Name:   "_minecraft._tcp.mc.example.com",
		Source: "dnsweaver",
		RecordHints: &RecordHints{
			Type: "SRV",
			SRV: &SRVHints{
				Priority: 10,
				Weight:   5,
				Port:     25565,
			},
		},
	}
	if err := srvHostname.Validate(); err != nil {
		t.Errorf("Validate() for SRV hostname returned error: %v", err)
	}
	if !srvHostname.IsValid() {
		t.Error("IsValid() for SRV hostname returned false")
	}

	// Same hostname WITHOUT SRV hints should fail RFC 1123 validation
	nonSRVHostname := Hostname{
		Name:   "_minecraft._tcp.mc.example.com",
		Source: "dnsweaver",
	}
	if err := nonSRVHostname.Validate(); err == nil {
		t.Error("Validate() for underscore hostname without SRV hints expected error, got nil")
	}
	if nonSRVHostname.IsValid() {
		t.Error("IsValid() for underscore hostname without SRV hints returned true")
	}
}

func TestHostnames_ValidHostnames(t *testing.T) {
	hostnames := Hostnames{
		{Name: "valid1.example.com", Source: "traefik"},
		{Name: "invalid_one.example.com", Source: "traefik"}, // underscore
		{Name: "valid2.example.com", Source: "traefik"},
		{Name: "-invalid.example.com", Source: "traefik"}, // leading hyphen
		{Name: "valid3.example.com", Source: "traefik"},
	}

	valid := hostnames.ValidHostnames()
	if len(valid) != 3 {
		t.Errorf("ValidHostnames() returned %d items, want 3", len(valid))
	}

	for _, h := range valid {
		if !h.IsValid() {
			t.Errorf("ValidHostnames() returned invalid hostname: %s", h.Name)
		}
	}
}

func TestHostnames_ValidateAll(t *testing.T) {
	hostnames := Hostnames{
		{Name: "valid1.example.com", Source: "traefik"},
		{Name: "invalid_one.example.com", Source: "traefik", Router: "router1"}, // underscore
		{Name: "valid2.example.com", Source: "caddy"},
		{Name: "-invalid.example.com", Source: "traefik", Router: "router2"}, // leading hyphen
	}

	result := hostnames.ValidateAll()

	if len(result.Valid) != 2 {
		t.Errorf("ValidateAll().Valid has %d items, want 2", len(result.Valid))
	}

	if len(result.Invalid) != 2 {
		t.Errorf("ValidateAll().Invalid has %d items, want 2", len(result.Invalid))
	}

	// Check that invalid entries have error details
	for _, inv := range result.Invalid {
		if inv.Error == nil {
			t.Errorf("Invalid hostname %s has nil error", inv.Hostname.Name)
		}
		if inv.Hostname.Name == "" {
			t.Error("Invalid entry has empty hostname")
		}
	}
}

func TestHostname_String(t *testing.T) {
	tests := []struct {
		name     string
		hostname Hostname
		want     string
	}{
		{
			name:     "with router",
			hostname: Hostname{Name: "app.example.com", Source: "traefik", Router: "myapp"},
			want:     "app.example.com (from traefik:myapp)",
		},
		{
			name:     "without router",
			hostname: Hostname{Name: "app.example.com", Source: "traefik"},
			want:     "app.example.com (from traefik)",
		},
		{
			name:     "empty router string",
			hostname: Hostname{Name: "app.example.com", Source: "caddy", Router: ""},
			want:     "app.example.com (from caddy)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.hostname.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHostnames_Names(t *testing.T) {
	hostnames := Hostnames{
		{Name: "app1.example.com", Source: "traefik"},
		{Name: "app2.example.com", Source: "traefik"},
		{Name: "app3.example.com", Source: "caddy"},
	}

	names := hostnames.Names()

	if len(names) != 3 {
		t.Fatalf("Names() returned %d items, want 3", len(names))
	}

	want := []string{"app1.example.com", "app2.example.com", "app3.example.com"}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("names[%d] = %q, want %q", i, names[i], w)
		}
	}
}

func TestHostnames_Names_Empty(t *testing.T) {
	var hostnames Hostnames
	names := hostnames.Names()

	if len(names) != 0 {
		t.Errorf("Names() returned %d items, want 0", len(names))
	}
}

func TestHostnames_Deduplicate(t *testing.T) {
	hostnames := Hostnames{
		{Name: "app.example.com", Source: "traefik", Router: "app1"},
		{Name: "other.example.com", Source: "traefik", Router: "other"},
		{Name: "app.example.com", Source: "caddy", Router: "app2"},   // duplicate name, different source
		{Name: "app.example.com", Source: "traefik", Router: "app3"}, // duplicate name, same source
	}

	deduped := hostnames.Deduplicate()

	if len(deduped) != 2 {
		t.Fatalf("Deduplicate() returned %d items, want 2", len(deduped))
	}

	// First occurrence should be kept
	if deduped[0].Name != "app.example.com" {
		t.Errorf("deduped[0].Name = %q, want %q", deduped[0].Name, "app.example.com")
	}
	if deduped[0].Source != "traefik" {
		t.Errorf("deduped[0].Source = %q, want %q", deduped[0].Source, "traefik")
	}
	if deduped[0].Router != "app1" {
		t.Errorf("deduped[0].Router = %q, want %q", deduped[0].Router, "app1")
	}

	if deduped[1].Name != "other.example.com" {
		t.Errorf("deduped[1].Name = %q, want %q", deduped[1].Name, "other.example.com")
	}
}

func TestHostnames_Filter(t *testing.T) {
	hostnames := Hostnames{
		{Name: "app1.example.com", Source: "traefik"},
		{Name: "app2.example.com", Source: "caddy"},
		{Name: "app3.example.com", Source: "traefik"},
	}

	// Filter to only traefik sources
	filtered := hostnames.Filter(func(h Hostname) bool {
		return h.Source == "traefik"
	})

	if len(filtered) != 2 {
		t.Fatalf("Filter() returned %d items, want 2", len(filtered))
	}

	if filtered[0].Name != "app1.example.com" {
		t.Errorf("filtered[0].Name = %q, want %q", filtered[0].Name, "app1.example.com")
	}
	if filtered[1].Name != "app3.example.com" {
		t.Errorf("filtered[1].Name = %q, want %q", filtered[1].Name, "app3.example.com")
	}
}

func TestHostnames_FromSource(t *testing.T) {
	hostnames := Hostnames{
		{Name: "app1.example.com", Source: "traefik"},
		{Name: "app2.example.com", Source: "caddy"},
		{Name: "app3.example.com", Source: "traefik"},
		{Name: "app4.example.com", Source: "nginx"},
	}

	traefik := hostnames.FromSource("traefik")
	if len(traefik) != 2 {
		t.Errorf("FromSource(traefik) returned %d items, want 2", len(traefik))
	}

	caddy := hostnames.FromSource("caddy")
	if len(caddy) != 1 {
		t.Errorf("FromSource(caddy) returned %d items, want 1", len(caddy))
	}

	unknown := hostnames.FromSource("unknown")
	if len(unknown) != 0 {
		t.Errorf("FromSource(unknown) returned %d items, want 0", len(unknown))
	}
}
