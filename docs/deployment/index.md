---
title: Deployment
description: Deploy dnsweaver on Docker, Swarm, Kubernetes, Proxmox VE, Incus, or hybrid environments
icon: material/server
---

# Deployment

dnsweaver runs as a lightweight container in Docker or Kubernetes, and can additionally watch Proxmox VE clusters via the PVE API. This section provides production-ready configurations for all supported environments.

## Deployment Options

<div class="grid cards" markdown>

-   :material-docker:{ .lg .middle } **Docker Compose**

    ---

    The simplest deployment for single-host environments. Recommended for getting started.

    [:octicons-arrow-right-24: Docker Compose](docker-compose.md)

-   :fontawesome-brands-docker:{ .lg .middle } **Docker Swarm**

    ---

    Production deployment for multi-node Docker clusters with high availability.

    [:octicons-arrow-right-24: Docker Swarm](swarm.md)

-   :material-kubernetes:{ .lg .middle } **Kubernetes**

    ---

    Deploy with Helm or Kustomize. Watches Ingress, IngressRoute, HTTPRoute, and Service resources.

    [:octicons-arrow-right-24: Kubernetes](kubernetes.md)

-   :material-transit-connection-variant:{ .lg .middle } **Split-Horizon DNS**

    ---

    Configure internal and external DNS records from the same container labels.

    [:octicons-arrow-right-24: Split-Horizon](split-horizon.md)

</div>

## Quick Comparison

| Feature | Docker Compose | Docker Swarm | Kubernetes |
| :------ | :------------- | :----------- | :--------- |
| Complexity | Simple | Moderate | Moderate |
| High availability | :material-close: | :material-check: | :material-check: |
| Secrets management | File-based | Native secrets | K8s Secrets |
| RBAC | Docker socket | Docker socket | ClusterRole |
| Best for | Development | Multi-node Docker | K8s clusters |

## Common Requirements

Regardless of deployment method, dnsweaver needs:

1. **Platform access** — Docker socket (Docker) or RBAC ServiceAccount (Kubernetes)
2. **Network connectivity** — To reach DNS provider APIs
3. **Credentials** — API tokens for your DNS providers

!!! warning "Docker socket security"
    The Docker socket provides root-level access to your host. For production deployments, consider using a [socket proxy](https://github.com/Tecnativa/docker-socket-proxy) to limit dnsweaver's API access.

## Next Steps

Choose the deployment guide that matches your environment, then configure your DNS providers.
