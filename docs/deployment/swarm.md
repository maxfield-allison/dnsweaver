# Docker Swarm Deployment

Production deployment for Docker Swarm clusters.

## Basic Stack

```yaml
version: "3.8"

services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
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
    networks:
      - internal
    deploy:
      mode: replicated
      replicas: 1
      placement:
        constraints:
          - node.role == manager
      restart_policy:
        condition: any
        delay: 5s
        max_attempts: 3
      update_config:
        parallelism: 1
        delay: 10s
    secrets:
      - dns_token

secrets:
  dns_token:
    external: true

networks:
  internal:
    external: true
```

## Creating Secrets

Before deploying, create the required secrets:

```bash
# Create from file
docker secret create dns_token ./secrets/dns_token.txt

# Or create from stdin
echo "your-api-token" | docker secret create dns_token -

# Verify
docker secret ls
```

## Production Stack with Socket Proxy

Recommended secure setup:

```yaml
version: "3.8"

services:
  socket-proxy:
    image: tecnativa/docker-socket-proxy
    environment:
      - CONTAINERS=1
      - SERVICES=1
      - TASKS=1
      - NETWORKS=1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    networks:
      - socket-proxy
    deploy:
      mode: global
      placement:
        constraints:
          - node.role == manager
      resources:
        limits:
          cpus: '0.25'
          memory: 64M

  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_DOCKER_HOST=tcp://socket-proxy:2375
      - DNSWEAVER_LOG_LEVEL=info
      - DNSWEAVER_RECONCILE_INTERVAL=60s

      - DNSWEAVER_INSTANCES=internal
      - DNSWEAVER_INTERNAL_TYPE=technitium
      - DNSWEAVER_INTERNAL_URL=http://dns-server:5380
      - DNSWEAVER_INTERNAL_TOKEN_FILE=/run/secrets/dns_token
      - DNSWEAVER_INTERNAL_ZONE=home.example.com
      - DNSWEAVER_INTERNAL_RECORD_TYPE=A
      - DNSWEAVER_INTERNAL_TARGET=10.0.0.100
      - DNSWEAVER_INTERNAL_DOMAINS=*.home.example.com
    networks:
      - socket-proxy
      - internal
    deploy:
      mode: replicated
      replicas: 1
      placement:
        constraints:
          - node.role == manager
      restart_policy:
        condition: any
      update_config:
        parallelism: 1
        order: start-first
      resources:
        limits:
          cpus: '0.5'
          memory: 128M
        reservations:
          cpus: '0.1'
          memory: 64M
    secrets:
      - dns_token
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s

secrets:
  dns_token:
    external: true

networks:
  socket-proxy:
    driver: overlay
    attachable: false
  internal:
    external: true
```

## Deployment Commands

```bash
# Deploy the stack
docker stack deploy -c docker-stack-dnsweaver.yml dnsweaver

# Check service status
docker service ls | grep dnsweaver

# View logs
docker service logs dnsweaver_dnsweaver --follow

# Update the service
docker service update --image maxamill/dnsweaver:latest dnsweaver_dnsweaver

# Remove the stack
docker stack rm dnsweaver
```

## Multi-Provider Swarm Stack

For split-horizon DNS in Swarm:

```yaml
version: "3.8"

services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_DOCKER_HOST=tcp://socket-proxy:2375
      - DNSWEAVER_INSTANCES=internal,external

      # Internal DNS
      - DNSWEAVER_INTERNAL_TYPE=technitium
      - DNSWEAVER_INTERNAL_URL=http://dns:5380
      - DNSWEAVER_INTERNAL_TOKEN_FILE=/run/secrets/technitium_token
      - DNSWEAVER_INTERNAL_ZONE=example.com
      - DNSWEAVER_INTERNAL_RECORD_TYPE=A
      - DNSWEAVER_INTERNAL_TARGET=10.0.0.100
      - DNSWEAVER_INTERNAL_DOMAINS=*.example.com

      # External DNS
      - DNSWEAVER_EXTERNAL_TYPE=cloudflare
      - DNSWEAVER_EXTERNAL_TOKEN_FILE=/run/secrets/cloudflare_token
      - DNSWEAVER_EXTERNAL_ZONE=example.com
      - DNSWEAVER_EXTERNAL_RECORD_TYPE=CNAME
      - DNSWEAVER_EXTERNAL_TARGET=tunnel.example.com
      - DNSWEAVER_EXTERNAL_DOMAINS=*.example.com
      - DNSWEAVER_EXTERNAL_EXCLUDE_DOMAINS=*.internal.example.com
    deploy:
      mode: replicated
      replicas: 1
      placement:
        constraints:
          - node.role == manager
    secrets:
      - technitium_token
      - cloudflare_token

secrets:
  technitium_token:
    external: true
  cloudflare_token:
    external: true
```

## High Availability Considerations

dnsweaver should run as a **single replica** to avoid race conditions:

- Multiple instances might create duplicate records
- Cleanup could race between instances
- Use `replicas: 1` with restart policies for HA

The Swarm scheduler will automatically reschedule if the node fails.

## Monitoring Integration

Expose metrics for Prometheus:

```yaml
services:
  dnsweaver:
    # ... other config ...
    ports:
      - target: 8080
        published: 8080
        mode: host
    deploy:
      labels:
        - "prometheus.scrape=true"
        - "prometheus.port=8080"
        - "prometheus.path=/metrics"
```
