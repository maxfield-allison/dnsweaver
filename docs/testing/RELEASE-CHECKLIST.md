# Release Checklist

Release verification for dnsweaver. Complete the source checks before tagging, then qualify the versioned images before publication. Record evidence or an explicit limitation for each applicable item. The [release procedure](../contributing/releases.md) defines the publication and recovery steps.

## Quick Reference

```bash
# Run the full pre-release validation locally
test -z "$(gofmt -l .)"
golangci-lint run --config .golangci.yml
go test -race -count=1 ./...
go build ./...
make test-integration  # Requires test environment
```

---

## 1. Automated CI Checks

GitHub Actions runs source checks on pull requests and main. GitLab also checks qualifying source changes and version tags. Complete source checks on the exact merged commit before tagging. Then complete image qualification and release assembly for the exact tag before publication.

| Check | Command | Pass Criteria |
|-------|---------|---------------|
| ☐ Go format | `gofmt -l .` | No output (all files formatted) |
| ☐ Linting | `golangci-lint run ./...` | Zero issues |
| ☐ Unit tests | `go test ./... -count=1` | All pass, zero failures |
| ☐ Race detection | `go test ./... -race` | No data races detected |
| ☐ Build | `go build ./...` | Clean build, no errors |
| ☐ Docker build | `docker build .` | Image builds successfully |
| ☐ Dependency gate | `./scripts/govulncheck-gate.sh` | Structured scan completes; every finding is fixed or has a current accepted exception |
| ☐ Secret scan | GitLab `security:gitleaks` | Checked-out tree has no detected secret |
| ☐ Native image qualification | GitLab `docker:build:amd64` and `docker:build:arm64` | Each exact digest passes version, health, listener and readiness checks on its native architecture |
| ☐ Image scans | GitLab `security:container:amd64` and `security:container:arm64` | Each qualified digest passes the reviewed HIGH/CRITICAL vulnerability policy and has a matching SBOM |
| ☐ Release assembly | GitLab `release:assemble` | Index contains exactly the two qualified architecture digests; provenance, SBOMs and checksums match |

The publication job promotes the qualified digests without rebuilding. Review the retained evidence for the exact tag and inspect public assets for private data before publication. Provenance is an unsigned declared build record, not a signed attestation. A green branch pipeline does not qualify a later squash commit or a different embedded release version.

## 2. Manual Integration Tests

These require a running test environment with real provider backends.

### Provider Verification

For each provider enabled in the test environment:

| Provider | Create | Update | Delete | Orphan Cleanup | Ownership |
|----------|--------|--------|--------|----------------|-----------|
| ☐ Technitium | ☐ | ☐ | ☐ | ☐ | ☐ |
| ☐ Cloudflare | ☐ | ☐ | ☐ | ☐ | ☐ |
| ☐ RFC 2136 | ☐ | ☐ | ☐ | ☐ | ☐ |
| ☐ Pi-hole v5 | ☐ | ☐ | ☐ | ☐ | ☐ |
| ☐ Pi-hole v6 | ☐ | ☐ | ☐ | ☐ | ☐ |
| ☐ dnsmasq | ☐ | ☐ | ☐ | ☐ | ☐ |
| ☐ Webhook | ☐ | ☐ | ☐ | ☐ | ☐ |

### Source Verification

For each source enabled in the test environment:

| Source | Discovery | Watch Mode | Poll Mode | Multi-Hostname |
|--------|-----------|------------|-----------|----------------|
| ☐ Traefik (Labels) | ☐ | ☐ | ☐ | ☐ |
| ☐ Traefik (File) | ☐ | ☐ | ☐ | ☐ |
| ☐ Kubernetes | ☐ | ☐ | ☐ | ☐ |
| ☐ dnsweaver Native | ☐ | ☐ | ☐ | ☐ |

### Scenario Verification

| Scenario | Status | Notes |
|----------|--------|-------|
| ☐ Service start → DNS record created | | |
| ☐ Service stop → DNS record removed | | |
| ☐ Service hostname change → old removed, new created | | |
| ☐ Service target change → record updated | | |
| ☐ dnsweaver restart → no orphans, no duplicates | | |
| ☐ Provider outage → recovery without data loss | | |

## 3. Documentation Verification

| Item | Status |
|------|--------|
| ☐ CHANGELOG.md updated with all changes since last release | |
| ☐ README.md accurate (badges, features, quick-start) | |
| ☐ Provider docs match current behavior | |
| ☐ Source docs match current behavior | |
| ☐ Configuration reference complete (all env vars documented) | |
| ☐ Docker secrets documentation current | |
| ☐ Deployment examples work with current image | |
| ☐ API version/compatibility documented (if applicable) | |

## 4. Release Artifacts

| Artifact | Status |
|----------|--------|
| ☐ Version bumped in relevant files | |
| ☐ CHANGELOG.md has release date and version header | |
| ☐ Git tag follows SemVer (`vMAJOR.MINOR.PATCH`) | |
| ☐ Docker image tagged and pushed to registry | |
| ☐ Versioned amd64/arm64 images qualified before public promotion | |
| ☐ Explicit publication approval recorded before the manual `github:release` job | |
| ☐ GitHub Release published by the manual job | |
| ☐ Both SBOMs, unsigned provenance and SHA256SUMS attached and verified | |

## 5. Post-Release Verification

| Check | Status |
|-------|--------|
| ☐ Public images pull by digest from GHCR and Docker Hub; both platforms and embedded versions match | |
| ☐ Intended version/latest aliases and GitHub latest-release pointer read back | |
| ☐ Downloaded release assets match SHA256SUMS | |
| ☐ Fresh deployment with example config works | |
| ☐ GitLab pipeline for tag completed successfully | |
| ☐ GitHub Release published, notes match CHANGELOG.md | |

---

## Version Bump Guidelines

| Change Type | Version Bump | Example |
|-------------|--------------|---------|
| Breaking API/config change | MAJOR | Removed env var, changed schema |
| New feature (backward-compatible) | MINOR | New provider, new source |
| Bug fix | PATCH | Fixed crash, corrected behavior |

See the [CHANGELOG](https://github.com/maxfield-allison/dnsweaver/blob/main/CHANGELOG.md) for version history.

## Release Workflow

GitHub is the public source. The `Sync to GitLab` workflow mirrors merged `main` commits and version tags. A tag starts qualification; publication waits for the manual `github:release` job and maintainer approval.

1. Verify the merged commit on both forges and complete its source checks. Use an isolated checkout; preserve any unrelated local work.
2. Prepare and merge a version/changelog PR. Include migration instructions, update the Helm chart's version and default application image tag, and review the release notes. The publication job draws its notes from the versioned changelog section.
3. With approval for the exact commit and version, create and push the public tag. Confirm that the mirror starts the tag pipeline.
4. Inspect both native architecture jobs, image scans, the assembled index, SBOMs, unsigned provenance and checksums. Branch-version images do not substitute for tag-version qualification.
5. With publication approval, run the manual `github:release` job. Follow the [release procedure](../contributing/releases.md) for retry and rollback; never move an existing tag to conceal a mismatch.
6. Verify the public registries, assets and intended aliases. Deployment is a separate action.

## Hotfix Workflow

A hotfix follows the same sequence: merge a focused fix against `main`, prepare the patch version and changelog, qualify the exact tag, then publish with approval. A smaller source change does not remove the artifact verification or post-publication checks.
