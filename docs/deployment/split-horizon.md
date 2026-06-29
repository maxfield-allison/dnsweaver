---
title: Split-Horizon DNS
description: Set up split-horizon (split-brain) DNS for your homelab with dnsweaver — route internal hostnames to Technitium or Pi-hole and public ones to Cloudflare from the same labels.
---

# Split-Horizon DNS

Split-horizon DNS (also called split-brain DNS) allows the same hostname to resolve to different addresses depending on where the query originates. dnsweaver makes this easy with multi-provider support.

!!! tip "Looking for dual-stack (A + AAAA)?"
    If you want both IPv4 and IPv6 records for the same hostname, see the [dual-stack guide](dual-stack.md).

## What Is Split-Horizon DNS?

```mermaid
flowchart TB
    subgraph Internal["Internal Query (LAN)"]
        A1[app.example.com] --> B1[192.0.2.100]
        B1 --> C1[Direct to reverse proxy]
    end
    subgraph External["External Query (Internet)"]
        A2[app.example.com] --> B2[203.0.113.50]
        B2 --> C2[Public IP or tunnel]
    end
```

This allows:
- Internal clients to reach services directly (faster, no hairpin NAT)
- External clients to reach services through your public endpoint
- Same hostnames work everywhere

## Basic Setup

Configure two provider instances with overlapping domain patterns:

```yaml
environment:
  - DNSWEAVER_INSTANCES=internal,external

  # Internal DNS (Technitium, Pi-hole, etc.)
  - DNSWEAVER_INTERNAL_TYPE=technitium
  - DNSWEAVER_INTERNAL_URL=http://dns-server:5380
  - DNSWEAVER_INTERNAL_TOKEN_FILE=/run/secrets/technitium_token
  - DNSWEAVER_INTERNAL_ZONE=example.com
  - DNSWEAVER_INTERNAL_RECORD_TYPE=A
  - DNSWEAVER_INTERNAL_TARGET=192.0.2.100
  - DNSWEAVER_INTERNAL_DOMAINS=*.example.com

  # External DNS (Cloudflare)
  - DNSWEAVER_EXTERNAL_TYPE=cloudflare
  - DNSWEAVER_EXTERNAL_TOKEN_FILE=/run/secrets/cloudflare_token
  - DNSWEAVER_EXTERNAL_ZONE=example.com
  - DNSWEAVER_EXTERNAL_RECORD_TYPE=CNAME
  - DNSWEAVER_EXTERNAL_TARGET=tunnel.example.com
  - DNSWEAVER_EXTERNAL_DOMAINS=*.example.com
```

When container `app.example.com` starts:
- Internal DNS → `A` record → `192.0.2.100`
- External DNS → `CNAME` record → `tunnel.example.com`

!!! tip "Companion HTTPS Records"
    When using Technitium for internal DNS, dnsweaver automatically creates companion HTTPS (SVCB) records alongside A/CNAME records. This prevents ECH (Encrypted Client Hello) fallback errors that commonly occur in split-horizon setups where external DNS (e.g., Cloudflare) provides HTTPS records but internal DNS doesn't. See [Technitium — Companion HTTPS Records](../providers/technitium.md#companion-https-records) for details.

## Internal-Only Services

Some services should only be accessible internally. Use exclusion patterns:

```yaml
environment:
  - DNSWEAVER_INSTANCES=internal,external

  # Internal DNS - all subdomains
  - DNSWEAVER_INTERNAL_DOMAINS=*.example.com

  # External DNS - exclude internal-only subdomains
  - DNSWEAVER_EXTERNAL_DOMAINS=*.example.com
  - DNSWEAVER_EXTERNAL_EXCLUDE_DOMAINS=*.internal.example.com,admin.example.com
```

Now:
- `app.example.com` → records in both providers
- `db.internal.example.com` → internal only
- `admin.example.com` → internal only

## Architecture Patterns

### Pattern 1: Cloudflare Tunnel + Internal DNS

```mermaid
flowchart LR
    subgraph External["External Path"]
        E1[Internet] --> E2[Cloudflare] --> E3[Tunnel] --> E4[Reverse Proxy] --> E5[Container]
    end
    subgraph Internal["Internal Path"]
        I1[LAN] --> I2[Internal DNS] --> I3[Reverse Proxy] --> I4[Container]
    end
```

```yaml
# External: CNAME to Cloudflare Tunnel
- DNSWEAVER_EXTERNAL_TYPE=cloudflare
- DNSWEAVER_EXTERNAL_RECORD_TYPE=CNAME
- DNSWEAVER_EXTERNAL_TARGET=abc123.cfargotunnel.com

# Internal: A record to reverse proxy
- DNSWEAVER_INTERNAL_TYPE=technitium
- DNSWEAVER_INTERNAL_RECORD_TYPE=A
- DNSWEAVER_INTERNAL_TARGET=192.0.2.100
```

### Pattern 2: Public IP + Internal IP

```mermaid
flowchart LR
    subgraph External["External Path"]
        E1[Internet] --> E2[Public IP] --> E3[NAT] --> E4[Reverse Proxy] --> E5[Container]
    end
    subgraph Internal["Internal Path"]
        I1[LAN] --> I2[Internal DNS] --> I3[Reverse Proxy] --> I4[Container]
    end
```

```yaml
# External: A record to public IP
- DNSWEAVER_EXTERNAL_TYPE=cloudflare
- DNSWEAVER_EXTERNAL_RECORD_TYPE=A
- DNSWEAVER_EXTERNAL_TARGET=203.0.113.50

# Internal: A record to private IP
- DNSWEAVER_INTERNAL_TYPE=technitium
- DNSWEAVER_INTERNAL_RECORD_TYPE=A
- DNSWEAVER_INTERNAL_TARGET=192.0.2.100
```

### Pattern 3: Different Subdomains

Different subdomain schemes for internal vs external:

```yaml
# Internal: *.home.example.com → 192.0.2.100
- DNSWEAVER_INTERNAL_DOMAINS=*.home.example.com

# External: *.example.com (excluding .home) → tunnel
- DNSWEAVER_EXTERNAL_DOMAINS=*.example.com
- DNSWEAVER_EXTERNAL_EXCLUDE_DOMAINS=*.home.example.com
```

## Complete Example

Full Docker Compose with split-horizon:

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_LOG_LEVEL=info
      - DNSWEAVER_INSTANCES=internal,external

      # Internal: Technitium for LAN
      - DNSWEAVER_INTERNAL_TYPE=technitium
      - DNSWEAVER_INTERNAL_URL=http://dns.lan:5380
      - DNSWEAVER_INTERNAL_TOKEN_FILE=/run/secrets/technitium_token
      - DNSWEAVER_INTERNAL_ZONE=example.com
      - DNSWEAVER_INTERNAL_RECORD_TYPE=A
      - DNSWEAVER_INTERNAL_TARGET=192.0.2.100
      - DNSWEAVER_INTERNAL_DOMAINS=*.example.com

      # External: Cloudflare with proxy
      - DNSWEAVER_EXTERNAL_TYPE=cloudflare
      - DNSWEAVER_EXTERNAL_TOKEN_FILE=/run/secrets/cloudflare_token
      - DNSWEAVER_EXTERNAL_ZONE=example.com
      - DNSWEAVER_EXTERNAL_RECORD_TYPE=CNAME
      - DNSWEAVER_EXTERNAL_TARGET=tunnel.example.com
      - DNSWEAVER_EXTERNAL_DOMAINS=*.example.com
      - DNSWEAVER_EXTERNAL_EXCLUDE_DOMAINS=*.internal.example.com,*.home.example.com
      - DNSWEAVER_EXTERNAL_PROXIED=true
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    secrets:
      - technitium_token
      - cloudflare_token

  # Example app - gets records in both DNS providers
  webapp:
    image: nginx
    labels:
      - "traefik.http.routers.webapp.rule=Host(`webapp.example.com`)"

  # Internal-only app - only internal DNS
  database-ui:
    image: adminer
    labels:
      - "traefik.http.routers.dbui.rule=Host(`db.internal.example.com`)"

secrets:
  technitium_token:
    file: ./secrets/technitium.txt
  cloudflare_token:
    file: ./secrets/cloudflare.txt
```

## Verification

Test that both DNS providers have correct records:

```bash
# Query internal DNS
dig @192.0.2.53 webapp.example.com

# Query external DNS (Cloudflare)
dig @1.1.1.1 webapp.example.com
```

## Troubleshooting

### Records Only in One Provider

Check domain patterns match:

```bash
docker logs dnsweaver 2>&1 | grep "webapp.example.com"
```

### Exclusion Not Working

Verify exclusion patterns are correct:

```yaml
# ✅ Correct - pattern matches subdomains
EXCLUDE_DOMAINS=*.internal.example.com

# ❌ Wrong - won't match subdomains
EXCLUDE_DOMAINS=internal.example.com
```
