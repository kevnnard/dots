package test

import (
	"testing"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
	"github.com/kevnnard/dots/pkg/restore"
)

func platforms() map[string]*detector.SystemInfo {
	return map[string]*detector.SystemInfo{
		"omarchy": {OS: "linux", Distro: detector.DistroArch, PrimaryManager: detector.ManagerYay,
			Managers: []detector.PackageManager{detector.ManagerYay, detector.ManagerPacman}},
		"ubuntu": {OS: "linux", Distro: detector.DistroUbuntu, PrimaryManager: detector.ManagerApt,
			Managers: []detector.PackageManager{detector.ManagerApt}},
		"fedora": {OS: "linux", Distro: detector.DistroFedora, PrimaryManager: detector.ManagerDnf,
			Managers: []detector.PackageManager{detector.ManagerDnf}},
		"windows": {OS: "windows", Distro: detector.DistroWindows, PrimaryManager: detector.ManagerWinget,
			Managers: []detector.PackageManager{detector.ManagerWinget}},
		"macos": {OS: "darwin", Distro: detector.DistroDarwin, PrimaryManager: detector.ManagerBrew,
			Managers: []detector.PackageManager{detector.ManagerBrew}},
	}
}

// Tools needed on every platform to bootstrap a working workstation.
var essentialApps = []string{"node", "pi", "claude", "codex", "git", "nvim"}

func TestEssentialAppsHaveRecipeOnEveryPlatform(t *testing.T) {
	m, err := manifest.Load("../manifest.yaml")
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	for name, sys := range platforms() {
		for _, app := range essentialApps {
			cfg, ok := m.Apps[app]
			if !ok {
				t.Fatalf("app %q missing from manifest", app)
			}
			plan := restore.PlanApp(app, cfg, sys)
			if !plan.RecipeFound || plan.InstallCmd == "" {
				t.Errorf("%s: no install recipe for %q", name, app)
			}
		}
	}
}

func TestNativeManagerPreferredOnOmarchy(t *testing.T) {
	m, err := manifest.Load("../manifest.yaml")
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	plan := restore.PlanApp("gh", m.Apps["gh"], platforms()["omarchy"])
	if plan.InstallCmd != "sudo pacman -S --noconfirm github-cli" {
		t.Errorf("got %q", plan.InstallCmd)
	}
}

func TestNodeIsPlannedBeforeNpmTools(t *testing.T) {
	m, err := manifest.Load("../manifest.yaml")
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	pos := map[string]int{}
	for i, p := range restore.PlanAll(m, platforms()["omarchy"]) {
		pos[p.AppName] = i
	}
	for _, tool := range []string{"pi", "claude", "codex"} {
		if pos["node"] >= pos[tool] {
			t.Errorf("node (%d) must be planned before %s (%d)", pos["node"], tool, pos[tool])
		}
	}
}
