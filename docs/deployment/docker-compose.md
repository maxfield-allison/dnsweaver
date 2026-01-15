# Docker Compose Deployment

Basic deployment with Docker Compose for standalone Docker hosts.

## Minimal Example

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    container_name: dnsweaver
    restart: unless-stopped
    environment:
      - DNSWEAVER_INSTANCES=internal
      - DNSWEAVER_INTERNAL_TYPE=technitium
      - DNSWEAVER_INTERNAL_URL=http://dns-server:5380
      - DNSWEAVER_INTERNAL_TOKEN_FILE=/run/secrets/dns_token
      - DNSWEAVER_INTERNAL_ZONE=home.example.com
      - DNSWEAVER_INTERNAL_RECORD_TYPE=A
      - DNSWEAVER_INTERNAL_TARGET=10.0.0.100
      - DNSWEAVER_INTERNAL_DOMAINS=*.home.example.com
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    secrets:
      - dns_token

secrets:
  dns_token:
    file: ./secrets/dns_token.txt
```

## With Socket Proxy

More secure setup using a Docker socket proxy:

```yaml
services:
  socket-proxy:
    image: tecnativa/docker-socket-proxy
    container_name: socket-proxy
    restart: unless-stopped
    environment:
      - CONTAINERS=1
      - SERVICES=1
      - TASKS=1
      - NETWORKS=1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    networks:
      - socket-proxy

  dnsweaver:
    image: maxamill/dnsweaver:latest
    container_name: dnsweaver
    restart: unless-stopped
    environment:
      - DNSWEAVER_DOCKER_HOST=tcp://socket-proxy:2375
      - DNSWEAVER_INSTANCES=internal
      - DNSWEAVER_INTERNAL_TYPE=technitium
      - DNSWEAVER_INTERNAL_URL=http://dns-server:5380
      - DNSWEAVER_INTERNAL_TOKEN_FILE=/run/secrets/dns_token
      - DNSWEAVER_INTERNAL_ZONE=home.example.com
      - DNSWEAVER_INTERNAL_RECORD_TYPE=A
      - DNSWEAVER_INTERNAL_TARGET=10.0.0.100
      - DNSWEAVER_INTERNAL_DOMAINS=*.home.example.com
    depends_on:
      - socket-proxy
    networks:
      - socket-proxy
    secrets:
      - dns_token

secrets:
  dns_token:
    file: ./secrets/dns_token.txt

networks:
  socket-proxy:
    driver: bridge
```

## Multi-Provider Example

Managing internal and external DNS:

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    container_name: dnsweaver
    restart: unless-stopped
    environment:
      # Global settings
      - DNSWEAVER_LOG_LEVEL=info
      - DNSWEAVER_RECONCILE_INTERVAL=60s

      # Provider instances
      - DNSWEAVER_INSTANCES=internal,external

      # Internal DNS (Technitium)
      - DNSWEAVER_INTERNAL_TYPE=technitium
      - DNSWEAVER_INTERNAL_URL=http://dns-server:5380
      - DNSWEAVER_INTERNAL_TOKEN_FILE=/run/secrets/technitium_token
      - DNSWEAVER_INTERNAL_ZONE=home.example.com
      - DNSWEAVER_INTERNAL_RECORD_TYPE=A
      - DNSWEAVER_INTERNAL_TARGET=10.0.0.100
      - DNSWEAVER_INTERNAL_DOMAINS=*.home.example.com,*.example.com

      # External DNS (Cloudflare)
      - DNSWEAVER_EXTERNAL_TYPE=cloudflare
      - DNSWEAVER_EXTERNAL_TOKEN_FILE=/run/secrets/cloudflare_token
      - DNSWEAVER_EXTERNAL_ZONE=example.com
      - DNSWEAVER_EXTERNAL_RECORD_TYPE=CNAME
      - DNSWEAVER_EXTERNAL_TARGET=tunnel.example.com
      - DNSWEAVER_EXTERNAL_DOMAINS=*.example.com
      - DNSWEAVER_EXTERNAL_EXCLUDE_DOMAINS=*.home.example.com
      - DNSWEAVER_EXTERNAL_PROXIED=true
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    secrets:
      - technitium_token
      - cloudflare_token

secrets:
  technitium_token:
    file: ./secrets/technitium_token.txt
  cloudflare_token:
    file: ./secrets/cloudflare_token.txt
```

## With Traefik

Common setup alongside Traefik reverse proxy:

```yaml
services:
  traefik:
    image: traefik:v3.0
    container_name: traefik
    command:
      - "--providers.docker=true"
      - "--entrypoints.web.address=:80"
    ports:
      - "80:80"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    labels:
      - "traefik.http.routers.traefik.rule=Host(`traefik.home.example.com`)"

  dnsweaver:
    image: maxamill/dnsweaver:latest
    container_name: dnsweaver
    environment:
      - DNSWEAVER_INSTANCES=internal
      - DNSWEAVER_INTERNAL_TYPE=technitium
      - DNSWEAVER_INTERNAL_URL=http://dns-server:5380
      - DNSWEAVER_INTERNAL_TOKEN_FILE=/run/secrets/dns_token
      - DNSWEAVER_INTERNAL_ZONE=home.example.com
      - DNSWEAVER_INTERNAL_RECORD_TYPE=A
      - DNSWEAVER_INTERNAL_TARGET=10.0.0.100  # Traefik's IP
      - DNSWEAVER_INTERNAL_DOMAINS=*.home.example.com
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    secrets:
      - dns_token

  whoami:
    image: traefik/whoami
    labels:
      - "traefik.http.routers.whoami.rule=Host(`whoami.home.example.com`)"
    # dnsweaver will create: whoami.home.example.com → 10.0.0.100

secrets:
  dns_token:
    file: ./secrets/dns_token.txt
```

## Health Checks

Add health check configuration:

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s
```

## Resource Limits

For production deployments:

```yaml
services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    deploy:
      resources:
        limits:
          cpus: '0.5'
          memory: 128M
        reservations:
          cpus: '0.1'
          memory: 64M
```
