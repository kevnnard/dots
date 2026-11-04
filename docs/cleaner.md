# Cleaner plan

## Command line

```
dots clean <app>... | --all  [--packages] [--configs] [--backups] [--dry-run]
dots clean recover <~/.dots-quarantine/clean-*> [--dry-run]
```

- Name apps exactly or pass `--all`. `--all` means every app declared in the
  manifest, optional ones included; never every package on the machine.
- `--packages`, `--configs` and `--backups` are independent. With none of
  them, nothing is planned and the command fails without changing anything.
  An unselected packages or configurations scope is reported as
  `not selected`; without `--backups` no backup is listed or touched.
- `--backups` (off by default) moves older restore backups of the selected
  apps' files into the quarantine; see [Old backups](#old-backups).
- The manifest is read without cloning or writing anything (`--dir` selects
  the repository; without a `manifest.yaml` the built-in inventory is used).
- Both plans print first. `--dry-run` stops there. Otherwise one prompt asks
  for the whole plan and only a typed `yes` continues. Without an
  interactive terminal the command fails without changing anything; there is
  no flag to skip the prompt. With nothing removable or movable it does not
  ask.
- After `yes`, configurations run first, then old backups (both can be
  undone), then packages.
  Each runner only accepts the confirmation for exactly the summary that was
  shown: if the configuration preview changed, nothing moves and no package
  is removed. Ownership is still rechecked before every removal, and every
  file before its move.
- Failures exit non-zero; package command output is never printed. Blocked
  and unsupported items are counted as left untouched, never as success.
- `dots clean recover` previews one record, asks once and never replaces a
  file that exists again (`conflict`).

The summary match covers paths and statuses, not file identity: a file
replaced by another regular file between the preview and the run is moved.
Directory configs are not supported, the sensitive-name check does not prove
a file holds no credentials, and TOCTOU limits below apply.

## TUI

`dots` → **Clean Up Apps** runs the same planner, `NativeProbe`,
`PackageRunner` and `ConfigRunner` as `dots clean`, in process.

1. Choose **All declared apps** (every manifest app, optional ones included;
   never every package on the machine) or **Custom Selection** (checkboxes,
   all unchecked).
2. Toggle the scopes independently: `p` packages, `c` configurations, `b` old
   backups. All start off; with none, or with no app checked, nothing is
   planned. With `b` off no backup is listed or touched.
3. The preview shows both plans (read-only ownership queries only). With
   nothing removable or movable there is nothing to confirm. Otherwise type
   `yes` and press Enter; any other answer clears the input and changes
   nothing, and Esc goes back without changes.
4. After `yes`, the TUI hands the terminal to the run (`tea.Exec`) so `sudo`
   can prompt. The same rules as the CLI apply: configurations and old backups
   first, packages
   only when every confirmed file moved, and each runner accepts only exactly
   the previewed summary.
5. The summary lists every removal outcome (`removed`, `failed`, `not-run`),
   config failures and the `dots clean recover <dir>` path. "Applied" is shown
   only when the run finished without errors.

Opening Clean always starts from an empty selection, and a preview that
arrives after you left the screen is discarded.

## Planner

`clean.BuildPlan` previews which declared apps can be cleaned. It is read-only:
it runs no commands, queries no package manager and touches no files. A runner
executes the plan only after explicit confirmation.

## Selection

- `clean.Candidates(m)` lists every manifest app, `optional` ones included.
  "All" means all of these, never all packages on the machine.
- `Request.Packages` and `Request.Configs` are chosen independently and taken
  exactly: duplicates collapse, unknown apps are errors and dependencies are
  never added.

## Packages

Evidence comes from an injected `OwnershipProbe`. A command on `PATH` or an
install recipe is never ownership; only a native package record (`Owner`) is.

| Status | Meaning |
|---|---|
| `remove` | One declared owner; `Argv` uninstalls it without a shell. |
| `not-installed` | No owner and no presence. |
| `config-only` | The app declares no package (for example the `brew` Brewfile app; `brew bundle` is never ownership). |
| `duplicate` | An earlier item removes the same package. |
| `unsupported` | Script installs, or a manager without a tested uninstall. |
| `blocked` | Unknown or ambiguous ownership, presence without an owner, an owner that matches no recipe, a flag-like name, a missing manager, or a kept app that requires or declares the same package. |

Items run dependents first (inverse `requires`). Removing a runtime is blocked
while an unselected app that requires it is installed or unknown; a plugin that
`requires` its host blocks the host the same way. `config_requires` only adds a
warning when the expecting app's configuration is kept.

Tested uninstall mappings (no force, zap, purge or autoremove):

| Owner | Argv |
|---|---|
| brew formula | `brew uninstall --formula <pkg>` |
| brew cask (macOS) | `brew uninstall --cask <cask>` |
| pacman (also yay recipes) | `sudo pacman -R <pkg>` |
| npm | `npm uninstall -g [--prefix <prefix>] <pkg>` |
| pnpm (default prefix) | `pnpm remove -g <pkg>` |

An npm recipe may be owned by pnpm, because installs prefer pnpm when present.

## Native probe

`clean.NativeProbe` is the production `OwnershipProbe`. It runs one argv per
declared recipe through an injected `Executor` (never a shell; `OSExecutor`
sets `LC_ALL=C`) and asks only about that exact package:

| Recipe | Query | Owner |
|---|---|---|
| brew formula | `brew list --formula --versions <pkg>` | `brew` + `Package` |
| brew cask (macOS only) | `brew list --cask --versions <cask>` | `brew` + `Cask` |
| pacman / yay | `pacman -Q <pkg>` | `pacman` + `Package` |
| npm | `npm prefix -g`, then `npm ls -g --prefix <p> --depth=0 --json <pkg>` for that prefix and `~/.local` | `npm` + exact `Prefix` |
| npm / pnpm | `pnpm ls -g --depth=0 --json <pkg>` | `pnpm` |

- yay is never invoked. A yay recipe (Omarchy) is owned by its pacman
  database record, so it is planned, rechecked and removed exactly like a
  pacman package (`sudo pacman -R <pkg>`, which still refuses packages other
  packages depend on), and a pacman and a yay recipe naming the same package
  are one owner. Without pacman the evidence is unknown, which blocks. This
  mapping is covered by tests with injected commands only; it has not been run
  on a real Omarchy machine.
- A query is absence only when it cleanly says so (silent exit 1, pacman
  "was not found", a valid JSON tree without the package). Errors, other
  output, unparsable JSON, npm `problems` or `missing` entries make the
  evidence unknown, which blocks.
- Several matching records (npm and pnpm, two npm prefixes) are all
  reported, so the planner blocks them as ambiguous.
- Script recipes are never queried. An available manager without a tested
  query (apt, dnf, zypper, winget, scoop, choco, cargo) makes evidence unknown.
- `Present` is required; `PathPresence(home)` checks `check` entries (or the
  app name) on `PATH` or with `Lstat`.

## Package runner

`clean.PackageRunner` executes the `remove` items of a plan for the CLI and
TUI:

1. Every remove item is verified first: declared app, owner matching a
   declared recipe and `Argv` equal to the tested uninstall re-derived from
   the owner. Any mismatch refuses the whole plan with no output or command.
2. `Summary(plan)` is printed: every status, blocked reason and warning.
3. `DryRun` stops there. With nothing to remove it stops without asking.
   Otherwise `Confirm(summary)` must return true; a nil `Confirm` denies.
4. Items run in plan order (dependents first). Before each, `Recheck` must
   report exactly the planned owner. The first failure (changed or unknown
   ownership, a command that cannot start, a non-zero exit) stops every
   later removal, which are reported `not-run`, and `Run` returns an error.

Command output is never printed, only the exit status.

## Configurations

| Status | Meaning |
|---|---|
| `quarantine` | `Paths` (`~/...` sources) may be moved into a recoverable quarantine after the runner validates each one. |
| `no-config` | The app declares no paths. |
| `blocked` | A source is not inside home (repository files such as `Brewfile`, `..`, `~`). |

Paths named in `secrets` are left out, and `Preserve` lists secrets and
`exclude` patterns (data, caches, history) the runner must keep in place.

## Configuration runner

`clean.ConfigRunner` moves the `quarantine` items of a plan into a recoverable
quarantine for the CLI and TUI. Nothing is deleted.

1. Every item must equal the one re-derived from the manifest, and `Home` must
   be absolute and not `/`; a `Repo` overlapping the quarantine or a cancelled
   context is refused too. Refusals print nothing and write nothing.
2. Each path is printed with its status: `quarantine` (a regular file),
   `missing`, or `blocked`: outside home, inside the quarantine or repository, a
   symlink or symlinked parent, a directory (not supported yet), not a regular
   file, listed twice, covered by a `Preserve` pattern, an uncertain `Preserve`
   pattern, or a name that may hold secrets or data (`token`, `auth`,
   `session`, `history`, `cache`, `hosts.yml`, `.env`, `.pem`, `.db`, ...).
   `Preserve` patterns print as `keep` lines.
3. `DryRun` stops after the summary; otherwise `Confirm(summary)` must return
   true (nil denies).
4. `~/.dots-quarantine/clean-*` (0700, unique) receives `record.json` (0600,
   exclusive, synced) with every original path before anything moves.
5. Right before each rename the file must be unchanged (identity, mode, size,
   mtime). Any failure moves every moved file back by link-then-unlink, which
   never replaces a path that exists again; `Moved`, `Failures` and
   `Quarantine` report what stayed and where. Cross-device renames fail and
   roll back.

### Preserve patterns

`Preserve` holds the app's `secrets` and `exclude` entries; a file they cover
never moves. As in backup, a pattern is relative to the mapped directory and a
matching directory keeps everything below it. A file's mapped directory is not
recorded, so every ancestor up to home counts: the pattern may match any run of
consecutive path segments. With `exclude: [plugins/local.json, nested/dir/**,
"*.lock"]`, `~/.app/plugins/local.json`, `~/.app/nested/dir/x.json` and
`~/.app/state.lock` all stay. `*` and `?` never cross `/`, and a trailing `/**`
means the directory itself.

A pattern whose meaning is uncertain (absolute, `~`, `.` or `..` segments,
empty segments such as a trailing `/`, `**` other than a trailing `/**`,
backslashes, or a malformed glob) blocks every path of that app instead of
being guessed.

The sensitive-name list is a best-effort extra: it does not prove a file holds
no secrets. Only files a manifest maps explicitly are ever candidates, and
directories stay blocked.

### Limits

Checks and moves are separate system calls, so a process that changes the home
between the last check and a rename (TOCTOU) can still win the race; the
identity check narrows that window but does not close it. The runner assumes a
home owned by the user and gives no security guarantee against a hostile
filesystem or a concurrent attacker.

## Old backups

`dots restore` keeps every file it replaces beside the original as
`<file>.<YYYYMMDD_HHMMSS>[.<n>].bk`. `clean.BackupRunner` (`--backups`, TUI
`b`) finds these backups for the selected apps' mapped paths: beside a mapped
file, or anywhere below a mapped directory (symlinked directories, the
quarantine and the repository are not entered). Backups are grouped by the
file they back up and decided per group, newest first:

| Line | Meaning |
|---|---|
| `keep ...: most recent backup of <file>` | The newest backup. It always stays. |
| `keep ...: only backup of <file>` | A file with a single backup. It always stays. |
| `keep ...: same timestamp as another backup of <file>` | Its age is not certain; every backup sharing that second stays. |
| `keep ...: unrecognized backup name` | A `.bk` whose name does not carry a valid timestamp; it stays. |
| `quarantine ...: older backup of <file>` | Strictly older than a kept backup of the same file; it moves. |
| `blocked ...` | An older backup that is a symlink, a backup of a `secrets`/`exclude` file, or has a sensitive name (same rules as configurations); it stays. |

Guarantees:

- Off by default. Without the option nothing is scanned, listed or moved.
- The last remaining backup of a file is never moved, even with the option
  on: only backups strictly older than a kept one of the same file can move.
- Nothing is deleted. Older backups move into the same
  `~/.dots-quarantine/clean-*` mechanism as configurations (own record, same
  identity recheck and rollback) and come back with `dots clean recover <dir>`.
- The same single `yes` covers the whole plan, and the runner only accepts
  exactly the previewed summary: if any backup appeared, vanished or changed
  status, nothing moves and no package is removed.

Ordering uses the timestamp in the name, never the file's mtime. The limits of
the configuration runner (TOCTOU, trusted home) apply.

## Recovery

`clean.Recover(ctx, clean.Recovery{...})` restores one `clean-*` record
directly inside `~/.dots-quarantine` whose record names the same home. Entries
get the same path checks; an existing destination is a `conflict`, and
conflicts, `blocked` and `missing` entries never move. Restores need `Confirm`
(nil denies), are revalidated and use link-then-unlink. The first failure stops
the run; the record directory is kept.
