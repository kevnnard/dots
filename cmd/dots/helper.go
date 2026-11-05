package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

// loadManifestReadOnly loads the repository manifest without side effects:
// when it is missing, the embedded default is used and nothing is cloned or
// written. It returns the manifest exactly as the user wrote it.
func loadManifestReadOnly(out io.Writer, repoDir string) (*manifest.Manifest, error) {
	manifestPath := getManifestPath(repoDir)
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		fmt.Fprintf(out, "ℹ️  No manifest at %s; planning with the built-in inventory (nothing cloned or written).\n", manifestPath)
		return manifest.DefaultManifest(), nil
	}
	return manifest.Load(manifestPath)
}

// loadRestoreManifest is loadManifestReadOnly plus the shipped install recipes
// that an older local manifest lacks. Restore installs with them; clean never
// does, because it uninstalls, so it uses the manifest as written.
func loadRestoreManifest(out io.Writer, repoDir string) (*manifest.Manifest, error) {
	local, err := loadManifestReadOnly(out, repoDir)
	if err != nil {
		return nil, err
	}
	eff, extended := manifest.Effective(local)
	if len(extended) > 0 {
		fmt.Fprintf(out, "ℹ️  Added the built-in install recipes missing from your manifest for %s.\n", strings.Join(extended, ", "))
	}
	return eff, nil
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
