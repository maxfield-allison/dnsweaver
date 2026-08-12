---
title: Sources
description: How dnsweaver discovers hostnames from Docker containers, Kubernetes resources, Proxmox VE, and Incus workloads
icon: material/source-branch
---

# Sources

dnsweaver reads hostnames from seven **sources**. All seven are peers: enable any combination with `DNSWEAVER_SOURCES`, and run as many at once as you need. Each one extracts hostnames differently, so dnsweaver works with the reverse proxy configuration you already have and with VMs and containers discovered straight from the Proxmox VE and Incus APIs.

## Available Sources

<div class="grid cards" markdown>

-   :material-label:{ .lg .middle } **Docker Labels**

    ---

    Parse hostnames from Traefik router labels on Docker containers.

    [:octicons-arrow-right-24: Docker Labels](docker.md)

-   :fontawesome-brands-docker:{ .lg .middle } **Docker Swarm**

    ---

    Discover services in Docker Swarm mode with support for service labels and tasks.

    [:octicons-arrow-right-24: Swarm Mode](swarm.md)

-   :material-file-document:{ .lg .middle } **Traefik Files**

    ---

    Watch Traefik dynamic configuration files for hostname changes.

    [:octicons-arrow-right-24: Traefik Files](traefik-files.md)

-   :material-rocket-launch:{ .lg .middle } **Caddy Labels**

    ---

    Parse hostnames from caddy-docker-proxy style container labels.

    [:octicons-arrow-right-24: Caddy Labels](caddy.md)

-   :simple-nginx:{ .lg .middle } **nginx-proxy Labels**

    ---

    Parse `VIRTUAL_HOST` labels used by jwilder/nginx-proxy.

    [:octicons-arrow-right-24: nginx-proxy Labels](nginx-proxy.md)

-   :material-tag-text:{ .lg .middle } **Native Labels**

    ---

    Use dnsweaver-specific labels for explicit DNS record configuration.

    [:octicons-arrow-right-24: Native Labels](native-labels.md)

-   :material-kubernetes:{ .lg .middle } **Kubernetes**

    ---

    Automatic hostname extraction from Ingress, IngressRoute, HTTPRoute, and Service resources.

    [:octicons-arrow-right-24: Kubernetes](kubernetes.md)

-   :material-server:{ .lg .middle } **Proxmox VE**

    ---

    Discover VMs and LXC containers on a Proxmox cluster and create A records from VM names.

    [:octicons-arrow-right-24: Proxmox](proxmox.md)

-   :material-cube-outline:{ .lg .middle } **Incus**

    ---

    Discover Incus system containers and VMs over a local socket or remote HTTPS and create A records from instance names.

    [:octicons-arrow-right-24: Incus](incus.md)

</div>

## Source Priority

When multiple sources provide the same hostname, dnsweaver uses the following priority:

1. `dnsweaver` native labels (explicit configuration)
2. `traefik` labels (reverse proxy configuration)
3. `caddy` labels (caddy-docker-proxy configuration)
4. `nginx-proxy` labels (`VIRTUAL_HOST`)
5. `traefik` files (dynamic configuration)
6. `kubernetes` (resource spec hostnames)
7. `proxmox` (VM/LXC name + domain suffix)
8. `incus` (instance name + domain suffix)

Priority applies only when two sources claim the same hostname. It is a tiebreaker, not a ranking
of the sources themselves.

## Capability matrix

The value in the first column is what you put in `DNSWEAVER_SOURCES`.

| Source | Reads | Runs over | Example |
| :----- | :---- | :-------- | :------ |
| `traefik` | `traefik.http.routers.*.rule` labels, plus `http.routers.*.rule` in Traefik dynamic config files | Docker, Swarm, Incus | `` Host(`app.example.com`) `` |
| `caddy` | `caddy` / `caddy_<n>` labels from caddy-docker-proxy | Docker, Swarm, Incus | `caddy=app.example.com` |
| `nginx-proxy` | `VIRTUAL_HOST` labels from jwilder/nginx-proxy | Docker, Swarm, Incus | `VIRTUAL_HOST=app.example.com` |
| `dnsweaver` | Native `dnsweaver.*` labels, for explicit record configuration | Docker, Swarm, Incus | `dnsweaver.hostname=app.example.com` |
| `kubernetes` | Ingress, IngressRoute, HTTPRoute, and Service resource specs | Kubernetes | `.spec.rules[].host` |
| `proxmox` | PVE API: QEMU VM names via the guest agent, and LXC container names | Proxmox VE | `webserver` + `home.example.com` |
| `incus` | Incus API: system container and VM instance names | Incus | `webserver` + `home.example.com` |

The four label-reading sources also work on Incus, because the Incus adapter surfaces
[incus-compose](https://github.com/lxc/incus-compose) `user.label.<key>` config keys under their
stripped `<key>` form. No extra configuration needed.

Traefik file discovery is part of the `traefik` source, not a separate entry: set
`DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS` and it turns on.

!!! info "Multiple hostnames"
    Containers and Kubernetes resources can expose multiple hostnames. All discovered hostnames are processed independently and matched against configured provider domains.
