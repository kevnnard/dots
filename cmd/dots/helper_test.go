package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDotfilesDirExplicitFlag(t *testing.T) {
	dotfilesDirFlag = "/custom/path"
	defer func() { dotfilesDirFlag = "" }()

	dir, err := resolveDotfilesDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/custom/path" {
		t.Errorf("expected /custom/path, got %s", dir)
	}
}

func TestResolveDotfilesDirEnv(t *testing.T) {
	dotfilesDirFlag = ""
	os.Setenv("DOTFILES_DIR", "/env/path")
	defer os.Unsetenv("DOTFILES_DIR")

	dir, err := resolveDotfilesDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/env/path" {
		t.Errorf("expected /env/path, got %s", dir)
	}
}

func TestResolveDotfilesDirCwd(t *testing.T) {
	dotfilesDirFlag = ""
	os.Unsetenv("DOTFILES_DIR")

	tmpDir := t.TempDir()
	manifestFile := filepath.Join(tmpDir, "manifest.yaml")
	os.WriteFile(manifestFile, []byte("version: 1.0.0\n"), 0644)

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	dir, err := resolveDotfilesDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	realTmp, _ := filepath.EvalSymlinks(tmpDir)
	realDir, _ := filepath.EvalSymlinks(dir)
	if realDir != realTmp {
		t.Errorf("expected %s, got %s", realTmp, realDir)
	}
}
