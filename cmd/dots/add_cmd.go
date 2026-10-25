package main

import (
	"fmt"

	"github.com/kevnnard/dots/pkg/backup"
	"github.com/kevnnard/dots/pkg/status"
	"github.com/spf13/cobra"
)

var autoBackupFlag bool

var addCmd = &cobra.Command{
	Use:   "add <app-name> <src-path> [dest-path]",
	Short: "Track a new configuration path or application",
	Args:  cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		srcPath := args[1]
		destPath := ""
		if len(args) == 3 {
			destPath = args[2]
		}

		repoDir, err := resolveDotfilesDir()
		if err != nil {
			return err
		}

		m, err := loadOrInitManifest(repoDir)
		if err != nil {
			return err
		}

		if err := status.AddAppPath(m, appName, srcPath, destPath); err != nil {
			return err
		}

		manifestPath := getManifestPath(repoDir)
		if err := m.Save(manifestPath); err != nil {
			return fmt.Errorf("failed updating manifest: %w", err)
		}

		fmt.Printf("✓ Added path '%s' to app '%s' in manifest.\n", srcPath, appName)

		if autoBackupFlag {
			app := m.Apps[appName]
			res, err := backup.BackupApp(appName, app, repoDir)
			if err != nil {
				return err
			}
			printAppBackupResult(res)
		} else {
			fmt.Println("Tip: Run 'dots backup' to snapshot this new configuration into the repository.")
		}

		return nil
	},
}

func init() {
	addCmd.Flags().BoolVarP(&autoBackupFlag, "backup", "b", true, "Immediately backup the path after adding (default true)")
	rootCmd.AddCommand(addCmd)
}
