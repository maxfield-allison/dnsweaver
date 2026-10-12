# Caddy Labels

dnsweaver can extract hostnames from Docker containers that use
[caddy-docker-proxy](https://github.com/lucaslorentz/caddy-docker-proxy)
style labels. This lets you keep a single source of truth for your
reverse-proxy configuration _and_ your DNS records.

## Enabling Caddy

Add `caddy` to `DNSWEAVER_SOURCES`:

```yaml
environment:
  - DNSWEAVER_SOURCES=caddy
```

You can combine Caddy with other sources:

```yaml
- DNSWEAVER_SOURCES=traefik,caddy,dnsweaver
```

## Label Format

dnsweaver recognises two label shapes used by caddy-docker-proxy:

### Single hostname

```yaml
labels:
  caddy: app.example.com
```

### Multiple hostnames

Either use indexed labels:

```yaml
labels:
  caddy_0: app.example.com
  caddy_1: www.example.com
```

…or a comma- or space-separated list on a single label:

```yaml
labels:
  caddy: "app.example.com, www.example.com"
```

### Caddy directive labels

Labels that describe Caddy directives (not hostnames) are ignored:

```yaml
labels:
  caddy: app.example.com
  caddy.reverse_proxy: "{{upstreams 8080}}"   # ignored
  caddy.tls: internal                          # ignored
  caddy_0: www.example.com
  caddy_0.reverse_proxy: "{{upstreams 8080}}" # ignored
  caddy_0.tls: internal                       # ignored
```

Only labels named exactly `caddy` or beginning with `caddy_` without a dot
declare hostnames. Dotted directive keys are ignored, including indexed directives.

## Supported Record Types

Caddy labels only declare **hostnames**. dnsweaver pairs each hostname
with the default record type for the matching provider (typically `A`).
To override the record type, target, or TTL on a per-hostname basis,
use [native dnsweaver labels](native-labels.md) alongside Caddy.

## Caveats

- **Caddyfile parsing is not supported.** Only Docker labels are read.
  If your Caddy configuration lives in a `Caddyfile` rather than labels,
  use [native dnsweaver labels](native-labels.md) on the Caddy
  container itself.
- **No reverse-proxy detection.** dnsweaver only extracts hostnames; it
  does not verify that Caddy is actually configured to serve them.
