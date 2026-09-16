# Govulncheck exception review

This record explains the dependency exceptions enforced by [`govulncheck-exceptions.json`](govulncheck-exceptions.json). It is evidence for a bounded decision, not a claim that the affected dependencies are free of vulnerabilities.

## Scan evidence

The accepted policy was revalidated on 2026-09-16 with:

- Go 1.26.8
- govulncheck v1.8.0
- vulnerability database `https://vuln.go.dev`, last modified `2026-09-15T18:39:25Z`
- symbol-level source mode across `./...`

The completed structured scan contained 285 OSV records and normalized six findings. The gate matched the following exact scopes:

| Advisory | Module and version | Reachability | Exact package set |
| --- | --- | --- | --- |
| GO-2026-4883 | `github.com/docker/docker@v28.5.2+incompatible` | symbol | Docker client package set below |
| GO-2026-4887 | `github.com/docker/docker@v28.5.2+incompatible` | symbol | Docker client package set below |
| GO-2026-5617 | `github.com/docker/docker@v28.5.2+incompatible` | module | empty |
| GO-2026-5668 | `github.com/docker/docker@v28.5.2+incompatible` | module | empty |
| GO-2026-5746 | `github.com/docker/docker@v28.5.2+incompatible` | module | empty |
| GO-2026-5932 | `golang.org/x/crypto@v0.57.0` | module | empty |

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

GO-2026-5932 was a module-only finding for `golang.org/x/crypto/openpgp`. dnsweaver imports `golang.org/x/crypto/ssh` and `golang.org/x/crypto/ssh/knownhosts`, not an OpenPGP package.

## Maintainer decision

Maintainer Max Allison accepted only the six exact scopes above on 2026-09-16. The next review date is 2026-10-16 and the hard expiry is 2026-12-16. A missing decision, due review, expired entry, different module version, different package set, or changed reachability fails the gate.

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
