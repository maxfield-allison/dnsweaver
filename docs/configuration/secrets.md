# Docker Secrets

dnsweaver supports Docker secrets for secure credential management. Any environment variable can use the `_FILE` suffix to read its value from a file.

## How It Works

Instead of passing a secret directly:

```yaml
environment:
  - DNSWEAVER_INTERNAL_DNS_TOKEN=my-secret-token  # ❌ Exposed in environment
```

Use the `_FILE` suffix to read from a secrets file:

```yaml
environment:
  - DNSWEAVER_INTERNAL_DNS_TOKEN_FILE=/run/secrets/dns_token  # ✅ Secure
secrets:
  - dns_token
```

## Docker Compose Example

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_INSTANCES=internal-dns,cloudflare

      # Technitium with secret
      - DNSWEAVER_INTERNAL_DNS_TYPE=technitium
      - DNSWEAVER_INTERNAL_DNS_URL=http://dns:5380
      - DNSWEAVER_INTERNAL_DNS_TOKEN_FILE=/run/secrets/technitium_token
      - DNSWEAVER_INTERNAL_DNS_ZONE=home.example.com
      - DNSWEAVER_INTERNAL_DNS_RECORD_TYPE=A
      - DNSWEAVER_INTERNAL_DNS_TARGET=10.0.0.100
      - DNSWEAVER_INTERNAL_DNS_DOMAINS=*.home.example.com

      # Cloudflare with secret
      - DNSWEAVER_CLOUDFLARE_TYPE=cloudflare
      - DNSWEAVER_CLOUDFLARE_TOKEN_FILE=/run/secrets/cloudflare_token
      - DNSWEAVER_CLOUDFLARE_ZONE=example.com
      - DNSWEAVER_CLOUDFLARE_RECORD_TYPE=CNAME
      - DNSWEAVER_CLOUDFLARE_TARGET=proxy.example.com
      - DNSWEAVER_CLOUDFLARE_DOMAINS=*.example.com
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    secrets:
      - technitium_token
      - cloudflare_token

secrets:
  technitium_token:
    external: true
  cloudflare_token:
    external: true
```

## Docker Swarm Example

In Swarm mode, create secrets with `docker secret create`:

```bash
# Create secrets
echo "your-technitium-token" | docker secret create technitium_token -
echo "your-cloudflare-token" | docker secret create cloudflare_token -
```

Then reference them in your stack file exactly as shown above.

## Supported Variables

Any environment variable that accepts sensitive data supports the `_FILE` suffix:

| Variable | File Suffix |
|----------|-------------|
| `DNSWEAVER_{NAME}_TOKEN` | `DNSWEAVER_{NAME}_TOKEN_FILE` |
| `DNSWEAVER_{NAME}_PASSWORD` | `DNSWEAVER_{NAME}_PASSWORD_FILE` |
| `DNSWEAVER_{NAME}_AUTH_TOKEN` | `DNSWEAVER_{NAME}_AUTH_TOKEN_FILE` |

## Secret File Format

Secret files should contain only the secret value, with optional trailing newline:

```
my-secret-token-value
```

!!! warning
    Do not include variable names, quotes, or other formatting in secret files. Just the raw value.
