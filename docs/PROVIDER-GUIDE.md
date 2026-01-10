# Adding a New Provider

This guide describes how to implement a new DNS provider for dnsweaver.

## Provider Structure

Each provider lives in `providers/{provider-name}/` with the following files:

```
providers/
└── {provider-name}/
    ├── client.go          # HTTP/API client implementation
    ├── client_test.go     # Client tests with mock HTTP server
    ├── config.go          # Configuration struct and loading
    ├── config_test.go     # Config validation tests
    ├── provider.go        # Provider interface implementation
    └── provider_test.go   # Provider tests
```

## Implementation Checklist

### 1. Config (`config.go`)

```go
package myprovider

import (
	"fmt"
	"os"
	"strings"
)

const DefaultTTL = 300

type Config struct {
	// Provider-specific settings
	URL   string
	Token string
	Zone  string
	TTL   int
}

func (c *Config) Validate() error {
	var errs []string
	if c.URL == "" {
		errs = append(errs, "URL is required")
	}
	if c.Token == "" {
		errs = append(errs, "TOKEN is required")
	}
	// Add other validations...
	if len(errs) > 0 {
		return fmt.Errorf("config validation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

// LoadConfig loads from environment variables.
// Pattern: DNSWEAVER_{INSTANCE_NAME}_{SETTING}
func LoadConfig(instanceName string) (*Config, error) {
	prefix := envPrefix(instanceName)
	config := &Config{
		URL:   getEnv(prefix + "URL"),
		Token: getEnvOrFile(prefix+"TOKEN", prefix+"TOKEN_FILE"),
		Zone:  getEnv(prefix + "ZONE"),
		TTL:   DefaultTTL,
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration for %s: %w", instanceName, err)
	}
	return config, nil
}

// LoadConfigFromMap loads from a map (used by Factory pattern).
func LoadConfigFromMap(instanceName string, m map[string]string) (*Config, error) {
	config := &Config{
		URL:   m["URL"],
		Token: m["TOKEN"],
		Zone:  m["ZONE"],
		TTL:   DefaultTTL,
	}
	// Parse optional numeric fields...
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return config, nil
}

func envPrefix(instanceName string) string {
	normalized := strings.ToUpper(instanceName)
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return "DNSWEAVER_" + normalized + "_"
}

func getEnv(key string) string {
	return os.Getenv(key)
}

func getEnvOrFile(keyDirect, keyFile string) string {
	if v := os.Getenv(keyDirect); v != "" {
		return v
	}
	if path := os.Getenv(keyFile); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}
```

### 2. Client (`client.go`)

The client handles HTTP/API communication. Keep provider-specific API details here.

```go
package myprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	logger     *slog.Logger
}

type ClientOption func(*Client)

func WithLogger(logger *slog.Logger) ClientOption {
	return func(c *Client) {
		if logger != nil {
			c.logger = logger
		}
	}
}

func NewClient(baseURL, token string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) Ping(ctx context.Context) error {
	// Implement connectivity check
	return nil
}

// Add provider-specific methods: ListRecords, CreateRecord, DeleteRecord, etc.
```

### 3. Provider (`provider.go`)

```go
package myprovider

import (
	"context"
	"fmt"
	"log/slog"

	"gitlab.bluewillows.net/root/dnsweaver/pkg/provider"
)

type Provider struct {
	name   string
	zone   string
	ttl    int
	client *Client
	logger *slog.Logger
}

type ProviderOption func(*Provider)

func WithProviderLogger(logger *slog.Logger) ProviderOption {
	return func(p *Provider) {
		if logger != nil {
			p.logger = logger
		}
	}
}

func New(name string, config *Config, opts ...ProviderOption) (*Provider, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}

	p := &Provider{
		name:   name,
		zone:   config.Zone,
		ttl:    config.TTL,
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(p)
	}

	p.client = NewClient(config.URL, config.Token, WithLogger(p.logger))
	return p, nil
}

func NewFromEnv(instanceName string, opts ...ProviderOption) (*Provider, error) {
	config, err := LoadConfig(instanceName)
	if err != nil {
		return nil, err
	}
	return New(instanceName, config, opts...)
}

func NewFromMap(name string, config map[string]string) (*Provider, error) {
	cfg, err := LoadConfigFromMap(name, config)
	if err != nil {
		return nil, err
	}
	return New(name, cfg)
}

func (p *Provider) Name() string { return p.name }
func (p *Provider) Type() string { return "myprovider" }

func (p *Provider) Ping(ctx context.Context) error {
	return p.client.Ping(ctx)
}

func (p *Provider) List(ctx context.Context) ([]provider.Record, error) {
	// Fetch and convert to provider.Record
	return nil, nil
}

func (p *Provider) Create(ctx context.Context, record provider.Record) error {
	// Create record via client
	p.logger.Info("created record",
		slog.String("provider", p.name),
		slog.String("hostname", record.Hostname),
	)
	return nil
}

func (p *Provider) Delete(ctx context.Context, record provider.Record) error {
	// Delete record via client
	p.logger.Info("deleted record",
		slog.String("provider", p.name),
		slog.String("hostname", record.Hostname),
	)
	return nil
}

// Factory returns a provider.Factory for use with the registry.
func Factory() provider.Factory {
	return func(name string, config map[string]string) (provider.Provider, error) {
		return NewFromMap(name, config)
	}
}

// Compile-time interface check
var _ provider.Provider = (*Provider)(nil)
```

### 4. Register the Factory

In `cmd/dnsweaver/main.go`:

```go
import (
	// ...
	"gitlab.bluewillows.net/root/dnsweaver/providers/myprovider"
)

func registerProviderFactories(registry *provider.Registry) {
	// Existing registrations...

	// Register new provider
	registry.RegisterFactory("myprovider", myprovider.Factory())
}
```

## Testing Patterns

### Mock HTTP Server

Use `httptest.NewServer` to mock API responses:

```go
func TestClient_CreateRecord(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/api/records" {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"id": "123"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.CreateRecord(context.Background(), "test.example.com", "A", "10.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

### Factory Test

```go
func TestFactory(t *testing.T) {
	factory := Factory()

	p, err := factory("test", map[string]string{
		"URL":   "http://localhost",
		"TOKEN": "test-token",
		"ZONE":  "example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Name() != "test" {
		t.Errorf("expected name test, got %s", p.Name())
	}
	if p.Type() != "myprovider" {
		t.Errorf("expected type myprovider, got %s", p.Type())
	}
}
```

## Public vs Private DNS Providers

### Public DNS Providers (Cloudflare, Route53, DigitalOcean, etc.)

Characteristics:
- External API access (internet required)
- API rate limiting considerations
- Zone/domain lookup by name or ID
- May have CDN/proxy features
- Authentication via API tokens or cloud credentials

Common configuration:
```bash
DNSWEAVER_{NAME}_TOKEN=...       # API token
DNSWEAVER_{NAME}_ZONE_ID=...     # Or ZONE for name lookup
DNSWEAVER_{NAME}_TTL=300
```

### Private DNS Providers (Technitium, Pi-hole, dnsmasq, unbound)

Characteristics:
- Local network access
- Often self-hosted
- Zone management for internal domains
- May require file writes or control sockets

Common configuration:
```bash
DNSWEAVER_{NAME}_URL=http://dns.local:5380
DNSWEAVER_{NAME}_TOKEN=...
DNSWEAVER_{NAME}_ZONE=home.example.com
```

## Documentation

1. Update `README.md` with provider-specific settings table
2. Add example configuration in "Quick Start" section
3. Update `CHANGELOG.md` with new provider announcement
4. Create issue in GitLab for tracking

## Record Types

Your provider should support these record types as applicable:

| Record Type | Purpose | Target Validation |
|------------|---------|-------------------|
| `A` | IPv4 address record | Valid IPv4 address (e.g., `10.1.20.210`) |
| `AAAA` | IPv6 address record | Valid IPv6 address (e.g., `2001:db8::1` or `fd00::1`) |
| `CNAME` | Canonical name (alias) | Valid hostname (e.g., `target.example.com`) |
| `TXT` | Text record (used for ownership) | String value |
| `SRV` | Service record | Priority (0-65535), weight (0-65535), port (1-65535), target hostname |

### SRV Record Format

SRV records are used for service discovery. The hostname follows the pattern `_service._proto.name`:

- `_minecraft._tcp.example.com` → Minecraft server discovery
- `_sip._tcp.example.com` → SIP server discovery
- `_ldap._tcp.example.com` → LDAP server discovery

SRV records have additional fields beyond standard records:

```go
type SRVData struct {
    Priority uint16 // Lower values = higher priority (0-65535)
    Weight   uint16 // Load balancing among same-priority servers (0-65535)
    Port     uint16 // TCP/UDP port number (1-65535)
}

type Record struct {
    Hostname   string
    Type       RecordType
    Target     string   // Target hostname for SRV records
    TTL        int
    SRV        *SRVData // Only set when Type is SRV
}
```

### Implementation Notes

1. **A and AAAA records** use the same pattern — only the target format differs:
   - `A` → IPv4: `record.Target` must be a valid IPv4 address
   - `AAAA` → IPv6: `record.Target` must be a valid IPv6 address (including shorthand)

2. **IPv6 considerations:**
   - Accept both full (`2001:0db8:0000:0000:0000:0000:0000:0001`) and shorthand (`2001:db8::1`) notation
   - Use `net.ParseIP()` for validation — it handles both
   - Common private IPv6: `fd00::/8` (unique local addresses)

3. **SRV record handling:**
   - Check `record.SRV` is not nil before accessing priority/weight/port
   - Return an error if SRV data is missing for SRV record operations
   - SRV records cannot be proxied (for providers like Cloudflare)

4. **API-specific handling:**
   - Some APIs (like Technitium) use `ipAddress` param for both A and AAAA
   - Others (like Cloudflare) use `content` for the value regardless of type
   - SRV records often require structured data (Cloudflare) or separate params (Technitium)
   - Check your DNS provider's API documentation

### Example Implementation

```go
func (p *Provider) Create(ctx context.Context, record provider.Record) error {
    switch record.Type {
    case provider.RecordTypeA, provider.RecordTypeAAAA:
        return p.client.AddAddressRecord(ctx, record.Hostname, record.Type, record.Target, p.ttl)
    case provider.RecordTypeCNAME:
        return p.client.AddCNAME(ctx, record.Hostname, record.Target, p.ttl)
    case provider.RecordTypeTXT:
        return p.client.AddTXT(ctx, record.Hostname, record.Target, p.ttl)
    case provider.RecordTypeSRV:
        if record.SRV == nil {
            return fmt.Errorf("SRV data is required for SRV records")
        }
        return p.client.AddSRV(ctx, record.Hostname,
            int(record.SRV.Priority), int(record.SRV.Weight), int(record.SRV.Port),
            record.Target, p.ttl)
    default:
        return fmt.Errorf("unsupported record type: %s", record.Type)
    }
}
```

## Checklist Summary

- [ ] `config.go` with Validate(), LoadConfig(), LoadConfigFromMap()
- [ ] `client.go` with Ping(), and record CRUD methods
- [ ] `provider.go` implementing `provider.Provider` interface
- [ ] `Factory()` function for registry
- [ ] Tests for config, client, and provider
- [ ] Factory registered in `main.go`
- [ ] README documentation updated
- [ ] CHANGELOG updated
