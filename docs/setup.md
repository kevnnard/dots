# dots setup

`dots setup` runs the guided post-install steps in priority order, after
`dots restore` has installed packages and deployed configs.

## Order

1. **Default shell** (zsh or bash). Runs `chsh -s <shell>`. The account database
   must show the new shell before the step passes. It applies in the next session.
2. **zsh with powerlevel10k**. No token. Passes when `~/.zshrc` and
   `~/.p10k.zsh` are in place (from `dots restore zsh --with-config`).
3. **Gentle Shell (bundled install)**. No token. dots never runs the installer:
   it shows where to download `gentle-shell-installer` and to run
   `sh install-gentle-shell.sh`, then passes when `gentle-shell --version` works.
   Restore its configs afterwards with `dots restore gentle-shell --received`.
4. **Logins**: Claude Code, Copilot (Neovim), GitHub CLI, Codex, Gentle Shell
   (`/login` inside `gentle-shell`; login files never travel in backups),
   Antigravity, Linear MCP, Postgres MCP.

## How logins work

dots runs each login command in the same terminal. Password prompts and login
links appear there, and you complete them. dots never reads, stores, or logs a
credential.

A login is verified only through its tool's own status command. GitHub CLI uses
`gh auth status`. Claude Code and Copilot have no status command dots can use,
so they are reported as **manual**; check them yourself in the tool.

## Usage

```bash
dots setup --dry-run   # show the ordered plan, change nothing
dots setup             # ask for the shell, then run each step
```

The final line counts done, manual, and failed steps. A failed step makes the
command exit non-zero, but the remaining steps still run.

## Limits

- Only zsh and bash are offered for the shell now. More shells can come from
  `/etc/shells` later.
- The shell change needs your password when `chsh` asks for it.

## Guided steps

Some steps need a credential that only you can enter: the Postgres MCP connection password, the Linear MCP sign-in, the Codex and Antigravity sign-ins. For these, `dots setup` never runs the login and never reads the credential. It shows the guide (the command or app step to do yourself) and then checks the tool's own status when one exists. Without a status check the step stays `manual`.
