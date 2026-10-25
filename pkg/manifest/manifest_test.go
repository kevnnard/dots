package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultManifest(t *testing.T) {
	m := DefaultManifest()
	if m.Version == "" {
		t.Error("expected non-empty version")
	}

	expectedApps := []string{"nvim", "pi", "ghostty", "zsh", "git", "pnpm", "brew"}
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
