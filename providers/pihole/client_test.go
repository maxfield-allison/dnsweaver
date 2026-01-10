package pihole

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid API mode",
			config: Config{
				Mode:     ModeAPI,
				URL:      "http://pihole.local",
				Password: "test",
				TTL:      300,
			},
			wantErr: false,
		},
		{
			name: "valid file mode",
			config: Config{
				Mode:          ModeFile,
				ConfigDir:     "/etc/pihole",
				ConfigFile:    "custom.list",
				ReloadCommand: "pihole restartdns",
				TTL:           300,
			},
			wantErr: false,
		},
		{
			name: "API mode missing URL",
			config: Config{
				Mode:     ModeAPI,
				Password: "test",
			},
			wantErr: true,
		},
		{
			name: "API mode missing password",
			config: Config{
				Mode: ModeAPI,
				URL:  "http://pihole.local",
			},
			wantErr: true,
		},
		{
			name: "file mode missing config dir",
			config: Config{
				Mode:          ModeFile,
				ConfigFile:    "custom.list",
				ReloadCommand: "pihole restartdns",
			},
			wantErr: true,
		},
		{
			name: "missing mode",
			config: Config{
				URL:      "http://pihole.local",
				Password: "test",
			},
			wantErr: true,
		},
		{
			name: "invalid mode",
			config: Config{
				Mode: "invalid",
			},
			wantErr: true,
		},
		{
			name: "negative TTL",
			config: Config{
				Mode:     ModeAPI,
				URL:      "http://pihole.local",
				Password: "test",
				TTL:      -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAPIClient_Ping(t *testing.T) {
	tests := []struct {
		name       string
		response   string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "successful ping",
			response:   `{"status":"enabled"}`,
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "server error",
			response:   `Internal Server Error`,
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "testpassword", "")
			err := client.Ping(context.Background())

			if (err != nil) != tt.wantErr {
				t.Errorf("Ping() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAPIClient_List(t *testing.T) {
	tests := []struct {
		name         string
		customDNS    customDNSResponse
		cname        cnameResponse
		zone         string
		wantRecords  int
		wantHostname string
	}{
		{
			name: "list A records",
			customDNS: customDNSResponse{
				Data: [][]string{
					{"10.1.20.210", "app.example.com"},
					{"10.1.20.211", "db.example.com"},
				},
			},
			cname:        cnameResponse{Data: [][]string{}},
			wantRecords:  2,
			wantHostname: "app.example.com",
		},
		{
			name: "list AAAA records",
			customDNS: customDNSResponse{
				Data: [][]string{
					{"fd00::1", "app.example.com"},
				},
			},
			cname:        cnameResponse{Data: [][]string{}},
			wantRecords:  1,
			wantHostname: "app.example.com",
		},
		{
			name:      "list CNAME records",
			customDNS: customDNSResponse{Data: [][]string{}},
			cname: cnameResponse{
				Data: [][]string{
					{"alias.example.com", "target.example.com"},
				},
			},
			wantRecords:  1,
			wantHostname: "alias.example.com",
		},
		{
			name: "filter by zone",
			customDNS: customDNSResponse{
				Data: [][]string{
					{"10.1.20.210", "app.example.com"},
					{"10.1.20.211", "other.different.com"},
				},
			},
			cname:        cnameResponse{Data: [][]string{}},
			zone:         "example.com",
			wantRecords:  1,
			wantHostname: "app.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var resp interface{}
				query := r.URL.Query()
				if query.Has("customdns") {
					resp = tt.customDNS
				} else if query.Has("customcname") {
					resp = tt.cname
				}
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "testpassword", tt.zone)
			records, err := client.List(context.Background())

			if err != nil {
				t.Fatalf("List() error = %v", err)
			}

			if len(records) != tt.wantRecords {
				t.Errorf("List() got %d records, want %d", len(records), tt.wantRecords)
			}

			if len(records) > 0 && records[0].Hostname != tt.wantHostname {
				t.Errorf("List() first hostname = %v, want %v", records[0].Hostname, tt.wantHostname)
			}
		})
	}
}

func TestAPIClient_Create(t *testing.T) {
	tests := []struct {
		name     string
		record   piholeRecord
		response string
		wantErr  bool
	}{
		{
			name: "create A record",
			record: piholeRecord{
				Hostname: "app.example.com",
				Type:     provider.RecordTypeA,
				Target:   "10.1.20.210",
			},
			response: `{"success": true}`,
			wantErr:  false,
		},
		{
			name: "create AAAA record",
			record: piholeRecord{
				Hostname: "app.example.com",
				Type:     provider.RecordTypeAAAA,
				Target:   "fd00::1",
			},
			response: `{"success": true}`,
			wantErr:  false,
		},
		{
			name: "create CNAME record",
			record: piholeRecord{
				Hostname: "alias.example.com",
				Type:     provider.RecordTypeCNAME,
				Target:   "target.example.com",
			},
			response: `{"success": true}`,
			wantErr:  false,
		},
		{
			name: "duplicate record (not an error)",
			record: piholeRecord{
				Hostname: "app.example.com",
				Type:     provider.RecordTypeA,
				Target:   "10.1.20.210",
			},
			response: `{"success": false, "message": "Record already exists"}`,
			wantErr:  false,
		},
		{
			name: "API error",
			record: piholeRecord{
				Hostname: "app.example.com",
				Type:     provider.RecordTypeA,
				Target:   "10.1.20.210",
			},
			response: `{"success": false, "message": "Invalid request"}`,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "testpassword", "")
			err := client.Create(context.Background(), tt.record)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAPIClient_Delete(t *testing.T) {
	tests := []struct {
		name     string
		record   piholeRecord
		response string
		wantErr  bool
	}{
		{
			name: "delete A record",
			record: piholeRecord{
				Hostname: "app.example.com",
				Type:     provider.RecordTypeA,
				Target:   "10.1.20.210",
			},
			response: `{"success": true}`,
			wantErr:  false,
		},
		{
			name: "delete CNAME record",
			record: piholeRecord{
				Hostname: "alias.example.com",
				Type:     provider.RecordTypeCNAME,
				Target:   "target.example.com",
			},
			response: `{"success": true}`,
			wantErr:  false,
		},
		{
			name: "record not found (not an error)",
			record: piholeRecord{
				Hostname: "app.example.com",
				Type:     provider.RecordTypeA,
				Target:   "10.1.20.210",
			},
			response: `{"success": false, "message": "Record not found"}`,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewAPIClient(server.URL, "testpassword", "")
			err := client.Delete(context.Background(), tt.record)

			if (err != nil) != tt.wantErr {
				t.Errorf("Delete() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestProvider_Type(t *testing.T) {
	config := &Config{
		Mode:     ModeAPI,
		URL:      "http://pihole.local",
		Password: "test",
		TTL:      300,
	}

	p, err := New("test", config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if p.Type() != "pihole" {
		t.Errorf("Type() = %v, want pihole", p.Type())
	}
}

func TestProvider_Name(t *testing.T) {
	config := &Config{
		Mode:     ModeAPI,
		URL:      "http://pihole.local",
		Password: "test",
		TTL:      300,
	}

	p, err := New("my-pihole", config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if p.Name() != "my-pihole" {
		t.Errorf("Name() = %v, want my-pihole", p.Name())
	}
}

func TestProvider_Mode(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		wantMode Mode
	}{
		{
			name: "API mode",
			config: &Config{
				Mode:     ModeAPI,
				URL:      "http://pihole.local",
				Password: "test",
				TTL:      300,
			},
			wantMode: ModeAPI,
		},
		{
			name: "file mode",
			config: &Config{
				Mode:          ModeFile,
				ConfigDir:     "/etc/pihole",
				ConfigFile:    "custom.list",
				ReloadCommand: "echo reload",
				TTL:           300,
			},
			wantMode: ModeFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New("test", tt.config)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			if p.Mode() != tt.wantMode {
				t.Errorf("Mode() = %v, want %v", p.Mode(), tt.wantMode)
			}
		})
	}
}

func TestProvider_UnsupportedRecordTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	defer server.Close()

	config := &Config{
		Mode:     ModeAPI,
		URL:      server.URL,
		Password: "test",
		TTL:      300,
	}

	p, err := New("test", config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name    string
		record  provider.Record
		wantErr bool
	}{
		{
			name: "TXT record skipped silently",
			record: provider.Record{
				Hostname: "_dnsweaver.app.example.com",
				Type:     provider.RecordTypeTXT,
				Target:   "heritage=dnsweaver",
			},
			wantErr: false,
		},
		{
			name: "SRV record returns error",
			record: provider.Record{
				Hostname: "_minecraft._tcp.example.com",
				Type:     provider.RecordTypeSRV,
				Target:   "mc.example.com",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := p.Create(context.Background(), tt.record)
			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
