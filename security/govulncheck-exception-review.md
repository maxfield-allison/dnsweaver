# Govulncheck exception review

This record explains the dependency exceptions enforced by [`govulncheck-exceptions.json`](govulncheck-exceptions.json). It is evidence for a bounded decision, not a claim that the affected dependencies are free of vulnerabilities.

## 2026-11-17 technical revalidation

The current release keeps `github.com/docker/docker@v28.5.2+incompatible` and its Docker API behavior. A fresh symbol-level scan with Go 1.26.11 and govulncheck v1.8.0 reported the same five Docker scopes listed below, with no change in module, version, package set or reachability. The initial scan failed because the 2026-11-16 review was due. The module, version, package and reachability fields of all five findings were compared with the existing accepted policy and matched exactly. This is a technical revalidation of those scopes; it does not create a new acceptance or advance its original date.

The dependency update brings the OpenTelemetry trace, metric and SDK cohort to v1.47.0. `golang.org/x/crypto` moves to `v0.58.1-0.20261009181718-2f1f834330b2`, the exact official commit that ends the affected parent-module range for [GO-2026-5932](https://vuln.go.dev/ID/GO-2026-5932.json). Its [entire diff from v0.58.0](https://github.com/golang/crypto/compare/v0.58.0...2f1f834330b2d36ab3db8bfb33aaa1ac16794588) adds only `openpgp/go.mod`; SSH implementation and defaults are unchanged. No v0.58.1 release tag exists at this review. v0.59.0 was deferred because it removes DSA algorithms from defaults and private-key parsing used by dnsweaver's SSH providers. The fresh scan no longer reports GO-2026-5932, so its exception is removed rather than renewed. The obsolete standalone OpenPGP module is not added.

The fresh dependency package closure contains the same 22 Docker client/API packages listed in the historical evidence below and no Docker daemon, plugin or API-server implementation. dnsweaver's Docker operations remain read-only: ping, info, service/container listing and inspection, and event subscription; there is no plugin-install, archive-copy or daemon authorization implementation call.

The current official [plugin privilege advisory](https://github.com/moby/moby/security/advisories/GHSA-pxq6-2prw-chj9) concerns a daemon's privilege comparison during plugin installation. The [authorization advisory](https://github.com/moby/moby/security/advisories/GHSA-x744-4wpc-v9h2) concerns a daemon forwarding oversized request bodies to authorization plugins. The remaining [archive race](https://vuln.go.dev/ID/GO-2026-5617.json), [file creation race](https://vuln.go.dev/ID/GO-2026-5668.json) and [archive extraction](https://vuln.go.dev/ID/GO-2026-5746.json) advisories name daemon functions absent from this dependency closure. The first two advisories retain module-wide Go metadata and the scanner still reports the client packages as symbol-reachable; that ambiguity remains a bounded residual risk, not a fixed vulnerability claim.

A maintained replacement exists: `github.com/moby/moby/client@v0.6.3` and `github.com/moby/moby/api@v1.57.0`. The [split client](https://github.com/moby/moby/releases/tag/client/v0.6.3) raises automatic negotiation's minimum to API 1.40, so it was deferred to a separately evaluated compatibility change. There is no newer release on the legacy Docker module path. This revalidation addresses the existing migration-availability trigger. The maintainer's current release direction is to keep the legacy SDK unless a meaningful user feature warrants bundling a compatibility change; the SDK migration is therefore deferred rather than included merely to remove scanner exceptions.

The policy retains the original 2026-10-15 accepted decision for these five unchanged Docker scopes and its 2026-12-16 hard expiry. Following this 2026-11-17 technical revalidation, the next review is 2026-12-01; all prior scope-change triggers remain. This records maintenance of the existing bounded acceptance and the current instruction to retain the SDK, not a claim that the maintainer issued a new blanket vulnerability acceptance. GO-2026-5932 is removed because the dependency upgrade eliminates its finding.

## Historical scan evidence

The accepted policy was revalidated on 2026-10-15 with:

- Go 1.26.9
- govulncheck v1.8.0
- vulnerability database `https://vuln.go.dev`, last modified `2026-10-08T22:31:09Z`
- symbol-level source mode across `./...`

The completed structured scan contained 309 OSV records and normalized the same six findings as the 2026-09-16 review. The gate matched the following exact scopes:

| Advisory | Module and version | Reachability | Exact package set |
| --- | --- | --- | --- |
| GO-2026-4883 | `github.com/docker/docker@v28.5.2+incompatible` | symbol | Docker client package set below |
| GO-2026-4887 | `github.com/docker/docker@v28.5.2+incompatible` | symbol | Docker client package set below |
| GO-2026-5617 | `github.com/docker/docker@v28.5.2+incompatible` | module | empty |
| GO-2026-5668 | `github.com/docker/docker@v28.5.2+incompatible` | module | empty |
| GO-2026-5746 | `github.com/docker/docker@v28.5.2+incompatible` | module | empty |
| GO-2026-5932 | `golang.org/x/crypto@v0.58.0` | module | empty |

The exact package set for GO-2026-4883 and GO-2026-4887 is:

```text
github.com/docker/docker/api
github.com/docker/docker/api/types
github.com/docker/docker/api/types/blkiodev
github.com/docker/docker/api/types/build
github.com/docker/docker/api/types/checkpoint
github.com/docker/docker/api/types/common
github.com/docker/docker/api/types/container
github.com/docker/docker/api/types/events
github.com/docker/docker/api/types/filters
github.com/docker/docker/api/types/image
github.com/docker/docker/api/types/mount
github.com/docker/docker/api/types/network
github.com/docker/docker/api/types/registry
github.com/docker/docker/api/types/storage
github.com/docker/docker/api/types/strslice
github.com/docker/docker/api/types/swarm
github.com/docker/docker/api/types/swarm/runtime
github.com/docker/docker/api/types/system
github.com/docker/docker/api/types/time
github.com/docker/docker/api/types/versions
github.com/docker/docker/api/types/volume
github.com/docker/docker/client
```

GO-2026-5158 is not excepted. Updating OpenTelemetry to 1.44.0 removed it from the normalized findings. The advisory can still appear in raw OSV metadata because the scanner emits database records that do not all become findings.

## Reachability review

GO-2026-4883 and GO-2026-4887 describe Docker daemon plugin and authorization behavior. dnsweaver uses the Docker client packages listed above. Inspection found no import or call into Docker daemon, plugin, authorization, or server implementations. The scanner still classifies the client package set as symbol-reachable, so the accepted decision retains that metadata ambiguity rather than treating the advisories as fixed.

GO-2026-5617, GO-2026-5668, and GO-2026-5746 were module-only findings. Their affected Docker daemon archive and copy functions are not imported or called by dnsweaver.

GO-2026-5932 was a module-only finding for `golang.org/x/crypto/openpgp`. dnsweaver imports `golang.org/x/crypto/ssh` and `golang.org/x/crypto/ssh/knownhosts`, not an OpenPGP package. Updating `golang.org/x/crypto` from v0.57.0 to v0.58.0 leaves the finding module-only with an empty package set.

## Maintainer decision

The paragraph below records the historical acceptance. The 2026-11-17 technical revalidation above now covers only the five unchanged Docker scopes; the OpenPGP scope was removed, the next review is 2026-12-01, and the hard expiry remains 2026-12-16. No new acceptance date is asserted.


Maintainer Max Allison first accepted these exceptions on 2026-09-16 and renewed the six exact scopes above on 2026-10-15, with `golang.org/x/crypto` at v0.58.0. The next review date is 2026-11-16 and the hard expiry remains 2026-12-16. For a reported finding, a missing accepted decision, due review, expired entry, or mismatch in module, version, package set, or reachability fails the gate.

GO-2026-4883 and GO-2026-4887 require immediate review when any of these conditions occurs:

- advisory or Go vulnerability database symbol metadata changes
- dnsweaver imports Docker daemon, plugin, authorization, or server code
- dnsweaver Docker access becomes mutating
- a supported fixed release on the current Docker module path becomes available
- a stable `github.com/moby/moby/v2` client module becomes available

GO-2026-5617, GO-2026-5668, and GO-2026-5746 require immediate review when any of these conditions occurs:

- dnsweaver imports a Docker daemon package
- dnsweaver adds a Docker copy or archive operation
- advisory or Go vulnerability database scope changes
- a supported fixed release on the current Docker module path becomes available
- a stable `github.com/moby/moby/v2` client module becomes available

GO-2026-5932 requires immediate review when any of these conditions occurs:

- dnsweaver imports any `x/crypto/openpgp` package
- advisory or Go vulnerability database scope changes
- the dependency graph changes package reachability
- a supported fixed `x/crypto` release becomes available

The `evidence_revision` stored with each policy entry is the SHA-256 digest of this file. The gate test recomputes it, so a changed record cannot retain an old evidence identity.

## Reproduction and limitations

Run the policy and scanner checks from the repository root:

```sh
go test ./cmd/govulncheck-gate
./scripts/test-govulncheck-gate.sh
./scripts/govulncheck-gate.sh
```

The first command checks exact scope and decision metadata, due and expired decisions, changed scope, and structured-stream completeness. The fixture script covers clean input, a proposed finding, malformed input, scanner failure containing a finding, and a missing scanner. The final command runs the pinned scanner and writes raw and normalized evidence.

Govulncheck protocol v1.0.0 has no terminal completion record. The gate therefore requires the pinned scanner process to exit successfully and the structured stream to contain valid config and SBOM envelopes. Static reachability and package inspection do not prove that exploitation is impossible. These exceptions remain accepted risk until they are reviewed, expire, or one of the triggers above fires.
