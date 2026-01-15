# dnsweaver

**Automatic DNS record management for Docker containers with multi-provider support.**

dnsweaver watches Docker events and automatically creates and deletes DNS records for services with reverse proxy labels (Traefik, etc.). Unlike single-provider tools, dnsweaver supports **split-horizon DNS** and **multiple DNS providers** simultaneously.

## Key Features

<div class="grid cards" markdown>

-   :material-dns:{ .lg .middle } **Multi-Provider Support**

    ---

    Route different domains to different DNS providers. Technitium, Cloudflare, Pi-hole, dnsmasq, and webhook.

-   :material-sync:{ .lg .middle } **Split-Horizon DNS**

    ---

    Create internal and external records from the same container labels automatically.

-   :material-docker:{ .lg .middle } **Docker & Swarm Native**

    ---

    Works with standalone Docker and Docker Swarm clusters. Socket proxy compatible.

-   :material-chart-line:{ .lg .middle } **Observable**

    ---

    Prometheus metrics, health endpoints, and structured logging built-in.

</div>

## How It Works

```
┌─────────────────┐     ┌──────────────┐     ┌─────────────────┐
│  Docker Events  │────▶│  dnsweaver   │────▶│  DNS Providers  │
│  (start/stop)   │     │  (matching)  │     │  (A/CNAME/SRV)  │
└─────────────────┘     └──────────────┘     └─────────────────┘
```

1. A container starts with a Traefik label:
   ```yaml
   labels:
     - "traefik.http.routers.myapp.rule=Host(`myapp.home.example.com`)"
   ```

2. dnsweaver extracts the hostname and matches it against configured provider domain patterns

3. The matching provider creates the DNS record:
   - **A record**: `myapp.home.example.com → 10.0.0.100`
   - **CNAME**: `myapp.example.com → proxy.example.com`

4. When the container stops, the DNS record is automatically cleaned up

## Quick Example

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_INSTANCES=internal-dns
      - DNSWEAVER_INTERNAL_DNS_TYPE=technitium
      - DNSWEAVER_INTERNAL_DNS_URL=http://dns.internal:5380
      - DNSWEAVER_INTERNAL_DNS_TOKEN_FILE=/run/secrets/technitium_token
      - DNSWEAVER_INTERNAL_DNS_ZONE=home.example.com
      - DNSWEAVER_INTERNAL_DNS_RECORD_TYPE=A
      - DNSWEAVER_INTERNAL_DNS_TARGET=10.0.0.100
      - DNSWEAVER_INTERNAL_DNS_DOMAINS=*.home.example.com
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    secrets:
      - technitium_token
```

See the [Getting Started](getting-started.md) guide for complete setup instructions.

## Supported Providers

| Provider | Record Types | Notes |
|----------|--------------|-------|
| [Technitium](providers/technitium.md) | A, AAAA, CNAME, SRV, TXT | Full-featured self-hosted DNS |
| [Cloudflare](providers/cloudflare.md) | A, AAAA, CNAME, TXT | With optional proxy support |
| [Pi-hole](providers/pihole.md) | A, AAAA, CNAME | API or file mode |
| [dnsmasq](providers/dnsmasq.md) | A, AAAA, CNAME | File-based configuration |
| [Webhook](providers/webhook.md) | A, AAAA, CNAME, TXT | Custom integrations |

## Next Steps

- **[Getting Started](getting-started.md)** - Install and configure dnsweaver
- **[Configuration](configuration/environment.md)** - Full environment variable reference
- **[Deployment Examples](deployment/docker-compose.md)** - Production-ready configurations
- **[Split-Horizon DNS](deployment/split-horizon.md)** - Internal + external records setup
