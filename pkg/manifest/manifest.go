package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type PathMapping struct {
	Src  string `yaml:"src"`
	Dest string `yaml:"dest"`
}

type InstallRecipe struct {
	Manager string `yaml:"manager"` // brew, apt, pacman, pnpm, script
	Package string `yaml:"package,omitempty"`
	Cask    string `yaml:"cask,omitempty"`
	Command string `yaml:"command,omitempty"`
}

type AppConfig struct {
	Name        string                   `yaml:"name,omitempty"`
	Description string                   `yaml:"description,omitempty"`
	Profile     string                   `yaml:"profile,omitempty"` // core (default), mobile, infra, ai, optional
	Optional    bool                     `yaml:"optional,omitempty"`
	Paths       []PathMapping            `yaml:"paths"`
	Exclude     []string                 `yaml:"exclude,omitempty"`
	Secrets     []string                 `yaml:"secrets,omitempty"`
	Install     map[string]InstallRecipe `yaml:"install,omitempty"`
}

// MatchesProfile returns true if the app matches the requested profile.
func (a AppConfig) MatchesProfile(target string) bool {
	if target == "" || target == "all" {
		return true
	}
	p := a.Profile
	if p == "" {
		p = "core"
	}
	for _, part := range strings.Split(p, ",") {
		if strings.EqualFold(strings.TrimSpace(part), target) {
			return true
		}
	}
	return false
}

type Manifest struct {
	Version     string               `yaml:"version"`
	DotfilesDir string               `yaml:"dotfiles_dir,omitempty"`
	Apps        map[string]AppConfig `yaml:"apps"`
}

// DefaultManifest returns the standard baseline configuration tailored for macOS / cross-platform setups.
func DefaultManifest() *Manifest {
	return &Manifest{
		Version:     "1.0.0",
		DotfilesDir: "~/.dotfiles",
		Apps: map[string]AppConfig{
			"nvim": {
				Description: "Neovim editor and plugins",
				Paths: []PathMapping{
					{Src: "~/.config/nvim", Dest: "configs/nvim"},
				},
				Exclude: []string{
					".git/**",
					".DS_Store",
					"lazy-lock.json.backup",
					"undo/**",
				},
				Install: map[string]InstallRecipe{
					"darwin": {Manager: "brew", Package: "neovim"},
					"debian": {Manager: "apt", Package: "neovim"},
					"ubuntu": {Manager: "apt", Package: "neovim"},
					"arch":   {Manager: "pacman", Package: "neovim"},
				},
			},
			"ghostty": {
				Description: "Ghostty terminal emulator",
				Paths: []PathMapping{
					{Src: "~/.config/ghostty", Dest: "configs/ghostty"},
				},
				Exclude: []string{".DS_Store"},
				Install: map[string]InstallRecipe{
					"darwin": {Manager: "brew", Cask: "ghostty"},
					"arch":   {Manager: "yay", Package: "ghostty"},
				},
			},
			"pi": {
				Description: "Pi coding agent harness settings, models, MCP servers, skills, and agents",
				Profile:     "core,ai",
				Paths: []PathMapping{
					{Src: "~/.pi/agent/settings.json", Dest: "configs/pi/settings.json"},
					{Src: "~/.pi/agent/models.json", Dest: "configs/pi/models.json"},
					{Src: "~/.pi/agent/mcp.json", Dest: "configs/pi/mcp.json"},
					{Src: "~/.pi/agent/trust.json", Dest: "configs/pi/trust.json"},
					{Src: "~/.pi/agent/skills", Dest: "configs/pi/skills"},
					{Src: "~/.pi/agent/agents", Dest: "configs/pi/agents"},
					{Src: "~/.pi/agent/chains", Dest: "configs/pi/chains"},
					{Src: "~/.pi/agent/auth.json", Dest: "configs/pi/auth.json"},
					{Src: "~/.pi/agent/mcp-auth.json", Dest: "configs/pi/mcp-auth.json"},
				},
				Exclude: []string{
					"sessions/**",
					"web-search-cache/**",
					"cc-cli-logs/**",
					"*.log",
					"*.tmp.*",
					"*.cache.json",
					"crashes.json",
					"npm/**",
					"bin/**",
					"node_modules/**",
					".git/**",
					".DS_Store",
				},
				Secrets: []string{
					"auth.json",
					"mcp-auth.json",
					"mcp.json",
				},
			},
			"skills": {
				Description: "Agent skills catalog in ~/.agents/skills",
				Profile:     "core,ai",
				Paths: []PathMapping{
					{Src: "~/.agents/skills", Dest: "configs/agents/skills"},
				},
				Exclude: []string{
					".git/**",
					".DS_Store",
				},
			},
			"zsh": {
				Description: "Zsh shell configuration and Powerlevel10k theme",
				Profile:     "core",
				Paths: []PathMapping{
					{Src: "~/.zshrc", Dest: "configs/zsh/.zshrc"},
					{Src: "~/.p10k.zsh", Dest: "configs/zsh/.p10k.zsh"},
				},
				Exclude: []string{
					".zsh_history",
					".zsh_sessions/**",
				},
				Install: map[string]InstallRecipe{
					"darwin": {Manager: "brew", Package: "zsh"},
					"debian": {Manager: "apt", Package: "zsh"},
					"ubuntu": {Manager: "apt", Package: "zsh"},
					"arch":   {Manager: "pacman", Package: "zsh"},
				},
			},
			"claude": {
				Description: "Claude Code CLI settings, MCP servers, agents, and instructions",
				Profile:     "core,ai",
				Paths: []PathMapping{
					{Src: "~/.claude/settings.json", Dest: "configs/claude/settings.json"},
					{Src: "~/.claude/mcp.json", Dest: "configs/claude/mcp.json"},
					{Src: "~/.claude/CLAUDE.md", Dest: "configs/claude/CLAUDE.md"},
					{Src: "~/.claude/skills", Dest: "configs/claude/skills"},
					{Src: "~/.claude/agents", Dest: "configs/claude/agents"},
				},
				Secrets: []string{
					"mcp.json",
				},
				Exclude: []string{
					"session-env/**",
					"sessions/**",
					"history.jsonl",
					"file-history/**",
					"daemon.log",
					"cache/**",
					"backups/**",
					"*.tmp.*",
					"*.backup",
					".DS_Store",
				},
			},
			"codex": {
				Description: "Codex CLI configurations, hooks, agents, and rules",
				Profile:     "core,ai",
				Paths: []PathMapping{
					{Src: "~/.codex/config.toml", Dest: "configs/codex/config.toml"},
					{Src: "~/.codex/hooks.json", Dest: "configs/codex/hooks.json"},
					{Src: "~/.codex/AGENTS.md", Dest: "configs/codex/AGENTS.md"},
					{Src: "~/.codex/rules", Dest: "configs/codex/rules"},
					{Src: "~/.codex/skills", Dest: "configs/codex/skills"},
					{Src: "~/.codex/auth.json", Dest: "configs/codex/auth.json"},
				},
				Exclude: []string{
					"*.sqlite*",
					"cache/**",
					"sessions/**",
					"archived_sessions/**",
					"history.jsonl",
					"models_cache.json",
					"log/**",
					".tmp/**",
					".DS_Store",
				},
				Secrets: []string{
					"auth.json",
				},
			},
			"ollama": {
				Description: "Ollama local AI runtime and models runner",
				Profile:     "ai",
				Optional:    true,
				Paths: []PathMapping{
					{Src: "~/.ollama", Dest: "configs/ollama"},
				},
				Exclude: []string{
					"models/**",
					"blobs/**",
					"manifests/**",
					"*.bin",
					"*.gguf",
				},
				Install: map[string]InstallRecipe{
					"darwin": {Manager: "brew", Cask: "ollama"},
					"linux":  {Manager: "script", Command: "curl -fsSL https://ollama.com/install.sh | sh"},
				},
			},
			"zed": {
				Description: "Zed Editor configurations, keymaps, and themes",
				Profile:     "core",
				Paths: []PathMapping{
					{Src: "~/.config/zed", Dest: "configs/zed"},
				},
				Exclude: []string{".DS_Store"},
				Install: map[string]InstallRecipe{
					"darwin": {Manager: "brew", Cask: "zed"},
				},
			},
			"atuin": {
				Description: "Atuin shell history sync configuration",
				Profile:     "core",
				Paths: []PathMapping{
					{Src: "~/.config/atuin/config.toml", Dest: "configs/atuin/config.toml"},
				},
				Install: map[string]InstallRecipe{
					"darwin": {Manager: "brew", Package: "atuin"},
				},
			},
			"flashspace": {
				Description: "Flashspace macOS workspace manager settings and profiles",
				Profile:     "core",
				Paths: []PathMapping{
					{Src: "~/.config/flashspace", Dest: "configs/flashspace"},
				},
				Exclude: []string{".DS_Store"},
			},
			"gh": {
				Description: "GitHub CLI configuration and preferences",
				Profile:     "core",
				Paths: []PathMapping{
					{Src: "~/.config/gh/config.yml", Dest: "configs/gh/config.yml"},
					{Src: "~/.config/gh/hosts.yml", Dest: "configs/gh/hosts.yml"},
				},
				Secrets: []string{
					"hosts.yml",
				},
				Install: map[string]InstallRecipe{
					"darwin": {Manager: "brew", Package: "gh"},
				},
			},
			"ssh": {
				Description: "SSH host aliases and configuration (excludes private keys)",
				Profile:     "core",
				Paths: []PathMapping{
					{Src: "~/.ssh/config", Dest: "configs/ssh/config"},
				},
				Secrets: []string{
					"config",
				},
				Exclude: []string{
					"id_*",
					"*.pub",
					"known_hosts*",
					"digitalocean*",
				},
			},
			"git": {
				Description: "Git global configuration",
				Paths: []PathMapping{
					{Src: "~/.gitconfig", Dest: "configs/git/.gitconfig"},
					{Src: "~/.config/git", Dest: "configs/git/config_dir"},
				},
				Exclude: []string{".DS_Store"},
				Install: map[string]InstallRecipe{
					"darwin": {Manager: "brew", Package: "git"},
					"debian": {Manager: "apt", Package: "git"},
					"ubuntu": {Manager: "apt", Package: "git"},
					"arch":   {Manager: "pacman", Package: "git"},
				},
			},
			"pnpm": {
				Description: "Fast, disk space efficient package manager",
				Paths:       []PathMapping{},
				Install: map[string]InstallRecipe{
					"darwin": {Manager: "brew", Package: "pnpm"},
					"linux":  {Manager: "script", Command: "curl -fsSL https://get.pnpm.io/install.sh | sh -"},
				},
			},
			"brew": {
				Description: "Core Homebrew bundle (CLI tools, fonts, GUIs)",
				Profile:     "core",
				Paths: []PathMapping{
					{Src: "Brewfile", Dest: "Brewfile"},
				},
				Install: map[string]InstallRecipe{
					"darwin": {Manager: "script", Command: "brew bundle --file=Brewfile"},
				},
			},
			"mobile": {
				Description: "React Native & Mobile Build Toolchain (OpenJDK 17, CocoaPods, Fastlane)",
				Profile:     "mobile",
				Paths: []PathMapping{
					{Src: "Brewfile.mobile", Dest: "Brewfile.mobile"},
				},
				Install: map[string]InstallRecipe{
					"darwin": {Manager: "script", Command: "brew bundle --file=Brewfile.mobile"},
				},
			},
			"infra": {
				Description: "Local infrastructure & Actions testing (Caddy, mkcert, act)",
				Profile:     "infra",
				Paths: []PathMapping{
					{Src: "Brewfile.infra", Dest: "Brewfile.infra"},
				},
				Install: map[string]InstallRecipe{
					"darwin": {Manager: "script", Command: "brew bundle --file=Brewfile.infra"},
				},
			},
		},
	}
}

// Load reads and parses a manifest YAML file.
func Load(filePath string) (*Manifest, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading manifest file: %w", err)
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshaling manifest yaml: %w", err)
	}

	return &m, nil
}

// Save writes the manifest out to the given path in YAML format.
func (m *Manifest) Save(filePath string) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating manifest directory: %w", err)
	}

	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshaling manifest yaml: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("writing manifest file: %w", err)
	}

	return nil
}

// ExpandPath expands leading ~ to user home directory and environment variables.
func ExpandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("getting home directory: %w", err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	return os.ExpandEnv(path), nil
}
