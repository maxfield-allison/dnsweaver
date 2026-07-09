# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [2.4.0] - 2026-07-08

Minor release focused on Docker connectivity robustness: an opt-in for reading
root-owned sockets (Synology), a bounded startup connection retry for label-driven
socket proxies, and a Go toolchain security bump.

### Added
- **`DNSWEAVER_DOCKER_GID` escape hatch** for reading a Docker socket whose group
  can't be auto-detected. The container entrypoint auto-detects a bind-mounted
  socket's GID and adds the unprivileged `dnsweaver` user to it, but deliberately
  skips GID 0 (root-owned sockets) rather than silently granting root-group
  access. Platforms that force a root-owned socket and won't let you change it
  (e.g. Synology Container Manager) can now set `DNSWEAVER_DOCKER_GID=0` to opt
  in explicitly — the process still drops to `uid 1000` via `su-exec`, only a
  supplementary group is added, so the application never runs as root. A loud
  startup warning is emitted for the GID-0 case.
  ([GitHub #79](https://github.com/maxfield-allison/dnsweaver/issues/79))
- **Bounded Docker startup connection retry** (`DNSWEAVER_DOCKER_CONNECT_TIMEOUT`,
  default `30s`). dnsweaver now retries the initial Docker connection with capped
  backoff before failing hard, instead of exiting on the first error. This fixes
  a startup race with label-driven socket proxies (e.g. `wollomatic/socket-proxy`)
  that only authorize dnsweaver's container a few seconds after it starts — the
  first `Info`/`ping` call would hit `403`/`405` and the process would exit. It
  logs `waiting for docker endpoint to become available` at INFO until connected.
  Set `DNSWEAVER_DOCKER_CONNECT_TIMEOUT=0` for strict fail-fast. Deterministic
  misconfiguration (Swarm forced but node is not a manager) still fails
  immediately. ([GitHub #125](https://github.com/maxfield-allison/dnsweaver/issues/125))

### Changed
- Deprecation notices for the legacy TLS aliases (`INSECURE_SKIP_VERIFY`,
  `DNSWEAVER_PROXMOX_VERIFY_TLS`) no longer claim removal "in v2.0" — the
  project is already past v2.0 and the aliases still ship. The operator-facing
  log warnings now say "a future major release."

### Security
- Bumped the Go toolchain to **1.26.5** to pick up the `crypto/tls` fix for
  [GO-2026-5856](https://pkg.go.dev/vuln/GO-2026-5856) (Encrypted Client Hello
  privacy leak). `go.mod`, the Dockerfile `GO_VERSION`, and the GitLab CI base
  image are bumped in lock-step.

### Documentation
- Made the **Docker socket proxy** the recommended standalone deployment path,
  with a full runnable example
  ([`docs/examples/docker-compose.socket-proxy.example.yml`](docs/examples/docker-compose.socket-proxy.example.yml))
  and a Synology-specific section covering the root-owned socket case. Documented
  socket permissions, the non-root privilege drop, and `DNSWEAVER_DOCKER_GID`.
  ([GitHub #79](https://github.com/maxfield-allison/dnsweaver/issues/79))
- Documented the Cloudflare per-host **`proxied` override** in the native Label
  Reference (`dnsweaver.proxied` and `dnsweaver.records.<name>.proxied`, plus
  `dnsweaver.records.<name>.meta.<key>`), with a worked example on the native
  labels and Cloudflare provider pages. Previously the override was usable but
  undocumented. ([discussion #113](https://github.com/maxfield-allison/dnsweaver/discussions/113))
- Added the `incus` source to the `DNSWEAVER_SOURCES` enumeration and refreshed
  stale "will be removed in v2.0" deprecation notes across the provider,
  environment, FAQ, and Proxmox source pages.

## [2.3.0] - 2026-07-04

Minor release adding the OPNsense DNS provider and a standalone platform mode for
running dnsweaver without Docker or Kubernetes.

### Added
- **Standalone platform mode** (`DNSWEAVER_PLATFORM=none`, alias `standalone`).
  dnsweaver can now run as a bare binary on a host, VM, or LXC with no Docker or
  Kubernetes runtime — previously it always created a Docker (or Kubernetes)
  client at startup and exited fatally with `Cannot connect to the Docker
  daemon` even when only non-container sources were configured. With `none`, no
  container-runtime client is created; configure at least one non-container
  source instead — a Proxmox VE source (`DNSWEAVER_PROXMOX_URL`), an Incus
  source (`DNSWEAVER_INCUS_URL`/`DNSWEAVER_INCUS_SOCKET_PATH`), or a
  file-discovery source (e.g. `DNSWEAVER_SOURCE_TRAEFIK_FILE_PATHS`). Startup
  fails with an actionable error if `none` is set without any such source.
  ([GitHub #116](https://github.com/maxfield-allison/dnsweaver/issues/116))
- **OPNsense provider** with pluggable engine backend for **Unbound** and
  **Dnsmasq** (OPNsense 24.7+), driven by the OPNsense REST API
  ([GitHub #114](https://github.com/maxfield-allison/dnsweaver/issues/114),
  GitLab #188). One provider covers both DNS engines OPNsense ships with;
  pick with `DNSWEAVER_{NAME}_ENGINE=unbound|dnsmasq` (default `unbound`).
  Ownership is tracked via a `dnsweaver:{instance}` prefix on the host
  override's description field, since neither engine's host overrides support
  TXT records — dnsweaver only lists and mutates rows it owns, so
  operator-managed host overrides in the OPNsense GUI are always safe.
  Post-write reload behavior is controlled by
  `DNSWEAVER_{NAME}_RECONFIGURE_MODE` (`per_write` default, or `never`).
  Supports A/AAAA in v1; see
  [docs/providers/opnsense.md](docs/providers/opnsense.md).

## [2.2.1] - 2026-07-03

Patch release rolling up TLS diagnostics, RFC 2136 TTL hardening, a Go toolchain
+ dependency bump for CVE coverage, docs/SEO polish, and the sync-workflow tag
idempotency fix. No new features and no breaking changes.

### Changed
- **TLS file permission errors now name the runtime uid/gid.** The container
  drops privileges to the unprivileged `dnsweaver` user (uid/gid `1000`) via
  `su-exec`, so a CA bundle, client certificate, or private key mounted
  `root:root 0600` fails with `permission denied` even when the container is
  started as root. The error message now reports the uid/gid the process
  actually runs as and points at the fix, so operators no longer burn time
  thinking "but I AM root."
  ([#90](https://github.com/maxfield-allison/dnsweaver/issues/90),
  [#98](https://github.com/maxfield-allison/dnsweaver/pull/98))
- **Go toolchain pinned to 1.26.4** (from 1.25.11). `k8s.io/{apimachinery,
  client-go}` 0.36 requires Go 1.26; pinning the patch release pulls in the
  standard-library fixes for 13 CVEs present in 1.26.0 (`crypto/x509`,
  `crypto/tls`, `net/http`, `net/url`, `os`, `mime`, `net/textproto`), keeping
  `govulncheck` green.
  ([#109](https://github.com/maxfield-allison/dnsweaver/pull/109))

### Fixed
- **RFC 2136 record TTL conversion is now clamped on both bounds.**
  `record.TTL` is a platform `int` that can exceed `uint32` or be negative;
  the existing conversion clamped the upper bound only, so a negative TTL
  would wrap to a large `uint32`. TTLs are now clamped through an explicit
  `clampTTL` helper (`v<0 → 0`, `v>MaxUint32 → MaxUint32`) that CodeQL
  recognizes as a bounds check, closing the
  `go/incorrect-integer-conversion` alert without relying on Go's `min`/`max`
  builtins.
  ([#108](https://github.com/maxfield-allison/dnsweaver/pull/108),
  [#110](https://github.com/maxfield-allison/dnsweaver/pull/110))
- **GitHub→GitLab tag sync no longer creates spurious "tag differs" pipelines.**
  The sync workflow now compares peeled commit SHAs when checking whether an
  existing tag matches, so annotated tags (which point at a tag object, not a
  commit) are treated as equivalent to their commit-pointing counterparts.
  ([#97](https://github.com/maxfield-allison/dnsweaver/pull/97))

### Dependencies
- Bumped the `go-dependencies` group:
  `golang.org/x/crypto` 0.52.0 → 0.53.0, `k8s.io/apimachinery` and
  `k8s.io/client-go` 0.35.2 → 0.36.2.
  ([#109](https://github.com/maxfield-allison/dnsweaver/pull/109))
- Bumped GitHub Actions to current majors:
  `actions/setup-go` 5→6 ([#101](https://github.com/maxfield-allison/dnsweaver/pull/101)),
  `actions/configure-pages` 4→6 ([#102](https://github.com/maxfield-allison/dnsweaver/pull/102)),
  `github/codeql-action` 3→4 ([#103](https://github.com/maxfield-allison/dnsweaver/pull/103)),
  `actions/setup-python` 5→6 ([#104](https://github.com/maxfield-allison/dnsweaver/pull/104)),
  `actions/checkout` 4→7 ([#105](https://github.com/maxfield-allison/dnsweaver/pull/105)),
  `actions/upload-pages-artifact` 3→5 ([#111](https://github.com/maxfield-allison/dnsweaver/pull/111)),
  `actions/deploy-pages` 4→5 ([#112](https://github.com/maxfield-allison/dnsweaver/pull/112)).

### Documentation
- Added a **TLS Certificate File Permissions** section to the environment
  reference (chown / group-readable / Docker secrets recipes), linked from the
  README TLS section and every provider's mTLS example (Technitium, AdGuard,
  Cloudflare, Pi-hole, Webhook) plus the Proxmox source.
  ([#90](https://github.com/maxfield-allison/dnsweaver/issues/90))
- **Discoverability & SEO pass:** added a "Why dnsweaver?" positioning section
  and a Star History chart to the README, published a Docker Hub overview
  (`docker/DOCKERHUB.md`) with a CI job that pushes it to Docker Hub's
  `full_description` on release, and added SEO meta descriptions to provider,
  source, deployment, and getting-started pages (coverage went from 10/46 to
  every high-intent page).
  ([#107](https://github.com/maxfield-allison/dnsweaver/pull/107))

### Repository
- Community-health scaffolding: `CODE_OF_CONDUCT.md`, issue and PR templates,
  Dependabot config for Go / GitHub Actions / Docker, CodeQL analysis workflow,
  and a CI status badge in the README.
  ([#100](https://github.com/maxfield-allison/dnsweaver/pull/100))

### CI
- **GitLab CI runner image and Dockerfile builder image bumped to
  `golang:1.26.4-alpine`** to match the `go.mod` toolchain pin from #109. The
  initial `v2.2.1` tag pipeline failed with
  `go.mod requires go >= 1.26.4 (running go 1.25.11)`; this bumps both build
  surfaces in lock-step.

## [2.2.0] - 2026-06-22

### Added
- **Incus platform source** (`DNSWEAVER_SOURCES=incus`). Discovers running Incus
  system containers and virtual machines and creates DNS `A` records mapping
  each instance to its resolved IP address. Connects either to a local Unix
  socket (`DNSWEAVER_INCUS_SOCKET_PATH`) or a remote HTTPS endpoint
  (`DNSWEAVER_INCUS_URL`) secured with a client certificate — the two are
  mutually exclusive. Per-source configuration:
  - `DNSWEAVER_INCUS_PROJECT` — restrict discovery to a single Incus project.
  - `DNSWEAVER_INCUS_STATE_FILTER` — instance status filter (default `running`).
  - `DNSWEAVER_INCUS_DOMAIN_SUFFIX` — domain appended to instance names
    (e.g. `webserver` → `webserver.home.example.com`).
  - `DNSWEAVER_INCUS_TARGET_MODE` — `guest-ip` (default, A record per instance
    IP) or `instance` (defer record type/target to the matching provider, for
    CNAME-to-reverse-proxy setups).
  - `DNSWEAVER_INCUS_TLS_*` — unified TLS surface (custom CA, mTLS client cert,
    SNI override, configurable minimum version) for remote HTTPS access.

  Hostname precedence: a `user.dnsweaver.hostname` instance config key (verbatim
  override) > an instance name that is already an FQDN > `<name>.<domain>`.
  Instances with no resolvable IP are skipped as a liveness gate. The source is
  auto-registered when an Incus endpoint is configured.

### Fixed
- **Documentation:** the landing-page "Supported Providers" table and the
  testing record-type support matrix were missing the OVHcloud and PowerDNS
  providers added in 2.1.0 (the matrix also omitted AdGuard Home). Both now
  list all nine providers, matching the provider reference and `mkdocs` nav.

## [2.1.0] - 2026-06-21

### Added
- **OVHcloud DNS provider** (`TYPE=ovh`). Manages `A`, `AAAA`, `CNAME`, `SRV`,
  and `TXT` records in OVH DNS zones via the OVH API. Authenticates with an
  application key, application secret, and consumer key (all support the `_FILE`
  suffix for Docker/Kubernetes secrets) and signs requests against OVH server
  time. Per-instance configuration:
  - `DNSWEAVER_{NAME}_APPLICATION_KEY` / `_APPLICATION_SECRET` / `_CONSUMER_KEY`
    — OVH API credentials (required).
  - `DNSWEAVER_{NAME}_ENDPOINT` — API region (`ovh-eu` default, plus `ovh-ca`,
    `ovh-us`, `kimsufi-*`, `soyoustart-*`).
  - `DNSWEAVER_{NAME}_ZONE` — DNS zone name (required).
  - `DNSWEAVER_{NAME}_TTL` — record TTL (default `3600`, minimum `60`, or `0`
    for the zone default).
  Hostnames are converted to zone-relative subdomains automatically and the zone
  is refreshed after every change so updates propagate without manual steps. The
  provider supports native in-place updates and shares the unified TLS
  configuration surface (`DNSWEAVER_{NAME}_TLS_*`).
- **PowerDNS provider via the native Authoritative HTTP API** (`TYPE=powerdns`).
  Manages A/AAAA/CNAME/SRV/TXT records in a pre-existing zone using the PowerDNS
  `/api/v1` REST API with `X-API-Key` authentication, including native in-place
  updates and TXT ownership tracking. This complements the existing
  [RFC 2136](https://maxfield-allison.github.io/dnsweaver/providers/rfc2136/)
  path (which drives PowerDNS over DNS UPDATE/TSIG). New per-instance variable
  `DNSWEAVER_{NAME}_SERVER_ID` (default `localhost`).

## [2.0.0] - 2026-06-21

This release contains no runtime behavior changes. It is a breaking release
solely because the Go module path changed, which requires a major version bump
under Semantic Versioning. It also re-architects the project's collaboration and
release workflow.

### Changed
- **BREAKING: module path is now `github.com/maxfield-allison/dnsweaver`**
  (previously a private GitLab path). Public consumers can now `go get` the
  module by its declared path, and `pkg.go.dev` can resolve it. Anyone importing
  the previous path must update their imports. No runtime behavior changed.

### Infrastructure
- **GitHub is now the source of truth and collaboration surface.** Issues, pull
  requests, code review, and releases live on GitHub; the project follows GitHub
  Flow with `main` as the always-releasable trunk. External contributions are
  now possible — the previous GitLab→GitHub force-push mirror that clobbered
  merges has been removed.
- **Free PR validation on GitHub Actions** (`lint`, `test -race`, `build`,
  `govulncheck`) runs on every pull request.
- **GitLab remains the release engine**, building multi-arch images
  (GHCR + Docker Hub) and publishing GitHub Releases on version tags. `main` and
  tags are synced one-way GitHub→GitLab.
- Removed the dead `advanced-git-sync` integration.
## [1.6.0] - 2026-06-10

### Added
- **SSH remote management for the dnsmasq provider is now functional**
  ([GitHub #91](https://github.com/maxfield-allison/dnsweaver/issues/91),
  GitLab #186). SSH mode was documented and config-validated since v0.7.0 but
  the transport was never wired into the provider, so every reload ran inside
  the dnsweaver container instead of on the remote host (producing errors such
  as `exec: "supervisorctl": executable file not found in $PATH`). The provider
  now uses the shared `pkg/sshutil` package: SFTP writes the managed config file
  on the remote host and SSH exec runs `RELOAD_COMMAND` there. No shared volumes
  or local mounts are required.
- **SSH host key verification via `known_hosts`** (GitLab #153). Two new
  per-instance variables for the dnsmasq provider:
  - `DNSWEAVER_{NAME}_SSH_KNOWN_HOSTS_FILE` — path to an OpenSSH `known_hosts`
    file used to verify the remote host key. Supports the `_FILE` suffix for
    Docker secrets.
  - `DNSWEAVER_{NAME}_SSH_STRICT_HOST_KEY_CHECKING` — `true` (default) or
    `false`. When enabled, a `known_hosts` file is required and a changed or
    unknown host key fails the connection fast with a clear error.
  Host-key verification lives in `pkg/sshutil`, so it is reusable by any future
  SSH-based provider.
- `Closer` interface in `pkg/provider`. Providers that hold long-lived
  connections (such as the dnsmasq SSH transport) are now closed cleanly when
  the registry shuts down.

### Changed
- **SSH host key verification is enabled by default** for the dnsmasq provider
  (`SSH_STRICT_HOST_KEY_CHECKING=true`). Because SSH mode never actually
  connected before this release, there is no practical behavior change for
  existing deployments. Operators who want the previous unverified behavior can
  set `SSH_STRICT_HOST_KEY_CHECKING=false` (insecure; a warning is logged on
  every connection).
- SSH-configured dnsmasq instances now **fail fast at startup** if the remote
  host is unreachable or the host key cannot be verified, instead of silently
  falling back to local execution.

### Fixed
- dnsmasq reload commands configured for SSH mode now execute on the remote host
  via SSH exec rather than inside the dnsweaver container
  ([GitHub #91](https://github.com/maxfield-allison/dnsweaver/issues/91)).

### Security
- **Go toolchain updated from 1.25.10 to 1.25.11**, resolving three standard
  library advisories surfaced by `govulncheck`: `GO-2026-5037` (`crypto/x509`),
  `GO-2026-5038` (`mime`), and `GO-2026-5039` (`net/textproto`).
- **`go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` updated
  from v1.39.0 to v1.43.0** (transitive via the Docker SDK), resolving
  `CVE-2026-39882`.

## [1.5.0] - 2026-05-28

### Added
- **Unified TLS configuration across every HTTP provider and the Proxmox
  source** ([GitHub #89](https://github.com/maxfield-allison/dnsweaver/issues/89),
  GitLab #183). A single `httputil.TLSConfig` now drives TLS for `technitium`,
  `adguard`, `cloudflare`, `pihole`, `webhook`, and the Proxmox VE client.
  New per-instance environment variables (also accepted as
  `DNSWEAVER_PROXMOX_TLS_*`):
  - `DNSWEAVER_{NAME}_TLS_CA_FILE` — PEM CA bundle **appended** to the system
    trust pool (private/internal CAs no longer require disabling verification).
  - `DNSWEAVER_{NAME}_TLS_CERT_FILE` / `_TLS_KEY_FILE` — client certificate
    for **mutual TLS** authentication (resolves the original ask in #89).
  - `DNSWEAVER_{NAME}_TLS_SERVER_NAME` — SNI / verification hostname override
    for IP-addressed connections or split-horizon names.
  - `DNSWEAVER_{NAME}_TLS_MIN_VERSION` — `1.2` (default) or `1.3`.
  - `DNSWEAVER_{NAME}_TLS_SKIP_VERIFY` — canonical name for the legacy
    `INSECURE_SKIP_VERIFY` flag.
- TLS minimum version pinned explicitly to **TLS 1.2** by default. See
  `SECURITY.md`.

### Changed
- **Proxmox HTTP client now uses a transport cloned from
  `http.DefaultTransport`** instead of a bare `&http.Transport{}`. This enables
  HTTP/2 negotiation, proxy environment variables, dial/idle timeouts, and
  shared connection pooling for PVE API traffic — previously all of these were
  silently disabled.
- The shared `httputil` client preserves the same cloned-transport behavior
  whenever a custom TLS configuration is supplied (including skip-verify),
  fixing a long-standing transport-loss bug.
- `technitium.Client.WithInsecureSkipVerify(bool)` now **composes** with any
  pre-existing HTTP client options instead of replacing the entire
  `httpClient`, preventing accidental loss of user-supplied timeouts or
  transports.

### Deprecated
- `DNSWEAVER_{NAME}_INSECURE_SKIP_VERIFY` — use `DNSWEAVER_{NAME}_TLS_SKIP_VERIFY`.
  The legacy variable is auto-migrated at load time with a `WARN` log; it will
  be removed in v2.0.
- `DNSWEAVER_PROXMOX_VERIFY_TLS` (note the inverted polarity) — use
  `DNSWEAVER_PROXMOX_TLS_SKIP_VERIFY`. Auto-migrated at load time with a
  `WARN` log; will be removed in v2.0.

### Fixed
- `httputil.NewClient` no longer discards `http.DefaultTransport` when
  configured with `TLSSkipVerify=true`. HTTP/2 and the shared connection pool
  are now preserved in every TLS configuration. Regression test added.
- Registry HTTP-level `TLSSkipVerify` is now actually consumed by provider
  factories (previously it was set but the per-provider TLS path always took
  precedence, making the registry-level toggle dead code).

### Security
- Bump `golang.org/x/crypto` to `v0.52.0` and `golang.org/x/net` to `v0.55.0`
  to patch govulncheck advisories GO-2026-5013, GO-2026-5017, GO-2026-5018,
  GO-2026-5019, GO-2026-5020 (x/crypto) and GO-2026-5026 (x/net). Closes #184.

## [1.4.6] - 2026-05-12

### Fixed
- **Regression from #86: legitimate fan-out to distinct backends was being
  collapsed.** The first-match-wins fix in
  [#86](https://github.com/maxfield-allison/dnsweaver/issues/86) treated *every*
  pair of overlapping instances as a conflict, including the supported case of
  intentionally writing the same hostname to two different DNS backends (e.g.
  internal Technitium + external Cloudflare for a split-horizon setup). Only
  the first instance in declaration order would write; the rest were silently
  dropped. Precedence is now scoped to a *backend identity* tuple
  `(provider type, endpoint, zone) × record type`. Distinct backends each get
  their write; only instances pointing at the same physical zone collapse to
  the first declared. A startup-time `WARN` enumerates colliding instances so
  misconfigurations remain visible. Backward-compatible: out-of-tree providers
  that don't implement the new optional `provider.Identifiable` interface fall
  back to type-only identity, preserving #86's behavior for them. Closes
  upstream [#88](https://github.com/maxfield-allison/dnsweaver/issues/88).
  Thanks to [@Dampfwalze](https://github.com/Dampfwalze) for the regression
  report and clear reproducer.

## [1.4.5] - 2026-05-10

### Security
- **Bumped Go toolchain to 1.25.10** (CI image, Dockerfile, `go` directive) to
  pick up stdlib fixes for [GO-2026-4971](https://pkg.go.dev/vuln/GO-2026-4971)
  and the `net/http` portion of
  [GO-2026-4918](https://pkg.go.dev/vuln/GO-2026-4918).
- **Bumped `golang.org/x/net` to v0.53.0** for the module portion of
  [GO-2026-4918](https://pkg.go.dev/vuln/GO-2026-4918).
- **Bumped `go.opentelemetry.io/otel` to v1.41.0** to address
  [CVE-2026-29181](https://avd.aquasec.com/nvd/cve-2026-29181) (HIGH):
  multi-value `baggage` header extraction caused excessive allocations,
  enabling a remote DoS amplification. Pulled in transitively via the Docker
  client SDK; no dnsweaver code changes required.

### Fixed
- **Multiple instances fighting over the same record (race condition).** When
  more than one provider instance matched the same hostname — typically because
  `DNSWEAVER_{NAME}_ENTRYPOINTS` filters did not disambiguate them (a hostname
  missing the filtered metadata key is treated as a wildcard) — every matching
  instance wrote the record on every reconciliation. Each provider's cached
  view of the zone was stale relative to the others' writes, so the apparent
  owner alternated each cycle and the record's target flapped between targets.
  `ensureRecord` now respects first-match-wins per the documented contract:
  only the first instance in `DNSWEAVER_INSTANCES` declaration order writes,
  and a `WARN` is logged when overlap is detected so users can narrow scopes
  with `DNSWEAVER_{NAME}_ENTRYPOINTS` (or other metadata filters). Closes
  upstream [#86](https://github.com/maxfield-allison/dnsweaver/issues/86).
  Thanks to [@Dampfwalze](https://github.com/Dampfwalze) for the reproducer
  and detailed log analysis.
- **Reconciler: stopped re-issuing ownership TXT creates every cycle.** Once a
  hostname's `_dnsweaver.<host>` TXT record existed, dnsweaver still POSTed a
  duplicate-create on every reconciliation. dnsweaver swallowed the resulting
  conflict, but upstream DNS servers logged each one as an error — Technitium
  in particular wrote a full `DnsWebServiceException: Cannot add record: record
  already exists` stack trace per managed hostname per cycle. `ensureOwnership
  Record` now consults the per-cycle record cache and short-circuits when it
  already shows our ownership TXT, eliminating the redundant API call across
  all providers. Closes upstream
  [#87](https://github.com/maxfield-allison/dnsweaver/issues/87). Thanks to
  [@Dampfwalze](https://github.com/Dampfwalze) for the report and Technitium
  log evidence.

## [1.4.4] - 2026-05-02

### Fixed
- **AdGuard Home: `USERNAME` env var was silently dropped during config load.**
  `USERNAME` was missing from `providerConfigFields`, so
  `DNSWEAVER_<INSTANCE>_USERNAME` never reached the provider's `LoadConfigFromMap`.
  The AdGuard provider then failed validation with `USERNAME is required` even
  when the variable was clearly set, making the provider impossible to configure
  via environment variables. Added `USERNAME` (and clarified `PASSWORD` shared
  use) to the field list, plus regression tests for both direct and `_FILE`
  secret loading. Closes upstream
  [#85](https://github.com/maxfield-allison/dnsweaver/issues/85). Thanks to
  [@XayneCast](https://github.com/XayneCast) for the report.

## [1.4.3] - 2026-04-30

### Fixed
- **Technitium: CNAME updates were silently a no-op.** `UpdateCNAMERecord` sent
  the new target as `newCname`, which is not a valid Technitium API parameter.
  The request matched the existing record by `cname=<old>` and applied no
  change, while the dnsweaver log claimed success. The fix sends `cname=<new>`
  (CNAME records are unique per name, so the API identifies the record by
  `domain`+`type` alone). Regression test added that asserts `newCname` is
  never sent. Closes upstream
  [#84](https://github.com/maxfield-allison/dnsweaver/issues/84). Thanks to
  [@Dampfwalze](https://github.com/Dampfwalze) for the detailed bug report
  including the Technitium server log proving the no-op.

## [1.4.2] - 2026-04-29

### Added
- **Traefik: `DNSWEAVER_SOURCE_TRAEFIK_DEFAULT_ENTRYPOINTS`** — source-level
  setting that mirrors Traefik's
  [`entryPoints.<name>.asDefault = true`](https://doc.traefik.io/traefik/reference/install-configuration/entrypoints/#opt-asdefault)
  configuration. When set, Traefik routers without an explicit `entryPoints`
  declaration (label or static config) fan out one extraction per default
  entrypoint instead of being treated as wildcard. Required for users with
  `asDefault` flagged in Traefik so unlabeled routers don't silently
  over-publish records to all dnsweaver instances. Unset preserves prior
  wildcard behavior. Closes #180. Refs upstream
  https://github.com/maxfield-allison/dnsweaver/issues/82.

## [1.4.1] - 2026-04-27

### Fixed
- **Technitium: `AUTO_HTTPS_RECORDS` and `AUTO_HTTPS_ALPN` env vars are now
  respected.** Previously these settings were silently ignored because they
  were never wired through `internal/config`'s provider-config field list, so
  the Technitium provider always saw the default (`AUTO_HTTPS_RECORDS=true`)
  regardless of what the user set. Setting
  `DNSWEAVER_{INSTANCE}_AUTO_HTTPS_RECORDS=false` now correctly disables
  companion HTTPS record creation. Fixes
  [#83](https://github.com/maxfield-allison/dnsweaver/issues/83).

## [1.4.0] - 2026-04-27

### Added
- **Proxmox: opt-in `DNSWEAVER_PROXMOX_TARGET_MODE`**. Adds a new env var
  controlling how the Proxmox source resolves DNS targets. Default `guest-ip`
  preserves today's behavior (A record per VM IP). New `instance` mode emits
  the hostname only and defers `RECORD_TYPE` and `TARGET` to the matching
  provider instance — enabling, for example, CNAMEs from every Proxmox
  workload to a reverse proxy. Closes #81.
- **Traefik per-entrypoint routing**: Routers bound to multiple Traefik
  entrypoints now produce one extraction per `(host, entrypoint)` pair,
  and provider instances can opt in to entrypoint-scoped matching via
  `DNSWEAVER_{NAME}_ENTRYPOINTS`. Hostnames carry generic
  `Metadata` (currently `traefik.entrypoint`); instances filter on it
  with AND-of-OR semantics, treating missing keys as wildcards so
  pre-1.4 configs are unchanged. Enables split LAN/VPN DNS targets from
  a single Traefik router. Closes #178. Refs upstream
  https://github.com/maxfield-allison/dnsweaver/issues/82.

### Fixed
- **Proxmox: instance `TARGET` was silently ignored**. Previously the source
  unconditionally set `RecordHints.Target` to the VM's IP, overriding any
  configured `DNSWEAVER_{INSTANCE}_TARGET` and forcing `RECORD_TYPE=A`. Users
  who wanted to point Proxmox-discovered hostnames at a reverse proxy could
  not do so. Now opt-in via `DNSWEAVER_PROXMOX_TARGET_MODE=instance` (see
  Added). Default behavior is unchanged. Closes #81.

## [1.3.0] - 2026-04-23

### Added
- **Caddy source**: Extract hostnames from Docker containers that use
  [caddy-docker-proxy](https://github.com/lucaslorentz/caddy-docker-proxy)
  style labels (`caddy=app.example.com` or indexed `caddy_0`, `caddy_1`).
  Enable with `DNSWEAVER_SOURCES=caddy` (combinable with other sources).
  Caddyfile discovery is not included — only Docker labels are parsed.
  Closes #175.
- **nginx-proxy source**: Extract hostnames from Docker containers that
  declare a `VIRTUAL_HOST` in the jwilder/nginx-proxy convention.
  Recognizes both the literal `VIRTUAL_HOST` label and the canonical
  `com.nginx-proxy.virtual_host` label; comma-separated hostnames are
  supported. Env-var extraction (upstream jwilder reads from container
  environment) is not yet supported and is tracked separately.
  Enable with `DNSWEAVER_SOURCES=nginx-proxy`.
  Closes #174.

### Fixed
- **Proxmox: filter non-routable IPs from guest-agent and LXC responses**.
  The guest-agent and LXC `net0` parsers were returning loopback, link-local,
  and other non-routable addresses alongside real LAN IPs, producing
  unusable A records. All RFC 5735 / RFC 4291 non-routable ranges are now
  filtered out at the source (loopback, link-local, multicast, broadcast,
  unspecified, documentation, benchmarking, IPv6 ULA non-fc00::/7 edges,
  etc.).
- **Proxmox: allow CGNAT range (100.64.0.0/10) for Tailscale IPs**.
  The non-routable filter was too aggressive: it dropped 100.64.0.0/10
  addresses, which Tailscale uses for its mesh (`100.x.y.z`). CGNAT is
  now treated as routable so Proxmox VMs/LXCs on a Tailscale network
  resolve correctly.

### Changed
- **Documentation accuracy pass**: Removed stale references to AdGuard
  as a primary provider example, deleted unused Proxmox role-creation
  copy that referenced internal hostnames, corrected provider capability
  tables to reflect actual code (Pi-hole and dnsmasq are A/CNAME only;
  Cloudflare adds SRV), simplified the architecture mermaid, and removed
  inaccurate claims of Caddy/nginx-proxy support that have now been
  properly implemented in this release.

## [1.2.0] - 2026-04-22

### Added
- **Proxmox VE source**: Auto-create A records for VMs and LXC containers on a
  Proxmox cluster. VMs resolve via the QEMU guest agent
  (`/agent/network-get-interfaces`); LXC containers resolve via the `net0`
  config field. Supports node, tag, and state filtering, and exposes PVE tags
  as workload labels (`proxmox.tag/*`). Configure via `DNSWEAVER_PROXMOX_*`
  environment variables. See `docs/sources/proxmox.md` for the required PVE
  role privileges (`VM.Audit`, `VM.Monitor`, `Pool.Audit`).
  Closes maxfield-allison/dnsweaver#78. Thanks @jaykumar2001 for the request.

### Fixed
- **Technitium `svcParams` unmarshal failure on newer versions**: The
  `zones/records/get` endpoint in newer Technitium DNS Server releases
  returns `svcParams` as a JSON object (`{"alpn":"h2"}`) instead of the
  documented pipe-delimited string (`"alpn|h2"`). This caused
  `failed to recover ownership state` warnings and prevented the reconciler
  from recognising its own existing HTTPS records, triggering spurious
  recreate cycles. Added a `svcParamsValue` named type with a custom
  `UnmarshalJSON` that accepts both representations and normalises to the
  pipe-delimited form internally.

## [1.1.4] - 2026-04-21

### Fixed
- **Docker socket permission denied on first run**: The image runs as a non-root
  user (UID/GID 1000), but the host's `docker` group GID is almost never 1000
  (typically 999 on Debian/Ubuntu, varies on other distros), so mounting
  `/var/run/docker.sock` failed with `permission denied` out of the box.
  Added a small entrypoint script (`docker/entrypoint.sh`) that detects the
  socket's GID at runtime, adds the `dnsweaver` user to a group with that GID,
  then drops privileges via `su-exec` before exec'ing the binary. The standard
  compose example now works without `group_add`. K8s-only deployments and
  socket-proxy setups skip the logic entirely (no socket mounted = no-op).
  Closes maxfield-allison/dnsweaver#79.

### Changed
- **Runtime image now includes `su-exec`** (~20KB) for the entrypoint privilege
  drop. Container briefly starts as root to perform GID detection, then exec's
  the binary as the unprivileged `dnsweaver` user.

## [1.1.3] - 2026-04-10

### Security
- **CACHE_BUST build arg for Docker layer cache invalidation**: `--pull` alone was
  insufficient — if the Alpine base image tag hasn't been rebuilt, Docker layer
  caching preserves stale `apk upgrade` output. Added `ARG CACHE_BUST` in the
  runtime stage and `--build-arg CACHE_BUST=$CI_PIPELINE_ID` to all CI Docker
  build commands, ensuring every pipeline runs a fresh `apk upgrade`
- **Reconciler race condition**: Added `reconcileMu` mutex to serialize
  `Reconcile()` calls, preventing concurrent map access
- **Case-sensitive hostname comparison**: Fixed orphan cleanup to use
  `source.NormalizeHostname()` for consistent case-insensitive hostname matching
- **SSH config `getEnvOrFile` alignment**: When `_FILE` key is set but file is
  unreadable, now returns empty string (hard-fail) matching config behavior
- **Dry-run orphan accuracy**: Always build record cache (was nil in dry-run mode);
  refactored deletion functions to check dry-run per-record
- **RecoverOwnership error handling**: Now returns error listing failed providers
  instead of silently continuing
- **Bounded HTTP response reading**: Replaced all `io.ReadAll` calls in Pi-hole
  client with `httputil.ReadBody` (10 MB limit) to prevent memory exhaustion
- **Integer overflow guards**: Added `gosec G115` clamps — TTL to `uint32` in
  RFC 2136, SRV/HTTPS fields to `uint16` in Technitium

### Fixed
- **Hostname provider map initialization**: Initialize `hostnameProviders` map in
  `New()` instead of lazy nil check

## [1.1.2] - 2026-04-10

### Security
- **Docker build: pull fresh base images**: Added `--pull` to all `docker build`
  commands in CI/CD pipeline to prevent Docker layer caching from reusing Alpine
  images with stale OpenSSL packages (CVE-2026-28390)

## [1.1.1] - 2026-04-10

### Fixed
- **MkDocs build warning**: Fixed broken relative link to CHANGELOG in
  `testing/RELEASE-CHECKLIST.md` that caused GitHub Actions doc build to fail

### Changed
- **Multi-instance docs**: Added section explaining how `DNSWEAVER_INSTANCE_ID`
  interacts with providers that use target-based ownership inference (AdGuard Home,
  Pi-hole file mode, dnsmasq)

## [1.1.0] - 2026-04-10

### Added
- **AdGuard Home provider**: New DNS provider supporting AdGuard Home's DNS Rewrite
  API. Supports A, AAAA, and CNAME records with full CRUD lifecycle, native
  in-place updates via `PUT /control/rewrite/update`, and Basic Auth. Requires
  AdGuard Home v0.107+ ([#77](https://github.com/maxfield-allison/dnsweaver/issues/77))
- **Target-based ownership inference**: Managed-mode orphan cleanup now works for
  providers that cannot store TXT ownership records (AdGuard Home, Pi-hole file mode,
  dnsmasq). When a record's type and target match the provider instance's configured
  values, dnsweaver infers ownership and cleans up the record. Records with different
  targets are preserved, protecting manually-created entries

### Changed
- **Startup warning for non-TXT providers**: Updated log message from
  "ownership tracking unavailable" to "managed mode will use target-based
  ownership inference" — managed mode is now fully functional for these providers

## [1.0.5] - 2026-04-08

### Fixed
- **Companion HTTPS records via `NewWithHTTPClient()`**: `autoHTTPSRecords` and
  `autoHTTPSALPN` config fields were not propagated when constructing Technitium
  providers with a custom HTTP client, causing companion HTTPS record creation to
  silently fail in that code path

### Security
- **Go 1.25.0 → 1.25.9**: Fixes three crypto stdlib CVEs:
  - GO-2026-4947 — crypto/x509 chain building DoS
  - GO-2026-4946 — crypto/x509 policy validation DoS
  - GO-2026-4870 — crypto/tls KeyUpdate DoS
- **CI govulncheck allowlist**: Improved documentation for docker/docker SDK
  vulns (GO-2026-4887, GO-2026-4883) — daemon-side AuthZ issues, not
  exploitable via client SDK imports; no upstream fix available

## [1.0.4] - 2026-04-02

### Security
- **Alpine 3.23 base image**: Runtime base image upgraded from Alpine 3.21 to 3.23
  for reduced CVE surface and latest security patches
- **CI security hardening**: `security:trivy` (filesystem scan) and
  `security:govulncheck` now block pipeline on CRITICAL/HIGH findings instead
  of running in warn-only mode. `govulncheck` uses a wrapper that allows
  known-unfixed upstream vulnerabilities (docker/docker SDK) while blocking
  on any new findings
- **SECURITY.md**: Added responsible vulnerability disclosure policy with
  supported versions, reporting process, and security practices

### Changed
- Alpine base image upgraded from 3.21 to 3.23
- `.trivyignore` entries now include explicit review dates

## [1.0.3] - 2026-03-31

### Security
- **Alpine 3.20 → 3.21**: Resolves 14 CVEs in base image packages (10 openssl
  including CVE-2025-15467 RCE, 3 busybox, 1 zlib)
- **Remove wget from runtime image**: Eliminates 2 HIGH CVEs
  (CVE-2025-69194 path traversal, CVE-2024-10524 SSRF) and reduces attack
  surface. Healthcheck now uses busybox `nc` instead of wget.
- **Container image scanning**: Added Trivy container image scan to CI/CD
  pipeline — CRITICAL/HIGH CVEs now block releases automatically

### Changed
- CI validation job images updated from Alpine 3.20 to 3.21

## [1.0.2] - 2026-03-31

### Added
- **Companion HTTPS Records (Technitium)**: Auto-creates HTTPS (SVCB Type 65)
  companion records alongside A/AAAA/CNAME records to prevent ECH fallback
  errors in split-horizon DNS environments (#158)
  - Enabled by default (`AUTO_HTTPS_RECORDS=true`); set `false` to disable
  - Configurable ALPN protocol via `AUTO_HTTPS_ALPN` (default: `h2`)
  - Skips creation if HTTPS record already exists (safe for manual records)
  - Lifecycle-managed: companion record deleted when parent record is removed
- **HTTPS Record Type**: Added `RecordTypeHTTPS` with `HTTPSData` struct to
  provider type system
- **ECH Troubleshooting**: FAQ entry for Firefox/Chrome ECH connection failures

### Documentation
- **Companion HTTPS Guide**: Full section in Technitium provider docs with
  Why/What/Behavior/Configuration subsections
- **Split-Horizon Tip**: Added companion HTTPS recommendation to split-horizon
  deployment guide
- **Config Example**: Added `auto_https_records` and `auto_https_alpn` to
  example configuration file

## [1.0.1] - 2026-03-30

### Documentation
- **Platform Parity**: Comprehensive docs refresh positioning Docker and
  Kubernetes as co-equal first-class platforms across all pages
- **Landing Page Rewrite**: Tabbed quick start (Docker Compose, Helm, Kustomize),
  updated flowchart with Kubernetes sources, comparison table vs external-dns
- **Getting Started Overhaul**: Dual Docker/K8s paths with tabbed prerequisites,
  installation, configuration, secrets, and verification steps
- **Secrets Management**: Renamed from "Docker Secrets"; added Kubernetes Secrets
  section (secretKeyRef, volume mounts, external secret operators)
- **Observability**: Added Kubernetes monitoring section (ServiceMonitor, pod
  probes, Helm chart integration)
- **FAQ Expansion**: Added Kubernetes-specific entries (RBAC, namespace filtering,
  dual-platform configuration, troubleshooting)
- **Development Guide**: Added Kubernetes dev workflow (kind cluster, local testing)
- **RFC 5737 IPs**: Standardized all example IPs from private ranges to
  documentation-reserved ranges (192.0.2.x, 198.51.100.x) across 30+ files
- **SEO Improvements**: Updated site metadata, page titles, and descriptions for
  better discoverability of Kubernetes-related searches

## [1.0.0] - 2026-03-25

### Added
- **Config Validation CLI**: `--validate` flag for pre-flight config checks with
  structured error reporting (#71)
- **Graceful Shutdown**: In-flight reconciliation tracking with clean provider
  teardown (#69)
- **Structured Logging**: `slog`-based logging with JSON/text format and file
  rotation support (#67)
- **`dnsweaver.hostnames` Label**: Comma-separated hostname declarations for
  Docker containers (#96)
- **Dual-Stack DNS Guide**: Deployment documentation for IPv4/IPv6 environments
- **Source/Watcher Metrics**: Prometheus instrumentation for source discovery and
  watcher activity (#97)

### Changed
- **Pi-hole Config**: `MODE` renamed to `ACCESS_MODE`; `DNSWEAVER_SOURCE` (singular)
  deprecated in favor of `DNSWEAVER_SOURCES` (plural) (#93)
- **`instance_id` Restructured**: Moved to top-level config field (#93)

### Fixed
- **Orphan Cleanup**: Hostname-provider mapping tracked correctly during provider
  switches (#51)
- **Startup Race**: Events during initial reconciliation no longer lost (#55)
- **Health Recovery**: Health checker registered for recovered providers (#127)
- **Pi-hole Default**: `ACCESS_MODE` defaults to `api` when not specified (#98)

### Security
- **Pre-v1.0 Security Audit**: Comprehensive hardening including HTTP response
  body limits, shell metacharacter validation, SSH credential handling, and input
  sanitization (#36)
- **Code Review**: Dead code removal, error handling improvements, naming
  consistency (#94)

### Testing
- **Shared Test Harness**: Mock infrastructure and helper utilities for provider
  testing (#111)
- **Reconciler Edge Cases**: Failure, behavior, and observability tests (#77)
- **RFC 2136 Integration Tests**: Full CRUD lifecycle testing (#130)
- **Standardized Test Templates**: Reusable integration testing frameworks (#136)
- **Integration Tested**: Verified against Technitium DNS and Cloudflare in
  multi-provider E2E scenarios

### Documentation
- **Architecture Overview**: System design and component interaction docs (#35)
- **Multi-Instance Guide**: Running multiple dnsweaver instances (#35)
- **SSH Remote Management**: dnsmasq provider SSH docs and secrets guide (#99)
- **Test Case Matrix**: Release checklist and test coverage mapping (#109)
- **Provider Documentation**: Accuracy corrections across all providers

## [0.9.3] - 2026-03-11

### Fixed
- **Source Registry**: `dnsweaver.enabled=false` (Docker) and `dnsweaver.dev/enabled=false`
  (K8s) now opt out of ALL sources at the registry level, not just the dnsweaver native
  source. Previously the traefik source still extracted hostnames from disabled workloads.
  Fixes [#75](https://github.com/maxfield-allison/dnsweaver/issues/75),
  [#152](https://github.com/maxfield-allison/dnsweaver/-/issues/152).
- **Helm Chart**: Bumped appVersion to 0.9.3

## [0.9.2] - 2026-03-11

### Fixed
- **Critical Correctness** (#148):
  - `parseIntEnv` replaced with `strconv.Atoi` to prevent silent integer overflow on 32-bit platforms
  - `formatSRVKey` uses `fmt.Sprintf` instead of `string(rune(int))` which produced Unicode characters instead of numeric keys
  - Thread-safe DNS record `Catalog` — all public methods now protected by `sync.Mutex`
  - Enum validation for `log_level`, `log_format`, and `docker_mode` config fields
  - `RemoveHostname` normalizes hostname before operations (RFC 1035)
  - A record validation rejects IPv6 addresses (must use AAAA)
  - `GetExistingRecords` uses case-insensitive comparison per RFC 1035
- **Concurrency & Safety** (#149):
  - `_FILE` secret read failure is now a hard error — no longer silently falls through to direct env var
  - Signal handler registered early (before initialization) so SIGINT/SIGTERM during startup triggers graceful shutdown
  - Reconciliation concurrency guard — `TryLock` prevents overlapping reconciliation from timer + events
  - `SetEnabled`/`SetDryRun` use `atomic.Bool` for thread-safe access from concurrent goroutines
  - Mass deletion circuit breaker — orphan cleanup aborts if >50% of known hostnames would be deleted
  - Kubernetes `AddEventHandler` errors now propagated (previously logged and silently dropped)
  - `sync.WaitGroup` ensures periodic reconciliation goroutine completes during shutdown
- **Provider & Security Hardening** (#150):
  - dnsmasq `GetServer()` uses `net.SplitHostPort` — correct IPv6 address parsing
  - Domain matcher strips trailing dots before comparison for consistent matching
  - Pi-hole v6 URL path values escaped with `url.PathEscape` to prevent injection
  - dnsmasq reload command validated against shell metacharacters
  - SSH `RunWithSudo` pipes password via stdin instead of `echo` (no `/proc` exposure)
  - HTTP response bodies capped at 10 MB via `httputil.ReadBody` across all providers
- **Documentation Alignment** (#151):
  - Added dnsmasq provider example to `config.example.yml`
  - Documented `_FILE` suffix support for `TSIG_SECRET` and `SSH_PASSWORD`
  - Fixed `DNSWEAVER_SOURCE` (singular) → `DNSWEAVER_SOURCES` (plural) in K8s deployment docs
  - Documented Kubernetes source auto-registration behavior
  - `record-type` annotation and K8s source doc consistently list A, AAAA, CNAME, SRV, TXT
- **Helm Chart**: Bumped appVersion to 0.9.2

## [0.9.1] - 2026-03-09

### Fixed
- **Documentation**: Comprehensive documentation review and corrections
  - Fixed incorrect env var `TLS_SKIP_VERIFY` → `INSECURE_SKIP_VERIFY` in FAQ
  - Added `rfc2136` to provider TYPE list in environment variable reference
  - Added missing per-instance env vars to reference: `MODE`, `EXCLUDE_DOMAINS_REGEX`,
    `INSECURE_SKIP_VERIFY`
  - Added `Operational Modes` page (`modes.md`) to mkdocs navigation
  - Updated Go version from 1.24+ to 1.25+ in contributing guide
  - Updated project structure in contributing guide (added `sources/`, `internal/kubernetes/`,
    `pkg/workload/`, `pkg/sshutil/`, `providers/rfc2136/`; removed obsolete `internal/sources/`)
  - Added 11 missing CHANGELOG version comparison links (v0.3.1–v0.7.0)
  - Used standard documentation IPs in Kubernetes source docs and code examples
  - Fixed placeholder GitLab social link in mkdocs.yml
  - Added `instance_id` field and RFC 2136 provider example to config.example.yml
- **Helm Chart**: Bumped chart version to 0.2.0 and appVersion to 0.9.1

## [0.9.0] - 2026-03-08

### Added
- **Kubernetes Platform Support**: Full Kubernetes-native DNS management
  - K8s watcher with informer-based event watching (#138) — real-time detection
    of Ingress, IngressRoute (Traefik CRD), HTTPRoute (Gateway API), and Service resources
  - K8s source with annotation-driven configuration (#139) — hostnames, provider hints,
    TTL, proxied, and metadata via `dnsweaver.dev/*` annotations
  - Deployment manifests: Helm chart, Kustomize base, and raw RBAC manifests (#140)
  - Comprehensive Kubernetes deployment and source documentation
  - Platform selector (`DNSWEAVER_PLATFORM`) — run in `docker`, `kubernetes`, or `both` mode
- **Per-Record Metadata System** (#141): Extensible key-value metadata on DNS records
  - `Metadata map[string]string` field on `Record` and `RecordHints` (Phase 2)
  - Cloudflare per-record proxied control via `Record.Metadata["proxied"]` (Phase 3)
  - Source-level proxied field and `meta.*` label parsing in dnsweaver source (Phase 4)
  - Metadata persistence in ownership TXT records (Phase 5)
  - Metadata recovery from ownership TXT on startup for reconciliation (Phase 6)
- **Workload Abstraction** (#137): Platform-agnostic workload interface replacing
  Docker-specific container/service types — enables multi-platform source support

### Changed
- **Cloudflare proxied default**: Changed from `false` to `true` to match Cloudflare's
  own default behavior — new records are proxied unless explicitly disabled

### Fixed
- **CI/CD**: Bumped Go version in CI pipeline and Dockerfile to 1.25 to match `go.mod`

## [0.8.1] - 2026-02-27

### Fixed
- **Cloudflare Provider**: Fix JSON parsing failure on API responses — Cloudflare
  changed `messages` field from `[]string` to `[]object` (matching `errors` shape),
  causing `json: cannot unmarshal object into Go struct field` on every API call

## [0.8.0] - 2026-02-27

### Added
- **RFC 2136 Dynamic DNS Provider** (#132): Industry-standard DNS update protocol support
  - Works with BIND, Windows DNS, PowerDNS, Knot DNS, Technitium, and any RFC 2136-compliant server
  - TSIG authentication with HMAC-SHA256/SHA512 support
  - Catalog-based hostname enumeration via `_dnsweaver-catalog-N.<zone>` TXT records
  - Per-record ownership verification via `_dnsweaver.<hostname>` TXT records
  - Supports A, AAAA, CNAME, and SRV record types
  - No AXFR required — catalog provides efficient O(n) enumeration
- **Multi-Instance Coordination** (#84): Instance-scoped ownership for shared DNS zones
  - New `DNSWEAVER_INSTANCE_ID` config (env var + YAML) identifies each instance
  - Ownership TXT records now carry instance ID: `heritage=dnsweaver,instance=<id>`
  - Each instance only manages its own records — no cross-instance interference
  - Orphan cleanup is instance-scoped: removing a service only cleans that instance's records
  - Fully backward compatible: empty instance ID preserves legacy single-instance behavior
  - Enables multiple dnsweaver deployments (e.g., per-node Docker, Pi clusters) sharing a zone

## [0.7.0] - 2026-01-19

### Added
- **Pi-hole v6 API Support** (#74): Full support for Pi-hole v6 REST API
  - Automatic version detection probes Pi-hole to determine API version
  - Session-based authentication with SID tokens for v6
  - Supports `dns.hosts` and `dns.cnameRecords` config endpoints
  - New `API_VERSION` config option to override auto-detection (`v5`/`v6`/`auto`)
  - Maintains full backward compatibility with Pi-hole v5 legacy API
- **Graceful Provider Initialization** (#125): Resilient startup when providers are unavailable
  - Providers that fail to connect at startup are retried in the background
  - dnsweaver starts in "degraded" mode and becomes healthy once providers recover
  - Configurable retry interval and max attempts
  - Ping verification ensures provider connectivity before marking ready
- **Shared SSH/SFTP Client Package** (#120): New `pkg/sshutil` for remote provider operations
  - SSH client with connection pooling and keepalive
  - SFTP-based FileSystem interface for remote file operations
  - SSH exec-based CommandRunner for remote commands
  - Configuration loading with Docker secrets support (`_FILE` pattern)
  - Foundation for remote dnsmasq, BIND, and hosts-file providers

### Fixed
- **Pi-hole v6 version detection** (#126): Use correct `/api/info/login` endpoint
  - Pi-hole v6 does not have `/api/info` endpoint (returns 404)
  - Changed to probe `/api/info/login` which is unauthenticated and confirms v6
- **Pi-hole v6 List() returns 0 records** (#103): Correct API response parsing
  - Pi-hole v6 `/api/config/dns` returns hosts nested under `config.dns`, not `config`
  - Fixed parsing to handle `{ "config": { "dns": { "hosts": [...] } } }` structure
  - Resolves "Item already present" errors when records already existed

## [0.6.0] - 2026-01-15

### Added
- **MkDocs Documentation Site** (#65): Comprehensive documentation with Material theme
  - Full API reference, configuration guides, and provider documentation
  - Searchable, mobile-friendly documentation hosted via GitHub Pages
  - Includes examples, quickstart guides, and architecture overview

### Changed
- **HTTP Client Consolidation** (#92): Refactored HTTP client logic into shared `pkg/httputil` package
  - All providers now use consistent HTTP client configuration
  - Centralized TLS settings, timeouts, and error handling
  - Added comprehensive test coverage for HTTP client behavior
  - Providers use new factory pattern for cleaner initialization

## [0.5.3] - 2026-01-15

### Fixed
- **INSECURE_SKIP_VERIFY env var not working** (#95, GitHub #74): Fixed environment variable not being loaded
  - `DNSWEAVER_{INSTANCE}_INSECURE_SKIP_VERIFY=true` was silently ignored due to missing field in config loader
  - Environment variable now correctly propagates to the HTTP client TLS configuration
  - Thanks to @jaykumar2001 for reporting this issue

## [0.5.2] - 2026-01-14

### Added
- **INSECURE_SKIP_VERIFY for Technitium** (#86): Skip TLS certificate verification for self-signed certs
  - Configure via `DNSWEAVER_{INSTANCE}_INSECURE_SKIP_VERIFY=true`
  - Enables connections to HTTPS endpoints using IP addresses or self-signed certificates
  - Logs security warning when enabled
  - HTTP client consolidation planned in #92

### Fixed
- **dnsweaver.enabled=false label ignored** (#89): Services with `dnsweaver.enabled=false` now correctly skip record creation
  - Global `dnsweaver.enabled=false` prevents all record creation for the workload
  - Per-record `dnsweaver.records.<name>.enabled=false` disables specific named records
- **dnsweaver.ttl label ignored for simple hostname** (#90): TTL override now works in simple hostname mode
  - `dnsweaver.ttl=60` now correctly sets TTL when using `dnsweaver.hostname`
  - Previously only worked with named records (`dnsweaver.records.<name>.ttl`)

## [0.5.1] - 2026-01-13

### Added
- **Environment Variable Override for YAML Configs** (#67): Inject secrets into YAML-based provider configs
  - Provider-specific env vars override YAML config values: `DNSWEAVER_{PROVIDER}_{FIELD}`
  - Secret fields support `_FILE` suffix for Docker/Kubernetes secrets: `DNSWEAVER_{PROVIDER}_TOKEN_FILE`
  - Secret fields: TOKEN, API_KEY, AUTH_TOKEN, PASSWORD
  - Non-secret fields: URL, ZONE, ZONE_ID, API_EMAIL, TARGET, TTL, MODE
  - Allows YAML configs to be version-controlled safely without secrets
  - See `docs/examples/` for configuration and deployment examples
- **Reorganized Example Documentation**: Moved examples to `docs/examples/` folder
  - `config.example.yml` - Complete YAML configuration reference
  - `docker-compose.dev.example.yml` - Local development setup
  - `docker-stack.example.yml` - Production Swarm deployment
  - `docker-stack-testing.example.yml` - Testing stack with Docker secrets
  - `docker-entrypoint.sh` - Entrypoint wrapper for config templating

### Changed
- Refactored `loadInstanceConfig` to use shared `providerConfigFields` for consistency

## [0.5.0] - 2026-01-13

### Added
- **YAML Configuration File Support** (#66): Full YAML config file support
  - Load configuration from YAML file via `DNSWEAVER_CONFIG` env var or `--config` flag
  - Environment variable interpolation with `${VAR}` and `${VAR:-default}` syntax
  - Configuration priority: env vars > config file > defaults
  - Example config file at `docs/config.example.yml`
  - Supports all existing configuration options in structured YAML format
- **Version Flag**: Added `--version` flag to display version and build date
- **Provider Capabilities Interface** (#79): Providers report their capabilities
  - `SupportsOwnershipTXT` — whether provider can create TXT records for ownership
  - `SupportsNativeUpdate` — whether provider implements `Updater` interface
  - `SupportedRecordTypes` — list of record types the provider handles (A, AAAA, CNAME, SRV)
- **Updater Interface** (#70): Optional provider interface for native record updates
  - Providers implementing `Updater` can update records in-place without delete+create
  - Reconciler automatically falls back to delete+create for providers without native update
  - Technitium provider implements native update support
- **Per-Instance Operational Modes** (#80): Control how dnsweaver manages records per provider
  - `managed` (default) — only touch records dnsweaver created (with ownership TXT)
  - `authoritative` — full control over configured scope; deletes unmatched in-scope records
  - `additive` — write-only mode; never deletes any records
  - Configure via `DNSWEAVER_{INSTANCE}_MODE` environment variable
- **Comprehensive Test Coverage** (#68): Core reconciler coverage increased from 26% to 83%+
  - Added tests for reconciler, watcher, provider registry, and error handling
  - Edge case coverage for debouncing, lifecycle, and event filtering

### Changed
- **Reconciler Refactored** (#78): Split monolithic reconciler into focused modules
  - `reconciler.go` — main loop and orchestration (~300 lines)
  - `actions.go` — create/update/delete operations
  - `comparison.go` — record diffing with `CompareRecordSets()` helper
  - `orphan.go` — orphan detection and cleanup
  - `ownership.go` — TXT record ownership tracking
  - `cache.go` — provider state caching
  - Each module under 400 lines for maintainability

## [0.4.2] - 2026-01-12

### Fixed
- **Lint Compliance**: Resolved all golangci-lint issues for stricter configuration
  - Fixed gofmt formatting across 45 files
  - Fixed exhaustive switch statements (RecordType, Validator interface)
  - Fixed errorlint issues (use `errors.Is` instead of direct comparison)
  - Fixed variable shadowing in dnsmasq/Pi-hole providers
  - Fixed typos (cancelled → canceled)
  - Added status constants for health checks and provider metrics

### Changed
- **Linter Configuration**: Refined `.golangci.yml` for long-term maintainability
  - Disabled `prealloc` (micro-optimization not worth verbosity)
  - Disabled `revive:unexported-return` (intentional API pattern)
  - Added structured exclusions for tests, providers, and config
  - Enabled only diagnostic and performance gocritic tags
- **Contributing Guide**: Fixed internal GitLab URL to public GitHub URL

## [0.4.1] - 2026-01-11

### Added
- **CLEANUP_ON_STOP Option**: New `DNSWEAVER_CLEANUP_ON_STOP` configuration option (default: `true`)
  - When `true` (default): DNS records are deleted when containers stop or are removed
  - When `false`: DNS records are only deleted when containers are removed, not when stopped
  - Useful for containers that frequently stop/start and don't need DNS cleanup on stop
- **Native dnsweaver Labels** (#27): Use dnsweaver without Traefik dependency
  - New label format: `dnsweaver.hostname`, `dnsweaver.type`, `dnsweaver.target`
  - Works alongside existing Traefik label parsing
  - Enables DNS management for services that don't use Traefik
- **Pi-hole Provider** (#15): Native Pi-hole DNS integration with two operation modes
  - **API mode**: Uses Pi-hole's Admin API (recommended for Pi-hole v5)
    - Manages Local DNS Records (A/AAAA) and Local CNAME Records
    - Authentication via admin password (supports `_FILE` suffix for secrets)
  - **File mode**: Direct file manipulation for containerized Pi-hole setups
    - Uses dnsmasq config format internally
    - Configurable config directory, filename, and reload command
  - Supports A, AAAA, and CNAME record types
  - Zone filtering for multi-zone environments
  - **Note**: Pi-hole v6+ uses a different API; see #74 for v6 support
- **dnsmasq Provider** (#28): File-based DNS provider for dnsmasq DNS server
  - Manages records by writing to dnsmasq configuration files
  - Supports `address=` directive for A/AAAA records
  - Supports `cname=` directive for CNAME records
  - Automatic dnsmasq reload after changes (configurable)
  - Serves as foundation for Pi-hole integration
  - Configurable config directory, filename, and reload command
  - **Note**: Orphan cleanup limited due to lack of TXT ownership support; see #73
- **SRV Record Support** (#62): Service discovery DNS records
  - Added `SRV` record type for service discovery (Minecraft, SIP, LDAP, XMPP)
  - SRV records include priority, weight, port, and target fields
  - SRV naming convention: `_service._proto.name` (e.g., `_minecraft._tcp.example.com`)
  - Full support across all providers: Technitium, Cloudflare, Webhook
  - Updated README with SRV record type in reference table
- **AAAA Record Support** (#63): IPv6 DNS record support
  - Added `AAAA` record type for IPv6 addresses alongside existing `A` (IPv4) and `CNAME` types
  - Strict validation: A records require IPv4, AAAA records require IPv6, CNAME requires hostname
  - Full support across all providers: Technitium, Cloudflare, Webhook
  - Updated README with IPv6 configuration examples

### Fixed
- **Cache includes all record types** (#63, #62): Record cache now properly includes AAAA and SRV records
  - Previously, `getExistingRecords()` only cached A and CNAME records
  - SRV and AAAA records were being missed during orphan cleanup
- **Orphan cleanup uses correct record type** (#63, #62): Delete operations now use the actual record type
  - Previously, orphan cleanup always used `A` record type for deletion regardless of actual type
  - Now correctly deletes AAAA records as AAAA and SRV records as SRV
- **SRV record data updates**: Fixed multiple issues with SRV record lifecycle
  - Proper detection of SRV record data changes (priority, weight, port, target)
  - Correct API parameter names for Technitium SRV records
  - SRV data properly passed through reconciler to providers
  - RFC 2782 validation for SRV record hostnames

## [0.3.3] - 2026-01-09

### Added
- **Periodic Reconciliation Timer**: Implemented the missing periodic reconciliation loop
  - Uses `DNSWEAVER_RECONCILE_INTERVAL` setting (default: 60 seconds)
  - Acts as a safety net for any missed Docker events
  - Ensures containers with slow restarts don't get their DNS records deleted prematurely
  - The config value existed since v0.1.0 but the timer was never wired up (oversight in initial implementation)

### Changed
- **Package Structure Refactor** (#61): Moved source implementations to root-level `sources/` directory
  - `pkg/source/traefik/` → `sources/traefik/` for consistency with `providers/` structure
  - Import path changed: `github.com/maxfield-allison/dnsweaver/sources/traefik`
  - Internal interfaces remain in `pkg/source/` (no breaking changes for external consumers)

### Fixed
- **CI: Trivy security scan fails** (#59): Fixed container entrypoint issue
  - The `aquasec/trivy:latest` image has trivy as entrypoint, causing "unknown command sh" error
  - Added explicit entrypoint override in GitLab CI configuration
- **CI: Lint job errors** (#60): Fixed all golangci-lint errors
  - Fixed unchecked error returns in test files (errcheck)
  - Fixed deprecated Docker types: `types.ServiceListOptions` → `swarm.ServiceListOptions` (staticcheck SA1019)
  - Removed unused `printUsage` function and mock types
  - Fixed unnecessary nil check before len() (gosimple S1009)

## [0.3.2] - 2026-01-09

### Fixed
- **DNSWEAVER_ADOPT_EXISTING not working** (#58): Environment variable was parsed but not passed to reconciler
  - The value was correctly loaded from environment but was missing from reconciler config initialization
  - Now `DNSWEAVER_ADOPT_EXISTING=true` works as documented
  - Added `adopt_existing` to startup log for easier debugging
  - Thanks to u/pheitman on Reddit for reporting this bug

## [0.3.1] - 2026-01-09

### Added
- **Hostname Validation** (#49): RFC 1123 hostname validation before DNS operations
  - Validates label length (max 63 chars) and total hostname length (max 253 chars)
  - Checks for valid characters (alphanumeric and hyphens)
  - Rejects empty labels, leading/trailing hyphens, special characters
  - Supports wildcards (`*.example.com`) in first label only
  - Invalid hostnames are logged with warnings and skipped (won't fail reconciliation)
  - New `HostnamesInvalid` counter in reconciliation results
- **Adopt Existing Setting** (#58): Control whether dnsweaver adopts existing DNS records
  - New `DNSWEAVER_ADOPT_EXISTING` environment variable (default: `false`)
  - When false, existing records without ownership TXT are left unmanaged
  - When true, dnsweaver creates ownership TXT to adopt matching records
  - Prevents surprising behavior where dnsweaver silently takes over manually-created records
  - Thanks to u/pheitman on Reddit for testing and feedback on this feature
- **Duplicate Hostname Detection** (#54): Warn when same hostname appears in multiple workloads
  - Logs warning with both workload names when duplicate hostname detected
  - First workload wins (deterministic, alphabetical by service discovery order)
  - New `HostnamesDuplicate` counter in reconciliation results

### Documentation
- **Domain Pattern Overlap** (#52): Documented multi-provider matching behavior
  - Clarified that hostnames are sent to ALL matching providers (split-horizon DNS design)
  - Added examples for non-overlapping patterns using `EXCLUDE_DOMAINS`
  - Documented that instance order doesn't affect provider selection
- **TTL Handling** (#46): Documented TTL configuration and provider-specific behavior
  - Added TTL handling section explaining caching behavior
  - Documented Cloudflare quirks: proxied records use "Automatic" TTL (ignores configured value)
  - Clarified that TTL changes require record deletion/recreation

## [0.3.0] - 2026-01-08

### Added
- **IP Change Detection** (#43, #44): Reconciler now detects when a DNS record exists with a different target
  - Updates records in-place instead of failing with conflict errors
  - Logs `updated record` with old and new target values
  - Handles A→CNAME and CNAME→A type conflicts by deleting and recreating
- **Provider Record Caching**: Cache DNS records per reconciliation cycle
  - Reduces API calls by querying each provider once per cycle
  - Significant performance improvement for large deployments
  - Cache automatically invalidated between reconciliation runs
- **Environment Variable Rename**: `DNSWEAVER_PROVIDERS` → `DNSWEAVER_INSTANCES`
  - Clarifies that instance names are arbitrary identifiers, not provider types
  - Old variable still works with deprecation warning
  - README and examples updated to use new naming

### Fixed
- **Technitium**: Detect "Identical record" response as conflict error (#56)

## [0.2.1] - 2026-01-07

### Fixed
- **CI/CD**: GitHub mirror now preserves commit history instead of force-pushing
  - Clones existing GitHub repo before applying changes
  - Only force-pushes tags (for re-releases), not the main branch
  - New releases now appear as proper commits on top of history

## [0.2.0] - 2026-01-07

### Added
- **Cloudflare DNS Provider**: Public DNS management via Cloudflare API (#24)
  - API token authentication (scoped tokens supported)
  - Zone ID or zone name lookup
  - A and CNAME record support
  - Proxied/unproxied records with `PROXIED` setting
  - Rate limiting awareness
- **Webhook Provider**: Generic webhook for custom DNS integrations (#26)
  - Configurable endpoints for create/delete operations
  - Authentication via custom headers
  - Retry logic with configurable backoff
  - Enables integration with any DNS provider via HTTP API
- **TXT Record Ownership Tracking** (#37): Prevents orphan cleanup from deleting manually-created DNS records
  - Creates `_dnsweaver.{hostname}` TXT records with `heritage=dnsweaver` value
  - Only deletes records during orphan cleanup if ownership TXT record exists
  - Configurable via `DNSWEAVER_OWNERSHIP_TRACKING` (default: true)
  - All providers now support TXT records for ownership markers
- **Ownership State Recovery** (#40): Recover ownership state from DNS on startup
  - Scans all providers for `_dnsweaver.*` TXT records at startup
  - Repopulates known hostnames so orphan cleanup works after restarts
  - No manual intervention needed—dnsweaver remembers what it manages
- **Orphan Cleanup Configuration**: New `DNSWEAVER_CLEANUP_ORPHANS` setting (default: true)
- **Domain Exclusion**: `DNSWEAVER_<PROVIDER>_EXCLUDE_DOMAINS` for excluding domains from a provider

### Fixed
- **Cloudflare**: Return ErrConflict for duplicate records (error codes 81053, 81058)
- **Cloudflare**: Don't proxy TXT records (fixes error 9004)
- **Technitium**: Add required `domain` parameter when listing zone records
- **Reconciler**: Silence warnings when ownership TXT record already exists (expected case)

## [0.1.1] - 2026-01-07

### Added
- **TOML File Support**: Parse Traefik TOML configuration files in addition to YAML (#25)
  - Automatically detects file format by extension (`.toml`, `.yml`, `.yaml`)
  - Default file pattern now includes `*.toml` alongside YAML patterns
  - Mixed YAML/TOML directories fully supported

## [0.1.0] - 2026-01-07

### Added
- **Technitium DNS Provider**: Full implementation with create, update, delete operations
- **Traefik Source**: Extract hostnames from `traefik.http.routers.*.rule` Docker labels
- **Static File Discovery**: Parse Traefik dynamic configuration YAML files for Host rules
- **Multi-Provider Routing**: Route different domains to different DNS providers with glob/regex patterns
- **Split-Horizon DNS**: Support for internal and external records from the same container labels
- **Docker Swarm Support**: Full support for Docker Swarm services alongside standalone containers
- **Socket Proxy Support**: Connect via TCP to Docker socket proxy for improved security
- **Reconciliation Engine**: Periodic full sync ensures DNS records match running containers
- **Event-Driven Updates**: Real-time DNS updates on container start/stop events
- **Health Endpoints**: `/health`, `/ready`, and `/metrics` for monitoring and orchestration
- **Prometheus Metrics**: `dnsweaver_*` metrics for observability
- **Docker Secrets Support**: `_FILE` suffix for all sensitive environment variables
- **Multi-arch Images**: linux/amd64 and linux/arm64 Docker images

### Infrastructure
- Go module: `github.com/maxfield-allison/dnsweaver`
- Minimum Go version: 1.23
- GitLab CI/CD pipeline with GitHub release automation
- Docker Hub and GitHub Container Registry publishing

[Unreleased]: https://github.com/maxfield-allison/dnsweaver/compare/v1.1.4...HEAD
[1.1.4]: https://github.com/maxfield-allison/dnsweaver/compare/v1.1.3...v1.1.4
[1.1.3]: https://github.com/maxfield-allison/dnsweaver/compare/v1.1.2...v1.1.3
[1.1.2]: https://github.com/maxfield-allison/dnsweaver/compare/v1.1.1...v1.1.2
[1.1.1]: https://github.com/maxfield-allison/dnsweaver/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/maxfield-allison/dnsweaver/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/maxfield-allison/dnsweaver/compare/v0.9.3...v1.0.0
[0.9.3]: https://github.com/maxfield-allison/dnsweaver/compare/v0.9.2...v0.9.3
[0.9.2]: https://github.com/maxfield-allison/dnsweaver/compare/v0.9.1...v0.9.2
[0.9.1]: https://github.com/maxfield-allison/dnsweaver/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/maxfield-allison/dnsweaver/compare/v0.8.1...v0.9.0
[0.8.1]: https://github.com/maxfield-allison/dnsweaver/compare/v0.8.0...v0.8.1
[0.8.0]: https://github.com/maxfield-allison/dnsweaver/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/maxfield-allison/dnsweaver/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/maxfield-allison/dnsweaver/compare/v0.5.3...v0.6.0
[0.5.3]: https://github.com/maxfield-allison/dnsweaver/compare/v0.5.2...v0.5.3
[0.5.2]: https://github.com/maxfield-allison/dnsweaver/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/maxfield-allison/dnsweaver/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/maxfield-allison/dnsweaver/compare/v0.4.2...v0.5.0
[0.4.2]: https://github.com/maxfield-allison/dnsweaver/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/maxfield-allison/dnsweaver/compare/v0.3.3...v0.4.1
[0.3.3]: https://github.com/maxfield-allison/dnsweaver/compare/v0.3.2...v0.3.3
[0.3.2]: https://github.com/maxfield-allison/dnsweaver/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/maxfield-allison/dnsweaver/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/maxfield-allison/dnsweaver/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/maxfield-allison/dnsweaver/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/maxfield-allison/dnsweaver/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/maxfield-allison/dnsweaver/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/maxfield-allison/dnsweaver/releases/tag/v0.1.0
