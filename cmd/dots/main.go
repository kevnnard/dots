package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	dotfilesDirFlag string
	version         = "1.0.0"
)

var rootCmd = &cobra.Command{
	Use:     "dots",
	Short:   "dots - Cross-platform dotfiles and workstation setup manager",
	Long:    `dots is a compiled Go CLI to backup, manage, and restore dotfiles and software packages across macOS and Linux machines.`,
	Version: version,
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
