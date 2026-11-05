package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultManifest(t *testing.T) {
	m := DefaultManifest()
	if m.Version == "" {
		t.Error("expected non-empty version")
	}

	expectedApps := []string{"nvim", "pi", "ghostty", "zsh", "git", "pnpm", "brew", "claude", "codex", "zed", "atuin", "flashspace", "gh", "ssh", "ollama", "gentle-ai", "engram", "herdr", "orca", "android-studio", "xcode", "tailscale", "mongodb-compass", "postman"}
	for _, app := range expectedApps {
		if _, ok := m.Apps[app]; !ok {
			t.Errorf("expected default manifest to contain %s", app)
		}
	}

	nvim := m.Apps["nvim"]
	if len(nvim.Paths) == 0 {
		t.Error("expected nvim to have at least one path mapping")
	}
	if nvim.Install["darwin"].Package != "neovim" {
		t.Errorf("expected nvim darwin package to be neovim, got %s", nvim.Install["darwin"].Package)
	}
}

func TestDefaultManifestMatchesRepositoryManifest(t *testing.T) {
	want, err := Load("../../manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(DefaultManifest(), want) {
		t.Error("DefaultManifest() drifted from manifest.yaml; regenerate default_manifest.go")
	}
}

func TestEmbeddedBrewfilesMatchRepository(t *testing.T) {
	for name, content := range defaultBrewfiles() {
		data, err := os.ReadFile(filepath.Join("../..", name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != content {
			t.Errorf("embedded %s drifted from the repository copy", name)
		}
	}
}

func TestManifestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	manifestFile := filepath.Join(tmpDir, "manifest.yaml")

	m := DefaultManifest()
	err := m.Save(manifestFile)
	if err != nil {
		t.Fatalf("failed to save manifest: %v", err)
	}

	loaded, err := Load(manifestFile)
	if err != nil {
		t.Fatalf("failed to load manifest: %v", err)
	}

	if loaded.Version != m.Version {
		t.Errorf("expected version %s, got %s", m.Version, loaded.Version)
	}
	if len(loaded.Apps) != len(m.Apps) {
		t.Errorf("expected %d apps, got %d", len(m.Apps), len(loaded.Apps))
	}
}

func TestMatchesProfile(t *testing.T) {
	appCore := AppConfig{Profile: "core"}
	appMobile := AppConfig{Profile: "mobile"}
	appMulti := AppConfig{Profile: "core,ai"}
	appDefault := AppConfig{}

	if !appCore.MatchesProfile("core") {
		t.Error("expected appCore to match 'core'")
	}
	if !appDefault.MatchesProfile("core") {
		t.Error("expected appDefault to match 'core'")
	}
	if appCore.MatchesProfile("mobile") {
		t.Error("expected appCore not to match 'mobile'")
	}
	if !appMobile.MatchesProfile("mobile") {
		t.Error("expected appMobile to match 'mobile'")
	}
	if !appMulti.MatchesProfile("core") || !appMulti.MatchesProfile("ai") {
		t.Error("expected appMulti to match both 'core' and 'ai'")
	}
	if !appMobile.MatchesProfile("all") {
		t.Error("expected appMobile to match 'all'")
	}
	if !appMobile.MatchesProfile("") {
		t.Error("expected appMobile to match empty target (all)")
	}
}

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot get user home dir")
	}

	expanded, err := ExpandPath("~/.config/nvim")
	if err != nil {
		t.Fatalf("failed to expand path: %v", err)
	}
	expected := filepath.Join(home, ".config", "nvim")
	if expanded != expected {
		t.Errorf("expected %s, got %s", expected, expanded)
	}
}

// Gentle Shell keeps its own agent home, ~/.gentle-shell/agent, on macOS and
// Linux alike. Its configs travel like Pi's; its login files never do, not
// even as templates: masking keeps OAuth identifiers such as codeVerifier.
func TestGentleShellAgentHomeIsDeclared(t *testing.T) {
	gs, ok := DefaultManifest().Apps["gentle-shell"]
	if !ok {
		t.Fatal("manifest must declare gentle-shell")
	}
	srcs := map[string]string{}
	for _, p := range gs.Paths {
		if !strings.HasPrefix(p.Src, "~/.gentle-shell/agent/") {
			t.Errorf("path %s is outside the Gentle Shell agent home", p.Src)
		}
		srcs[p.Src] = p.Dest
	}
	for _, rel := range []string{"settings.json", "models.json", "mcp.json", "trust.json", "skills", "agents", "chains", "extensions"} {
		if dest := srcs["~/.gentle-shell/agent/"+rel]; dest != "configs/gentle-shell/"+rel {
			t.Errorf("%s: dest %q, want configs/gentle-shell/%s", rel, dest, rel)
		}
	}
	for _, never := range []string{"auth.json", "mcp-auth.json", "antigravity-accounts.json", "configs", "sessions"} {
		if _, ok := srcs["~/.gentle-shell/agent/"+never]; ok {
			t.Errorf("%s holds credentials or private state and must not be backed up", never)
		}
	}
	secrets := strings.Join(gs.Secrets, ",")
	for _, s := range []string{"mcp.json"} {
		if !strings.Contains(","+secrets+",", ","+s+",") {
			t.Errorf("%s must be a secret (template only), secrets = %v", s, gs.Secrets)
		}
	}
}

// Login files never travel, not even as templates: masking keeps OAuth
// identifiers such as codeVerifier, client_id and account emails.
func TestNoAppBacksUpLoginFiles(t *testing.T) {
	login := map[string]bool{"auth.json": true, "mcp-auth.json": true, "oauth_creds.json": true, "google_accounts.json": true}
	for name, app := range DefaultManifest().Apps {
		for _, p := range app.Paths {
			if login[filepath.Base(p.Src)] {
				t.Errorf("%s backs up the login file %s", name, p.Src)
			}
		}
	}
}
