# Release procedure

A merged commit, a passing source pipeline, qualified images, a published release and a deployment are separate checkpoints. Do not describe a release as qualified until both architecture jobs have produced the required evidence for its exact source commit.

## Candidate qualification

GitLab builds each candidate once per architecture. The builder and runtime image digests are recorded in `ci/release-inputs.json`; `go.mod` and `go.sum` lock Go dependencies. Release builds also verify the complete installed Alpine package name/version sets against that file. Repository drift fails the build until the lock is reviewed and refreshed. Packages are fetched from the configured repositories, so offline reproducibility is not guaranteed. The resulting image digest and SBOM identify what was actually built. No public job rebuilds it.

Each native amd64 and arm64 runner checks the embedded version, container healthcheck, YAML/environment port precedence, default loopback listener, explicit network opt-in and aggregate degraded readiness with a provider unavailable at startup. Trivy scans each exact digest. HIGH/CRITICAL findings with available fixes block qualification except for the reviewed `.trivyignore` entries; this is not a claim of zero vulnerabilities. The separate Go vulnerability gate retains its own narrower reachability and expiry policy.

Only after both architectures pass does `release:assemble` create an index containing those two digests. It updates the staging registry's `sha-<short-commit>` convenience alias and advances `edge` only for the default branch. These development aliases may move after a rebuild; qualification always uses the immutable digest. Its artifacts contain the private promotion plan and scan reports, plus these public files:

- `provenance.json`: source commit, index and architecture digests, base inputs, runtime checks, scan evidence hashes and SBOM hashes.
- `sbom-amd64.cdx.json` and `sbom-arm64.cdx.json`: CycloneDX inventories with private staging names removed.
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
2. Prepare a version/changelog PR with migration instructions. Move the intended entries out of `Unreleased`, update version references and review the public release notes. Merge and qualify that exact source.
3. With maintainer approval, create the public `vX.Y.Z` tag or `vX.Y.Z-rc.N` prerelease tag. The existing mirror starts the GitLab tag pipeline. Tag images report that version; branch candidates report `sha-<commit>`.
4. Verify both architecture results, scans, the assembled index and public evidence. Review the public asset contents for staging names or other private material. Source CI alone does not satisfy this step.
5. With publication approval, run the blocking manual `github:release` job. It requires the existing public tag to resolve to the qualified commit. It copies the qualified index and images to the public version tags, creates or resumes a matching draft, verifies uploaded asset digests and publishes the release. Stable releases then advance the public and staging container `latest` tags and GitHub's latest-release pointer. Prereleases leave every `latest` pointer unchanged.
6. Pull the public version by digest from each registry, verify its platforms and version, download and check the release evidence, and read back all intended aliases. Publication does not deploy the service. The optional Docker Hub description sync is a separate manual job after review of its copy.

CI needs registry write credentials for private staging, a GitHub token with release and GHCR publication access, and Docker Hub publication credentials. Credentials are supplied as protected CI variables. Do not put tokens in commands, artifacts, notes or Git remotes. The Docker auth directory is excluded from image context and removed by the CI cleanup step.

## Failure, retry and rollback

Publication is serialized by the `public-release` resource group. Keep that serialization when performing operator recovery. Never overwrite a conflicting version tag or move a public Git tag to make a failed pipeline pass.

An existing version tag is accepted only when its digest matches the qualified candidate. Existing release notes, prerelease status and assets must also match. A draft with partially uploaded matching evidence can be resumed; a conflicting asset or a published release missing evidence stops the job.

Before any public write, stable publication records the previous container `latest` digests and GitHub's previous latest-release ID in `release/publication.json`. Missing rollback targets stop publication. Public version tags can remain after a draft/upload failure, but `latest` has not moved. Retry the same candidate and evidence rather than rebuilding or deleting tags.

If advancing `latest` fails, the job attempts to restore every potentially changed registry pointer and the GitHub pointer, then records any failed compensations. These separate services cannot provide an atomic transaction: interrupted processes or failed rollback calls require operator recovery. Preserve the journal, inspect every recorded pointer, restore the recorded previous digests/ID if needed, and read them back before retrying. The immutable version tags and published release remain available for diagnosis. A rerun after rebuilding different image bytes must fail on those existing tags.

Run the nonpublishing failure fixtures with:

```sh
python3 -m unittest discover -s scripts -p test_release.py -v
```

They exercise mismatched source/digests, interrupted uploads, conflicting tags/assets, partial `latest` updates, rollback and retry. Fixtures are not proof of registry permissions or successful production publication; the post-publication read-back remains required.
