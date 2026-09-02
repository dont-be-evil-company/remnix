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
      path: syncsh
      enabled: true
```

`sync.transport` / `rclone.primary` files are no longer supported; run
`syncsh setup`. All enabled endpoints are mirrors of the same encrypted
repository. `syncsh sync` fans out to every enabled endpoint;
`syncsh sync --endpoint=<id>` targets one. `syncsh remote add` appends.

Secrets stay in `$XDG_DATA_HOME/syncsh/rclone.conf`, not next to the
version-controllable `config.yaml`. Import copies a section from the user’s
rclone config without modifying the original.

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
duplicate parent become visible and can be collected. Dedupe also
deletes leftover `.tmp-*` objects.

## Providers

First-class wizard ids: `s3`, `gcs`, `dropbox`, `azure-files`, `icloud-drive`,
`onedrive`, `google-drive`, `webdav`, `smb`, `custom`.

Limitations (shown in the wizard, not treated as errors):

- S3/GCS: prefixes, expensive rename
- iCloud: auth can expire; reconnect is interactive (2FA). Daemon never opens
  a browser - use `syncsh remote reconnect <name>`
- SMB: a share must be selected
- WebDAV: capabilities vary; prefer HTTPS

## Callbacks vs native transport

Existing `sync.callbacks` that run `rclone sync` against a **directory**
endpoint keep working. Native rclone endpoints do not replace those
callbacks automatically.
