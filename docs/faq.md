---
title: FAQ
description: Frequently asked questions about dnsweaver — how it compares to external-dns, supported providers and platforms, ownership tracking, and troubleshooting.
---

# Frequently Asked Questions

## General

### What's the difference between dnsweaver and external-dns?

external-dns is primarily designed for Kubernetes and cloud DNS providers. dnsweaver supports Docker, Kubernetes, and Proxmox VE with:

- First-class Docker Swarm, Kubernetes, and Proxmox VE support
- Self-hosted DNS provider focus (Technitium, Pi-hole, AdGuard Home, dnsmasq)
- Multi-provider for split-horizon DNS
- Platform-agnostic design — run on any combination of Docker, Kubernetes, and Proxmox VE simultaneously
- Simpler configuration via environment variables or YAML

### Do I need to run dnsweaver on every Docker host?

No. dnsweaver connects to the Docker socket (or socket proxy) and watches events cluster-wide in Swarm mode. Run a single instance on a manager node.

### How does dnsweaver work in Kubernetes?

dnsweaver watches Kubernetes resources (Ingress, IngressRoute, HTTPRoute, Service) for hostname information and creates DNS records automatically. It uses a ServiceAccount with RBAC permissions — no Docker socket needed. Deploy as a single-replica Deployment via Helm or Kustomize.

### Can I run dnsweaver on both Docker and Kubernetes at the same time?

Yes! Set `DNSWEAVER_PLATFORM=both` to watch Docker events and Kubernetes resources simultaneously. This is useful for mixed environments or gradual migrations between platforms.

### Can I run dnsweaver without Docker or Kubernetes?

Yes. Set `DNSWEAVER_PLATFORM=none` (alias `standalone`) to run dnsweaver as a bare binary on a host, VM, or LXC with no container runtime. No Docker or Kubernetes client is created, so it won't fail with `Cannot connect to the Docker daemon`. You must configure at least one non-container source — a Proxmox VE source (`DNSWEAVER_PROXMOX_URL`), an Incus source (`DNSWEAVER_INCUS_URL`/`DNSWEAVER_INCUS_SOCKET_PATH`), or a file-discovery source (e.g. `DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS`). See [Standalone](configuration/environment.md#standalone-no-docker-or-kubernetes) for a full example.

### Can dnsweaver manage existing DNS records?

By default, dnsweaver only manages records it creates (tracked via ownership TXT records). To adopt existing records:

```yaml
- DNSWEAVER_ADOPT_EXISTING=true
```

!!! warning
    This will modify existing records. Test with `DRY_RUN=true` first.

## Configuration

### Why aren't my container labels being detected?

Common causes:

1. **Swarm mode**: Labels must be on the service, not deploy labels
2. **Label format**: Check Traefik Host rule syntax
3. **Domain patterns**: Hostname might not match your `DOMAINS` patterns

Enable debug logging to see what's happening:
```yaml
- DNSWEAVER_LOG_LEVEL=debug
```

### Why aren't my Kubernetes resources being detected?

Common causes:

1. **RBAC**: The ServiceAccount needs `get`, `list`, `watch` on the resource types you're using (Ingress, IngressRoute, HTTPRoute, Service)
2. **Namespace filtering**: If `DNSWEAVER_KUBE_NAMESPACES` is set, only those namespaces are watched
3. **Resource type**: dnsweaver watches Ingress, IngressRoute (Traefik CRD), HTTPRoute (Gateway API), and Service resources — verify yours is a supported type
4. **Hostname extraction**: The hostname must be in a recognized field (e.g., `.spec.rules[].host` for Ingress)

Enable debug logging:
```yaml
- DNSWEAVER_LOG_LEVEL=debug
```

### How do I use different record types for different subdomains?

Create multiple provider instances with different configurations:

```yaml
- DNSWEAVER_INSTANCES=cname-provider,a-provider

- DNSWEAVER_CNAME_PROVIDER_RECORD_TYPE=CNAME
- DNSWEAVER_CNAME_PROVIDER_DOMAINS=*.external.example.com

- DNSWEAVER_A_PROVIDER_RECORD_TYPE=A
- DNSWEAVER_A_PROVIDER_DOMAINS=*.internal.example.com
```

### How do I set up dual-stack DNS (A + AAAA)?

Configure two provider instances with the same domain patterns but different record types and targets:

```yaml
- DNSWEAVER_INSTANCES=dns-v4,dns-v6

- DNSWEAVER_DNS_V4_RECORD_TYPE=A
- DNSWEAVER_DNS_V4_TARGET=192.0.2.100
- DNSWEAVER_DNS_V4_DOMAINS=*.example.com

- DNSWEAVER_DNS_V6_RECORD_TYPE=AAAA
- DNSWEAVER_DNS_V6_TARGET=fd00::100
- DNSWEAVER_DNS_V6_DOMAINS=*.example.com
```

Both instances can point to the same DNS server — dnsweaver treats each independently. See the [dual-stack deployment guide](deployment/dual-stack.md) for full examples.

### Can I use regex for domain matching?

Yes, use `DOMAINS_REGEX` instead of `DOMAINS`:

```yaml
- DNSWEAVER_INTERNAL_DOMAINS_REGEX=^[a-z0-9-]+\.example\.com$
```

### How do I exclude specific hostnames?

Use `EXCLUDE_DOMAINS`:

```yaml
- DNSWEAVER_INTERNAL_DOMAINS=*.example.com
- DNSWEAVER_INTERNAL_EXCLUDE_DOMAINS=admin.example.com,secret.example.com
```

### What configuration options are available for Kubernetes?

In addition to the standard provider configuration, Kubernetes-specific options include:

| Variable | Description | Default |
|----------|-------------|---------|
| `DNSWEAVER_PLATFORM` | Platform to watch: `docker`, `kubernetes`, `both`, or `none` | `docker` |
| `DNSWEAVER_KUBE_NAMESPACES` | Comma-separated list of namespaces to watch | All namespaces |
| `DNSWEAVER_KUBE_LABEL_SELECTOR` | Label selector to filter watched resources | None |

See the [Kubernetes source docs](sources/kubernetes.md) for the full list.

## Operations

### Why do I see duplicate records?

Possible causes:

1. **Multiple dnsweaver instances**: Only run one replica
2. **Multiple providers matching**: Check domain patterns for unintended overlap
3. **Ownership tracking disabled**: Records might be created without tracking

### How often does dnsweaver check for changes?

- **Docker events**: Real-time via event stream
- **Kubernetes events**: Real-time via watch API
- **Reconciliation**: Periodic (default 60s) to catch any missed events
- **File sources**: Configurable poll interval

### What happens if a DNS provider is unavailable?

dnsweaver will:
1. Log the error
2. Continue processing other providers
3. Retry on next reconciliation cycle

Records in unavailable providers won't be updated until connectivity is restored.

### How do I clean up orphaned records?

Orphaned records (records without corresponding containers) are cleaned up automatically if:

```yaml
- DNSWEAVER_CLEANUP_ORPHANS=true  # Default
```

For manual cleanup, you'll need to delete records directly from the DNS provider.

### Can I preview changes without applying them?

Yes, use dry-run mode:

```yaml
- DNSWEAVER_DRY_RUN=true
```

Changes are logged but not applied to DNS providers.

## Troubleshooting

### Firefox or Chrome fails to connect to internal services (ECH errors)

Modern browsers use ECH (Encrypted Client Hello) when HTTPS records exist in public DNS. If your internal DNS zone lacks matching HTTPS records, browsers may fail with connection errors or experience delays.

**Solution:** dnsweaver's Technitium provider automatically creates companion HTTPS records by default. If you've disabled this, re-enable it:

```yaml
- DNSWEAVER_TECHNITIUM_AUTO_HTTPS_RECORDS=true
```

See [Technitium — Companion HTTPS Records](providers/technitium.md#companion-https-records) for details.

### "No matching providers for hostname"

The extracted hostname doesn't match any provider's domain patterns. Check:

1. Provider `DOMAINS` patterns include the hostname
2. Provider `EXCLUDE_DOMAINS` doesn't exclude it
3. Hostname is fully qualified

### "Failed to connect to Docker"

Check Docker socket access:

```bash
# Verify socket exists
ls -la /var/run/docker.sock

# Check permissions
docker exec dnsweaver ls -la /var/run/docker.sock
```

!!! tip
    If running on Kubernetes only, set `DNSWEAVER_PLATFORM=kubernetes` to skip the Docker connection entirely. If running with no container runtime at all (e.g. a bare host or LXC using only the Proxmox or a file-discovery source), set `DNSWEAVER_PLATFORM=none`.

### "Failed to create Kubernetes watcher"

Check RBAC permissions:

```bash
# Verify the ServiceAccount has the correct ClusterRole
kubectl auth can-i list ingresses --as=system:serviceaccount:dnsweaver:dnsweaver
```

See the [Kubernetes deployment guide](deployment/kubernetes.md) for the required RBAC configuration.

### "Provider authentication failed"

Verify credentials:
- Token/password is correct
- Token file path is accessible
- Token has required permissions

### "TLS certificate verification failed"

For servers using a private/internal CA, supply the CA bundle so the chain
validates normally:

```yaml
- DNSWEAVER_{INSTANCE}_TLS_CA_FILE=/run/secrets/internal_ca.pem
```

For mutual-TLS (server requires a client certificate):

```yaml
- DNSWEAVER_{INSTANCE}_TLS_CA_FILE=/run/secrets/internal_ca.pem
- DNSWEAVER_{INSTANCE}_TLS_CERT_FILE=/run/secrets/dnsweaver.crt
- DNSWEAVER_{INSTANCE}_TLS_KEY_FILE=/run/secrets/dnsweaver.key
```

If you connect by IP but the certificate is issued for a hostname, override
the SNI / verification name:

```yaml
- DNSWEAVER_{INSTANCE}_TLS_SERVER_NAME=dns.internal.example.com
```

As a last resort for self-signed certificates that cannot be supplied as a CA
bundle, you can disable verification entirely — this removes MITM protection
and is **not recommended for production**:

```yaml
- DNSWEAVER_{INSTANCE}_TLS_SKIP_VERIFY=true
```

The legacy `INSECURE_SKIP_VERIFY` variable still works but emits a deprecation
warning and will be removed in a future major release.

### Records created but not resolving

1. Check DNS propagation time (TTL)
2. Verify record in provider's web interface
3. Test with direct query: `dig @dns-server hostname`
4. Check for zone/domain mismatch

## Feature Requests

### Will you add support for [DNS Provider X]?

Check existing issues on GitHub. If not requested, open a feature request. The webhook provider can be used as a workaround for unsupported providers.

### Can dnsweaver do load balancing / round-robin?

dnsweaver creates single records per hostname. For load balancing, use:
- Your reverse proxy (Traefik, Nginx)
- DNS provider's native round-robin (if supported)
- Multiple A records (requires custom provider implementation)
