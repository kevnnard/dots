package main

import (
	"context"
	"fmt"

	"github.com/kevnnard/dots/pkg/clean"
	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/engramsync"
	"github.com/kevnnard/dots/pkg/manifest"
	"github.com/kevnnard/dots/pkg/packagelink"
	"github.com/kevnnard/dots/pkg/restore"
	"github.com/kevnnard/dots/pkg/transfer"
	"github.com/spf13/cobra"
)

var (
	dryRunFlag          bool
	installPackagesFlag bool
	profileFlag         string
	allProfilesFlag     bool
	withConfigFlag      bool
	replaceExistingFlag bool
	receivedFlag        bool

	// External effects of restore, replaced by tests.
	detectSystem                           = detector.Detect
	installProbe    restore.InstalledProbe = restore.CommandProbe
	installExecutor restore.Executor       = restore.ShellExecutor
	// Read-only package manager queries that resolve package links.
	packageQuery packagelink.Runner = packagelink.ExecRunner{}
)

var restoreCmd = &cobra.Command{
	Use:   "restore [app-name]",
	Short: "Install packages and, on request, restore backed-up configs on this machine",
	Long: `Detects the current operating system and package managers, plans the selected
apps with their dependencies in order, skips apps that are already installed and,
with --install-packages, runs the install commands from the dotfiles repository.
An app is already installed when its check commands or paths exist, or when the
package manager (pacman, brew, npm, pnpm) confirms the package of the selected recipe.

By default apps keep their stock configuration: configs in the repository are
only deployed with --with-config, after every package the config needs is
installed. Existing local files that differ are kept unless --replace-existing
is also given, in which case each one gets a timestamped .bk safety backup
before it is replaced. Machine-specific files (keep_local in the manifest:
~/.gitconfig with its signing key, the Pi and Gentle Shell subagents.json) are
deployed only where missing and never replaced, even with --replace-existing.

Backed-up links stay links. A relative link must stay inside home. A link into
a Homebrew cask (Caskroom) or formula, dependencies included (Cellar), from any
prefix (Apple silicon, Intel, Linuxbrew, custom) is pointed at the same resource
of the package installed here, found with read-only brew queries after the
installs: the package must be installed in one unambiguous version and the
resource must exist inside it. Anything else, or any link that cannot be
confirmed, is not restored and the run reports it; nothing is installed or
guessed for it. Other managers and layouts (such as /opt/homebrew/opt) are not
resolved.

--received restores the last backup taken with ` + "`dots backup --receive <code>`" + `
and implies --with-config: dots restore --received --all --dry-run, then
without --dry-run.

Known legacy .zshrc plugin lines and the obsolete duplicate Pi Engram declaration
are repaired in backup content before deployment. Other content is not repaired.
With --with-config --replace-existing, the exact known local Pi Engram conflict
is backed up and repaired before installers run. Otherwise that conflict blocks
Pi installation; dry runs never repair files. Unknown conflicts are preserved.

The summary ends with a readiness report per app: missing keys (placeholder
fields and unset environment variables, by name only), pending logins and
missing MCP commands found in the apps' declared files in your home. Values
are never printed. The check is offline: no key, token or session is
validated, so anything dots cannot prove is reported as "not-verified" with
the command to check it.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if receivedFlag {
			withConfigFlag = true
		}
		if replaceExistingFlag && !withConfigFlag {
			return fmt.Errorf("--replace-existing only applies together with --with-config")
		}
		repoDir, err := resolveRestoreDir()
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()

		var m *manifest.Manifest
		if dryRunFlag {
			// A dry run never clones the repository or writes a manifest.
			m, err = loadRestoreManifest(out, repoDir)
		} else {
			m, err = loadOrInitManifest(repoDir)
		}
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
		selected := restore.Select(m, targetProfile)
		if len(args) > 0 {
			selected = args
		}

		sys := detectSystem()
		fmt.Fprintf(out, "🎯 Target System: %s (%s, %s) | Profile: %s\n", sys.OS, sys.Arch, sys.PrimaryManager, targetProfile)
		if dryRunFlag {
			fmt.Fprintln(out, "🔍 DRY RUN MODE: No files will be modified or commands executed.")
		}
		if withConfigFlag {
			mode := "differing local files are kept"
			if replaceExistingFlag {
				mode = "differing local files are replaced after a .bk safety backup"
			}
			fmt.Fprintf(out, "📁 Restoring backed-up configs from %s (%s)\n", repoDir, mode)
		}
		fmt.Fprintln(out)

		cmd.SilenceUsage = true
		res, err := restore.Run(m, selected, sys, restore.RunOptions{
			RepoDir:         repoDir,
			WithConfig:      withConfigFlag,
			ReplaceExisting: replaceExistingFlag,
			InstallPackages: installPackagesFlag,
			DryRun:          dryRunFlag,
			Explicit:        len(args) > 0,
			Probe:           installProbe,
			Receipts:        installReceipts(sys),
			Packages:        packagelink.NewResolver(packageQuery),
			Exec: func(req restore.ExecRequest) error {
				fmt.Fprintf(out, "▶ [%s] %s\n", req.AppName, req.Command)
				return installExecutor(req)
			},
		})
		if err != nil {
			return err
		}

		for _, line := range res.Summary() {
			fmt.Fprintln(out, line)
		}
		// Memories travel with a whole backup, so a restore of named apps
		// leaves them alone.
		var engramErr error
		if withConfigFlag && len(args) == 0 {
			line, err := engramsync.Import(context.Background(), engramOptions(), repoDir, dryRunFlag)
			if err != nil {
				engramErr = err
				line = fmt.Sprintf("❌ Engram memories not imported: %v", err)
			}
			fmt.Fprintln(out, line)
		}
		if !installPackagesFlag && !dryRunFlag {
			fmt.Fprintln(out, "\n💡 Tip: run with --install-packages (-i) to execute the planned installs.")
		}
		if !withConfigFlag {
			fmt.Fprintln(out, "💡 Tip: run with --with-config to deploy the configs backed up in the repository.")
		}
		if problems := res.Problems(); len(problems) > 0 {
			return fmt.Errorf("restore finished with %d problem(s)", len(problems))
		}
		return engramErr
	},
}

// resolveRestoreDir is the dotfiles directory, or with --received the last
// received backup.
func resolveRestoreDir() (string, error) {
	if !receivedFlag {
		return resolveDotfilesDir()
	}
	if dotfilesDirFlag != "" {
		return "", fmt.Errorf("--received and --dir cannot be combined: --received already selects the last received backup")
	}
	incoming, err := transfer.IncomingDir()
	if err != nil {
		return "", err
	}
	return transfer.LatestReceived(incoming)
}

func init() {
	restoreCmd.Flags().BoolVarP(&dryRunFlag, "dry-run", "n", false, "Simulate restore without cloning, modifying files or running commands")
	restoreCmd.Flags().BoolVarP(&installPackagesFlag, "install-packages", "i", false, "Execute package manager commands to install apps")
	restoreCmd.Flags().StringVarP(&profileFlag, "profile", "p", "core", "Target profile: core (default), ai, mobile, infra, or all")
	restoreCmd.Flags().BoolVarP(&allProfilesFlag, "all", "a", false, "Restore all profiles (optional apps still need to be named)")
	restoreCmd.Flags().BoolVar(&withConfigFlag, "with-config", false, "Deploy configs backed up in the dotfiles repo (default: keep stock configs)")
	restoreCmd.Flags().BoolVar(&receivedFlag, "received", false, "Restore the last backup received with dots backup --receive (implies --with-config)")
	restoreCmd.Flags().BoolVar(&replaceExistingFlag, "replace-existing", false, "With --with-config, replace differing local files after a .bk safety backup")
	rootCmd.AddCommand(restoreCmd)
}

// installReceipts asks this machine's package manager, read-only and through
// the same executor clean uses, whether the recipe restore selected is
// already installed, so a package found under another name is not reinstalled.
func installReceipts(sys *detector.SystemInfo) restore.ReceiptProbe {
	home, err := cleanHome()
	if err != nil {
		home = ""
	}
	return clean.NativeProbe{Sys: sys, Exec: cleanExecutor, Home: home}.Installed
}
