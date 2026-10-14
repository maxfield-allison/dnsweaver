# Ownership storage and active writers

dnsweaver stores ownership in the DNS zone. A replacement process needs the provider configuration and credentials, with no persistent volume or separate database. `legacy` per-member TXT markers remain the default in the first release with compact storage. Set `DNSWEAVER_OWNERSHIP_STORAGE=compact` to opt in on TXT-capable providers with zone discovery after upgrading every writer in the zone.

```yaml
instance_id: docker-host-a
reconciler:
  ownership_tracking: true
  ownership_storage: compact
```

The environment variable overrides `reconciler.ownership_storage`. `--validate` prints the configured mode. Startup logs report which providers use it. Webhooks without zone enumeration retain their existing behavior. Providers without TXT support retain their documented ownership limitations.

## Record overhead

Compact storage packs exact ownership claims and metadata into compressed, immutable TXT pages. Each owner has a manifest describing its current pages. Pages contain at most 3,500 encoded bytes, so the number of records depends on the claims and their metadata. The migration test packs 80 ordinary A-record claims into one page plus one manifest, compared with 80 legacy markers. A larger snapshot uses additional pages.

Cloudflare uses a conservative 2,000-byte bound for new pages and manifests. Live tests found that some larger encoded values were rejected even when repetitive TXT values of the same size were accepted. Other providers use the format's 3,500-byte bound unless their adapter supplies a lower limit. Readers retain the full v1 format bound for existing snapshots. A manifest that cannot fit its provider's bound stops publication before any writes.

One claim still needs a page and a manifest, so small owners can use more TXT records than legacy storage. These counts measure DNS record overhead. Publication and readback add API calls; fewer stored records does not establish faster reconciliation or fewer requests.

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

To return to an older binary, first select `legacy` on the new binary. It restores and verifies every legacy marker before committing an empty compact snapshot and cleaning obsolete pages. RFC 2136 also restores and verifies the old hostname catalog before completing rollback. Reverse migration needs quota headroom too. Complete this for every compact owner before running an older writer in that zone. Keep the empty manifest. It preserves ownership history. Changing the instance ID is a transfer of ownership and requires a separate migration.

The legacy RFC 2136 catalog replaces shared chunks. Serialize reverse migration and keep other catalog writers paused during conversion and verification. This restriction concerns ownership conversion; DNS answers remain served throughout it.

For a snapshot with `N` legacy markers and `P` new pages, initial migration can temporarily need `P + 1` free record slots. An update needs its newly staged pages and one manifest while old records remain. Rollback can need `N` absent legacy markers, any absent legacy catalog records, and one retirement manifest in addition to the compact records. Other writers and failed cleanup consume that same quota. Check the actual zone quota in both directions before opting in.

Reserve rollback capacity if downgrading to an older binary must remain possible. Spending the freed space on new answers can make the legacy representation exceed the zone quota entirely. In that case, reversal requires a higher quota, fewer managed records, or another provider. Keeping the new binary in compact mode preserves access to the state while that capacity is arranged.

For example, the 200-record quota test starts with 80 answers, 80 legacy markers, and 38 unrelated TXT records: 198 total. Two free slots allow migration, after which 120 remain. Reversal temporarily needs 201 records to publish the empty manifest safely. A 200-record limit leaves the compact claims intact and reports an error. Successful reversal retains 199 records, including the empty manifest. Space for migration alone does not guarantee space for rollback.

Before enabling compact storage:

1. Export the complete zone, including ownership TXT, and record each writer's version, instance ID, source scope, and storage setting.
2. Upgrade every writer in `legacy` mode. Confirm normal reconciliation and complete provider listings before changing storage.
3. Check temporary migration and rollback capacity. Enable compact for one owner and confirm valid state, claim counts, and removal of that owner's legacy markers.
4. Restart that owner and verify recovery from DNS. In a disposable test zone, reverse it to `legacy` and verify a fresh old reader can recover before wider rollout.

Use a consistent provider access method for writers sharing a zone. RFC 2136 discovers the common anchor only; it cannot enumerate owner-specific manifests published through another provider API. Mixing those access methods in one zone is not qualified. A raw zone backup is evidence for recovery, but restoring an old whole-zone export can overwrite other active owners' newer changes. Pause affected writers and reconcile those changes before a manual restore.

Missing pages, invalid checksums, unsupported versions and competing histories make the ownership snapshot unavailable. Reconciliation preserves DNS data and reports the error. Repair the zone from its retained records or backup. Deleting manifests to silence the error discards ownership evidence. A DNS answer created just before ownership publication fails may need explicit adoption to repair its claim.

After a confirmed answer write whose ownership publication fails, the running process retains a disposable repair intent. A later complete cycle verifies the exact answer, TTL and provider options, checks foreign claims, and retries its member marker. Failed repairs remain errors and block changes at that name. Changed or missing data invalidates the intent. This adds no volume or external dependency. A process crash loses that local proof, so an unclaimed answer still requires explicit adoption or manual review after restart. Staged pages alone never grant ownership.

## Observability

The existing metrics endpoint exposes `dnsweaver_ownership_storage_mode{provider,mode}`, `dnsweaver_ownership_state_valid{provider}`, `dnsweaver_ownership_entries{provider}` and `dnsweaver_ownership_state_records{provider}`. The record gauge includes visible predecessor manifests and legacy markers. Zone-enumerating APIs also count staged pages. RFC 2136 can count only discoverable records. Debug logs report each committed revision, claim count and page count. Cleanup failures appear as warnings.

PostgreSQL and Valkey adapters remain follow-up work. They are optional and must establish transaction, lease-loss and provider-write guarantees before being described as stricter coordination. The DNS-backed default does not require either service. See the [wire format](../contributing/ownership-format.md) for the shared ownership and recovery contract.
