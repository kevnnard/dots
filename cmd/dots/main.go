package main

import (
	"fmt"
	"os"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/ui"
	"github.com/spf13/cobra"
)

var (
	dotfilesDirFlag string
	version         = "1.0.0"
)

var rootCmd = &cobra.Command{
	Use:     "dots",
	Aliases: []string{"kevnnard-dots"},
	Short:   "kevnnard-dots - Interactive terminal workstation and dotfiles manager",
	Long:    `kevnnard-dots is an interactive Go TUI and CLI to backup, manage, and restore dotfiles and software packages across macOS and Linux machines.`,
	Version: version,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoDir, err := resolveDotfilesDir()
		if err != nil {
			return err
		}

		m, err := loadOrInitManifest(repoDir)
		if err != nil {
			return err
		}

		sys := detector.Detect()
		return ui.RunTUI(m, sys, repoDir)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&dotfilesDirFlag, "dir", "d", "", "Dotfiles storage directory (default: ~/.dotfiles)")
}

func main() {
	Execute()
}
