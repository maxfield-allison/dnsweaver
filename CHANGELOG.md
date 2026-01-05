# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial project scaffold
- Project structure with cmd/internal/pkg/providers layout
- Multi-stage Dockerfile with multi-arch support
- GitLab CI/CD pipeline with GitHub release automation
- Provider interface and registry
- Source interface for hostname extraction
- Technitium provider stub
- Health server with `/health` and `/ready` endpoints

### Infrastructure
- Go module: `gitlab.bluewillows.net/root/dnsweaver`
- Minimum Go version: 1.23
- Multi-arch Docker images (amd64 + arm64)

## [0.1.0] - TBD

### Planned
- Complete configuration system with environment variables
- Docker client with Swarm and standalone support
- Traefik source for hostname extraction
- Full Technitium provider implementation
- Reconciler for DNS record synchronization
- Event watcher for real-time updates
- Prometheus metrics

[Unreleased]: https://gitlab.bluewillows.net/root/dnsweaver/-/compare/main...develop
[0.1.0]: https://gitlab.bluewillows.net/root/dnsweaver/-/releases/v0.1.0
