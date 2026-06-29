---
title: AdGuard Home DNS Provider
description: Automate AdGuard Home DNS rewrites from Docker, Kubernetes, and Proxmox with dnsweaver — managed A, AAAA, and CNAME records via the AdGuard Home API.
---

# AdGuard Home

AdGuard Home is a network-wide ad blocker and DNS server. dnsweaver manages DNS records via AdGuard Home's DNS Rewrite API.

## Requirements

- AdGuard Home v0.107+ (DNS Rewrite API support)
- Admin credentials with API access

## Configuration

```yaml
environment:
  - DNSWEAVER_INSTANCES=adguard

  - DNSWEAVER_ADGUARD_TYPE=adguard
  - DNSWEAVER_ADGUARD_URL=http://adguard:3000
  - DNSWEAVER_ADGUARD_USERNAME=admin
  - DNSWEAVER_ADGUARD_PASSWORD_FILE=/run/secrets/adguard_password
  - DNSWEAVER_ADGUARD_ZONE=home.example.com
  - DNSWEAVER_ADGUARD_RECORD_TYPE=A
  - DNSWEAVER_ADGUARD_TARGET=192.0.2.100
  - DNSWEAVER_ADGUARD_DOMAINS=*.home.example.com
```

## Configuration Reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TYPE` | Yes | - | Must be `adguard` |
| `URL` | Yes | - | AdGuard Home web interface URL |
| `USERNAME` | Yes | - | Admin username |
| `PASSWORD` | Yes | - | Admin password |
| `PASSWORD_FILE` | Alt | - | Path to password file (Docker secrets) |
| `ZONE` | No | - | DNS zone for record filtering |
| `TTL` | No | `300` | Record TTL (for provider consistency; see note below) |
| `RECORD_TYPE` | Yes | - | `A`, `AAAA`, or `CNAME` |
| `TARGET` | Yes | - | Record value (IP or hostname) |
| `DOMAINS` | Yes | - | Glob patterns to match |
| `EXCLUDE_DOMAINS` | No | - | Patterns to exclude |

## Record Types

### A Records

Points hostnames to IPv4 addresses:

```yaml
- DNSWEAVER_ADGUARD_RECORD_TYPE=A
- DNSWEAVER_ADGUARD_TARGET=192.0.2.100
```

### AAAA Records

Points hostnames to IPv6 addresses:

```yaml
- DNSWEAVER_ADGUARD_RECORD_TYPE=AAAA
- DNSWEAVER_ADGUARD_TARGET=2001:db8::1
```

### CNAME Records

Points hostnames to another hostname:

```yaml
- DNSWEAVER_ADGUARD_RECORD_TYPE=CNAME
- DNSWEAVER_ADGUARD_TARGET=proxy.example.com
```

## Capabilities and Limitations

!!! info "No TXT Record Support — Target-Based Ownership Inference"
    AdGuard Home's DNS Rewrite API does not support TXT records. This means dnsweaver
    **cannot create ownership TXT records** (`_dnsweaver.*`) for this provider.

    Instead, dnsweaver uses **target-based ownership inference** for orphan cleanup in managed mode.
    When a record's type and target value match the provider instance's configured `RECORD_TYPE` and
    `TARGET`, dnsweaver infers that it created the record and will delete it during orphan cleanup.
    Records with different targets are preserved.

    **How each mode works:**

    - In **managed mode** (default), orphan cleanup uses target-based inference. Records matching
      your configured type+target are cleaned up; records with different targets (e.g., manually-created
      rewrites pointing elsewhere) are left untouched.
    - In **authoritative mode**, dnsweaver deletes any record in scope regardless of ownership.
      Orphan cleanup works fully, but any manually-created rewrites in matching domains will also be deleted.
    - In **additive mode**, dnsweaver only creates records and never deletes. No ownership needed.

    !!! warning "Edge Case"
        If you manually create a rewrite in AdGuard Home with the **exact same domain, record type,
        and target** as a dnsweaver-managed record, dnsweaver will treat it as its own and may delete
        it during orphan cleanup. This scenario is narrow — it requires an exact match on all three fields
        within dnsweaver's configured domain patterns.

### Other Provider Differences

| Feature | AdGuard Home | Notes |
|---------|:------------:|-------|
| A records | ✅ | |
| AAAA records | ✅ | Native support (no workarounds needed) |
| CNAME records | ✅ | |
| SRV records | ❌ | Not supported by rewrite API |
| TXT records | ❌ | Not supported by rewrite API |
| Per-record TTL | ❌ | Rewrites use the global DNS cache TTL; the `TTL` config is tracked for consistency but not sent to AdGuard |
| Native update | ✅ | Records are updated in-place via `PUT /control/rewrite/update` |
| Ownership tracking | ⚡ | Target-based inference (see above) |

!!! note "Duplicate Rewrites"
    AdGuard Home allows creating multiple rewrites with the same domain and answer.
    dnsweaver's reconciler handles deduplication, but if you're also managing rewrites
    manually in the AdGuard UI, be aware of potential duplicates.

## Recommended Mode Configuration

```yaml
# Default — managed mode with target-based ownership inference
- DNSWEAVER_ADGUARD_MODE=managed

# Write-only — no deletions, no ownership tracking needed
- DNSWEAVER_ADGUARD_MODE=additive

# Full control — only use for dnsweaver-exclusive subdomains
- DNSWEAVER_ADGUARD_MODE=authoritative
```

See [Operational Modes](../configuration/modes.md) for details on each mode.

## Docker Deployment

### Same Docker Host

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_INSTANCES=adguard
      - DNSWEAVER_ADGUARD_TYPE=adguard
      - DNSWEAVER_ADGUARD_URL=http://adguard:3000
      - DNSWEAVER_ADGUARD_USERNAME=admin
      - DNSWEAVER_ADGUARD_PASSWORD_FILE=/run/secrets/adguard_password
      - DNSWEAVER_ADGUARD_MODE=managed
      - DNSWEAVER_ADGUARD_RECORD_TYPE=A
      - DNSWEAVER_ADGUARD_TARGET=192.0.2.100
      - DNSWEAVER_ADGUARD_DOMAINS=*.home.example.com
    secrets:
      - adguard_password
    networks:
      - adguard_network

  adguardhome:
    image: adguard/adguardhome:latest
    networks:
      - adguard_network

secrets:
  adguard_password:
    file: ./adguard_password.txt

networks:
  adguard_network:
```

### Remote AdGuard Home

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_ADGUARD_URL=http://192.168.1.100:3000
      - DNSWEAVER_ADGUARD_USERNAME=admin
      - DNSWEAVER_ADGUARD_PASSWORD=your-password
```

## TLS Configuration

AdGuard Home is often fronted by a reverse proxy with a private CA or self-signed certificate. dnsweaver supports the unified TLS surface used by every HTTP provider:

| Env key | Purpose |
|---------|---------|
| `DNSWEAVER_ADGUARD_TLS_CA_FILE` | Trust a private CA bundle (PEM) |
| `DNSWEAVER_ADGUARD_TLS_CERT_FILE` / `_TLS_KEY_FILE` | Present a client certificate (mTLS) |
| `DNSWEAVER_ADGUARD_TLS_SERVER_NAME` | Override SNI / hostname verification |
| `DNSWEAVER_ADGUARD_TLS_SKIP_VERIFY` | Disable verification (development only) |
| `DNSWEAVER_ADGUARD_TLS_MIN_VERSION` | `1.2` (default) or `1.3` |

The legacy `DNSWEAVER_ADGUARD_INSECURE_SKIP_VERIFY` variable still works but emits a deprecation warning and will be removed in v2.0. See the [environment reference](../configuration/environment.md) for complete recipes.

!!! warning "Mounted certs must be readable by uid/gid 1000"
    The container drops privileges to the unprivileged `dnsweaver` user, so a
    client key mounted `root:root 0600` yields `permission denied`. See
    [TLS Certificate File Permissions](../configuration/environment.md#tls-certificate-file-permissions).


## Troubleshooting

### Authentication Failed

Verify your credentials work with a direct API call:

```bash
curl -u admin:password http://adguard:3000/control/status
```

### Records Not Appearing

1. Check that the domain matches your `DOMAINS` pattern
2. Verify the rewrite was created in AdGuard Home → Filters → DNS Rewrites
3. If using `ZONE` filtering, ensure the domain ends with the zone suffix

### Duplicate Records

If you see duplicate rewrites in AdGuard Home:

1. Check for manual rewrites that overlap with dnsweaver's management
2. Consider using `authoritative` mode on a dedicated subdomain
3. Review dnsweaver logs at debug level for reconciliation details

### Records Persist After Container Stops

This is expected in `additive` mode. In `managed` mode, records may persist after restart because
AdGuard Home doesn't support ownership TXT records — dnsweaver can't verify it created them.
See [Capabilities and Limitations](#capabilities-and-limitations) above.
