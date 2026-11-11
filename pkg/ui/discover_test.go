package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/discover"
	"github.com/kevnnard/dots/pkg/manifest"
)

func TestBackupShowsDiscoveryAndStagesItOnlyInTheStage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	repoManifest := "version: 1.0.0\napps:\n    zsh:\n        paths:\n            - src: ~/.zshrc\n              dest: configs/zsh/.zshrc\n"
	for p, body := range map[string]string{
		filepath.Join(repo, "manifest.yaml"):                   repoManifest,
		filepath.Join(home, ".zshrc"):                          "# zsh\n",
		filepath.Join(home, ".config/wezterm/wezterm.lua"):     "return {}\n",
		filepath.Join(home, ".config/configstore/update.json"): "{}\n",
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := manifest.Load(filepath.Join(repo, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(m, &detector.SystemInfo{OS: "darwin"}, repo)
	model.Discovery = discover.Options{Home: home}
	model.Action = "backup"
	done, _ := model.executeAction()

	logs := strings.Join(done.(Model).Logs, "\n")
	for _, want := range []string{"New:", "  - wezterm (config): darwin, linux", "Skipped:", "  - configstore: may hold credentials"} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %q:\n%s", want, logs)
		}
	}
	stage := filepath.Join(home, ".cache", "dots", "backup")
	if _, err := os.Stat(filepath.Join(stage, "configs/wezterm/wezterm.lua")); err != nil {
		t.Errorf("discovered config not staged: %v", err)
	}
	staged, err := manifest.Load(filepath.Join(stage, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := staged.Apps["wezterm"]; !ok {
		t.Error("staged manifest lacks the discovered app")
	}
	if data, _ := os.ReadFile(filepath.Join(repo, "manifest.yaml")); string(data) != repoManifest {
		t.Errorf("repository manifest changed:\n%s", data)
	}
}
