package dnsmasq

import (
	"context"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

// mockFileSystem implements FileSystem for testing.
type mockFileSystem struct {
	files map[string][]byte
	dirs  map[string]bool
}

func newMockFileSystem() *mockFileSystem {
	return &mockFileSystem{
		files: make(map[string][]byte),
		dirs:  make(map[string]bool),
	}
}

func (m *mockFileSystem) ReadFile(path string) ([]byte, error) {
	if content, ok := m.files[path]; ok {
		return content, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	m.files[path] = data
	return nil
}

type mockFileInfo struct {
	name  string
	isDir bool
}

func (m mockFileInfo) Name() string       { return m.name }
func (m mockFileInfo) Size() int64        { return 0 }
func (m mockFileInfo) Mode() fs.FileMode  { return 0755 }
func (m mockFileInfo) ModTime() time.Time { return time.Now() }
func (m mockFileInfo) IsDir() bool        { return m.isDir }
func (m mockFileInfo) Sys() interface{}   { return nil }

func (m *mockFileSystem) Stat(path string) (os.FileInfo, error) {
	if m.dirs[path] {
		return mockFileInfo{name: path, isDir: true}, nil
	}
	if _, ok := m.files[path]; ok {
		return mockFileInfo{name: path, isDir: false}, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockFileSystem) MkdirAll(path string, perm os.FileMode) error {
	m.dirs[path] = true
	return nil
}

func TestClient_ConfigFilePath(t *testing.T) {
	client := NewClient("/etc/dnsmasq.d", "test.conf", "echo reload", "")

	got := client.ConfigFilePath()
	want := "/etc/dnsmasq.d/test.conf"

	if got != want {
		t.Errorf("ConfigFilePath() = %v, want %v", got, want)
	}
}

func TestClient_Ping(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*mockFileSystem)
		wantErr bool
	}{
		{
			name: "directory exists",
			setup: func(m *mockFileSystem) {
				m.dirs["/etc/dnsmasq.d"] = true
			},
			wantErr: false,
		},
		{
			name: "directory does not exist",
			setup: func(m *mockFileSystem) {
				// Don't add directory
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := newMockFileSystem()
			tt.setup(mockFS)

			client := NewClient("/etc/dnsmasq.d", "test.conf", "echo reload", "",
				WithFileSystem(mockFS))

			err := client.Ping(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Ping() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_ParseConfigContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []dnsmasqRecord
	}{
		{
			name:    "empty content",
			content: "",
			want:    nil,
		},
		{
			name:    "comments only",
			content: "# This is a comment\n# Another comment\n",
			want:    nil,
		},
		{
			name:    "A record",
			content: "address=/app.example.com/10.1.20.210\n",
			want: []dnsmasqRecord{
				{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.1.20.210"},
			},
		},
		{
			name:    "AAAA record",
			content: "address=/app.example.com/fd00::1\n",
			want: []dnsmasqRecord{
				{Hostname: "app.example.com", Type: provider.RecordTypeAAAA, Target: "fd00::1"},
			},
		},
		{
			name:    "CNAME record",
			content: "cname=alias.example.com,target.example.com\n",
			want: []dnsmasqRecord{
				{Hostname: "alias.example.com", Type: provider.RecordTypeCNAME, Target: "target.example.com"},
			},
		},
		{
			name: "mixed records",
			content: `# Managed by dnsweaver
address=/app.example.com/10.1.20.210
address=/ipv6.example.com/2001:db8::1
cname=www.example.com,app.example.com
`,
			want: []dnsmasqRecord{
				{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.1.20.210"},
				{Hostname: "ipv6.example.com", Type: provider.RecordTypeAAAA, Target: "2001:db8::1"},
				{Hostname: "www.example.com", Type: provider.RecordTypeCNAME, Target: "app.example.com"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient("/etc/dnsmasq.d", "test.conf", "echo reload", "")

			got, err := client.parseConfigContent(tt.content)
			if err != nil {
				t.Fatalf("parseConfigContent() error = %v", err)
			}

			if len(got) != len(tt.want) {
				t.Errorf("parseConfigContent() returned %d records, want %d", len(got), len(tt.want))
				return
			}

			for i, record := range got {
				if record.Hostname != tt.want[i].Hostname {
					t.Errorf("record[%d].Hostname = %v, want %v", i, record.Hostname, tt.want[i].Hostname)
				}
				if record.Type != tt.want[i].Type {
					t.Errorf("record[%d].Type = %v, want %v", i, record.Type, tt.want[i].Type)
				}
				if record.Target != tt.want[i].Target {
					t.Errorf("record[%d].Target = %v, want %v", i, record.Target, tt.want[i].Target)
				}
			}
		})
	}
}

func TestClient_ParseConfigContent_WithZoneFilter(t *testing.T) {
	content := `address=/app.example.com/10.1.20.210
address=/app.other.com/10.1.20.211
cname=www.example.com,app.example.com
`

	client := NewClient("/etc/dnsmasq.d", "test.conf", "echo reload", "example.com")

	got, err := client.parseConfigContent(content)
	if err != nil {
		t.Fatalf("parseConfigContent() error = %v", err)
	}

	// Should only return records for example.com zone
	if len(got) != 2 {
		t.Errorf("parseConfigContent() returned %d records, want 2 (filtered)", len(got))
	}

	for _, record := range got {
		if !strings.HasSuffix(record.Hostname, "example.com") {
			t.Errorf("record %s should have been filtered out", record.Hostname)
		}
	}
}

func TestClient_List(t *testing.T) {
	tests := []struct {
		name      string
		fileData  string
		fileExist bool
		wantCount int
		wantErr   bool
	}{
		{
			name:      "file exists with records",
			fileData:  "address=/app.example.com/10.1.20.210\n",
			fileExist: true,
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "file does not exist",
			fileExist: false,
			wantCount: 0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := newMockFileSystem()
			if tt.fileExist {
				mockFS.files["/etc/dnsmasq.d/test.conf"] = []byte(tt.fileData)
			}

			client := NewClient("/etc/dnsmasq.d", "test.conf", "echo reload", "",
				WithFileSystem(mockFS))

			records, err := client.List(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("List() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(records) != tt.wantCount {
				t.Errorf("List() returned %d records, want %d", len(records), tt.wantCount)
			}
		})
	}
}

func TestClient_FormatRecord(t *testing.T) {
	tests := []struct {
		name    string
		record  dnsmasqRecord
		want    string
		wantErr bool
	}{
		{
			name: "A record",
			record: dnsmasqRecord{
				Hostname: "app.example.com",
				Type:     provider.RecordTypeA,
				Target:   "10.1.20.210",
			},
			want:    "address=/app.example.com/10.1.20.210",
			wantErr: false,
		},
		{
			name: "AAAA record",
			record: dnsmasqRecord{
				Hostname: "app.example.com",
				Type:     provider.RecordTypeAAAA,
				Target:   "fd00::1",
			},
			want:    "address=/app.example.com/fd00::1",
			wantErr: false,
		},
		{
			name: "CNAME record",
			record: dnsmasqRecord{
				Hostname: "www.example.com",
				Type:     provider.RecordTypeCNAME,
				Target:   "app.example.com",
			},
			want:    "cname=www.example.com,app.example.com",
			wantErr: false,
		},
		{
			name: "unsupported type",
			record: dnsmasqRecord{
				Hostname: "example.com",
				Type:     provider.RecordTypeTXT,
				Target:   "text",
			},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient("/etc/dnsmasq.d", "test.conf", "echo reload", "")

			got, err := client.formatRecord(tt.record)
			if (err != nil) != tt.wantErr {
				t.Errorf("formatRecord() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got != tt.want {
				t.Errorf("formatRecord() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClient_Create(t *testing.T) {
	mockFS := newMockFileSystem()
	mockFS.dirs["/etc/dnsmasq.d"] = true

	client := NewClient("/etc/dnsmasq.d", "test.conf", "echo reload", "",
		WithFileSystem(mockFS))

	ctx := context.Background()

	// Create first record
	err := client.Create(ctx, dnsmasqRecord{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.1.20.210",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Verify file content
	content := string(mockFS.files["/etc/dnsmasq.d/test.conf"])
	if !strings.Contains(content, "address=/app.example.com/10.1.20.210") {
		t.Errorf("file should contain the record, got: %s", content)
	}

	// Create second record
	err = client.Create(ctx, dnsmasqRecord{
		Hostname: "other.example.com",
		Type:     provider.RecordTypeCNAME,
		Target:   "app.example.com",
	})
	if err != nil {
		t.Fatalf("Create() second record error = %v", err)
	}

	content = string(mockFS.files["/etc/dnsmasq.d/test.conf"])
	if !strings.Contains(content, "cname=other.example.com,app.example.com") {
		t.Errorf("file should contain second record, got: %s", content)
	}
}

func TestClient_Delete(t *testing.T) {
	mockFS := newMockFileSystem()
	mockFS.files["/etc/dnsmasq.d/test.conf"] = []byte(`# Managed by dnsweaver
address=/app.example.com/10.1.20.210
address=/other.example.com/10.1.20.211
`)

	client := NewClient("/etc/dnsmasq.d", "test.conf", "echo reload", "",
		WithFileSystem(mockFS))

	ctx := context.Background()

	// Delete one record
	err := client.Delete(ctx, dnsmasqRecord{
		Hostname: "app.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.1.20.210",
	})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	content := string(mockFS.files["/etc/dnsmasq.d/test.conf"])
	if strings.Contains(content, "address=/app.example.com/10.1.20.210") {
		t.Errorf("file should not contain deleted record, got: %s", content)
	}
	if !strings.Contains(content, "address=/other.example.com/10.1.20.211") {
		t.Errorf("file should still contain other record, got: %s", content)
	}
}

func TestClient_Delete_NonExistent(t *testing.T) {
	mockFS := newMockFileSystem()
	mockFS.files["/etc/dnsmasq.d/test.conf"] = []byte("address=/other.example.com/10.1.20.211\n")

	client := NewClient("/etc/dnsmasq.d", "test.conf", "echo reload", "",
		WithFileSystem(mockFS))

	// Delete record that doesn't exist - should not error
	err := client.Delete(context.Background(), dnsmasqRecord{
		Hostname: "notexist.example.com",
		Type:     provider.RecordTypeA,
		Target:   "10.1.20.210",
	})
	if err != nil {
		t.Errorf("Delete() should not error for non-existent record, got: %v", err)
	}
}

func TestClient_WriteRecords(t *testing.T) {
	mockFS := newMockFileSystem()
	mockFS.dirs["/etc/dnsmasq.d"] = true

	client := NewClient("/etc/dnsmasq.d", "test.conf", "echo reload", "",
		WithFileSystem(mockFS))

	records := []dnsmasqRecord{
		{Hostname: "app.example.com", Type: provider.RecordTypeA, Target: "10.1.20.210"},
		{Hostname: "www.example.com", Type: provider.RecordTypeCNAME, Target: "app.example.com"},
	}

	err := client.WriteRecords(context.Background(), records)
	if err != nil {
		t.Fatalf("WriteRecords() error = %v", err)
	}

	content := string(mockFS.files["/etc/dnsmasq.d/test.conf"])
	if !strings.Contains(content, "address=/app.example.com/10.1.20.210") {
		t.Errorf("file should contain A record")
	}
	if !strings.Contains(content, "cname=www.example.com,app.example.com") {
		t.Errorf("file should contain CNAME record")
	}
	if !strings.Contains(content, "# Managed by dnsweaver") {
		t.Errorf("file should contain header comment")
	}
}

func TestClient_ParseLine(t *testing.T) {
	client := NewClient("/etc/dnsmasq.d", "test.conf", "echo reload", "")

	tests := []struct {
		name     string
		line     string
		wantType provider.RecordType
		wantHost string
		wantTgt  string
		wantNil  bool
	}{
		{
			name:     "IPv4 address",
			line:     "address=/test.example.com/192.168.1.1",
			wantType: provider.RecordTypeA,
			wantHost: "test.example.com",
			wantTgt:  "192.168.1.1",
		},
		{
			name:     "IPv6 address",
			line:     "address=/test.example.com/2001:db8::1",
			wantType: provider.RecordTypeAAAA,
			wantHost: "test.example.com",
			wantTgt:  "2001:db8::1",
		},
		{
			name:     "CNAME",
			line:     "cname=alias.example.com,target.example.com",
			wantType: provider.RecordTypeCNAME,
			wantHost: "alias.example.com",
			wantTgt:  "target.example.com",
		},
		{
			name:    "unknown directive",
			line:    "server=8.8.8.8",
			wantNil: true,
		},
		{
			name:    "comment",
			line:    "# this is a comment",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record, err := client.parseLine(tt.line)
			if err != nil && !tt.wantNil {
				t.Errorf("parseLine() unexpected error = %v", err)
				return
			}

			if tt.wantNil {
				if record != nil {
					t.Errorf("parseLine() should return nil for %q", tt.line)
				}
				return
			}

			if record == nil {
				t.Errorf("parseLine() returned nil, want record")
				return
			}

			if record.Type != tt.wantType {
				t.Errorf("parseLine() type = %v, want %v", record.Type, tt.wantType)
			}
			if record.Hostname != tt.wantHost {
				t.Errorf("parseLine() hostname = %v, want %v", record.Hostname, tt.wantHost)
			}
			if record.Target != tt.wantTgt {
				t.Errorf("parseLine() target = %v, want %v", record.Target, tt.wantTgt)
			}
		})
	}
}
