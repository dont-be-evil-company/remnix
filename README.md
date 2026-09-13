# `remnix`

Encrypted, server-free shell history. Commands live in a local SQLite database.
Synchronization is optional: a background daemon can copy **encrypted** event
bundles to storage you already have (Google Drive, Dropbox, S3, a folder, ...)
using an embedded rclone engine. There is no remnix cloud and no account.

Remote storage is untrusted. Encryption, key wrapping, and merge happen in
remnix - rclone only reads and writes objects.

## Install

### Pre-built binaries

**Linux / macOS:**

```sh
curl -sSL https://dont-be-evil-company.github.io/remnix/install.sh | sh
```

Installs both `remnix` and `remnix-attach` into the same directory (`~/.local/bin` or `/usr/local/bin`).

**Windows (PowerShell):**

```powershell
iwr https://dont-be-evil-company.github.io/remnix/install.ps1 -useb | iex
```

**Arch Linux (AUR):**

```sh
yay -S remnix-bin
# or: paru -S remnix-bin
```

Update an existing install:

```sh
remnix update
```

### Build from source

Go 1.25+ is required. The `piv` build tag enables YubiKey PIV support.

```sh
pnpm install && pnpm run changelog   # embed changelog for go build
task build
```

`task test` runs unit tests. `task ci` also runs `golangci-lint`.

### Scale test

`go test ./...` skips the million-command timing test. To measure **Ctrl+R**
unique search, **ghost text** / **LSP-style suggest menu**, and **sync
encrypt** (checkpoint snapshot + event bundle) against 1 000 000 unique
commands:

```sh
REMNIX_SCALE=1 go test ./internal/history/ -run TestScaleMillionUniqueCommands -timeout 45m -v
```

Use `REMNIX_SCALE_N=10000` for a shorter dry run. `-short` skips the test.
Timings print with `-v`. See [architecture](https://remnix.app/docs/architecture#scale-test).

## Quick start

### New history

```sh
remnix setup
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
eval "$(remnix init zsh)"

# bash - add to ~/.bashrc
eval "$(remnix init bash)"

# fish - add to ~/.config/fish/config.fish
remnix init fish | source

# nushell: source is parse-time, so do not generate and source in the same file.
# env.nu (runs before config.nu is parsed):
mkdir ~/.cache
^remnix init nu | save --force ~/.cache/remnix.nu
# config.nu, near the top if pty_proxy is on:
source ~/.cache/remnix.nu
```

Prefer typing the recovery key interactively. Putting `REMNIX_RECOVERY_KEY=...`
on the command line is recorded by the shell; remnix skips inserting those
commands into history, but they may still appear in your original histfile.

### Join another device

Do **not** run `setup` again against the same remote. That would mint a second
Sync Master Key and make old history unreadable.

```sh
remnix device add --name "$(hostname)"
```

The wizard configures or imports the same rclone remote, browses to the existing
folder, and **requires a valid repository** before joining. Unlock with the
original recovery key and/or the enrolled security key:

```sh
remnix unlock
eval "$(remnix init zsh)"
```

Copying `~/.config/remnix/config.yaml` is still valid. Do **not** copy
`local.yaml`; this machine gets its own device id.

### Local-only

Choose “Use remnix locally without synchronization” in `remnix setup`, or later:

```sh
remnix config sync
```

Disable sync keeps local history. Remote data is not deleted.

## Shell integration

On zsh, `init` installs **Ctrl+R** search and **inline suggestions** (ghost
text). Right arrow accepts when the cursor is at the end of the line.
Suggestions talk to a long-lived `remnix agent` over a unix socket (or a
coproc fallback) so the shell does not spawn a process on every keystroke.
The agent is started on first use. Configure accept keys in `config.yaml`:

```yaml
pty_proxy:
  enabled: false       # Unix: wrap the shell so Ctrl+R / suggest overlay the prompt
  height: 100%         # Ctrl+R overlay; omit or 100% = full screen (alt-screen). Try 40%.
suggest:
  enabled: true
  menu: false          # LSP-style picker (zsh POSTDISPLAY, or overlay TUI with pty_proxy)
  menu_max: 8
  completions: false   # merge shell completions into the zsh POSTDISPLAY picker
  icons:
    typed: "›"
    history: "*"
    completion: "+"
  accept:
    - Right
```

Set `enabled: false` to keep Ctrl+R without ghost text. Set `menu: true` to enable
the zsh POSTDISPLAY picker (no pty-proxy). Ghost text still previews the best
history match from the local database; accept keys (Right, Tab, ...) insert only
that ghost suffix. History stays ghost text: it is not listed in the dropdown.
Set `completions: true` to list the shell’s own completers (carapace, git, and
anything else registered with compsys), with descriptions when the completer
provides them. The first row is always the text you typed (no item is selected
until you navigate). Completions sit directly under that typed row. Press Tab to
load them into the float (Tab again cycles; results are cached until the line
changes). Up/Down (and Ctrl-P/N) start at the first completion and rewrite the
line to that suggestion; Enter runs it. Esc restores the typed line and
dismisses the list. `menu_max` is the number of suggestion rows shown at once
(the typed row stays pinned). The full completion list is scrollable; a thumb
on the right edge of the box shows where you are. Capture stops at 512 matches
so a huge path completion cannot freeze the prompt. Icons are configurable so
typed, history (overlay), and completions stay distinguishable.

### Terminal proxy (overlay TUIs)

On Linux, macOS, and WSL, set `pty_proxy.enabled: true` and put `remnix init`
high in the shell rc. `init` then `exec`s `remnix-attach`, a tiny helper that
connects to the remnix daemon. The daemon owns the inner PTY, the shadow
screen, and the Ctrl+R / Ctrl+Space overlay TUIs. Ctrl+Space lists native
shell completions (zsh compsys, fish `complete -C`, nu `--ide-complete`)
instead of history; history stays ghost text. The shell widget asks the
daemon over RPC (`search-interactive` / `suggest-complete-interactive`);
keys and paint stay on the existing attach stream so a second Go process is
not spawned. Set `REMNIX_PTY_PROXY_LEGACY=1` to use the old per-terminal
`remnix pty-proxy` Go process for one release. Without a daemon session, the
same widgets fall back to a local `remnix search --interactive` TUI.
`pty_proxy.height` is a percent of the terminal (for example `40` or `40%`).
Omit it, or set `100%`, for a fullscreen overlay. Smaller values still keep
at least five history rows plus the header, rule, help line, and input box.

After changing hooks, re-run `eval "$(remnix init zsh)"` (or start a new
shell). Installing a new binary is not enough.

Ghost text stays in each shell’s line editor:

| Shell | Ghost text | Ctrl+R | Suggest overlay (Ctrl+Space) |
| --- | --- | --- | --- |
| zsh | `POSTDISPLAY` | yes | compsys overlay when `pty_proxy` is on; else POSTDISPLAY menu |
| bash | ble.sh, if loaded | yes | history overlay when `suggest.menu` is on |
| fish | not supported (no `POSTDISPLAY`) | yes | `complete -C` overlay when `suggest.menu` is on |
| nu | not supported | yes | `--ide-complete` overlay when `suggest.menu` is on |

Windows has no pty-proxy; widgets use the alt-screen. Put `eval "$(remnix init ...)"`
near the top of the rc file so only the proxy re-execs, not the rest of your
startup. For Nushell, put `^remnix init nu | save --force ~/.cache/remnix.nu`
in `env.nu` and `source ~/.cache/remnix.nu` with a literal path at the top of
`config.nu`. Regenerating in `config.nu` itself cannot work: `source` is
parse-time and will not see the file written on that same run.

## Search / suggestions

```sh
remnix                            # TUI (unique commands)
remnix search --interactive       # Ctrl+R widget
remnix search git
remnix search --explain git
remnix suggest --prefix 'git st' --cwd "$PWD"
remnix suggest --prefix 'git st' --cwd "$PWD" --list
remnix suggest --interactive --prefix 'git st' --cwd "$PWD"
remnix stats
remnix inspect                    # history stats TUI (`explore` alias)
remnix import histfile ~/.histfile
remnix import atuin ~/.local/share/atuin/history.db
```

Ranking is a deterministic weighted score: match quality (prefix over fuzzy),
recency, frequency, cwd, device, and session. The daemon keeps a compact
in-memory command index; SQLite remains the durable source of truth.

Colors and icons are configurable under `ui` in `config.yaml` (Catppuccin-like
defaults). `suggest.icons` still works as an alias. After editing config,
`remnix daemon` reloads on SIGHUP, `remnix daemon reload`, or the next mtime poll; `remnix init` must
be re-sourced for shell-generated glyphs.

## Synchronization

Embedded rclone is the default transport. Existing `directory`, `rsync`, and
`scp` configs keep working and are **not** auto-converted.

```sh
remnix config                 # wizard: enable/disable, transport, callbacks
remnix remote list
remnix remote add
remnix remote test
remnix remote reconnect <name>   # iCloud / OAuth expiry
remnix sync
remnix sync status
```

Credentials live in `$XDG_DATA_HOME/remnix/rclone.conf` (mode 0600), next to
`local.yaml`. Portable `config.yaml` only stores remote **names** and paths -
it stays commit-safe. A leftover `rclone.conf` under `~/.config/remnix/` is
moved on startup.

Post-sync `sync.callbacks` (for example an external `rclone sync` of a
directory-transport folder) still run. They are not the native rclone
transport. See [rclone](https://remnix.app/docs/rclone).

Files:

| Path | Purpose |
| --- | --- |
| `~/.config/remnix/config.yaml` | Portable settings (no secrets) |
| `~/.local/share/remnix/rclone.conf` | rclone credentials (not for git) |
| `~/.local/share/remnix/local.yaml` | This machine’s device id and name |
| `~/.local/share/remnix/history.db` | Local history and key metadata |
| `~/.local/share/remnix/daemon-status.json` | Last daemon sync result |
| `$XDG_RUNTIME_DIR/remnix/control.sock` | Unified daemon control RPC (suggest / history / overlay / stats) |
| `$XDG_RUNTIME_DIR/remnix/terminal.sock` | Daemon terminal attach socket |
| `~/.local/share/remnix/setup-state.json` | Crash-safe setup marker |

Override locations with `REMNIX_CONFIG_DIR` and `REMNIX_DATA_DIR`. Paths in
user configuration (`$HOME`, `${VAR}`, `~/`) are expanded when used, not when saved.

## Keys / recovery

Each device wraps a **Sync Master Key** (`SMK`) in slots: a `bech32` recovery
key (`remnix1...`), optional FIDO2 `hmac-secret`, optional YubiKey PIV. The
daemon stores the unwrapped SMK in the OS keyring after `remnix unlock`.

`remnix key rotate` creates a new SMK generation and keeps the previous
generation so existing encrypted history remains readable. Rotation is
retry-safe. If a storage or local-state error interrupts rotation, rerunning
the command first reconciles any in-progress rotation instead of blindly
creating another generation. A generation is not considered active until the
authenticated repository manifest selects it. Rerun the same command after an
ambiguous error rather than editing generation files by hand. `remnix key
recover` is for genuine fork or corruption states, not ordinary transient
failures. Old generations remain until GC can prove they are no longer
referenced.

```sh
remnix unlock
remnix key status
remnix key fido add
remnix key yubikey add
remnix key recovery rotate
remnix key rotate
remnix key recover [generation-id]
```

If `setup` was run twice on the same remote:

```sh
remnix key recover
```

See [cryptography](https://remnix.app/docs/cryptography).

## Garbage collection

When every active device has acknowledged a checkpoint, older event bundles
can be deleted. A device that never acks blocks GC until it catches up or is
explicitly retired. Do not delete remote files by hand.

Garbage collection is retry-safe: if a storage or network error interrupts
cleanup after some objects were deleted, rerunning `remnix gc` recomputes the
current safe set and continues. Objects that were already removed are ignored.
`remnix gc --dry-run` reports that set without deleting anything.

Pruning removes a retired device from the synchronized device roster and
cleans up remote state associated with it. The manifest update is the logical
commit. If cleanup is interrupted after the manifest has been updated, running
`remnix device prune <device-id>` again safely resumes cleanup.

```sh
remnix gc --dry-run
remnix device retire <device-id>
remnix device prune <device-id>
```

## Diagnostics

```sh
remnix doctor
remnix database compact
remnix sync status
remnix daemon status
remnix version --verbose    # includes pinned rclone engine
```

`doctor` reports config, SQLite size and b-tree usage, keys, transport, repository probe, and partial
setup. `remnix database compact` checkpoints the WAL and vacuums the local history database.
The daemon never opens a browser; auth failures back off and suggest
`remnix remote reconnect`. `remnix daemon status` (and `doctor`) include human
durations for the last tick, for example `sync=1m6s gc=12ms`.

## Advanced

Protocol, threat model, wizard keys, and rclone internals:

- [architecture](https://remnix.app/docs/architecture) (includes the 1M-command scale test)
- [sync protocol](https://remnix.app/docs/sync-protocol)
- [cryptography](https://remnix.app/docs/cryptography)
- [threat model](https://remnix.app/docs/threat-model)
- [rclone](https://remnix.app/docs/rclone)
- [config wizard](https://remnix.app/docs/config-wizard)

### Command reference

| Command | Purpose |
| --- | --- |
| `remnix setup` | First device: identity, transport, SMK |
| `remnix device add` | Join an existing remote |
| `remnix config` / `config sync` | Reusable configuration wizard |
| `remnix remote ...` | List/add/edit/remove/test/reconnect/browse |
| `remnix unlock` | SMK into the OS keyring |
| `remnix sync` / `sync status` | Pull/push now; health + probe |
| `remnix daemon` / `install` / `reload` / `compact` / `restart` / `status` | Background sync |
| `remnix agent` | Local SQLite RPC for suggest / history |
| `remnix key ...` | Slots, rotation, recover |
| `remnix search` / `suggest` / `stats` / `inspect` / `import` | Local history |
| `remnix gc` | Compact remote objects |
| `remnix daemon compact` | Prune the RAM history cache and return unused memory to the OS (also every 5m) |
| `remnix database compact` | Checkpoint WAL and vacuum local SQLite |
| `remnix doctor` | Read-only diagnostics |
| `remnix init zsh\|bash\|fish\|nu` | Shell integration |
| `remnix version` | Version (`-v` includes rclone) |
| `remnix update` | Download and install the latest release |
| `remnix changelog` | Show baked-in release notes (`latest` or a version) |

`REMNIX_RECOVERY_KEY` is accepted by sync, unlock, device add, and key
commands that need to unwrap the SMK.
