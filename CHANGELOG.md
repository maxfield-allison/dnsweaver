# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.5.3] - 2026-01-15

### Fixed
- **INSECURE_SKIP_VERIFY env var not working** (#95, GitHub #74): Fixed environment variable not being loaded
  - `DNSWEAVER_{INSTANCE}_INSECURE_SKIP_VERIFY=true` was silently ignored due to missing field in config loader
  - Environment variable now correctly propagates to the HTTP client TLS configuration
  - Thanks to @jaykumar2001 for reporting this issue

## [0.5.2] - 2026-01-14

### Added
- **INSECURE_SKIP_VERIFY for Technitium** (#86): Skip TLS certificate verification for self-signed certs
  - Configure via `DNSWEAVER_{INSTANCE}_INSECURE_SKIP_VERIFY=true`
  - Enables connections to HTTPS endpoints using IP addresses or self-signed certificates
  - Logs security warning when enabled
  - HTTP client consolidation planned in #92

### Fixed
- **dnsweaver.enabled=false label ignored** (#89): Services with `dnsweaver.enabled=false` now correctly skip record creation
  - Global `dnsweaver.enabled=false` prevents all record creation for the workload
  - Per-record `dnsweaver.records.<name>.enabled=false` disables specific named records
- **dnsweaver.ttl label ignored for simple hostname** (#90): TTL override now works in simple hostname mode
  - `dnsweaver.ttl=60` now correctly sets TTL when using `dnsweaver.hostname`
  - Previously only worked with named records (`dnsweaver.records.<name>.ttl`)

## [0.5.1] - 2026-01-13

### Added
- **Environment Variable Override for YAML Configs** (#67): Inject secrets into YAML-based provider configs
  - Provider-specific env vars override YAML config values: `DNSWEAVER_{PROVIDER}_{FIELD}`
  - Secret fields support `_FILE` suffix for Docker/Kubernetes secrets: `DNSWEAVER_{PROVIDER}_TOKEN_FILE`
  - Secret fields: TOKEN, API_KEY, AUTH_TOKEN, PASSWORD
  - Non-secret fields: URL, ZONE, ZONE_ID, API_EMAIL, TARGET, TTL, MODE
  - Allows YAML configs to be version-controlled safely without secrets
  - See `docs/examples/` for configuration and deployment examples
- **Reorganized Example Documentation**: Moved examples to `docs/examples/` folder
  - `config.example.yml` - Complete YAML configuration reference
  - `docker-compose.dev.example.yml` - Local development setup
  - `docker-stack.example.yml` - Production Swarm deployment
  - `docker-stack-testing.example.yml` - Testing stack with Docker secrets
  - `docker-entrypoint.sh` - Entrypoint wrapper for config templating

### Changed
- Refactored `loadInstanceConfig` to use shared `providerConfigFields` for consistency

## [0.5.0] - 2026-01-13

### Added
- **YAML Configuration File Support** (#66): Full YAML config file support
  - Load configuration from YAML file via `DNSWEAVER_CONFIG` env var or `--config` flag
  - Environment variable interpolation with `${VAR}` and `${VAR:-default}` syntax
  - Configuration priority: env vars > config file > defaults
  - Example config file at `docs/config.example.yml`
  - Supports all existing configuration options in structured YAML format
- **Version Flag**: Added `--version` flag to display version and build date
- **Provider Capabilities Interface** (#79): Providers report their capabilities
  - `SupportsOwnershipTXT` — whether provider can create TXT records for ownership
  - `SupportsNativeUpdate` — whether provider implements `Updater` interface
  - `SupportedRecordTypes` — list of record types the provider handles (A, AAAA, CNAME, SRV)
- **Updater Interface** (#70): Optional provider interface for native record updates
  - Providers implementing `Updater` can update records in-place without delete+create
  - Reconciler automatically falls back to delete+create for providers without native update
  - Technitium provider implements native update support
- **Per-Instance Operational Modes** (#80): Control how dnsweaver manages records per provider
  - `managed` (default) — only touch records dnsweaver created (with ownership TXT)
  - `authoritative` — full control over configured scope; deletes unmatched in-scope records
  - `additive` — write-only mode; never deletes any records
  - Configure via `DNSWEAVER_{INSTANCE}_MODE` environment variable
- **Comprehensive Test Coverage** (#68): Core reconciler coverage increased from 26% to 83%+
  - Added tests for reconciler, watcher, provider registry, and error handling
  - Edge case coverage for debouncing, lifecycle, and event filtering

### Changed
- **Reconciler Refactored** (#78): Split monolithic reconciler into focused modules
  - `reconciler.go` — main loop and orchestration (~300 lines)
  - `actions.go` — create/update/delete operations
  - `comparison.go` — record diffing with `CompareRecordSets()` helper
  - `orphan.go` — orphan detection and cleanup
  - `ownership.go` — TXT record ownership tracking
  - `cache.go` — provider state caching
  - Each module under 400 lines for maintainability

## [0.4.2] - 2026-01-12

### Fixed
- **Lint Compliance**: Resolved all golangci-lint issues for stricter configuration
  - Fixed gofmt formatting across 45 files
  - Fixed exhaustive switch statements (RecordType, Validator interface)
  - Fixed errorlint issues (use `errors.Is` instead of direct comparison)
  - Fixed variable shadowing in dnsmasq/Pi-hole providers
  - Fixed typos (cancelled → canceled)
  - Added status constants for health checks and provider metrics

### Changed
- **Linter Configuration**: Refined `.golangci.yml` for long-term maintainability
  - Disabled `prealloc` (micro-optimization not worth verbosity)
  - Disabled `revive:unexported-return` (intentional API pattern)
  - Added structured exclusions for tests, providers, and config
  - Enabled only diagnostic and performance gocritic tags
- **Contributing Guide**: Fixed internal GitLab URL to public GitHub URL

## [0.4.1] - 2026-01-11

### Added
- **CLEANUP_ON_STOP Option**: New `DNSWEAVER_CLEANUP_ON_STOP` configuration option (default: `true`)
  - When `true` (default): DNS records are deleted when containers stop or are removed
  - When `false`: DNS records are only deleted when containers are removed, not when stopped
  - Useful for containers that frequently stop/start and don't need DNS cleanup on stop
- **Native dnsweaver Labels** (#27): Use dnsweaver without Traefik dependency
  - New label format: `dnsweaver.hostname`, `dnsweaver.type`, `dnsweaver.target`
  - Works alongside existing Traefik label parsing
  - Enables DNS management for services that don't use Traefik
- **Pi-hole Provider** (#15): Native Pi-hole DNS integration with two operation modes
  - **API mode**: Uses Pi-hole's Admin API (recommended for Pi-hole v5)
    - Manages Local DNS Records (A/AAAA) and Local CNAME Records
    - Authentication via admin password (supports `_FILE` suffix for secrets)
  - **File mode**: Direct file manipulation for containerized Pi-hole setups
    - Uses dnsmasq config format internally
    - Configurable config directory, filename, and reload command
  - Supports A, AAAA, and CNAME record types
  - Zone filtering for multi-zone environments
  - **Note**: Pi-hole v6+ uses a different API; see #74 for v6 support
- **dnsmasq Provider** (#28): File-based DNS provider for dnsmasq DNS server
  - Manages records by writing to dnsmasq configuration files
  - Supports `address=` directive for A/AAAA records
  - Supports `cname=` directive for CNAME records
  - Automatic dnsmasq reload after changes (configurable)
  - Serves as foundation for Pi-hole integration
  - Configurable config directory, filename, and reload command
  - **Note**: Orphan cleanup limited due to lack of TXT ownership support; see #73
- **SRV Record Support** (#62): Service discovery DNS records
  - Added `SRV` record type for service discovery (Minecraft, SIP, LDAP, XMPP)
  - SRV records include priority, weight, port, and target fields
  - SRV naming convention: `_service._proto.name` (e.g., `_minecraft._tcp.example.com`)
  - Full support across all providers: Technitium, Cloudflare, Webhook
  - Updated README with SRV record type in reference table
- **AAAA Record Support** (#63): IPv6 DNS record support
  - Added `AAAA` record type for IPv6 addresses alongside existing `A` (IPv4) and `CNAME` types
  - Strict validation: A records require IPv4, AAAA records require IPv6, CNAME requires hostname
  - Full support across all providers: Technitium, Cloudflare, Webhook
  - Updated README with IPv6 configuration examples

### Fixed
- **Cache includes all record types** (#63, #62): Record cache now properly includes AAAA and SRV records
  - Previously, `getExistingRecords()` only cached A and CNAME records
  - SRV and AAAA records were being missed during orphan cleanup
- **Orphan cleanup uses correct record type** (#63, #62): Delete operations now use the actual record type
  - Previously, orphan cleanup always used `A` record type for deletion regardless of actual type
  - Now correctly deletes AAAA records as AAAA and SRV records as SRV
- **SRV record data updates**: Fixed multiple issues with SRV record lifecycle
  - Proper detection of SRV record data changes (priority, weight, port, target)
  - Correct API parameter names for Technitium SRV records
  - SRV data properly passed through reconciler to providers
  - RFC 2782 validation for SRV record hostnames

## [0.3.3] - 2026-01-09

### Added
- **Periodic Reconciliation Timer**: Implemented the missing periodic reconciliation loop
  - Uses `DNSWEAVER_RECONCILE_INTERVAL` setting (default: 60 seconds)
  - Acts as a safety net for any missed Docker events
  - Ensures containers with slow restarts don't get their DNS records deleted prematurely
  - The config value existed since v0.1.0 but the timer was never wired up (oversight in initial implementation)

### Changed
- **Package Structure Refactor** (#61): Moved source implementations to root-level `sources/` directory
  - `pkg/source/traefik/` → `sources/traefik/` for consistency with `providers/` structure
  - Import path changed: `gitlab.bluewillows.net/root/dnsweaver/sources/traefik`
  - Internal interfaces remain in `pkg/source/` (no breaking changes for external consumers)

### Fixed
- **CI: Trivy security scan fails** (#59): Fixed container entrypoint issue
  - The `aquasec/trivy:latest` image has trivy as entrypoint, causing "unknown command sh" error
  - Added explicit entrypoint override in GitLab CI configuration
- **CI: Lint job errors** (#60): Fixed all golangci-lint errors
  - Fixed unchecked error returns in test files (errcheck)
  - Fixed deprecated Docker types: `types.ServiceListOptions` → `swarm.ServiceListOptions` (staticcheck SA1019)
  - Removed unused `printUsage` function and mock types
  - Fixed unnecessary nil check before len() (gosimple S1009)

## [0.3.2] - 2026-01-09

### Fixed
- **DNSWEAVER_ADOPT_EXISTING not working** (#58): Environment variable was parsed but not passed to reconciler
  - The value was correctly loaded from environment but was missing from reconciler config initialization
  - Now `DNSWEAVER_ADOPT_EXISTING=true` works as documented
  - Added `adopt_existing` to startup log for easier debugging
  - Thanks to u/pheitman on Reddit for reporting this bug

## [0.3.1] - 2026-01-09

### Added
- **Hostname Validation** (#49): RFC 1123 hostname validation before DNS operations
  - Validates label length (max 63 chars) and total hostname length (max 253 chars)
  - Checks for valid characters (alphanumeric and hyphens)
  - Rejects empty labels, leading/trailing hyphens, special characters
  - Supports wildcards (`*.example.com`) in first label only
  - Invalid hostnames are logged with warnings and skipped (won't fail reconciliation)
  - New `HostnamesInvalid` counter in reconciliation results
- **Adopt Existing Setting** (#58): Control whether dnsweaver adopts existing DNS records
  - New `DNSWEAVER_ADOPT_EXISTING` environment variable (default: `false`)
  - When false, existing records without ownership TXT are left unmanaged
  - When true, dnsweaver creates ownership TXT to adopt matching records
  - Prevents surprising behavior where dnsweaver silently takes over manually-created records
  - Thanks to u/pheitman on Reddit for testing and feedback on this feature
- **Duplicate Hostname Detection** (#54): Warn when same hostname appears in multiple workloads
  - Logs warning with both workload names when duplicate hostname detected
  - First workload wins (deterministic, alphabetical by service discovery order)
  - New `HostnamesDuplicate` counter in reconciliation results

### Documentation
- **Domain Pattern Overlap** (#52): Documented multi-provider matching behavior
  - Clarified that hostnames are sent to ALL matching providers (split-horizon DNS design)
  - Added examples for non-overlapping patterns using `EXCLUDE_DOMAINS`
  - Documented that instance order doesn't affect provider selection
- **TTL Handling** (#46): Documented TTL configuration and provider-specific behavior
  - Added TTL handling section explaining caching behavior
  - Documented Cloudflare quirks: proxied records use "Automatic" TTL (ignores configured value)
  - Clarified that TTL changes require record deletion/recreation

## [0.3.0] - 2026-01-08

### Added
- **IP Change Detection** (#43, #44): Reconciler now detects when a DNS record exists with a different target
  - Updates records in-place instead of failing with conflict errors
  - Logs `updated record` with old and new target values
  - Handles A→CNAME and CNAME→A type conflicts by deleting and recreating
- **Provider Record Caching**: Cache DNS records per reconciliation cycle
  - Reduces API calls by querying each provider once per cycle
  - Significant performance improvement for large deployments
  - Cache automatically invalidated between reconciliation runs
- **Environment Variable Rename**: `DNSWEAVER_PROVIDERS` → `DNSWEAVER_INSTANCES`
  - Clarifies that instance names are arbitrary identifiers, not provider types
  - Old variable still works with deprecation warning
  - README and examples updated to use new naming

### Fixed
- **Technitium**: Detect "Identical record" response as conflict error (#56)

## [0.2.1] - 2026-01-07

### Fixed
- **CI/CD**: GitHub mirror now preserves commit history instead of force-pushing
  - Clones existing GitHub repo before applying changes
  - Only force-pushes tags (for re-releases), not the main branch
  - New releases now appear as proper commits on top of history

## [0.2.0] - 2026-01-07

### Added
- **Cloudflare DNS Provider**: Public DNS management via Cloudflare API (#24)
  - API token authentication (scoped tokens supported)
  - Zone ID or zone name lookup
  - A and CNAME record support
  - Proxied/unproxied records with `PROXIED` setting
  - Rate limiting awareness
- **Webhook Provider**: Generic webhook for custom DNS integrations (#26)
  - Configurable endpoints for create/delete operations
  - Authentication via custom headers
  - Retry logic with configurable backoff
  - Enables integration with any DNS provider via HTTP API
- **TXT Record Ownership Tracking** (#37): Prevents orphan cleanup from deleting manually-created DNS records
  - Creates `_dnsweaver.{hostname}` TXT records with `heritage=dnsweaver` value
  - Only deletes records during orphan cleanup if ownership TXT record exists
  - Configurable via `DNSWEAVER_OWNERSHIP_TRACKING` (default: true)
  - All providers now support TXT records for ownership markers
- **Ownership State Recovery** (#40): Recover ownership state from DNS on startup
  - Scans all providers for `_dnsweaver.*` TXT records at startup
  - Repopulates known hostnames so orphan cleanup works after restarts
  - No manual intervention needed—dnsweaver remembers what it manages
- **Orphan Cleanup Configuration**: New `DNSWEAVER_CLEANUP_ORPHANS` setting (default: true)
- **Domain Exclusion**: `DNSWEAVER_<PROVIDER>_EXCLUDE_DOMAINS` for excluding domains from a provider

### Fixed
- **Cloudflare**: Return ErrConflict for duplicate records (error codes 81053, 81058)
- **Cloudflare**: Don't proxy TXT records (fixes error 9004)
- **Technitium**: Add required `domain` parameter when listing zone records
- **Reconciler**: Silence warnings when ownership TXT record already exists (expected case)

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

[Unreleased]: https://github.com/maxfield-allison/dnsweaver/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/maxfield-allison/dnsweaver/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/maxfield-allison/dnsweaver/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/maxfield-allison/dnsweaver/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/maxfield-allison/dnsweaver/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/maxfield-allison/dnsweaver/releases/tag/v0.1.0
