---
title: UniFi Network DNS Provider
description: Automate UniFi Network DNS records from Docker, Kubernetes, and Proxmox with dnsweaver — managed A, AAAA, CNAME, TXT, and SRV records via the official UniFi Integration API.
---

# UniFi Network

[UniFi Network](https://www.ui.com/) is Ubiquiti's network controller. dnsweaver manages **DNS policy** records on the console via the official [UniFi Network Integration API](https://developer.ui.com/).

## Requirements

- A **UniFi OS console** (Dream Machine, Cloud Gateway, Cloud Key) or **UniFi OS Server** (Ubiquiti's self-hosted UniFi OS for Linux)
- UniFi Network **10.1 or later** (DNS policies were added to the Integration API in 10.1)
- An Integration API key, created under **Settings → Control Plane → Integrations → Create API Key**
- Network reachability from dnsweaver to the console's HTTPS port

!!! warning "Standalone Network Application is not supported"
    The legacy standalone Network Application (the Linux, Windows, macOS and Docker builds, such as the `linuxserver/unifi-network-application` image) does not expose the Integration API or API keys, so it has no Integrations page and cannot be used with this provider. Ubiquiti's replacement for it is UniFi OS Server, which does support both.

!!! note "Official API only"
    dnsweaver uses the versioned, documented Integration API (`/proxy/network/integration/v1`), not the undocumented `static-dns` endpoints older tooling relied on. The legacy endpoints need cookie login and CSRF handling and have no native update; the Integration API needs only an API key.

## Configuration

```yaml
environment:
  - DNSWEAVER_INSTANCES=unifi

  - DNSWEAVER_UNIFI_TYPE=unifi
  - DNSWEAVER_UNIFI_URL=https://192.168.1.1
  - DNSWEAVER_UNIFI_API_KEY_FILE=/run/secrets/unifi_api_key
  - DNSWEAVER_UNIFI_SITE=default
  - DNSWEAVER_UNIFI_ZONE=home.example.com
  - DNSWEAVER_UNIFI_TLS_SKIP_VERIFY=true   # or TLS_CA_FILE, see TLS below
  - DNSWEAVER_UNIFI_RECORD_TYPE=A
  - DNSWEAVER_UNIFI_TARGET=192.168.1.100
  - DNSWEAVER_UNIFI_DOMAINS=*.home.example.com
```

## Configuration Reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TYPE` | Yes | - | Must be `unifi` |
| `URL` | Yes | - | Console base URL (`https://192.168.1.1`) |
| `API_KEY` | Yes | - | Integration API key (sent as the `X-API-KEY` header) |
| `API_KEY_FILE` | Alt | - | Path to API key file (Docker secrets) |
| `SITE` | No | `default` | Site `internalReference` (as shown in the console URL) or site UUID |
| `ZONE` | No | - | DNS zone for record filtering |
| `TTL` | No | `300` | TTL for A, AAAA, and CNAME records (TXT and SRV have no TTL on UniFi) |
| `RECORD_TYPE` | Yes | - | `A`, `AAAA`, or `CNAME` |
| `TARGET` | Yes | - | Record value (IP or hostname) |
| `DOMAINS` | Yes | - | Glob patterns to match |
| `EXCLUDE_DOMAINS` | No | - | Patterns to exclude |

## Record Types

### A Records

Points hostnames to IPv4 addresses:

```yaml
- DNSWEAVER_UNIFI_RECORD_TYPE=A
- DNSWEAVER_UNIFI_TARGET=192.168.1.100
```

### AAAA Records

Points hostnames to IPv6 addresses:

```yaml
- DNSWEAVER_UNIFI_RECORD_TYPE=AAAA
- DNSWEAVER_UNIFI_TARGET=2001:db8::1
```

### CNAME Records

Points hostnames to another hostname:

```yaml
- DNSWEAVER_UNIFI_RECORD_TYPE=CNAME
- DNSWEAVER_UNIFI_TARGET=proxy.home.example.com
```

### SRV and TXT Records

SRV records are created from workload labels (see [Native Labels](../sources/native-labels.md#srv-record-minecraft-server)). UniFi stores SRV records as separate service, protocol, and domain fields, so the hostname must have the standard `_service._proto.domain` form, for example `_minecraft._tcp.home.example.com`.

TXT records are used for [ownership tracking](#capabilities-and-limitations) and are managed automatically.

## Capabilities and Limitations

UniFi's Integration API supports TXT records, so dnsweaver uses its standard `_dnsweaver.*` ownership records here. Managed mode works fully: orphan cleanup only ever removes records dnsweaver created, and records you add by hand in the console are left alone.

| Feature | UniFi Network | Notes |
|---------|:-------------:|-------|
| A records | ✅ | |
| AAAA records | ✅ | |
| CNAME records | ✅ | |
| SRV records | ✅ | Hostname must be `_service._proto.domain` |
| TXT records | ✅ | Ownership tracking supported |
| Per-record TTL | ⚡ | A, AAAA, and CNAME only; TXT and SRV have no TTL on UniFi and report the instance `TTL` |
| Native update | ✅ | Records are replaced in place via `PUT` |
| Ownership tracking | ✅ | Standard `_dnsweaver.*` TXT records |

!!! info "What dnsweaver will and won't touch"
    - Only policies the console marks as **user-defined** are managed. Entries the console derives from clients, devices, or other settings are never listed or deleted, even in authoritative mode.
    - **Disabled** policies are treated as absent: they are never listed, updated, or deleted. If you disable a dnsweaver-managed record in the console, dnsweaver sees it as missing and tries to create it again, which the console may reject as a duplicate. Delete the record, or remove the workload label, instead of disabling it.
    - `MX` and `FORWARD_DOMAIN` policies are ignored.
    - Domain names are limited to 127 characters by the API.

!!! note "TXT values and commas"
    The Integration API requires TXT text containing commas to be enclosed in double quotes. Every dnsweaver ownership value contains commas, so dnsweaver quotes on write and unquotes on read. You will see the quotes if you inspect the record in the console; that is expected. One value cannot be represented losslessly: TXT text that both contains a comma and is itself wrapped in double quotes. dnsweaver rejects that value with a clear error rather than storing it ambiguously.

## Recommended Mode Configuration

```yaml
# Default — managed mode with TXT ownership tracking
- DNSWEAVER_UNIFI_MODE=managed

# Write-only — no deletions
- DNSWEAVER_UNIFI_MODE=additive

# Full control — deletes any user-defined record in scope, use on a dedicated subdomain
- DNSWEAVER_UNIFI_MODE=authoritative
```

See [Operational Modes](../configuration/modes.md) for details on each mode.

## Multiple Sites

Each provider instance targets one site. To manage records on several sites of the same console, define one instance per site:

```yaml
- DNSWEAVER_INSTANCES=unifi-main,unifi-branch

- DNSWEAVER_UNIFI_MAIN_TYPE=unifi
- DNSWEAVER_UNIFI_MAIN_URL=https://192.168.1.1
- DNSWEAVER_UNIFI_MAIN_API_KEY_FILE=/run/secrets/unifi_api_key
- DNSWEAVER_UNIFI_MAIN_SITE=default
- DNSWEAVER_UNIFI_MAIN_DOMAINS=*.home.example.com
- DNSWEAVER_UNIFI_MAIN_TARGET=192.168.1.100

- DNSWEAVER_UNIFI_BRANCH_TYPE=unifi
- DNSWEAVER_UNIFI_BRANCH_URL=https://192.168.1.1
- DNSWEAVER_UNIFI_BRANCH_API_KEY_FILE=/run/secrets/unifi_api_key
- DNSWEAVER_UNIFI_BRANCH_SITE=branch
- DNSWEAVER_UNIFI_BRANCH_DOMAINS=*.branch.example.com
- DNSWEAVER_UNIFI_BRANCH_TARGET=10.20.0.100
```

## Docker Deployment

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_INSTANCES=unifi
      - DNSWEAVER_UNIFI_TYPE=unifi
      - DNSWEAVER_UNIFI_URL=https://192.168.1.1
      - DNSWEAVER_UNIFI_API_KEY_FILE=/run/secrets/unifi_api_key
      - DNSWEAVER_UNIFI_TLS_SKIP_VERIFY=true
      - DNSWEAVER_UNIFI_MODE=managed
      - DNSWEAVER_UNIFI_RECORD_TYPE=A
      - DNSWEAVER_UNIFI_TARGET=192.168.1.100
      - DNSWEAVER_UNIFI_DOMAINS=*.home.example.com
    secrets:
      - unifi_api_key
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro

secrets:
  unifi_api_key:
    file: ./unifi_api_key.txt
```

## TLS Configuration

UniFi consoles present a self-signed certificate by default. dnsweaver supports the unified TLS surface used by every HTTP provider:

| Env key | Purpose |
|---------|---------|
| `DNSWEAVER_UNIFI_TLS_CA_FILE` | Trust the console's certificate or a private CA bundle (PEM) |
| `DNSWEAVER_UNIFI_TLS_CERT_FILE` / `_TLS_KEY_FILE` | Present a client certificate (mTLS) |
| `DNSWEAVER_UNIFI_TLS_SERVER_NAME` | Override SNI / hostname verification |
| `DNSWEAVER_UNIFI_TLS_SKIP_VERIFY` | Disable verification (logs a warning at startup) |
| `DNSWEAVER_UNIFI_TLS_MIN_VERSION` | `1.2` (default) or `1.3` |

Prefer `TLS_CA_FILE` where you can: export the console certificate once and mount it. `TLS_SKIP_VERIFY=true` is acceptable on a trusted management network. See the [environment reference](../configuration/environment.md) for complete recipes.

!!! warning "Mounted certs must be readable by uid/gid 1000"
    The container drops privileges to the unprivileged `dnsweaver` user, so a
    client key mounted `root:root 0600` yields `permission denied`. See
    [TLS Certificate File Permissions](../configuration/environment.md#tls-certificate-file-permissions).

## API Endpoints Used

For reference, dnsweaver uses these Integration API endpoints (relative to `/proxy/network/integration/v1`):

- `GET /info` — connectivity and authentication check
- `GET /sites` — resolve `SITE` to the site UUID
- `GET /sites/{siteId}/dns/policies` — list records (paged, 200 per page)
- `POST /sites/{siteId}/dns/policies` — create
- `PUT /sites/{siteId}/dns/policies/{id}` — update
- `DELETE /sites/{siteId}/dns/policies/{id}` — delete

## Troubleshooting

### Authentication Failed

Verify the key works with a direct API call:

```bash
curl -k -H "X-API-KEY: your-key" https://192.168.1.1/proxy/network/integration/v1/info
```

A `401` means the key is wrong or was revoked. Keys are created under Settings → Control Plane → Integrations.

### `site "..." not found (available: default, ...)`

`SITE` does not match any site the key can see. Use the `internalReference` shown in the error (it is also the segment right after `/network/` in the console URL), or the site UUID.

### `404` on Every Request

The Integration API is not available at that URL. The most common cause is pointing at a standalone Network Application, which does not have the API (see the warning under Requirements). Consoles older than 10.1 do not expose DNS policies at all. Check that `URL` is the console itself, not a reverse proxy that strips paths.

### Records Not Appearing

1. Check that the domain matches your `DOMAINS` pattern
2. Verify the record exists in the console under Settings → Routing → DNS
3. If using `ZONE` filtering, ensure the domain ends with the zone suffix
4. Check the record is enabled; disabled records are treated as absent

### SRV Record Rejected

UniFi requires SRV names in the form `_service._proto.domain` (for example `_minecraft._tcp.home.example.com`). Names without both underscore-prefixed labels are rejected before any API call.
