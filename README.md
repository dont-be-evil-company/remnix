# `syncsh`

Encrypted, server-free shell history. Commands live in a local SQLite database.
Synchronization is optional: a background daemon can copy **encrypted** event
bundles to storage you already have (Google Drive, Dropbox, S3, a folder, ...)
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

Prefer typing the recovery key interactively. Putting `SYNCSH_RECOVERY_KEY=...`
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
Suggestions talk to a long-lived `syncsh agent` over a unix socket (or a
coproc fallback) so the shell does not spawn a process on every keystroke.
The agent is started on first use. Configure accept keys in `config.yaml`:

```yaml
suggest:
  enabled: true
  menu: false          # optional LSP-style picker under the prompt (zsh)
  menu_max: 8
  completions: false   # merge shell completions into the picker
  icons:
    typed: "›"
    history: "*"
    completion: "+"
  accept:
    - Right
```

Set `enabled: false` to keep Ctrl+R without ghost text. Set `menu: true` to show a
small list under the prompt while typing. The first row is always the text you
typed (no item is selected until you navigate). Ghost text still previews the
best history match; accept keys (Right, Tab, ...) insert only that ghost suffix.
Up/Down (and Ctrl-P/N) start at the first completion or history row and rewrite
the line to that suggestion; Enter runs it. Esc restores the typed line and
dismisses the list. `menu_max` is the number of suggestion rows shown at once
(the typed row stays pinned). Set `completions: true` to also list the shell’s
own completers (carapace, git, and anything else registered with compsys), with
descriptions when the completer provides them. Completions sit directly under
the typed row; history follows. Typing only queries history; press Tab to load
completions into the float (Tab again cycles; results are cached until the line
changes). The full completion list is scrollable; a thumb on the right edge of
the box shows where you are. Capture stops at 512 matches so a huge path
completion cannot freeze the prompt. Icons are configurable so history and
completions stay distinguishable.

## Search / suggestions

```sh
syncsh                            # TUI (unique commands)
syncsh search --interactive       # Ctrl+R widget
syncsh search git
syncsh search --explain git
syncsh suggest --prefix 'git st' --cwd "$PWD"
syncsh suggest --prefix 'git st' --cwd "$PWD" --list
syncsh stats
syncsh inspect                    # history stats TUI (`explore` alias)
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
| `$XDG_RUNTIME_DIR/syncsh/agent.sock` | Local history agent (suggest / history RPC) |
| `~/.local/share/syncsh/setup-state.json` | Crash-safe setup marker |

Override locations with `SYNCSH_CONFIG_DIR` and `SYNCSH_DATA_DIR`. Paths in
user configuration (`$HOME`, `${VAR}`, `~/`) are expanded when used, not when saved.

## Keys / recovery

Each device wraps a **Sync Master Key** (`SMK`) in slots: a `bech32` recovery
key (`syncsh1...`), optional FIDO2 `hmac-secret`, optional YubiKey PIV. The
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
`syncsh remote reconnect`. `syncsh daemon status` (and `doctor`) include human
durations for the last tick, for example `sync=1m6s gc=12ms`.

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
| `syncsh remote ...` | List/add/edit/remove/test/reconnect/browse |
| `syncsh unlock` | SMK into the OS keyring |
| `syncsh sync` / `sync status` | Pull/push now; health + probe |
| `syncsh daemon` / `install` / `status` | Background sync |
| `syncsh agent` | Local SQLite RPC for suggest / history |
| `syncsh key ...` | Slots, rotation, recover |
| `syncsh search` / `suggest` / `stats` / `inspect` / `import` | Local history |
| `syncsh gc` | Compact remote objects |
| `syncsh doctor` | Read-only diagnostics |
| `syncsh init zsh\|bash\|fish` | Shell integration |
| `syncsh version` | Version (`-v` includes rclone) |

`SYNCSH_RECOVERY_KEY` is accepted by sync, unlock, device add, and key
commands that need to unwrap the SMK.
