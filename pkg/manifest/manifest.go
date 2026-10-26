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
	Profile     string                   `yaml:"profile,omitempty"` // core (default), mobile, infra, optional
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
	return strings.EqualFold(p, target)
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
				Profile:     "core",
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
				},
			},
			"zsh": {
				Description: "Zsh shell configuration",
				Paths: []PathMapping{
					{Src: "~/.zshrc", Dest: "configs/zsh/.zshrc"},
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
