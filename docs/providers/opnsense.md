# OPNsense

[OPNsense](https://opnsense.org/) is an open-source firewall and routing platform. dnsweaver manages **host override** records on OPNsense's DNS resolvers via its REST API.

OPNsense ships with two DNS resolvers: **Unbound** (default) and **Dnsmasq** (24.7+). A single dnsweaver `opnsense` provider covers both — pick the resolver with the `ENGINE` setting.

## Requirements

- OPNsense with the target resolver enabled (Services → Unbound DNS, or Services → Dnsmasq DNS)
- An API key + secret for a user with permission to manage the target resolver (System → Access → Users → *user* → **API keys**)
- Network reachability from dnsweaver to the OPNsense web/GUI port

## Why REST, not SSH?

OPNsense manages state through `config.xml` and its own internal state machine. Writing to the underlying resolver's config files over SSH would work briefly, then break the moment HA sync, a firmware upgrade, or a backup/restore rewrites `config.xml`. The REST API is the only supported way to make changes.

If you have a standalone Linux dnsmasq host (not fronted by OPNsense), use the [dnsmasq](dnsmasq.md) provider instead.

## Configuration

```yaml
environment:
  - DNSWEAVER_INSTANCES=opns

  - DNSWEAVER_OPNS_TYPE=opnsense
  - DNSWEAVER_OPNS_URL=https://opnsense.internal
  - DNSWEAVER_OPNS_API_KEY_FILE=/run/secrets/opnsense_key
  - DNSWEAVER_OPNS_API_SECRET_FILE=/run/secrets/opnsense_secret
  - DNSWEAVER_OPNS_ENGINE=unbound        # or dnsmasq
  - DNSWEAVER_OPNS_ZONE=home.example.com
  - DNSWEAVER_OPNS_RECORD_TYPE=A
  - DNSWEAVER_OPNS_TARGET=192.0.2.10
  - DNSWEAVER_OPNS_DOMAINS=*.home.example.com
```

## Configuration Reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TYPE` | Yes | – | Must be `opnsense` |
| `URL` | Yes | – | OPNsense base URL (`https://opnsense.internal`) |
| `API_KEY` | Yes | – | OPNsense API key (per-user) |
| `API_SECRET` | Yes | – | OPNsense API secret (paired with `API_KEY`) |
| `API_KEY_FILE` / `API_SECRET_FILE` | Alt | – | File-based alternatives for Docker/K8s secrets |
| `ENGINE` | No | `unbound` | Target resolver: `unbound` or `dnsmasq` |
| `ZONE` | No | – | DNS zone for record filtering |
| `TTL` | No | `300` | Informational only — host overrides have no per-record TTL |
| `RECONFIGURE_MODE` | No | `per_write` | `per_write` (reload after every change) or `never` |
| `TLS_CA_FILE` | No | – | Path to a PEM CA bundle (for private CA on OPNsense) |
| `TLS_SKIP_VERIFY` | No | `false` | Bypass certificate verification (**insecure**) |
| `RECORD_TYPE` | Yes | – | `A` or `AAAA` |
| `TARGET` | Yes | – | IP address |
| `DOMAINS` | Yes | – | Glob patterns to match |
| `EXCLUDE_DOMAINS` | No | – | Patterns to exclude |

## Record Types (v1)

Both engines support **A** and **AAAA** host overrides. CNAME and other record types are not implemented in the first release.

## Ownership Marking

Neither Unbound nor Dnsmasq host overrides expose TXT records, so dnsweaver cannot write its usual `_dnsweaver.*` ownership record.

Instead, every record dnsweaver creates carries a marker in the OPNsense **description** field:

```
dnsweaver:{instance-name}
```

Two consequences:

1. **Operator-managed host overrides are always safe.** dnsweaver's `List()` filters to rows that carry its marker, so orphan cleanup can never touch a host override you created by hand in the OPNsense GUI.
2. **Editing a dnsweaver-managed record's description in the GUI unbinds it from dnsweaver.** If you want to add a note, keep the `dnsweaver:{instance}` prefix and separate your note with ` | ` — the parser tolerates that form.

## Reconfigure Semantics

OPNsense requires an explicit "reconfigure" call to reload the resolver after a change. That call fully restarts the daemon, so it's not free.

| Mode | Behavior | When to use |
|------|----------|-------------|
| `per_write` (default) | Reconfigure after every Create/Delete | Small deployments; correctness over throughput |
| `never` | Never auto-reconfigure | Large deployments; you reload externally (cron, orchestration) |

Batched/debounced reconfigure is a planned follow-up.

## TLS

OPNsense typically presents a self-signed certificate. Prefer one of these approaches (in order of preference):

1. **Trust the OPNsense CA properly** — export it from OPNsense (System → Trust → Authorities) and point `TLS_CA_FILE` at it.
2. **Bypass verification** — set `TLS_SKIP_VERIFY=true`. dnsweaver will log a WARN at startup. Only acceptable on trusted management networks.

## API Endpoints Used

For reference (all POST):

**Unbound:**
- `/api/unbound/settings/searchHostOverride` — list rows
- `/api/unbound/settings/addHostOverride` — create
- `/api/unbound/settings/delHostOverride/{uuid}` — delete
- `/api/unbound/service/reconfigure` — reload resolver

**Dnsmasq (OPNsense 24.7+):**
- `/api/dnsmasq/settings/searchHost`
- `/api/dnsmasq/settings/addHost`
- `/api/dnsmasq/settings/delHost/{uuid}`
- `/api/dnsmasq/service/reconfigure`

## Troubleshooting

**`unauthorized`**
: The API key/secret pair is wrong, or the user lacks permission for the target module. Verify in System → Access → Users → *user* → API keys.

**`opnsense reachable but {engine} search response is unparseable (is the {engine} module enabled?)`**
: The API responded but the payload doesn't match the expected shape. Most often this means the target resolver (Unbound or Dnsmasq) is disabled on OPNsense.

**Records appear in dnsweaver logs but not in DNS answers**
: OPNsense didn't reload. Check the reconfigure endpoint in dnsweaver logs, or if you're in `RECONFIGURE_MODE=never`, trigger the reload out of band.

**`opnsense provider requires an IP target`**
: The A/AAAA record's `TARGET` is not a parseable IP. Host overrides on both engines resolve names to IPs, so the target must be `10.1.2.3` or `2001:db8::1`, not another hostname.
