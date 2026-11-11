package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kevnnard/dots/pkg/backup"
	"github.com/spf13/cobra"
)

type commandStreamer interface {
	Stream(out io.Writer, name string, args ...string) error
}

type execStreamer struct{}

func (execStreamer) Stream(out io.Writer, name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout = out
	c.Stderr = out
	return c.Run()
}

var (
	syncYesFlag       bool
	syncDryRunFlag    bool
	syncNoInstallFlag bool

	syncRunner commandStreamer = execStreamer{}

	errSSHConnect = errors.New("cannot connect")
)

var syncCmd = &cobra.Command{
	Use:   "sync <device>",
	Short: "Back up this machine, send it to another one and restore it there over ssh",
	Long: `Keeps another machine in step with this one, in one command:

  1. Stages a full backup into a new private folder under ~/.cache/dots
     (as dots backup --out <new folder>). Once the send succeeds that folder
     is removed when sync ends, whatever happens next (applied, not applied,
     --dry-run or a remote failure). If the backup or the send fails, it is
     kept and its path printed. Nothing else under ~/.cache/dots is removed.
  2. Sends it to <device> over Taildrop (as dots backup --send <device>).
  3. Over ssh <device>, in the remote user's login shell, runs
       dots backup --receive <code>
       dots restore --received --all --replace-existing --install-packages --dry-run
     and prints that plan: packages to install, apps skipped as not for that
     system, files that change, problems and the readiness count.
  4. Asks "Apply on <device>? [y/N]". Only y or yes runs the same restore
     without --dry-run and prints its summary; anything else changes nothing.

Every replaced file gets a .bk copy first, as with restore --replace-existing,
and the receive keeps only the 3 newest received backups, as with
dots backup --receive.
A dry run that reports problems still asks (default no); with --yes it stops
instead. --dry-run stops after the remote dry run, --no-install restores the
configs without --install-packages.

Works in either direction between macOS and Linux. Both machines need dots and
Tailscale on the same tailnet, the receiver must be able to read its Taildrop
inbox (tailscale set --operator=$USER), and ssh <device> must connect without
a prompt. Any other failed step stops with its error and a nonzero exit.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return syncDevice(cmd.InOrStdin(), cmd.OutOrStdout(), args[0])
	},
}

func syncDevice(in io.Reader, out io.Writer, device string) error {
	if device == "" || strings.HasPrefix(device, "-") || strings.ContainsAny(device, " \t\n'\"\\$`;&|<>") {
		return fmt.Errorf("invalid device name %q: use the Tailscale device name", device)
	}
	repoDir, err := resolveDotfilesDir()
	if err != nil {
		return err
	}
	m, err := loadOrInitManifest(repoDir)
	if err != nil {
		return err
	}
	def, err := backup.DefaultOutDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(def), 0o700); err != nil {
		return err
	}
	fresh, err := os.MkdirTemp(filepath.Dir(def), "sync-")
	if err != nil {
		return err
	}
	kept := func(err error) error {
		fmt.Fprintf(out, "\n📁 Stage kept: %s\n", fresh)
		return err
	}
	stage, err := resolveOutDir(fresh, repoDir)
	if err != nil {
		return kept(err)
	}

	fmt.Fprintf(out, "📦 Staging backup in: %s\n\n", stage)
	printMissingDefaultPaths(repoDir, m, "")
	if err := runFullBackup(m, repoDir, stage, false); err != nil {
		return kept(err)
	}
	fmt.Fprintln(out)
	code, err := sendStage(stage, device)
	if err != nil {
		return kept(err)
	}
	defer removeStage(out, fresh)

	restore := "dots restore --received --all --replace-existing"
	if !syncNoInstallFlag {
		restore += " --install-packages"
	}
	fmt.Fprintf(out, "\n📥 Receiving on %s:\n", device)
	if err := remote(out, device, "receive", "dots backup --receive "+code); err != nil {
		return err
	}
	fmt.Fprintf(out, "\n🔎 Dry run on %s:\n", device)
	problems := false
	if err := remote(out, device, "dry run", restore+" --dry-run"); err != nil {
		if errors.Is(err, errSSHConnect) {
			return err
		}
		problems = true
		fmt.Fprintf(out, "\n⚠️  The dry run reported problems: %v\n", err)
	}
	if syncDryRunFlag {
		fmt.Fprintf(out, "\nDry run only: nothing changed on %s.\n", device)
		return nil
	}
	if syncYesFlag && problems {
		return fmt.Errorf("not applied on %s: the dry run reported problems; review them and run dots sync %s without --yes", device, device)
	}
	if !syncYesFlag && !confirmApply(in, out, device) {
		fmt.Fprintf(out, "Not applied: nothing changed on %s.\n", device)
		return nil
	}
	fmt.Fprintf(out, "\n🚀 Applying on %s:\n", device)
	if err := remote(out, device, "restore", restore); err != nil {
		return err
	}
	fmt.Fprintf(out, "\n✨ %s is in sync.\n", device)
	return nil
}

func removeStage(out io.Writer, stage string) {
	info, err := os.Lstat(stage)
	if err != nil || !info.IsDir() || !strings.HasPrefix(filepath.Base(stage), "sync-") {
		return
	}
	if err := os.RemoveAll(stage); err != nil {
		fmt.Fprintf(out, "⚠️  Stage not removed: %s: %v\n", stage, err)
	}
}

func remote(out io.Writer, device, step, script string) error {
	err := syncRunner.Stream(out, "ssh", device, `"$SHELL" -lic '`+script+`'`)
	if err == nil {
		return nil
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) && coded.ExitCode() == 255 {
		return fmt.Errorf("%w with `ssh %s` (%v): check that `ssh %s` works without a prompt", errSSHConnect, device, err, device)
	}
	return fmt.Errorf("%s on %s failed over `ssh %s`: %w", step, device, device, err)
}

func confirmApply(in io.Reader, out io.Writer, device string) bool {
	fmt.Fprintf(out, "\nApply on %s? [y/N] ", device)
	line, _ := bufio.NewReader(in).ReadString('\n')
	fmt.Fprintln(out)
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

func init() {
	syncCmd.Flags().BoolVar(&syncYesFlag, "yes", false, "Apply on the device without asking")
	syncCmd.Flags().BoolVar(&syncDryRunFlag, "dry-run", false, "Stop after the remote dry-run")
	syncCmd.Flags().BoolVar(&syncNoInstallFlag, "no-install", false, "Restore configs only, without --install-packages")
	rootCmd.AddCommand(syncCmd)
}
