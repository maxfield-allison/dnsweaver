# Compact ownership qualification

Compact ownership is opt-in in its first release. DNS remains the only required state backend. This qualification scope covers ownership migration, recovery, and provider boundaries. It does not establish strict fencing for same-owner replicas.

## Retained automated evidence

| Provider | Current automated evidence | Real backend qualification still required |
| --- | --- | --- |
| Cloudflare | Registry wiring, paginated TXT listing, zone-ID resolution, quoted TXT strings, migration, restart, metadata and sibling preservation, reverse migration; live distinct-owner and authoritative wire checks described below | Restricted DNS token, quota exhaustion and temporary peak headroom beyond the observed production migration/reversal, broader account and TXT limits, throttling, adversarial stale/in-flight writes |
| Technitium | Registry wiring, HTTP form and JSON record round trips, migration, restart, metadata and sibling preservation, reverse migration; live server 15.6 and five authoritative nodes described below | Other supported versions, adversarial stale/in-flight writes |
| OVH | Signed HTTP requests, quoted TXT, exact record IDs, zone refresh calls, migration, restart, metadata and sibling preservation, reverse migration | Long TXT API representation and limits, refresh visibility, throttling, concurrent distinct owners |
| PowerDNS | Registry wiring, quoted TXT, whole-RRset replacement, exact sibling preservation, migration, restart, reverse migration | Supported server and backend versions, long TXT wire answers, disjoint ownership writes, documented data RRset restrictions |
| UniFi | Registry wiring, Integration API pagination and record provenance, TXT quoting, migration, restart, metadata and sibling preservation, reverse migration | Supported console versions, actual TXT length and domain-name rules, concurrent distinct owners |
| RFC 2136 | TCP DNS wire UPDATE/query, multi-string TXT, two-owner discovery without catalog, restart, interrupted catalog restoration, reverse migration to a catalog-only reader | BIND and Technitium authoritative servers, TSIG/update policy, large shared discovery RRsets, concurrent distinct owners |

The five API adapters run the same `RunOwnershipMigrationConformance` suite through their actual serializers and clients against HTTP fixtures. Its 19 A, AAAA, CNAME and SRV claims include spaces, quotes, and 512-byte synthetic metadata values. At the 3,500-byte format bound they use four physical TXT records, with the largest page at 3,469 bytes. Cloudflare uses its lower adapter bound. This exercises multiple pages near the applicable byte bound. Fixtures model selected API contracts. They do not establish real service limits or consistency.

The generic storage tests cover four concurrent independent owners, shared-claim withdrawal, corrupt and missing pages, unknown versions, forks, ambiguous writes, interrupted publication, cleanup retry, quota exhaustion, reverse migration, and disposable-process restart. Reconciler tests cover incomplete discovery, dry-run, compatible shared claims, metadata and interrupted record deletion. The static v1 fixture checks persisted compatibility independently of the current encoder.

Run the provider boundary checks with:

```sh
go test -race -run 'OwnershipMigrationConformance|TestRFC2136(CompactOwnership|ReverseMigration)' ./providers/cloudflare ./providers/technitium ./providers/ovh ./providers/powerdns ./providers/unifi ./providers/rfc2136
go test -race ./pkg/provider ./pkg/dnsupdate ./internal/config ./internal/reconciler
```

## Live qualification on 2026-10-14

Two disposable OS processes read declarations from a live Docker host and Kubernetes IngressRoutes on every reconciliation. The starting census contained 18 running Docker workloads with 20 distinct declared hostnames and 274 IngressRoutes with 153 distinct declared hostnames. Each process selected eight declarations and mapped them to hashed aliases in a disposable DNS scope. A controlled shared declaration exercised overlapping ownership. Production workload labels, routes and DNSweaver deployments were unchanged.

| Check | Cloudflare DNS API v4 | Technitium 15.6 |
| --- | --- | --- |
| Concurrent forward migration | 13 claims: 13 legacy TXT to 4 compact TXT | Same |
| Concurrent scope growth | 17 claims, 16 CNAME answers, 4 ownership TXT | 17 claims, 16 A answers, 4 ownership TXT; automatic HTTPS companions enabled |
| Disposable process restart | All claims recovered; no answer changes | Same |
| Simulated source interruption | Inventory preserved | Same |
| Shared-claim withdrawal | First claimant retained answer; last claimant removed it | Same |
| Reverse migration | 15 legacy claims restored; two empty manifests retained | Same |
| Pre-feature provider reader | Recovered all 15 remaining claims and answers | Same |
| User TXT sibling and cleanup | Sibling preserved; test-owned inventory returned to empty | Same; NS and SOA retained |

The pre-feature reader was compiled against an unmodified pre-feature provider adapter and ownership parser, with a read-only helper. This verifies restored representation compatibility. It does not qualify a rolling deployment of the full older CLI. Discovery uses live declarations with mapped test names; production watchers and application traffic were not switched to the candidate.

The first Cloudflare concurrent-growth cycle rejected a changing ownership snapshot before creating its answer. All answers present had committed claims. A complete subsequent run passed. Later attempts also exposed a Technitium object-state error during concurrent listing and a Cloudflare startup recovery failure. Those runs stopped and cleaned their inventories. The harness retains these errors, follows the daemon's existing nonfatal startup recovery behavior, and permits bounded complete retries for changing snapshots and the observed Technitium listing error. It requires complete expected claims and answers. A rejected cycle is preserved as evidence; passing a later cycle does not establish single-cycle success or distributed fencing.

A stricter later check caught an answer whose claim existed only in an uncommitted page. Publication had failed during a changing snapshot; the next cycle treated the answer as existing, skipped adoption, and reported no failed action. The fix retains a disposable repair intent only after a confirmed local answer write. A later cycle checks the unchanged member and foreign claims before completing its durable marker. Unresolved repairs report failure and block changes at that name. Regression tests cover same-process recovery, continued failure, withdrawal, dry-run, external changes, foreign claims and loss of proof after restart. A fresh process cannot use staged pages or assume ownership of an unclaimed answer.

Valid mixed-member wire tests used 19 A, AAAA, CNAME and SRV claims with 512-byte opaque metadata, an offline owner's legacy claim, and a user TXT sibling. Technitium used four compact TXT records; its largest page was 3,364 bytes. All pages and answers matched TCP queries on five authoritative servers. Cloudflare's qualified smaller pages used six compact TXT records, with the largest at 1,988 bytes, matching both assigned authoritative nameservers. Exact claim metadata survived expansion from the wire-verified state.

Cloudflare rejected a 3,124-byte encoded page with HTTP 400 and API code 1000. The rejection reproduced under a neutral name and with explicit quoted character strings. Repetitive 3,500-byte content and truncated encoded values through 2,500 bytes were accepted; a 3,500-byte opaque hex value was rejected. These observations do not establish a universal size threshold. The adapter now limits new pages and manifests to a conservative 2,000 bytes. Failed publication preserved legacy claims, and each test cleaned its owned records. Readers still accept the full persisted v1 format bound.

The disposable Cloudflare zone reported a quota of 1,000 records. A later [production canary](#production-canary-follow-up-on-2026-10-14) exercised migration and reversal in a real 200-record zone. Quota exhaustion and temporary peak headroom remain separate qualification gaps; the generic quota fixture covers forward and reverse headroom failures. The existing DNS-scoped credential could not access that historical zone, so this run used another existing authorized credential. A minimally scoped live token remains a qualification gap. API request savings and broad latency claims are not established.

The opt-in live harness is excluded from ordinary builds and skips without `DNSWEAVER_LIVE_CONFIG`. Supply a protected JSON file with `Provider`, `Zone`, `ZoneID` (Cloudflare) or `URL` (Technitium), `TokenFile`, `Target`, `RecordType`, a fresh `qa-` `Prefix`, two distinct scoped `Owners`, `Kubeconfig`, `DockerAddress`, `SSHKey`, and an `Output` directory. Wire tests also require explicit `NameServers` as `address:53`. Configure `AutoHTTPS` to exercise Technitium companions. Credentials are read inside the process; snapshots and actor logs are private files.

```sh
go test -c -tags integration -o /path/to/ownership-live.test ./internal/reconciler
DNSWEAVER_LIVE_CONFIG=/path/to/protected-config.json /path/to/ownership-live.test -test.run='^TestOwnershipLiveWorkloads$' -test.v -test.timeout=20m
DNSWEAVER_LIVE_CONFIG=/path/to/protected-config.json /path/to/ownership-live.test -test.run='^TestOwnershipLiveWirePages$' -test.v -test.timeout=15m
```

Both tests require an initially empty inventory of provider-managed record types. Each cleans only its fresh QA names and owner namespaces, leaving outside records untouched and failing on an unexpected inventory. Give each run a separate output directory and verify cleanup before the next run. `scripts/ownership_live_preflight.py` captures immutable baselines and current provider quota; creating a local zone requires its explicit flag. `scripts/ownership_live_txt_probe.py` isolates a rejected Cloudflare page using the optional offline `TestOwnershipPrepareProbe` preparation step. It is a transport experiment; truncated probe values never grant ownership.

## Production canary follow-up on 2026-10-14

After upgrading every reader in the shared zones to v3.2.0, one owner was switched to compact storage. The canary covered Cloudflare and Technitium 15.6. Its 21 per-claim ownership TXT records became six compact TXT records, a reduction of 15 records (71%).

The Cloudflare zone had a 200-record limit. Its observed count was 182 before migration, 176 in compact mode, 183 in the verified legacy reversal state and 176 after compact was re-enabled. The reversal count is an observed state, not a measured peak. This qualifies those migration and reversal paths within a real 200-record zone; quota-exhaustion behavior and broader account limits still need separate qualification.

A fresh container reconstructed its claims from DNS. Real Docker and Kubernetes discovery exercised compatible distinct-owner claims across compact and legacy storage. First withdrawal retained the shared answer and its metadata; final withdrawal removed it. Complete supported logical provider views matched the expected state throughout. Authoritative TCP queries checked the persisted TXT values.

Reversal ran on the new binary and a pre-feature provider reader recovered every claim. Empty manifests were retained before compact was re-enabled. No older production writer was started. This verifies the restored ownership representation; full older-daemon rollback remains a separate qualification case.

Minimum credential scopes, additional provider versions, same-owner fencing and stale in-flight provider writes remain open. Compact stays opt-in.

## Real backend acceptance

Use disposable zones and independently configured processes. Preserve exact backend versions, candidate commit, API permissions, record counts, response formats, and cleanup receipts. A skipped credential-dependent test is a coverage gap.

Each advertised provider needs a forward migration and verified reverse migration with a fresh legacy reader, one-page and multi-page snapshots near the 3,500-byte bound, all supported exact member types and metadata, and unchanged unrelated records. Exercise quota failure before page creation, between pages and manifest, and during reversal. Restart after interruption and verify every claim before enabling removals.

Then run distinct owners concurrently over disjoint names, retain an offline owner's claims, interrupt one source, and withdraw compatible shared claims. Test the provider's actual mutation unit. Whole-RRset APIs need disjoint data RRsets or additional coordination. A stale read or an in-flight request can defeat readback-based exclusion, so fixture convergence does not authorize a strict fencing claim.

Measure both steady and peak DNS record use, API calls per change, convergence time, and cleanup after failure. The record savings are established for the retained fixtures. API and latency savings remain unmeasured. Uncommitted staged pages can consume quota after interruption and need explicit recovery guidance.

Before changing the default, complete the advertised provider matrix, retain public CI and built-image evidence for the exact candidate, exercise rolling upgrade and downgrade, and publish the [migration instructions](../configuration/ownership.md#migration-and-rollback). PostgreSQL and Valkey adapters can follow separately. Track provider qualification and the later default-change decision in [#223](https://github.com/maxfield-allison/dnsweaver/issues/223); same-owner coordination remains [#139](https://github.com/maxfield-allison/dnsweaver/issues/139).

## Provider references

Cloudflare documents [record quotas](https://developers.cloudflare.com/dns/manage-dns-records/) and [wire and combined RRset size limits](https://developers.cloudflare.com/dns/manage-dns-records/reference/dns-record-types/). Its [zone-details API](https://developers.cloudflare.com/api/resources/zones/methods/get/) accepts DNS Read or DNS Write permission for zone-name discovery.

PowerDNS documents [automatic splitting of long TXT values](https://doc.powerdns.com/authoritative/appendices/types.html#txt). Technitium's [API contract](https://github.com/TechnitiumSoftware/DnsServer/blob/master/APIDOCS.md) and [TXT implementation](https://github.com/TechnitiumSoftware/TechnitiumLibrary/blob/master/TechnitiumLibrary.Net/Dns/ResourceRecords/DnsTXTRecordData.cs) distinguish text from character strings. These documents support the test design; running the target server remains a separate verification.
