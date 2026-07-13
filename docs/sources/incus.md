---
title: Incus Source
description: Automatic DNS A record creation for Incus system containers and virtual machines
icon: material/cube-outline
---

# Incus Source

The Incus source creates DNS A records for running [Incus](https://linuxcontainers.org/incus/) system containers and virtual machines. It polls the Incus REST API to discover instances, resolves each instance's IP address from its network state, then registers an A record mapping `<instance-name>.<domain>` to that IP. It also subscribes to the Incus event stream so instance changes are reflected in DNS almost immediately (see [Event-Driven Updates](#event-driven-updates)).

It connects either to a **local Unix socket** (agent running on the Incus host) or to a **remote HTTPS endpoint** secured with a client certificate.

## How It Works

```mermaid
flowchart LR
    A["Incus API<br/>/1.0/instances?recursion=2"] -->|"Poll interval"| B["List containers / VMs"]
    B -->|"Filter: state"| C["Resolve IP"]
    C -->|"InstanceState.Network<br/>(first global inet addr)"| D["Hostname + IP"]
    D --> E["Incus Source"]
    E -->|"A record"| F["Reconciler → DNS"]
```

1. **Lister** polls `/1.0/instances?recursion=2` and applies the state filter
2. **IP resolver** scans each instance's network interfaces, skipping loopback and non-routable addresses, and selects the first global IPv4 address
3. **Source** maps the instance name + configured domain suffix to a fully-qualified hostname
4. **Reconciler** creates or updates A records via the matching DNS provider

A background **event watcher** subscribes to the Incus event stream and triggers
an out-of-band reconcile whenever an instance changes, so updates do not have to
wait for the next poll. See [Event-Driven Updates](#event-driven-updates).

## Event-Driven Updates

Alongside the periodic poll, the Incus source runs an event watcher that
subscribes to the Incus event stream at `/1.0/events` (filtered to `lifecycle`
events). When an instance is created, started, stopped, or deleted, the watcher
triggers a reconcile within a short debounce window, giving near-instant DNS
updates instead of waiting for `DNSWEAVER_RECONCILE_INTERVAL`.

```mermaid
flowchart LR
    A["Incus event stream<br/>/1.0/events (lifecycle)"] -->|"instance-started<br/>instance-stopped<br/>..."| B["Event watcher"]
    B -->|"debounce 2s"| C["Trigger reconcile"]
    C --> D["Lister re-reads state<br/>/1.0/instances"]
    D --> E["Reconciler → DNS"]
```

Design notes:

- **The lister remains the source of truth.** Events only *trigger* a reconcile;
  the current instance state (and each instance's IP) is always re-read from
  `/1.0/instances`. The periodic poll stays enabled as a safety net for any
  missed event.
- **Bursts coalesce.** A 2-second debounce collapses a burst of events (for
  example, an `incus-compose up` that starts many instances at once) into a
  single reconcile.
- **Resilient connection.** The watcher reconnects with a short backoff if the
  stream drops, and shuts down cleanly on exit. It reuses the same transport as
  the lister, so it works over both the local Unix socket and a remote
  HTTPS + TLS endpoint.
- **No extra configuration.** The watcher starts automatically whenever the
  Incus source is active (`DNSWEAVER_INCUS_URL` or
  `DNSWEAVER_INCUS_SOCKET_PATH` is set).

### Metrics

| Metric | Type | Description |
| :----- | :--- | :---------- |
| `dnsweaver_incus_events_processed_total{action}` | counter | Incus events processed, labeled by lifecycle action (e.g. `instance-started`). |
| `dnsweaver_incus_watcher_reconnects_total` | counter | Number of times the event watcher reconnected after a stream error. |

## Multiple Projects

Incus organizes instances into [projects](https://linuxcontainers.org/incus/docs/main/projects/).
By default the Incus source watches a single project. A single dnsweaver instance
can watch many projects at once, so one deployment can serve an entire Incus
cluster rather than running one dnsweaver per project.

There are three modes, in order of precedence:

| Mode | Configuration | Behavior |
| :--- | :------------ | :------- |
| **All projects** | `DNSWEAVER_INCUS_ALL_PROJECTS=true` | Watch every project the credentials can see, including projects created later. Lowest friction for large deployments. |
| **Explicit list** | `DNSWEAVER_INCUS_PROJECTS=team-a,team-b` | Watch exactly the listed projects. Projects that do not exist yet are still watched and picked up the moment they appear. |
| **Single project** | `DNSWEAVER_INCUS_PROJECT=team-a` | Watch one project (the original behavior). Empty = the Incus `default` project. |

`DNSWEAVER_INCUS_PROJECTS` also accepts `*` or `all` as a shorthand that is
equivalent to `DNSWEAVER_INCUS_ALL_PROJECTS=true`.

Each instance's project is preserved in its [workload metadata](#workload-metadata),
so records stay correctly attributed no matter how many projects are in scope.
Both the periodic lister and the [event watcher](#event-driven-updates) honor the
selected scope: all-projects mode uses one stream for the whole server, while an
explicit list uses one stream per project.

!!! warning "All-projects requires unrestricted credentials"
    `DNSWEAVER_INCUS_ALL_PROJECTS=true` (and the `*` / `all` shorthand) use the
    Incus API's all-projects mode, which requires **server-wide (unrestricted)**
    credentials. A project-restricted certificate cannot list or watch other
    projects — use `DNSWEAVER_INCUS_PROJECTS` with an explicit list in that case.

```bash
# Watch an entire Incus cluster with one dnsweaver instance
DNSWEAVER_INCUS_URL=https://incus-host:8443
DNSWEAVER_INCUS_ALL_PROJECTS=true
DNSWEAVER_INCUS_DOMAIN_SUFFIX=home.example.com
```

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
| :------- | :------- | :------ | :---------- |
| `DNSWEAVER_INCUS_URL` | Alt | — | Remote Incus API base URL, e.g. `https://incus-host:8443`. Mutually exclusive with `DNSWEAVER_INCUS_SOCKET_PATH`. |
| `DNSWEAVER_INCUS_SOCKET_PATH` | Alt | — | Path to the local Incus Unix socket, e.g. `/var/lib/incus/unix.socket`. Mutually exclusive with `DNSWEAVER_INCUS_URL`. |
| `DNSWEAVER_INCUS_PROJECT` | No | _(all / default)_ | Restrict discovery to a single Incus project |
| `DNSWEAVER_INCUS_PROJECTS` | No | — | Comma-separated list of Incus projects to watch, e.g. `team-a,team-b`. Use `*` or `all` as a shorthand for all projects. See [Multiple Projects](#multiple-projects). |
| `DNSWEAVER_INCUS_ALL_PROJECTS` | No | `false` | Watch **every** Incus project via the API's all-projects mode. Requires server-wide (unrestricted) credentials. See [Multiple Projects](#multiple-projects). |
| `DNSWEAVER_INCUS_STATE_FILTER` | No | `running` | Instance status filter (`running`, `stopped`, etc.) |
| `DNSWEAVER_INCUS_DOMAIN_SUFFIX` | No | — | Domain suffix appended to instance names, e.g. `home.example.com` |
| `DNSWEAVER_INCUS_TARGET_MODE` | No | `guest-ip` | Target resolution mode. `guest-ip` (default) emits an A record per instance IP. `instance` defers record type and target to the matching provider instance — useful for pointing all instances at a reverse proxy via CNAME. |
| `DNSWEAVER_INCUS_TLS_CA_FILE` | No | — | Path to PEM CA bundle that issued the Incus server certificate (remote HTTPS). |
| `DNSWEAVER_INCUS_TLS_CERT_FILE` | No | — | Client certificate for mutual TLS against the Incus API (pair with `TLS_KEY_FILE`). |
| `DNSWEAVER_INCUS_TLS_KEY_FILE` | No | — | Client private key for mutual TLS. |
| `DNSWEAVER_INCUS_TLS_SERVER_NAME` | No | — | SNI / verification hostname override. |
| `DNSWEAVER_INCUS_TLS_MIN_VERSION` | No | `1.2` | Minimum TLS protocol version (`1.2` or `1.3`). |
| `DNSWEAVER_INCUS_TLS_SKIP_VERIFY` | No | `false` | Skip Incus TLS certificate verification. Prefer `TLS_CA_FILE`. |

!!! warning "Pick exactly one endpoint"
    Set **either** `DNSWEAVER_INCUS_URL` (remote HTTPS) **or**
    `DNSWEAVER_INCUS_SOCKET_PATH` (local socket) — never both. Setting both is a
    configuration error and dnsweaver will refuse to start.

### Source Registration

Add `incus` to `DNSWEAVER_SOURCES`:

```bash
DNSWEAVER_SOURCES=incus
```

!!! tip "Auto-registration"
    When `DNSWEAVER_INCUS_URL` or `DNSWEAVER_INCUS_SOCKET_PATH` is set, the Incus
    source is **automatically registered** even if not listed in
    `DNSWEAVER_SOURCES`. You only need to list it explicitly if you want to
    control source ordering relative to other sources.

## Hostname Resolution

The source determines the DNS hostname for each instance using this logic, in order of precedence:

1. **`user.dnsweaver.hostname` config key** — set on the instance, its value is used verbatim as the hostname (e.g. `incus config set web user.dnsweaver.hostname=app.example.net`)
2. **`dnsweaver.hostname` label** — the [incus-compose](#incus-compose-labels) form of the override; used verbatim when the native `user.dnsweaver.hostname` key is not set
3. **Instance name contains a dot** — used directly as an FQDN (e.g., `db.home.example.com`)
4. **Domain suffix configured** — appended to the instance name (e.g., `web` + `home.example.com` → `web.home.example.com`)
5. **None of the above** — the instance is skipped; a debug log entry is emitted

!!! warning "Domain suffix is strongly recommended"
    Without a domain suffix, only instances whose names already contain a dot (or
    that set `user.dnsweaver.hostname`) will produce DNS records. Set
    `DNSWEAVER_INCUS_DOMAIN_SUFFIX` to ensure all instances are registered.

## incus-compose Labels

[incus-compose](https://github.com/lxc/incus-compose) runs Docker Compose files
against Incus. Since v1.0.0-rc.2 it stores each Compose `labels:` entry as an
instance config key prefixed `user.label.` — for example, a Compose label
`traefik.http.routers.app.rule` becomes the instance config key
`user.label.traefik.http.routers.app.rule`.

dnsweaver's Incus adapter surfaces every instance config key as a workload
label, and **additionally** exposes each `user.label.<key>` under its stripped
`<key>` form. The raw `user.label.*` key is always retained for transparency,
and a stripped alias never overwrites a label that is already present.

This means the existing label-based sources consume incus-compose labels with
no extra configuration — just enable the source that matches your labels
alongside `incus`:

| Compose label | Stored as | Read by source |
| :------------ | :-------- | :------------- |
| `dnsweaver.hostname=app.example.com` | `user.label.dnsweaver.hostname` | `dnsweaver` (or the Incus source's own override) |
| `traefik.http.routers.app.rule=Host(...)` | `user.label.traefik.http.routers.app.rule` | `traefik` |
| `caddy=app.example.com` | `user.label.caddy` | `caddy` |

Example — a Compose service whose Traefik router labels drive DNS:

```yaml
services:
  app:
    image: docker.io/nginx:alpine
    labels:
      traefik.enable: "true"
      traefik.http.routers.app.rule: "Host(`app.example.com`)"
```

```bash
DNSWEAVER_SOURCES=traefik,incus
DNSWEAVER_INCUS_SOCKET_PATH=/var/lib/incus/unix.socket
```

!!! note "Two always-present labels"
    incus-compose always adds `user.label.incus-compose.project` and
    `user.label.incus-compose.service` (the Compose project and service names).
    These are surfaced de-prefixed as `incus-compose.project` /
    `incus-compose.service` and can be read by any source or used for debugging.

### Per-instance hostname override

Pin an arbitrary FQDN to any instance with a config key:

```bash
incus config set webserver user.dnsweaver.hostname=shop.example.com
```

This takes precedence over the derived `<name>.<domain>` hostname.

## IP Address Resolution

The resolver reads each instance's live network state (`InstanceState.Network`) and selects an address by:

1. Sorting interface names for deterministic selection
2. Skipping loopback interfaces (`lo`, `lo0`)
3. Selecting the first **global** IPv4 (`inet`) address
4. Skipping non-routable addresses (link-local, etc.)

!!! note "Tailscale / CGNAT addresses are kept"
    Addresses in the `100.64.0.0/10` CGNAT range (used by Tailscale) are treated
    as valid targets, so Tailscale-connected instances resolve to their tailnet IP.

Instances with no resolvable IP are skipped in **both** target modes — the IP existence acts as a liveness gate.

## Target Mode

`DNSWEAVER_INCUS_TARGET_MODE` controls what the source emits for each discovered instance:

| Mode | Record Type | Target | Use Case |
| :--- | :---------- | :----- | :------- |
| `guest-ip` *(default)* | `A` | Instance's resolved IP | Direct DNS resolution to each container/VM |
| `instance` | _from instance_ | _from instance_ | Point all Incus-discovered hostnames at a reverse proxy |

In `instance` mode, the source emits the hostname only (no record-type or target hints).
The matching provider instance's `RECORD_TYPE` and `TARGET` drive the resulting record,
so a CNAME instance pointed at NPMplus / Traefik / Caddy will create CNAME records for
every Incus instance that matches its `DOMAINS` filter.

### Example: CNAME everything to a reverse proxy

```bash
DNSWEAVER_SOURCES=incus
DNSWEAVER_INCUS_SOCKET_PATH=/var/lib/incus/unix.socket
DNSWEAVER_INCUS_DOMAIN_SUFFIX=home.example.com
DNSWEAVER_INCUS_TARGET_MODE=instance         # opt in

DNSWEAVER_INSTANCES=npmplus
DNSWEAVER_NPMPLUS_TYPE=technitium
DNSWEAVER_NPMPLUS_RECORD_TYPE=CNAME
DNSWEAVER_NPMPLUS_TARGET=npmplus.home.example.com   # all instances point here
DNSWEAVER_NPMPLUS_DOMAINS=*.home.example.com
DNSWEAVER_NPMPLUS_URL=https://technitium.home.example.com
DNSWEAVER_NPMPLUS_TOKEN_FILE=/run/secrets/technitium_token
```

Every Incus instance matching `*.home.example.com` will get a CNAME pointing to
`npmplus.home.example.com` instead of an A record pointing at the instance's own IP.

## Remote HTTPS Access

To reach Incus over the network, add a trust certificate to the Incus server and point dnsweaver at the API with a matching client certificate:

```bash
# On the Incus host: expose the API and trust dnsweaver's client cert
incus config set core.https_address :8443
incus config trust add-certificate dnsweaver.crt
```

Then configure dnsweaver:

```bash
DNSWEAVER_INCUS_URL=https://incus-host.home.example.com:8443
DNSWEAVER_INCUS_TLS_CERT_FILE=/run/secrets/incus_client_cert
DNSWEAVER_INCUS_TLS_KEY_FILE=/run/secrets/incus_client_key
DNSWEAVER_INCUS_TLS_CA_FILE=/run/secrets/incus_server_ca
```

!!! tip "Local socket needs no TLS"
    When using `DNSWEAVER_INCUS_SOCKET_PATH`, no TLS configuration is required —
    access is governed by Unix socket file permissions. Mount the socket into the
    container and ensure the dnsweaver process can read it.

## Workload Metadata

Each Incus instance is mapped to a workload with the following metadata:

| Field | Value |
| :---- | :---- |
| `Platform` | `incus` |
| `Kind` | `incus-container` or `incus-vm` |
| `ID` / `Router` | `<project>/<instance-name>` |
| Labels | The instance's `config` keys, verbatim (e.g. `user.dnsweaver.hostname`) |

## Example: Docker Compose (local socket)

```yaml
services:
  dnsweaver:
    image: ghcr.io/maxfield-allison/dnsweaver:latest
    environment:
      DNSWEAVER_SOURCES: incus
      DNSWEAVER_INCUS_SOCKET_PATH: /var/lib/incus/unix.socket
      DNSWEAVER_INCUS_DOMAIN_SUFFIX: home.example.com
    volumes:
      # Mount the host's Incus socket read-only
      - /var/lib/incus/unix.socket:/var/lib/incus/unix.socket:ro
```

## Example: Docker Compose (remote HTTPS)

```yaml
services:
  dnsweaver:
    image: ghcr.io/maxfield-allison/dnsweaver:latest
    environment:
      DNSWEAVER_SOURCES: incus
      DNSWEAVER_INCUS_URL: https://incus-host.home.example.com:8443
      DNSWEAVER_INCUS_TLS_CERT_FILE: /run/secrets/incus_client_cert
      DNSWEAVER_INCUS_TLS_KEY_FILE: /run/secrets/incus_client_key
      DNSWEAVER_INCUS_TLS_CA_FILE: /run/secrets/incus_server_ca
      DNSWEAVER_INCUS_DOMAIN_SUFFIX: home.example.com
    secrets:
      - incus_client_cert
      - incus_client_key
      - incus_server_ca

secrets:
  incus_client_cert:
    file: ./secrets/incus_client.crt
  incus_client_key:
    file: ./secrets/incus_client.key
  incus_server_ca:
    file: ./secrets/incus_server_ca.pem
```

## Example: Kubernetes Secret (remote HTTPS)

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: dnsweaver-incus
  namespace: dnsweaver
type: Opaque
stringData:
  client.crt: |
    -----BEGIN CERTIFICATE-----
    ...
  client.key: |
    -----BEGIN PRIVATE KEY-----
    ...
  server-ca.pem: |
    -----BEGIN CERTIFICATE-----
    ...
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dnsweaver
  namespace: dnsweaver
spec:
  template:
    spec:
      containers:
        - name: dnsweaver
          env:
            - name: DNSWEAVER_SOURCES
              value: incus
            - name: DNSWEAVER_INCUS_URL
              value: https://incus-host.home.example.com:8443
            - name: DNSWEAVER_INCUS_TLS_CERT_FILE
              value: /etc/dnsweaver/incus/client.crt
            - name: DNSWEAVER_INCUS_TLS_KEY_FILE
              value: /etc/dnsweaver/incus/client.key
            - name: DNSWEAVER_INCUS_TLS_CA_FILE
              value: /etc/dnsweaver/incus/server-ca.pem
            - name: DNSWEAVER_INCUS_DOMAIN_SUFFIX
              value: home.example.com
          volumeMounts:
            - name: incus-tls
              mountPath: /etc/dnsweaver/incus
              readOnly: true
      volumes:
        - name: incus-tls
          secret:
            secretName: dnsweaver-incus
```
