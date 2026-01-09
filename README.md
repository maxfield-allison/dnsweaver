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
- **Socket Proxy Compatible**: Connect via TCP to a Docker socket proxy for improved security
- **Traefik Integration**: Parses `traefik.http.routers.*.rule` labels to extract hostnames
- **Static File Discovery**: Parse Traefik dynamic configuration files (YAML and TOML) for hostnames not defined in container labels
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
      # Define instance names (you choose these, they're arbitrary identifiers)
      - DNSWEAVER_INSTANCES=internal-dns,public-dns

      # Internal DNS (Technitium) - instance name "internal-dns" → prefix "INTERNAL_DNS"
      - DNSWEAVER_INTERNAL_DNS_TYPE=technitium
      - DNSWEAVER_INTERNAL_DNS_URL=http://dns.internal:5380
      - DNSWEAVER_INTERNAL_DNS_TOKEN_FILE=/run/secrets/technitium_token
      - DNSWEAVER_INTERNAL_DNS_ZONE=home.example.com
      - DNSWEAVER_INTERNAL_DNS_RECORD_TYPE=A
      - DNSWEAVER_INTERNAL_DNS_TARGET=10.0.0.100
      - DNSWEAVER_INTERNAL_DNS_DOMAINS=*.home.example.com

      # Public DNS (Cloudflare)
      - DNSWEAVER_PUBLIC_DNS_TYPE=cloudflare
      - DNSWEAVER_PUBLIC_DNS_TOKEN_FILE=/run/secrets/cloudflare_token
      - DNSWEAVER_PUBLIC_DNS_ZONE=example.com
      - DNSWEAVER_PUBLIC_DNS_RECORD_TYPE=CNAME
      - DNSWEAVER_PUBLIC_DNS_TARGET=proxy.example.com
      - DNSWEAVER_PUBLIC_DNS_DOMAINS=*.example.com
      - DNSWEAVER_PUBLIC_DNS_EXCLUDE_DOMAINS=*.home.example.com
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    secrets:
      - technitium_token
      - cloudflare_token
    ports:
      - "8080:8080"

secrets:
  technitium_token:
    external: true
  cloudflare_token:
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
   - **A record**: `myapp.home.example.com → 10.0.0.100` (direct IP)
   - **CNAME record**: `myapp.example.com → docker-host.example.com` (alias to target hostname)

4. When the container stops, the DNS record is automatically deleted

> **Note:** dnsweaver only manages records it creates. Your existing DNS records (like the A record for your docker host) are never modified or deleted — ownership is tracked via TXT records. By default, dnsweaver will **not** adopt existing DNS records; if a record already exists with the correct target but no ownership TXT, dnsweaver skips it. Set `DNSWEAVER_ADOPT_EXISTING=true` to have dnsweaver take ownership of matching records. You can also run with `DNSWEAVER_DRY_RUN=true` to see what changes would be made without actually modifying DNS.

### Record Types and Targets

The `RECORD_TYPE` and `TARGET` settings control what DNS records are created:

| Record Type | TARGET Value | Result | Use Case |
|-------------|--------------|--------|----------|
| `A` | IP address (e.g., `10.0.0.100`) | Direct IP resolution | Internal DNS, split-horizon |
| `CNAME` | Hostname (e.g., `ingress.example.com`) | Alias to another name | Public DNS via reverse proxy |

**Example scenarios:**

- **Single Docker host:** All service subdomains CNAME to your docker host
  ```bash
  DNSWEAVER_INTERNAL_DNS_RECORD_TYPE=CNAME
  DNSWEAVER_INTERNAL_DNS_TARGET=docker-host.example.com
  DNSWEAVER_INTERNAL_DNS_DOMAINS=*.example.com
  # app1.example.com → CNAME → docker-host.example.com
  # app2.example.com → CNAME → docker-host.example.com
  ```

- **Dedicated ingress/reverse proxy:** All services CNAME to a shared ingress hostname
  ```bash
  DNSWEAVER_PUBLIC_DNS_TARGET=ingress.example.com
  ```

- **Multiple Docker hosts:** Each dnsweaver instance points to its own host
  ```bash
  # On docker-host-1
  DNSWEAVER_PUBLIC_DNS_TARGET=docker1.example.com
  DNSWEAVER_PUBLIC_DNS_DOMAINS=*.docker1.example.com

  # On docker-host-2
  DNSWEAVER_PUBLIC_DNS_TARGET=docker2.example.com
  DNSWEAVER_PUBLIC_DNS_DOMAINS=*.docker2.example.com
  ```

- **Internal A records:** Point directly to a load balancer VIP
  ```bash
  DNSWEAVER_INTERNAL_DNS_RECORD_TYPE=A
  DNSWEAVER_INTERNAL_DNS_TARGET=10.0.0.100
  ```

## Configuration

All configuration is via environment variables with the `DNSWEAVER_` prefix. Variables support the `_FILE` suffix for Docker secrets.

### Global Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_LOG_LEVEL` | `info` | Logging level: debug, info, warn, error |
| `DNSWEAVER_LOG_FORMAT` | `json` | Log format: json, text |
| `DNSWEAVER_DRY_RUN` | `false` | Log changes without applying |
| `DNSWEAVER_CLEANUP_ORPHANS` | `true` | Delete DNS records when workloads are removed |
| `DNSWEAVER_OWNERSHIP_TRACKING` | `true` | Use TXT records to track record ownership (prevents deletion of manually-created records) |
| `DNSWEAVER_ADOPT_EXISTING` | `false` | Adopt existing DNS records by creating ownership TXT records (see note below) |
| `DNSWEAVER_DEFAULT_TTL` | `300` | Default TTL for DNS records (seconds) |
| `DNSWEAVER_RECONCILE_INTERVAL` | `60s` | Full reconciliation interval |
| `DNSWEAVER_HEALTH_PORT` | `8080` | Port for health/metrics endpoints |

**TTL Handling:**

- TTL (Time To Live) controls how long DNS resolvers cache records
- Default is 300 seconds (5 minutes) — a balance between responsiveness and cache efficiency
- Per-instance TTL can be set with `DNSWEAVER_{NAME}_TTL` to override the global default
- TTL is set at record creation time; changing TTL doesn't update existing records (delete and recreate to change)
- **Cloudflare special case:** Proxied records ignore TTL and use "Automatic" (API shows TTL=1)

### Docker Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker host (socket path or TCP URL) |
| `DNSWEAVER_DOCKER_MODE` | `auto` | Mode: auto, swarm, standalone |

**Socket Proxy Support**

For improved security, dnsweaver can connect to a Docker socket proxy instead of mounting the Docker socket directly:

```yaml
environment:
  - DNSWEAVER_DOCKER_HOST=tcp://socket-proxy:2375
```

This is the recommended approach for production deployments. The socket proxy only needs to expose read-only access to containers, services, and events.

### Instance Configuration

Define named **instances** that each connect to a DNS provider. Instance names are arbitrary identifiers you choose:

```bash
# List of instance names you define (order = priority for domain matching)
DNSWEAVER_INSTANCES=internal-dns,public-dns

# Each instance needs TYPE to specify which provider it uses
# Note: Dashes in names become underscores in env vars
# Example: "internal-dns" → DNSWEAVER_INTERNAL_DNS_*
DNSWEAVER_INTERNAL_DNS_TYPE=technitium    # ← This sets the provider type
DNSWEAVER_INTERNAL_DNS_RECORD_TYPE=A
DNSWEAVER_INTERNAL_DNS_TARGET=10.0.0.100
DNSWEAVER_INTERNAL_DNS_DOMAINS=*.home.example.com
DNSWEAVER_INTERNAL_DNS_TTL=300

DNSWEAVER_PUBLIC_DNS_TYPE=cloudflare
DNSWEAVER_PUBLIC_DNS_RECORD_TYPE=CNAME
DNSWEAVER_PUBLIC_DNS_TARGET=proxy.example.com
DNSWEAVER_PUBLIC_DNS_DOMAINS=*.example.com
DNSWEAVER_PUBLIC_DNS_EXCLUDE_DOMAINS=*.home.example.com
```

> **Note:** `DNSWEAVER_PROVIDERS` is deprecated but still works as an alias for `DNSWEAVER_INSTANCES`.

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

#### Multi-Provider Matching (Split-Horizon DNS)

When a hostname matches multiple providers, dnsweaver creates records in **all matching providers**. This is intentional for split-horizon DNS:

```bash
DNSWEAVER_INSTANCES=internal-dns,public-dns

# Internal DNS: *.example.com → 10.0.0.100 (private IP)
DNSWEAVER_INTERNAL_DNS_DOMAINS=*.example.com
DNSWEAVER_INTERNAL_DNS_TARGET=10.0.0.100

# Public DNS: *.example.com → public.example.com (public CNAME)
DNSWEAVER_PUBLIC_DNS_DOMAINS=*.example.com
DNSWEAVER_PUBLIC_DNS_TARGET=public.example.com
```

With this configuration, `app.example.com` creates records in **both** providers:

- Internal DNS: `app.example.com → A → 10.0.0.100`
- Public DNS: `app.example.com → CNAME → public.example.com`

**To route different subdomains to different providers**, use non-overlapping patterns or `EXCLUDE_DOMAINS`:

```bash
# Internal only: *.internal.example.com
DNSWEAVER_INTERNAL_DNS_DOMAINS=*.internal.example.com

# Public only: *.example.com but NOT internal subdomains
DNSWEAVER_PUBLIC_DNS_DOMAINS=*.example.com
DNSWEAVER_PUBLIC_DNS_EXCLUDE_DOMAINS=*.internal.example.com
```

#### Instance Order (Priority)

The order of instances in `DNSWEAVER_INSTANCES` does **not** affect which providers receive records — all matching providers get records. However, instance order matters for:

1. **Logging**: Actions are logged in instance order
2. **Startup validation**: Providers are initialized in order

### Provider-Specific Settings

#### Technitium

| Variable | Required | Description |
|----------|----------|-------------|
| `DNSWEAVER_{NAME}_URL` | Yes | Technitium API URL |
| `DNSWEAVER_{NAME}_TOKEN` | Yes* | API token (*or use `_FILE`) |
| `DNSWEAVER_{NAME}_ZONE` | Yes | DNS zone to manage |

#### Cloudflare

| Variable | Required | Description |
|----------|----------|-------------|
| `DNSWEAVER_{NAME}_TOKEN` | Yes* | Cloudflare API token (*or use `_FILE`) |
| `DNSWEAVER_{NAME}_ZONE_ID` | Yes** | Zone ID (**or use `ZONE`) |
| `DNSWEAVER_{NAME}_ZONE` | Yes** | Zone name for lookup (**or use `ZONE_ID`) |
| `DNSWEAVER_{NAME}_TTL` | No | Record TTL, default 300 (1 = automatic when proxied) |
| `DNSWEAVER_{NAME}_PROXIED` | No | Enable Cloudflare proxy (default: false) |

**TTL Note:** When `PROXIED=true`, Cloudflare ignores the TTL and uses "Automatic" (displayed as TTL=1 in the API). For unproxied records, Cloudflare requires TTL >= 60 seconds.

**Example:**
```bash
DNSWEAVER_PUBLIC_DNS_TYPE=cloudflare
DNSWEAVER_PUBLIC_DNS_TOKEN_FILE=/run/secrets/cloudflare_token
DNSWEAVER_PUBLIC_DNS_ZONE=example.com
DNSWEAVER_PUBLIC_DNS_PROXIED=false
DNSWEAVER_PUBLIC_DNS_RECORD_TYPE=CNAME
DNSWEAVER_PUBLIC_DNS_TARGET=proxy.example.com
DNSWEAVER_PUBLIC_DNS_DOMAINS=*.example.com
DNSWEAVER_PUBLIC_DNS_EXCLUDE_DOMAINS=*.home.example.com
```

#### Webhook

Generic webhook provider for custom DNS integrations.

| Variable | Required | Description |
|----------|----------|-------------|
| `DNSWEAVER_{NAME}_URL` | Yes | Base URL for webhook endpoint |
| `DNSWEAVER_{NAME}_AUTH_HEADER` | No | Custom auth header name (e.g., `Authorization`) |
| `DNSWEAVER_{NAME}_AUTH_TOKEN` | No* | Auth token value (*or use `_FILE`) |
| `DNSWEAVER_{NAME}_TIMEOUT` | No | HTTP timeout (default: 30s) |
| `DNSWEAVER_{NAME}_RETRIES` | No | Retry attempts (default: 3) |
| `DNSWEAVER_{NAME}_RETRY_DELAY` | No | Base delay between retries (default: 1s) |

**Webhook API Contract:**

dnsweaver sends the following HTTP requests to your webhook:

| Operation | Method | Path | Body |
|-----------|--------|------|------|
| Ping | GET | `/ping` | — |
| List | GET | `/records` | — |
| Create | POST | `/records` | `{"hostname": "...", "type": "A", "value": "...", "ttl": 300}` |
| Delete | DELETE | `/records/{hostname}/{type}` | — |

### Source Configuration

dnsweaver discovers hostnames from Docker container labels by default. Additionally, you can configure **static file discovery** to parse Traefik configuration files for Host rules.

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_SOURCES` | `traefik` | Comma-separated list of source types |
| `DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS` | *(none)* | Comma-separated paths to Traefik config directories or files |
| `DNSWEAVER_SOURCE_TRAEFIK_FILE_PATTERN` | `*.yml,*.yaml,*.toml` | Glob pattern for config files |
| `DNSWEAVER_SOURCE_TRAEFIK_POLL_INTERVAL` | `60s` | How often to re-scan files for changes |
| `DNSWEAVER_SOURCE_TRAEFIK_WATCH_METHOD` | `auto` | File watching method: `auto`, `inotify`, `poll` |

**Example: Static File Discovery**

```yaml
environment:
  # Enable file discovery by specifying paths
  - DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS=/traefik/rules,/traefik/dynamic
  # Default pattern includes: *.yml, *.yaml, *.toml
  # Or specify custom pattern:
  # - DNSWEAVER_SOURCE_TRAEFIK_FILE_PATTERN=*.yml,*.toml
volumes:
  # Mount Traefik config directory
  - /path/to/traefik/rules:/traefik/rules:ro
```

With file discovery enabled, dnsweaver parses Traefik dynamic configuration files for `Host()` rules and creates DNS records for discovered hostnames. This is useful for:
- Hostnames defined in static Traefik files (not container labels)
- External services routed through Traefik
- Pre-provisioning DNS before container deployment

## Endpoints

| Endpoint | Description |
|----------|-------------|
| `/health` | Always 200 if process is running |
| `/ready` | 503 if any provider is unreachable, 200 if healthy |
| `/metrics` | Prometheus metrics |

## Related Projects

- [technitium-companion](https://github.com/maxfield-allison/technitium-companion) - **Superseded by dnsweaver.** Single-provider predecessor for Technitium-only setups. dnsweaver provides the same functionality plus multi-provider support, static file discovery, and improved observability.

## Uninstalling

To stop using dnsweaver:

1. **Stop the container** — No more DNS changes will be made

2. **Choose what to keep:**
   - **Keep DNS records, stop automation:** Delete only the `_dnsweaver.*` TXT ownership records. Your A/CNAME records remain and become manually managed.
   - **Remove everything:** Delete both the TXT ownership records and the A/CNAME records dnsweaver created.

That's it — dnsweaver has no external state beyond the DNS records themselves.

## License

MIT License - see [LICENSE](LICENSE) for details
