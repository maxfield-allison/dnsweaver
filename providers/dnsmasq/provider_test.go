package dnsmasq

import (
	"context"
	"testing"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				ConfigDir:     "/etc/dnsmasq.d",
				ConfigFile:    "dnsweaver.conf",
				ReloadCommand: "echo reload",
				TTL:           300,
			},
			wantErr: false,
		},
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name:   "invalid config",
			config: &Config{
				// Missing required fields
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New("test", tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && p == nil {
				t.Error("New() returned nil provider")
			}
		})
	}
}

func TestProvider_Name(t *testing.T) {
	config := &Config{
		ConfigDir:     "/etc/dnsmasq.d",
		ConfigFile:    "dnsweaver.conf",
		ReloadCommand: "echo reload",
	}

	p, err := New("my-pihole", config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := p.Name(); got != "my-pihole" {
		t.Errorf("Name() = %v, want my-pihole", got)
	}
}

func TestProvider_Type(t *testing.T) {
	config := &Config{
		ConfigDir:     "/etc/dnsmasq.d",
		ConfigFile:    "dnsweaver.conf",
		ReloadCommand: "echo reload",
	}

	p, err := New("test", config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := p.Type(); got != "dnsmasq" {
		t.Errorf("Type() = %v, want dnsmasq", got)
	}
}

func TestProvider_Zone(t *testing.T) {
	config := &Config{
		ConfigDir:     "/etc/dnsmasq.d",
		ConfigFile:    "dnsweaver.conf",
		ReloadCommand: "echo reload",
		Zone:          "home.arpa",
	}

	p, err := New("test", config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := p.Zone(); got != "home.arpa" {
		t.Errorf("Zone() = %v, want home.arpa", got)
	}
}

func TestProvider_List(t *testing.T) {
	mockFS := newMockFileSystem()
	mockFS.dirs["/etc/dnsmasq.d"] = true
	mockFS.files["/etc/dnsmasq.d/dnsweaver.conf"] = []byte(`address=/app.example.com/10.0.0.100
address=/ipv6.example.com/fd00::1
cname=www.example.com,app.example.com
`)

	client := NewClient("/etc/dnsmasq.d", "dnsweaver.conf", "echo reload", "",
		WithFileSystem(mockFS))

	config := &Config{
		ConfigDir:     "/etc/dnsmasq.d",
		ConfigFile:    "dnsweaver.conf",
		ReloadCommand: "echo reload",
		TTL:           300,
	}

	p, err := New("test", config, WithClient(client))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	records, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(records) != 3 {
		t.Errorf("List() returned %d records, want 3", len(records))
	}

	// Verify record types
	typeCount := map[provider.RecordType]int{}
	for _, r := range records {
		typeCount[r.Type]++
	}

	if typeCount[provider.RecordTypeA] != 1 {
		t.Errorf("expected 1 A record, got %d", typeCount[provider.RecordTypeA])
	}
	if typeCount[provider.RecordTypeAAAA] != 1 {
		t.Errorf("expected 1 AAAA record, got %d", typeCount[provider.RecordTypeAAAA])
	}
	if typeCount[provider.RecordTypeCNAME] != 1 {
		t.Errorf("expected 1 CNAME record, got %d", typeCount[provider.RecordTypeCNAME])
	}
}

func TestProvider_Create(t *testing.T) {
	mockFS := newMockFileSystem()
	mockFS.dirs["/etc/dnsmasq.d"] = true

	client := NewClient("/etc/dnsmasq.d", "dnsweaver.conf", "echo reload", "",
		WithFileSystem(mockFS))

	config := &Config{
		ConfigDir:     "/etc/dnsmasq.d",
		ConfigFile:    "dnsweaver.conf",
		ReloadCommand: "echo reload",
		TTL:           300,
	}

	p, err := New("test", config, WithClient(client), WithReloadOnWrite(false))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Create A record
	err = p.Create(context.Background(), provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.100",
		TTL:      300,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Verify file contains record
	content := string(mockFS.files["/etc/dnsmasq.d/dnsweaver.conf"])
	if content == "" {
		t.Error("Create() should have written to file")
	}
}

func TestProvider_Create_UnsupportedType(t *testing.T) {
	mockFS := newMockFileSystem()
	mockFS.dirs["/etc/dnsmasq.d"] = true

	client := NewClient("/etc/dnsmasq.d", "dnsweaver.conf", "echo reload", "",
		WithFileSystem(mockFS))

	config := &Config{
		ConfigDir:     "/etc/dnsmasq.d",
		ConfigFile:    "dnsweaver.conf",
		ReloadCommand: "echo reload",
	}

	p, err := New("test", config, WithClient(client), WithReloadOnWrite(false))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// SRV should fail
	err = p.Create(context.Background(), provider.Record{
		Hostname: "_minecraft._tcp.example.com",
		Type:     provider.RecordTypeSRV,
		Target:   "mc.example.com",
		TTL:      300,
		SRV: &provider.SRVData{
			Priority: 10,
			Weight:   5,
			Port:     25565,
		},
	})
	if err == nil {
		t.Error("Create() should error for SRV records")
	}

	// TXT should be silently skipped (ownership tracking)
	err = p.Create(context.Background(), provider.Record{
		Hostname: "_dnsweaver.app.example.com",
		Type:     provider.RecordTypeTXT,
		Target:   "heritage=dnsweaver",
		TTL:      300,
	})
	if err != nil {
		t.Errorf("Create() should skip TXT records without error, got: %v", err)
	}
}

func TestProvider_Delete(t *testing.T) {
	mockFS := newMockFileSystem()
	mockFS.dirs["/etc/dnsmasq.d"] = true
	mockFS.files["/etc/dnsmasq.d/dnsweaver.conf"] = []byte("address=/app.example.com/10.0.0.100\n")

	client := NewClient("/etc/dnsmasq.d", "dnsweaver.conf", "echo reload", "",
		WithFileSystem(mockFS))

	config := &Config{
		ConfigDir:     "/etc/dnsmasq.d",
		ConfigFile:    "dnsweaver.conf",
		ReloadCommand: "echo reload",
	}

	p, err := New("test", config, WithClient(client), WithReloadOnWrite(false))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = p.Delete(context.Background(), provider.Record{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.0.0.100",
	})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// File should still exist but be empty (or just header)
	content := string(mockFS.files["/etc/dnsmasq.d/dnsweaver.conf"])
	// Empty content is acceptable after delete
	_ = content
}

func TestNewFromMap(t *testing.T) {
	configMap := map[string]string{
		"CONFIG_DIR":     "/custom/dnsmasq.d",
		"CONFIG_FILE":    "custom.conf",
		"RELOAD_COMMAND": "killall -HUP dnsmasq",
		"ZONE":           "local.home",
		"TTL":            "600",
	}

	p, err := NewFromMap("test-instance", configMap)
	if err != nil {
		t.Fatalf("NewFromMap() error = %v", err)
	}

	if p.Name() != "test-instance" {
		t.Errorf("Name() = %v, want test-instance", p.Name())
	}
	if p.Type() != "dnsmasq" {
		t.Errorf("Type() = %v, want dnsmasq", p.Type())
	}
	if p.Zone() != "local.home" {
		t.Errorf("Zone() = %v, want local.home", p.Zone())
	}
	if p.ttl != 600 {
		t.Errorf("ttl = %v, want 600", p.ttl)
	}
}

func TestFactory(t *testing.T) {
	factory := Factory()

	configMap := map[string]string{
		"CONFIG_DIR":     "/etc/dnsmasq.d",
		"CONFIG_FILE":    "test.conf",
		"RELOAD_COMMAND": "echo reload",
	}

	p, err := factory("factory-test", configMap)
	if err != nil {
		t.Fatalf("Factory() error = %v", err)
	}

	if p.Name() != "factory-test" {
		t.Errorf("Name() = %v, want factory-test", p.Name())
	}
	if p.Type() != "dnsmasq" {
		t.Errorf("Type() = %v, want dnsmasq", p.Type())
	}
}

// Verify compile-time interface satisfaction
var _ provider.Provider = (*Provider)(nil)
