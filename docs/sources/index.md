---
title: Sources
description: How dnsweaver discovers hostnames from Docker containers
icon: material/source-branch
---

# Sources

dnsweaver discovers hostnames to manage from multiple **sources**. Each source type extracts hostnames differently, allowing dnsweaver to work with existing reverse proxy configurations.

## Available Sources

<div class="grid cards" markdown>

-   :material-label:{ .lg .middle } **Docker Labels**

    ---

    Parse hostnames from Traefik, Caddy, and nginx-proxy labels on Docker containers.

    [:octicons-arrow-right-24: Docker Labels](docker.md)

-   :fontawesome-brands-docker:{ .lg .middle } **Docker Swarm**

    ---

    Discover services in Docker Swarm mode with support for service labels and tasks.

    [:octicons-arrow-right-24: Swarm Mode](swarm.md)

-   :material-file-document:{ .lg .middle } **Traefik Files**

    ---

    Watch Traefik dynamic configuration files for hostname changes.

    [:octicons-arrow-right-24: Traefik Files](traefik-files.md)

-   :material-tag-text:{ .lg .middle } **Native Labels**

    ---

    Use dnsweaver-specific labels for explicit DNS record configuration.

    [:octicons-arrow-right-24: Native Labels](native-labels.md)

</div>

## Source Priority

When multiple sources provide the same hostname, dnsweaver uses the following priority:

1. **Native labels** (explicit dnsweaver configuration)
2. **Traefik/Caddy labels** (reverse proxy configuration)
3. **Traefik files** (dynamic configuration)

## Hostname Extraction

Each source extracts hostnames differently:

| Source | Extracts From | Example Label/Config |
| :----- | :------------ | :------------------- |
| Docker (Traefik) | `traefik.http.routers.*.rule` | `` Host(`app.example.com`) `` |
| Docker (Caddy) | `caddy` or `caddy_*` | `caddy=app.example.com` |
| Docker Swarm | Service labels | Same as Docker |
| Traefik Files | `http.routers.*.rule` in YAML/TOML | Standard Traefik config |
| Native | `dnsweaver.hostname` | `dnsweaver.hostname=app.example.com` |

!!! info "Multiple hostnames"
    Containers can expose multiple hostnames. All discovered hostnames are processed independently and matched against configured provider domains.
