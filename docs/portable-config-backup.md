# Portable config backup

`dots backup` stages the personal configuration of every app in `manifest.yaml`. Each location comes from the app's own loader or documentation. Every source is written with `~/`, and the current home is stored as `__HOME__` inside text files, so the same manifest maps to the home of macOS or Linux.

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

## Before restoring on a new machine

- Install Oh My Zsh **before** restoring zsh. Its installer stops when `~/.oh-my-zsh` already exists, and restoring `custom` creates that folder.
- Paths under your home are rewritten for the new machine. Paths outside it are copied as they are. For example, a macOS `~/.codex/config.toml` can name `/opt/homebrew/bin/engram`, a `~/Library/Application Support/...` folder or a macOS `.app`; fix those by hand on Linux.

## Not covered

- **Orca settings.** Orca keeps them inside its profile data (`orca-data.json` and a SQLite store in the app data folder), mixed with managed accounts, workspace and session state, and telemetry. No safe export has been established, and Orca has no settings export command. `dots` copies only `~/.orca/agent-hooks`. Never copy the Orca data folder as is.
- **CodexBar** (`~/.config/codexbar/config.json`): macOS only, and it stores provider keys and token accounts.
- Your own files inside Oh My Zsh `custom/plugins` or `custom/themes`. Move them to a top-level `custom/*.zsh` file, or add their folders to the manifest.
- Apps not in the manifest, and locations no loader documents.
