# Install flow

`dots restore` and the TUI's *Restore / Install Setup* run the same code:
`restore.Select` picks the apps, `restore.BuildPlan` orders them and
`restore.Run` executes the plan. The app inventory is described in
[install-inventory.md](install-inventory.md).

## What gets selected

| Selection | CLI | TUI | Optional apps |
|:---|:---|:---|:---|
| Profile | `--profile core` (default), `--profile ai`, … | Core / AI / Mobile presets | Excluded |
| Everything | `--all` | Full Setup | Excluded |
| Named apps | `dots restore <app>` | Custom Selection | Included when named or checked |

Each selected app brings the apps it `requires`, in order (for example `pi`
pulls `node` first).

## Packages

For every app in plan order:

1. Already present: skipped, never reinstalled. An app is present when its
   `check` commands or paths are found, or when the package manager confirms
   the package of the recipe this platform selects (read-only `pacman -Q`,
   `brew list --versions`, `npm ls -g`, `pnpm ls -g`). That second check
   finds packages installed under another command name or path, such as
   `bitwarden` on Arch (`bitwarden-desktop`) or fonts in `/usr/share/fonts`.
   A failed or unclear answer counts as not installed, as before.
2. No install recipe needed (config-only app): nothing to run.
3. No recipe for this platform, or the recipe's package manager is missing:
   reported as *unsupported* / *manager-missing* and skipped.
4. A required app failed or is unavailable: reported as *blocked* and skipped.
5. Otherwise the install command runs (CLI: only with `--install-packages`;
   TUI: always). Commands run from the dotfiles repository directory.

The TUI suspends itself for the whole run (Bubble Tea `tea.Exec`): it leaves
the alternate screen and hands the real terminal, including stdin, to the
install commands, so their output and `sudo` password prompts appear in the
normal terminal. Each command is announced as `▶ [app] command`. The TUI
resumes with the summary when the run ends; if the terminal cannot be handed
off, nothing runs and the TUI says so.

A failed install blocks the apps that require it; unrelated apps continue.

## Configuration

By default nothing is copied from the repository: apps keep the
configuration they ship with, and local files are untouched.

Configs are deployed only on request: `--with-config` in the CLI, `c` on the
TUI's installation mode screen. This also pulls each app's `config_requires`
(for zsh: powerlevel10k, zsh plugins, fzf, fd, bat, zoxide, atuin, carapace).
Plugins that require their host are installed after it (zsh before its
plugins).

An app's config is deployed only after the app, its `requires` and its
`config_requires` are all installed. Otherwise it is reported as
*incomplete* and nothing is written, even when the app itself installed.
When the repository has no backup for an app, it keeps its stock
configuration.

Existing local files:

| Local file | Default | `--replace-existing` (TUI: `r`) |
|:---|:---|:---|
| Missing | Created | Created |
| Same content | Left as is | Left as is |
| Different content | Kept and listed | Replaced after `<file>.<timestamp>.bk` is written |
| Directory where a file is expected | Not touched, reported | Not touched, reported |

If the safety backup cannot be written, the file is not replaced and the run
reports a failure. Nothing is deleted in bulk. Secret `.template` files are
only created where no file exists.

## Known legacy configuration repairs

Restore adapts two recognized legacy configurations, including old backup copies:

| File | Repair |
|:---|:---|
| `~/.zshrc` | Exact legacy initialization and plugin-loading lines gain availability checks; Powerlevel10k tries Homebrew and Arch system locations. A macOS-only `export PNPM_HOME="/Users/<you>/Library/pnpm"` (an absolute home path) becomes a switch that keeps that path on macOS and uses `${XDG_DATA_HOME:-$HOME/.local/share}/pnpm` elsewhere. Unknown shell statements stay unchanged. |
| `~/.pi/agent/settings.json` | Remove the obsolete `npm:gentle-engram@0.3.0` string only when its single companion is the canonical `npm:gentle-engram` object with `extensions: ["+index.ts"]`. Other settings are preserved. |

Source content is repaired before comparison and deployment. Existing different
local files are still preserved by default; the summary says **repair not
applied**, rather than claiming success. Replacement still swaps the whole
local file for the backed-up source, after a safety backup; it does not merge
custom local settings.

For the known conflicting **local Pi settings**, `--with-config
--replace-existing` additionally authorizes a narrow local repair **before any
installer runs**, even if Pi is already installed. It backs up the original,
removes only that obsolete duplicate, preserves permissions and unrelated
settings, and checks the result before allowing installation. If an installer
later fails, the repaired file and its backup remain; the summary names the
backup to copy back for recovery. Without those flags, the conflict is reported
and Pi installation is blocked. Dry runs never change it.

Preview first, using the intended backup repository:

```sh
dots --dir /path/to/backup restore pi --install-packages --with-config --replace-existing --dry-run
```

After checking the preview, remove `--dry-run` only if replacing the declared
Pi configurations from that backup is intended. In the TUI, enable both config
restoration (`c`) and replacement (`r`); these authorize the same flow.

This is **not** a general corrupt-file cleaner, a shell validator or a settings
migration engine. Unrecognized package conflicts remain blocked; invalid JSON
and unavailable files are not automatically repaired. Skill-directory versus
symlink collisions remain refused. No external `gentle-ai` preflight, real login
or installer validation is implied by the local tests. Existing filesystem
check/write race limitations still apply.

## Moving to another machine: known pitfalls

These were found moving a macOS setup to Linux (Omarchy). dots handles the
first three itself; the rest are steps for you.

| Pitfall | What dots does or what to do |
|:---|:---|
| Skills shared through `~/.agents/skills` are symlinks | Taildrop archives carry relative symlinks; restore refuses links that point outside home. |
| Login files (`auth.json`, `mcp-auth.json`) keep OAuth identifiers even when masked | They never travel. Sign in again on the new machine (`dots setup` lists every login). |
| `PNPM_HOME` pinned to `~/Library/pnpm` breaks pnpm on Linux | Repaired on restore (see above). |
| Gentle Shell | Install the bundled Gentle Shell with its own installer (`dots setup` shows where), then `dots restore gentle-shell --received`. Do not use `gentle-ai shell install` on Linux, and never run `gentle-shell update` or `pi update` inside such a private install: gentle-ai 4.3.1 leaves it unlaunchable (gentle-ai#5812, #5808). If the installer finds a `gentle-shell` on `PATH` that pnpm or npm did not install, move that link aside first; do not delete the old install. |
| Neon MCP sign-in fails with "Invalid redirect URI" | Use an API key from https://console.neon.tech instead: `"headers": {"Authorization": "Bearer <key>"}` on `mcpServers.neon` in `~/.gentle-shell/agent/mcp.json` and `~/.pi/agent/mcp.json`. `dots setup` shows this step. |
| Engram inside Gentle Shell | Memory works through the `gentle-engram` extension and the shared server on `127.0.0.1:7437`. Gentle Shell points `ENGRAM_BIN` at its guard, so an `engram` MCP server in `~/.gentle-shell/agent/mcp.json` always fails there; keep it `"enabled": false`. Pi keeps its own. |

## Results and exit code

Both front ends print the same summary from `restore.RunResult`. The CLI exits
nonzero, and the TUI shows the problem list instead of a success line, when:

- an install command failed, or an app was blocked by such a failure;
- a config is incomplete or failed;
- an explicitly named app cannot be installed on this platform.

Unsupported apps in a profile run are listed as skips, not failures.

## Readiness report

The summary ends with a readiness report, per app, built by
`restore.ScanReadiness` and rendered through the same `RunResult.Summary`
in the CLI and the TUI. It lists what the user still has to do:

| State | Meaning | Evidence |
|:---|:---|:---|
| `missing` field | A field still holds a dots placeholder (`YOUR_…_HERE`) | JSON/YAML/TOML field path and file |
| `missing` env | A referenced variable is not set in the environment dots runs in | `${NAME}`, `{env:NAME}`, `env_key`, `*_env_var`, `env_vars` |
| `login-pending` | A credentials file still holds the dots template placeholders (left by an older dots; login files no longer travel in backups) | Codex `auth.json`, Pi `auth.json` / `mcp-auth.json` (per provider) |
| `dependency-missing` | An MCP server `command` is not found on `PATH` | `mcpServers.<name>.command`, `[mcp_servers.<name>] command` |
| `not-verified` | dots cannot tell; the action says how to check | Claude Code session, absent or filled credentials files, GitHub CLI, Copilot in Neovim, unreadable files |

Each item names the variable, field and `~/` file plus the action (edit the
field, `export NAME=…`, `codex login`, `/login` in `pi` or `claude`,
`gh auth login`, `:Copilot auth`).

No rule can tell from its shape alone whether a name is a secret, so names
read from a file or a manifest are printed only under an allow-list policy:

- Field paths print trusted schema keys (`mcpServers`, `headers`, `env`,
  `command`, `env_key`, …), the MCP server and model provider names dots
  ships, and allow-listed variable or header names (below) inside
  `env`/`headers` tables. Any other key is shown as
  `<key #N>`, its position among the sorted keys of its table; array
  elements are shown as `[N]`.
- Variable names (`${NAME}`, `env_key`, `env_vars`, …) and header names are
  printed only when they are in a fixed allow-list of the services dots
  ships configs for (`DATABASE_URL`, `LINEAR_API_KEY`,
  `GITHUB_PERSONAL_ACCESS_TOKEN`, `X-Goog-Api-Key`, `Authorization`, …).
  The shape of a name is never used: any other name, however conventional
  it looks, is reported as `<variable>` (or `<key #N>` as a table key) with
  its field and file and the action "set the variable this field
  references".
- A declared file is printed as its `~/` path only when the built-in
  inventory or the login rules declare it (`~/.pi/agent/mcp.json`, …). Any
  other file is shown as `path #N of <app>`, its position in that app's
  `paths` in the manifest.
- Apps of the built-in inventory are named; any other app in the report is
  `app #N`, its position in the run.
- Pi login entries print a known provider (`anthropic`, `openai-codex`, …)
  or `MCP server <name>` for a shipped server; any other entry is
  `provider entry #N`.
- A missing MCP command is reported as `MCP server command` with its field
  and file; the configured command or path is never printed.
- Path components of printed files that are not plainly printable are
  shown as `<unprintable name>`, and every rendered line escapes control,
  format and invalid characters.

Limits:

- The allow-list is fixed: legitimate custom variables, headers, apps and
  paths are shown only by their generic or positional label; those items
  keep their field, file and action.
- The naming policy covers the readiness report only. The package and
  configuration lines of the summary print app names, install commands,
  restored paths and errors from the manifest as they are, with control,
  format and invalid characters escaped on every summary line in the CLI and
  the TUI. Output streamed by install commands, and the CLI's `▶ [app]
  command` line before each command, are not escaped or filtered.
- Redacted names are positional (`<key #N>`, `path #N of <app>`): the user
  finds the entry by position in the file or manifest, and the count and
  position of keys is visible.

- Only files declared in the selected apps' `paths` under the target home
  are read; symlinks resolving outside it, directories and files over 1 MiB
  are not scanned.
- Values, file snippets and decoder errors are never printed; a malformed
  file is reported as "not valid JSON/YAML; not scanned".
- It is offline: no key, token or session is validated, no login command or
  network request runs. A filled credentials file is "present, validity not
  checked", never "ready"; an absent one is "not verified", never "invalid".
- Environment variables are checked in the dots process only; a variable
  exported later in a shell profile shows as missing until dots runs again.
- Readiness items do not change the exit code.
- In a dry run the report describes the current files: nothing was deployed.

## Dry run

`--dry-run` resolves the plan and reports what would happen. It never clones
the repository, writes the manifest or config files, or runs commands. When
the repository has no manifest it plans with the built-in inventory.

## Not done by the installer

Service enablement, docker group changes, model downloads, credential or
session validation, encryption and cleanup of previous installs are out of
scope.
