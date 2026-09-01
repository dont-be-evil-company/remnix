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

`config.yaml`:

```yaml
sync:
  enabled: true          # omit on legacy files; missing must not disable
  transport: rclone
  rclone:
    engine: embedded
    primary: syncsh-google-drive
    remotes:
      - id: syncsh-google-drive
        rclone_remote: syncsh-google-drive
        provider: google-drive
        path: syncsh
        enabled: true
```

Secrets stay in `$XDG_DATA_HOME/syncsh/rclone.conf`, not next to the
version-controllable `config.yaml`. Import copies a section from the user’s
rclone config without modifying the original.

Google Drive remotes get `skip_gdocs` and `skip_dangling_shortcuts` so native
Docs/Sheets (size -1, `alt=media` downloads) are never listed or fetched.
They also get `use_trash=false` so overwrites and GC permanently replace
files. Drive allows multiple objects with the same name; PutAtomic uploads a
temp object, moves it into place, then deletes every other object with that
path (by file ID). Sync and GC run the same collapse on `acks/`,
`metadata/`, and `keys/` so leftover duplicates from older builds are
removed.

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
transport keep working. Native `transport: rclone` is opt-in and does not
replace those callbacks automatically.
