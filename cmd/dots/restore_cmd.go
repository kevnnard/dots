package main

import (
	"fmt"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/restore"
	"github.com/spf13/cobra"
)

var (
	dryRunFlag          bool
	installPackagesFlag bool
	profileFlag         string
	allProfilesFlag     bool
)

var restoreCmd = &cobra.Command{
	Use:   "restore [app-name]",
	Short: "Restore configs and provision packages on this machine",
	Long:  `Detects the current operating system, packages manager, installs missing dependencies, and restores configuration files from the dotfiles repository to their target locations.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoDir, err := resolveDotfilesDir()
		if err != nil {
			return err
		}

		m, err := loadOrInitManifest(repoDir)
		if err != nil {
			return err
		}

		targetProfile := profileFlag
		if allProfilesFlag {
			targetProfile = "all"
		}
		if targetProfile == "" {
			targetProfile = "core"
		}

		sys := detector.Detect()
		fmt.Printf("🎯 Target System: %s (%s, %s) | Profile: %s\n", sys.OS, sys.Arch, sys.PrimaryManager, targetProfile)
		if dryRunFlag {
			fmt.Println("🔍 DRY RUN MODE: No files will be modified or commands executed.")
			fmt.Println()
		} else {
			fmt.Println()
		}

		// 1. Package Installation Planning & Execution
		plans := restore.PlanAll(m, sys)
		fmt.Println("📦 Software Provisioning Plan:")
		for _, p := range plans {
			if len(args) > 0 && args[0] != p.AppName {
				continue
			}
			app := m.Apps[p.AppName]
			if len(args) == 0 && !app.MatchesProfile(targetProfile) {
				continue
			}
			if p.InstallCmd != "" {
				fmt.Printf("  • %-10s : %s\n", p.AppName, p.InstallCmd)
				if installPackagesFlag && !dryRunFlag {
					fmt.Printf("    ▶ Executing: %s\n", p.InstallCmd)
					if err := restore.ExecuteInstallCommand(p.InstallCmd); err != nil {
						fmt.Printf("    ⚠️  Failed: %v\n", err)
					}
				}
			} else {
				fmt.Printf("  • %-10s : (no package recipe needed or defined)\n", p.AppName)
			}
		}

		if !installPackagesFlag && !dryRunFlag {
			fmt.Println("\n💡 Tip: Run with --install-packages (-i) to automatically execute package installations.")
		}

		// 2. Deploy Configuration Files
		fmt.Println("\n📁 Deploying Configurations:")
		for name, app := range m.Apps {
			if len(args) > 0 && args[0] != name {
				continue
			}
			if len(args) == 0 && !app.MatchesProfile(targetProfile) {
				continue
			}

			res, err := restore.ApplyAppFiles(name, app, repoDir, dryRunFlag)
			if err != nil {
				fmt.Printf("  ⚠️  Error restoring %s: %v\n", name, err)
				continue
			}

			fmt.Printf("[%s]\n", name)
			for _, r := range res.Restored {
				fmt.Printf("  ✓ Restored: %s\n", r)
			}
			for _, t := range res.TemplatesInit {
				fmt.Printf("  🔒 Secret template deployed: %s (please fill with real secrets!)\n", t)
			}
			for _, s := range res.Skipped {
				fmt.Printf("  - Skipped: %s\n", s)
			}
			for _, w := range res.Warnings {
				fmt.Printf("  ⚠️  %s\n", w)
			}
		}

		fmt.Println("\n✨ Restore operation complete!")
		return nil
	},
}

func init() {
	restoreCmd.Flags().BoolVarP(&dryRunFlag, "dry-run", "n", false, "Simulate restore without modifying files or running commands")
	restoreCmd.Flags().BoolVarP(&installPackagesFlag, "install-packages", "i", false, "Execute package manager commands to install apps")
	restoreCmd.Flags().StringVarP(&profileFlag, "profile", "p", "core", "Target profile: core (default), mobile, infra, or all")
	restoreCmd.Flags().BoolVarP(&allProfilesFlag, "all", "a", false, "Restore all profiles")
	rootCmd.AddCommand(restoreCmd)
}
