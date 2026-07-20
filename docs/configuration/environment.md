---
title: Environment Variables Reference
description: Complete reference for every dnsweaver environment variable — instances, providers, sources, TLS, and secrets configuration with the DNSWEAVER_ prefix.
---

# Environment Variables Reference

All configuration is via environment variables with the `DNSWEAVER_` prefix. Variables support the `_FILE` suffix for [secrets management](secrets.md) (Docker secrets, Kubernetes Secrets, or any file-based injection).

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
| `DNSWEAVER_LOG_FILE` | *(empty)* | Path to log file (empty = stdout only) |
| `DNSWEAVER_LOG_MAX_SIZE` | `100` | Max log file size in MB before rotation |
| `DNSWEAVER_LOG_MAX_BACKUPS` | `5` | Number of old log files to keep |
| `DNSWEAVER_LOG_MAX_AGE` | `30` | Days to retain old log files |
| `DNSWEAVER_LOG_COMPRESS` | `true` | Compress rotated log files |
| `DNSWEAVER_DRY_RUN` | `false` | Preview changes without modifying DNS |
| `DNSWEAVER_CLEANUP_ORPHANS` | `true` | Delete DNS records when workloads are removed |
| `DNSWEAVER_CLEANUP_ON_STOP` | `true` | Delete DNS records when containers stop |
| `DNSWEAVER_OWNERSHIP_TRACKING` | `true` | Use TXT records to track record ownership |
| `DNSWEAVER_ADOPT_EXISTING` | `false` | Adopt existing DNS records by creating ownership TXT |
| `DNSWEAVER_DEFAULT_TTL` | `300` | Default TTL for DNS records (seconds) |
| `DNSWEAVER_RECONCILE_INTERVAL` | `60s` | Periodic reconciliation interval |
| `DNSWEAVER_SHUTDOWN_TIMEOUT` | `30s` | Graceful shutdown timeout for in-flight updates |
| `DNSWEAVER_HEALTH_PORT` | `8080` | Port for health/metrics endpoints |

!!! note "Deprecated Variable"
    `DNSWEAVER_PROVIDERS` still works as an alias for `DNSWEAVER_INSTANCES` but is deprecated.

## Docker Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker host (socket path or TCP URL) |
| `DNSWEAVER_DOCKER_MODE` | `auto` | Docker mode: `auto`, `swarm`, `standalone` |
| `DNSWEAVER_DOCKER_GID` | _(unset)_ | Explicitly add the unprivileged `dnsweaver` user to this group GID so it can read a socket whose GID can't be auto-detected (e.g. a root-owned socket on Synology: set `0`). The process still drops privileges; a [socket proxy](../sources/docker.md#socket-proxy-recommended-for-security) is stronger. |
| `DNSWEAVER_DOCKER_CONNECT_TIMEOUT` | `30s` | How long to retry the initial Docker connection before failing hard (Go duration). Set `0` for strict fail-fast (exit on the first error). Useful with a label-driven [socket proxy](../sources/docker.md#socket-proxy-recommended-for-security) that authorizes dnsweaver a few seconds after startup. |

### Socket Proxy Support

For improved security, connect to a Docker socket proxy instead of mounting the Docker socket directly:

```yaml
environment:
  - DNSWEAVER_DOCKER_HOST=tcp://socket-proxy:2375
```

The socket proxy only needs read-only access to containers, services, and events.

## Platform Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_PLATFORM` | `docker` | Platform mode: `docker`, `kubernetes`, `both`, or `none` |
| `DNSWEAVER_INSTANCE_ID` | *(empty)* | Unique instance identifier for multi-instance coordination |

Set `DNSWEAVER_PLATFORM` to control which workload sources are active:

- **`docker`** — Watch Docker containers/services only (default, backward-compatible)
- **`kubernetes`** — Watch Kubernetes Ingress/IngressRoute/HTTPRoute/Service resources only
- **`both`** — Watch both Docker and Kubernetes workloads simultaneously
- **`none`** (alias: `standalone`) — Create no container-runtime client. Use this to run dnsweaver as a bare binary on a host, VM, or LXC where there is no Docker or Kubernetes. You must configure at least one non-container source: a Proxmox VE source (`DNSWEAVER_PROXMOX_URL`), an Incus source (`DNSWEAVER_INCUS_URL` / `DNSWEAVER_INCUS_SOCKET_PATH`), or a file-discovery source (e.g. `DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS`). Startup will fail if `none` is set and no such source is configured.

### Standalone (no Docker or Kubernetes)

To run dnsweaver directly on a host alongside, for example, Technitium DNS, using only the Proxmox VE source:

```bash
DNSWEAVER_PLATFORM=none
DNSWEAVER_SOURCES=proxmox
DNSWEAVER_PROXMOX_URL=https://pve.example.com:8006/
DNSWEAVER_PROXMOX_TOKEN_ID=user@pve!dnsweaver
DNSWEAVER_PROXMOX_TOKEN_SECRET_FILE=/etc/dnsweaver/pve-token

DNSWEAVER_INSTANCES=technitium
DNSWEAVER_TECHNITIUM_TYPE=technitium
DNSWEAVER_TECHNITIUM_URL=http://127.0.0.1:5380
DNSWEAVER_TECHNITIUM_TOKEN_FILE=/etc/dnsweaver/technitium-token
DNSWEAVER_TECHNITIUM_ZONE=example.com
```

With `DNSWEAVER_PLATFORM=none` no Docker or Kubernetes client is created, so dnsweaver no longer fails with `Cannot connect to the Docker daemon` on hosts without a container runtime.

## Kubernetes Settings

These settings are only relevant when `DNSWEAVER_PLATFORM` is `kubernetes` or `both`.

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_K8S_KUBECONFIG` | *(empty)* | Path to kubeconfig file. Empty uses in-cluster config |
| `DNSWEAVER_K8S_NAMESPACES` | *(empty)* | Comma-separated namespace list. Empty watches all namespaces |
| `DNSWEAVER_K8S_WATCH_INGRESS` | `true` | Watch `networking.k8s.io/v1` Ingress resources |
| `DNSWEAVER_K8S_WATCH_INGRESSROUTE` | `true` | Watch `traefik.io/v1alpha1` IngressRoute CRDs |
| `DNSWEAVER_K8S_WATCH_HTTPROUTE` | `true` | Watch `gateway.networking.k8s.io/v1` HTTPRoute CRDs |
| `DNSWEAVER_K8S_WATCH_SERVICES` | `false` | Watch `v1` Service resources (opt-in, can be noisy) |
| `DNSWEAVER_K8S_LABEL_SELECTOR` | *(empty)* | Kubernetes label selector to filter watched resources |
| `DNSWEAVER_K8S_ANNOTATION_FILTER` | *(empty)* | Annotation `key=value` filter for watched resources |

!!! tip "In-Cluster vs External"
    When running inside Kubernetes (recommended), leave `DNSWEAVER_K8S_KUBECONFIG` empty —
    dnsweaver automatically uses the pod's service account token. Set it only for out-of-cluster
    development or testing.

!!! note "RBAC Required"
    Kubernetes mode requires a `ClusterRole` with read access to the resource types you're watching.
    See the [Kubernetes deployment guide](../deployment/kubernetes.md) for ready-to-use RBAC manifests.

## Per-Instance Settings

Replace `{NAME}` with your instance name. For example, instance `internal-dns` uses prefix `INTERNAL_DNS`.

| Variable | Required | Description |
|----------|----------|-------------|
| `DNSWEAVER_{NAME}_TYPE` | Yes | Provider type: `technitium`, `cloudflare`, `ovh`, `rfc2136`, `powerdns`, `pihole`, `dnsmasq`, `adguard`, `webhook` |
| `DNSWEAVER_{NAME}_RECORD_TYPE` | No | Record type: `A`, `AAAA`, `CNAME` (default: `A`) |
| `DNSWEAVER_{NAME}_TARGET` | Cond. | Record target (IPv4, IPv6, or hostname). Required unless `TARGET_MODE` is set, in which case it is an optional fallback. |
| `DNSWEAVER_{NAME}_TARGET_MODE` | No | Resolve the target dynamically instead of using a literal `TARGET`. One of `public` or `interface:<name>`. See [Dynamic Targets](#dynamic-targets). |
| `DNSWEAVER_{NAME}_TARGET_REFRESH_INTERVAL` | No | How often to re-resolve a dynamic target (Go duration, e.g. `5m`). Default: `5m`. Only used with `TARGET_MODE`. |
| `DNSWEAVER_{NAME}_TARGET_PUBLIC_ENDPOINTS` | No | Comma-separated override of the public-IP echo endpoints used by `TARGET_MODE=public`. |
| `DNSWEAVER_{NAME}_DOMAINS` | Yes | Glob patterns for matching hostnames |
| `DNSWEAVER_{NAME}_DOMAINS_REGEX` | No | Regex patterns (alternative to glob) |
| `DNSWEAVER_{NAME}_EXCLUDE_DOMAINS` | No | Glob patterns to exclude |
| `DNSWEAVER_{NAME}_EXCLUDE_DOMAINS_REGEX` | No | Regex patterns to exclude (alternative to glob) |
| `DNSWEAVER_{NAME}_ENTRYPOINTS` | No | Comma-separated Traefik entrypoint allowlist for this instance (e.g. `webA,webB`). Only routers bound to one of these entrypoints will be matched. Routers without entrypoint metadata always match (wildcard). See [Traefik source](../sources/swarm.md#per-entrypoint-routing). |
| `DNSWEAVER_{NAME}_TTL` | No | Per-instance TTL override |
| `DNSWEAVER_{NAME}_MODE` | No | Operational mode: `managed` (default), `authoritative`, `additive` |
| `DNSWEAVER_{NAME}_TLS_CA_FILE` | No | Path to a PEM CA bundle appended to system roots (private CAs). Supports `_FILE` suffix. |
| `DNSWEAVER_{NAME}_TLS_CERT_FILE` | No | Path to PEM client certificate for mutual TLS. Must be set with `TLS_KEY_FILE`. |
| `DNSWEAVER_{NAME}_TLS_KEY_FILE` | No | Path to PEM client private key for mutual TLS. Must be set with `TLS_CERT_FILE`. |
| `DNSWEAVER_{NAME}_TLS_SERVER_NAME` | No | SNI / verification hostname override. Use when the server's certificate CN/SAN does not match the URL host. |
| `DNSWEAVER_{NAME}_TLS_MIN_VERSION` | No | Minimum TLS protocol version: `1.2` (default) or `1.3`. |
| `DNSWEAVER_{NAME}_TLS_SKIP_VERIFY` | No | Skip TLS certificate verification (`true`/`false`, default: `false`). **Warning:** disables MITM protection — prefer `TLS_CA_FILE`. |
| `DNSWEAVER_{NAME}_INSECURE_SKIP_VERIFY` | No | **Deprecated** — alias of `TLS_SKIP_VERIFY`. Will be removed in a future major release. |

### Dynamic Targets

By default `DNSWEAVER_{NAME}_TARGET` is a literal IP or hostname. Setting
`DNSWEAVER_{NAME}_TARGET_MODE` instead resolves the target at runtime and keeps
it up to date, which is useful when the target is the machine's own address and
changes over time (dynamic public IP, VPN address, or a specific interface).

Supported modes:

| Mode | Resolves to |
|------|-------------|
| `public` | The host's public IP, discovered via HTTP echo endpoints (`checkip.amazonaws.com`, `api.ipify.org`, `ipinfo.io/ip`, `ifconfig.me`) with fallback. Doubles as a dynamic-DNS setup. |
| `interface:<name>` | The primary global IP of the named local network interface (e.g. `interface:eth0`). No external calls. |

Behavior:

- **Family-aware.** An `A` record resolves an IPv4 address; an `AAAA` record
  resolves an IPv6 address. `CNAME` + a dynamic mode is a configuration error.
- **`TARGET` becomes an optional fallback.** If set, it is used until the first
  successful resolution.
- **Last known-good.** If a resolution fails (e.g. an echo endpoint is briefly
  down), dnsweaver keeps the previous value and logs a warning rather than
  dropping the record, so transient failures do not churn DNS.
- **Refresh.** The target is re-resolved every
  `DNSWEAVER_{NAME}_TARGET_REFRESH_INTERVAL` (default `5m`). A change triggers a
  reconcile so records update promptly.

```bash
# Point all matched records at this host's current public IP, refreshed every 5m
DNSWEAVER_PUBLIC_DNS_TYPE=cloudflare
DNSWEAVER_PUBLIC_DNS_RECORD_TYPE=A
DNSWEAVER_PUBLIC_DNS_TARGET_MODE=public
DNSWEAVER_PUBLIC_DNS_DOMAINS=*.example.com

# Or use a specific interface's IP
DNSWEAVER_LAN_DNS_TARGET_MODE=interface:eth0
```

!!! note "Containers"
    Both `public` and `interface:<name>` work inside a container with no extra
    mounts. `interface:<name>` reads the interface visible to the container's
    network namespace, so use the interface name as seen inside the container.

### TLS Certificate File Permissions

The official container **drops privileges to an unprivileged user** (`dnsweaver`,
uid/gid `1000`) before starting, even when you launch the container as root. The
entrypoint needs root only briefly to auto-detect the Docker socket group, then
hands off to uid `1000` via `su-exec`. The long-running process therefore reads
your CA bundle, client certificate, and **private key as uid/gid 1000 — not as
root and not as the host user that owns the files.**

If a mounted key is owned `root:root` with mode `0600` (or `0640`), the process
gets `permission denied` even though the file "looks" readable on the host:

```text
TLS configuration failed to build, falling back to stdlib defaults
  error="loading TLS client keypair (cert=\"/etc/certs/cert.crt\" key=\"/etc/certs/key.pem\"):
  open /etc/certs/key.pem: permission denied
  (dnsweaver runs as uid=1000 gid=1000 after dropping privileges; the file must be
  readable by that user — chown it to that uid/gid, make it group-readable, or mount
  it as a Docker secret: .../#tls-certificate-file-permissions)"
```

Pick **one** of these fixes (do **not** `chmod 0666` a private key — that makes it
world-readable inside the container, a real downgrade):

=== "Chown to uid/gid 1000 (recommended)"

    ```bash
    sudo chown 1000:1000 cert.crt key.pem
    sudo chmod 0644 cert.crt   # cert is public
    sudo chmod 0600 key.pem    # key stays private, but now owned by uid 1000
    ```

=== "Group-readable"

    ```bash
    sudo chgrp 1000 key.pem
    sudo chmod 0640 key.pem    # readable by gid 1000, still not world-readable
    ```

=== "Docker secrets (no chown needed)"

    Files under `/run/secrets/` are readable by the in-container user by design,
    so mounting certs as secrets sidesteps host ownership entirely:

    ```yaml
    services:
      dnsweaver:
        image: ghcr.io/maxfield-allison/dnsweaver:latest
        environment:
          - DNSWEAVER_TECHNITIUM_TLS_CERT_FILE=/run/secrets/dnsweaver_crt
          - DNSWEAVER_TECHNITIUM_TLS_KEY_FILE=/run/secrets/dnsweaver_key
        secrets:
          - dnsweaver_crt
          - dnsweaver_key
    secrets:
      dnsweaver_crt:
        file: ./cert.crt
      dnsweaver_key:
        file: ./key.pem
    ```

!!! note "Kubernetes"
    When the pod sets `securityContext.runAsUser`, certs projected from a
    `Secret` volume are already readable by that user. Match the uid/gid you
    run as, or rely on the default projected-secret mode (`0644`) for the cert
    and a `defaultMode` of `0640` for the key with an `fsGroup` that the
    container belongs to.

## Source Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_SOURCES` | `traefik` | Comma-separated list: `traefik`, `caddy`, `nginx-proxy`, `dnsweaver`, `kubernetes`, `proxmox`, `incus` |

!!! warning "Deprecated Variable"
    `DNSWEAVER_SOURCE` (singular) is deprecated and will be removed in a future major release. Use `DNSWEAVER_SOURCES` (plural) instead.
    When both are set, `DNSWEAVER_SOURCES` takes precedence.

### Traefik File Source Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS` | *(none)* | Paths to Traefik config directories/files |
| `DNSWEAVER_SOURCE_TRAEFIK_FILE_PATTERN` | `*.yml,*.yaml,*.toml` | Glob pattern for config files |
| `DNSWEAVER_SOURCE_TRAEFIK_POLL_INTERVAL` | `60s` | File re-scan interval |
| `DNSWEAVER_SOURCE_TRAEFIK_WATCH_METHOD` | `auto` | Watch method: `auto`, `inotify`, `poll` |
| `DNSWEAVER_SOURCE_TRAEFIK_DEFAULT_ENTRYPOINTS` | *(none)* | Comma-separated entrypoints to assign to Traefik routers that declare none. Mirrors Traefik's [`asDefault`](https://doc.traefik.io/traefik/reference/install-configuration/entrypoints/#opt-asdefault) setting; required if you flag any entrypoint `asDefault = true` in Traefik so unlabeled routers don't become wildcards in dnsweaver. See [Traefik `asDefault` Entrypoints](../sources/swarm.md#traefik-asdefault-entrypoints). |

### Proxmox VE Source Settings

Setting `DNSWEAVER_PROXMOX_URL` auto-registers the Proxmox source even if not
listed in `DNSWEAVER_SOURCES`. See [Proxmox Source](../sources/proxmox.md) for
full setup including the required PVE role privileges.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DNSWEAVER_PROXMOX_URL` | Yes | — | PVE API base URL, e.g. `https://pve-00:8006` |
| `DNSWEAVER_PROXMOX_TOKEN_ID` | Yes | — | API token ID, e.g. `dnsweaver@pve!dnsweaver` |
| `DNSWEAVER_PROXMOX_TOKEN_SECRET` | Yes | — | API token secret (UUID). Supports `_FILE` suffix. |
| `DNSWEAVER_PROXMOX_TOKEN_SECRET_FILE` | Alt | — | Path to a file containing the token secret |
| `DNSWEAVER_PROXMOX_TLS_CA_FILE` | No | — | Path to a PEM CA bundle for the PVE certificate chain (homelab-issued certs). |
| `DNSWEAVER_PROXMOX_TLS_CERT_FILE` | No | — | Client certificate for PVE mutual-TLS (paired with `TLS_KEY_FILE`). |
| `DNSWEAVER_PROXMOX_TLS_KEY_FILE` | No | — | Client private key for PVE mutual-TLS. |
| `DNSWEAVER_PROXMOX_TLS_SERVER_NAME` | No | — | SNI/verification hostname override. |
| `DNSWEAVER_PROXMOX_TLS_MIN_VERSION` | No | `1.2` | Minimum TLS protocol version (`1.2` or `1.3`). |
| `DNSWEAVER_PROXMOX_TLS_SKIP_VERIFY` | No | `false` | Skip PVE TLS certificate verification. Prefer `TLS_CA_FILE`. |
| `DNSWEAVER_PROXMOX_VERIFY_TLS` | No | `true` | **Deprecated** — inverted-polarity alias of `TLS_SKIP_VERIFY`. Will be removed in a future major release. |
| `DNSWEAVER_PROXMOX_NODE_FILTER` | No | *(all)* | Restrict discovery to a single PVE node name |
| `DNSWEAVER_PROXMOX_TAG_FILTER` | No | *(all)* | Only include resources with this tag (prefix match) |
| `DNSWEAVER_PROXMOX_HOSTNAME_TAG_PREFIX` | No | — | Optional tag prefix in the form `<prefix>+<hostname>` used to override the discovered hostname |
| `DNSWEAVER_PROXMOX_INTERFACE_TAG_PREFIX` | No | — | Optional tag prefix in the form `<prefix>+<interface>` that selects a specific guest interface for IP resolution. A matching tag overrides the allow-list and is honored even when that interface is not otherwise allowed. |
| `DNSWEAVER_PROXMOX_ALLOWED_INTERFACES` | No | — | Comma-separated allow-list of guest interface prefixes to consider when resolving IPs. Entries are matched as prefixes (for example `eth` matches `eth0`), and if no entry matches, dnsweaver falls back to the first non-loopback IPv4 address instead of skipping the VM. |
| `DNSWEAVER_PROXMOX_STATE_FILTER` | No | `running` | Resource status filter (`running`, `stopped`, etc.) |
| `DNSWEAVER_PROXMOX_DOMAIN_SUFFIX` | No | — | Domain suffix appended to VM names |

## Provider-Specific Settings

See the individual provider documentation for complete settings:

- [Technitium](../providers/technitium.md) — includes companion HTTPS record options
- [Cloudflare](../providers/cloudflare.md)
- [OVHcloud](../providers/ovh.md)
- [RFC 2136](../providers/rfc2136.md)
- [PowerDNS](../providers/powerdns.md)
- [Pi-hole](../providers/pihole.md)
- [AdGuard Home](../providers/adguard.md)
- [dnsmasq](../providers/dnsmasq.md)
- [Webhook](../providers/webhook.md)

For Kubernetes source configuration, see [Kubernetes Source](../sources/kubernetes.md).
