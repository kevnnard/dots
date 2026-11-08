package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	// PackageLinks is written only into transferred backups, never into the
	// repository manifest: it carries the backed-up links that pointed into a
	// package install, which an archive cannot hold as links.
	PackageLinks []PackageLink `yaml:"package_links,omitempty"`
}

// PackageLink is a backed-up symlink into a resource of an installed package.
// Path is the staged entry (slash-separated, below one of the app's path
// dests); Resource is the unchanged path below the package version directory.
// Every field is untrusted: restore re-resolves the link against the package
// installed on the receiving machine (see pkg/packagelink).
type PackageLink struct {
	Path     string `yaml:"path"`
	Manager  string `yaml:"manager"`
	Kind     string `yaml:"kind"`
	Package  string `yaml:"package"`
	Version  string `yaml:"version,omitempty"`
	Resource string `yaml:"resource"`
	// Type is "dir" or "file" when the source resource could be inspected.
	Type string `yaml:"type,omitempty"`
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

// WithPackageLinks returns the manifest YAML data with each named app's
// package_links set to links[app], replacing any previous value. Everything
// else in data is kept.
func WithPackageLinks(data []byte, links map[string][]PackageLink) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("unmarshaling manifest yaml: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, fmt.Errorf("manifest is not a single YAML document")
	}
	apps := mappingValue(doc.Content[0], "apps")
	if apps == nil {
		return nil, fmt.Errorf("manifest declares no apps")
	}
	names := make([]string, 0, len(links))
	for name := range links {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		app := mappingValue(apps, name)
		if app == nil {
			return nil, fmt.Errorf("manifest does not declare app %q", name)
		}
		var value yaml.Node
		if err := value.Encode(links[name]); err != nil {
			return nil, err
		}
		kept := app.Content[:0:0]
		for i := 0; i+1 < len(app.Content); i += 2 {
			if app.Content[i].Value != "package_links" {
				kept = append(kept, app.Content[i], app.Content[i+1])
			}
		}
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "package_links"}
		app.Content = append(kept, key, &value)
	}
	return yaml.Marshal(&doc)
}

// mappingValue returns the mapping value of key in node, or nil.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key && node.Content[i+1].Kind == yaml.MappingNode {
			return node.Content[i+1]
		}
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
