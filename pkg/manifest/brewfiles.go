package manifest

import (
	"os"
	"path/filepath"
)

const DefaultCoreBrewfile = `# Brewfile (Core Workstation Setup)
tap "homebrew/bundle"
tap "gentleman-programming/tap"
tap "shopify/shopify"

# Fonts (essential for terminal icons and powerlevel10k)
cask "font-caskaydia-cove-nerd-font"
cask "font-iosevka-term-nerd-font"

# Terminal & Shell
brew "zsh"
brew "powerlevel10k"
brew "zsh-autocomplete"
brew "zsh-autosuggestions"
brew "zsh-syntax-highlighting"
brew "fzf"
brew "zoxide"
brew "atuin"
brew "carapace"
brew "tmux"

# Editors & Git Tools
brew "neovim"
brew "gh"
brew "lazygit"
brew "gnupg"

# Runtimes & Package Managers
brew "go"
brew "fnm"
brew "pnpm"
brew "uv"

# Cloud & Platforms
brew "railway"
brew "vercel"
brew "shopify/shopify/shopify-cli"

# Gentleman Tools
brew "gentleman-programming/tap/engram"
brew "gentleman-programming/tap/gentle-ai"
brew "gentleman-programming/tap/gentleman-dots"
brew "gentleman-programming/tap/gga"
brew "herdr"
brew "mole"

# Casks & GUI Apps
cask "ghostty"
cask "zed"
cask "claude-code"
cask "codex"
cask "codexbar"
cask "smartwindow"
cask "docker-desktop"
cask "dbeaver-community"
cask "bruno"
cask "bitwarden"
cask "ngrok"
cask "dotnet-sdk"
`

const DefaultMobileBrewfile = `# Brewfile.mobile (React Native & Mobile Builds)
brew "openjdk@17"
brew "cocoapods"
brew "fastlane"
`

const DefaultInfraBrewfile = `# Brewfile.infra (Local Infrastructure & Actions Testing)
brew "caddy"
brew "mkcert"
brew "act"
`

const DefaultAIBrewfile = `# Brewfile.ai (AI Agents & Local Models Runtime)
cask "ollama"
`

// WriteDefaultBrewfiles writes the curated Brewfiles if they do not exist.
func WriteDefaultBrewfiles(repoDir string) error {
	files := map[string]string{
		"Brewfile":        DefaultCoreBrewfile,
		"Brewfile.mobile": DefaultMobileBrewfile,
		"Brewfile.infra":  DefaultInfraBrewfile,
		"Brewfile.ai":     DefaultAIBrewfile,
	}

	for filename, content := range files {
		target := filepath.Join(repoDir, filename)
		if _, err := os.Stat(target); os.IsNotExist(err) {
			if err := os.WriteFile(target, []byte(content), 0644); err != nil {
				return err
			}
		}
	}

	return nil
}
