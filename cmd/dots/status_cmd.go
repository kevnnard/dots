package main

import (
	"fmt"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/status"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check synchronization state and drift between machine and dotfiles repo",
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
		fmt.Printf("System: %s (%s) | Manager: %s | Repo: %s\n\n", sys.OS, sys.Arch, sys.PrimaryManager, repoDir)

		overall := status.CheckAll(m, repoDir)

		driftCount := 0
		for name, st := range overall.Apps {
			installedBadge := "✗ Not installed"
			if st.Installed {
				installedBadge = "✓ Installed"
			}

			fmt.Printf("%-12s [%s]\n", name, installedBadge)
			if len(st.Paths) == 0 {
				fmt.Println("    (no paths defined)")
			}
			for _, p := range st.Paths {
				var badge string
				switch p.State {
				case status.StateSynced:
					badge = "✓ In sync"
				case status.StateModified:
					badge = "Δ Modified (drift)"
					driftCount++
				case status.StateMissingLocal:
					badge = "↓ In repo, missing on local machine"
					driftCount++
				case status.StateMissingRepo:
					badge = "↑ Exists locally, not backed up"
					driftCount++
				case status.StateMissingBoth:
					badge = "○ Missing locally and in repo"
				}
				fmt.Printf("    • %-35s -> %s\n", p.Src, badge)
			}
		}

		if driftCount > 0 {
			fmt.Printf("\n⚠️  Detected %d items with drift. Run 'dots backup' or 'dots restore' to synchronize.\n", driftCount)
		} else {
			fmt.Println("\n✨ Everything is in sync!")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
