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
   internal/app        (endpoint factory, fan-out sync, lock, callbacks)
        │
   internal/sync/syncer  (events, checkpoints, GC, keys)
        │
   Endpoints: directory | rsync | scp | rclone  (any mix, all mirrors)
        │
   internal/repository.Probe   (empty / unrelated / valid / partial / unsupported)
```

rclone is a **filesystem adapter**. syncsh never calls `rclone sync` or
`rclone bisync` on the native path. Merge, encryption, and GC stay in syncsh.

A device may have unlimited enabled endpoints (Google Drive, S3, Dropbox, a
local folder, ...). Each is a mirror of the same encrypted repository. Sync
pulls and publishes per endpoint, then equalizes missing objects so a
reachable subset still converges the rest.

rsync and scp are session transports (`Begin`/`End` staging). rclone talks to
the cloud directly.

The exclusive mutator for remote writes is `internal/app`’s file lock
(`syncsh.lock`), used by daemon, manual sync, GC, and key/device ops.

Further reading: [sync-protocol.md](sync-protocol.md),
[cryptography.md](cryptography.md), [rclone.md](rclone.md),
[threat-model.md](threat-model.md).
