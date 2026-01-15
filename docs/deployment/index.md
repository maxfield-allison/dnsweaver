---
title: Deployment
description: Deploy dnsweaver in Docker, Docker Compose, and Docker Swarm environments
icon: material/server
---

# Deployment

dnsweaver is designed for containerized deployments. This section provides production-ready configurations for various environments.

## Deployment Options

<div class="grid cards" markdown>

-   :material-docker:{ .lg .middle } **Docker Compose**

    ---

    The simplest deployment for single-host environments. Recommended for getting started.

    [:octicons-arrow-right-24: Docker Compose](docker-compose.md)

-   :fontawesome-brands-docker:{ .lg .middle } **Docker Swarm**

    ---

    Production deployment for multi-node clusters with high availability considerations.

    [:octicons-arrow-right-24: Docker Swarm](swarm.md)

-   :material-transit-connection-variant:{ .lg .middle } **Split-Horizon DNS**

    ---

    Configure internal and external DNS records from the same container labels.

    [:octicons-arrow-right-24: Split-Horizon](split-horizon.md)

</div>

## Quick Comparison

| Feature | Docker Compose | Docker Swarm |
| :------ | :------------- | :----------- |
| Complexity | Simple | Moderate |
| High availability | :material-close: | :material-check: |
| Secrets management | File-based | Native secrets |
| Best for | Development, single-host | Production, multi-node |

## Common Requirements

Regardless of deployment method, dnsweaver needs:

1. **Docker socket access** — To watch container events
2. **Network connectivity** — To reach DNS provider APIs
3. **Credentials** — API tokens for your DNS providers

!!! warning "Docker socket security"
    The Docker socket provides root-level access to your host. For production deployments, consider using a [socket proxy](https://github.com/Tecnativa/docker-socket-proxy) to limit dnsweaver's API access.

## Next Steps

Choose the deployment guide that matches your environment, then configure your DNS providers.
