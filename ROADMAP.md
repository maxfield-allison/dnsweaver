# DNSWeaver Roadmap

This document outlines the planned development path for DNSWeaver from v0.1.0 to v1.0.0 and beyond.

## Versioning Strategy

- **Minor versions** (v0.X.0) represent milestone themes
- **Patch versions** (v0.X.Y) are released for each new provider, source, or bug fix within a milestone
- **v1.0.0** represents a stable, production-ready release with comprehensive provider/source support

## Milestone Overview

| Version | Theme | Key Deliverables |
|---------|-------|------------------|
| v0.1.x | Foundation | Technitium, Traefik, Docker/Swarm, TOML support |
| v0.2.x | Public DNS + Escape Hatch | Cloudflare, Webhook |
| v0.3.x | Labels + nginx | Native dnsweaver labels, nginx source |
| v0.4.x | Interleaved Providers | Route53, Pi-hole |
| v0.5.x | Sources + Private DNS | Caddy, dnsmasq, unbound |
| v0.6.x | Public Cloud DNS | Google Cloud DNS, DigitalOcean |
| v0.7.x | Enterprise + AdGuard | HAProxy, Azure DNS, AdGuard Home |
| v0.8.x | Standards + PowerDNS | PowerDNS, RFC2136 |
| v0.9.x | Hardening + Review | Security audit, code review, edge cases |
| v1.0.0 | Stable Release | Documentation, stability, production-ready |
| v2.0.0 | Kubernetes | Ingress, Gateway API, Services |

---

## v0.1.x - Foundation ✅

**Theme:** Core architecture and first working release

### v0.1.0 (Released)
- Technitium DNS provider
- Traefik source (Docker labels + static file YAML discovery)
- Multi-provider routing with domain pattern matching
- Docker Swarm and standalone support
- Socket proxy support
- Prometheus metrics
- Health endpoints (/health, /ready, /metrics)
- Multi-arch images (amd64, arm64)

### v0.1.1 (Planned)
- TOML file support for Traefik static file discovery (#25)

---

## v0.2.x - Public DNS + Escape Hatch

**Theme:** First public DNS provider and webhook for custom integrations

### Planned
- Cloudflare DNS provider (#24)
- Webhook provider for custom integrations (#26)

---

## v0.3.x - Labels + nginx

**Theme:** Native dnsweaver labels and first non-Traefik source

### Planned
- Native dnsweaver labels source (#27)
- nginx source (labels + nginx.conf) (#13)

---

## v0.4.x - Interleaved Providers

**Theme:** Mix of public and private DNS providers

### Planned
- AWS Route53 DNS provider (#30)
- Pi-hole DNS provider (#15)

---

## v0.5.x - Sources + Private DNS

**Theme:** Additional sources and private DNS providers

### Planned
- Caddy source (labels + Caddyfile) (#12)
- dnsmasq DNS provider (#28)
- unbound DNS provider (#29)

---

## v0.6.x - Public Cloud DNS

**Theme:** Additional public cloud DNS providers

### Planned
- Google Cloud DNS provider (#33)
- DigitalOcean DNS provider (#31)

---

## v0.7.x - Enterprise + AdGuard

**Theme:** Enterprise sources and homelab-popular providers

### Planned
- HAProxy source (labels + haproxy.cfg) (#14)
- Azure DNS provider (#32)
- AdGuard Home DNS provider (#34)

---

## v0.8.x - Standards + PowerDNS

**Theme:** Enterprise and standards-based DNS

### Planned
- PowerDNS provider (#16)
- RFC2136 (Dynamic DNS) provider (#18)

---

## v0.9.x - Hardening + Review

**Theme:** Pre-release hardening and quality assurance

### Planned
- Security and code quality audit (#36)
- Edge case testing
- Performance optimization

---

## v1.0.0 - Stable Release

**Theme:** Production-ready with comprehensive documentation

### v1.0.0 Targets

**Providers (13):**
- Technitium ✅
- Cloudflare
- Webhook
- Route53
- Pi-hole
- dnsmasq
- unbound
- Google Cloud DNS
- DigitalOcean
- Azure DNS
- AdGuard Home
- PowerDNS
- RFC2136

**Sources (5):**
- Traefik ✅
- Native dnsweaver labels
- nginx
- Caddy
- HAProxy

### Planned
- Comprehensive documentation for v1.0.0 (#35)
- Full user guide
- Provider and source guides
- Contributing guide

---

## v2.0.0 - Kubernetes (Future)

**Theme:** Kubernetes ecosystem support

### Planned
- Kubernetes source (Ingress/Service) (#17)
- Gateway API support
- Annotations support
- Multi-cluster awareness
- Core/Agent architecture (#23)

---

## Contributing

We welcome contributions! If you'd like to implement a provider or source, please:

1. Check the issues for existing work
2. Open an issue to discuss your approach
3. Follow the patterns established in existing providers/sources
4. Include tests and documentation

## Issue Labels

- `component::provider` - DNS provider implementations
- `component::source` - Hostname source implementations
- `priority::high/medium/low` - Implementation priority
- `status::todo/in-progress/done` - Current status
- `type::feature/bug/documentation` - Issue type
