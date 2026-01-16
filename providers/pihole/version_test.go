package pihole

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionDetector_DetectV6(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/info" {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]any{
				"ftl": map[string]string{
					"version": "v6.0.0",
					"branch":  "master",
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	detector := NewVersionDetector(server.URL, nil, nil)
	version, versionStr, err := detector.Detect(context.Background())

	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if version != APIVersionV6 {
		t.Errorf("Detect() version = %v, want %v", version, APIVersionV6)
	}
	if versionStr != "v6.0.0" {
		t.Errorf("Detect() versionStr = %v, want v6.0.0", versionStr)
	}
}

func TestVersionDetector_DetectV5(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// V6 endpoint fails
		if r.URL.Path == "/api/info" {
			http.NotFound(w, r)
			return
		}
		// V5 endpoint succeeds
		if r.URL.Path == "/admin/api.php" {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]int{"version": 5}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	detector := NewVersionDetector(server.URL, nil, nil)
	version, versionStr, err := detector.Detect(context.Background())

	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if version != APIVersionV5 {
		t.Errorf("Detect() version = %v, want %v", version, APIVersionV5)
	}
	if versionStr != "5" {
		t.Errorf("Detect() versionStr = %v, want 5", versionStr)
	}
}

func TestVersionDetector_DetectFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	detector := NewVersionDetector(server.URL, nil, nil)
	version, _, err := detector.Detect(context.Background())

	if err == nil {
		t.Fatal("Detect() expected error, got nil")
	}
	if version != APIVersionUnknown {
		t.Errorf("Detect() version = %v, want %v", version, APIVersionUnknown)
	}
}

func TestAPIVersion_String(t *testing.T) {
	tests := []struct {
		version APIVersion
		want    string
	}{
		{APIVersionUnknown, "unknown"},
		{APIVersionV5, "v5"},
		{APIVersionV6, "v6"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.version.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}
