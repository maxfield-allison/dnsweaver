# Test Cases

Comprehensive test case documentation for dnsweaver integration testing. This document defines the test matrix for every provider, source, and end-to-end scenario that must pass before release.

For test templates and patterns, see the [templates/](templates/) directory.

## Overview

| Category | Test Cases | Description |
|----------|-----------|-------------|
| [Provider Tests](#provider-tests) | 10 providers × record types | CRUD operations, orphan cleanup |
| [Source Tests](#source-tests) | 3 sources × modes | Service discovery, watch/poll |
| [Scenario Tests](#scenario-tests) | 9 scenarios | End-to-end behavior, recovery |
| [Reconciler Tests](#reconciler-tests) | Edge cases | Multi-provider, conflict resolution |

---

## Provider Tests

Each provider must pass the full test matrix for every record type it supports.

### Record Type Support Matrix

| Provider | A | AAAA | CNAME | SRV | TXT | Ownership |
|----------|---|------|-------|-----|-----|-----------|
| Technitium | ✅ | ✅ | ✅ | ✅ | ✅ | TXT record |
| Cloudflare | ✅ | ✅ | ✅ | ✅ | ✅ | TXT record |
| OVHcloud | ✅ | ✅ | ✅ | ✅ | ✅ | TXT record |
| RFC 2136 | ✅ | ✅ | ✅ | ✅ | ✅ | TXT record |
| PowerDNS | ✅ | ✅ | ✅ | ✅ | ✅ | TXT record |
| Pi-hole v5 | ✅ | ✅ | ✅ | ❌ | ❌ | Custom list |
| Pi-hole v6 | ✅ | ✅ | ✅ | ❌ | ❌ | Custom list |
| AdGuard Home | ✅ | ✅ | ✅ | ❌ | ❌ | DNS rewrite |
| OPNsense | ✅ | ✅ | ❌ | ❌ | ❌ | Description prefix |
| dnsmasq | ✅ | ✅ | ✅ | ❌ | ❌ | Config file |
| Webhook | ✅ | ✅ | ✅ | ✅ | ✅ | Callback |

### Per-Provider Test Cases

For each supported record type, every provider must pass these operations:

#### TC-P-001: Create Record

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Discover a new service with matching domain | Record appears in desired state |
| 2 | Reconciler creates the record | Provider API confirms creation |
| 3 | Verify DNS resolution | Record resolves to expected target |

#### TC-P-002: Update Record (Target Change)

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Change service target (e.g., new IP address) | Desired state updates |
| 2 | Reconciler detects drift | Record updated in provider |
| 3 | Verify DNS resolution | Record resolves to new target |

#### TC-P-003: Delete Record

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Remove the source service | Service disappears from desired state |
| 2 | Reconciler identifies orphan | Record deleted from provider |
| 3 | Verify DNS resolution | Record no longer resolves |

#### TC-P-004: Orphan Cleanup

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Start dnsweaver with pre-existing managed records | Provider has stale records |
| 2 | Reconciler runs with current desired state | Orphaned records cleaned up |
| 3 | Only valid records remain | No stale entries in provider |

#### TC-P-005: Ownership Verification

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Manually create a record outside dnsweaver | Record exists without ownership marker |
| 2 | Reconciler encounters unowned record | Record is NOT modified or deleted |
| 3 | Create same hostname via dnsweaver | Ownership marker is set on creation |

#### TC-P-006: Idempotent Apply

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Reconciler creates a record | Record exists in provider |
| 2 | Run reconciler again with same state | No errors, no duplicate records |
| 3 | Provider state unchanged | API calls are minimal (no-op) |

### Provider-Specific Tests

#### Technitium (TC-TECH-xxx)

| ID | Test Case | Notes |
|----|-----------|-------|
| TC-TECH-001 | Zone creation when zone doesn't exist | Auto-create primary zone |
| TC-TECH-002 | SRV record with port and weight | Full SRV fields |
| TC-TECH-003 | Multiple zones in single instance | Zone isolation |
| TC-TECH-004 | Token-based authentication | `_FILE` suffix support |
| TC-TECH-005 | API error handling (429, 500) | Retry and backoff |

#### Cloudflare (TC-CF-xxx)

| ID | Test Case | Notes |
|----|-----------|-------|
| TC-CF-001 | Proxied vs DNS-only records | Proxy flag handling |
| TC-CF-002 | Zone ID auto-discovery | From zone name |
| TC-CF-003 | Rate limit handling | Cloudflare API limits |
| TC-CF-004 | API token authentication | Bearer token |
| TC-CF-005 | TTL management | Custom vs auto TTL |

#### RFC 2136 (TC-RFC-xxx)

| ID | Test Case | Notes |
|----|-----------|-------|
| TC-RFC-001 | TSIG authentication (HMAC-SHA256) | Key-based auth |
| TC-RFC-002 | TSIG authentication (HMAC-SHA512) | Key-based auth |
| TC-RFC-003 | Unauthenticated updates | No TSIG key |
| TC-RFC-004 | Multiple record updates in single message | Batch efficiency |
| TC-RFC-005 | SOA serial increment verification | After update |

#### Pi-hole v5 (TC-PH5-xxx)

| ID | Test Case | Notes |
|----|-----------|-------|
| TC-PH5-001 | Custom DNS entry via API | Admin API |
| TC-PH5-002 | CNAME via local DNS records | File-based CNAME |
| TC-PH5-003 | Auth token handling | `_FILE` suffix support |
| TC-PH5-004 | Gravity database interaction | Verify no interference |

#### Pi-hole v6 (TC-PH6-xxx)

| ID | Test Case | Notes |
|----|-----------|-------|
| TC-PH6-001 | New v6 API endpoint usage | `/api/dns/` |
| TC-PH6-002 | Session-based authentication | Login/session flow |
| TC-PH6-003 | Migration from v5 to v6 | Config continuity |
| TC-PH6-004 | Custom DNS entry via v6 API | New API format |

#### dnsmasq (TC-DM-xxx)

| ID | Test Case | Notes |
|----|-----------|-------|
| TC-DM-001 | Config file creation | New file in config dir |
| TC-DM-002 | Config file update (add record) | Append to existing |
| TC-DM-003 | Config file update (remove record) | Remove line from file |
| TC-DM-004 | Reload via SIGHUP | Kill -HUP command |
| TC-DM-005 | Reload via systemctl | systemctl reload |
| TC-DM-006 | SSH remote management | SFTP write + SSH reload |
| TC-DM-007 | File permission handling | Write access validation |

#### Webhook (TC-WH-xxx)

| ID | Test Case | Notes |
|----|-----------|-------|
| TC-WH-001 | POST on record create | Custom URL + payload |
| TC-WH-002 | POST on record update | Updated payload |
| TC-WH-003 | POST on record delete | Delete notification |
| TC-WH-004 | Custom headers | Auth headers |
| TC-WH-005 | Webhook failure retry | Transient error handling |
| TC-WH-006 | Template rendering | Go template in payload |

---

## Source Tests

Each source must pass discovery and mode-specific tests.

### Per-Source Test Cases

#### TC-S-001: Service Discovery

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Deploy a service with DNS labels/annotations | Source discovers the service |
| 2 | Verify hostname extraction | Correct FQDN(s) parsed |
| 3 | Verify target extraction | Correct IP/hostname parsed |
| 4 | Verify record type assignment | Matches configured type |

#### TC-S-002: Service Removal

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Remove a previously discovered service | Source detects removal |
| 2 | Service removed from desired state | Reconciler notified |

#### TC-S-003: Service Update (Hostname Change)

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Change a service's DNS label/annotation | Source detects change |
| 2 | Old hostname removed, new hostname added | Desired state updated |

#### TC-S-004: Service Update (Target Change)

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Service target changes (new IP, new backend) | Source detects change |
| 2 | Existing record updated with new target | Desired state updated |

#### TC-S-005: Multi-Hostname Service

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Service has multiple hostnames (comma-separated) | All hostnames discovered |
| 2 | Each hostname creates a separate record | Correct count in desired state |

#### TC-S-006: Domain Filtering

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Service hostname matches `DOMAINS` glob | Service is included |
| 2 | Service hostname matches `EXCLUDE_DOMAINS` glob | Service is excluded |
| 3 | Service hostname matches neither | Service is excluded |

### Source-Specific Tests

#### Traefik (TC-TRK-xxx)

| ID | Test Case | Mode | Notes |
|----|-----------|------|-------|
| TC-TRK-001 | Label-based discovery | Docker labels | `traefik.http.routers.*.rule` |
| TC-TRK-002 | File provider discovery | File config | Static routes from YAML/TOML |
| TC-TRK-003 | Watch mode (real-time) | Watch | Event-driven updates |
| TC-TRK-004 | Poll mode (interval) | Poll | Timer-based refresh |
| TC-TRK-005 | Router rule parsing (Host) | Both | `Host(\`example.com\`)` |
| TC-TRK-006 | Router rule parsing (HostSNI) | Both | TLS routes |
| TC-TRK-007 | Router rule parsing (multiple hosts) | Both | `Host(\`a.com\`) \|\| Host(\`b.com\`)` |
| TC-TRK-008 | EntryPoint filtering | Both | Only specific entrypoints |
| TC-TRK-009 | Service target from loadbalancer | Both | IP extraction |

#### Kubernetes (TC-K8S-xxx)

| ID | Test Case | Mode | Notes |
|----|-----------|------|-------|
| TC-K8S-001 | Ingress discovery | Watch | Standard Ingress resources |
| TC-K8S-002 | Service annotation discovery | Watch | `dnsweaver.dev/hostname` |
| TC-K8S-003 | Namespace filtering | Both | Include/exclude namespaces |
| TC-K8S-004 | Watch mode reconnection | Watch | After API server restart |
| TC-K8S-005 | Poll mode fallback | Poll | Timer-based refresh |
| TC-K8S-006 | LoadBalancer IP extraction | Both | From service status |
| TC-K8S-007 | NodePort target resolution | Both | Node IP + port |

#### dnsweaver Native (TC-DWN-xxx)

| ID | Test Case | Mode | Notes |
|----|-----------|------|-------|
| TC-DWN-001 | Label-based discovery | Docker | `dnsweaver.hostname` label |
| TC-DWN-002 | Multi-hostname labels | Docker | Comma-separated hostnames |
| TC-DWN-003 | Container IP extraction | Docker | Bridge/overlay network |
| TC-DWN-004 | Watch mode (Docker events) | Watch | Real-time container events |
| TC-DWN-005 | Poll mode (Docker API) | Poll | Timer-based container list |
| TC-DWN-006 | Container start/stop lifecycle | Watch | Full lifecycle tracking |

---

## Scenario Tests

End-to-end scenarios testing system behavior across sources and providers.

#### TC-E2E-001: Service Lifecycle (Start → Stop)

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Start a service with DNS labels | Record created in provider |
| 2 | Verify DNS resolution | Name resolves correctly |
| 3 | Stop the service | Record removed from provider |
| 4 | Verify DNS resolution | Name no longer resolves |

#### TC-E2E-002: Service Hostname Change

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Service running with hostname `old.example.com` | Record exists |
| 2 | Update service with hostname `new.example.com` | Old record removed, new record created |
| 3 | Verify both DNS states | Old gone, new resolves |

#### TC-E2E-003: Service Target Change

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Service running with target `192.0.2.1` | A record points to 192.0.2.1 |
| 2 | Service target changes to `192.0.2.2` | Record updated to 192.0.2.2 |
| 3 | Verify DNS resolution | Resolves to new target |

#### TC-E2E-004: Multi-Service Conflict Resolution

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Two services claim same hostname | Conflict detected |
| 2 | Reconciler applies conflict policy | One record wins (deterministic) |
| 3 | Remove winning service | Remaining service takes over |

#### TC-E2E-005: Multi-Hostname Service

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Service declares 3 hostnames | 3 DNS records created |
| 2 | All records point to same target | Correct target for all |
| 3 | Remove service | All 3 records cleaned up |

#### TC-E2E-006: Provider Outage Recovery

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Provider becomes unreachable | Reconciler logs error |
| 2 | Source changes accumulate | Desired state updated |
| 3 | Provider recovers | All pending changes applied |
| 4 | Verify final state | Provider matches desired state |

#### TC-E2E-007: dnsweaver Restart Recovery

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | dnsweaver managing records, then restart | Process stops cleanly |
| 2 | dnsweaver starts with same config | Sources re-discovered |
| 3 | Reconciler runs full sync | Provider state matches desired |
| 4 | No duplicate or orphaned records | Clean state |

#### TC-E2E-008: Rolling Update (Zero-Downtime)

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Service running with DNS record | Record exists |
| 2 | Deploy new version of service (rolling) | Old and new containers overlap |
| 3 | Rollout completes | Record points to new container |
| 4 | Verify no DNS gaps | Resolution always succeeds |

#### TC-E2E-009: Multi-Provider Same Hostname

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Two providers configured for same domain | Both active |
| 2 | Service discovered with matching hostname | Both providers create records |
| 3 | Service removed | Both providers clean up |

---

## Reconciler Tests

Tests for the reconciler's edge case handling and correctness.

#### TC-R-001: Empty Desired State

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | No services discovered | Desired state is empty |
| 2 | Reconciler runs | All owned records cleaned up |
| 3 | Unowned records untouched | No accidental deletions |

#### TC-R-002: Empty Provider State

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Fresh provider with no records | Provider state is empty |
| 2 | Reconciler runs with desired state | All records created |

#### TC-R-003: Concurrent Reconciliation

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Multiple reconcile triggers arrive simultaneously | Only one runs at a time |
| 2 | Subsequent triggers queued or deduplicated | No race conditions |
| 3 | Final state is consistent | Matches latest desired state |

#### TC-R-004: Provider Rate Limiting

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Large batch of record changes | Many API calls needed |
| 2 | Provider returns rate limit error | Reconciler backs off |
| 3 | Retries after backoff period | All changes eventually applied |

#### TC-R-005: Partial Failure Recovery

| Step | Action | Expected Result |
|------|--------|-----------------|
| 1 | Batch of 10 record creates | Reconciler processes batch |
| 2 | Record 5 fails, others succeed | Failure logged, 9 records created |
| 3 | Next reconcile cycle | Failed record retried and created |

---

## Running Tests

### Unit Tests

```bash
go test ./... -count=1
```

### Integration Tests

Integration tests require a running test environment with real provider backends.

```bash
# All integration tests
make test-integration

# Specific provider
go test -tags=integration ./providers/technitium/... -v

# Specific source
go test -tags=integration ./sources/traefik/... -v
```

### Test Environment

See [README.md](README.md) for test environment setup and the [templates/](templates/) directory for writing new tests.
