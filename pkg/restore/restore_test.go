package restore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

func TestPlanRestore(t *testing.T) {
	app := manifest.AppConfig{
		Description: "Neovim",
		Paths: []manifest.PathMapping{
			{Src: "~/.config/nvim", Dest: "configs/nvim"},
		},
		Install: map[string]manifest.InstallRecipe{
			"darwin": {Manager: "brew", Package: "neovim"},
			"debian": {Manager: "apt", Package: "neovim"},
		},
	}

	sysDarwin := &detector.SystemInfo{
		OS:             "darwin",
		Distro:         detector.DistroDarwin,
		PrimaryManager: detector.ManagerBrew,
	}

	plan := PlanApp("nvim", app, sysDarwin)
	if plan.InstallCmd == "" {
		t.Error("expected install command for darwin")
	}
	if plan.InstallCmd != "brew install neovim" {
		t.Errorf("expected 'brew install neovim', got %s", plan.InstallCmd)
	}

	sysDebian := &detector.SystemInfo{
		OS:             "linux",
		Distro:         detector.DistroDebian,
		PrimaryManager: detector.ManagerApt,
	}

	planDeb := PlanApp("nvim", app, sysDebian)
	if planDeb.InstallCmd != "sudo apt-get install -y neovim" {
		t.Errorf("expected 'sudo apt-get install -y neovim', got %s", planDeb.InstallCmd)
	}
}

func TestApplyFiles(t *testing.T) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "dotfiles")
	targetHome := filepath.Join(tmpDir, "home")

	// Setup mock repo files
	nvimRepo := filepath.Join(repoDir, "configs", "nvim")
	os.MkdirAll(nvimRepo, 0755)
	os.WriteFile(filepath.Join(nvimRepo, "init.lua"), []byte("print('restored')\n"), 0644)

	app := manifest.AppConfig{
		Paths: []manifest.PathMapping{
			{Src: filepath.Join(targetHome, ".config", "nvim"), Dest: "configs/nvim"},
		},
	}

	res, err := ApplyAppFiles("nvim", app, repoDir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Restored) != 1 {
		t.Fatalf("expected 1 restored file, got %d", len(res.Restored))
	}

	targetFile := filepath.Join(targetHome, ".config", "nvim", "init.lua")
	data, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("failed reading restored file: %v", err)
	}
	if string(data) != "print('restored')\n" {
		t.Errorf("expected 'print('restored')\\n', got %s", string(data))
	}
}
