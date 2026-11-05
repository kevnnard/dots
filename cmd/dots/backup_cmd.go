package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kevnnard/dots/pkg/backup"
	"github.com/kevnnard/dots/pkg/transfer"
	"github.com/spf13/cobra"
)

var (
	pushFlag    bool
	sendFlag    string
	receiveFlag bool
	sha256Flag  string
	intoFlag    string
)

var backupCmd = &cobra.Command{
	Use:   "backup [app-name]",
	Short: "Backup configs and dependencies into the dotfiles repository",
	Long:  `Copies configuration files and directories from the local machine into the dotfiles repository. Automatically sanitizes sensitive files into templates and dumps Brewfile on macOS.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Changed, not a non-empty check: --send '' must fail here, never fall
		// through to the normal backup, which commits and pushes to the repo.
		if cmd.Flags().Changed("send") {
			return runSend(sendFlag)
		}
		if receiveFlag {
			return runReceive(sha256Flag, intoFlag)
		}

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

		// Git Snapshot Commit & Push
		snap, snapErr := backup.CreateGitSnapshot(repoDir, pushFlag)
		if snapErr != nil {
			fmt.Printf("⚠️  Git snapshot error: %v\n", snapErr)
		} else if snap.Committed {
			fmt.Printf("📸 Git snapshot committed: %s [%s]\n", snap.Message, snap.CommitHash)
			if snap.Pushed {
				fmt.Println("🚀 Snapshot pushed to remote repository!")
			} else if snap.Error != "" {
				fmt.Printf("⚠️  Remote push note: %s\n", snap.Error)
			}
		}

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

// runSend archives the repository's configs and sends them to a Tailscale device.
// Run `dots backup --push=false` first so the archive holds the current configs.
func runSend(device string) error {
	repoDir, err := resolveDotfilesDir()
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "dots-send-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	archive := filepath.Join(tmp, transfer.ArchivePrefix+time.Now().Format("20060102-150405")+".tar.gz")
	sum, err := transfer.Archive(repoDir, archive)
	if err != nil {
		return err
	}
	if err := transfer.Send(transfer.ExecRunner{}, archive, device); err != nil {
		return err
	}
	fmt.Printf("✈️  Sent %s to %s\n", filepath.Base(archive), device)
	fmt.Printf("   sha256: %s\n", sum)
	fmt.Printf("   On the receiving machine: dots backup --receive --sha256 %s\n", sum)
	return nil
}

// runReceive takes the backup from the Taildrop inbox. It needs the sha256 the
// sender printed; nothing is extracted unless it matches.
func runReceive(sha, into string) error {
	if len(sha) != 64 {
		return fmt.Errorf("--sha256 is required: pass the 64-character sha256 the sender printed")
	}
	if into == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		into = filepath.Join(home, ".cache", "dots", "incoming", sha[:12])
	}
	if err := transfer.Receive(transfer.ExecRunner{}, into, sha); err != nil {
		return err
	}
	fmt.Printf("📥 Received and verified (sha256 %s)\n", sha)
	fmt.Printf("   Extracted into %s. Review it before restoring anything.\n", into)
	return nil
}

func init() {
	backupCmd.Flags().BoolVarP(&pushFlag, "push", "p", true, "Push snapshot commit to remote git repository (default true)")
	backupCmd.Flags().StringVar(&sendFlag, "send", "", "Send the configs archive to a Tailscale device name (use --push=false first)")
	backupCmd.Flags().BoolVar(&receiveFlag, "receive", false, "Receive the backup from the Taildrop inbox (needs --sha256)")
	backupCmd.Flags().StringVar(&sha256Flag, "sha256", "", "sha256 printed by the sender; required with --receive")
	backupCmd.Flags().StringVar(&intoFlag, "into", "", "Directory to extract into (default ~/.cache/dots/incoming/<sha prefix>)")
	rootCmd.AddCommand(backupCmd)
}
