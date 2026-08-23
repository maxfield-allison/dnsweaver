# Release Checklist

Pre-release testing protocol for dnsweaver. Every item must pass before tagging a release.

## Quick Reference

```bash
# Run the full pre-release validation locally
gofmt -w . && gofmt -l .
golangci-lint run ./...
go test ./... -count=1 -race
go build ./...
make test-integration  # Requires test environment
```

---

## 1. Automated CI Checks

These run automatically: GitHub Actions on every pull request (lint, tests, build, govulncheck, CodeQL), and the GitLab tag pipeline again at release time. Verify all pass on `main` before tagging.

| Check | Command | Pass Criteria |
|-------|---------|---------------|
| ☐ Go format | `gofmt -l .` | No output (all files formatted) |
| ☐ Linting | `golangci-lint run ./...` | Zero issues |
| ☐ Unit tests | `go test ./... -count=1` | All pass, zero failures |
| ☐ Race detection | `go test ./... -race` | No data races detected |
| ☐ Build | `go build ./...` | Clean build, no errors |
| ☐ Docker build | `docker build .` | Image builds successfully |
| ☐ SBOM generation | CI artifact | SBOM generated for release |
| ☐ Security scan | `gitleaks detect` | No secret leaks |

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
| ☐ GitHub Release created by the tag pipeline | |
| ☐ SBOM attached to release | |

## 5. Post-Release Verification

| Check | Status |
|-------|--------|
| ☐ Docker image pulls successfully | |
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

GitHub is the origin and the tag is what triggers a release. The `Sync to GitLab`
workflow mirrors `main` and `v*` tags to the GitLab instance, whose tag pipeline
builds the multi-arch images, pushes them to GHCR and Docker Hub, and publishes
the GitHub Release with notes drawn from the changelog. There is no `develop`
branch; everything merges to `main` by pull request.

```bash
# 1. main is clean and the merged PRs were green in CI
git checkout main
git pull origin main
gofmt -l . && golangci-lint run ./... && go test ./... -count=1 -race && go build ./...

# 2. Move the Unreleased section of CHANGELOG.md under a version header with
#    today's date, and merge that change by PR like any other.

# 3. Tag on main and push the tag to GitHub
git tag -a v1.0.0 -m "v1.0.0 - Description of release"
git push origin v1.0.0

# 4. Watch the Sync to GitLab workflow, then the GitLab tag pipeline. When it
#    finishes, the images are published and the GitHub Release exists.
```

## Hotfix Workflow

A hotfix is an ordinary pull request against `main` followed by a patch tag:

```bash
# 1. Branch from main, fix, run the checks above, update CHANGELOG.md, open the PR
git checkout -b fix/short-description main

# 2. After merge, tag the patch release
git checkout main
git pull origin main
git tag -a v1.0.1 -m "v1.0.1 - Hotfix: description"
git push origin v1.0.1
```
