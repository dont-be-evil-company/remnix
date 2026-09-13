---
title: Configuration
excerpt: config.yaml, sidecar ignore files, paths, and every setting remnix reads.
description: Portable remnix configuration - files, environment overrides, YAML keys, ignore lists, and how the daemon reloads them.
order: 6
---

Portable settings live in `config.yaml`. Machine identity and credentials stay
next to the database, not in that file, so the YAML can be copied or committed.

```yaml path=~/.config/remnix/config.yaml
version: 2
disable_auto_migrate: false
sync:
    enabled: false
    interval: 5m
    gc_interval: 1h
    rclone_engine: embedded
    endpoints: []
    callbacks: []
daemon:
    compact_interval: 5m
    config_watch_interval: 30s
suggest:
    enabled: true
    menu: false
    menu_max: 8
    completions: false
    accept:
        - Right
pty_proxy:
    enabled: false
    height: 100
```

Editors that speak YAML language servers can load
[config.schema.json](https://remnix.app/config.schema.json):

```yaml
# yaml-language-server: $schema=https://remnix.app/config.schema.json
```

`remnix` writes that comment when it saves. Unknown YAML keys are rejected
(`additionalProperties: false`). A pre-v2 file with `sync.transport` or the
old nested `sync.rclone` object is refused; run `remnix setup`.

Interactive changes: `remnix config`, `remnix config sync`, and
`remnix remote ...`. The [configuration wizard](/docs/config-wizard) saves
only on success.

## Files

Defaults follow XDG. Override the directories with environment variables;
filenames inside them are fixed.

| Path | Purpose |
| --- | --- |
| `$XDG_CONFIG_HOME/remnix/config.yaml` | Portable settings (no secrets). Default `~/.config/remnix/config.yaml` |
| `$XDG_CONFIG_HOME/remnix/ignore-commands.txt` | Exact commands that must not be recorded |
| `$XDG_CONFIG_HOME/remnix/ignore-commands.regex` | Regex patterns for commands that must not be recorded |
| `$XDG_DATA_HOME/remnix/local.yaml` | This machine’s `device_id` and `device_name` |
| `$XDG_DATA_HOME/remnix/rclone.conf` | rclone credentials (mode 0600) |
| `$XDG_DATA_HOME/remnix/history.db` | Local history and key metadata |
| `$XDG_DATA_HOME/remnix/daemon-status.json` | Last daemon sync result |
| `$XDG_DATA_HOME/remnix/setup-state.json` | Crash-safe setup marker |
| `$XDG_RUNTIME_DIR/remnix/control.sock` | Daemon RPC (suggest, history, overlays) |
| `$XDG_RUNTIME_DIR/remnix/terminal.sock` | Daemon terminal attach socket |

| Variable | Overrides |
| --- | --- |
| `REMNIX_CONFIG_DIR` | Directory that holds `config.yaml` and the ignore files |
| `REMNIX_DATA_DIR` | Directory that holds `local.yaml`, `rclone.conf`, and `history.db` |
| `REMNIX_RUNTIME_DIR` | Directory that holds the control and terminal sockets |

A leftover `rclone.conf` under the config directory is moved into the data
directory on startup. Do not copy `local.yaml` to another machine; that
device needs its own id. See [rclone](/docs/rclone) for where encrypted
objects live on remotes.

## Reload

The daemon reloads `config.yaml` on `SIGHUP`, `remnix daemon reload`, or when
the file’s mtime changes (polled every `daemon.config_watch_interval`). Sync
interval picks up immediately.

Ignore files are not part of that YAML watch. Recording re-reads them when
their mtime or presence changes, so new ignore rules apply to the next
command without a daemon restart.

Shell-generated glyphs and hooks come from `remnix init`. After changing
`ui` colors/icons, `suggest`, or `pty_proxy`, re-source init (or start a new
shell). Installing a new binary is not enough.

## `version` and migrations

| Key | Default | Meaning |
| --- | --- | --- |
| `version` | `2` | Config format. remnix writes `2`. Older files are bumped on load |
| `disable_auto_migrate` | `false` | Skip automatic SQLite migrations when opening the database |

## `sync`

Encrypted history synchronization. Details of bundles, checkpoints, and
equalize are in the [sync protocol](/docs/sync-protocol). Provider-specific
rclone notes are in [rclone](/docs/rclone).

| Key | Default | Meaning |
| --- | --- | --- |
| `sync.enabled` | on if any endpoint is enabled | Master switch. `false` keeps local history and does not delete remotes |
| `sync.interval` | `5m` | How often the daemon syncs. Go duration (`30s`, `5m`, `1h`). Values under 1s fall back to `5m` |
| `sync.gc_interval` | `1h` | How often the daemon runs remote garbage collection |
| `sync.rclone_engine` | `embedded` | `embedded` uses the bundled rclone library. `external` uses an rclone binary on `PATH` |
| `sync.endpoints` | `[]` | Mirrors of the **same** encrypted repository. Sync fans out to every enabled endpoint |
| `sync.callbacks` | `[]` | Commands run after a successful sync. Do not put secrets here |

`remnix sync` uses every enabled endpoint; `remnix sync --endpoint=<id>`
targets one. `remnix remote add` appends; it does not replace existing
endpoints or mint a new Sync Master Key.

### Endpoints

Each endpoint needs a unique `id` and a `type`. `name` is the display label
(falls back to `id`). `enabled` opts it out of sync without deleting it.

Paths in `path` (and other user-supplied location strings) keep `$HOME`,
`${VAR}`, and `~/` as written. remnix expands them when the path is **used**,
not when the file is saved.

| `type` | Fields | Notes |
| --- | --- | --- |
| `rclone` | `rclone_remote`, `provider`, `path` | Default for new setups. `rclone_remote` is the section name in `rclone.conf`, not a token. `provider` is a wizard id: `s3`, `gcs`, `dropbox`, `azure-files`, `icloud-drive`, `onedrive`, `google-drive`, `webdav`, `smb`, `custom` |
| `directory` | `path` | Local or already-mounted folder. That folder **is** the repository |
| `rsync` | `remote` | `user@host:path` session transport |
| `scp` | `host`, `user`, `port`, `path` | SSH session transport. `port` is 1-65535 |

```yaml
sync:
    enabled: true
    rclone_engine: embedded
    endpoints:
        - id: google-drive
          type: rclone
          name: Google Drive
          rclone_remote: remnix-google-drive
          provider: google-drive
          path: remnix
          enabled: true
        - id: local-mirror
          type: directory
          path: ~/src/remnix-backup
          enabled: false
```

On S3/GCS the first path component is the bucket. Callbacks that run an
external `rclone sync` against a **directory** endpoint still work; they are
not the native rclone transport.

## `daemon`

Background cadences for the long-lived `remnix daemon`.

| Key | Default | Meaning |
| --- | --- | --- |
| `daemon.compact_interval` | `5m` | How often the daemon prunes the RAM history cache and returns unused memory |
| `daemon.config_watch_interval` | `30s` | How often to `stat` `config.yaml` for reload. Values under 1s fall back to `30s` |

`remnix daemon compact` runs the same cache prune on demand.

## `suggest`

Inline history completion (ghost text) and the suggestion menu. Ranking is
unchanged by these keys: match quality, recency, frequency, cwd, device, and
session.

| Key | Default | Meaning |
| --- | --- | --- |
| `suggest.enabled` | `true` | Ghost-text suggestions. `false` keeps Ctrl+R without ghost text |
| `suggest.menu` | `false` | LSP-style picker (zsh `POSTDISPLAY`, or overlay TUI with `pty_proxy`) |
| `suggest.menu_max` | `8` | Suggestion rows shown at once (typed row stays pinned). Capped at 32 |
| `suggest.completions` | `false` | Merge shell completions into the picker |
| `suggest.accept` | `[Right]` | Keys that accept the ghost suffix when the cursor is at the end of the line (`Right`, `Tab`, ...) |
| `suggest.icons` | see `ui.icons` | Legacy aliases: `typed`, `history`, `completion`. Prefer `ui.icons` |

Ghost text still previews the best history match. History is not listed in
the dropdown; completions sit under the typed row. Capture stops at 512
matches so a huge path completion cannot freeze the prompt.

| Shell | Ghost text | Ctrl+R | Suggest overlay (Ctrl+Space) |
| --- | --- | --- | --- |
| zsh | `POSTDISPLAY` | yes | compsys overlay when `pty_proxy` is on; else `POSTDISPLAY` menu |
| bash | ble.sh, if loaded | yes | history overlay when `suggest.menu` is on |
| fish | not supported | yes | `complete -C` overlay when `suggest.menu` is on |
| nu | not supported | yes | `--ide-complete` overlay when `suggest.menu` is on |

## `pty_proxy`

Unix only (Linux, macOS, WSL). Windows widgets use the alt-screen.

| Key | Default | Meaning |
| --- | --- | --- |
| `pty_proxy.enabled` | `false` | `remnix init` `exec`s `remnix-attach` so overlay TUIs can snapshot the live prompt |
| `pty_proxy.height` | `100` (fullscreen) | Ctrl+R overlay height. `40`, `40%`, or `0.4` are 40%. Omit, `0`, or `100` is fullscreen |

Put `eval "$(remnix init ...)"` near the top of the rc file so only the proxy
re-execs. Set `REMNIX_PTY_PROXY_LEGACY=1` to use the old per-terminal
`remnix pty-proxy` process. Without a daemon session, widgets fall back to a
local `remnix search --interactive` TUI.

Smaller heights still keep at least five history rows plus header, rule,
help, and input.

## `ui`

Colors and icons for the TUI. Empty values and the string `default` use the
built-in Catppuccin-like theme. Hex must be `#RGB` or `#RRGGBB`. Ready-made
palettes are on [themes](/themes).

### `ui.colors`

| Key | Default | Use |
| --- | --- | --- |
| `accent` | `#F5C2E7` | Highlights |
| `title` | `#CBA6F7` | Titles |
| `muted` | `#585B70` | Secondary text |
| `rule` | `#313244` | Rules / borders |
| `badge` | `#89B4FA` | Badges |
| `duration` | `#A6E3A1` | Durations |
| `failed` | `#F38BA8` | Failed commands |
| `time` | `#7F849C` | Timestamps |
| `text` | `#CDD6F4` | Body text |

### `ui.colors.syntax`

Command highlighting in search and inspect.

| Key | Default |
| --- | --- |
| `command` | `#89B4FA` |
| `keyword` | `#CBA6F7` |
| `flag` | `#FAB387` |
| `string` | `#A6E3A1` |
| `comment` | `#6C7086` |
| `operator` | `#F38BA8` |
| `variable` | `#89DCEB` |
| `path` | `#94E2D5` |
| `number` | `#F9E2AF` |
| `argument` | `#CDD6F4` |

### `ui.icons`

| Key | Default | Use |
| --- | --- | --- |
| `cursor` | `❯` | Cursor glyph |
| `suggestion_typed` | `›` | Typed row in the picker (`suggest.icons.typed` still works) |
| `suggestion_history` | `*` | History / overlay |
| `suggestion_completion` | `+` | Shell completions |
| `separator` | `·` | Separators |
| `move_up_down` | `↑↓` | Move hint |

## Ignoring commands

Optional sidecar files next to `config.yaml` skip **recording**. They do not
delete or hide rows already in `history.db`, and they do not filter search,
suggestions, inspect, or inbound sync. A command skipped at start still gets
a UUID so the shell `end` hook is a no-op.

Both files are optional. Empty lines are skipped. Surrounding whitespace is
trimmed. There are no `#` comments: a line is a pattern or it is empty.
Matching either file is enough (OR).

### `ignore-commands.txt`

One **exact full command** per line. `ls` skips `ls`, not `ls -la`.

```text path=~/.config/remnix/ignore-commands.txt
ls
ll
exit
```

### `ignore-commands.regex`

One [Go RE2](https://github.com/google/re2/wiki/Syntax) pattern per line.
The command is skipped if `MatchString` succeeds on the raw command (not
anchored unless the pattern says so). Invalid lines are skipped with a
warning so the rest of the file still applies.

```text path=~/.config/remnix/ignore-commands.regex
^sudo 
^rm -rf
```

### Built-in secret skips

Regardless of those files, remnix never records a command whose text
contains (case-insensitive):

- `REMNIX_RECOVERY_KEY=`
- `RCLONE_CONFIG_PASS=`
- `AWS_SECRET_ACCESS_KEY=`
- `AZURE_STORAGE_KEY=`

Prefer typing the recovery key interactively. Putting it on the command line
is still visible in the original shell histfile.

Import (`remnix import histfile` / `atuin`) uses the same skip rules for
**new** inserts. Existing rows are left alone.

## Commands

| Command | Purpose |
| --- | --- |
| `remnix config` / `config sync` | Interactive configuration wizard |
| `remnix remote list` / `add` / `edit` / `remove` / `test` / `reconnect` / `browse` | Endpoints |
| `remnix daemon reload` | Reload `config.yaml` now |
| `remnix doctor` | Read-only diagnostics (config version, leftover rclone.conf, ...) |

Further reading: [configuration wizard](/docs/config-wizard),
[rclone](/docs/rclone), [architecture](/docs/architecture),
[themes](/themes).
