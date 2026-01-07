# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
- Go module: `gitlab.bluewillows.net/root/dnsweaver`
- Minimum Go version: 1.23
- GitLab CI/CD pipeline with GitHub release automation
- Docker Hub and GitHub Container Registry publishing

[Unreleased]: https://github.com/maxfield-allison/dnsweaver/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/maxfield-allison/dnsweaver/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/maxfield-allison/dnsweaver/releases/tag/v0.1.0
