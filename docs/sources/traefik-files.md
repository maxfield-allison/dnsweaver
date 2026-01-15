# Traefik File Provider

dnsweaver can read Traefik configuration files to discover hostnames, enabling DNS management for non-Docker workloads.

## Use Cases

- Kubernetes workloads with Traefik ingress config files
- Static routes in Traefik file provider
- Non-Docker services with Traefik routing
- External services routed through Traefik

## Configuration

Enable the Traefik file source:

```yaml
environment:
  - DNSWEAVER_SOURCES=traefik,traefik-file

  # File source settings
  - DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS=/config/traefik
  - DNSWEAVER_SOURCE_TRAEFIK_FILE_PATTERN=*.yml,*.yaml,*.toml
  - DNSWEAVER_SOURCE_TRAEFIK_POLL_INTERVAL=60s
volumes:
  - /path/to/traefik/config:/config/traefik:ro
```

## Configuration Reference

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS` | *(none)* | Comma-separated paths to watch |
| `DNSWEAVER_SOURCE_TRAEFIK_FILE_PATTERN` | `*.yml,*.yaml,*.toml` | File patterns to match |
| `DNSWEAVER_SOURCE_TRAEFIK_POLL_INTERVAL` | `60s` | How often to re-scan files |
| `DNSWEAVER_SOURCE_TRAEFIK_WATCH_METHOD` | `auto` | `auto`, `inotify`, or `poll` |

## Supported File Formats

### YAML

```yaml
# /config/traefik/apps.yml
http:
  routers:
    my-app:
      rule: Host(`app.example.com`)
      service: my-app
      entryPoints:
        - web
```

### TOML

```toml
# /config/traefik/apps.toml
[http.routers.my-app]
  rule = "Host(`app.example.com`)"
  service = "my-app"
  entryPoints = ["web"]
```

## Hostname Extraction

dnsweaver parses Traefik router rules to extract hostnames:

| Rule | Extracted Hostnames |
|------|---------------------|
| `Host(\`app.example.com\`)` | `app.example.com` |
| `Host(\`a.example.com\`) \|\| Host(\`b.example.com\`)` | `a.example.com`, `b.example.com` |
| `Host(\`app.example.com\`) && PathPrefix(\`/api\`)` | `app.example.com` |
| `HostRegexp(\`{subdomain:[a-z]+}.example.com\`)` | *(not extracted - too dynamic)* |

## File Watching

### inotify (Linux)

Uses filesystem events for instant detection:

```yaml
- DNSWEAVER_SOURCE_TRAEFIK_WATCH_METHOD=inotify
```

### Polling

Periodically scans for changes (works on all platforms):

```yaml
- DNSWEAVER_SOURCE_TRAEFIK_WATCH_METHOD=poll
- DNSWEAVER_SOURCE_TRAEFIK_POLL_INTERVAL=30s
```

### Auto (Default)

Uses inotify if available, falls back to polling.

## Directory Structure

Mount your Traefik configuration directory:

```
/config/traefik/
├── dynamic/
│   ├── apps.yml
│   ├── services.yml
│   └── middlewares.yml
└── traefik.yml  # Static config (typically not needed)
```

Configure dnsweaver to watch the dynamic config:

```yaml
- DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS=/config/traefik/dynamic
```

## Multiple Paths

Watch multiple directories:

```yaml
- DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS=/config/traefik,/config/external-routes
```

## Combining with Docker Source

Use file and Docker sources together:

```yaml
- DNSWEAVER_SOURCES=traefik,traefik-file
```

This enables DNS for both:
- Docker containers with Traefik labels
- Static routes in Traefik config files

## Troubleshooting

### Files Not Found

Check path accessibility:

```bash
docker exec dnsweaver ls -la /config/traefik
```

### Changes Not Detected

Check watch method and interval:

```yaml
- DNSWEAVER_LOG_LEVEL=debug  # See file scan logs
```

### YAML Parse Errors

Validate your Traefik config:

```bash
cat /path/to/config.yml | python3 -c "import yaml, sys; yaml.safe_load(sys.stdin)"
```
