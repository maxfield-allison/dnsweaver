# Adding a New Source

This guide describes how to implement a new hostname source for dnsweaver. Sources are responsible for discovering hostnames that should have DNS records created.

## What Are Sources?

Sources extract hostnames from various inputs:

| Source | Description |
|--------|-------------|
| `traefik` | Extract from Traefik container/service labels |
| `dnsweaver` | Extract from native dnsweaver labels |
| `traefik-file` | Parse Traefik dynamic configuration files |

## Source Structure

Sources live in `internal/sources/` with the following pattern:

```
internal/
└── sources/
    ├── source.go           # Source interface definition
    ├── manager.go          # Source manager orchestration
    ├── traefik.go          # Traefik label source
    ├── traefik_test.go
    ├── dnsweaver.go        # Native label source
    ├── dnsweaver_test.go
    ├── file.go             # File-based source
    └── file_test.go
```

## Source Interface

```go
package sources

import (
    "context"
)

// Hostname represents a discovered hostname with metadata
type Hostname struct {
    Name       string            // The hostname (e.g., "app.example.com")
    SourceType string            // Source type that discovered it (e.g., "traefik")
    SourceID   string            // Unique identifier (container ID, file path, etc.)
    Labels     map[string]string // Original labels/metadata
}

// Source discovers hostnames from a specific input type
type Source interface {
    // Name returns the source identifier
    Name() string

    // Type returns the source type for configuration
    Type() string

    // Discover returns all currently known hostnames
    Discover(ctx context.Context) ([]Hostname, error)

    // Watch starts watching for changes, sending updates to the channel
    // Returns when context is cancelled
    Watch(ctx context.Context, updates chan<- []Hostname) error
}
```

## Implementation Steps

### 1. Create Source File

```go
// internal/sources/mysource.go
package sources

import (
    "context"
    "log/slog"
)

type MySource struct {
    name   string
    config *MySourceConfig
    logger *slog.Logger
}

type MySourceConfig struct {
    // Source-specific configuration
    Path     string
    Pattern  string
    Interval time.Duration
}

func NewMySource(name string, config *MySourceConfig, logger *slog.Logger) (*MySource, error) {
    if config == nil {
        return nil, fmt.Errorf("config is required")
    }
    return &MySource{
        name:   name,
        config: config,
        logger: logger,
    }, nil
}

func (s *MySource) Name() string { return s.name }
func (s *MySource) Type() string { return "mysource" }
```

### 2. Implement Discover

The `Discover` method returns all currently known hostnames:

```go
func (s *MySource) Discover(ctx context.Context) ([]Hostname, error) {
    var hostnames []Hostname

    // Your discovery logic here
    // Example: Read from a file, query an API, etc.

    items, err := s.fetchItems(ctx)
    if err != nil {
        return nil, fmt.Errorf("fetching items: %w", err)
    }

    for _, item := range items {
        hostname := s.extractHostname(item)
        if hostname != "" {
            hostnames = append(hostnames, Hostname{
                Name:       hostname,
                SourceType: s.Type(),
                SourceID:   item.ID,
                Labels:     item.Labels,
            })
        }
    }

    s.logger.Debug("discovered hostnames",
        slog.String("source", s.name),
        slog.Int("count", len(hostnames)),
    )

    return hostnames, nil
}
```

### 3. Implement Watch

The `Watch` method enables real-time updates:

```go
func (s *MySource) Watch(ctx context.Context, updates chan<- []Hostname) error {
    ticker := time.NewTicker(s.config.Interval)
    defer ticker.Stop()

    // Initial discovery
    hostnames, err := s.Discover(ctx)
    if err != nil {
        return err
    }
    updates <- hostnames

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            hostnames, err := s.Discover(ctx)
            if err != nil {
                s.logger.Error("discovery failed", slog.Any("error", err))
                continue
            }
            updates <- hostnames
        }
    }
}
```

For event-driven sources (like Docker), use event streams:

```go
func (s *DockerSource) Watch(ctx context.Context, updates chan<- []Hostname) error {
    events, errs := s.client.Events(ctx, types.EventsOptions{})

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case err := <-errs:
            return err
        case event := <-events:
            if s.isRelevant(event) {
                hostnames, err := s.Discover(ctx)
                if err != nil {
                    s.logger.Error("discovery failed", slog.Any("error", err))
                    continue
                }
                updates <- hostnames
            }
        }
    }
}
```

### 4. Register the Source

In the source manager or main.go:

```go
func registerSources(manager *sources.Manager, config *Config) error {
    // ... existing sources ...

    if config.MySourceEnabled {
        source, err := sources.NewMySource("mysource", &sources.MySourceConfig{
            Path:     config.MySourcePath,
            Pattern:  config.MySourcePattern,
            Interval: config.MySourceInterval,
        }, logger)
        if err != nil {
            return err
        }
        manager.Register(source)
    }

    return nil
}
```

### 5. Add Configuration

Add environment variable support:

```go
// internal/config/config.go
type Config struct {
    // ... existing fields ...

    // MySource settings
    MySourceEnabled  bool          `env:"DNSWEAVER_SOURCE_MYSOURCE_ENABLED" default:"false"`
    MySourcePath     string        `env:"DNSWEAVER_SOURCE_MYSOURCE_PATH"`
    MySourcePattern  string        `env:"DNSWEAVER_SOURCE_MYSOURCE_PATTERN" default:"*"`
    MySourceInterval time.Duration `env:"DNSWEAVER_SOURCE_MYSOURCE_INTERVAL" default:"60s"`
}
```

## Hostname Extraction Patterns

### From Labels

```go
func (s *TraefikSource) extractHostnames(labels map[string]string) []string {
    var hostnames []string

    // Match traefik.http.routers.*.rule
    routerPattern := regexp.MustCompile(`^traefik\.http\.routers\.([^.]+)\.rule$`)
    hostPattern := regexp.MustCompile(`Host\(\x60([^` + "`" + `]+)\x60\)`)

    for key, value := range labels {
        if routerPattern.MatchString(key) {
            matches := hostPattern.FindAllStringSubmatch(value, -1)
            for _, match := range matches {
                if len(match) > 1 {
                    hostnames = append(hostnames, match[1])
                }
            }
        }
    }

    return hostnames
}
```

### From Files

```go
func (s *FileSource) extractFromFile(path string) ([]Hostname, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }

    var config TraefikConfig
    if err := yaml.Unmarshal(data, &config); err != nil {
        return nil, err
    }

    var hostnames []Hostname
    for name, router := range config.HTTP.Routers {
        for _, host := range parseHostRule(router.Rule) {
            hostnames = append(hostnames, Hostname{
                Name:       host,
                SourceType: s.Type(),
                SourceID:   fmt.Sprintf("%s#%s", path, name),
            })
        }
    }

    return hostnames, nil
}
```

## Testing

### Unit Tests

```go
func TestMySource_Discover(t *testing.T) {
    source := &MySource{
        name:   "test",
        config: &MySourceConfig{Path: "testdata/hosts.yaml"},
        logger: slog.Default(),
    }

    hostnames, err := source.Discover(context.Background())
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    expected := []string{"app.example.com", "api.example.com"}
    if len(hostnames) != len(expected) {
        t.Errorf("expected %d hostnames, got %d", len(expected), len(hostnames))
    }
}
```

### Integration Tests

```go
func TestMySource_Watch(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    source := NewMySource("test", config, logger)
    updates := make(chan []Hostname, 10)

    go func() {
        if err := source.Watch(ctx, updates); err != nil && err != context.Canceled {
            t.Errorf("watch error: %v", err)
        }
    }()

    // Wait for initial update
    select {
    case hostnames := <-updates:
        if len(hostnames) == 0 {
            t.Error("expected at least one hostname")
        }
    case <-ctx.Done():
        t.Fatal("timeout waiting for update")
    }
}
```

## Documentation

1. Add source to `docs/sources/` with configuration examples
2. Update `configuration/environment.md` with new variables
3. Add to `mkdocs.yml` navigation
4. Update CHANGELOG

## Checklist

- [ ] Source struct with config
- [ ] `Name()` and `Type()` methods
- [ ] `Discover()` implementation
- [ ] `Watch()` implementation
- [ ] Configuration loading
- [ ] Registration in manager
- [ ] Unit tests
- [ ] Integration tests
- [ ] Documentation
- [ ] CHANGELOG entry
