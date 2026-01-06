# DNSWeaver Architecture Decisions

> **Status:** Pre-implementation alignment document
> **Created:** January 5, 2026
> **Authors:** @maxfield-allison

This document captures architectural decisions made before implementation begins. It serves as the source of truth for design choices and their rationale.

---

## Table of Contents

1. [Project Identity](#1-project-identity)
2. [Technical Stack](#2-technical-stack)
3. [Provider Configuration](#3-provider-configuration)
4. [Domain Matching](#4-domain-matching)
5. [Error Handling](#5-error-handling)
6. [Testing Philosophy](#6-testing-philosophy)
7. [CI/CD & Release Workflow](#7-cicd--release-workflow)
8. [v0.1.0 Scope](#8-v010-scope)

---

## 1. Project Identity

| Aspect | Decision |
|--------|----------|
| **Go module (development)** | `gitlab.bluewillows.net/root/dnsweaver` |
| **Go module (public release)** | `github.com/maxfield-allison/dnsweaver` |
| **Docker images** | `maxamill/dnsweaver` (Docker Hub), `ghcr.io/maxfield-allison/dnsweaver` (GitHub) |
| **License** | MIT |
| **Minimum Go version** | 1.24+ (Docker SDK requirement) |

### Rationale

- Development happens privately on GitLab
- On release, module paths are rewritten for public consumption
- Dual Docker registry ensures availability (Docker Hub rate limits, GitHub for ecosystem)

---

## 2. Technical Stack

| Component | Choice | Rationale |
|-----------|--------|-----------|
| **Logging** | `log/slog` (stdlib) | Zero dependencies, sufficient for needs, Go team's recommended approach |
| **Metrics** | `prometheus/client_golang` | Industry standard, excellent ecosystem |
| **Configuration** | Environment variables only | Docker-native, no file parsing complexity |
| **HTTP server** | `net/http` (stdlib) | Minimal endpoints, no framework needed |
| **Docker SDK** | `github.com/docker/docker` | Required for container/service inspection |

### Why slog over zerolog?

While SwarmWeaver uses zerolog, DNSWeaver will use slog because:

1. No external dependency
2. Sufficient performance for this use case (not a high-throughput logging scenario)
3. If a shared library is ever needed, slog's Handler interface allows writing adapters
4. Different projects can have different logging needs

---

## 3. Provider Configuration

### Instance Model

DNSWeaver uses an **explicit instance model** where users define named provider instances.

```bash
# Explicit list of provider instance names (order matters for domain matching)
DNSWEAVER_PROVIDERS=internal-dns,public-dns

# Each instance has TYPE + provider-specific settings
DNSWEAVER_INTERNAL_DNS_TYPE=technitium
DNSWEAVER_INTERNAL_DNS_URL=http://dns.example.com:5380
DNSWEAVER_INTERNAL_DNS_TOKEN_FILE=/run/secrets/tech_token
DNSWEAVER_INTERNAL_DNS_ZONE=internal.example.com
DNSWEAVER_INTERNAL_DNS_RECORD_TYPE=A
DNSWEAVER_INTERNAL_DNS_TARGET=10.0.0.100
DNSWEAVER_INTERNAL_DNS_DOMAINS=*.internal.example.com

DNSWEAVER_PUBLIC_DNS_TYPE=cloudflare
DNSWEAVER_PUBLIC_DNS_TOKEN_FILE=/run/secrets/cf_token
DNSWEAVER_PUBLIC_DNS_ZONE_ID=abc123
DNSWEAVER_PUBLIC_DNS_RECORD_TYPE=CNAME
DNSWEAVER_PUBLIC_DNS_TARGET=example.com
DNSWEAVER_PUBLIC_DNS_DOMAINS=*.example.com
DNSWEAVER_PUBLIC_DNS_EXCLUDE_DOMAINS=*.internal.example.com
```

### Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Instance naming** | User-provided via `DNSWEAVER_PROVIDERS` | Explicit is better than implicit; enables validation |
| **Type field** | Required `_TYPE` per instance | Consistent, even if instance name matches type |
| **Ordering** | `DNSWEAVER_PROVIDERS` order = evaluation order | Predictable, user-controlled priority |
| **Config ownership** | Each provider parses its own config | Separation of concerns |
| **Multiple instances of same type** | Fully supported | e.g., two Cloudflare accounts |

### Environment Variable Naming

Pattern: `DNSWEAVER_{INSTANCE_NAME}_{SETTING}`

- Instance names are normalized to uppercase with `-` converted to `_`
- `internal-dns` → looks for `DNSWEAVER_INTERNAL_DNS_*`

### Secret Handling

All sensitive values support `_FILE` suffix for Docker secrets:

```bash
# Direct value (not recommended for production)
DNSWEAVER_INTERNAL_DNS_TOKEN=secret123

# File-based (Docker secrets pattern)
DNSWEAVER_INTERNAL_DNS_TOKEN_FILE=/run/secrets/dns_token
```

### TTL Configuration

```bash
# Global default (applies if provider/instance doesn't specify)
DNSWEAVER_DEFAULT_TTL=300

# Provider-specific defaults (built into provider code)
# - Technitium: 300
# - Cloudflare: 300 (or "auto" → 1 when proxied)

# Instance-level override
DNSWEAVER_INTERNAL_DNS_TTL=600
```

**Precedence:** Instance TTL > Provider default > Global default (300)

---

## 4. Domain Matching

### Dual-Mode Matching

DNSWeaver supports both **glob patterns** (simple) and **regex** (powerful).

#### Glob Patterns (Default)

```bash
# Simple wildcards
DNSWEAVER_INTERNAL_DNS_DOMAINS=*.internal.example.com
DNSWEAVER_INTERNAL_DNS_EXCLUDE_DOMAINS=admin.internal.example.com
```

| Pattern | Matches | Doesn't Match |
|---------|---------|---------------|
| `*.example.com` | `app.example.com`, `a.b.example.com` | `example.com` |
| `?.example.com` | `a.example.com` | `app.example.com` |
| `app.example.com` | exactly `app.example.com` | anything else |

#### Regex Patterns (Opt-in)

```bash
# Explicit regex with _REGEX suffix
DNSWEAVER_INTERNAL_DNS_DOMAINS_REGEX=^[a-z0-9-]+\.internal\.example\.com$
DNSWEAVER_INTERNAL_DNS_EXCLUDE_DOMAINS_REGEX=^(test|dev)\..*
```

### Matching Behavior

1. **Exclude patterns evaluated first** — if any exclude matches, skip this provider
2. **Include patterns evaluated second** — at least one must match
3. **First provider wins** — `DNSWEAVER_PROVIDERS` order determines priority
4. **No match = warning** — hostname logged as unhandled, no DNS record created

### Configuration Validation

| Scenario | Behavior |
|----------|----------|
| Both `_DOMAINS` and `_DOMAINS_REGEX` set | Error: choose one |
| Invalid regex pattern | Error at startup |
| Empty domains for a provider | Error: at least one pattern required |

---

## 5. Error Handling

### Configuration Errors

**Decision:** Fail fast with clear messaging.

```
If any provider instance has invalid configuration:
  → Log detailed error explaining what's wrong
  → Exit with non-zero status
  → Do NOT start with partial configuration
```

**Rationale:** Silent partial failures lead to confusion. Users should know immediately if something is misconfigured.

### Runtime Errors

| Scenario | Behavior |
|----------|----------|
| Provider API temporarily unreachable | Retry with backoff, log warning |
| Provider API returns error | Log error, continue with other providers |
| Docker socket unavailable | Fatal error, exit |
| Hostname matches no providers | Log warning, skip hostname |

### Health Check Behavior

- `/health` — Always 200 if process is running
- `/ready` — 503 if any provider fails ping, 200 if all healthy

---

## 6. Testing Philosophy

### Principles

1. **Test behavior, not implementation** — tests verify outcomes, not internal mechanics
2. **Meaningful assertions** — each test should catch real bugs
3. **No coverage theater** — good tests > high coverage numbers
4. **Table-driven tests** — Go idiom, easy to extend

### Coverage Targets

| Package | Target | Notes |
|---------|--------|-------|
| `internal/config` | 80%+ | Critical for correct startup |
| `internal/matcher` | 90%+ | Core domain matching logic |
| `pkg/provider` | 70%+ | Interface + registry |
| `providers/*` | 70%+ | API client testing with mocks |
| `internal/reconciler` | 80%+ | Core business logic |
| `internal/docker` | 50%+ | Harder to test, requires socket |

### Test Types

| Type | Purpose | Location |
|------|---------|----------|
| Unit | Pure logic (config parsing, matching) | `*_test.go` next to code |
| Integration | HTTP client behavior | `*_test.go` with mocked servers |
| E2E | Full workflow (optional) | `test/e2e/` directory |

---

## 7. CI/CD & Release Workflow

### Branching Model

GitFlow-inspired:

| Branch | Purpose |
|--------|---------|
| `main` | Stable, tagged releases only |
| `develop` | Integration branch, always deployable |
| `feature/*` | Feature development |
| `bugfix/*` | Bug fixes |
| `hotfix/*` | Urgent production fixes |

### Pipeline Stages

```
validate → test → security → build → docker → deploy → github-release
```

### GitHub Integration

**Development:** All work happens on private GitLab.

**On version tag (e.g., `v1.0.0`):**
1. CI creates clean export (respects `.gitattributes export-ignore`)
2. Module paths rewritten: `gitlab.bluewillows.net/root/dnsweaver` → `github.com/maxfield-allison/dnsweaver`
3. Force-push to GitHub main + tag
4. Create GitHub Release with binaries

**Accepting GitHub PRs:**
1. Review and merge PR on GitHub
2. Pull GitHub changes to local branch
3. Cherry-pick/rebase into GitLab repo (reverse path rewrite)
4. Continue development on GitLab
5. Next release includes the contribution, PR shows as "merged"

### Files Excluded from GitHub

Via `.gitattributes export-ignore`:
- `.gitlab-ci.yml`
- `docs/internal/` (if any)
- Development-only configs

---

## 8. v0.1.0 Scope

### Included

| Component | Notes |
|-----------|-------|
| Project scaffold | Go module, dirs, Dockerfile, CI |
| Provider interface | Core abstraction |
| Source interface | Core abstraction |
| Technitium provider | First provider implementation |
| Traefik source | First source implementation |
| Docker client | Swarm + standalone support |
| Multi-provider routing | Domain-based routing |
| A + CNAME record types | Full record type support |
| Config system | Env vars, `_FILE` secrets, validation |
| Prometheus metrics | `dnsweaver_*` namespace |
| Health endpoints | `/health`, `/ready`, `/metrics` |
| Dry-run mode | Safe testing |
| Event-driven updates | Docker event watching |

### Deferred

| Component | Target Milestone |
|-----------|------------------|
| Cloudflare provider | v0.2.0 |
| Webhook provider | v0.2.0 |
| Caddy source | v0.3.0 |
| nginx-proxy source | v0.3.0 |
| Pi-hole provider | v0.4.0 |
| PowerDNS provider | v0.4.0 |
| Manual reconcile API | v0.3.0+ |
| Kubernetes source | v1.0.0 |

---

## Appendix: Configuration Reference

### Global Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_LOG_LEVEL` | `info` | Logging level: debug, info, warn, error |
| `DNSWEAVER_LOG_FORMAT` | `json` | Log format: json, text |
| `DNSWEAVER_DRY_RUN` | `false` | Log changes without applying |
| `DNSWEAVER_DEFAULT_TTL` | `300` | Default TTL for DNS records |
| `DNSWEAVER_RECONCILE_INTERVAL` | `60s` | Full reconciliation interval |
| `DNSWEAVER_HEALTH_PORT` | `8080` | Port for health/metrics endpoints |

### Docker Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker host |
| `DNSWEAVER_DOCKER_MODE` | `auto` | Mode: auto, swarm, standalone |

### Source Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `DNSWEAVER_SOURCE` | `traefik` | Hostname source: traefik |

### Provider Instance Settings

| Variable | Required | Description |
|----------|----------|-------------|
| `DNSWEAVER_PROVIDERS` | Yes | Comma-separated list of instance names |
| `DNSWEAVER_{NAME}_TYPE` | Yes | Provider type: technitium, cloudflare, webhook |
| `DNSWEAVER_{NAME}_RECORD_TYPE` | Yes | A or CNAME |
| `DNSWEAVER_{NAME}_TARGET` | Yes | IP (for A) or hostname (for CNAME) |
| `DNSWEAVER_{NAME}_DOMAINS` | Yes | Glob pattern for matching hostnames |
| `DNSWEAVER_{NAME}_DOMAINS_REGEX` | No | Regex pattern (alternative to DOMAINS) |
| `DNSWEAVER_{NAME}_EXCLUDE_DOMAINS` | No | Glob pattern for exclusions |
| `DNSWEAVER_{NAME}_EXCLUDE_DOMAINS_REGEX` | No | Regex pattern for exclusions |
| `DNSWEAVER_{NAME}_TTL` | No | Override TTL for this provider |

### Technitium-Specific Settings

| Variable | Required | Description |
|----------|----------|-------------|
| `DNSWEAVER_{NAME}_URL` | Yes | Technitium API URL |
| `DNSWEAVER_{NAME}_TOKEN` | Yes* | API token (*or use _FILE) |
| `DNSWEAVER_{NAME}_TOKEN_FILE` | Yes* | Path to token file |
| `DNSWEAVER_{NAME}_ZONE` | Yes | DNS zone to manage |

---

*This document will be updated as implementation progresses and new decisions are made.*
