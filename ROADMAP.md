# DNSWeaver Roadmap

This document outlines the planned development path for DNSWeaver from v0.1.0 to v1.0.0 and beyond.

## Versioning Strategy

- **Minor versions** (v0.X.0) represent milestone themes
- **Patch versions** (v0.X.Y) are released for each new provider, source, or bug fix within a milestone
- **v1.0.0** represents a stable, production-ready release with comprehensive provider/source support

## Milestone Overview

| Version | Theme | Status | Key Deliverables |
|---------|-------|--------|------------------|
| v0.1.x | Foundation | ✅ Complete | Technitium, Traefik, Docker/Swarm |
| v0.2.x | Cloudflare + Webhook | ✅ Complete | Cloudflare, Webhook, Ownership tracking |
| v0.3.x | Reconciliation | ✅ Complete | IP change detection, caching, API improvements |
| v0.4.x | Record Types & Local DNS | ✅ Complete | Pi-hole, dnsmasq, SRV, AAAA, Native labels |
| v0.5.x | Foundation Hardening | 🔄 Active | Test coverage, architecture review, logging |
| v0.6.x | Additional Sources | Planned | nginx, Caddy, HAProxy |
| v0.7.x | Public Cloud DNS | Planned | Route53, Google Cloud DNS, DigitalOcean, Azure |
| v0.8.x | Enterprise DNS | Planned | unbound, AdGuard Home, PowerDNS, RFC2136 |
| v0.9.x | Pre-Release Polish | Planned | Security audit, edge cases, performance |
| v1.0.0 | Stable Release | Planned | Documentation, stability, production-ready |
| v2.0.0 | Kubernetes | Future | Ingress, Gateway API, Services |

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

---

## v0.2.x - Cloudflare + Webhook ✅

**Theme:** First public DNS provider and webhook for custom integrations

### v0.2.0 (Released)
- Cloudflare DNS provider (#24)
- Webhook provider for custom integrations (#26)

---

## v0.3.x - Reconciliation ✅

**Theme:** Reconciliation improvements and API refinements

### v0.3.0 (Released)
- IP change detection (#43, #44) - update records when target changes
- Provider record caching - reduce API calls per reconciliation cycle
- Environment variable rename: `DNSWEAVER_PROVIDERS` → `DNSWEAVER_INSTANCES`
- Technitium "Identical record" conflict detection (#56)

---

## v0.4.x - Record Types & Local DNS ✅

**Theme:** Expanded record type support and local DNS providers

### v0.4.0 (Released)
- Pi-hole DNS provider (#15)
- dnsmasq DNS provider (#28)
- Native dnsweaver labels source (#27)

### v0.4.1 (Released)
- SRV record support (#60)
- AAAA record support (#61)
- Provider `Update()` method implementation
- Enhanced configuration validation

### v0.4.2 (Released)
- Lint compliance improvements
- `DNSWEAVER_TRAEFIK_` environment variable prefix
- Configuration improvements and cleanup

---

## v0.5.x - Foundation Hardening 🔄

**Theme:** Strengthen the codebase before adding new features

The v0.4.x milestone delivered significant functionality rapidly. Before continuing to add providers and sources, we need to ensure the foundation is solid with proper test coverage, consistent architecture, and well-documented code.

### Focus Areas

1. **Test Coverage** - Core reconciler at 26%, needs significant improvement
2. **Architecture Review** - Ensure consistent patterns across all providers
3. **Logging & Observability** - Consistent log output, better debugging
4. **Bug Fixes** - Address known issues before they compound

### Issues Assigned

**High Priority:**
- #68 - Unit test coverage for core reconciliation logic
- #70 - Implement Update() method in Technitium/Webhook providers
- #67 - Clean up logging output (consistent format, reduce noise)
- #38 - Architecture review across all providers

**Medium Priority:**
- #50, #51, #55 - Provider-specific improvements
- #64 - SRV record refinements
- #45, #57 - Orphan delay persistence issues

**Technical Debt:**
- #65, #66 - Test file organization
- #69 - Configuration validation
- #71, #73 - Documentation improvements

### Success Criteria
- Reconciler test coverage > 60%
- All providers implement consistent interface patterns
- Logging follows structured slog patterns throughout
- No known bugs blocking production use

---

## v0.6.x - Additional Sources

**Theme:** Support for more reverse proxy sources

### Planned
- nginx source (labels + nginx.conf) (#13)
- Caddy source (labels + Caddyfile) (#12)
- HAProxy source (labels + haproxy.cfg) (#14)

---

## v0.7.x - Public Cloud DNS

**Theme:** Public cloud DNS providers for external domains

### Planned
- AWS Route53 DNS provider (#30)
- Google Cloud DNS provider (#33)
- DigitalOcean DNS provider (#31)
- Azure DNS provider (#32)

---

## v0.8.x - Enterprise DNS

**Theme:** Enterprise and standards-based DNS providers

### Planned
- unbound DNS provider (#29)
- AdGuard Home DNS provider (#34)
- PowerDNS provider (#16)
- RFC2136 (Dynamic DNS) provider (#18)

---

## v0.9.x - Pre-Release Polish

**Theme:** Final hardening before v1.0

### Planned
- Security and code quality audit (#36)
- Edge case testing
- Performance optimization
- API stability review

---

## v1.0.0 - Stable Release

**Theme:** Production-ready with comprehensive documentation

### v1.0.0 Targets

**Providers (12):**
- Technitium ✅
- Cloudflare ✅
- Webhook ✅
- Pi-hole ✅
- dnsmasq ✅
- Route53
- Google Cloud DNS
- DigitalOcean
- Azure DNS
- unbound
- AdGuard Home
- PowerDNS
- RFC2136

**Sources (5):**
- Traefik ✅
- Native dnsweaver labels ✅
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
