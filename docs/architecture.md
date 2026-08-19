# Architecture

syncsh is a local-first shell history manager. A device writes commands to
SQLite, optionally encrypts them into event bundles, and stores those objects
on a remote filesystem. There is no syncsh server.

```text
TUI / wizard / picker / shell agent
        │
   internal/agent      (unix socket RPC: suggest, history start/end)
        │
   internal/config     (portable YAML; rclone.conf is in the data dir)
        │
   internal/app        (Transport factory, lock, callbacks)
        │
   internal/sync/syncer  (events, checkpoints, GC, keys)
        │
   Transport: directory | rsync | scp | rclone
        │
   internal/repository.Probe   (empty / unrelated / valid / partial / unsupported)
```

rclone is a **filesystem adapter**. syncsh never calls `rclone sync` or
`rclone bisync` on the native path. Merge, encryption, and GC stay in syncsh.

rsync and scp are session transports (`Begin`/`End` staging). rclone talks to
the cloud directly.

The exclusive mutator for remote writes is `internal/app`’s file lock
(`syncsh.lock`), used by daemon, manual sync, GC, and key/device ops.

Further reading: [sync-protocol.md](sync-protocol.md),
[cryptography.md](cryptography.md), [rclone.md](rclone.md),
[threat-model.md](threat-model.md).
