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
	Name        string   `yaml:"name,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Profile     string   `yaml:"profile,omitempty"` // core (default), mobile, infra, ai, optional
	Optional    bool     `yaml:"optional,omitempty"`
	Requires    []string `yaml:"requires,omitempty"` // apps that must be installed first
	// ConfigRequires lists apps the backed-up configuration expects (plugins,
	// helpers). They are pulled only when that configuration is restored.
	ConfigRequires []string `yaml:"config_requires,omitempty"`
	// Provides lists package managers this app makes available (node -> npm).
	Provides []string `yaml:"provides,omitempty"`
	// Check lists commands (or ~/absolute paths) whose presence means the app
	// is already installed. Empty means the app name is used as the command.
	Check   []string                 `yaml:"check,omitempty"`
	Paths   []PathMapping            `yaml:"paths"`
	Exclude []string                 `yaml:"exclude,omitempty"`
	Secrets []string                 `yaml:"secrets,omitempty"`
	Install map[string]InstallRecipe `yaml:"install,omitempty"`
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

// DefaultManifest returns the canonical inventory shipped with dots. It is a
// fresh copy on every call, so callers may modify it freely.
func DefaultManifest() *Manifest {
	var m Manifest
	if err := yaml.Unmarshal([]byte(defaultManifestYAML), &m); err != nil {
		panic(fmt.Sprintf("embedded default manifest is invalid: %v", err))
	}
	return &m
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
