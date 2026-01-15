# Getting Started

This guide walks you through installing dnsweaver and setting up your first DNS provider.

## Prerequisites

- Docker (standalone or Swarm mode)
- A supported DNS provider with API access
- Container labels using Traefik-style `Host()` rules (or [native dnsweaver labels](sources/native-labels.md))

## Installation

### Docker Hub

```bash
docker pull maxamill/dnsweaver:latest
```

### GitHub Container Registry

```bash
docker pull ghcr.io/maxfield-allison/dnsweaver:latest
```

### Supported Architectures

- `linux/amd64`
- `linux/arm64`

## Basic Configuration

dnsweaver uses environment variables for all configuration. The key concepts:

1. **Instances** - Named configurations that connect to DNS providers
2. **Domain patterns** - Which hostnames each instance manages
3. **Record types** - What DNS records to create (A, AAAA, CNAME)

### Minimal Example

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    restart: unless-stopped
    environment:
      # Define your instance name
      - DNSWEAVER_INSTANCES=my-dns

      # Configure the instance
      - DNSWEAVER_MY_DNS_TYPE=technitium
      - DNSWEAVER_MY_DNS_URL=http://dns-server:5380
      - DNSWEAVER_MY_DNS_TOKEN=your-api-token
      - DNSWEAVER_MY_DNS_ZONE=example.com
      - DNSWEAVER_MY_DNS_RECORD_TYPE=A
      - DNSWEAVER_MY_DNS_TARGET=10.0.0.100
      - DNSWEAVER_MY_DNS_DOMAINS=*.example.com
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
```

### How Instance Names Work

Instance names are arbitrary identifiers you choose. They become environment variable prefixes:

| Instance Name | Environment Variable Prefix |
|---------------|----------------------------|
| `internal-dns` | `DNSWEAVER_INTERNAL_DNS_*` |
| `cloudflare` | `DNSWEAVER_CLOUDFLARE_*` |
| `my-dns` | `DNSWEAVER_MY_DNS_*` |

!!! note
    Dashes (`-`) in instance names become underscores (`_`) in environment variables.

## Using Docker Secrets

For production deployments, use Docker secrets instead of plain environment variables:

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_INSTANCES=internal-dns
      - DNSWEAVER_INTERNAL_DNS_TYPE=technitium
      - DNSWEAVER_INTERNAL_DNS_URL=http://dns-server:5380
      - DNSWEAVER_INTERNAL_DNS_TOKEN_FILE=/run/secrets/dns_token  # Note: _FILE suffix
      - DNSWEAVER_INTERNAL_DNS_ZONE=example.com
      - DNSWEAVER_INTERNAL_DNS_RECORD_TYPE=A
      - DNSWEAVER_INTERNAL_DNS_TARGET=10.0.0.100
      - DNSWEAVER_INTERNAL_DNS_DOMAINS=*.example.com
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    secrets:
      - dns_token

secrets:
  dns_token:
    external: true
```

See [Docker Secrets](configuration/secrets.md) for more details.

## Verify It's Working

1. **Check logs:**
   ```bash
   docker logs dnsweaver
   ```

2. **Check health endpoint:**
   ```bash
   curl http://localhost:8080/health
   ```

3. **View metrics:**
   ```bash
   curl http://localhost:8080/metrics
   ```

4. **Start a container with Traefik labels:**
   ```bash
   docker run -d \
     --label "traefik.http.routers.test.rule=Host(\`test.example.com\`)" \
     nginx
   ```

5. **Verify the DNS record was created in your provider**

## Next Steps

- **[Environment Variables](configuration/environment.md)** - Complete configuration reference
- **[Domain Matching](configuration/domains.md)** - Wildcards, regex, and exclusions
- **[Provider Setup](providers/index.md)** - Detailed provider configuration
- **[Split-Horizon DNS](deployment/split-horizon.md)** - Internal + external records
