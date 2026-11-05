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
	outFlag     string
	sendFlag    string
	receiveFlag bool
	sha256Flag  string
	intoFlag    string
)

var backupCmd = &cobra.Command{
	Use:   "backup [app-name]",
	Short: "Stage a private backup of local configs outside the repository",
	Long: `Copies configuration files and directories from the local machine into a staging
directory (default ~/.cache/dots/backup, or --out). The repository is never written
and nothing is committed or pushed: backups hold personal configs, and the repository
is public. Sensitive files are sanitized into templates; credentials never travel.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Changed, not a non-empty check: --send '' must fail here, never fall
		// through to the normal backup.
		if cmd.Flags().Changed("send") {
			return runSend(sendFlag, outFlag)
		}
		if receiveFlag {
			return runReceive(sha256Flag, intoFlag)
		}

		repoDir, err := resolveDotfilesDir()
		if err != nil {
			return err
		}
		outDir, err := resolveOutDir(outFlag, repoDir)
		if err != nil {
			return err
		}

		m, err := loadOrInitManifest(repoDir)
		if err != nil {
			return err
		}

		fmt.Printf("📦 Staging backup in: %s\n\n", outDir)

		if len(args) > 0 {
			targetApp := args[0]
			app, ok := m.Apps[targetApp]
			if !ok {
				return fmt.Errorf("app '%s' not found in manifest", targetApp)
			}
			res, err := backup.BackupApp(targetApp, app, outDir)
			if err != nil {
				return err
			}
			printAppBackupResult(res)
			return nil
		}

		total, err := backup.BackupAll(m, outDir)
		if err != nil {
			return err
		}
		if err := backup.StageManifest(repoDir, outDir); err != nil {
			return err
		}

		for _, res := range total.Results {
			printAppBackupResult(res)
		}

		fmt.Printf("\n✨ Backup staged. Files processed: %d\n", total.Total)
		fmt.Printf("   Send it with: dots backup --send <device>\n")

		if len(total.Warnings) > 0 {
			fmt.Println("\n⚠️  Warnings:")
			for _, w := range total.Warnings {
				fmt.Printf("   - %s\n", w)
			}
		}
		return nil
	},
}

// resolveOutDir returns the staging directory and refuses one inside the repository.
func resolveOutDir(out, repoDir string) (string, error) {
	dir := out
	if dir == "" {
		def, err := backup.DefaultOutDir()
		if err != nil {
			return "", err
		}
		dir = def
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if err := backup.CheckOutsideRepo(repoDir, dir); err != nil {
		return "", err
	}
	return dir, nil
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

// runSend archives the staged backup and sends it to a Tailscale device.
// Run `dots backup` first so the staging directory holds the current configs.
func runSend(device, out string) error {
	repoDir, err := resolveDotfilesDir()
	if err != nil {
		return err
	}
	stage, err := resolveOutDir(out, repoDir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(stage, "manifest.yaml")); err != nil {
		return fmt.Errorf("no staged backup in %s: run `dots backup` first", stage)
	}

	tmp, err := os.MkdirTemp("", "dots-send-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	archive := filepath.Join(tmp, transfer.ArchivePrefix+time.Now().Format("20060102-150405")+".tar.gz")
	sum, err := transfer.Archive(stage, archive)
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
	fmt.Printf("   Restore with: dots -d %s restore\n", into)
	return nil
}

func init() {
	backupCmd.Flags().StringVar(&outFlag, "out", "", "Staging directory outside the repository (default ~/.cache/dots/backup)")
	backupCmd.Flags().StringVar(&sendFlag, "send", "", "Send the staged backup to a Tailscale device name")
	backupCmd.Flags().BoolVar(&receiveFlag, "receive", false, "Receive the backup from the Taildrop inbox (needs --sha256)")
	backupCmd.Flags().StringVar(&sha256Flag, "sha256", "", "sha256 printed by the sender; required with --receive")
	backupCmd.Flags().StringVar(&intoFlag, "into", "", "Directory to extract into (default ~/.cache/dots/incoming/<sha prefix>)")
	rootCmd.AddCommand(backupCmd)
}
