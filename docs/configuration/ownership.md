# Ownership storage and active writers

dnsweaver stores ownership in the DNS zone. A replacement process needs the provider configuration and credentials, with no persistent volume or separate database. In the unreleased source, `DNSWEAVER_OWNERSHIP_STORAGE=compact` is the default for TXT-capable providers with zone discovery. `legacy` selects per-member TXT markers.

```yaml
instance_id: docker-host-a
reconciler:
  ownership_tracking: true
  ownership_storage: compact
```

The environment variable overrides `reconciler.ownership_storage`. `--validate` prints the configured mode. Startup logs report which providers use it. Webhooks without zone enumeration retain their existing behavior. Providers without TXT support retain their documented ownership limitations.

## Record overhead

Compact storage packs exact ownership claims and metadata into compressed, immutable TXT pages. Each owner has a manifest describing its current pages. Pages contain at most 3,500 encoded bytes, so the number of records depends on the claims and their metadata. The migration test packs 80 ordinary A-record claims into one page plus one manifest, compared with 80 legacy markers. A larger snapshot uses additional pages.

Provider APIs use `_dnsweaver-state.<owner-hash>.<zone>` for manifests and `_dw-p.<owner-hash>.<page-hash>.<zone>` for pages. RFC 2136 queries the known name `_dnsweaver-state.<zone>` and then its referenced pages. Its compact writes use TCP and do not update the shared hostname catalog. Existing catalog records remain available for legacy discovery.

Publication stages and verifies new pages before publishing the manifest. The old state stays readable until that succeeds. Allow quota headroom for the new pages and one new manifest during migration and subsequent updates. A full zone cannot safely migrate by deleting unverified ownership first. Failed writes preserve legacy markers and report the error.

Cleanup removes obsolete pages and their manifests. A failed cleanup leaves enough history to retry after restart. Pages staged before an interrupted publication may remain: automatically deleting them could interrupt another writer. Include these records when checking provider quotas.

## Active/active operation

Assign a stable `DNSWEAVER_INSTANCE_ID` to each independent source scope. For example, a Docker host and a Kubernetes watcher can use different IDs while managing the same zone. Replacing a process keeps its ID. Provider instance names are local configuration names. They do not define ownership.

Distinct owners publish separate page sets. Their claims remain present while a process is offline or its source is unavailable. Incomplete discovery suppresses removals. Identical, unchanged members may have multiple owners: withdrawing one claim retains the answer, and the last claimant can remove it. Changes to shared members are rejected. Agree on their TTL and provider options before sharing them. Legacy hostname claims remain conservative and cannot grant a new shared claim. Simultaneous withdrawals can conservatively leave an unclaimed answer that needs explicit adoption or manual review.

This does not provide distributed fencing for two processes using the same owner ID. Conflicting manifest histories stop reconciliation, but a provider request already in flight cannot be revoked by a re-read. Give concurrently active processes separate IDs and source scopes. An ID shared by replicas with different or incomplete source views can authorize removal of the other replica's resources.

Provider atomicity also matters when owners write different members of the same DNS RRset. RFC 2136 can insert and delete exact values atomically. APIs that replace whole RRsets, including the current PowerDNS adapter, cannot safely support simultaneous changes to that RRset using readback alone. Separate owners avoid this race for ownership storage. Shared data RRsets still require provider-specific coordination. An observed foreign claim blocks conflicting mutations, but DNS-only storage cannot promise fencing across every provider API.

## Migration and rollback

Upgrade all writers in a shared zone with `ownership_storage: legacy` first. Older binaries cannot read compact pages. After every writer understands the format, enable compact storage for the desired owners. Migration copies only the current owner's markers, verifies the committed pages, then deletes those exact legacy markers. It preserves foreign claims and unrelated user TXT. Dry-run and disabled ownership tracking never migrate.

To return to an older binary, first select `legacy` on the new binary. It restores and verifies every legacy marker before committing an empty compact snapshot and cleaning obsolete pages. Reverse migration needs quota headroom too. Complete this for every compact owner before running an older writer in that zone. Keep the empty manifest. It preserves ownership history. Changing the instance ID is a transfer of ownership and requires a separate migration.

Missing pages, invalid checksums, unsupported versions and competing histories make the ownership snapshot unavailable. Reconciliation preserves DNS data and reports the error. Repair the zone from its retained records or backup. Deleting manifests to silence the error discards ownership evidence. A DNS answer created just before ownership publication fails may need explicit adoption to repair its claim.

## Observability

The existing metrics endpoint exposes `dnsweaver_ownership_storage_mode{provider,mode}`, `dnsweaver_ownership_state_valid{provider}`, `dnsweaver_ownership_entries{provider}` and `dnsweaver_ownership_state_records{provider}`. The record gauge includes visible predecessor manifests and legacy markers. Zone-enumerating APIs also count staged pages. RFC 2136 can count only discoverable records. Debug logs report each committed revision, claim count and page count. Cleanup failures appear as warnings.

PostgreSQL and Valkey adapters remain follow-up work. They are optional and must establish transaction, lease-loss and provider-write guarantees before being described as stricter coordination. The DNS-backed default does not require either service. See the [wire format](../contributing/ownership-format.md) for the shared ownership and recovery contract.
