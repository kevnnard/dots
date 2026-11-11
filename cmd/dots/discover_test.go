package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/clean"
	"github.com/kevnnard/dots/pkg/discover"
	"github.com/kevnnard/dots/pkg/manifest"
)

// No test may query the real Homebrew or read the real home through backup
// discovery; tests that exercise it inject their own options.
func TestMain(m *testing.M) {
	discoverOptions = func(string) discover.Options { return discover.Options{} }
	os.Exit(m.Run())
}

type discoverExec struct {
	t   *testing.T
	out map[string]string
}

func (f discoverExec) Run(_ context.Context, argv []string) (clean.Result, error) {
	out, ok := f.out[strings.Join(argv, " ")]
	if !ok {
		f.t.Errorf("unexpected command %q", strings.Join(argv, " "))
		return clean.Result{}, errors.New("unexpected command")
	}
	return clean.Result{Stdout: []byte(out)}, nil
}

const discoverManifest = `version: 1.0.0
apps:
    zsh:
        paths:
            - src: ~/.zshrc
              dest: configs/zsh/.zshrc
    btop:
        install:
            brew: {manager: brew, package: btop}
`

func useDiscovery(t *testing.T, opts discover.Options) {
	t.Helper()
	old := discoverOptions
	t.Cleanup(func() { discoverOptions = old })
	discoverOptions = func(string) discover.Options { return opts }
}

func TestFullBackupStagesDiscoveredItemsOnlyInTheStage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "manifest.yaml"), []byte(discoverManifest))
	writeTestFile(t, filepath.Join(home, ".zshrc"), []byte("# zsh\n"))
	writeTestFile(t, filepath.Join(home, ".config/wezterm/wezterm.lua"), []byte("return {}\n"))
	writeTestFile(t, filepath.Join(home, ".config/wezterm/.env"), []byte("TOKEN=fixture-value\n"))
	writeTestFile(t, filepath.Join(home, ".config/gcloud/credentials.db"), []byte("x"))
	useDiscovery(t, discover.Options{Home: home, Exec: discoverExec{t: t, out: map[string]string{
		"brew leaves --installed-on-request":     "btop\nripgrep\n",
		"brew list --cask":                       "godot-mono\n",
		"brew tap":                               "",
		"brew info --json=v2 ripgrep godot-mono": `{"formulae":[{"full_name":"ripgrep","bottle":{"stable":{"files":{"x86_64_linux":{}}}}}],"casks":[{"token":"godot-mono","full_token":"godot-mono","tap":"homebrew/cask"}]}`,
	}}})

	stage := filepath.Join(t.TempDir(), "stage")
	out, err := runBackup(t, "--dir", repo, "--out", stage)
	if err != nil {
		t.Fatalf("backup: %v\n%s", err, out)
	}
	want := strings.Join([]string{
		"New:",
		"  - godot-mono (cask): darwin",
		"  - ripgrep (formula): darwin, linux",
		"  - wezterm (config): darwin, linux",
		"Skipped:",
		"  - gcloud: may hold credentials",
	}, "\n") + "\n"
	if !strings.Contains(out, want) {
		t.Errorf("output lacks the discovery lists:\n%s\nwant:\n%s", out, want)
	}
	if strings.Contains(out, "fixture-value") {
		t.Error("output exposes a secret value")
	}

	if data, _ := os.ReadFile(filepath.Join(repo, "manifest.yaml")); string(data) != discoverManifest {
		t.Errorf("repository manifest changed:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(stage, "configs/wezterm/wezterm.lua")); err != nil {
		t.Errorf("discovered config not staged: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stage, "configs/wezterm/.env.template")); err != nil {
		t.Errorf("secret in a discovered config must be masked: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stage, "configs/wezterm/.env")); !os.IsNotExist(err) {
		t.Error("the raw secret must not be staged")
	}
	if _, err := os.Stat(filepath.Join(stage, "configs/gcloud")); !os.IsNotExist(err) {
		t.Error("credential directories must not be staged")
	}
	m, err := manifest.Load(filepath.Join(stage, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for name, osList := range map[string][]string{"godot-mono": {"darwin"}, "ripgrep": {"darwin", "linux"}, "wezterm": {"darwin", "linux"}} {
		if got := m.Apps[name].OS; !reflect.DeepEqual(got, osList) {
			t.Errorf("staged %s os = %v, want %v", name, got, osList)
		}
	}
}

func TestFullBackupWithNothingNewPrintsNothingNew(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "manifest.yaml"), []byte(discoverManifest))
	writeTestFile(t, filepath.Join(home, ".zshrc"), []byte("# zsh\n"))
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(t.TempDir(), "stage")

	before, err := runBackup(t, "--dir", repo, "--out", stage)
	if err != nil {
		t.Fatal(err)
	}
	useDiscovery(t, discover.Options{Home: home, Exec: discoverExec{t: t, out: map[string]string{
		"brew leaves --installed-on-request": "btop\n",
		"brew list --cask":                   "",
		"brew tap":                           "homebrew/core\n",
	}}})
	after, err := runBackup(t, "--dir", repo, "--out", stage)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("nothing new must print nothing new:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if data, _ := os.ReadFile(filepath.Join(stage, "manifest.yaml")); string(data) != discoverManifest {
		t.Errorf("staged manifest must be the repository bytes:\n%s", data)
	}
}
