package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/kevnnard/dots/pkg/manifest"
)

func resolveDotfilesDir() (string, error) {
	if dotfilesDirFlag != "" {
		return manifest.ExpandPath(dotfilesDirFlag)
	}
	if env := os.Getenv("DOTFILES_DIR"); env != "" {
		return manifest.ExpandPath(env)
	}

	// 1. Check current working directory
	cwd, err := os.Getwd()
	if err == nil {
		if fileExists(filepath.Join(cwd, "manifest.yaml")) {
			return cwd, nil
		}
		parent := filepath.Dir(cwd)
		if fileExists(filepath.Join(parent, "manifest.yaml")) {
			return parent, nil
		}
	}

	// 2. Check executable directory (e.g. repo/bin/dots -> repo/)
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if fileExists(filepath.Join(exeDir, "manifest.yaml")) {
			return exeDir, nil
		}
		parentDir := filepath.Dir(exeDir)
		if fileExists(filepath.Join(parentDir, "manifest.yaml")) {
			return parentDir, nil
		}
	}

	// 3. Check standard repo clone path: ~/Repositories/dots
	repoPath, _ := manifest.ExpandPath("~/Repositories/dots")
	if fileExists(filepath.Join(repoPath, ".git")) {
		return repoPath, nil
	}

	// 4. Default fallback: ~/.dotfiles
	return manifest.ExpandPath("~/.dotfiles")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func getManifestPath(repoDir string) string {
	return filepath.Join(repoDir, "manifest.yaml")
}

func loadOrInitManifest(repoDir string) (*manifest.Manifest, error) {
	manifestPath := getManifestPath(repoDir)
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		// If git repo does not exist in repoDir, attempt clone if git is available
		gitDir := filepath.Join(repoDir, ".git")
		if !fileExists(gitDir) {
			if autoCloneRepo(repoDir) {
				if fileExists(manifestPath) {
					return manifest.Load(manifestPath)
				}
			}
		}

		m := manifest.DefaultManifest()
		if err := m.Save(manifestPath); err != nil {
			return nil, fmt.Errorf("failed creating default manifest at %s: %w", manifestPath, err)
		}
		return m, nil
	}
	return manifest.Load(manifestPath)
}

func autoCloneRepo(destDir string) bool {
	repoURL := os.Getenv("DOTFILES_REPO_URL")
	if repoURL == "" {
		repoURL = "https://github.com/kevnnard/dots.git"
	}

	cmd := exec.Command("git", "clone", repoURL, destDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run() == nil
}
