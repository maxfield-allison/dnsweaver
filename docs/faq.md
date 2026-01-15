# Frequently Asked Questions

## General

### What's the difference between dnsweaver and external-dns?

external-dns is primarily designed for Kubernetes and cloud DNS providers. dnsweaver is purpose-built for Docker and Docker Swarm with:

- First-class Docker Swarm support
- Self-hosted DNS provider focus (Technitium, Pi-hole, dnsmasq)
- Multi-provider for split-horizon DNS
- Simpler configuration via environment variables

### Do I need to run dnsweaver on every Docker host?

No. dnsweaver connects to the Docker socket (or socket proxy) and watches events cluster-wide in Swarm mode. Run a single instance on a manager node.

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

### How do I use different record types for different subdomains?

Create multiple provider instances with different configurations:

```yaml
- DNSWEAVER_INSTANCES=cname-provider,a-provider

- DNSWEAVER_CNAME_PROVIDER_RECORD_TYPE=CNAME
- DNSWEAVER_CNAME_PROVIDER_DOMAINS=*.external.example.com

- DNSWEAVER_A_PROVIDER_RECORD_TYPE=A
- DNSWEAVER_A_PROVIDER_DOMAINS=*.internal.example.com
```

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

## Operations

### Why do I see duplicate records?

Possible causes:

1. **Multiple dnsweaver instances**: Only run one replica
2. **Multiple providers matching**: Check domain patterns for unintended overlap
3. **Ownership tracking disabled**: Records might be created without tracking

### How often does dnsweaver check for changes?

- **Docker events**: Real-time via event stream
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

### "Provider authentication failed"

Verify credentials:
- Token/password is correct
- Token file path is accessible
- Token has required permissions

### "TLS certificate verification failed"

For self-signed certificates:

```yaml
- DNSWEAVER_<PROVIDER>_TLS_SKIP_VERIFY=true
```

Or add the CA certificate to dnsweaver's trust store.

### Records created but not resolving

1. Check DNS propagation time (TTL)
2. Verify record in provider's web interface
3. Test with direct query: `dig @dns-server hostname`
4. Check for zone/domain mismatch

## Feature Requests

### Will dnsweaver support Kubernetes?

dnsweaver is focused on Docker/Swarm. For Kubernetes, consider:
- external-dns (cloud providers)
- ExternalDNS with custom webhooks
- dnsweaver webhook provider for custom integration

### Will you add support for [DNS Provider X]?

Check existing issues on GitHub. If not requested, open a feature request. The webhook provider can be used as a workaround for unsupported providers.

### Can dnsweaver do load balancing / round-robin?

dnsweaver creates single records per hostname. For load balancing, use:
- Your reverse proxy (Traefik, Nginx)
- DNS provider's native round-robin (if supported)
- Multiple A records (requires custom provider implementation)
