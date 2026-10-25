package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new dotfiles repository with default manifest",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoDir, err := resolveDotfilesDir()
		if err != nil {
			return err
		}

		if err := os.MkdirAll(repoDir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", repoDir, err)
		}

		m, err := loadOrInitManifest(repoDir)
		if err != nil {
			return err
		}

		// Initialize git repo if not already present
		gitDir := filepath.Join(repoDir, ".git")
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			gitInit := exec.Command("git", "init", "-b", "main")
			gitInit.Dir = repoDir
			_ = gitInit.Run()
		}

		fmt.Printf("✓ Initialized dotfiles repository at: %s\n", repoDir)
		fmt.Printf("✓ Manifest generated with %d default apps (nvim, pi, ghostty, zsh, git, pnpm, brew)\n", len(m.Apps))
		fmt.Println("\nNext steps:")
		fmt.Println("  dots status          # Check what is currently tracked vs local machine")
		fmt.Println("  dots backup          # Copy your current configs to the dotfiles repo")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
