# dots ⚡️ (`kevnnard-dots`)

An interactive Go TUI and standalone CLI to manage, backup, version, and restore dotfiles, AI agent configs, and software packages across macOS and Linux machines.

```
██╗  ██╗███████╗██╗   ██╗███╗   ██╗███╗   ██╗ █████╗ ██████╗ ██████╗ 
██║ ██╔╝██╔════╝██║   ██║████╗  ██║████╗  ██║██╔══██╗██╔══██╗██╔══██╗
█████╔╝ █████╗  ██║   ██║██╔██╗ ██║██╔██╗ ██║███████║██████╔╝██║  ██║
██╔═██╗ ██╔══╝  ╚██╗ ██╔╝██║╚██╗██║██║╚██╗██║██╔══██║██╔══██╗██║  ██║
██║  ██╗███████╗ ╚████╔╝ ██║ ╚████║██║ ╚████║██║  ██║██║  ██║██████╔╝
╚═╝  ╚═╝╚══════╝  ╚═══╝  ╚═╝  ╚═══╝╚═╝  ╚═══╝╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝ 
                      DOTFILES & MACHINE PROVISIONER
```

## Why `dots`?

- **Interactive TUI Dashboard**: Full visual terminal interface built with Bubble Tea & Lipgloss featuring retro 8-bit ANSI shadow banner, live drift detection, and single-click or keystroke restore/backup actions.
- **Zero dependencies on clean machines**: Single static Go binary. Auto-clones repository when executed in an empty environment.
- **Enterprise-Grade Secret Protection**: AST-level sanitization for `.env`, JSON configs (`auth.json`, `mcp-auth.json`, `mcp.json`), and SSH configs. Automatically redacts API keys (Linear, OpenAI, Anthropic, GCP), bearer tokens, database connection strings, and private server IPs into clean `.template` files.
- **Zero Data Loss Guarantee**: Local safety backups (`<target>.<timestamp>.bk`) created before overwriting modified files during restoration.
- **Automated Git Snapshots**: Commits backups with timestamped host snapshots and remote push.
- **Cross-Platform OS & Package Manager Detection**: Automatically identifies Darwin (macOS), Linux distributions (Ubuntu, Debian, Arch, Fedora) and their package managers (`brew`, `apt`, `pacman`, `pnpm`).

---

## ⚡️ Quick Install

### Option A: One-line bootstrap (recommended for fresh machines)
```bash
curl -fsSL https://raw.githubusercontent.com/kevnnard/dots/main/install.sh | bash
```

### Option B: Homebrew Tap (macOS)
```bash
brew tap kevnnard/tap
brew install --HEAD kevnnard/tap/dots
```
*(Both `dots` and `kevnnard-dots` binary aliases are available).*

### Option C: Build from source
```bash
git clone https://github.com/kevnnard/dots.git ~/Repositories/dots
cd ~/Repositories/dots
go build -o ~/.local/bin/dots ./cmd/dots
```

---

## Interactive Visual TUI

Running `dots` (or `kevnnard-dots`) with no arguments launches the interactive dashboard:

```bash
dots
```

- Navigate applications with `↑` / `↓` or `j` / `k`
- Select profiles (`core`, `mobile`, `infra`, `all`) with `Tab`
- Hit `b` to Backup, `r` to Restore, or `d` to Dry-run simulate
- Hit `q` to quit

---

## CLI Commands

### Environment & Drift Status
```bash
# Detect OS, Architecture, and available package managers
dots detect

# Inspect synchronization state and file drift
dots status
```

### Backup Configurations
```bash
# Backup all apps declared in manifest.yaml
dots backup

# Backup a specific app
dots backup nvim
```

### Restore on a Clean Machine
```bash
# Preview what would be copied and installed
dots restore --dry-run

# Restore CORE profile (configs + auto package installation)
dots restore --install-packages

# Restore MOBILE profile (React Native, OpenJDK 17, CocoaPods, Fastlane)
dots restore --profile mobile --install-packages

# Restore INFRA profile (Caddy, mkcert, act)
dots restore --profile infra --install-packages

# Restore ALL profiles
dots restore --all --install-packages
```

### Profiles
- **`core` (default)**: Essential shell (zsh, p10k, zoxide, atuin, fzf), Neovim, Ghostty, Zed, Docker Desktop, .NET SDK, Bruno, Bitwarden, Nerd Fonts, Pi, Claude Code, and Codex CLI.
- **`mobile`**: Toolchain for mobile development (`openjdk@17`, `cocoapods`, `fastlane`).
- **`infra`**: Server and container testing tools (`caddy`, `mkcert`, `act`).

---

## Managing Secrets & Templates

Files containing sensitive information are masked on backup and stored as `.template` files:
- `~/.ssh/config` ➔ `configs/ssh/config.template` (Private IPs and usernames replaced with `YOUR_SERVER_HOST_OR_IP` and `YOUR_SSH_USER`)
- `~/.pi/agent/mcp.json` ➔ `configs/pi/mcp.json.template` (API keys, Postgres URLs, tokens sanitized)
- `~/.claude/mcp.json` ➔ `configs/claude/mcp.json.template`
- `auth.json` / `mcp-auth.json` ➔ `*.template`

When restoring on a new machine, `.template` files are copied only if the target does not already exist, prompting you to fill in your private credentials.

---

## License
[PolyForm Noncommercial License 1.0.0](LICENSE) — Free for personal and non-commercial use. Selling or commercial distribution is strictly prohibited.


