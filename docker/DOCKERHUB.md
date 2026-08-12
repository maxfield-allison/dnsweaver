# dnsweaver

**Automatic DNS records for containers, VMs & clusters.**

dnsweaver reads hostnames from seven sources and writes records to eleven DNS providers. A container starts with a Traefik label or a Proxmox VM boots, and the record appears; when they go away, so does it. Think of it as **external-dns for the homelab**.

📚 **[Documentation](https://maxfield-allison.github.io/dnsweaver/)** · 🐙 **[GitHub](https://github.com/maxfield-allison/dnsweaver)** · 🔖 **[Releases](https://github.com/maxfield-allison/dnsweaver/releases)**

---

## Why dnsweaver?

- **Proxmox VE auto-DNS** — A records for VMs and LXCs from the PVE API. Almost nothing else does this.
- **Self-hosted DNS, first-class** — Technitium, Pi-hole, AdGuard Home, dnsmasq, OPNsense, and pfSense.
- **Seven sources** — Traefik, Caddy, nginx-proxy, native labels, Kubernetes, Proxmox VE, and Incus in one binary. Run one or all at once.
- **Split-horizon** — Internal and external records from the *same* labels (e.g. Technitium internally, Cloudflare externally).
- **Single static Go binary** — ~15 MB, multi-arch (amd64/arm64), zero runtime dependencies.

## Supported Sources

Enable any combination with `DNSWEAVER_SOURCES`.

| Source | Reads | Runs over |
|--------|-------|-----------|
| `traefik` | `traefik.http.routers.*.rule` labels, plus Traefik dynamic config files | Docker, Swarm, Incus |
| `caddy` | `caddy` / `caddy_<n>` labels from caddy-docker-proxy | Docker, Swarm, Incus |
| `nginx-proxy` | `VIRTUAL_HOST` labels from jwilder/nginx-proxy | Docker, Swarm, Incus |
| `dnsweaver` | Native `dnsweaver.*` labels for explicit record configuration | Docker, Swarm, Incus |
| `kubernetes` | Ingress, IngressRoute, HTTPRoute, and Service resources | Kubernetes |
| `proxmox` | PVE API: QEMU VMs via guest agent, and LXC containers | Proxmox VE |
| `incus` | Incus API: system containers and VMs | Incus |

## Supported Providers

| Provider | Record Types |
|----------|--------------|
| Technitium | A, AAAA, CNAME, SRV, TXT |
| Cloudflare | A, AAAA, CNAME, SRV, TXT (optional proxy) |
| OVHcloud | A, AAAA, CNAME, SRV, TXT |
| RFC 2136 (BIND, Windows DNS, Knot) | A, AAAA, CNAME, SRV, TXT |
| PowerDNS | A, AAAA, CNAME, SRV, TXT |
| Pi-hole | A, CNAME |
| AdGuard Home | A, AAAA, CNAME |
| OPNsense | A, AAAA |
| pfSense | A, AAAA |
| dnsmasq | A, CNAME |
| Webhook | Any (custom integrations) |

## Tags

- `latest` — latest stable release
- `vX.Y.Z` — specific release (recommended for production)

Multi-arch images are published for `linux/amd64` and `linux/arm64`.

Also available on GitHub Container Registry: `ghcr.io/maxfield-allison/dnsweaver`.

## Quick Start

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

A container with the label `traefik.http.routers.myapp.rule=Host(myapp.home.example.com)` gets an A record created automatically. When the container stops, the record is removed.

See the **[Getting Started guide](https://maxfield-allison.github.io/dnsweaver/getting-started/)** for the full walkthrough, Kubernetes and Proxmox setup, and provider-specific configuration.

## License

[MIT](https://github.com/maxfield-allison/dnsweaver/blob/main/LICENSE) · Maintained by [Maxfield Allison](https://github.com/maxfield-allison)
