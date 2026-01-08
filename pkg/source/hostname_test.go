package source

import (
	"testing"
)

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
		{Name: "app.example.com", Source: "caddy", Router: "app2"}, // duplicate name, different source
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
