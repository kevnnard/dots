# Portable config backup

`dots backup` stages the personal configuration of every app in `manifest.yaml`. Each location comes from the app's own loader or documentation. Every source is written with `~/`, and the current home is stored as `__HOME__` inside text files, so the same manifest maps to the home of macOS or Linux.

The Homebrew prefix travels the same way. Inside text files the source machine's prefix (for example `/opt/homebrew`) is stored as `__BREW_PREFIX__`, and restore expands it to the destination's prefix (for example `/home/linuxbrew/.linuxbrew`), so `command = "/opt/homebrew/bin/engram"` becomes `command = "/home/linuxbrew/.linuxbrew/bin/engram"` on Linux. The prefix is taken from a non-empty `HOMEBREW_PREFIX`, otherwise from `brew --prefix`, and is used only when it is an absolute directory holding `bin/brew`. Without a verified prefix on the destination the placeholder is kept and restore warns `<file>: no Homebrew prefix verified on this machine; __BREW_PREFIX__ left unexpanded`. Only whole path prefixes are replaced, binary files and files over 8 MiB are copied verbatim, and `dots status` and restore treat a deployed file as equal to its backup once both placeholders are normalized. Versioned paths such as `Cellar/node/<version>` keep their version, `~/Library/...` paths are not rewritten, and backups made before this change keep their literal prefix.

The coverage is not complete; see [Not covered](#not-covered).

## What is mapped

| App | Locations | Why |
|:---|:---|:---|
| zsh | `~/.zshrc`, `~/.p10k.zsh`, `~/.oh-my-zsh/custom` without `plugins/` and `themes/` | Oh My Zsh sources `$ZSH_CUSTOM/*.zsh`, lib overrides, functions and completions from `custom`. |
| pi | `~/.pi/agent`: settings, models, MCP, trust (existing), skills, agents, chains, extensions, keybindings, `AGENTS.md`, `AGENTS.override.md`, `CLAUDE.md`, `SYSTEM.md`, `APPEND_SYSTEM.md`, prompts, themes | Pi's agent directory, as listed in Pi's `docs/configuration.md`. |
| pi | `~/.pi/gentle-ai/<preference>.json`, `channel.json`, `~/.pi/web-search.json` (template) | Gentle AI preferences for Pi and the web search provider. |
| gentle-shell | The same agent files under `~/.gentle-shell/agent` | Gentle Shell runs Pi with this agent directory. |
| gentle-shell | `~/.gentle-shell/config/gentle-ai/<preference>.json`, `~/.gentle-shell/gentle-ai/<preference>.json` | Config homes of the bundled install and of the standalone launcher. |
| claude | settings, MCP, `CLAUDE.md`, skills, agents, commands, output styles, hooks, keybindings | User-level Claude Code configuration. |
| codex | config, hooks, `AGENTS.md`, rules, skills, and the files `config.toml` and `hooks.json` name (`engram-instructions.md`, `engram-compact-prompt.md`, `herdr-agent-state.sh`) | A restored config must find the files it points to. |
| gga | `~/.config/gga/config` | Global settings; the rules file stays in each project. |
| ghostty, herdr, nvim, zed, git, atuin, gh, flashspace, ssh | Unchanged | See `manifest.yaml`. |

`<preference>` is one of: `animations`, `background-subagents`, `banner`, `builtin-codemode-optout`, `double-esc-cancel`, `history-capture`, `keymaps`, `models`, `notifications`, `persona`, `profile-fallbacks`, `profiles`, `profiles.export`, `star-prompt`, `vim`, `visual-customization`, `visual-profiles`. The model files (`models`, `profiles`, `profiles.export`, `profile-fallbacks`) hold agent-to-model routing only: their readers reject unknown keys, and they have no account or permission fields.

`trust.json` (Pi and Gentle Shell) was mapped before this change and is unchanged. It is existing coverage, not a new grant: review it before you restore it on another machine.

A missing location is reported as `Skipped (does not exist)` and nothing else happens.

## What is not mapped

- **Plugin and theme folders**: Oh My Zsh `custom/plugins` and `custom/themes` are excluded as a whole. Installed sources (powerlevel10k, zsh-autosuggestions) are skipped, but so is anything you wrote inside these folders.
- **Permission authority**: `runtime-guardrails.json` and `permission-mode.json` are not portable preferences.
- **Runtime state**: Gentle AI config homes are never copied as folders. They also hold builds (`main/`), `scratch/`, `state.json`, `telemetry.json` (an install id) and `dev-binary.json`.
- **Leftovers**: `*.bak*`, logs, caches, sessions.
- **Logins**: `auth.json`, `mcp-auth.json` and account files.

## Secrets

`mcp.json` and `web-search.json` hold API keys, so they travel as `.template` files. Values under credential-like keys (`key`, `token`, `secret`, `auth`, `pass`, …) and values with known prefixes (`sk-`, `ghp_`, `postgres://`, …) become placeholders. Restore deploys a template only where no file exists.

### Staging folders from an earlier dots

Earlier versions copied `~/.gentle-shell/agent/web-search.json` as is. A staging folder they wrote still holds that unmasked copy, and restore would deploy it before the template. When backup finds anything (a file, a folder, even a dangling symlink) where a secret's unmasked copy would go, it stops with an error naming the path. It removes nothing, writes no template for it, and stages no manifest, so nothing new is ready to send. Stage into a new empty folder:

```bash
dots backup --out ~/.cache/dots/backup-new
dots backup --send <device> --out ~/.cache/dots/backup-new
```

The old folder is left as it is. Do not send it; delete it yourself once you no longer need it.

Detection goes by file name and key name. It cannot prove that other files are free of credentials: hooks, extensions, aliases, prompts and settings are copied as they are. **Review the staged folder** (`~/.cache/dots/backup` by default) before `dots backup --send`.

## Older manifests

`dots backup` reads the manifest in your dotfiles directory and never rewrites it. When that manifest declares an app but lacks a built-in location for it, backup lists the missing locations as `app: ~/path` before copying. Add the locations you want to the manifest. Apps your manifest does not declare are not listed.

## Links into installed packages

Some configs link into a package install, such as a skill folder that links to `/opt/homebrew/Caskroom/<cask>/<version>/...`. These links follow the package to the new machine automatically, whether the link is a child of a backed-up folder or the mapped file or folder itself. No per-app setup is needed beyond the app's normal install recipe.

| Step | What happens |
|:---|:---|
| `dots backup` | Stages the link as it is. |
| `dots backup --send` | Absolute links never travel as links. A link into a supported package travels as a reference in the archived `manifest.yaml`: app, staged path, package, source version, resource path below the version folder, and whether it is a folder or a file. The staged folder is not changed. |
| `dots backup --receive` | Refuses the archive, and extracts nothing, if a reference is malformed, sits outside its app's paths, or sits where the archive holds an entry. |
| `dots restore --with-config` | After the installs, asks the package manager here, read-only, where the package is installed. Then links the same resource of the installed version. An existing different link or file is kept, or with `--replace-existing` replaced after its `.bk` safety backup. A staged folder copied as is (for example over SSH) still holds the absolute links, and they are resolved the same way. |

**Supported:** Homebrew casks (`Caskroom`) and formulae, dependencies included (`Cellar`). The source prefix does not matter: Apple silicon, Intel, Linuxbrew and custom prefixes all work, and the prefix of the receiving machine comes from `brew --caskroom` and `brew --cellar`.

A link is restored only when all of these hold. Otherwise restore reports the link as not restored, names the package and the reason, and fails. The resolver only issues read-only Homebrew queries, not installation or update commands; Homebrew may consult its cached or API metadata. Missing resources are not guessed.

- `brew` runs on this machine.
- `brew info --json=v2` lists the package as installed, in exactly one version. With several formula versions, the linked one is used.
- The same resource path exists in the installed version, as the same type (folder or file).
- The package and version folders do not alias another package or version. The Homebrew root itself may be a symlink.
- Once every link is followed, the resource stays inside that package's version folder.
- The reference agrees with the staged link, when both exist.

Malformed, duplicate, out-of-scope references or references colliding with staged files or folders cause the whole affected app to be refused before any of its configuration is written. Existing targets and safety backups remain unchanged. Other failures, such as a missing package resource, are reported per link.

The destination is checked like any other restored file: no parent folder may lead out of home, not even into the package. Confirming the package authorizes that one link, never a write inside the package. An existing destination directory is never replaced.

**Not resolved:** other managers, and other Homebrew paths such as `/opt/homebrew/opt/<formula>`, `bin` or `share`. A resource that moved or was renamed between versions is not found. Relink these by hand.

## Before restoring on a new machine

- Install Oh My Zsh **before** restoring zsh. Its installer stops when `~/.oh-my-zsh` already exists, and restoring `custom` creates that folder.
- Paths under your home are rewritten for the new machine. Links into Homebrew packages are resolved (see [Links into installed packages](#links-into-installed-packages)). Other paths outside home are copied as they are. For example, a macOS `~/.codex/config.toml` can name `/opt/homebrew/bin/engram`, a `~/Library/Application Support/...` folder or a macOS `.app`; fix those by hand on Linux.

## Not covered

- **CodexBar** (`~/.config/codexbar/config.json`): macOS only, and it stores provider keys and token accounts.
- Your own files inside Oh My Zsh `custom/plugins` or `custom/themes`. Move them to a top-level `custom/*.zsh` file, or add their folders to the manifest.
- Apps not in the manifest, and locations no loader documents.
