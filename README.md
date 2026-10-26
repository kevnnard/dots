# dots ⚡️

A compiled, standalone Go CLI to manage, backup, and restore your dotfiles, configurations, and software packages across macOS and Linux machines.

## Why `dots`?

- **Zero dependencies on clean machines**: Compiled as a single static Go binary. No Node/Python/Brew required to run the tool that installs your environment.
- **Cross-platform OS & package manager detection**: Automatically identifies Darwin (macOS), Linux distributions (Ubuntu, Debian, Arch, Fedora) and their package managers (`brew`, `apt`, `pacman`, `pnpm`).
- **Secret & token protection**: Automatically sanitizes `.env` files, certificates, and private keys into `.template` / `.example` files so you never leak secrets in git dotfiles repositories.
- **Homebrew bundle sync**: Seamlessly dumps and restores your macOS `Brewfile`.
- **Drift detection**: Quickly inspect which files have drifted between your machine and your dotfiles repo.

---

## Installation & Setup

### 1. Build from source
```bash
git clone https://github.com/kevnnard/dots.git ~/Repositories/dots
cd ~/Repositories/dots
go build -o /usr/local/bin/dots ./cmd/dots
```

### 2. Initialize your dotfiles repository
By default, `dots` manages `~/.dotfiles`:
```bash
dots init
```
This sets up a git-ready `~/.dotfiles` repository with default configurations for:
- **Neovim** (`~/.config/nvim`)
- **Pi** (`~/.pi/agent/settings.json`, skills)
- **Ghostty** (`~/.config/ghostty`)
- **Zsh** (`~/.zshrc`)
- **Git** (`~/.gitconfig`, `~/.config/git`)
- **pnpm**
- **Homebrew** (`Brewfile`)

---

## Usage Commands

### Check Environment & Sync Status
```bash
# Detect OS, Architecture, and available package managers
dots detect

# Inspect synchronization state and drift
dots status
```

### Backup Your Configurations
```bash
# Backup all apps declared in manifest.yaml
dots backup

# Or backup a specific app
dots backup nvim
```

### Restore on a Clean Machine
```bash
# Preview what would be installed and copied for the CORE profile
dots restore --dry-run

# Restore CORE profile (config files + automatic package installation)
dots restore --install-packages

# Restore MOBILE profile (React Native, OpenJDK 17, CocoaPods, Fastlane)
dots restore --profile mobile --install-packages

# Restore INFRA profile (Caddy, mkcert, act)
dots restore --profile infra --install-packages

# Restore ALL profiles at once
dots restore --all --install-packages
```

### Stacks & Profiles
- **`core` (default)**: Essential shell (zsh, p10k, zoxide, atuin, fzf), Neovim, Ghostty, Zed, Docker Desktop, .NET SDK, Bruno, Bitwarden, Nerd Fonts, and Gentleman tools.
- **`mobile`**: Toolchain for React Native and native builds (`openjdk@17`, `cocoapods`, `fastlane`).
- **`infra`**: Optional local server & action testing tools (`caddy`, `mkcert`, `act`).

### Track a New App / Config
```bash
# Track and immediately backup a custom config
dots add alacritty ~/.config/alacritty/alacritty.toml

# Or track without immediate backup
dots add custom-app ~/.config/app/conf.json --backup=false
```

---

## Architecture: `manifest.yaml`

Your configurations and installation recipes are declared in `~/.dotfiles/manifest.yaml`:

```yaml
version: 1.0.0
apps:
  nvim:
    description: "Neovim editor and plugins"
    paths:
      - src: "~/.config/nvim"
        dest: "configs/nvim"
    exclude:
      - ".git/**"
      - ".DS_Store"
      - "undo/**"
    install:
      darwin:
        manager: brew
        package: neovim
      ubuntu:
        manager: apt
        package: neovim
      arch:
        manager: pacman
        package: neovim
```

---

## License
MIT
