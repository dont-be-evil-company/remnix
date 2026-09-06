# rclone transport

syncsh embeds [rclone](https://rclone.org) **v1.73.4** as a Go library
(`github.com/rclone/rclone`). The binary talks to backends through `fs.Fs`.
There is no `exec.Command("rclone")` on the default path and no FUSE/mount.

## Engine

- Config path: `$XDG_DATA_HOME/syncsh/rclone.conf` (created 0600, directory 0700).
  A leftover file in `$XDG_CONFIG_HOME/syncsh/rclone.conf` is moved here on
  startup so the portable config directory can be version-controlled.
- Process-global rclone config: `Init` runs once; mutations take a mutex
- Featured backends (blank imports, not `backend/all`): S3, GCS, Dropbox,
  Azure Files, iCloud Drive, OneDrive, Google Drive, WebDAV, SMB, plus local
  and memory for tests
- `syncsh version --verbose` prints the pinned engine string
- Release builds stay `CGO_ENABLED=0`

## Config

`config.yaml` (version 2):

```yaml
sync:
  enabled: true
  rclone_engine: embedded
  endpoints:
    - id: google-drive
      type: rclone
      rclone_remote: syncsh-google-drive
      provider: google-drive
      path: syncsh
      enabled: true
    - id: s3
      type: rclone
      rclone_remote: syncsh-s3
      provider: s3
      path: my-bucket/syncsh
      enabled: true
```

On S3/GCS the first path component is the **bucket**. A scoped IAM user does
not need `s3:ListAllMyBuckets` or `s3:CreateBucket`. The wizard asks for the
bucket and an optional prefix: listing is `s3:ListBucket` on that bucket
(with `s3:prefix` if the policy is prefix-scoped). Sync itself also needs
`s3:GetObject`, `s3:PutObject`, and `s3:DeleteObject` on the object keys.

`sync.transport` / `rclone.primary` files are no longer supported; run
`syncsh setup`. All enabled endpoints are mirrors of the same encrypted
repository. `syncsh sync` fans out to every enabled endpoint;
`syncsh sync --endpoint=<id>` targets one. `syncsh remote add` appends.

Secrets stay in `$XDG_DATA_HOME/syncsh/rclone.conf`, not next to the
version-controllable `config.yaml`. Import copies a section from the user’s
rclone config without modifying the original.

## Where data lives

The encrypted repository (`metadata/`, `keys/`, `events/`, `checkpoints/`,
`acks/`) is **only** on configured endpoints. An rclone Google Drive
endpoint with `path: syncsh` is `gdrive:syncsh` on Drive (My Drive →
`syncsh`), not a folder on this machine. Sync and GC talk to that path
through the Drive API.

On the device, under `$XDG_DATA_HOME/syncsh` (default
`~/.local/share/syncsh`):

- `history.db` - local shell history (plus SQLite `-wal`/`-shm`)
- `local.yaml` - device id and local state
- `rclone.conf` - provider credentials
- `syncsh.lock`, `daemon-status.json` - daemon lock and status

Portable settings are `$XDG_CONFIG_HOME/syncsh/config.yaml`. The agent
socket is `$XDG_RUNTIME_DIR/syncsh/control.sock` (else `/tmp/syncsh/`).
Atomic upload temps (`.tmp-<uuid>`) are remote objects on backends that
still use tmp+rename; Drive writes in place.

A path like `~/GoogleDrive/syncsh` is the repository only if an endpoint
is `type: directory` with that `path`. Otherwise it is unrelated local
files (for example an old copy or a Drive desktop client mirror) and
syncsh will not GC it.

Google Drive remotes get `skip_gdocs` and `skip_dangling_shortcuts` so native
Docs/Sheets (size -1, `alt=media` downloads) are never listed or fetched.
They also get `use_trash=false` so overwrites and GC permanently replace
files. Drive allows multiple objects **and folders** with the same name;
PutAtomic writes in place (no tmp+rename, which left `.tmp-*` names in
local Drive mirrors) and then deletes every other object with that path
(by file ID). Extra `Mkdir` is skipped because Drive `Put` already creates
parents - a Mkdir after a dir-cache flush used to spawn a second
`checkpoints/` folder that GC could not see. Sync and GC collapse
duplicate files and merge same-named directories (`MergeDirs`) starting
at the repository root so leftover checkpoint UUID dirs in a hidden
duplicate parent become visible and can be collected. The same Mkdir
bug can also leave a second `syncsh` folder next to the live repo;
rclone `gdrive:syncsh` is bound to one folder ID, so a Drive name query
merges those siblings into the live root (never listing all of My Drive).
After merge, GC sees every UUID dir and keeps the best checkpoint.
Dedupe also deletes leftover `.tmp-*` objects.

## Providers

First-class wizard ids: `s3`, `gcs`, `dropbox`, `azure-files`, `icloud-drive`,
`onedrive`, `google-drive`, `webdav`, `smb`, `custom`.

Limitations (shown in the wizard, not treated as errors):

- S3/GCS: prefixes, expensive rename; wizard asks for a bucket (account-wide
  bucket listing is not required)
- iCloud: auth can expire; reconnect is interactive (2FA). Daemon never opens
  a browser - use `syncsh remote reconnect <name>`
- SMB: a share must be selected
- WebDAV: capabilities vary; prefer HTTPS

## Callbacks vs native transport

Existing `sync.callbacks` that run `rclone sync` against a **directory**
endpoint keep working. Native rclone endpoints do not replace those
callbacks automatically.
