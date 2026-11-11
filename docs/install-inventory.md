# Install inventory

`manifest.yaml` is the single install inventory. `DefaultManifest()` and the
embedded Brewfiles are byte copies of the repository files, enforced by tests.

## Rules

- Every `brew`, `cask` and `npm` line in `Brewfile*` (60 distinct declarations)
  is owned by exactly one manifest app (`TestEveryBrewfileDeclarationHasExactlyOneOwner`).
- The `brew`, `infra` and `mobile` apps only track their Brewfile; they no longer
  run `brew bundle`, so nothing is installed twice.
- One owner per platform: `claude` and `codex` use their casks on macOS and npm
  elsewhere; `vercel`, `railway` and `shopify-cli` use Homebrew where present and
  their official npm packages otherwise.
- `requires` orders installs (dependencies first). `config_requires` lists what a
  backed-up configuration expects (zsh plugins, `fzf`/`fd`/`bat`, `ripgrep` for
  Neovim). It is pulled only when that configuration is restored, so a stock
  install never acquires personal plugins.
- `provides` lets a planned app satisfy a manager (`node` provides `npm`).
- `check` lists commands or `~`/absolute paths that prove an app is installed;
  empty means the app name is used as the command. Packages that ship no
  command (zsh plugins and theme, Nerd Fonts, OpenJDK 17) must list the files
  their recipes install, or every run would reinstall them.
- Full selection (`restore.Select(m, "all")`) excludes `optional` apps.

## Plan statuses (`restore.BuildPlan`)

| Status | Meaning |
|---|---|
| `install` | A recipe for this platform has a usable manager; `InstallCmd` is set. |
| `installed` | The probe found the app; nothing runs. |
| `config-only` | The app has no package, only configuration. |
| `unsupported` | No recipe targets this platform (for example macOS-only apps on Linux). |
| `manager-missing` | A recipe targets this platform but its manager is absent (for example `yay` on plain Arch). |
| `blocked` | A required app is unsupported, missing its manager or blocked. |
| `not-for-os` | The app's `os` list excludes this system; reported as `not for <system>`, never a failure. |

Unknown references and dependency cycles are errors, never silently ignored.
Homebrew casks are only eligible on macOS.

## Verification sources (2026-11-04)

- Homebrew: `brew info --json=v2` for every formula and cask. `tailscale` and
  `ollama` casks are aliases of `tailscale-app` and `ollama-app`.
- Arch and AUR: archlinux.org package search and AUR RPC. Arch `railway`
  (DieBahn) and AUR `mole` (SSH tunnels) are different programs, so those apps
  stay macOS/npm-only.
- Debian: `apt` ids verified against trixie (madison). Ubuntu releases are not
  verified and may lack `lazygit`, `atuin` or `mkcert`.
- Fedora: `dnf` ids verified against Fedora 43 (mdapi).
- Windows: `winget` ids verified against microsoft/winget-pkgs manifests.
- npm: package names and binaries verified against registry.npmjs.org.
- File checks: Homebrew paths match the installed `share/` files of
  `zsh-autosuggestions`, `zsh-syntax-highlighting`, `zsh-autocomplete` and
  `powerlevel10k`, the font casks' artifacts (`CaskaydiaCoveNerdFont-Regular.ttf`,
  `IosevkaTermNerdFont-Regular.ttf`, installed to `~/Library/Fonts`) and the
  Homebrew executables index (which lists no command for these packages).
  The other prefixes follow from the same layout. Arch and Debian/Ubuntu/Fedora
  plugin paths are the ones `configs/zsh/.zshrc` sources.

Not detected (each run re-plans the recipe): Arch `zsh-autocomplete` and the
Arch Nerd Font packages, whose install paths are not verified.

Not verified on a real machine: every Linux and Windows flow, `scoop` ids, the
pre-existing `snap` and `curl` script recipes, and Fedora `npm` in the `node` recipe (Fedora 43 ships it
as `nodejs-npm`; resolution through `Provides` is unverified). Native Windows is partial: zsh, its plugins, tmux and the
Gentleman Homebrew taps are unsupported there.

## Inventory

| App | Profile | Recipes (key: id) | Requires |
|---|---|---|---|
| act | core,infra | brew: `act`, pacman: `act`, winget: `nektos.act` |  |
| android-studio | mobile | arch: `android-studio`, darwin: `cask android-studio`, debian: `script`, ubuntu: `script` |  |
| atuin | core | apt: `atuin`, brew: `atuin`, darwin: `atuin`, dnf: `atuin`, pacman: `atuin` |  |
| bat | core | apt: `bat`, brew: `bat`, dnf: `bat`, pacman: `bat`, winget: `sharkdp.bat` |  |
| bitwarden | core | darwin: `cask bitwarden`, pacman: `bitwarden`, winget: `Bitwarden.Bitwarden` |  |
| brew | core | config-only |  |
| bruno | core | darwin: `cask bruno`, winget: `Bruno.Bruno`, yay: `bruno-bin` |  |
| btop | core | apt: `btop`, arch: `btop`, brew: `btop`, darwin: `btop`, debian: `btop`, linux: `btop`, pacman: `btop`, ubuntu: `btop` |  |
| caddy | core,infra | apt: `caddy`, brew: `caddy`, dnf: `caddy`, pacman: `caddy`, winget: `CaddyServer.Caddy` |  |
| carapace | core | brew: `carapace`, winget: `rsteube.Carapace`, yay: `carapace-bin` |  |
| claude | core,ai | darwin: `cask claude-code`, default: `@anthropic-ai/claude-code` | node |
| cliclick | core | darwin: `cliclick` |  |
| cocoapods | core,mobile | darwin: `cocoapods` |  |
| codegraph | core,ai | default: `@colbymchenry/codegraph` | node |
| codex | core,ai | darwin: `cask codex`, default: `@openai/codex` | node |
| codexbar | core,ai | darwin: `cask steipete/tap/codexbar` |  |
| dbeaver | core | darwin: `cask dbeaver-community`, pacman: `dbeaver`, winget: `DBeaver.DBeaver.Community` |  |
| docker | core | apt: `docker.io`, brew: `docker`, dnf: `moby-engine`, pacman: `docker` |  |
| docker-compose | core | apt: `docker-compose`, brew: `docker-compose`, dnf: `docker-compose`, pacman: `docker-compose` | docker |
| docker-desktop | core | darwin: `cask docker-desktop`, winget: `Docker.DockerDesktop` |  |
| dotnet-sdk | core | darwin: `cask dotnet-sdk`, pacman: `dotnet-sdk` |  |
| eas-cli | core,mobile | default: `eas-cli` | node |
| engram | core,ai | brew: `gentleman-programming/tap/engram`, darwin: `gentleman-programming/tap/engram` |  |
| fastlane | core,mobile | darwin: `fastlane` |  |
| fd | core | apt: `fd-find`, brew: `fd`, dnf: `fd-find`, pacman: `fd`, winget: `sharkdp.fd` |  |
| flashspace | core | config-only |  |
| fnm | core | brew: `fnm`, pacman: `fnm`, winget: `Schniz.fnm` |  |
| font-caskaydia-cove | core | darwin: `cask font-caskaydia-cove-nerd-font`, pacman: `ttf-cascadia-code-nerd` |  |
| font-iosevka-term | core | darwin: `cask font-iosevka-term-nerd-font`, pacman: `ttf-iosevkaterm-nerd` |  |
| fzf | core | apt: `fzf`, brew: `fzf`, dnf: `fzf`, pacman: `fzf`, winget: `junegunn.fzf` |  |
| gentle-ai | core,ai | brew: `gentleman-programming/tap/gentle-ai`, darwin: `gentleman-programming/tap/gentle-ai` |  |
| gentleman-dots | core | brew: `gentleman-programming/tap/gentleman-dots` |  |
| gga | core,ai | brew: `gentleman-programming/tap/gga` |  |
| gh | core | apt: `gh`, brew: `gh`, darwin: `gh`, dnf: `gh`, pacman: `github-cli`, scoop: `gh`, winget: `GitHub.cli` |  |
| ghostty | core | arch: `ghostty`, darwin: `cask ghostty`, pacman: `ghostty` |  |
| git | core | arch: `git`, brew: `git`, darwin: `git`, debian: `git`, dnf: `git`, scoop: `git`, ubuntu: `git`, winget: `Git.Git`, zypper: `git` |  |
| gnupg | core | apt: `gnupg`, brew: `gnupg`, dnf: `gnupg2`, pacman: `gnupg`, winget: `GnuPG.GnuPG` |  |
| go | core | apt: `golang-go`, brew: `go`, dnf: `golang`, pacman: `go`, winget: `GoLang.Go` |  |
| herdr | core,ai | brew: `herdr`, darwin: `herdr`, yay: `herdr-bin` |  |
| infra | infra | config-only |  |
| lazygit | core | apt: `lazygit`, brew: `lazygit`, pacman: `lazygit`, winget: `JesseDuffield.lazygit` |  |
| mkcert | core,infra | apt: `mkcert`, brew: `mkcert`, dnf: `mkcert`, pacman: `mkcert`, winget: `FiloSottile.mkcert` |  |
| mobile | mobile | config-only |  |
| mole | core | darwin: `mole` |  |
| mongodb-compass | core (optional) | arch: `mongodb-compass`, darwin: `cask mongodb-compass` |  |
| ngrok | core | darwin: `cask ngrok/ngrok/ngrok`, winget: `Ngrok.Ngrok`, yay: `ngrok` |  |
| node | core,ai,mobile,infra | apt: `nodejs npm`, brew: `node`, dnf: `nodejs npm`, pacman: `nodejs npm`, scoop: `nodejs-lts`, winget: `OpenJS.NodeJS.LTS` |  |
| nvim | core | arch: `neovim`, brew: `neovim`, darwin: `neovim`, debian: `neovim`, dnf: `neovim`, scoop: `neovim`, ubuntu: `neovim`, winget: `Neovim.Neovim`, zypper: `neovim` |  |
| ollama | ai (optional) | darwin: `cask ollama`, linux: `script`, pacman: `ollama`, winget: `Ollama.Ollama` |  |
| openjdk-17 | core,mobile | brew: `openjdk@17`, pacman: `jdk17-openjdk` |  |
| pnpm | core | brew: `pnpm`, darwin: `pnpm`, linux: `script`, pacman: `pnpm`, scoop: `pnpm`, winget: `pnpm.pnpm` |  |
| postman | core (optional) | arch: `postman-bin`, darwin: `cask postman`, ubuntu: `script` |  |
| powerlevel10k | core | brew: `powerlevel10k`, yay: `zsh-theme-powerlevel10k` | zsh |
| railway | core | brew: `railway`, default: `@railway/cli` | node |
| ripgrep | core | apt: `ripgrep`, brew: `ripgrep`, dnf: `ripgrep`, pacman: `ripgrep`, winget: `BurntSushi.ripgrep.MSVC` |  |
| shopify-cli | core | brew: `shopify/shopify/shopify-cli`, default: `@shopify/cli` | node |
| skills | core,ai | config-only |  |
| smartwindow | core | darwin: `cask kevnnard/tap/smartwindow` |  |
| ssh | core | config-only |  |
| tailscale | core | arch: `tailscale`, darwin: `cask tailscale`, debian: `script`, linux: `script`, ubuntu: `script`, winget: `Tailscale.Tailscale` |  |
| terminal-browser | core | arch: `terminal-browser-bin`, brew: `cask terminal-browser`, darwin: `cask terminal-browser`, linux: `cask terminal-browser` |  |
| tmux | core | apt: `tmux`, brew: `tmux`, dnf: `tmux`, pacman: `tmux` |  |
| uv | core | brew: `uv`, dnf: `uv`, pacman: `uv`, winget: `astral-sh.uv` |  |
| vercel | core | brew: `vercel`, default: `vercel` | node |
| wakeonlan | core | apt: `wakeonlan`, brew: `wakeonlan`, pacman: `wakeonlan` |  |
| xcode | mobile | darwin: `script` |  |
| yarn | core | apt: `yarnpkg`, brew: `yarn`, dnf: `yarnpkg`, pacman: `yarn`, winget: `Yarn.Yarn` |  |
| zed | core | darwin: `cask zed`, pacman: `zed` |  |
| zoxide | core | apt: `zoxide`, brew: `zoxide`, dnf: `zoxide`, pacman: `zoxide`, winget: `ajeetdsouza.zoxide` |  |
| zsh | core | arch: `zsh`, brew: `zsh`, darwin: `zsh`, debian: `zsh`, dnf: `zsh`, ubuntu: `zsh`, zypper: `zsh` |  |
| zsh-autocomplete | core | brew: `zsh-autocomplete`, pacman: `zsh-autocomplete` | zsh |
| zsh-autosuggestions | core | apt: `zsh-autosuggestions`, brew: `zsh-autosuggestions`, dnf: `zsh-autosuggestions`, pacman: `zsh-autosuggestions` | zsh |
| zsh-syntax-highlighting | core | apt: `zsh-syntax-highlighting`, brew: `zsh-syntax-highlighting`, dnf: `zsh-syntax-highlighting`, pacman: `zsh-syntax-highlighting` | zsh |
