---
title: Configuration wizard
excerpt: Setup, config, and local/remote filesystem pickers.
description: How remnix setup, device add, config, and remote pickers share wizard components.
order: 6
---

`remnix setup`, `remnix device add`, `remnix config`, and `remnix remote ...`
share wizard components in `internal/tui/wizard` and `internal/tui/picker`.

## Setup

- Welcome: Create / Join / Local-only / Exit (`setup` preselects Create,
  `device add` preselects Join)
- Create default endpoint type is rclone. Additional endpoints are added with
  `remnix remote add` (append; does not replace existing ones).
- Join requires `repository.Probe == Valid` on the endpoint being joined and
  never initializes that remote
- Crash-safe marker: `$XDG_DATA_HOME/remnix/setup-state.json`. Retry reuses
  the SMK already minted. `remnix doctor` reports a partial setup. Delete the
  file only to start over (unpublished local keys are cleared)

## Config

Draft is in memory. `Config.Save` runs only on success. Adding or removing
endpoints does not regenerate the SMK. Disabling sync keeps local history and
does not delete remotes.

rclone OAuth tokens are written to a temporary section, renamed on commit,
deleted on cancel. The file is `$XDG_DATA_HOME/remnix/rclone.conf` (beside
`local.yaml`), never `config.yaml`.

## Local picker

| Key                | Action                                                   |
| ------------------ | -------------------------------------------------------- |
| Enter              | Open directory                                           |
| Space / Ctrl+Enter | Select current directory                                 |
| Tab                | Autocomplete path (`~`, `$HOME`, `${VAR}`)               |
| `n`                | mkdir                                                    |
| `r`                | rename                                                   |
| `d`                | delete (empty vs recursive; type the name for recursive) |
| Ctrl+L             | Edit path                                                |
| hidden toggle      | Session-only                                             |

Protected deletes: `/`, drive root, `$HOME`, remnix config/data roots.
Directory deletes use `Lstat` so a directory symlink is not followed.

Non-empty destinations: Create subfolder / Select another / Proceed anyway /
Cancel. An existing **valid** repository offers Join, never Proceed anyway.
Partial repositories offer inspect, not default init.

## Remote picker

Enter opens a folder; Space or Ctrl+Enter selects. `n` creates a folder.
After a selection, if the path is not already a remnix repository, the wizard
offers to create a `remnix` subfolder so other Drive files are not scanned.

Join still requires `repository.Probe == Valid`. Creating a brand-new folder
on a second device is not enough - run `remnix setup` on the first device, or
pick the folder that already contains `metadata/`.

Object stores use virtual prefixes. Rename is disabled or warned when
expensive. Recursive delete counts children first. Paths named
`metadata`, `keys`, `events`, `checkpoints`, or `acks` cannot be deleted
while browsing an active repository.
