# syncsh

Encrypted, server-free shell history. Commands are stored in a local SQLite
database and synchronized as encrypted events through a folder you already
trust (Google Drive, Dropbox, Syncthing, rsync, or scp). There is no syncsh
cloud and no account.

## How it works

Each device writes history to `$XDG_DATA_HOME/syncsh/history.db` (default
`~/.local/share/syncsh/history.db`). A **Sync Master Key** (SMK) wraps every
event bundle and checkpoint. The SMK itself is wrapped in one or more **slots**:

- a bech32 **recovery key** (`syncsh1…`), shown once at setup and never stored
  on the remote
- optional **FIDO2 hmac-secret** (YubiKey, Security Key over USB HID)
- optional **YubiKey PIV**

Devices exchange ciphertext through the configured transport. A login daemon
syncs in the background after you unlock once (the SMK is kept in the OS
keyring).

| Path | Purpose |
| --- | --- |
| `~/.config/syncsh/config.yaml` | device id, transport, database path |
| `~/.local/share/syncsh/history.db` | local history and key metadata |
| `~/.local/share/syncsh/daemon-status.json` | last daemon sync result |

Override locations with `SYNCSH_CONFIG_DIR` and `SYNCSH_DATA_DIR`.

## Install

Go 1.25+ is required. The `piv` build tag enables YubiKey PIV support.

```sh
make build
mv bin/syncsh "$HOME/.local/bin/syncsh"
```

`make test` runs the unit tests. `make ci` also runs `golangci-lint`.

## First device: setup

Run **`syncsh setup` only on the first machine**. It creates a new SMK,
writes the remote layout, prints a recovery key, and can enroll a hardware
key and install the login daemon.

```sh
syncsh setup
```

Store the recovery key offline. It is not written to the remote. If the OS
keyring is unavailable, unlock later with:

```sh
SYNCSH_RECOVERY_KEY='syncsh1…' syncsh unlock
```

Then enable the shell widget (Ctrl+R) and command recording:

```sh
# zsh - add to ~/.zshrc
eval "$(syncsh init zsh)"

# bash - add to ~/.bashrc
eval "$(syncsh init bash)"

# fish - add to ~/.config/fish/config.fish
syncsh init fish | source
```

On zsh, `init` also installs **inline suggestions**: as you type, the rest of
the most recent matching command appears in dim text. Right arrow accepts it
when the cursor is at the end of the line (otherwise it still moves the
cursor). Re-run `eval "$(syncsh init zsh)"` after changing these keys.

```yaml
# ~/.config/syncsh/config.yaml
suggest:
  enabled: true
  accept:
    - Right          # default; also: Tab, End, C-e, C-f, or a raw bindkey like '^[[C'
    # - Tab          # also accept with Tab (skips completion while a suggestion is shown)
```

`accept` is a list; every entry is bound. Named keys: `Right`, `Tab`, `End`,
`C-e`, `C-f`. Anything else is passed to zsh `bindkey` as-is. Set
`enabled: false` to keep Ctrl+R search without ghost text. Bash and fish do
not have inline suggestions yet.

## Additional devices: join, do not run setup

A second machine must **join** the existing remote. Running `setup` again
against the same folder creates a **new** SMK (generation `seq=1`) and
overwrites `metadata/manifest`, while leaving the old
`keys/generations/<id>/manifest` files in place. History encrypted with the
original SMK becomes unreadable, and the daemon can fail with:

```
UNIQUE constraint failed: key_generations.seq
```

On the new device:

1. Copy or create `~/.config/syncsh/config.yaml` pointing at the **same**
   remote path (or run setup’s prompts only if this device has no remote yet -
   prefer writing the config by hand / copying it).
2. Join:

```sh
# unlock with the original recovery key and/or plug the enrolled security key
SYNCSH_RECOVERY_KEY='syncsh1…' syncsh device add --name "$(hostname)"
eval "$(syncsh init zsh)"
```

`device add` registers this device, pulls generations, restores history from
the newest **checkpoint** when event bundles have already been garbage-collected,
and stores the SMK in the keyring.

### Accidental second setup

If `setup` was run more than once on the same remote:

```sh
# plug the original YubiKey / Security Key, or export the original recovery key
syncsh key recover
```

With no argument, recover adopts the generation named by the newest checkpoint
(the one that still has your history), replaces the local fork, republishes
`metadata/manifest`, and loads the checkpoint snapshot.

If the authenticator has a PIN, run recover in a real terminal so the PIN
can be typed (it is never stored). After it succeeds:

```sh
syncsh key status
syncsh stats
syncsh daemon install   # or: systemctl --user start syncsh-daemon.service
```

Leftover `keys/generations/<other-id>/` directories on the remote are ignored
after recover. You can delete them once `key status` shows the recovered
generation as `active`.

## Unlock and the daemon

The daemon cannot prompt for a FIDO touch every minute. Unlock once per
session (or after reboot, depending on the keyring):

```sh
syncsh unlock                          # FIDO touch / PIV, or
SYNCSH_RECOVERY_KEY='syncsh1…' syncsh unlock
syncsh daemon install
syncsh daemon status
```

On Linux this installs `~/.config/systemd/user/syncsh-daemon.service`. The
daemon records success or failure in `daemon-status.json`; `syncsh doctor`
reads that file.

Manual sync:

```sh
syncsh sync
syncsh sync status
```

## Keys

```sh
syncsh key status                 # active generation, slots
syncsh key fido add               # enroll FIDO2 hmac-secret (USB HID, not NFC)
syncsh key fido remove
syncsh key yubikey add            # enroll YubiKey PIV
syncsh key yubikey remove
syncsh key recovery rotate        # new recovery key; same SMK
syncsh key rotate                 # new SMK generation (re-wrap slots)
syncsh key recover [generation-id]
```

`key rotate` creates generation `seq+1` and keeps the previous generation as
`retained` so older bundles still decrypt. That is different from running
`setup` twice, which produces two generations that both claim `seq=1`.

FIDO-only Security Keys have no PIV applet. Plug them in over USB and use
`syncsh key fido add`. Do not install `pcscd` for those devices.

## History, search, import

```sh
syncsh                            # TUI (unique commands)
syncsh search --interactive       # Ctrl+R widget
syncsh search git
syncsh stats
syncsh import histfile ~/.histfile
syncsh import atuin ~/.local/share/atuin/history.db
```

The shell hook records `history start` / `history end` for each command.
Tombstones (deletes from the TUI) propagate to other devices on the next sync.

## Garbage collection

When every active device has acknowledged a checkpoint, older event bundles
can be deleted. The checkpoint snapshot is the bootstrap source for a device
that joins later.

```sh
syncsh gc --dry-run
syncsh gc
syncsh gc status
syncsh device list
syncsh device retire <device-id>   # required before GC if a machine is gone
```

A device that never acks blocks GC. Retire it instead of deleting its files
by hand.

## Transports

Configured in `config.yaml` under `sync.transport`:

| Value | Remote |
| --- | --- |
| `directory` (default) | `sync.directory.path` - any synced folder |
| `rsync` | `sync.rsync.remote` |
| `scp` | `sync.scp.host` / `user` / `path` / `port` |

The remote layout is:

```
metadata/manifest              # signed active generation + device list
metadata/devices/<id>.json
keys/generations/<id>/manifest
events/<device-id>/…           # encrypted bundles
checkpoints/<id>/manifest
checkpoints/<id>/snapshot      # gzip+encrypted compact history
acks/<device-id>.ack
```

## Diagnostics

```sh
syncsh doctor
syncsh database status
syncsh database doctor
```

`doctor` checks config, SQLite integrity, migrations, keyring, FIDO/PIV,
transport, and the last daemon sync.

### `UNIQUE constraint failed: key_generations.seq`

Two generation manifests on the remote share the same `seq` (almost always
`1`) because `setup` was run more than once. Current syncsh skips leftover
forks and keeps the generation named in `metadata/manifest`. If that active
generation is the **new** empty fork, recover the original:

```sh
syncsh key recover
```

If you intended to start over and do not need old history, delete the unused
`keys/generations/<id>/` directories and keep the generation in
`metadata/manifest`.

### `no active key generation` / empty keyring

```sh
SYNCSH_RECOVERY_KEY='syncsh1…' syncsh unlock
# or plug the enrolled key and:
syncsh unlock
```

### `remote already initialized`

This device tried to run `setup` against a folder that already has
`metadata/manifest`. Use `syncsh device add` or `syncsh key recover`.

## Command reference

| Command | Purpose |
| --- | --- |
| `syncsh setup` | First device only: identity, transport, SMK |
| `syncsh device add` | Join an existing remote |
| `syncsh device list` / `retire` | Device roster |
| `syncsh unlock` | Wrap SMK into the OS keyring |
| `syncsh sync` | Pull/push now |
| `syncsh daemon` / `install` / `status` | Background sync |
| `syncsh key …` | Slots, rotation, recover |
| `syncsh search` / `suggest` / `stats` / `import` | Local history |
| `syncsh gc` | Compact remote objects |
| `syncsh doctor` | Read-only diagnostics |
| `syncsh init zsh\|bash\|fish` | Shell integration |
| `syncsh completion bash\|zsh\|fish` | Completions |

`SYNCSH_RECOVERY_KEY` is accepted by sync, unlock, device add, and key
commands that need to unwrap the SMK.
