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
| `DNSWEAVER_INCUS_TRUST_TOKEN` | No | — | One-time Incus trust token to enroll a client certificate. See [Trust-Token Authentication](#trust-token-authentication). |
| `DNSWEAVER_INCUS_CERT_STORE` | No | — | Writable directory where the enrolled client keypair is persisted. Required with `TRUST_TOKEN`. |
| `DNSWEAVER_INCUS_TLS_SERVER_NAME` | No | — | SNI / verification hostname override. |
| `DNSWEAVER_INCUS_TLS_MIN_VERSION` | No | `1.2` | Minimum TLS protocol version (`1.2` or `1.3`). |
| `DNSWEAVER_INCUS_TLS_SKIP_VERIFY` | No | `false` | Skip Incus TLS certificate verification. Prefer `TLS_CA_FILE` or `TLS_PIN_SHA256`. Logs a warning when enabled. |
| `DNSWEAVER_INCUS_TLS_PIN_SHA256` | No | — | Pin the Incus server's leaf certificate to this SHA-256 fingerprint (hex, colons optional). Verifies a self-signed server without a CA file. See [Server verification](#server-verification). |

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

### Server verification

Incus's default server certificate only carries loopback Subject Alternative
Names (`127.0.0.1`, `::1`). Verifying it by hostname against a LAN address
therefore fails with `x509: certificate is valid for 127.0.0.1, ::1, not <ip>`.
There are three ways to make a remote HTTPS connection verify:

1. **CA file** (`DNSWEAVER_INCUS_TLS_CA_FILE`): supply the PEM that issued the
   server certificate. Standard chain verification applies. Best when you run
   your own CA.
2. **Fingerprint pin** (`DNSWEAVER_INCUS_TLS_PIN_SHA256`): pin the server's
   leaf certificate SHA-256. No CA file needed, still cryptographically
   anchored. Get the fingerprint from the Incus host with
   `incus config trust list` or
   `openssl s_client -connect host:8443 </dev/null | openssl x509 -fingerprint -sha256 -noout`.
   Trust-token enrollment sets this pin automatically from the token.
3. **Skip verification** (`DNSWEAVER_INCUS_TLS_SKIP_VERIFY=true`): opt-in,
   for throwaway dev boxes only. Logs a warning, and leaves the connection open
   to MITM. Prefer one of the above for anything you keep.

!!! tip "Recommended: pre-provisioned certificate, no persistent state"
    The stateless default is to generate the client certificate once,
    out-of-band, and mount it read-only from your platform's secret store
    (Kubernetes Secret, Docker/Swarm secret, or a mounted file):

    ```bash
    # once, on the Incus host
    incus remote generate-certificate    # writes client.crt / client.key
    incus config trust add-certificate client.crt
    ```

    ```bash
    DNSWEAVER_INCUS_URL=https://incus-host:8443
    DNSWEAVER_INCUS_TLS_CERT_FILE=/run/secrets/incus_client_cert
    DNSWEAVER_INCUS_TLS_KEY_FILE=/run/secrets/incus_client_key
    DNSWEAVER_INCUS_TLS_PIN_SHA256=<server leaf fingerprint>   # or TLS_CA_FILE
    ```

    Nothing is generated at runtime, so the pod needs no writable volume. This
    works identically on Kubernetes, Docker, and Incus, and is the recommended
    setup for HA (every replica mounts the same certificate).

### Trust-Token Authentication

Instead of pre-provisioning a client certificate, dnsweaver can enroll itself
using an Incus [trust token](https://linuxcontainers.org/incus/docs/main/authentication/#adding-client-certificates-using-tokens).
This is the easy opt-in path: hand dnsweaver a token and it does the rest. It
trades the stateless property for convenience, so it requires a writable cert
store (see the warning below). If you want to stay stateless, use the
pre-provisioned certificate approach under [Server verification](#server-verification)
instead.

On first start dnsweaver generates a client keypair, registers it with the
token, and **persists it to a writable cert store** for reuse. Trust tokens are
one-time use, so the cert store must survive restarts.

```bash
# On the Incus host: issue a one-time token for dnsweaver
incus config trust add dnsweaver
# (copy the printed token)
```

```bash
# dnsweaver
DNSWEAVER_INCUS_URL=https://incus-host.home.example.com:8443
DNSWEAVER_INCUS_TRUST_TOKEN=<one-time token>
DNSWEAVER_INCUS_CERT_STORE=/var/lib/dnsweaver/incus   # writable, persistent
DNSWEAVER_INCUS_TLS_CA_FILE=/run/secrets/incus_server_ca   # optional
```

Behavior:

- **First start:** generates an ECDSA keypair + self-signed client certificate,
  presents it in the TLS handshake to `/1.0/certificates` with the token, and
  writes `client.crt` / `client.key` (mode `0600`) into the cert store.
- **Server pinning:** the trust token embeds the server's certificate
  fingerprint. dnsweaver pins it automatically for both enrollment and ongoing
  connections, and persists it as `server.fingerprint` in the cert store. This
  is why token enrollment works against Incus's loopback-only default
  certificate with no CA file.
- **Subsequent starts:** reuses the persisted certificate and pinned
  fingerprint; the (now-consumed) token is ignored. Enrollment is idempotent.
- **Restricted to projects:** when `DNSWEAVER_INCUS_PROJECTS` is set, the
  certificate is registered restricted to those projects.
- **Fallback:** if no token is set, dnsweaver uses
  `DNSWEAVER_INCUS_TLS_CERT_FILE` / `DNSWEAVER_INCUS_TLS_KEY_FILE` as before.

!!! warning "The cert store must be persistent"
    Because trust tokens are single-use, a non-persistent cert store means every
    restart needs a fresh token. Mount `DNSWEAVER_INCUS_CERT_STORE` on a durable
    volume. The directory is created `0700` and the keypair written `0600`.

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
