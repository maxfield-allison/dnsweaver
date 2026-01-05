# dnsweaver

[![Release](https://img.shields.io/github/v/release/maxfield-allison/dnsweaver?style=flat-square)](https://github.com/maxfield-allison/dnsweaver/releases)
[![Docker Pulls](https://img.shields.io/docker/pulls/maxamill/dnsweaver?style=flat-square)](https://hub.docker.com/r/maxamill/dnsweaver)
[![License](https://img.shields.io/github/license/maxfield-allison/dnsweaver?style=flat-square)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/maxfield-allison/dnsweaver?style=flat-square)](go.mod)

**Automatic DNS record management for Docker containers with multi-provider support.**

dnsweaver watches Docker events and automatically creates and deletes DNS records for services with reverse proxy labels (Traefik, etc.). Unlike single-provider tools, dnsweaver supports **split-horizon DNS** and **multiple DNS providers** simultaneously.

## Features

- **Multi-Provider Support**: Route different domains to different DNS providers
- **Split-Horizon DNS**: Internal and external records from the same container labels
- **Docker and Swarm Support**: Works with standalone Docker and Docker Swarm clusters
- **Traefik Integration**: Parses `traefik.http.routers.*.rule` labels to extract hostnames
- **A and CNAME Records**: Full record type support for flexible DNS configuration
- **Real-time Sync**: Watches Docker events and updates records instantly
- **Startup Reconciliation**: Full sync on startup ensures consistency
- **Prometheus Metrics**: Full observability with `dnsweaver_*` metrics
- **Secrets Support**: Docker secrets compatible via `_FILE` suffix variables
- **Health Endpoints**: `/health`, `/ready`, and `/metrics` for monitoring
- **Multi-arch Images**: Supports linux/amd64 and linux/arm64

## Quick Start

### Docker Hub

```bash
docker pull maxamill/dnsweaver:latest
```

### GitHub Container Registry

```bash
docker pull ghcr.io/maxfield-allison/dnsweaver:latest
```

### Docker Compose Example

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    restart: unless-stopped
    environment:
      # Provider configuration
      - DNSWEAVER_PROVIDERS=internal-dns,public-dns

      # Internal DNS (Technitium)
      - DNSWEAVER_INTERNAL_DNS_TYPE=technitium
      - DNSWEAVER_INTERNAL_DNS_URL=http://dns.internal:5380
      - DNSWEAVER_INTERNAL_DNS_TOKEN_FILE=/run/secrets/technitium_token
      - DNSWEAVER_INTERNAL_DNS_ZONE=home.example.com
      - DNSWEAVER_INTERNAL_DNS_RECORD_TYPE=A
      - DNSWEAVER_INTERNAL_DNS_TARGET=10.0.0.100
      - DNSWEAVER_INTERNAL_DNS_DOMAINS=*.home.example.com

      # Public DNS (Cloudflare) - coming in v0.2.0
      # - DNSWEAVER_PUBLIC_DNS_TYPE=cloudflare
      # - DNSWEAVER_PUBLIC_DNS_DOMAINS=*.example.com
      # - DNSWEAVER_PUBLIC_DNS_EXCLUDE_DOMAINS=*.home.example.com
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    secrets:
      - technitium_token
    ports:
      - "8080:8080"

secrets:
  technitium_token:
    external: true
```

### How It Works

1. A container starts with a Traefik label:
   ```yaml
   labels:
     - "traefik.http.routers.myapp.rule=Host(`myapp.home.example.com`)"
   ```

2. dnsweaver matches `myapp.home.example.com` against provider domain patterns

3. The matching provider creates the appropriate DNS record:
   - **A record**: `myapp.home.example.com → 10.0.0.100`
   - **CNAME record**: `myapp.example.com → example.com`

4. When the container stops, the DNS record is automatically deleted

## Configuration

All configuration is via environment variables with the `DNSWEAVER_` prefix. Variables support the `_FILE` suffix for Docker secrets.

### Global Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_LOG_LEVEL` | `info` | Logging level: debug, info, warn, error |
| `DNSWEAVER_LOG_FORMAT` | `json` | Log format: json, text |
| `DNSWEAVER_DRY_RUN` | `false` | Log changes without applying |
| `DNSWEAVER_DEFAULT_TTL` | `300` | Default TTL for DNS records |
| `DNSWEAVER_RECONCILE_INTERVAL` | `60s` | Full reconciliation interval |
| `DNSWEAVER_HEALTH_PORT` | `8080` | Port for health/metrics endpoints |

### Docker Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker host |
| `DNSWEAVER_DOCKER_MODE` | `auto` | Mode: auto, swarm, standalone |

### Provider Configuration

Providers are configured using an explicit instance model:

```bash
# List of provider instance names (order = priority)
DNSWEAVER_PROVIDERS=internal-dns,public-dns

# Each instance requires TYPE and provider-specific settings
DNSWEAVER_{NAME}_TYPE=technitium|cloudflare|webhook
DNSWEAVER_{NAME}_RECORD_TYPE=A|CNAME
DNSWEAVER_{NAME}_TARGET=<ip-or-hostname>
DNSWEAVER_{NAME}_DOMAINS=*.example.com
DNSWEAVER_{NAME}_EXCLUDE_DOMAINS=*.internal.example.com
DNSWEAVER_{NAME}_TTL=300
```

### Domain Matching

dnsweaver supports both **glob patterns** (default) and **regex** (opt-in):

**Glob patterns:**
```bash
DNSWEAVER_INTERNAL_DNS_DOMAINS=*.home.example.com
DNSWEAVER_INTERNAL_DNS_EXCLUDE_DOMAINS=admin.home.example.com
```

**Regex patterns:**
```bash
DNSWEAVER_INTERNAL_DNS_DOMAINS_REGEX=^[a-z0-9-]+\.home\.example\.com$
```

### Provider-Specific Settings

#### Technitium

| Variable | Required | Description |
|----------|----------|-------------|
| `DNSWEAVER_{NAME}_URL` | Yes | Technitium API URL |
| `DNSWEAVER_{NAME}_TOKEN` | Yes* | API token (*or use `_FILE`) |
| `DNSWEAVER_{NAME}_ZONE` | Yes | DNS zone to manage |

## Endpoints

| Endpoint | Description |
|----------|-------------|
| `/health` | Always 200 if process is running |
| `/ready` | 503 if any provider is unreachable, 200 if healthy |
| `/metrics` | Prometheus metrics |

## Roadmap

- **v0.1.0**: Technitium provider, Traefik source, multi-provider routing
- **v0.2.0**: Cloudflare provider, webhook provider
- **v0.3.0**: Caddy source, nginx-proxy source
- **v0.4.0**: Pi-hole provider, PowerDNS provider
- **v1.0.0**: Kubernetes source

## Related Projects

- [technitium-companion](https://github.com/maxfield-allison/technitium-companion) - Single-provider predecessor focused on Technitium

## License

MIT License - see [LICENSE](LICENSE) for details
