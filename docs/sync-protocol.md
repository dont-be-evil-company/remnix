# Sync protocol

Devices exchange ciphertext through a filesystem-shaped remote:

```text
metadata/manifest              # signed active generation + device list
metadata/devices/<id>.json
metadata/version
keys/generations/<id>/manifest
events/<device-id>/...           # encrypted bundles
checkpoints/<id>/manifest
checkpoints/<id>/snapshot      # gzip + encrypted compact history
acks/<device-id>.ack
```

`Engine.Sync` pulls metadata (with rollback detection on
`metadata/manifest` counters, namespaced per endpoint as `remote:<id>`),
imports generations, merges event bundles, pushes local events, writes acks,
and may checkpoint. `published_seq` is also per endpoint so a new mirror
receives this device's history. Pulled bundle keys stay global.

After every enabled endpoint has been synced, the coordinator equalizes
objects (copy any layout key that exists on one remote and is missing on
another). A `metadata/manifest` with a lower counter never overwrites a
higher one. Partial failure continues: one cloud being down does not skip
the others. Callbacks run if any endpoint succeeded.

Writes use `Transport.PutAtomic`. Directory transport is tmp+rename. rclone
writes a temp object then moves when the backend supports it; otherwise
write-then-verify. On backends that allow duplicate names (Google Drive),
the move is followed by a sweep that deletes every other object with that
path so acks and device metadata replace instead of stacking.

`repository.Probe` inspects `metadata/manifest` **and** `keys/generations/`
so a stray file is not treated as a live repository.

Create (`syncsh setup`) refuses Valid / Partial / UnsupportedVersion remotes.
Join (`syncsh device add`) **requires** Valid and never calls
`InitializeRemote`.

Garbage collection deletes superseding event objects after every required
device has acknowledged a checkpoint. Retired devices no longer block GC.

rsync/scp probe and setup wrap `Begin`/`End` so the live remote is inspected,
not an empty local stage.
