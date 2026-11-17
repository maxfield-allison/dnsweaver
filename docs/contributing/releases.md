# Release procedure

A merged commit, a passing source pipeline, qualified images, a published release and a deployment are separate checkpoints. Do not describe a release as qualified until both architecture jobs have produced the required evidence for its exact source commit.

## Candidate qualification

GitLab builds each candidate once per architecture. The builder and runtime image digests are recorded in `ci/release-inputs.json`; `go.mod` and `go.sum` lock Go dependencies. Release builds also verify the complete installed Alpine package name/version sets against that file. Repository drift fails the build until the lock is reviewed and refreshed. Packages are fetched from the configured repositories, so offline reproducibility is not guaranteed. The resulting image digest and SBOM identify what was actually built. No public job rebuilds it.

The runtime stage bypasses the build cache so package upgrades run for each build, without passing a CI identifier into image history. Each build retains the original manifest and config as private evidence, verifies their digests and candidate labels, and rejects known staging metadata in the config. Assembly and publication recheck those original bytes against qualification. The public provenance records the config digest and metadata check; it does not include the private files. This check covers known markers and does not replace manual disclosure review.

Each native amd64 and arm64 runner checks the embedded version, container healthcheck, YAML/environment port precedence, default loopback listener, explicit network opt-in and aggregate degraded readiness with a provider unavailable at startup. It also verifies the application's actual UID/GID, supplementary groups, graceful shutdown and startup as an explicit non-root user with a read-only root filesystem. Trivy scans each exact digest. HIGH/CRITICAL findings with available fixes block qualification except for the reviewed `.trivyignore` entries; this is not a claim of zero vulnerabilities. The separate Go vulnerability gate retains its own narrower reachability and expiry policy.

Only after both architectures pass does `release:assemble` create an index containing those two digests. It updates the staging registry's `sha-<short-commit>` convenience alias and advances `edge` only for the default branch. These development aliases may move after a rebuild; qualification always uses the immutable digest. Its artifacts contain the private promotion plan and scan reports, plus these public files:

- `provenance.json`: source commit, index and architecture digests, base inputs, runtime checks, scan evidence hashes and SBOM hashes.
- `sbom-amd64.cdx.json` and `sbom-arm64.cdx.json`: CycloneDX inventories with private staging names and optional repository tags removed. Leftover staging tags block qualification; image digests, package versions and license information are retained.
- `SHA256SUMS`: checksums for the three evidence files.

The provenance is an unsigned declared build record. It is not a signature or a SLSA attestation. Signing is a separate policy decision; the pipeline does not imply identity verification that it does not perform. Preserve the private CI artifacts before their 30-day expiry if qualification or publication will be delayed.

## Job admission

| Pipeline input | Source checks | Both image builds and qualification | Public promotion |
|---|---|---|---|
| Main, a supported feature/fix branch or merge request changing Go, Docker, release scripts or input locks | Required | Required | Absent |
| Documentation-only change | Not selected by GitLab source rules | Absent | Absent |
| Stable `vX.Y.Z` tag | Required | Required | Blocking manual job |
| `vX.Y.Z-rc.N` prerelease tag | Required | Required | Blocking manual job; no `latest` updates |
| Other tag | Checks may be admitted; semantic-version validation rejects image qualification | Cannot publish | Absent |

GitHub PR checks also run the nonpublishing release fixtures. GitLab's configuration lint is a separate syntax/dependency check; actual candidate job results establish execution.

## Cut a release

1. Merge the source changes and verify the exact commit on both forges. Investigate failed jobs; a successful mirror only proves the Git push succeeded.
2. Prepare a version/changelog PR with migration instructions. Preview the pending `changes/` fragments with `uv run --no-project --with towncrier==26.9.0 towncrier build --draft --version X.Y.Z`, then build the reviewed entry with the same command without `--draft` and with `--yes`. Review the generated `CHANGELOG.md`, update version references and review the public release notes. Merge and qualify that exact source. The Go version comes from the release tag; preparing notes does not create a release.
3. With maintainer approval, create the public `vX.Y.Z` tag or `vX.Y.Z-rc.N` prerelease tag. The existing mirror starts the GitLab tag pipeline. Tag images report that version; branch candidates report `sha-<commit>`.
4. Verify both architecture results, scans, the assembled index and public evidence. Review the actual manifests and configs, including history, environment and labels, as well as public asset contents for staging names or other private material. Source CI alone does not satisfy this step.
5. With publication approval, run the blocking manual `github:release` job. It requires the existing public tag to resolve to the qualified commit. It copies the qualified index and images to the public version tags, creates or resumes a matching draft, verifies uploaded asset digests and publishes the release. Stable releases then advance eligible public and staging container `vX.Y`, `vX` and `latest` tags according to the container alias contract below. GitHub's latest-release pointer follows the same global stable-version ordering as container `latest`. Prereleases leave every mutable release alias unchanged.
6. Pull the public version by digest from each registry, verify its platforms and version, download and check the release evidence, and read back all intended aliases. Publication does not deploy the service. The optional Docker Hub description sync is a separate manual job after review of its copy.

CI needs registry write credentials for private staging, a GitHub token with release and GHCR publication access, and Docker Hub publication credentials. Credentials are supplied as protected CI variables. Do not put tokens in commands, artifacts, notes or Git remotes. The Docker auth directory is excluded from image context and removed by the CI cleanup step.

## Container alias contract

Consumers may pin immutable `vX.Y.Z` images, a minor stream such as `v3.4`, a major stream such as `v3`, or `latest`. Promotion owns these pointers on GHCR, Docker Hub and the staging registry; changing this policy requires maintainer review. Every pointer copies the same qualified index by digest, without rebuilding.

Stable versions use strict SemVer integer components without leading zeroes. Published, non-draft, non-prerelease GitHub releases establish the ordered set of stable versions. A candidate updates `vX.Y` only when it is the greatest version in that minor stream, `vX` only when greatest in that major stream, and container/GitHub `latest` only when greatest overall. Equal versions permit retries. For example, publishing `v3.3.5` after `v3.4.0` can advance `v3.3` but cannot rewind `v3` or `latest`. Prereleases change no mutable release aliases. Unprefixed major/minor aliases are not published.

Before public writes, promotion inspects every immutable version destination and rejects any conflicting digest or failed inspection, including on a later registry. The final immutable copy checks again to catch a destination changing after inspection. The private publication journal records each eligible full registry alias and its previous digest independently in `previous_aliases`; JSON `null` means a confirmed missing manifest, never a failed read. Authorization, transport and unexpected inspection failures stop publication. When `latest` is eligible, existing container `latest` pointers and GitHub's latest-release ID remain required rollback targets; initial bootstrap is a separate operator action. Journal phases are `prepared`, `release-published`, `complete`, and `latest-update-failed` (the existing failure-phase name is retained for compatibility). Failure records `rollback_failures` and `retained_new_aliases` as full references. Promotion attempts to restore preexisting pointers after an ambiguous failed write and records any failed rollback. Each compensation records ordinary failures, including malformed protocol read-back, and continues restoring the other pointers; the original promotion error is preserved. A first-time alias cannot be removed safely by digest because it shares immutable version content: after a failed promotion it may remain pointing at the published candidate, and the journal names every such attempted alias for read-back. Retry the same qualified candidate and inspect the journal before recovery.

## Failure, retry and rollback

Publication is serialized by the `public-release` resource group. Keep that serialization when performing operator recovery. Never overwrite a conflicting version tag or move a public Git tag to make a failed pipeline pass.

An existing version tag is accepted only when its digest matches the qualified candidate. Existing release notes, prerelease status and assets must also match. A draft with partially uploaded matching evidence can be resumed; a conflicting asset or a published release missing evidence stops the job.

Before any public write, stable publication records every eligible container alias's previous digest (or confirmed absence for a first-time major/minor alias) and, when eligible, GitHub's previous latest-release ID in `release/publication.json`. A missing required `latest` rollback target stops publication. Public version tags can remain after a draft/upload failure, but mutable aliases have not moved. Retry the same candidate and evidence rather than rebuilding or deleting tags.

If advancing a mutable alias or GitHub latest fails, the job attempts to restore every potentially changed preexisting registry pointer and the GitHub pointer, then records any failed compensations and any attempted first-time aliases retained at the candidate. These separate services cannot provide an atomic transaction: interrupted processes or failed rollback calls require operator recovery. Preserve the journal, inspect every recorded pointer, restore the recorded previous digests/ID if needed, and read them back before retrying. The immutable version tags and published release remain available for diagnosis. A rerun after rebuilding different image bytes must fail on those existing tags.

Run the nonpublishing failure fixtures with:

```sh
python3 -m unittest discover -s scripts -p test_release.py -v
```

They exercise mismatched source/digests, interrupted uploads, conflicting tags/assets, major/minor channel ordering, first-time aliases, partial alias updates, rollback and retry. Fixtures are not proof of registry permissions or successful production publication; the post-publication read-back remains required.
