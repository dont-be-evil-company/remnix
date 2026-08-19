# `syncsh`

Encrypted, server-free shell history. Commands live in a local SQLite database.
Synchronization is optional: a background daemon can copy **encrypted** event
bundles to storage you already have (Google Drive, Dropbox, S3, a folder, …)
using an embedded rclone engine. There is no syncsh cloud and no account.

Remote storage is untrusted. Encryption, key wrapping, and merge happen in
syncsh - rclone only reads and writes objects.

## Install

Go 1.25+ is required. The `piv` build tag enables YubiKey PIV support.

```sh
task build
```

`task test` runs unit tests. `task ci` also runs `golangci-lint`.

## Quick start

### New history

```sh
syncsh setup
```

The wizard:

1. Create a new history (or join / local-only)
2. rclone (recommended) → provider → authorize → folder
3. Prints a recovery key (store it offline; it is never written to the remote)
4. Optional hardware key (FIDO2 over USB HID, or YubiKey PIV)
5. Optional login daemon

Then enable the shell widget:

```sh
# zsh - add to ~/.zshrc
eval "$(syncsh init zsh)"

# bash - add to ~/.bashrc
eval "$(syncsh init bash)"

# fish - add to ~/.config/fish/config.fish
syncsh init fish | source
```

Prefer typing the recovery key interactively. Putting `SYNCSH_RECOVERY_KEY=…`
on the command line is recorded by the shell; syncsh skips inserting those
commands into history, but they may still appear in your original histfile.

### Join another device

Do **not** run `setup` again against the same remote. That would mint a second
Sync Master Key and make old history unreadable.

```sh
syncsh device add --name "$(hostname)"
```

The wizard configures or imports the same rclone remote, browses to the existing
folder, and **requires a valid repository** before joining. Unlock with the
original recovery key and/or the enrolled security key:

```sh
syncsh unlock
eval "$(syncsh init zsh)"
```

Copying `~/.config/syncsh/config.yaml` is still valid. Do **not** copy
`local.yaml`; this machine gets its own device id.

### Local-only

Choose “Use syncsh locally without synchronization” in `syncsh setup`, or later:

```sh
syncsh config sync
```

Disable sync keeps local history. Remote data is not deleted.

## Shell integration

On zsh, `init` installs **Ctrl+R** search and **inline suggestions** (ghost
text). Right arrow accepts when the cursor is at the end of the line.
Configure accept keys in `config.yaml`:

```yaml
suggest:
  enabled: true
  accept:
    - Right
```

Set `enabled: false` to keep Ctrl+R without ghost text.

## Search / suggestions

```sh
syncsh                            # TUI (unique commands)
syncsh search --interactive       # Ctrl+R widget
syncsh search git
syncsh search --explain git
syncsh suggest --prefix 'git st' --cwd "$PWD"
syncsh stats
syncsh import histfile ~/.histfile
syncsh import atuin ~/.local/share/atuin/history.db
```

Ranking is a deterministic weighted score: match quality (prefix over fuzzy),
recency, frequency, cwd, device, and session. SQLite narrows candidates; Go
scores the set.

## Synchronization

Embedded rclone is the default transport. Existing `directory`, `rsync`, and
`scp` configs keep working and are **not** auto-converted.

```sh
syncsh config                 # wizard: enable/disable, transport, callbacks
syncsh remote list
syncsh remote add
syncsh remote test
syncsh remote reconnect <name>   # iCloud / OAuth expiry
syncsh sync
syncsh sync status
```

Credentials live in `$XDG_DATA_HOME/syncsh/rclone.conf` (mode 0600), next to
`local.yaml`. Portable `config.yaml` only stores remote **names** and paths -
it stays commit-safe. A leftover `rclone.conf` under `~/.config/syncsh/` is
moved on startup.

Post-sync `sync.callbacks` (for example an external `rclone sync` of a
directory-transport folder) still run. They are not the native rclone
transport. See [docs/rclone.md](docs/rclone.md).

Files:

| Path | Purpose |
| --- | --- |
| `~/.config/syncsh/config.yaml` | Portable settings (no secrets) |
| `~/.local/share/syncsh/rclone.conf` | rclone credentials (not for git) |
| `~/.local/share/syncsh/local.yaml` | This machine’s device id and name |
| `~/.local/share/syncsh/history.db` | Local history and key metadata |
| `~/.local/share/syncsh/daemon-status.json` | Last daemon sync result |
| `~/.local/share/syncsh/setup-state.json` | Crash-safe setup marker |

Override locations with `SYNCSH_CONFIG_DIR` and `SYNCSH_DATA_DIR`. Paths in
user configuration (`$HOME`, `${VAR}`, `~/`) are expanded when used, not when saved.

## Keys / recovery

Each device wraps a **Sync Master Key** (`SMK`) in slots: a `bech32` recovery
key (`syncsh1…`), optional FIDO2 `hmac-secret`, optional YubiKey PIV. The
daemon stores the unwrapped SMK in the OS keyring after `syncsh unlock`.

```sh
syncsh unlock
syncsh key status
syncsh key fido add
syncsh key yubikey add
syncsh key recovery rotate
syncsh key rotate
syncsh key recover [generation-id]
```

If `setup` was run twice on the same remote:

```sh
syncsh key recover
```

See [docs/cryptography.md](docs/cryptography.md).

## Garbage collection

When every active device has acknowledged a checkpoint, older event bundles
can be deleted. A device that never acks blocks GC - retire it.

```sh
syncsh gc --dry-run
syncsh device retire <device-id>
syncsh device prune <device-id>
```

## Diagnostics

```sh
syncsh doctor
syncsh sync status
syncsh daemon status
syncsh version --verbose    # includes pinned rclone engine
```

`doctor` reports config, SQLite, keys, transport, repository probe, and partial
setup. The daemon never opens a browser; auth failures back off and suggest
`syncsh remote reconnect`.

## Advanced

Protocol, threat model, wizard keys, and rclone internals:

- [docs/architecture.md](docs/architecture.md)
- [docs/sync-protocol.md](docs/sync-protocol.md)
- [docs/cryptography.md](docs/cryptography.md)
- [docs/threat-model.md](docs/threat-model.md)
- [docs/rclone.md](docs/rclone.md)
- [docs/config-wizard.md](docs/config-wizard.md)

### Command reference

| Command | Purpose |
| --- | --- |
| `syncsh setup` | First device: identity, transport, SMK |
| `syncsh device add` | Join an existing remote |
| `syncsh config` / `config sync` | Reusable configuration wizard |
| `syncsh remote …` | List/add/edit/remove/test/reconnect/browse |
| `syncsh unlock` | SMK into the OS keyring |
| `syncsh sync` / `sync status` | Pull/push now; health + probe |
| `syncsh daemon` / `install` / `status` | Background sync |
| `syncsh key …` | Slots, rotation, recover |
| `syncsh search` / `suggest` / `stats` / `import` | Local history |
| `syncsh gc` | Compact remote objects |
| `syncsh doctor` | Read-only diagnostics |
| `syncsh init zsh\|bash\|fish` | Shell integration |
| `syncsh version` | Version (`-v` includes rclone) |

`SYNCSH_RECOVERY_KEY` is accepted by sync, unlock, device add, and key
commands that need to unwrap the SMK.
