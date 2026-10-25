package main

import (
	"fmt"

	"github.com/kevnnard/dots/pkg/backup"
	"github.com/spf13/cobra"
)

var backupCmd = &cobra.Command{
	Use:   "backup [app-name]",
	Short: "Backup configs and dependencies into the dotfiles repository",
	Long:  `Copies configuration files and directories from the local machine into the dotfiles repository. Automatically sanitizes sensitive files into templates and dumps Brewfile on macOS.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoDir, err := resolveDotfilesDir()
		if err != nil {
			return err
		}

		m, err := loadOrInitManifest(repoDir)
		if err != nil {
			return err
		}

		fmt.Printf("📦 Backing up configurations to: %s\n\n", repoDir)

		if len(args) > 0 {
			targetApp := args[0]
			app, ok := m.Apps[targetApp]
			if !ok {
				return fmt.Errorf("app '%s' not found in manifest", targetApp)
			}
			res, err := backup.BackupApp(targetApp, app, repoDir)
			if err != nil {
				return err
			}
			printAppBackupResult(res)
			return nil
		}

		total, err := backup.BackupAll(m, repoDir)
		if err != nil {
			return err
		}

		for _, res := range total.Results {
			printAppBackupResult(res)
		}

		fmt.Printf("\n✨ Backup complete! Total files processed: %d\n", total.Total)
		if len(total.Warnings) > 0 {
			fmt.Println("\n⚠️  Warnings:")
			for _, w := range total.Warnings {
				fmt.Printf("   - %s\n", w)
			}
		}
		return nil
	},
}

func printAppBackupResult(res *backup.Result) {
	fmt.Printf("[%s]\n", res.AppName)
	for _, c := range res.Copied {
		fmt.Printf("  ✓ Copied: %s\n", c)
	}
	for _, s := range res.SecretsMasked {
		fmt.Printf("  🔒 Secret masked (template created): %s\n", s)
	}
	for _, sk := range res.Skipped {
		fmt.Printf("  - Skipped: %s\n", sk)
	}
	for _, w := range res.Warnings {
		fmt.Printf("  ⚠️  %s\n", w)
	}
	if len(res.Copied) == 0 && len(res.SecretsMasked) == 0 && len(res.Skipped) == 0 {
		fmt.Println("  (no files defined)")
	}
}

func init() {
	rootCmd.AddCommand(backupCmd)
}
