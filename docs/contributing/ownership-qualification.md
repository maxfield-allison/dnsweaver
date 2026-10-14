# Compact ownership qualification

Compact ownership is opt-in in its first release. DNS remains the only required state backend. This qualification scope covers ownership migration, recovery, and provider boundaries. It does not establish strict fencing for same-owner replicas or a completed public release.

## Retained automated evidence

| Provider | Current automated evidence | Real backend qualification still required |
| --- | --- | --- |
| Cloudflare | Registry wiring, paginated TXT listing, zone-ID resolution, quoted TXT strings, migration, restart, metadata and sibling preservation, reverse migration | Restricted DNS token, actual TXT and RRset limits, 200-record quota, visibility, throttling, concurrent distinct owners |
| Technitium | Registry wiring, HTTP form and JSON record round trips, migration, restart, metadata and sibling preservation, reverse migration | Supported server versions, long TXT wire answers, exact deletion, concurrent distinct owners |
| OVH | Signed HTTP requests, quoted TXT, exact record IDs, zone refresh calls, migration, restart, metadata and sibling preservation, reverse migration | Long TXT API representation and limits, refresh visibility, throttling, concurrent distinct owners |
| PowerDNS | Registry wiring, quoted TXT, whole-RRset replacement, exact sibling preservation, migration, restart, reverse migration | Supported server and backend versions, long TXT wire answers, disjoint ownership writes, documented data RRset restrictions |
| UniFi | Registry wiring, Integration API pagination and record provenance, TXT quoting, migration, restart, metadata and sibling preservation, reverse migration | Supported console versions, actual TXT length and domain-name rules, concurrent distinct owners |
| RFC 2136 | TCP DNS wire UPDATE/query, multi-string TXT, two-owner discovery without catalog, restart, interrupted catalog restoration, reverse migration to a catalog-only reader | BIND and Technitium authoritative servers, TSIG/update policy, large shared discovery RRsets, concurrent distinct owners |

The five API adapters run the same `RunOwnershipMigrationConformance` suite through their actual serializers and clients against HTTP fixtures. Its 19 A, AAAA, CNAME and SRV claims include spaces, quotes, and 512-byte synthetic metadata values. They use four physical TXT records, with the largest page at 3,469 bytes. This exercises multiple pages near the format's byte bound. Fixtures model selected API contracts. They do not establish real service limits or consistency.

The generic storage tests cover four concurrent independent owners, shared-claim withdrawal, corrupt and missing pages, unknown versions, forks, ambiguous writes, interrupted publication, cleanup retry, quota exhaustion, reverse migration, and disposable-process restart. Reconciler tests cover incomplete discovery, dry-run, compatible shared claims, metadata and interrupted record deletion. The static v1 fixture checks persisted compatibility independently of the current encoder.

Run the provider boundary checks with:

```sh
go test -race -run 'OwnershipMigrationConformance|TestRFC2136(CompactOwnership|ReverseMigration)' ./providers/cloudflare ./providers/technitium ./providers/ovh ./providers/powerdns ./providers/unifi ./providers/rfc2136
go test -race ./pkg/provider ./pkg/dnsupdate ./internal/config ./internal/reconciler
```

## Real backend acceptance

Use disposable zones and independently configured processes. Preserve exact backend versions, candidate commit, API permissions, record counts, response formats, and cleanup receipts. A skipped credential-dependent test is a coverage gap.

Each advertised provider needs a forward migration and verified reverse migration with a fresh legacy reader, one-page and multi-page snapshots near the 3,500-byte bound, all supported exact member types and metadata, and unchanged unrelated records. Exercise quota failure before page creation, between pages and manifest, and during reversal. Restart after interruption and verify every claim before enabling removals.

Then run distinct owners concurrently over disjoint names, retain an offline owner's claims, interrupt one source, and withdraw compatible shared claims. Test the provider's actual mutation unit. Whole-RRset APIs need disjoint data RRsets or additional coordination. A stale read or an in-flight request can defeat readback-based exclusion, so fixture convergence does not authorize a strict fencing claim.

Measure both steady and peak DNS record use, API calls per change, convergence time, and cleanup after failure. The record savings are established for the retained fixtures. API and latency savings remain unmeasured. Uncommitted staged pages can consume quota after interruption and need explicit recovery guidance.

Before changing the default, complete the advertised provider matrix, retain public CI and built-image evidence for the exact candidate, exercise rolling upgrade and downgrade, and publish the [migration instructions](../configuration/ownership.md#migration-and-rollback). PostgreSQL and Valkey adapters can follow separately.

## Provider references

Cloudflare documents [record quotas](https://developers.cloudflare.com/dns/manage-dns-records/) and [wire and combined RRset size limits](https://developers.cloudflare.com/dns/manage-dns-records/reference/dns-record-types/). Its [zone-details API](https://developers.cloudflare.com/api/resources/zones/methods/get/) accepts DNS Read or DNS Write permission for zone-name discovery.

PowerDNS documents [automatic splitting of long TXT values](https://doc.powerdns.com/authoritative/appendices/types.html#txt). Technitium's [API contract](https://github.com/TechnitiumSoftware/DnsServer/blob/master/APIDOCS.md) and [TXT implementation](https://github.com/TechnitiumSoftware/TechnitiumLibrary/blob/master/TechnitiumLibrary.Net/Dns/ResourceRecords/DnsTXTRecordData.cs) distinguish text from character strings. These documents support the test design; running the target server remains a separate verification.
