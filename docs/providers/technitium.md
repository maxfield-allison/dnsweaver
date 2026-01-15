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
  - DNSWEAVER_TECHNITIUM_TARGET=10.0.0.100
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
| `INSECURE_SKIP_VERIFY` | No | `false` | Skip TLS certificate verification |

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
- DNSWEAVER_TECHNITIUM_TARGET=10.0.0.100
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
- DNSWEAVER_TECHNITIUM_TARGET=10.0.0.100
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
  - DNSWEAVER_INTERNAL_TARGET=10.0.0.100
  - DNSWEAVER_INTERNAL_DOMAINS=*.internal.example.com

  # DMZ zone
  - DNSWEAVER_DMZ_TYPE=technitium
  - DNSWEAVER_DMZ_URL=http://dns-server:5380
  - DNSWEAVER_DMZ_TOKEN_FILE=/run/secrets/technitium_token
  - DNSWEAVER_DMZ_ZONE=dmz.example.com
  - DNSWEAVER_DMZ_RECORD_TYPE=A
  - DNSWEAVER_DMZ_TARGET=10.1.0.100
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

For self-signed certificates, either:

1. Add the CA to dnsweaver's trust store
2. Use `INSECURE_SKIP_VERIFY=true` (not recommended for production)

```yaml
- DNSWEAVER_TECHNITIUM_INSECURE_SKIP_VERIFY=true
```
