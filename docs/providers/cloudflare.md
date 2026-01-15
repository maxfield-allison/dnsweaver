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
| `ZONE` | Yes | - | DNS zone (domain name) |
| `RECORD_TYPE` | Yes | - | `A`, `AAAA`, `CNAME`, or `TXT` |
| `TARGET` | Yes | - | Record value |
| `DOMAINS` | Yes | - | Glob patterns to match |
| `EXCLUDE_DOMAINS` | No | - | Patterns to exclude |
| `TTL` | No | `1` | TTL in seconds (1 = auto) |
| `PROXIED` | No | `false` | Enable Cloudflare proxy |

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
  - DNSWEAVER_INTERNAL_TARGET=10.0.0.100
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
