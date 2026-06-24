---
title: Docker Swarm Source
description: Automate DNS records for Docker Swarm services with dnsweaver — cluster-wide service discovery with multi-instance-safe ownership tracking.
---

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
- DNSWEAVER_INTERNAL_TARGET=192.0.2.100  # Traefik service VIP
```

### Ingress Mode

Point to the Swarm ingress network gateway:

```yaml
- DNSWEAVER_INTERNAL_TARGET=192.0.2.1  # Swarm ingress gateway
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
      - DNSWEAVER_INTERNAL_TARGET=192.0.2.100
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

## Per-Entrypoint Routing

When the same Traefik router is bound to multiple `entrypoints`, dnsweaver
emits one extraction per `(host, entrypoint)` pair. Combined with the
per-instance `DNSWEAVER_{NAME}_ENTRYPOINTS` filter, this lets you point
each Traefik entrypoint at a different DNS target — useful for
split-horizon LAN/VPN setups, or any scenario where one router needs to
resolve to different IPs depending on which listener it was published on.

### Example: Split LAN / VPN Targets

Two entrypoints (`webA`, `webB`) on the same Traefik router need to
resolve `web.example.com` to different IPs:

```yaml
services:
  reverse-proxy:
    image: traefik:v3
    command:
      - "--entrypoints.webA.address=:80"
      - "--entrypoints.webB.address=:8080"

  myapp:
    image: myapp:latest
    labels:
      - "traefik.http.routers.myapp.rule=Host(`web.example.com`)"
      - "traefik.http.routers.myapp.entrypoints=webA,webB"

  dnsweaver:
    image: maxamill/dnsweaver:latest
    environment:
      - DNSWEAVER_INSTANCES=lan,vpn
      # LAN instance answers only routers bound to webA -> 10.0.0.10
      - DNSWEAVER_LAN_TYPE=technitium
      - DNSWEAVER_LAN_TARGET=10.0.0.10
      - DNSWEAVER_LAN_DOMAINS=*.example.com
      - DNSWEAVER_LAN_ENTRYPOINTS=webA
      # VPN instance answers only routers bound to webB -> 10.99.0.10
      - DNSWEAVER_VPN_TYPE=technitium
      - DNSWEAVER_VPN_TARGET=10.99.0.10
      - DNSWEAVER_VPN_DOMAINS=*.example.com
      - DNSWEAVER_VPN_ENTRYPOINTS=webB
```

### Matching Semantics

- `DNSWEAVER_{NAME}_ENTRYPOINTS` accepts a comma-separated allowlist
  (`webA,webB`). Whitespace is trimmed; empty values are ignored.
- A router with **no** `entrypoints` label produces a wildcard extraction
  that matches every instance regardless of `ENTRYPOINTS` filter
  (preserves pre-1.4 behavior).
- A router with `entrypoints=webA,webB` produces two distinct
  extractions; each instance only receives the ones whose entrypoint is
  in its allowlist.
- The same `(host, entrypoint)` pair declared in multiple
  containers/files is deduplicated.

### Traefik `asDefault` Entrypoints

Traefik supports flagging entrypoints as
[`asDefault = true`](https://doc.traefik.io/traefik/reference/install-configuration/entrypoints/#opt-asdefault).
When set, routers without an explicit `entryPoints` declaration bind
**only** to the `asDefault` entrypoints — not to all entrypoints.

dnsweaver cannot read Traefik's static config, so the wildcard behavior
above will silently over-publish records for unlabeled routers in this
case. If you use `asDefault`, declare the same defaults to dnsweaver via
the source-level setting:

```yaml
environment:
  - DNSWEAVER_SOURCE_TRAEFIK_DEFAULT_ENTRYPOINTS=webA,webC
```

With this set, an unlabeled router fans out one extraction per default
entrypoint — exactly mirroring what Traefik itself does — and per-instance
`ENTRYPOINTS` filters then claim each pair as usual. Routers with explicit
`entrypoints` labels are unaffected.

Unset (default) preserves pre-1.4.2 wildcard behavior.

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
