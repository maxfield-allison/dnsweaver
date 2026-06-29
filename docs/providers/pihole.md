---
title: Pi-hole DNS Provider
description: Automatically create and delete Pi-hole local DNS records from Docker containers and Kubernetes with dnsweaver — Pi-hole v6 API or file mode.
---

# Pi-hole

Pi-hole is a network-wide ad blocker that also serves as a local DNS server. dnsweaver supports Pi-hole through its API or by direct file manipulation.

## Requirements

- Pi-hole v5.0+ for API mode
- Pi-hole with accessible `/etc/pihole/` for file mode

## API Mode (Recommended)

For Pi-hole with API access:

```yaml
environment:
  - DNSWEAVER_INSTANCES=pihole

  - DNSWEAVER_PIHOLE_TYPE=pihole
  - DNSWEAVER_PIHOLE_ACCESS_MODE=api
  - DNSWEAVER_PIHOLE_URL=http://pihole:80
  - DNSWEAVER_PIHOLE_PASSWORD_FILE=/run/secrets/pihole_password
  - DNSWEAVER_PIHOLE_RECORD_TYPE=A
  - DNSWEAVER_PIHOLE_TARGET=192.0.2.100
  - DNSWEAVER_PIHOLE_DOMAINS=*.home.example.com
```

## File Mode

For direct file access (when dnsweaver can mount Pi-hole's config directory):

```yaml
environment:
  - DNSWEAVER_INSTANCES=pihole

  - DNSWEAVER_PIHOLE_TYPE=pihole
  - DNSWEAVER_PIHOLE_ACCESS_MODE=file
  - DNSWEAVER_PIHOLE_CONFIG_DIR=/etc/pihole
  - DNSWEAVER_PIHOLE_RECORD_TYPE=A
  - DNSWEAVER_PIHOLE_TARGET=192.0.2.100
  - DNSWEAVER_PIHOLE_DOMAINS=*.home.example.com
volumes:
  - /path/to/pihole/etc:/etc/pihole
```

## Configuration Reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TYPE` | Yes | - | Must be `pihole` |
| `ACCESS_MODE` | No | `api` | `api` or `file` |

!!! note "Renamed from MODE"
    This field was renamed from `MODE` to `ACCESS_MODE` in v0.10 to avoid collision with
    the per-instance operational `MODE` field (managed/authoritative/additive). The old name
    still works but will emit a deprecation warning.
| `URL` | API mode | - | Pi-hole web interface URL |
| `PASSWORD` | API mode | - | Web interface password |
| `PASSWORD_FILE` | API alt | - | Path to password file |
| `API_VERSION` | No | `auto` | Pi-hole API version: `v5`, `v6`, or `auto` |
| `CONFIG_DIR` | File mode | `/etc/pihole` | Path to Pi-hole config directory |
| `CONFIG_FILE` | File mode | `custom.list` | Filename for managed records |
| `RELOAD_COMMAND` | File mode | `pihole restartdns reload-lists` | Command to reload after file changes |
| `ZONE` | No | - | DNS zone for record filtering |
| `TTL` | No | `300` | Record TTL in seconds |
| `RECORD_TYPE` | Yes | - | `A` or `CNAME` |
| `TARGET` | Yes | - | Record value |
| `DOMAINS` | Yes | - | Glob patterns to match |
| `EXCLUDE_DOMAINS` | No | - | Patterns to exclude |

## Ownership and Managed Mode

!!! info "Target-Based Ownership Inference"
    Pi-hole does not support TXT records, so dnsweaver cannot create ownership TXT markers
    (`_dnsweaver.*`) for this provider. In **managed mode**, dnsweaver uses **target-based
    ownership inference** instead: if a record's type and target match the provider instance's
    configured `RECORD_TYPE` and `TARGET`, it is inferred as owned and cleaned up when the
    source workload disappears. Records with different targets are preserved.

    See [Operational Modes](../configuration/modes.md) for details.

## Record Types

### A Records (Local DNS)

Pi-hole stores local DNS entries in `/etc/pihole/custom.list`:

```yaml
- DNSWEAVER_PIHOLE_RECORD_TYPE=A
- DNSWEAVER_PIHOLE_TARGET=192.0.2.100
```

### CNAME Records

CNAME records require Pi-hole's FTL CNAME feature:

```yaml
- DNSWEAVER_PIHOLE_RECORD_TYPE=CNAME
- DNSWEAVER_PIHOLE_TARGET=proxy.example.com
```

## Getting the API Password

The Pi-hole API uses your web interface password. To set or retrieve it:

```bash
# Set a new password
pihole -a -p newpassword

# Or use the existing password from setup
```

## Docker Deployment Considerations

When running dnsweaver alongside Pi-hole in Docker:

### Same Docker Host

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_PIHOLE_URL=http://pihole:80
    networks:
      - pihole_network

  pihole:
    image: pihole/pihole:latest
    networks:
      - pihole_network

networks:
  pihole_network:
```

### Remote Pi-hole

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_PIHOLE_URL=http://192.168.1.100:80
```

## File Mode Details

In file mode, dnsweaver manages these files:

- `custom.list` - A records
- `05-pihole-custom-cname.conf` - CNAME records

!!! warning
    File mode requires dnsweaver to have write access to Pi-hole's config directory. Changes may require restarting Pi-hole's FTL service.

## TLS Configuration (API Mode)

When using API mode against a Pi-hole instance served over HTTPS — typically through a reverse proxy with a private CA — dnsweaver supports the unified TLS surface:

| Env key | Purpose |
|---------|---------|
| `DNSWEAVER_PIHOLE_TLS_CA_FILE` | Trust a private CA bundle (PEM) |
| `DNSWEAVER_PIHOLE_TLS_CERT_FILE` / `_TLS_KEY_FILE` | Present a client certificate (mTLS) |
| `DNSWEAVER_PIHOLE_TLS_SERVER_NAME` | Override SNI / hostname verification |
| `DNSWEAVER_PIHOLE_TLS_SKIP_VERIFY` | Disable verification (development only) |
| `DNSWEAVER_PIHOLE_TLS_MIN_VERSION` | `1.2` (default) or `1.3` |

The legacy `DNSWEAVER_PIHOLE_INSECURE_SKIP_VERIFY` variable still works but emits a deprecation warning and will be removed in v2.0. File mode does not perform HTTP requests so these keys have no effect there.

!!! warning "Mounted certs must be readable by uid/gid 1000"
    The container drops privileges to the unprivileged `dnsweaver` user, so a
    client key mounted `root:root 0600` yields `permission denied`. See
    [TLS Certificate File Permissions](../configuration/environment.md#tls-certificate-file-permissions).


## Troubleshooting

### API Authentication Failed

Check your password:

```bash
curl -X POST "http://pihole/admin/api.php?auth=$(echo -n 'password' | sha256sum | cut -d' ' -f1)"
```

### Records Not Resolving

After file mode changes, restart FTL:

```bash
pihole restartdns
```

### Permission Denied (File Mode)

Ensure the dnsweaver container runs as a user that can write to Pi-hole's config directory.
