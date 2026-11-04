package test

import (
	"strings"
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

func TestFullPlanResolvesOnEveryPlatform(t *testing.T) {
	m, err := manifest.Load("../manifest.yaml")
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	macOnly := []string{"cliclick", "mole", "smartwindow", "codexbar", "cocoapods"}
	for name, sys := range platforms() {
		steps, err := restore.BuildPlan(m, restore.Select(m, "all"), sys, restore.PlanOptions{WithConfig: true})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		status := map[string]restore.PlanStatus{}
		pos := map[string]int{}
		for i, s := range steps {
			status[s.AppName], pos[s.AppName] = s.Status, i
			if s.Status == restore.StatusInstall && sys.OS != "darwin" && strings.Contains(s.InstallCmd, "--cask") {
				t.Errorf("%s: cask leaked into %s plan: %s", name, s.AppName, s.InstallCmd)
			}
		}
		if _, ok := status["ollama"]; ok {
			t.Errorf("%s: full selection must exclude optional ollama", name)
		}
		for _, app := range []string{"flashspace", "ssh", "skills", "brew"} {
			if status[app] != restore.StatusConfigOnly {
				t.Errorf("%s: %s = %s, want config-only", name, app, status[app])
			}
		}
		if sys.OS != "darwin" {
			for _, app := range macOnly {
				if status[app] != restore.StatusUnsupported {
					t.Errorf("%s: macOS-only %s = %s, want unsupported", name, app, status[app])
				}
			}
		}
		if sys.OS != "windows" && pos["zsh"] > pos["zsh-autosuggestions"] {
			t.Errorf("%s: zsh must be planned before its plugins", name)
		}
	}
}
