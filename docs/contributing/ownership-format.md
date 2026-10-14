# Compact DNS ownership format

Version 1 stores ownership in the DNS zone. It requires no local state volume or separate service. The consumer is any dnsweaver process reading the same physical provider and zone. Each logical owner publishes its own snapshot. Local provider names are not part of the namespace.

The independently serialized fixture in `pkg/provider/testdata/ownership-v1.json` checks persisted compatibility. Later implementations must continue to read it.

## Records

Zone-enumerating APIs store each owner's manifests at `_dnsweaver-state.<owner-hash-16>.<zone>`. This avoids unrelated writers changing one TXT RRset, including APIs that replace an entire RRset. RFC 2136 uses the known discovery name `_dnsweaver-state.<zone>` and exact-value DNS UPDATE operations. It needs no shared mutable index or AXFR. Readers recognize both forms. Each TXT value is one manifest, prefixed `dnsweaver-state=v1:` and followed by base64url JSON without padding. The object has these fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `version` | integer | Exactly `1` |
| `owner` | string | Existing instance ID. Empty identifies the legacy single-instance owner |
| `revision` | integer | Positive revision within that owner |
| `previous` | string | SHA-256 of the previous encoded manifest, or empty for the first revision |
| `pages` | array of strings | Ordered SHA-256 digests of encoded page values. An empty array represents no claims |
| `entries` | integer | Number of ownership entries across those pages |

Each page is a TXT record at `_dw-p.<owner-hash-16>.<page-hash-32>.<zone>`. Its value starts `dnsweaver-page=v1:` followed by base64url, without padding, of gzip-compressed JSON. The JSON is an array of entries with `name` and `value`: the normalized legacy ownership name and its existing versioned ownership value. Those values preserve exact record membership, instance identity, SRV/HTTPS fields and metadata. They are not a serialization of the runtime provider record or its private API identifier.

Gzip headers contain no timestamp or process identity. Entries sort by name and then value. Digests cover the complete encoded TXT value. A page is at most 3,500 encoded bytes; its expanded JSON is at most 256 KiB. A manifest is at most 3,500 encoded bytes, references at most 64 distinct pages, and describes at most 16,384 distinct entries. Provider limits may require smaller pages. Unknown fields, unsupported versions, invalid ownership values, duplicate entries, oversized input and checksum mismatches are errors.

## Publishing and reading

The writer reads current state, creates missing immutable pages, reads them back, checks that its previous manifest has not changed, then publishes a new manifest. A timeout after any write requires readback before deciding whether the operation succeeded. A matching conflict is idempotent. A conflict alone is not evidence of success.

The new manifest is visible before the old manifest is removed. A reader can select the new snapshot when the visible manifests form one contiguous parent chain. Competing children of the same parent, unrelated histories or revision gaps are ambiguous and stop reconciliation for that provider. Only the selected snapshot's pages are required. The reader checks a second manifest listing before returning the snapshot, so a changed listing cannot authorize cleanup.

Cleanup visits predecessors from oldest to newest. It removes their pages that the selected snapshot no longer uses, then removes the predecessor manifest. A failed deletion leaves that manifest as a durable cleanup queue. Readers require only the selected snapshot's pages. Uncommitted pages do not represent ownership and are retained because a writer may still be staging them. Failed cleanup does not invalidate a committed snapshot, but must be reported and retried. Empty snapshots retain their manifest, so a stopped source does not reset ownership history.

## Compatibility and active writers

Readers continue to recognize existing per-member ownership TXT records. Migration copies only the current owner's markers into verified pages before deleting those exact markers. It must not run during dry-run or when ownership tracking is disabled. Reverse migration writes and verifies legacy markers before removing compact state. An older binary cannot be assumed to read compact state.

Distinct owners can publish and reconcile concurrently without rewriting one shared TXT value. A stable instance ID belongs to the logical source scope, not to a pod or container incarnation. Compatible shared claims must survive withdrawal by one owner. A failed discovery, disconnected process or expired heartbeat is not removal of its resources.

Manifest readback detects conflicting histories. It does not fence an already in-flight DNS request. This format does not claim strict mutual exclusion for replicas sharing an owner ID or conflicting mutations of one DNS name. Provider conditional writes and conservative ownership checks must establish the supported behavior. Unsupported overlap preserves records and reports a conflict. Optional external storage or coordination must use these same ownership and recovery semantics.
