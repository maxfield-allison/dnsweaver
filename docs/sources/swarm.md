# Docker Swarm

Docker Swarm mode provides service discovery across a cluster. dnsweaver integrates with Swarm to manage DNS for services.

## How Swarm Mode Differs

| Aspect | Standalone Docker | Docker Swarm |
|--------|-------------------|--------------|
| Labels on | Containers | Services |
| Watch events | Container start/stop | Service create/update/remove |
| Replicas | 1 container | Multiple tasks |
| DNS target | Container IP or gateway | Service VIP or ingress |

## Enabling Swarm Mode

dnsweaver auto-detects Swarm mode, but you can force it:

```yaml
environment:
  - DNSWEAVER_DOCKER_MODE=swarm
```

## Swarm Service Labels

Labels must be on the **service definition**, not deploy labels:

```yaml
services:
  myapp:
    image: myapp:latest
    labels:  # ✅ Service labels (top-level)
      - "traefik.http.routers.myapp.rule=Host(`app.example.com`)"
    deploy:
      labels:  # ❌ Deploy labels (not read by dnsweaver)
        - "some.deploy.label=value"
```

## Target Configuration

For Swarm, `TARGET` typically points to your reverse proxy or Swarm ingress:

### VIP Mode (Recommended)

Point to the Swarm VIP for your reverse proxy:

```yaml
- DNSWEAVER_INTERNAL_TARGET=10.0.0.100  # Traefik service VIP
```

### Ingress Mode

Point to the Swarm ingress network gateway:

```yaml
- DNSWEAVER_INTERNAL_TARGET=10.0.0.1  # Swarm ingress gateway
```

### CNAME to Proxy

Point to the proxy's DNS name:

```yaml
- DNSWEAVER_INTERNAL_RECORD_TYPE=CNAME
- DNSWEAVER_INTERNAL_TARGET=traefik.example.com
```

## Deployment Example

Complete Swarm stack with dnsweaver:

```yaml
version: "3.8"

services:
  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_INSTANCES=internal
      - DNSWEAVER_INTERNAL_TYPE=technitium
      - DNSWEAVER_INTERNAL_URL=http://dns:5380
      - DNSWEAVER_INTERNAL_TOKEN_FILE=/run/secrets/dns_token
      - DNSWEAVER_INTERNAL_ZONE=home.example.com
      - DNSWEAVER_INTERNAL_RECORD_TYPE=A
      - DNSWEAVER_INTERNAL_TARGET=10.0.0.100
      - DNSWEAVER_INTERNAL_DOMAINS=*.home.example.com
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    deploy:
      mode: replicated
      replicas: 1
      placement:
        constraints:
          - node.role == manager
    secrets:
      - dns_token

secrets:
  dns_token:
    external: true
```

!!! important
    dnsweaver must run on a **manager node** to access the Swarm API.

## Manager Node Constraint

dnsweaver needs access to the Swarm manager API. Always include:

```yaml
deploy:
  placement:
    constraints:
      - node.role == manager
```

## High Availability

For HA, run dnsweaver with a single replica:

```yaml
deploy:
  mode: replicated
  replicas: 1  # Only one instance should manage DNS
  update_config:
    parallelism: 1
    delay: 10s
  restart_policy:
    condition: any
```

Running multiple instances could cause duplicate record creation or deletion race conditions.

## Service Updates

When a Swarm service is updated:

1. dnsweaver detects the update event
2. Compares old vs new labels
3. Removes records for deleted hostnames
4. Creates records for new hostnames
5. Updates records if target changes

## Troubleshooting

### Labels Not Detected

Verify labels are on the service (not deploy):

```bash
docker service inspect myapp --format '{{json .Spec.Labels}}'
```

### Socket Connection Failed

Ensure dnsweaver is on a manager node:

```bash
docker node ls  # Run from manager
```

### Records Not Creating

Check dnsweaver logs:

```bash
docker service logs dnsweaver 2>&1 | grep -i "myapp"
```
