# Development Setup

This guide covers setting up your local development environment for contributing to dnsweaver.

## Prerequisites

- **Go 1.24+** — [Download](https://go.dev/dl/)
- **Docker** — For testing and running dependencies
- **golangci-lint** — [Installation](https://golangci-lint.run/usage/install/)
- **Make** — For running build targets

## Project Structure

```
dnsweaver/
├── cmd/
│   └── dnsweaver/          # Main application entry point
├── internal/
│   ├── config/             # Configuration loading
│   ├── docker/             # Docker client and event handling
│   ├── reconciler/         # Core reconciliation logic
│   └── sources/            # Hostname extraction sources
├── pkg/
│   ├── provider/           # Provider interface definitions
│   └── httputil/           # HTTP client utilities
├── providers/              # DNS provider implementations
│   ├── cloudflare/
│   ├── dnsmasq/
│   ├── pihole/
│   ├── technitium/
│   └── webhook/
└── docs/                   # Documentation (MkDocs)
```

## Building

```bash
# Build the binary
make build

# Build for specific platform
GOOS=linux GOARCH=amd64 make build

# Build Docker image
make docker-build
```

## Running Tests

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Run linter
make lint

# Run specific package tests
go test ./providers/technitium/...

# Run with verbose output
go test -v ./...
```

## Running Locally

### Option 1: Environment Variables

```bash
export DNSWEAVER_INSTANCES=test
export DNSWEAVER_TEST_TYPE=webhook
export DNSWEAVER_TEST_URL=http://localhost:8888
export DNSWEAVER_TEST_RECORD_TYPE=A
export DNSWEAVER_TEST_TARGET=10.0.0.1
export DNSWEAVER_TEST_DOMAINS=*.test.local

go run ./cmd/dnsweaver
```

### Option 2: Docker Compose

Create a `docker-compose.dev.yml`:

```yaml
services:
  dnsweaver:
    build: .
    environment:
      - DNSWEAVER_LOG_LEVEL=debug
      - DNSWEAVER_LOG_FORMAT=text
      - DNSWEAVER_INSTANCES=test
      - DNSWEAVER_TEST_TYPE=webhook
      - DNSWEAVER_TEST_URL=http://webhook-receiver:8080
      - DNSWEAVER_TEST_RECORD_TYPE=A
      - DNSWEAVER_TEST_TARGET=10.0.0.1
      - DNSWEAVER_TEST_DOMAINS=*.test.local
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro

  webhook-receiver:
    image: traefik/whoami
    ports:
      - "8080:80"

  test-container:
    image: nginx
    labels:
      - "traefik.http.routers.test.rule=Host(`test.test.local`)"
```

```bash
docker compose -f docker-compose.dev.yml up --build
```

## Code Style

### Go Conventions

- Follow standard Go conventions and idioms
- Use `gofmt` for formatting (automatic with most editors)
- Run `golangci-lint` before committing
- Keep functions focused and testable

### Naming

- Use clear, descriptive names
- Prefer `CreateRecord` over `Create` when context isn't obvious
- Interface implementations should be obvious from type names

### Error Handling

```go
// ✅ Good: Wrap errors with context
if err := client.CreateRecord(ctx, record); err != nil {
    return fmt.Errorf("creating record %s: %w", record.Hostname, err)
}

// ❌ Avoid: Bare error returns
if err := client.CreateRecord(ctx, record); err != nil {
    return err
}
```

### Logging

Use structured logging with `slog`:

```go
p.logger.Info("created record",
    slog.String("provider", p.name),
    slog.String("hostname", record.Hostname),
    slog.String("type", string(record.Type)),
)

p.logger.Error("failed to create record",
    slog.String("hostname", record.Hostname),
    slog.Any("error", err),
)
```

## Testing Patterns

### Table-Driven Tests

```go
func TestConfig_Validate(t *testing.T) {
    tests := []struct {
        name    string
        config  Config
        wantErr bool
    }{
        {
            name:    "valid config",
            config:  Config{URL: "http://localhost", Token: "test"},
            wantErr: false,
        },
        {
            name:    "missing URL",
            config:  Config{Token: "test"},
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
```

### Mock HTTP Servers

```go
func TestClient_CreateRecord(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method == "POST" && r.URL.Path == "/api/records" {
            w.WriteHeader(http.StatusCreated)
            return
        }
        w.WriteHeader(http.StatusNotFound)
    }))
    defer server.Close()

    client := NewClient(server.URL, "test-token")
    err := client.CreateRecord(context.Background(), record)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
}
```

## Debugging

### Enable Debug Logging

```bash
DNSWEAVER_LOG_LEVEL=debug DNSWEAVER_LOG_FORMAT=text go run ./cmd/dnsweaver
```

### Use Delve

```bash
# Install delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug
dlv debug ./cmd/dnsweaver -- [args]
```

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make build` | Build binary |
| `make test` | Run tests |
| `make lint` | Run linter |
| `make docker-build` | Build Docker image |
| `make clean` | Clean build artifacts |

## IDE Setup

### VS Code

Recommended extensions:
- Go (official)
- Docker
- YAML

Recommended settings (`.vscode/settings.json`):
```json
{
    "go.lintTool": "golangci-lint",
    "go.lintFlags": ["--fast"],
    "editor.formatOnSave": true,
    "[go]": {
        "editor.defaultFormatter": "golang.go"
    }
}
```

### GoLand

- Enable "Format on Save"
- Configure golangci-lint as external tool
- Set up Docker integration for testing
