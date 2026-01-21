# Environment Variables Reference

All configuration is via environment variables with the `DNSWEAVER_` prefix. Variables support the `_FILE` suffix for Docker secrets.

## Configuration File

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_CONFIG` | *(none)* | Path to YAML configuration file (see [config.example.yml](../config.example.yml)) |

When set, dnsweaver loads configuration from the specified YAML file. Environment variables override file values when both are set.

Alternatively, use the `--config` CLI flag:

```bash
dnsweaver --config /etc/dnsweaver/config.yml
```

## Global Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_INSTANCES` | *(required)* | Comma-separated list of provider instance names |
| `DNSWEAVER_LOG_LEVEL` | `info` | Logging level: `debug`, `info`, `warn`, `error` |
| `DNSWEAVER_LOG_FORMAT` | `json` | Log format: `json`, `text` |
| `DNSWEAVER_DRY_RUN` | `false` | Preview changes without modifying DNS |
| `DNSWEAVER_CLEANUP_ORPHANS` | `true` | Delete DNS records when workloads are removed |
| `DNSWEAVER_CLEANUP_ON_STOP` | `true` | Delete DNS records when containers stop |
| `DNSWEAVER_OWNERSHIP_TRACKING` | `true` | Use TXT records to track record ownership |
| `DNSWEAVER_ADOPT_EXISTING` | `false` | Adopt existing DNS records by creating ownership TXT |
| `DNSWEAVER_DEFAULT_TTL` | `300` | Default TTL for DNS records (seconds) |
| `DNSWEAVER_RECONCILE_INTERVAL` | `60s` | Periodic reconciliation interval |
| `DNSWEAVER_HEALTH_PORT` | `8080` | Port for health/metrics endpoints |

!!! note "Deprecated Variable"
    `DNSWEAVER_PROVIDERS` still works as an alias for `DNSWEAVER_INSTANCES` but is deprecated.

## Docker Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker host (socket path or TCP URL) |
| `DNSWEAVER_DOCKER_MODE` | `auto` | Docker mode: `auto`, `swarm`, `standalone` |

### Socket Proxy Support

For improved security, connect to a Docker socket proxy instead of mounting the Docker socket directly:

```yaml
environment:
  - DNSWEAVER_DOCKER_HOST=tcp://socket-proxy:2375
```

The socket proxy only needs read-only access to containers, services, and events.

## Per-Instance Settings

Replace `{NAME}` with your instance name. For example, instance `internal-dns` uses prefix `INTERNAL_DNS`.

| Variable | Required | Description |
|----------|----------|-------------|
| `DNSWEAVER_{NAME}_TYPE` | Yes | Provider type: `technitium`, `cloudflare`, `pihole`, `dnsmasq`, `webhook` |
| `DNSWEAVER_{NAME}_RECORD_TYPE` | No | Record type: `A`, `AAAA`, `CNAME` (default: `A`) |
| `DNSWEAVER_{NAME}_TARGET` | Yes | Record target (IPv4, IPv6, or hostname) |
| `DNSWEAVER_{NAME}_DOMAINS` | Yes | Glob patterns for matching hostnames |
| `DNSWEAVER_{NAME}_DOMAINS_REGEX` | No | Regex patterns (alternative to glob) |
| `DNSWEAVER_{NAME}_EXCLUDE_DOMAINS` | No | Glob patterns to exclude |
| `DNSWEAVER_{NAME}_TTL` | No | Per-instance TTL override |

## Source Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_SOURCES` | `traefik` | Comma-separated list: `traefik`, `dnsweaver` |
| `DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS` | *(none)* | Paths to Traefik config directories/files |
| `DNSWEAVER_SOURCE_TRAEFIK_FILE_PATTERN` | `*.yml,*.yaml,*.toml` | Glob pattern for config files |
| `DNSWEAVER_SOURCE_TRAEFIK_POLL_INTERVAL` | `60s` | File re-scan interval |
| `DNSWEAVER_SOURCE_TRAEFIK_WATCH_METHOD` | `auto` | Watch method: `auto`, `inotify`, `poll` |

## Provider-Specific Settings

See the individual provider documentation for complete settings:

- [Technitium](../providers/technitium.md)
- [Cloudflare](../providers/cloudflare.md)
- [Pi-hole](../providers/pihole.md)
- [dnsmasq](../providers/dnsmasq.md)
- [Webhook](../providers/webhook.md)
