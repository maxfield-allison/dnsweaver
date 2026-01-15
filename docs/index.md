---
title: Automatic DNS for Docker
description: dnsweaver automatically manages DNS records for your Docker containers with multi-provider support
---

# dnsweaver

**Automatic DNS record management for Docker containers with multi-provider support.**

dnsweaver watches Docker events and automatically creates and deletes DNS records for services with reverse proxy labels (Traefik, etc.). Unlike single-provider tools, dnsweaver supports **split-horizon DNS** and **multiple DNS providers** simultaneously.

---

## Key Features

<div class="grid cards" markdown>

-   :material-dns:{ .lg .middle } **Multi-Provider Support**

    ---

    Route different domains to different DNS providers. Technitium, Cloudflare, Pi-hole, dnsmasq, and webhook—all at once.

    [:octicons-arrow-right-24: Providers](providers/index.md)

-   :material-sync:{ .lg .middle } **Split-Horizon DNS**

    ---

    Create internal and external records from the same container labels automatically. One label, multiple zones.

    [:octicons-arrow-right-24: Split-Horizon Guide](deployment/split-horizon.md)

-   :material-docker:{ .lg .middle } **Docker & Swarm Native**

    ---

    Works with standalone Docker and Docker Swarm clusters. Socket proxy compatible for enhanced security.

    [:octicons-arrow-right-24: Docker Sources](sources/docker.md)

-   :material-chart-line:{ .lg .middle } **Observable**

    ---

    Prometheus metrics, health endpoints, and structured logging built-in. Know what's happening.

    [:octicons-arrow-right-24: Observability](observability.md)

</div>

## How It Works

```text
┌─────────────────┐     ┌──────────────┐     ┌─────────────────┐
│  Docker Events  │────▶│  dnsweaver   │────▶│  DNS Providers  │
│  (start/stop)   │     │  (matching)  │     │  (A/CNAME/SRV)  │
└─────────────────┘     └──────────────┘     └─────────────────┘
```

1. A container starts with a Traefik label:

    ```yaml
    labels:
      - "traefik.http.routers.myapp.rule=Host(`myapp.home.example.com`)" # (1)!
    ```

    1. dnsweaver extracts hostnames from Traefik, Caddy, and native labels

2. dnsweaver extracts the hostname and matches it against configured provider domain patterns

3. The matching provider creates the DNS record:
    - **A record**: `myapp.home.example.com → 10.0.0.100`
    - **CNAME**: `myapp.example.com → proxy.example.com`

4. When the container stops, the DNS record is automatically cleaned up

## Quick Start

!!! example "Minimal Docker Compose"

    ```yaml
    services:
      dnsweaver:
        image: maxamill/dnsweaver:latest
        environment:
          - DNSWEAVER_INSTANCES=internal-dns # (1)!
          - DNSWEAVER_INTERNAL_DNS_TYPE=technitium # (2)!
          - DNSWEAVER_INTERNAL_DNS_URL=http://dns.internal:5380
          - DNSWEAVER_INTERNAL_DNS_TOKEN_FILE=/run/secrets/technitium_token
          - DNSWEAVER_INTERNAL_DNS_ZONE=home.example.com
          - DNSWEAVER_INTERNAL_DNS_RECORD_TYPE=A
          - DNSWEAVER_INTERNAL_DNS_TARGET=10.0.0.100 # (3)!
          - DNSWEAVER_INTERNAL_DNS_DOMAINS=*.home.example.com # (4)!
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock:ro
        secrets:
          - technitium_token
    ```

    1. Comma-separated list of provider instance names
    2. Provider type: `technitium`, `cloudflare`, `pihole`, `dnsmasq`, or `webhook`
    3. Target IP for A records (or CNAME target hostname)
    4. Domain patterns to match—wildcards supported

[:octicons-arrow-right-24: Getting Started](getting-started.md){ .md-button .md-button--primary }
[:octicons-arrow-right-24: Configuration](configuration/environment.md){ .md-button }

## Supported Providers

<div class="grid" markdown>

| Provider | Record Types | Notes |
| :------- | :----------- | :---- |
| [Technitium](providers/technitium.md) | A, AAAA, CNAME, SRV, TXT | Full-featured self-hosted DNS |
| [Cloudflare](providers/cloudflare.md) | A, AAAA, CNAME, TXT | With optional proxy support |
| [Pi-hole](providers/pihole.md) | A, AAAA, CNAME | API or file mode |
| [dnsmasq](providers/dnsmasq.md) | A, AAAA, CNAME | File-based configuration |
| [Webhook](providers/webhook.md) | A, AAAA, CNAME, TXT | Custom integrations |

</div>

---

## Next Steps

<div class="grid cards" markdown>

-   :material-rocket-launch:{ .lg .middle } **Getting Started**

    ---

    Install and configure dnsweaver in minutes.

    [:octicons-arrow-right-24: Quick Start Guide](getting-started.md)

-   :material-cog:{ .lg .middle } **Configuration**

    ---

    Full environment variable and secrets reference.

    [:octicons-arrow-right-24: Configuration Docs](configuration/environment.md)

-   :material-server:{ .lg .middle } **Deployment Examples**

    ---

    Production-ready Docker Compose and Swarm configs.

    [:octicons-arrow-right-24: Deployment](deployment/docker-compose.md)

-   :material-transit-connection-variant:{ .lg .middle } **Split-Horizon DNS**

    ---

    Internal + external records from one config.

    [:octicons-arrow-right-24: Split-Horizon Guide](deployment/split-horizon.md)

</div>
