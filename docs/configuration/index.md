---
title: Configuration
description: Complete guide to configuring dnsweaver
icon: material/cog
---

# Configuration

dnsweaver is configured entirely through environment variables, making it ideal for containerized deployments. This section covers all configuration options.

## Configuration Methods

<div class="grid cards" markdown>

-   :material-application-variable:{ .lg .middle } **Environment Variables**

    ---

    The primary configuration method. All settings are controlled via `DNSWEAVER_*` environment variables.

    [:octicons-arrow-right-24: Environment Reference](environment.md)

-   :material-domain:{ .lg .middle } **Domain Matching**

    ---

    Configure which hostnames route to which DNS providers using glob patterns.

    [:octicons-arrow-right-24: Domain Patterns](domains.md)

-   :material-key:{ .lg .middle } **Docker Secrets**

    ---

    Secure API tokens and credentials using Docker secrets or file-based configuration.

    [:octicons-arrow-right-24: Secrets Management](secrets.md)

-   :material-shield-sync:{ .lg .middle } **Operational Modes**

    ---

    Control how aggressively dnsweaver manages records with managed, authoritative, and additive modes.

    [:octicons-arrow-right-24: Operational Modes](modes.md)

</div>

## Quick Reference

The most important environment variables to configure:

| Variable | Description | Required |
| :------- | :---------- | :------: |
| `DNSWEAVER_INSTANCES` | Comma-separated list of provider instance names | :material-check: |
| `DNSWEAVER_<INSTANCE>_TYPE` | Provider type (`technitium`, `cloudflare`, etc.) | :material-check: |
| `DNSWEAVER_<INSTANCE>_DOMAINS` | Domain patterns this instance handles | :material-check: |
| `DNSWEAVER_<INSTANCE>_TARGET` | Target IP or hostname for DNS records | :material-check: |
| `DNSWEAVER_<INSTANCE>_ZONE` | DNS zone name | Provider-specific |

!!! tip "Provider-specific variables"
    Each provider type has additional required and optional variables. See the [Providers](../providers/index.md) section for details.

## Configuration Validation

dnsweaver validates configuration at startup. If required variables are missing or invalid, it will log an error and exit. Run with `DNSWEAVER_LOG_LEVEL=debug` to see detailed configuration parsing.
