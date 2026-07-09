# pfSense

[pfSense](https://www.pfsense.org/) is an open-source firewall and routing platform. dnsweaver manages **host override** records on pfSense's DNS resolvers via a REST API.

pfSense exposes two DNS resolvers: the **DNS Resolver** (Unbound, default) and the **DNS Forwarder** (Dnsmasq). A single dnsweaver `pfsense` provider covers both — pick the resolver with the `ENGINE` setting.

## Requirements

pfSense does **not** ship a REST API in its base system, and Netgate offers no official one. You must install the community [pfSense-pkg-RESTAPI](https://pfrest.org) package (REST API v2) on the firewall first — it is the de facto standard and works on both pfSense CE and pfSense Plus:

- Install it from **System → Package Manager**, then create a key under **System → REST API → Keys**.
- Enable the target resolver (**Services → DNS Resolver**, or **Services → DNS Forwarder**).
- The API key needs permission to manage the target resolver.
- dnsweaver needs network reachability to the pfSense web/GUI port.

## Why REST, not SSH?

pfSense manages state through `config.xml` and its own internal state machine, including HA sync over CARP/XMLRPC. Writing to the underlying resolver's config files over SSH would work briefly, then break the moment HA sync, an upgrade, or a backup/restore rewrites `config.xml`. The REST API is the only supported way to make changes.

If you have a standalone Linux dnsmasq host (not fronted by pfSense), use the [dnsmasq](dnsmasq.md) provider instead.

## Configuration

```yaml
environment:
  - DNSWEAVER_INSTANCES=pf

  - DNSWEAVER_PF_TYPE=pfsense
  - DNSWEAVER_PF_URL=https://pfsense.internal
  - DNSWEAVER_PF_API_KEY_FILE=/run/secrets/pfsense_key
  - DNSWEAVER_PF_ENGINE=unbound            # or dnsmasq
  - DNSWEAVER_PF_ZONE=home.example.com
  - DNSWEAVER_PF_RECORD_TYPE=A
  - DNSWEAVER_PF_TARGET=192.0.2.10
  - DNSWEAVER_PF_DOMAINS=*.home.example.com
```

## Configuration Reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TYPE` | Yes | – | Must be `pfsense` |
| `URL` | Yes | – | pfSense base URL (`https://pfsense.internal`) |
| `API_KEY` | Yes | – | REST API key (sent as the `X-API-Key` header) |
| `API_KEY_FILE` | Alt | – | File-based alternative for Docker/K8s secrets |
| `ENGINE` | No | `unbound` | Target resolver: `unbound` (DNS Resolver) or `dnsmasq` (DNS Forwarder) |
| `ZONE` | No | – | DNS zone for record filtering |
| `TTL` | No | `300` | Informational only — host overrides have no per-record TTL |
| `RECONFIGURE_MODE` | No | `per_write` | `per_write` (apply after every change) or `never` |
| `TLS_CA_FILE` | No | – | Path to a PEM CA bundle (for a private CA on pfSense) |
| `TLS_SKIP_VERIFY` | No | `false` | Bypass certificate verification (**insecure**) |
| `RECORD_TYPE` | Yes | – | `A` or `AAAA` |
| `TARGET` | Yes | – | IP address |
| `DOMAINS` | Yes | – | Glob patterns to match |
| `EXCLUDE_DOMAINS` | No | – | Patterns to exclude |

## Record Types (v1)

Both engines support **A** and **AAAA** host overrides. CNAME and other record types are not implemented in the first release.

## Resolver Differences

The two pfSense resolvers store host overrides differently, and it matters for dual-stack names:

| | DNS Resolver (`unbound`) | DNS Forwarder (`dnsmasq`) |
|---|---|---|
| IPs per host override | Multiple | Exactly one |
| Dual-stack (A + AAAA) for one name | Supported — both IPs live on one override | **Not supported** — each `(host, domain)` must be unique with a single IP |

If you point the `dnsmasq` engine at a name that already has an IP and try to add a second (e.g. an AAAA alongside an existing A), dnsweaver returns a clear error and recommends the `unbound` engine for dual-stack.

## Ownership Marking

pfSense host overrides expose no TXT records, so dnsweaver cannot write its usual `_dnsweaver.*` ownership record.

Instead, every record dnsweaver creates carries a marker in the pfSense **description** field:

```
dnsweaver:{instance-name}
```

Two consequences:

1. **Operator-managed host overrides are always safe.** dnsweaver's `List()` filters to rows that carry its marker, so orphan cleanup can never touch a host override you created by hand in the pfSense GUI. dnsweaver also refuses to modify an existing `(host, domain)` override it doesn't own.
2. **Editing a dnsweaver-managed record's description in the GUI unbinds it from dnsweaver.** If you want to add a note, keep the `dnsweaver:{instance}` prefix and separate your note with ` | ` — the parser tolerates that form.

## Reconfigure Semantics

pfSense requires an explicit "apply" call to reload the resolver after a change.

| Mode | Behavior | When to use |
|------|----------|-------------|
| `per_write` (default) | Apply after every Create/Delete | Small deployments; correctness over throughput |
| `never` | Never auto-apply | Large deployments; you apply externally (cron, orchestration) |

## TLS

pfSense typically presents a self-signed certificate. Prefer one of these approaches (in order of preference):

1. **Trust the pfSense CA properly** — export it from pfSense (System → Cert. Manager → CAs) and point `TLS_CA_FILE` at it.
2. **Bypass verification** — set `TLS_SKIP_VERIFY=true`. dnsweaver will log a WARN at startup. Only acceptable on trusted management networks.

## API Endpoints Used

For reference, dnsweaver uses these pfSense-pkg-RESTAPI v2 endpoints:

**DNS Resolver (Unbound):**
- `GET /api/v2/services/dns_resolver/host_overrides` — list rows
- `POST /api/v2/services/dns_resolver/host_override` — create
- `PATCH /api/v2/services/dns_resolver/host_override` — update (merge/remove an IP)
- `DELETE /api/v2/services/dns_resolver/host_override?id={id}` — delete
- `POST /api/v2/services/dns_resolver/apply` — apply changes

**DNS Forwarder (Dnsmasq):**
- `GET /api/v2/services/dns_forwarder/host_overrides`
- `POST /api/v2/services/dns_forwarder/host_override`
- `PATCH /api/v2/services/dns_forwarder/host_override`
- `DELETE /api/v2/services/dns_forwarder/host_override?id={id}`
- `POST /api/v2/services/dns_forwarder/apply`

## Troubleshooting

**`unauthorized`**
: The API key is wrong, disabled, or lacks permission for the target resolver. Verify under System → REST API → Keys.

**`pfsense reachable but the {engine} host-override endpoint was not found (is the REST API package installed and the {engine} resolver enabled?)`**
: pfSense answered but the resolver's endpoints are missing. Most often this means the REST API package isn't installed, or the target resolver (DNS Resolver / DNS Forwarder) is disabled.

**`the dnsmasq engine (DNS Forwarder) stores one IP per host override … Use the unbound engine for dual-stack`**
: You tried to give one name both an A and an AAAA record on the DNS Forwarder, which pfSense doesn't allow. Switch that instance to `ENGINE=unbound`.

**`refusing to modify host override … it exists on pfsense but is not managed by dnsweaver`**
: A host override for that `(host, domain)` already exists and lacks the `dnsweaver:{instance}` marker. dnsweaver won't overwrite operator-managed rows. Remove or rename the conflicting override, or let dnsweaver own it.

**`pfsense provider requires an IP target`**
: The A/AAAA record's `TARGET` is not a parseable IP. Host overrides on both engines resolve names to IPs, so the target must be `10.1.2.3` or `2001:db8::1`, not another hostname.
