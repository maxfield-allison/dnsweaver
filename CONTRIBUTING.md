# Contributing to DNSWeaver

Thank you for your interest in contributing to DNSWeaver!

## Development Setup

### Prerequisites

- Go 1.24 or later
- Docker (for testing)
- Access to a Docker Swarm cluster (optional, for integration testing)

### Getting Started

1. Clone the repository:
   ```bash
   git clone https://github.com/maxfield-allison/dnsweaver.git
   cd dnsweaver
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
   go test -v ./...
   ```

## Branching Strategy

We follow GitFlow:

| Branch | Purpose |
|--------|---------|
| `main` | Stable releases only (tagged) |
| `develop` | Integration branch |
| `feature/*` | New features |
| `bugfix/*` | Bug fixes |
| `hotfix/*` | Urgent production fixes |

### Branch Naming

```
feature/[issue-number]-short-description
bugfix/[issue-number]-short-description
hotfix/[issue-number]-short-description
```

Example: `feature/21-multi-provider-design`

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

Examples:
```
feat(provider): add Cloudflare DNS provider
fix(reconciler): handle empty hostname list
docs: update configuration reference
```

## Code Style

- Use `gofmt` for formatting
- Use `golangci-lint` for linting
- Follow [Effective Go](https://golang.org/doc/effective_go)
- Add comments for exported functions, types, and packages

## Testing

- Write table-driven tests
- Test behavior, not implementation
- Aim for meaningful coverage, not 100%
- Mock external dependencies (Docker, DNS APIs)

## Pull Request Process

1. Create a feature branch from `develop`
2. Make your changes with tests
3. Ensure CI passes
4. Create a merge request to `develop`
5. Address review feedback
6. Squash and merge

## Questions?

Open an issue or reach out to the maintainers.
