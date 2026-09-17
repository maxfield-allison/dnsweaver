# Multi-Instance Configuration

dnsweaver supports managing DNS records across multiple providers simultaneously. Each **instance** binds a single DNS provider to a specific set of domains and record types.

## How Instances Work

```
DNSWEAVER_INSTANCES=internal-dns,cloudflare,webhook
```

This creates three independent provider instances. Each instance has its own:

- Provider type and credentials
- Domain matching patterns
- Record type and target
- Ownership tracking scope

By default, a service matching multiple instance domain patterns creates records in all matching providers. A workload can select a smaller set of those providers with `dnsweaver.instances`.

## Instance Naming

Instance names become part of environment variable prefixes. The name is uppercased and hyphens become underscores:

| Instance Name | Env Var Prefix | Example Variable |
|---------------|---------------|-------------------|
| `internal-dns` | `DNSWEAVER_INTERNAL_DNS_` | `DNSWEAVER_INTERNAL_DNS_TYPE` |
| `cloudflare` | `DNSWEAVER_CLOUDFLARE_` | `DNSWEAVER_CLOUDFLARE_TOKEN` |
| `home_pihole` | `DNSWEAVER_HOME_PIHOLE_` | `DNSWEAVER_HOME_PIHOLE_URL` |

!!! tip
    Use descriptive names that identify the provider's purpose, not just its type. `internal-dns` is better than `technitium1`.

## Workload Provider Selection

The Docker label `dnsweaver.instances` selects which configured provider instances receive every hostname discovered on that workload, including hostnames from Traefik. Names are case-sensitive and must match the configured instance names.

```yaml
labels:
  - "traefik.http.routers.app.rule=Host(`app.example.com`)"
  - "dnsweaver.instances=internal,external"
```

For Kubernetes, use the annotation:

```yaml
metadata:
  annotations:
    dnsweaver.dev/hostname: app.example.com
    dnsweaver.dev/instances: internal,external
```

The selection is intersected with the operator's domain patterns, exclusions and workload filters. It cannot grant a workload access to a provider outside that scope. `dnsweaver.records.<name>.provider` overrides the workload selection for that named record, while still obeying the provider's scope.

Without a selector, existing all-matching-provider routing is unchanged. Spaces around names are ignored and duplicate names are collapsed. An empty selector, an empty list member, an unknown provider, or contradictory label and annotation sets is invalid. Invalid selections do not fall back to all providers. They suppress removals for that reconciliation cycle; correcting or removing the selector lets reconciliation resume. A valid selection with no in-scope destination also suppresses removals.

Changing `internal,external` to `internal` retires only the exact owned members on the external route. On providers with ownership TXT support, this also works after a restart. Unrelated records remain untouched. If discovery is incomplete or a provider snapshot cannot be read, removals wait. A failed destination write preserves the previous route. If the DNS write succeeds but its ownership marker fails, that destination needs ownership repair or explicit adoption before the previous route can be retired.

These labels are available in the unreleased source. Check the changelog for your image tag before using them.

## Common Patterns

### Split-Horizon DNS

Serve different records for internal and external networks:

```yaml
environment:
  - DNSWEAVER_INSTANCES=internal,external

  # Internal: Technitium for *.home.example.com → LAN IP
  - DNSWEAVER_INTERNAL_TYPE=technitium
  - DNSWEAVER_INTERNAL_URL=http://dns.lan:5380
  - DNSWEAVER_INTERNAL_TOKEN_FILE=/run/secrets/technitium_token
  - DNSWEAVER_INTERNAL_ZONE=home.example.com
  - DNSWEAVER_INTERNAL_RECORD_TYPE=A
  - DNSWEAVER_INTERNAL_TARGET=192.0.2.100
  - DNSWEAVER_INTERNAL_DOMAINS=*.home.example.com

  # External: Cloudflare for *.example.com → CNAME to proxy
  - DNSWEAVER_EXTERNAL_TYPE=cloudflare
  - DNSWEAVER_EXTERNAL_TOKEN_FILE=/run/secrets/cloudflare_token
  - DNSWEAVER_EXTERNAL_ZONE=example.com
  - DNSWEAVER_EXTERNAL_RECORD_TYPE=CNAME
  - DNSWEAVER_EXTERNAL_TARGET=proxy.example.com
  - DNSWEAVER_EXTERNAL_DOMAINS=*.example.com
  - DNSWEAVER_EXTERNAL_EXCLUDE_DOMAINS=*.home.example.com
```

A service with hostname `app.home.example.com` creates:
- A record in Technitium → `192.0.2.100`
- No record in Cloudflare (excluded by `EXCLUDE_DOMAINS`)

A service with hostname `app.example.com` creates:
- No record in Technitium (doesn't match `*.home.example.com`)
- CNAME record in Cloudflare → `proxy.example.com`

### Same Provider, Different Zones

Use two instances of the same provider type for different zones:

```yaml
environment:
  - DNSWEAVER_INSTANCES=zone-a,zone-b

  - DNSWEAVER_ZONE_A_TYPE=technitium
  - DNSWEAVER_ZONE_A_URL=http://dns:5380
  - DNSWEAVER_ZONE_A_TOKEN_FILE=/run/secrets/tech_token
  - DNSWEAVER_ZONE_A_ZONE=alpha.example.com
  - DNSWEAVER_ZONE_A_RECORD_TYPE=A
  - DNSWEAVER_ZONE_A_TARGET=198.51.100.100
  - DNSWEAVER_ZONE_A_DOMAINS=*.alpha.example.com

  - DNSWEAVER_ZONE_B_TYPE=technitium
  - DNSWEAVER_ZONE_B_URL=http://dns:5380
  - DNSWEAVER_ZONE_B_TOKEN_FILE=/run/secrets/tech_token
  - DNSWEAVER_ZONE_B_ZONE=beta.example.com
  - DNSWEAVER_ZONE_B_RECORD_TYPE=A
  - DNSWEAVER_ZONE_B_TARGET=10.0.2.100
  - DNSWEAVER_ZONE_B_DOMAINS=*.beta.example.com
```

### Webhook Notifications

Add a webhook instance alongside real providers:

```yaml
environment:
  - DNSWEAVER_INSTANCES=dns,notify

  - DNSWEAVER_DNS_TYPE=technitium
  - DNSWEAVER_DNS_DOMAINS=*.example.com
  # ... provider config ...

  - DNSWEAVER_NOTIFY_TYPE=webhook
  - DNSWEAVER_NOTIFY_URL=https://hooks.example.com/dns-changes
  - DNSWEAVER_NOTIFY_DOMAINS=*.example.com
  - DNSWEAVER_NOTIFY_RECORD_TYPE=A
  - DNSWEAVER_NOTIFY_TARGET=0.0.0.0
```

## Multi-Instance Coordination

When running **multiple copies of dnsweaver** (not multiple instances within one copy), use distinct `DNSWEAVER_INSTANCE_ID` values and non-conflicting record scopes. This separates ownership namespaces; it does not provide a distributed lock, leader election or active-active coordination:

```yaml
# dnsweaver copy 1 (manages internal DNS)
environment:
  - DNSWEAVER_INSTANCE_ID=internal-mgr
  - DNSWEAVER_INSTANCES=technitium
  # ...

# dnsweaver copy 2 (manages external DNS)
environment:
  - DNSWEAVER_INSTANCE_ID=external-mgr
  - DNSWEAVER_INSTANCES=cloudflare
  # ...
```

Each copy creates ownership TXT records that include its instance ID:

```
_dnsweaver.app.example.com  TXT  "heritage=dnsweaver,instance=internal-mgr"
```

This prevents one copy's orphan cleanup from deleting records managed by the other copy.

!!! warning
    Without `DNSWEAVER_INSTANCE_ID`, all copies share the same ownership namespace. Running multiple copies without distinct instance IDs may cause records to be incorrectly deleted as orphans.

### Providers Without TXT Support

Providers without ownership TXT support cannot recover proof of exact-member ownership from DNS after a restart. A running process can retain evidence of records it created or explicitly adopted and use that evidence for later changes and route cleanup. Matching the configured target alone does not grant authority over a pre-existing record.

A restarted process leaves unowned records untouched unless adoption is explicitly enabled. `DNSWEAVER_INSTANCE_ID` cannot create durable ownership on these providers. Avoid overlapping scopes between independent dnsweaver processes, and review [Operational Modes](modes.md) before enabling adoption.

## Domain Overlap

When multiple instances match the same hostname, **all matching instances create records unless a workload selector narrows them**. This is by design — it enables split-horizon, multi-provider redundancy, and webhook notification patterns.

To prevent overlap, use `EXCLUDE_DOMAINS`:

```yaml
# Instance A: *.example.com EXCEPT *.internal.example.com
- DNSWEAVER_A_DOMAINS=*.example.com
- DNSWEAVER_A_EXCLUDE_DOMAINS=*.internal.example.com

# Instance B: *.internal.example.com only
- DNSWEAVER_B_DOMAINS=*.internal.example.com
```

## Debugging Multi-Instance

Enable debug logging to see per-instance reconciliation:

```yaml
- DNSWEAVER_LOG_LEVEL=debug
- DNSWEAVER_DRY_RUN=true
```

Dry-run mode shows what each instance **would** do without making changes. Look for log entries with the `provider` field to identify which instance is acting.

## See Also

- [Environment Variables Reference](environment.md) — Complete variable reference
- [Domain Matching](domains.md) — Glob and regex patterns
- [Operational Modes](modes.md) — managed, authoritative, additive
- [Docker Secrets](secrets.md) — Secure credential management
