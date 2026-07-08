---
title: Docker Labels Source
description: dnsweaver watches Docker containers and extracts hostnames from Traefik, Caddy, nginx-proxy, and native labels to create DNS records automatically.
---

# Docker Labels

dnsweaver watches Docker containers and services for hostname information, extracting them from labels to create DNS records.

## Supported Label Sources

dnsweaver extracts hostnames from:

1. **Traefik labels** (default) - `traefik.http.routers.*.rule=Host(...)`
2. **Native dnsweaver labels** - `dnsweaver.hostname=...`

Configure which sources to use:

```yaml
- DNSWEAVER_SOURCES=traefik,dnsweaver
```

## Docker Modes

### Standalone Docker

For single-host Docker:

```yaml
environment:
  - DNSWEAVER_DOCKER_MODE=standalone
  # or auto (default) - auto-detects mode
volumes:
  - /var/run/docker.sock:/var/run/docker.sock:ro
```

In standalone mode, dnsweaver watches:
- Container start/stop events
- Container labels

### Docker Swarm

For Swarm clusters:

```yaml
environment:
  - DNSWEAVER_DOCKER_MODE=swarm
  # or auto (default)
volumes:
  - /var/run/docker.sock:/var/run/docker.sock:ro
```

In Swarm mode, dnsweaver watches:
- Service create/update/remove events
- Service labels (not container labels)

!!! important
    In Swarm mode, labels must be on the **service**, not individual containers.

## Docker Socket Options

### Direct Mount

Standard approach - mount the Docker socket:

```yaml
volumes:
  - /var/run/docker.sock:/var/run/docker.sock:ro
```

### TCP Socket

Connect to a remote Docker host or socket proxy:

```yaml
environment:
  - DNSWEAVER_DOCKER_HOST=tcp://socket-proxy:2375
```

### Socket Proxy (Recommended for Security)

A socket proxy is the **recommended** way to give dnsweaver Docker access. It
sits in front of the Docker API and exposes only the read-only endpoints
dnsweaver needs, so a compromise of dnsweaver can't drive the full Docker API to
escape the container. dnsweaver connects over TCP and never touches the socket,
so it also keeps running as its unprivileged `uid 1000` user.

```yaml
services:
  socket-proxy:
    image: tecnativa/docker-socket-proxy
    environment:
      - CONTAINERS=1
      - EVENTS=1
      - INFO=1
      - PING=1
      # Swarm only:
      # - SERVICES=1
      # - TASKS=1
      # - NETWORKS=1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro

  dnsweaver:
    image: ghcr.io/maxfield-allison/dnsweaver:latest
    environment:
      - DNSWEAVER_DOCKER_HOST=tcp://socket-proxy:2375
    depends_on:
      - socket-proxy
```

Required socket proxy permissions:
- `CONTAINERS=1` - Read container info
- `EVENTS=1` - Stream container start/stop events
- `INFO=1` - dnsweaver queries `/info` at startup
- `PING=1` - Connectivity/health check
- `SERVICES=1` - Read service info (Swarm only)
- `TASKS=1` - Read task info (Swarm only)
- `NETWORKS=1` - Read network info (Swarm only)

A full runnable example lives at
[`docs/examples/docker-compose.socket-proxy.example.yml`](https://github.com/maxfield-allison/dnsweaver/blob/main/docs/examples/docker-compose.socket-proxy.example.yml).

## Socket Permissions & the Non-Root User

dnsweaver runs as an unprivileged user (`uid 1000`) inside the container. When
you bind-mount the socket directly, the container starts as root just long
enough for its entrypoint to detect the socket's group (GID) and add the
`dnsweaver` user to it, then drops privileges via `su-exec`. The standard
compose example works out of the box — no `group_add` needed.

The entrypoint deliberately **does not** grant access when the socket is owned
by GID 0 (root), because that would silently hand the `dnsweaver` user
root-group membership. If that happens you'll see a startup log explaining the
options below.

### `DNSWEAVER_DOCKER_GID` (explicit opt-in)

Set this to force the `dnsweaver` user into a specific group GID. It's an escape
hatch for platforms whose socket ownership can't be detected or changed:

```yaml
environment:
  - DNSWEAVER_DOCKER_GID=0   # read a root-owned socket
```

The process still drops to `uid 1000` — only a supplementary group is added, so
the application never runs as root. A socket proxy is still the stronger option.

### Synology (Container Manager)

Synology's Container Manager mounts `/var/run/docker.sock` as `root:root` and
reverts ownership changes on reboot, and it won't let you create host users with
arbitrary UIDs/GIDs. Because the socket is GID 0, direct-mount auto-detection is
intentionally skipped. Two supported ways forward:

1. **Socket proxy (recommended).** The proxy runs as root (which Synology
   requires anyway) and dnsweaver connects over TCP as `uid 1000`. See the
   example above.
2. **`DNSWEAVER_DOCKER_GID=0`.** Quick single-line opt-in that lets the
   unprivileged dnsweaver user read the root-owned socket. Simpler, but grants
   the container's user root-group membership, so prefer the proxy where you can.

## Event Processing

When a container/service starts:

1. dnsweaver receives the Docker event
2. Inspects the container/service for labels
3. Extracts hostnames from matching labels
4. Matches hostnames against provider domain patterns
5. Creates DNS records in matching providers

When a container/service stops:

1. dnsweaver receives the Docker event
2. Looks up previously created records
3. Deletes DNS records from providers

## Container ID Tracking

dnsweaver tracks which records belong to which containers using:

1. **Internal state** - In-memory mapping of containers to records
2. **TXT ownership records** - Persistent tracking in DNS (if enabled)

This ensures:
- Records are properly cleaned up when containers stop
- Duplicate containers don't create duplicate records
- Container restarts don't cause record churn
