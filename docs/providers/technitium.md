---
title: Technitium DNS Provider
description: Automatically manage Technitium DNS records from Docker, Kubernetes, and Proxmox with dnsweaver — A, AAAA, CNAME, SRV, and TXT support via the Technitium API.
---

# Technitium DNS

Technitium is a self-hosted DNS server with a REST API. It's the most full-featured provider in dnsweaver with support for all record types.

## Requirements

- Technitium DNS Server v11.0+ (for SRV record support) or v9.0+ (for basic records)
- API token with zone management permissions

## Basic Configuration

```yaml
environment:
  - DNSWEAVER_INSTANCES=technitium

  - DNSWEAVER_TECHNITIUM_TYPE=technitium
  - DNSWEAVER_TECHNITIUM_URL=http://dns-server:5380
  - DNSWEAVER_TECHNITIUM_TOKEN_FILE=/run/secrets/technitium_token
  - DNSWEAVER_TECHNITIUM_ZONE=home.example.com
  - DNSWEAVER_TECHNITIUM_RECORD_TYPE=A
  - DNSWEAVER_TECHNITIUM_TARGET=192.0.2.100
  - DNSWEAVER_TECHNITIUM_DOMAINS=*.home.example.com
```

## Configuration Reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TYPE` | Yes | - | Must be `technitium` |
| `URL` | Yes | - | Technitium server URL |
| `TOKEN` | Yes | - | API token |
| `TOKEN_FILE` | Alt | - | Path to file containing API token |
| `ZONE` | Yes | - | DNS zone to manage |
| `RECORD_TYPE` | Yes | - | `A`, `AAAA`, `CNAME`, `SRV`, or `TXT` |
| `TARGET` | Yes | - | Record value |
| `DOMAINS` | Yes | - | Glob patterns to match |
| `EXCLUDE_DOMAINS` | No | - | Patterns to exclude |
| `TTL` | No | `300` | Record TTL in seconds |
| `TLS_CA_FILE` | No | — | PEM CA bundle appended to system roots (private CAs) |
| `TLS_CERT_FILE` | No | — | PEM client cert for mutual TLS (pair with `TLS_KEY_FILE`) |
| `TLS_KEY_FILE` | No | — | PEM client key for mutual TLS |
| `TLS_SERVER_NAME` | No | — | SNI / verification hostname override |
| `TLS_MIN_VERSION` | No | `1.2` | Minimum TLS protocol version (`1.2` or `1.3`) |
| `TLS_SKIP_VERIFY` | No | `false` | Skip TLS certificate verification. Prefer `TLS_CA_FILE`. |
| `INSECURE_SKIP_VERIFY` | No | `false` | **Deprecated** alias of `TLS_SKIP_VERIFY` (will be removed in a future major release) |
| `AUTO_HTTPS_RECORDS` | No | `true` | Auto-create companion HTTPS records (see below) |
| `AUTO_HTTPS_ALPN` | No | `h2` | ALPN protocol for companion HTTPS records |

## Getting an API Token

1. Log into Technitium web interface
2. Navigate to **Administration** → **API Token**
3. Create a new token with appropriate permissions
4. Copy the token value

!!! warning
    Store the API token securely using Docker secrets. See [Docker Secrets](../configuration/secrets.md).

## Record Types

### A Records

Point hostnames to an IPv4 address:

```yaml
- DNSWEAVER_TECHNITIUM_RECORD_TYPE=A
- DNSWEAVER_TECHNITIUM_TARGET=192.0.2.100
```

### AAAA Records

Point hostnames to an IPv6 address:

```yaml
- DNSWEAVER_TECHNITIUM_RECORD_TYPE=AAAA
- DNSWEAVER_TECHNITIUM_TARGET=2001:db8::1
```

### CNAME Records

Point hostnames to another hostname:

```yaml
- DNSWEAVER_TECHNITIUM_RECORD_TYPE=CNAME
- DNSWEAVER_TECHNITIUM_TARGET=proxy.example.com
```

### SRV Records

Create SRV records for service discovery:

```yaml
- DNSWEAVER_TECHNITIUM_RECORD_TYPE=SRV
- DNSWEAVER_TECHNITIUM_TARGET=192.0.2.100
- DNSWEAVER_TECHNITIUM_SRV_PORT=443
- DNSWEAVER_TECHNITIUM_SRV_PRIORITY=10
- DNSWEAVER_TECHNITIUM_SRV_WEIGHT=100
```

## Multiple Zones Example

Manage multiple zones with separate instances:

```yaml
environment:
  - DNSWEAVER_INSTANCES=internal,dmz

  # Internal zone
  - DNSWEAVER_INTERNAL_TYPE=technitium
  - DNSWEAVER_INTERNAL_URL=http://dns-server:5380
  - DNSWEAVER_INTERNAL_TOKEN_FILE=/run/secrets/technitium_token
  - DNSWEAVER_INTERNAL_ZONE=internal.example.com
  - DNSWEAVER_INTERNAL_RECORD_TYPE=A
  - DNSWEAVER_INTERNAL_TARGET=192.0.2.100
  - DNSWEAVER_INTERNAL_DOMAINS=*.internal.example.com

  # DMZ zone
  - DNSWEAVER_DMZ_TYPE=technitium
  - DNSWEAVER_DMZ_URL=http://dns-server:5380
  - DNSWEAVER_DMZ_TOKEN_FILE=/run/secrets/technitium_token
  - DNSWEAVER_DMZ_ZONE=dmz.example.com
  - DNSWEAVER_DMZ_RECORD_TYPE=A
  - DNSWEAVER_DMZ_TARGET=198.51.100.100
  - DNSWEAVER_DMZ_DOMAINS=*.dmz.example.com
```

## Troubleshooting

### Connection Refused

Ensure Technitium's API is accessible from the dnsweaver container:

```bash
docker exec dnsweaver curl -s http://dns-server:5380/api/user/session/get
```

### Invalid Token

Verify your token is correct:

```bash
curl "http://dns-server:5380/api/zones/list?token=YOUR_TOKEN"
```

### TLS Certificate Errors

For servers using a private CA (e.g. an internal step-ca or Smallstep PKI),
provide the CA bundle so the certificate chain validates normally:

```yaml
- DNSWEAVER_TECHNITIUM_TLS_CA_FILE=/run/secrets/internal_ca.pem
```

For mutual-TLS (server requires a client cert):

```yaml
- DNSWEAVER_TECHNITIUM_TLS_CA_FILE=/run/secrets/internal_ca.pem
- DNSWEAVER_TECHNITIUM_TLS_CERT_FILE=/run/secrets/dnsweaver.crt
- DNSWEAVER_TECHNITIUM_TLS_KEY_FILE=/run/secrets/dnsweaver.key
```

!!! warning "Mounted certs must be readable by uid/gid 1000"
    The container drops privileges to the unprivileged `dnsweaver` user. A key
    mounted `root:root 0600` produces `permission denied`. Mounting via
    `/run/secrets/` (as above) avoids this; for bind-mounts, see
    [TLS Certificate File Permissions](../configuration/environment.md#tls-certificate-file-permissions).

If the server's certificate CN/SAN differs from the URL host (e.g. you connect
by IP but the cert is issued for a hostname), set an SNI override:

```yaml
- DNSWEAVER_TECHNITIUM_TLS_SERVER_NAME=dns.internal.example.com
```

As a last resort for self-signed certs that cannot be supplied as a CA bundle,
you can disable verification entirely — this removes MITM protection and is
**not recommended for production**:

```yaml
- DNSWEAVER_TECHNITIUM_TLS_SKIP_VERIFY=true
```

The legacy `DNSWEAVER_TECHNITIUM_INSECURE_SKIP_VERIFY` variable still works
but emits a deprecation warning and will be removed in a future major release.

## Companion HTTPS Records

By default, dnsweaver automatically creates companion HTTPS (SVCB Type 65) records whenever it creates an A, AAAA, or CNAME record in Technitium. This prevents **ECH (Encrypted Client Hello) fallback errors** that commonly affect split-horizon DNS environments.

### Why This Exists

Modern browsers (Firefox 128+, Chrome 131+) use ECH to encrypt the SNI during TLS handshakes. When a public domain has HTTPS records (provided by CDNs like Cloudflare), but your internal DNS zone doesn't, browsers may fail to connect or experience delays trying to use ECH parameters that don't apply internally.

The companion HTTPS record tells browsers "this host speaks HTTP/2 over TLS" without ECH, preventing the fallback error.

### What Gets Created

For each A/AAAA/CNAME record, dnsweaver creates:

```
app.example.com  300  IN  HTTPS  1 . alpn="h2"
```

- **Priority 1** (ServiceMode) — overrides any inherited ECH parameters
- **Target `.`** (self) — the record's own hostname, per RFC 9460
- **ALPN `h2`** — HTTP/2 over TLS (configurable)

### Behavior

- **Enabled by default** — no configuration needed
- **Safe** — skips creation if an HTTPS record already exists (won't overwrite manual records)
- **Lifecycle-managed** — companion records are deleted when the parent record is removed
- **Idempotent** — duplicate creation attempts are handled gracefully

### Configuration

```yaml
# Disable companion HTTPS records (not recommended for split-horizon setups)
- DNSWEAVER_TECHNITIUM_AUTO_HTTPS_RECORDS=false

# Change the ALPN protocol (default: h2)
- DNSWEAVER_TECHNITIUM_AUTO_HTTPS_ALPN=h2,h3
```

!!! tip
    If you use Cloudflare for external DNS and Technitium for internal DNS (a common split-horizon setup), companion HTTPS records are essential. Cloudflare provides HTTPS records automatically on their side — Technitium needs them too.

!!! note
    This feature only applies to the Technitium provider. Other providers either handle HTTPS records automatically (Cloudflare) or don't support them (Pi-hole, dnsmasq).
