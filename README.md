<p align="center">
  <img src="docs/assets/brand/hero-banner.png" alt="dnsweaver — Automatic DNS records for containers, VMs and clusters" width="100%">
</p>

# dnsweaver

[![CI](https://img.shields.io/github/actions/workflow/status/maxfield-allison/dnsweaver/ci.yml?branch=main&style=flat-square)](https://github.com/maxfield-allison/dnsweaver/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/maxfield-allison/dnsweaver?style=flat-square)](https://github.com/maxfield-allison/dnsweaver/releases)
[![Docker Pulls](https://img.shields.io/docker/pulls/maxamill/dnsweaver?style=flat-square)](https://hub.docker.com/r/maxamill/dnsweaver)
[![License](https://img.shields.io/github/license/maxfield-allison/dnsweaver?style=flat-square)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/maxfield-allison/dnsweaver?style=flat-square)](go.mod)

**Automatic DNS records for containers, VMs & clusters.**

dnsweaver reads hostnames from seven sources and writes records to eleven DNS providers. A container starts with a Traefik label or a Proxmox VM boots, and the record appears; when they go away, so does it. Internal and external records come from the same labels, so split-horizon costs nothing extra.

📚 **[Full Documentation](https://maxfield-allison.github.io/dnsweaver/)**

## Why dnsweaver?

Think of dnsweaver as **external-dns for the homelab**. Where most tools solve a slice of the problem, dnsweaver covers the parts self-hosters actually run into:

- **It does Proxmox.** Auto-create A records for VMs and LXCs from the PVE API. Almost no other DNS automation tool offers it.
- **It speaks self-hosted DNS.** First-class [Technitium](https://maxfield-allison.github.io/dnsweaver/providers/technitium/), [Pi-hole](https://maxfield-allison.github.io/dnsweaver/providers/pihole/), [AdGuard Home](https://maxfield-allison.github.io/dnsweaver/providers/adguard/), and [dnsmasq](https://maxfield-allison.github.io/dnsweaver/providers/dnsmasq/) support. Not an afterthought, and not alpha.
- **It's multi-source.** Traefik, Caddy, nginx-proxy, native labels, Kubernetes, Proxmox VE, and Incus in one binary. Run one or all of them at once. `external-dns` is Kubernetes-only.
- **It does split-horizon out of the box.** Internal and external records from the *same* labels. Route private hostnames to Technitium and public ones to Cloudflare simultaneously.
- **It's a single static Go binary.** ~15 MB, multi-arch (amd64/arm64), zero runtime dependencies. No Node.js, no sidecars.

If you manage a homelab with Traefik, Proxmox, and a self-hosted resolver and you're still creating DNS records by hand, dnsweaver is built for you.

## Features

- 🔀 **Multi-Provider Support** — Route different domains to different DNS providers
- 🌐 **Split-Horizon DNS** — Internal and external records from the same container labels
- 🧩 **Seven Sources** — Traefik, Caddy, nginx-proxy, native labels, Kubernetes, Proxmox VE, and Incus, in one binary. See [Supported Sources](#supported-sources)
- 🏗️ **Multi-Instance Safe** — Run multiple dnsweaver instances on the same DNS zone without conflicts
- 🔒 **Socket Proxy Compatible** — Connect via TCP to a Docker socket proxy for improved security
- 🛡️ **Hardened TLS** — Unified per-instance TLS controls (custom CA, mTLS client certs, SNI override, configurable min version; TLS 1.2 floor by default) for every HTTP-based provider and the Proxmox source
- 📊 **Observable** — Prometheus metrics, health endpoints, structured logging
- 🔑 **Secrets Support** — Docker secrets and Kubernetes Secrets via `_FILE` suffix variables

## Supported Sources

Sources are where hostnames come from. Enable any combination with `DNSWEAVER_SOURCES`.

| Source | Reads | Runs over |
|--------|-------|-----------|
| [`traefik`](https://maxfield-allison.github.io/dnsweaver/sources/docker/) | `traefik.http.routers.*.rule` labels, plus Traefik dynamic config files | Docker, Swarm, Incus |
| [`caddy`](https://maxfield-allison.github.io/dnsweaver/sources/caddy/) | `caddy` / `caddy_<n>` labels from [caddy-docker-proxy](https://github.com/lucaslorentz/caddy-docker-proxy) | Docker, Swarm, Incus |
| [`nginx-proxy`](https://maxfield-allison.github.io/dnsweaver/sources/nginx-proxy/) | `VIRTUAL_HOST` labels from [jwilder/nginx-proxy](https://github.com/nginx-proxy/nginx-proxy) | Docker, Swarm, Incus |
| [`dnsweaver`](https://maxfield-allison.github.io/dnsweaver/sources/native-labels/) | Native `dnsweaver.*` labels for explicit record configuration | Docker, Swarm, Incus |
| [`kubernetes`](https://maxfield-allison.github.io/dnsweaver/sources/kubernetes/) | Ingress, IngressRoute, HTTPRoute, and Service resources | Kubernetes |
| [`proxmox`](https://maxfield-allison.github.io/dnsweaver/sources/proxmox/) | PVE API: QEMU VMs via guest agent, and LXC containers | Proxmox VE |
| [`incus`](https://maxfield-allison.github.io/dnsweaver/sources/incus/) | Incus API: system containers and VMs, local socket or remote HTTPS | Incus |

## Supported Providers

Providers are where records get written. Run several at once to split internal and external DNS.

| Provider | Record Types | Notes |
|----------|--------------|-------|
| [Technitium](https://maxfield-allison.github.io/dnsweaver/providers/technitium/) | A, AAAA, CNAME, SRV, TXT | Full-featured self-hosted DNS |
| [Cloudflare](https://maxfield-allison.github.io/dnsweaver/providers/cloudflare/) | A, AAAA, CNAME, SRV, TXT | With optional proxy support |
| [OVHcloud](https://maxfield-allison.github.io/dnsweaver/providers/ovh/) | A, AAAA, CNAME, SRV, TXT | Public DNS for OVH-hosted domains |
| [RFC 2136](https://maxfield-allison.github.io/dnsweaver/providers/rfc2136/) | A, AAAA, CNAME, SRV, TXT | BIND, Windows DNS, PowerDNS, Knot |
| [PowerDNS](https://maxfield-allison.github.io/dnsweaver/providers/powerdns/) | A, AAAA, CNAME, SRV, TXT | Native Authoritative HTTP API |
| [Pi-hole](https://maxfield-allison.github.io/dnsweaver/providers/pihole/) | A, CNAME | API or file mode |
| [AdGuard Home](https://maxfield-allison.github.io/dnsweaver/providers/adguard/) | A, AAAA, CNAME | DNS rewrite management |
| [OPNsense](https://maxfield-allison.github.io/dnsweaver/providers/opnsense/) | A, AAAA | Unbound or Dnsmasq host overrides via REST API |
| [pfSense](https://maxfield-allison.github.io/dnsweaver/providers/pfsense/) | A, AAAA | Unbound or Dnsmasq host overrides via REST API (community pfSense-pkg-RESTAPI) |
| [dnsmasq](https://maxfield-allison.github.io/dnsweaver/providers/dnsmasq/) | A, CNAME | File-based configuration |
| [Webhook](https://maxfield-allison.github.io/dnsweaver/providers/webhook/) | Any | Custom integrations |

## Quick Start

### Installation

```bash
# Docker Hub
docker pull maxamill/dnsweaver:latest

# GitHub Container Registry
docker pull ghcr.io/maxfield-allison/dnsweaver:latest
```

### Basic Example

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    restart: unless-stopped
    environment:
      - DNSWEAVER_INSTANCES=internal-dns
      - DNSWEAVER_INTERNAL_DNS_TYPE=technitium
      - DNSWEAVER_INTERNAL_DNS_URL=http://dns.internal:5380
      - DNSWEAVER_INTERNAL_DNS_TOKEN_FILE=/run/secrets/technitium_token
      - DNSWEAVER_INTERNAL_DNS_ZONE=home.example.com
      - DNSWEAVER_INTERNAL_DNS_RECORD_TYPE=A
      - DNSWEAVER_INTERNAL_DNS_TARGET=192.0.2.100
      - DNSWEAVER_INTERNAL_DNS_DOMAINS=*.home.example.com
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    secrets:
      - technitium_token

secrets:
  technitium_token:
    external: true
```

### How It Works

```mermaid
flowchart LR
    subgraph sources["7 sources"]
        direction TB
        A["traefik · caddy · nginx-proxy · dnsweaver<br/>labels on Docker, Swarm, Incus"]
        D["kubernetes<br/>Ingress · IngressRoute · HTTPRoute · Service"]
        P["proxmox · incus<br/>API: VMs · LXC · system containers"]
    end

    subgraph engine["dnsweaver"]
        direction TB
        W["Watch<br/>events"] --> M["Match hostname<br/>to domain patterns"] --> R["Reconcile<br/>desired vs. live"]
    end

    subgraph providers["11 providers · split-horizon"]
        direction TB
        INT["Internal instance<br/>e.g. technitium · pi-hole · adguard"]
        EXT["External instance<br/>e.g. cloudflare · ovhcloud"]
    end

    A --> W
    D --> W
    P --> W
    R -->|create / update / delete| INT
    R -->|create / update / delete| EXT
```

1. A container starts with a Traefik label (or a Kubernetes Ingress/HTTPRoute is created):
   ```yaml
   labels:
     - "traefik.http.routers.myapp.rule=Host(`myapp.home.example.com`)"
   ```

2. dnsweaver extracts the hostname and matches it against configured provider domain patterns

3. The matching provider creates the DNS record:
   - **A record**: `myapp.home.example.com → 192.0.2.100`
   - **CNAME**: `myapp.example.com → proxy.example.com`

4. When the container stops (or the Kubernetes resource is deleted), the DNS record is automatically cleaned up

## Documentation

| Topic | Description |
|-------|-------------|
| [Getting Started](https://maxfield-allison.github.io/dnsweaver/getting-started/) | Installation and first configuration |
| [Configuration](https://maxfield-allison.github.io/dnsweaver/configuration/environment/) | Environment variables reference |
| [Providers](https://maxfield-allison.github.io/dnsweaver/providers/) | Provider-specific setup guides |
| [Sources](https://maxfield-allison.github.io/dnsweaver/sources/) | All seven sources, what each reads, and the capability matrix |
| [Kubernetes](https://maxfield-allison.github.io/dnsweaver/deployment/kubernetes/) | Kubernetes deployment with Helm/Kustomize |
| [Proxmox VE](https://maxfield-allison.github.io/dnsweaver/sources/proxmox/) | Auto-DNS for VMs and LXC containers |
| [Incus](https://maxfield-allison.github.io/dnsweaver/sources/incus/) | Auto-DNS for system containers and VMs |
| [Split-Horizon DNS](https://maxfield-allison.github.io/dnsweaver/deployment/split-horizon/) | Internal + external records |
| [Docker Swarm](https://maxfield-allison.github.io/dnsweaver/deployment/swarm/) | Swarm deployment guide |
| [Observability](https://maxfield-allison.github.io/dnsweaver/observability/) | Metrics, logging, and health checks |
| [FAQ](https://maxfield-allison.github.io/dnsweaver/faq/) | Common questions and troubleshooting |

### TLS Configuration

Every HTTP-based provider (Technitium, AdGuard Home, Cloudflare, OVHcloud, PowerDNS, Pi-hole, Webhook) and the Proxmox source share a single TLS configuration surface:

| Env key (per instance) | Purpose |
|------------------------|---------|
| `DNSWEAVER_<NAME>_TLS_CA_FILE` | Trust a private CA bundle (PEM) |
| `DNSWEAVER_<NAME>_TLS_CERT_FILE` / `_TLS_KEY_FILE` | Present a client certificate (mTLS) |
| `DNSWEAVER_<NAME>_TLS_SERVER_NAME` | Override SNI / hostname verification |
| `DNSWEAVER_<NAME>_TLS_SKIP_VERIFY` | Disable verification (development only) |
| `DNSWEAVER_<NAME>_TLS_MIN_VERSION` | `1.2` (default) or `1.3` |

The Proxmox source uses the same keys under `DNSWEAVER_PROXMOX_TLS_*`. The legacy `*_INSECURE_SKIP_VERIFY` and `DNSWEAVER_PROXMOX_VERIFY_TLS` variables are still accepted but emit a deprecation warning at startup — migrate to the unified `TLS_SKIP_VERIFY` keys. See the [Environment Reference](https://maxfield-allison.github.io/dnsweaver/configuration/environment/) and [SECURITY.md](SECURITY.md) for full details and recipes.

> **Mounting certs?** The container drops privileges to uid/gid `1000`, so cert/key files must be readable by that user — a `root:root 0600` key yields `permission denied`. See [TLS Certificate File Permissions](https://maxfield-allison.github.io/dnsweaver/configuration/environment/#tls-certificate-file-permissions).


## Kubernetes Quick Start

Deploy dnsweaver to watch Kubernetes resources for DNS management:

```bash
# Using Kustomize
kubectl apply -k https://github.com/maxfield-allison/dnsweaver/deploy/kustomize/base

# Using Helm
helm install dnsweaver deploy/helm/dnsweaver/ \
  --namespace dnsweaver --create-namespace
```

dnsweaver automatically detects hostnames from Ingress, IngressRoute (Traefik), HTTPRoute (Gateway API), and Service resources. Use `dnsweaver.dev/*` annotations for per-resource overrides:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: my-app
  annotations:
    dnsweaver.dev/record-type: "A"
    dnsweaver.dev/target: "192.0.2.100"
spec:
  rules:
    - host: app.example.com
```

See the [Kubernetes deployment guide](https://maxfield-allison.github.io/dnsweaver/deployment/kubernetes/) for full configuration.

## Split-Horizon DNS Example

Manage internal and external DNS from the same container labels:

```yaml
environment:
  - DNSWEAVER_INSTANCES=internal,external

  # Internal: Technitium → private IP
  - DNSWEAVER_INTERNAL_TYPE=technitium
  - DNSWEAVER_INTERNAL_RECORD_TYPE=A
  - DNSWEAVER_INTERNAL_TARGET=192.0.2.100
  - DNSWEAVER_INTERNAL_DOMAINS=*.example.com

  # External: Cloudflare → tunnel CNAME
  - DNSWEAVER_EXTERNAL_TYPE=cloudflare
  - DNSWEAVER_EXTERNAL_RECORD_TYPE=CNAME
  - DNSWEAVER_EXTERNAL_TARGET=tunnel.example.com
  - DNSWEAVER_EXTERNAL_DOMAINS=*.example.com
```

With this configuration, when `app.example.com` starts:
- Internal DNS → `A` record → `192.0.2.100`
- External DNS → `CNAME` record → `tunnel.example.com`

## Contributing

Contributions are welcome! See [CONTRIBUTING](https://maxfield-allison.github.io/dnsweaver/contributing/) for guidelines.

## Star History

If dnsweaver saves you time, a ⭐ helps others find it.

[![Star History Chart](https://api.star-history.com/svg?repos=maxfield-allison/dnsweaver&type=Date)](https://star-history.com/#maxfield-allison/dnsweaver&Date)

## License

[MIT License](LICENSE)
