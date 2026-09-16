# Contributing to DNSWeaver

Thank you for your interest in contributing to DNSWeaver! Contributions of all
kinds are welcome — bug reports, feature requests, documentation, and code.

DNSWeaver is developed in the open on **GitHub**, which is the source of truth
for the codebase, issues, pull requests, and releases. You only need a GitHub
account to contribute.

## Development Setup

### Prerequisites

- Go 1.26.8 (the supported toolchain patch is pinned in [`go.mod`](go.mod))
- Docker (for building images and integration testing)
- Access to a container orchestrator (optional, for integration testing)

### Getting Started

1. Fork the repository on GitHub, then clone your fork:
   ```bash
   git clone https://github.com/<your-username>/dnsweaver.git
   cd dnsweaver
   git remote add upstream https://github.com/maxfield-allison/dnsweaver.git
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Build:
   ```bash
   go build -o dnsweaver ./cmd/dnsweaver
   ```

4. Run tests:
   ```bash
   go test ./...
   ```

The module path is `github.com/maxfield-allison/dnsweaver`. Import internal
packages using that prefix.

## Branching Strategy

For contributions, DNSWeaver follows **GitHub Flow**: `main` is always
releasable, and all work happens on short-lived branches that are merged back
into `main` via pull request.

| Branch | Purpose |
|--------|---------|
| `main` | Always-releasable trunk; releases are tagged here |
| `feature/*` | New features |
| `bugfix/*` | Bug fixes |
| `hotfix/*` | Urgent fixes |

Branch off `main` and open your pull request against `main`.

### Branch Naming

```
feature/[issue-number]-short-description
bugfix/[issue-number]-short-description
hotfix/[issue-number]-short-description
```

Example: `feature/88-ovh-provider`

## Commit Messages

We use [Conventional Commits](https://www.conventionalcommits.org/):

```
type(scope): description

[optional body]

[optional footer]
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only
- `style`: Formatting, no code change
- `refactor`: Code change that neither fixes a bug nor adds a feature
- `perf`: Performance improvement
- `test`: Adding tests
- `build`: Build system or dependencies
- `ci`: CI/CD changes
- `chore`: Other changes

Breaking changes use a `!` after the type/scope and a `BREAKING CHANGE:` footer.

Examples:
```
feat(provider): add OVH DNS provider
fix(reconciler): handle empty hostname list
refactor!: migrate module path to github.com/maxfield-allison/dnsweaver
```

## Code Style

- Use `gofmt` for formatting
- Use `golangci-lint` for linting (config in [`.golangci.yml`](.golangci.yml))
- Follow [Effective Go](https://golang.org/doc/effective_go)
- Add comments for exported functions, types, and packages

Before opening a pull request, run the same gates CI runs:

```bash
gofmt -l .            # should print nothing
go vet ./...
golangci-lint run
go test -race ./...
go build ./...
```

## Testing

- Write table-driven tests
- Test behavior, not implementation
- Aim for meaningful coverage, not 100%
- Mock external dependencies (Docker, DNS APIs)

## Pull Request Process

1. Fork the repo and create a branch off `main`.
2. Make your changes with tests and run the gates above locally.
3. Push to your fork and open a pull request against `maxfield-allison/dnsweaver:main`.
4. GitHub Actions runs lint, tests, build, and a vulnerability scan
   automatically on your PR — these are free for public repositories.
5. Address review feedback. A maintainer merges once CI is green and the
   change is approved.

### Adding a DNS Provider

New providers implement the `provider.Provider` interface (and
`provider.Updater` if the provider supports updates). See the existing
providers under [`providers/`](providers/) for reference implementations,
and the provider docs under [`docs/providers/`](docs/providers/).

## How Releases Work

You don't need to do anything for releases as a contributor, but for
transparency: once changes are merged to `main`, the maintainer engineers
releases by tagging `vX.Y.Z`. A separate internal build system produces the
multi-arch container images (GHCR and Docker Hub) and publishes the GitHub
Release with notes drawn from the changelog. Versioning follows
[Semantic Versioning](https://semver.org/).

## Questions?

Open an issue on GitHub or start a discussion. Thanks for contributing!
