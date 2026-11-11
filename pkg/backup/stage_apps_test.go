package backup

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

const repoManifest = `version: 1.0.0
apps:
    # the shell
    zsh:
        paths:
            - src: ~/.zshrc
              dest: configs/zsh/.zshrc
    btop:
        install:
            brew: {manager: brew, package: btop}
`

func TestStageManifestAppsAddsAppsOnlyToTheStage(t *testing.T) {
	repo, stage := t.TempDir(), filepath.Join(t.TempDir(), "stage")
	if err := os.WriteFile(filepath.Join(repo, "manifest.yaml"), []byte(repoManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	apps := map[string]manifest.AppConfig{
		"godot-mono": {OS: []string{"darwin"}, Install: map[string]manifest.InstallRecipe{"darwin": {Manager: "brew", Cask: "godot-mono"}}},
		"btop": {Install: map[string]manifest.InstallRecipe{"brew": {Manager: "brew", Package: "btop"}},
			Paths: []manifest.PathMapping{{Src: "~/.config/btop", Dest: "configs/btop", OS: []string{"darwin", "linux"}}}},
	}
	if err := StageManifestApps(repo, stage, apps); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(repo, "manifest.yaml")); string(data) != repoManifest {
		t.Errorf("repository manifest changed:\n%s", data)
	}
	m, err := manifest.Load(filepath.Join(stage, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Apps["godot-mono"]; !reflect.DeepEqual(got.OS, []string{"darwin"}) || got.Install["darwin"].Cask != "godot-mono" {
		t.Errorf("staged godot-mono = %+v", got)
	}
	if got := m.Apps["btop"]; len(got.Paths) != 1 || got.Install["brew"].Package != "btop" {
		t.Errorf("staged btop = %+v", got)
	}
	if got := m.Apps["zsh"]; len(got.Paths) != 1 {
		t.Errorf("declared apps must stay: %+v", got)
	}
}

func TestStageManifestAppsWithoutAppsCopiesBytes(t *testing.T) {
	repo, stage := t.TempDir(), filepath.Join(t.TempDir(), "stage")
	if err := os.WriteFile(filepath.Join(repo, "manifest.yaml"), []byte(repoManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StageManifestApps(repo, stage, nil); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(stage, "manifest.yaml")); string(data) != repoManifest {
		t.Errorf("staged manifest must be the repository bytes:\n%s", data)
	}
}
