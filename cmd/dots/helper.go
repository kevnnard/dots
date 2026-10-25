package main

import (
	"fmt"
	"os"
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
	return manifest.ExpandPath("~/.dotfiles")
}

func getManifestPath(repoDir string) string {
	return filepath.Join(repoDir, "manifest.yaml")
}

func loadOrInitManifest(repoDir string) (*manifest.Manifest, error) {
	manifestPath := getManifestPath(repoDir)
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		m := manifest.DefaultManifest()
		if err := m.Save(manifestPath); err != nil {
			return nil, fmt.Errorf("failed creating default manifest at %s: %w", manifestPath, err)
		}
		return m, nil
	}
	return manifest.Load(manifestPath)
}
