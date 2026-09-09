# Architecture

remnix is a local-first shell history manager. A device writes commands to
SQLite, optionally encrypts them into event bundles, and stores those objects
on a remote filesystem. There is no remnix server.

```text
TUI / wizard / picker / remnix-attach / shell hooks
        │
   remnix daemon       (control.sock + terminal.sock)
        │
   history.Cache       (RAM projection) + history.Store (SQLite WAL)
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

One long-lived `remnix daemon` per user session owns SQLite, the interactive
history cache, periodic sync/GC, inner PTYs, and overlay TUIs (Ctrl+R search
and Ctrl+Space suggest). Terminals attach with the tiny `remnix-attach`
helper (not another copy of the Go binary). Shell widgets trigger overlays
over the control socket (`search-interactive` / `suggest-interactive`);
attach stays a byte pump. A daemon crash cannot reattach existing PTYs; the
service manager should restart it and new shells will reconnect.

rclone is a **filesystem adapter**. remnix never calls `rclone sync` or
`rclone bisync` on the native path. Merge, encryption, and GC stay in remnix.

The encrypted repository is the endpoint path on the provider (for example
`gdrive:remnix` on Google Drive). This device keeps SQLite history,
`local.yaml`, and `rclone.conf` under `$XDG_DATA_HOME/remnix`. A folder such
as `~/GoogleDrive/remnix` is **not** the repository unless it is a
`type: directory` endpoint in `config.yaml`.

A device may have unlimited enabled endpoints (Google Drive, S3, Dropbox, a
local folder, ...). Each is a mirror of the same encrypted repository. Sync
pulls and publishes per endpoint, then equalizes missing objects so a
reachable subset still converges the rest.

rsync and scp are session transports (`Begin`/`End` staging). rclone talks to
the cloud directly.

The exclusive mutator for remote writes is `internal/app`’s file lock
(`remnix.lock`), used by the daemon, manual sync (via RPC when the daemon
is up), GC, and key/device ops. Normal history mutations go through the
daemon so SQLite and the RAM cache stay coherent.

## Scale test

`TestScaleMillionUniqueCommands` in `internal/history` seeds unique commands
into a temp SQLite file and reports wall time for the interactive paths:

| Step | What it exercises |
| --- | --- |
| `ctrl+r unique list (limit 5000)` | Widget load (`history.Filter{Unique: true, Limit: 5000}`) |
| `ctrl+r rank fuzzy query` | In-memory ranking used after Ctrl+R / `remnix search --interactive` |
| `ctrl+r unique list + cwd` | Same unique scan filtered by working directory |
| `ghost-text end-to-end` | Prefix SQL + `search.BestSuggestion` (inline completion) |
| `lsp-style menu end-to-end` | Prefix SQL + `search.Suggestions` with `suggest.menu_max` |
| `sync encrypt checkpoint snapshot` | gzip + AEAD snapshot of all rows |
| `sync encrypt event bundle` | CBOR event encode + `bundle.Pack` / unpack |

Skipped unless `REMNIX_SCALE=1` is set (`go test ./...` and `-short` skip it).
Default size is 1 000 000 unique commands; override with `REMNIX_SCALE_N`.

```sh
REMNIX_SCALE=1 go test ./internal/history/ -run TestScaleMillionUniqueCommands -timeout 45m -v
```

Further reading: [sync-protocol.md](sync-protocol.md),
[cryptography.md](cryptography.md), [rclone.md](rclone.md),
[threat-model.md](threat-model.md).
