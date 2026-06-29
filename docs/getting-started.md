---
title: Getting Started
description: Install dnsweaver and set up automatic DNS for Docker, Kubernetes, or Proxmox in minutes — first provider configuration and your first auto-created record.
---

# Getting Started

This guide walks you through installing dnsweaver and setting up your first DNS provider on Docker or Kubernetes.

## Prerequisites

=== "Docker"

    - Docker (standalone or Swarm mode)
    - A supported DNS provider with API access
    - Container labels using Traefik-style `Host()` rules (or [native dnsweaver labels](sources/native-labels.md))

=== "Kubernetes"

    - Kubernetes 1.25+ cluster
    - Helm 3+ or kubectl with Kustomize
    - A supported DNS provider with API access
    - Ingress, IngressRoute (Traefik CRD), HTTPRoute (Gateway API), or annotated Service resources

## Installation

=== "Docker"

    ### Docker Hub

    ```bash
    docker pull maxamill/dnsweaver:latest
    ```

    ### GitHub Container Registry

    ```bash
    docker pull ghcr.io/maxfield-allison/dnsweaver:latest
    ```

=== "Kubernetes (Helm)"

    ```bash
    helm install dnsweaver oci://ghcr.io/maxfield-allison/charts/dnsweaver \
      --namespace dnsweaver --create-namespace
    ```

    Or from the repository:

    ```bash
    git clone https://github.com/maxfield-allison/dnsweaver.git
    helm install dnsweaver deploy/helm/dnsweaver/ \
      --namespace dnsweaver --create-namespace
    ```

=== "Kubernetes (Kustomize)"

    ```bash
    kubectl apply -k https://github.com/maxfield-allison/dnsweaver/deploy/kustomize/base
    ```

### Supported Architectures

- `linux/amd64`
- `linux/arm64`

## Basic Configuration

dnsweaver is configured via environment variables (Docker) or a combination of environment variables, ConfigMaps, and Secrets (Kubernetes). The key concepts:

1. **Instances** - Named configurations that connect to DNS providers
2. **Domain patterns** - Which hostnames each instance manages
3. **Record types** - What DNS records to create (A, AAAA, CNAME)

### Minimal Example

=== "Docker Compose"

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
          - DNSWEAVER_MY_DNS_TARGET=192.0.2.100
          - DNSWEAVER_MY_DNS_DOMAINS=*.example.com
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock:ro
    ```

=== "Kubernetes (Helm values)"

    ```yaml
    # values.yaml
    config:
      instances: "my-dns"
      providers:
        my-dns:
          type: technitium
          url: http://dns-server.dns.svc:5380
          zone: example.com
          recordType: A
          target: "192.0.2.100"
          domains: "*.example.com"

    secrets:
      technitium:
        token: "your-api-token"  # Or reference an existing Secret
    ```

=== "Kubernetes (raw manifest)"

    ```yaml
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: dnsweaver
      namespace: dnsweaver
    spec:
      replicas: 1
      selector:
        matchLabels:
          app: dnsweaver
      template:
        metadata:
          labels:
            app: dnsweaver
        spec:
          serviceAccountName: dnsweaver
          containers:
            - name: dnsweaver
              image: maxamill/dnsweaver:latest
              env:
                - name: DNSWEAVER_INSTANCES
                  value: "my-dns"
                - name: DNSWEAVER_MY_DNS_TYPE
                  value: "technitium"
                - name: DNSWEAVER_MY_DNS_URL
                  value: "http://dns-server.dns.svc:5380"
                - name: DNSWEAVER_MY_DNS_ZONE
                  value: "example.com"
                - name: DNSWEAVER_MY_DNS_RECORD_TYPE
                  value: "A"
                - name: DNSWEAVER_MY_DNS_TARGET
                  value: "192.0.2.100"
                - name: DNSWEAVER_MY_DNS_DOMAINS
                  value: "*.example.com"
                - name: DNSWEAVER_MY_DNS_TOKEN
                  valueFrom:
                    secretKeyRef:
                      name: dnsweaver-credentials
                      key: technitium-token
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

## Using Secrets

For production deployments, avoid passing credentials as plain environment variables.

=== "Docker Secrets"

    Use the `_FILE` suffix to read credentials from Docker secrets:

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
          - DNSWEAVER_INTERNAL_DNS_TARGET=192.0.2.100
          - DNSWEAVER_INTERNAL_DNS_DOMAINS=*.example.com
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock:ro
        secrets:
          - dns_token

    secrets:
      dns_token:
        external: true
    ```

=== "Kubernetes Secrets"

    Reference Kubernetes Secrets via `secretKeyRef`:

    ```yaml
    env:
      - name: DNSWEAVER_INTERNAL_DNS_TOKEN
        valueFrom:
          secretKeyRef:
            name: dnsweaver-credentials
            key: technitium-token
    ```

    Or mount as a file and use the `_FILE` suffix:

    ```yaml
    env:
      - name: DNSWEAVER_INTERNAL_DNS_TOKEN_FILE
        value: /etc/dnsweaver/secrets/technitium-token
    volumeMounts:
      - name: credentials
        mountPath: /etc/dnsweaver/secrets
        readOnly: true
    ```

See [Secrets Management](configuration/secrets.md) for more details.

## Verify It's Working

=== "Docker"

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

=== "Kubernetes"

    1. **Check pod status:**
       ```bash
       kubectl get pods -n dnsweaver
       ```

    2. **Check logs:**
       ```bash
       kubectl logs -n dnsweaver deploy/dnsweaver --tail=50
       ```

    3. **Check health endpoint:**
       ```bash
       kubectl port-forward -n dnsweaver svc/dnsweaver 8080:8080
       curl http://localhost:8080/health
       ```

    4. **Create a test Ingress:**
       ```yaml
       apiVersion: networking.k8s.io/v1
       kind: Ingress
       metadata:
         name: test-dns
       spec:
         rules:
           - host: test.example.com
             http:
               paths:
                 - path: /
                   pathType: Prefix
                   backend:
                     service:
                       name: nginx
                       port:
                         number: 80
       ```

    5. **Verify the DNS record was created in your provider**

## Next Steps

- **[Environment Variables](configuration/environment.md)** — Complete configuration reference
- **[Domain Matching](configuration/domains.md)** — Wildcards, regex, and exclusions
- **[Provider Setup](providers/index.md)** — Detailed provider configuration
- **[Kubernetes Deployment](deployment/kubernetes.md)** — Full Kubernetes guide with Helm, Kustomize, and RBAC
- **[Docker Swarm Deployment](deployment/swarm.md)** — Swarm-specific deployment patterns
- **[Split-Horizon DNS](deployment/split-horizon.md)** — Internal + external records
