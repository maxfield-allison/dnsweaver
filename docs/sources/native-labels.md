# Native dnsweaver Labels

While dnsweaver primarily extracts hostnames from Traefik labels, you can also use native dnsweaver labels for services that don't use Traefik.

## Why Use Native Labels?

- Services without a reverse proxy
- Direct DNS records for non-HTTP services
- More explicit control over DNS records
- Services using a different reverse proxy (Nginx, Caddy, etc.)

## Enabling Native Labels

Add `dnsweaver` to the sources:

```yaml
- DNSWEAVER_SOURCES=traefik,dnsweaver
```

Or use only native labels:

```yaml
- DNSWEAVER_SOURCES=dnsweaver
```

## Label Format

### Basic Hostname

```yaml
labels:
  - "dnsweaver.hostname=myapp.example.com"
```

### Multiple Hostnames (Named Records)

For multiple hostnames, use the named records format:

```yaml
labels:
  - "dnsweaver.records.primary.hostname=app1.example.com"
  - "dnsweaver.records.secondary.hostname=app2.example.com"
```

!!! note "Coming Soon"
    A simpler `dnsweaver.hostnames` label for comma-separated lists is planned. See [#96](https://gitlab.bluewillows.net/root/dnsweaver/-/issues/96).

### With Options

```yaml
labels:
  - "dnsweaver.hostname=myapp.example.com"
  - "dnsweaver.ttl=600"  # Override default TTL
  - "dnsweaver.enabled=true"  # Explicit enable (default)
```

### Disable for Specific Container

```yaml
labels:
  - "dnsweaver.enabled=false"  # Skip this container
```

## Label Reference

### Simple Labels

| Label | Default | Description |
|-------|---------|-------------|
| `dnsweaver.hostname` | - | Single hostname to create |
| `dnsweaver.enabled` | `true` | Enable/disable processing |
| `dnsweaver.ttl` | - | Override TTL for this container |

### Named Record Labels

For advanced use cases, use the named record format: `dnsweaver.records.<name>.<field>`

| Label Pattern | Default | Description |
|---------------|---------|-------------|
| `dnsweaver.records.<name>.hostname` | - | Hostname for this record (required) |
| `dnsweaver.records.<name>.type` | `A` | Record type: `A`, `AAAA`, `CNAME`, `SRV`, `TXT` |
| `dnsweaver.records.<name>.target` | - | Override target (IP or hostname) |
| `dnsweaver.records.<name>.provider` | - | Target specific provider instance |
| `dnsweaver.records.<name>.ttl` | - | TTL for this specific record |
| `dnsweaver.records.<name>.port` | - | Port (for SRV records) |
| `dnsweaver.records.<name>.priority` | - | Priority (for SRV records) |
| `dnsweaver.records.<name>.weight` | - | Weight (for SRV records) |
| `dnsweaver.records.<name>.enabled` | `true` | Enable/disable this record |

## Examples

### Database Server

Create A record for a database:

```yaml
services:
  postgres:
    image: postgres:15
    labels:
      - "dnsweaver.hostname=db.internal.example.com"
```

### Multi-Hostname Service

Create records for each service endpoint using named records:

```yaml
services:
  minio:
    image: minio/minio
    labels:
      - "dnsweaver.records.console.hostname=minio.example.com"
      - "dnsweaver.records.api.hostname=s3.example.com"
```

### SRV Record (Minecraft Server)

Create an SRV record for service discovery:

```yaml
services:
  minecraft:
    image: itzg/minecraft-server
    labels:
      - "dnsweaver.records.mc.hostname=_minecraft._tcp.mc.example.com"
      - "dnsweaver.records.mc.type=SRV"
      - "dnsweaver.records.mc.port=25565"
      - "dnsweaver.records.mc.priority=10"
      - "dnsweaver.records.mc.weight=100"
```

### Combine with Traefik Labels

Use both Traefik and native labels:

```yaml
services:
  webapp:
    image: myapp:latest
    labels:
      # Traefik for HTTP routing
      - "traefik.http.routers.webapp.rule=Host(`webapp.example.com`)"

      # Additional DNS record via dnsweaver
      - "dnsweaver.hostname=webapp-direct.example.com"
```

Both hostnames will be processed (if both sources are enabled).

## Docker Compose Example

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_SOURCES=dnsweaver  # Only native labels
      - DNSWEAVER_INSTANCES=internal
      - DNSWEAVER_INTERNAL_TYPE=technitium
      - DNSWEAVER_INTERNAL_URL=http://dns:5380
      - DNSWEAVER_INTERNAL_TOKEN_FILE=/run/secrets/dns_token
      - DNSWEAVER_INTERNAL_ZONE=example.com
      - DNSWEAVER_INTERNAL_RECORD_TYPE=A
      - DNSWEAVER_INTERNAL_TARGET=10.0.0.100
      - DNSWEAVER_INTERNAL_DOMAINS=*.example.com
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro

  my-service:
    image: nginx
    labels:
      - "dnsweaver.hostname=my-service.example.com"
```

## Priority and Conflicts

When a container has both Traefik and dnsweaver labels:

1. Both sources are processed independently
2. Duplicate hostnames are deduplicated
3. No conflict - same hostname from multiple sources creates one record

## Swarm Mode

For Swarm, labels go on the service (same as Traefik):

```yaml
services:
  myapp:
    image: myapp:latest
    labels:  # Service labels (not deploy labels)
      - "dnsweaver.hostname=myapp.example.com"
    deploy:
      replicas: 3
```
