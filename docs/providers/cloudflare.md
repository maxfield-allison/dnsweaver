---
title: Cloudflare DNS Provider
description: Automate Cloudflare DNS records from Docker, Kubernetes, and Proxmox with dnsweaver — A, AAAA, CNAME, SRV, and TXT records with optional proxying.
---

# Cloudflare

Cloudflare provides public DNS with optional proxy/CDN capabilities. dnsweaver supports Cloudflare's API for automated record management.

## Requirements

- Cloudflare account with at least one domain
- API token with DNS edit permissions

## Basic Configuration

```yaml
environment:
  - DNSWEAVER_INSTANCES=cloudflare

  - DNSWEAVER_CLOUDFLARE_TYPE=cloudflare
  - DNSWEAVER_CLOUDFLARE_TOKEN_FILE=/run/secrets/cloudflare_token
  - DNSWEAVER_CLOUDFLARE_ZONE=example.com
  - DNSWEAVER_CLOUDFLARE_RECORD_TYPE=CNAME
  - DNSWEAVER_CLOUDFLARE_TARGET=tunnel.example.com
  - DNSWEAVER_CLOUDFLARE_DOMAINS=*.example.com
```

## Configuration Reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TYPE` | Yes | - | Must be `cloudflare` |
| `TOKEN` | Yes | - | API token |
| `TOKEN_FILE` | Alt | - | Path to file containing API token |
| `ZONE_ID` | No* | - | Cloudflare Zone ID (alternative to `ZONE`) |
| `ZONE` | No* | - | DNS zone name for zone lookup |
| `RECORD_TYPE` | Yes | - | `A`, `AAAA`, `CNAME`, `SRV`, or `TXT` |
| `TARGET` | Yes | - | Record value |
| `DOMAINS` | Yes | - | Glob patterns to match |
| `EXCLUDE_DOMAINS` | No | - | Patterns to exclude |
| `TTL` | No | `300` | TTL in seconds |
| `PROXIED` | No | `true` | Enable Cloudflare proxy |

\* Either `ZONE_ID` or `ZONE` must be set. If both are provided, `ZONE_ID` takes precedence.

## Creating an API Token

1. Log into Cloudflare dashboard
2. Navigate to **My Profile** → **API Tokens**
3. Click **Create Token**
4. Use the **Edit zone DNS** template, or create custom:
   - **Permissions**: Zone → DNS → Edit
   - **Zone Resources**: Include → Specific zone → your-domain.com
5. Click **Continue to summary** → **Create Token**
6. Copy the token (shown only once)

!!! tip
    Use scoped API tokens instead of Global API Key for better security.

## Record Types

### A Records

Point to an IPv4 address (typically for origin servers):

```yaml
- DNSWEAVER_CLOUDFLARE_RECORD_TYPE=A
- DNSWEAVER_CLOUDFLARE_TARGET=203.0.113.100
```

### CNAME Records

Point to another hostname (common for Cloudflare Tunnels):

```yaml
- DNSWEAVER_CLOUDFLARE_RECORD_TYPE=CNAME
- DNSWEAVER_CLOUDFLARE_TARGET=abc123.cfargotunnel.com
```

### Proxied Records

Enable Cloudflare's CDN/proxy for the record:

```yaml
- DNSWEAVER_CLOUDFLARE_PROXIED=true
```

When proxied:
- Traffic routes through Cloudflare's network
- Origin IP is hidden
- Additional features available (caching, WAF, etc.)

### Per-Host Proxy Override (Labels)

`PROXIED` sets the default for **every** record this instance creates. To make
individual hostnames DNS-only (grey-cloud) while the rest stay proxied, use the
[native `proxied` label](../sources/native-labels.md#cloudflare-proxy-override-per-host)
on the workload:

```yaml
labels:
  # This host bypasses the Cloudflare proxy even though PROXIED=true
  - "dnsweaver.hostname=ssh.example.com"
  - "dnsweaver.proxied=false"
```

For several records on one service, use the named-record form
(`dnsweaver.records.<name>.proxied`). Records that omit `proxied` fall back to
the instance's `PROXIED` default.

## Split-Horizon with Cloudflare

Common pattern: Cloudflare for external, Technitium for internal:

```yaml
environment:
  - DNSWEAVER_INSTANCES=internal,external

  # Internal: Direct to reverse proxy
  - DNSWEAVER_INTERNAL_TYPE=technitium
  - DNSWEAVER_INTERNAL_URL=http://dns-server:5380
  - DNSWEAVER_INTERNAL_TOKEN_FILE=/run/secrets/technitium_token
  - DNSWEAVER_INTERNAL_ZONE=example.com
  - DNSWEAVER_INTERNAL_RECORD_TYPE=A
  - DNSWEAVER_INTERNAL_TARGET=192.0.2.100
  - DNSWEAVER_INTERNAL_DOMAINS=*.example.com

  # External: Through Cloudflare Tunnel
  - DNSWEAVER_EXTERNAL_TYPE=cloudflare
  - DNSWEAVER_EXTERNAL_TOKEN_FILE=/run/secrets/cloudflare_token
  - DNSWEAVER_EXTERNAL_ZONE=example.com
  - DNSWEAVER_EXTERNAL_RECORD_TYPE=CNAME
  - DNSWEAVER_EXTERNAL_TARGET=abc123.cfargotunnel.com
  - DNSWEAVER_EXTERNAL_DOMAINS=*.example.com
  - DNSWEAVER_EXTERNAL_PROXIED=true
```

## TLS Configuration

Cloudflare's public API uses publicly-trusted certificates so the defaults work out of the box. The TLS surface is still available for environments that proxy outbound traffic through an inspecting middlebox with its own CA:

| Env key | Purpose |
|---------|---------|
| `DNSWEAVER_CLOUDFLARE_TLS_CA_FILE` | Trust an additional CA bundle (PEM) — e.g. a corporate TLS-inspecting proxy |
| `DNSWEAVER_CLOUDFLARE_TLS_CERT_FILE` / `_TLS_KEY_FILE` | Present a client certificate (mTLS) |
| `DNSWEAVER_CLOUDFLARE_TLS_SERVER_NAME` | Override SNI / hostname verification |
| `DNSWEAVER_CLOUDFLARE_TLS_SKIP_VERIFY` | Disable verification (development only — **never** in production) |
| `DNSWEAVER_CLOUDFLARE_TLS_MIN_VERSION` | `1.2` (default) or `1.3` |

The legacy `DNSWEAVER_CLOUDFLARE_INSECURE_SKIP_VERIFY` variable still works but emits a deprecation warning and will be removed in a future major release.

!!! warning "Mounted certs must be readable by uid/gid 1000"
    The container drops privileges to the unprivileged `dnsweaver` user, so a
    client key mounted `root:root 0600` yields `permission denied`. See
    [TLS Certificate File Permissions](../configuration/environment.md#tls-certificate-file-permissions).


## Troubleshooting

### Authentication Error

Verify your token:

```bash
curl -X GET "https://api.cloudflare.com/client/v4/user/tokens/verify" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### Zone Not Found

Ensure your token has access to the zone:

```bash
curl -X GET "https://api.cloudflare.com/client/v4/zones?name=example.com" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### Rate Limiting

Cloudflare's API has rate limits. If you're hitting them:

- Increase `RECONCILE_INTERVAL` to reduce API calls
- Consider using a dedicated API token per zone
