---
title: DNS Providers
description: Configure dnsweaver to manage records across multiple DNS providers
icon: material/dns
---

# DNS Providers

dnsweaver supports multiple DNS providers, each with different capabilities and configuration options.

## Supported Providers

<div class="grid cards" markdown>

-   :simple-cloudflare:{ .lg .middle } **Cloudflare**

    ---

    Public DNS with CDN and proxy capabilities. REST API.

    [:octicons-arrow-right-24: Configuration](cloudflare.md)

-   :material-dns:{ .lg .middle } **Technitium**

    ---

    Self-hosted, full-featured DNS server. REST API.

    [:octicons-arrow-right-24: Configuration](technitium.md)

-   :material-raspberry-pi:{ .lg .middle } **Pi-hole**

    ---

    Integrate with existing Pi-hole setups. API or file mode.

    [:octicons-arrow-right-24: Configuration](pihole.md)

-   :material-file-cog:{ .lg .middle } **dnsmasq**

    ---

    Simple file-based DNS configuration.

    [:octicons-arrow-right-24: Configuration](dnsmasq.md)

-   :material-webhook:{ .lg .middle } **Webhook**

    ---

    Custom integrations via HTTP callbacks.

    [:octicons-arrow-right-24: Configuration](webhook.md)

-   :material-update:{ .lg .middle } **RFC 2136**

    ---

    Industry-standard Dynamic DNS protocol. BIND, Windows DNS, PowerDNS, etc.

    [:octicons-arrow-right-24: Configuration](rfc2136.md)

</div>

## Provider Comparison

| Provider | API Type | Record Types | Best For |
| :------- | :------- | :----------- | :------- |
| [Technitium](technitium.md) | REST API | A, AAAA, CNAME, SRV, TXT | Self-hosted, full-featured DNS |
| [Cloudflare](cloudflare.md) | REST API | A, AAAA, CNAME, TXT | Public DNS with CDN/proxy |
| [RFC 2136](rfc2136.md) | DNS Protocol | A, AAAA, CNAME, SRV, TXT | BIND, Windows DNS, PowerDNS, Knot |
| [Pi-hole](pihole.md) | REST API or File | A, AAAA, CNAME | Existing Pi-hole setups |
| [dnsmasq](dnsmasq.md) | File | A, AAAA, CNAME | Simple file-based DNS |
| [Webhook](webhook.md) | HTTP Callback | Any | Custom integrations |

## Multi-Provider Architecture

dnsweaver is designed to manage **multiple providers simultaneously**. This enables:

- **Split-horizon DNS**: Same hostname resolves differently internally vs externally
- **Redundancy**: Records in multiple DNS providers for failover
- **Migration**: Gradual transition between DNS providers

### Example: Split-Horizon Setup

```bash
DNSWEAVER_INSTANCES=internal,external

# Internal: Technitium for LAN resolution
DNSWEAVER_INTERNAL_TYPE=technitium
DNSWEAVER_INTERNAL_RECORD_TYPE=A
DNSWEAVER_INTERNAL_TARGET=10.0.0.100
DNSWEAVER_INTERNAL_DOMAINS=*.example.com

# External: Cloudflare for public resolution
DNSWEAVER_EXTERNAL_TYPE=cloudflare
DNSWEAVER_EXTERNAL_RECORD_TYPE=CNAME
DNSWEAVER_EXTERNAL_TARGET=proxy.example.com
DNSWEAVER_EXTERNAL_DOMAINS=*.example.com
```

With this configuration, when a container with label `Host(`app.example.com`)` starts:

1. Technitium gets: `app.example.com → A → 10.0.0.100`
2. Cloudflare gets: `app.example.com → CNAME → proxy.example.com`

## Provider Selection

When a hostname is extracted from a container, dnsweaver checks it against each provider's domain patterns:

```
Container: app.home.example.com

Provider A (domains: *.home.example.com) → ✅ Match → Create record
Provider B (domains: *.example.com)      → ✅ Match → Create record
Provider C (domains: *.other.com)        → ❌ No match → Skip
```

Use `EXCLUDE_DOMAINS` to prevent overlapping matches when needed.

## Common Settings

All providers share these configuration options:

| Variable | Required | Description |
|----------|----------|-------------|
| `TYPE` | Yes | Provider type |
| `RECORD_TYPE` | Yes | `A`, `AAAA`, `CNAME`, or `SRV` |
| `TARGET` | Yes | Record value (IP or hostname) |
| `DOMAINS` | Yes | Glob patterns to match |
| `EXCLUDE_DOMAINS` | No | Patterns to exclude |
| `TTL` | No | Record TTL in seconds |

## Ownership Tracking

By default, dnsweaver creates TXT records alongside DNS records to track ownership:

```
app.example.com         A      10.0.0.100
_dnsweaver.app.example.com  TXT    "owner=dnsweaver,source=docker,id=abc123"
```

This prevents dnsweaver from modifying records it didn't create. Disable with:

```bash
DNSWEAVER_OWNERSHIP_TRACKING=false
```

To adopt existing records (and take ownership):

```bash
DNSWEAVER_ADOPT_EXISTING=true
```
