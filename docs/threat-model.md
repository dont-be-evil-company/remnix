# Threat model

Remote storage and the network are untrusted. Local SQLite, the OS keyring,
and the recovery key / authenticators are trusted on a given device.

Classifications:

| # | Threat | Class | Notes |
| --- | --- | --- | --- |
| 1 | Rollback to older valid `metadata/manifest` | **detected** | `pullMetadata` rejects `rm.Counter < trusted` |
| 2 | Replay of old event bundles | **accepted / recoverable** | Idempotent apply by origin seq; old ciphertext still decrypts |
| 3 | Deleted/withheld acknowledgement | **accepted** | Blocks GC until retire; history remains |
| 4 | Checkpoint rollback | **detected / recoverable** | Trusted checkpoint id; `key recover` reloads snapshot |
| 5 | Concurrent key rotations | **detected** | Generation seq uniqueness; publish is lock-serialized |
| 6 | Active device retired while offline | **accepted** | Offline device cannot ack; retire is explicit |
| 7 | Partial upload | **prevented** | `PutAtomic`; rclone does not mark success on a truncated stream |
| 8 | Truncated object | **detected** | Decrypt/verify fail; FaultTransport tests |
| 9 | Duplicate event object | **prevented / recoverable** | Origin `(device, seq)` identity; insert ignore |
| 10 | Stale listing from cloud | **accepted / detected** | Next sync sees new listing; FaultTransport `StaleList` |
| 11 | Delete then stale reappearance | **accepted** | Tombstones + GC plan; cloud eventual consistency |
| 12 | Different remote views across devices | **accepted** | Sync converges; not a global snapshot isolation |
| 13 | Clock skew | **accepted** | Ranking uses timestamps; protocol identity is seq/counter not wall clock |
| 14 | Tampering with encrypted object bytes | **detected** | AEAD / MAC failure |
| 15 | Forged/modified manifest | **detected** | HMAC over generation and remote manifests |
| 16 | Copied metadata from another repository | **detected** | Probe + MAC; foreign SMK cannot unwrap |

Rows are not all “prevented” on purpose. The protocol was not rewritten solely
to close accepted limitations. Follow-ups belong in issues, not silent format
breaks.

Additional controls: portable YAML has no credentials; rclone.conf lives in the
data directory (0600) beside local.yaml, not in the commit-safe config dir;
logs, doctor, daemon status, and callback tails pass through `internal/redact`;
history insert skips `REMNIX_RECOVERY_KEY=` and similar.
