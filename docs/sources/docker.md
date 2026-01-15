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

Use a socket proxy for improved security:

```yaml
services:
  socket-proxy:
    image: tecnativa/docker-socket-proxy
    environment:
      - CONTAINERS=1
      - SERVICES=1
      - TASKS=1
      - NETWORKS=1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro

  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_DOCKER_HOST=tcp://socket-proxy:2375
    depends_on:
      - socket-proxy
```

Required socket proxy permissions:
- `CONTAINERS=1` - Read container info
- `SERVICES=1` - Read service info (Swarm)
- `TASKS=1` - Read task info (Swarm)
- `NETWORKS=1` - Read network info

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
