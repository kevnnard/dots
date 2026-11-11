package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kevnnard/dots/pkg/backup"
	"github.com/kevnnard/dots/pkg/discover"
	"github.com/kevnnard/dots/pkg/engramsync"
	"github.com/kevnnard/dots/pkg/manifest"
	"github.com/kevnnard/dots/pkg/transfer"
	"github.com/spf13/cobra"
)

var (
	outFlag     string
	sendFlag    string
	receiveFlag bool
	sha256Flag  string
	intoFlag    string

	// Runs `tailscale file`, replaced by tests.
	transferRunner transfer.Runner = transfer.ExecRunner{}

	// Where full backup discovery looks; replaced by tests.
	discoverOptions = discover.ForThisMachine

	// How backup and restore run engram; replaced by tests.
	engramOptions = engramsync.ForThisMachine
)

var backupCmd = &cobra.Command{
	Use:   "backup [app-name | --receive <code>]",
	Short: "Stage a private backup of local configs outside the repository",
	Long: `Copies configuration files and directories from the local machine into a staging
directory (default ~/.cache/dots/backup, or --out). The repository is never written
and nothing is committed or pushed: backups hold personal configs, and the repository
is public. Files the manifest names as secrets, and recognized key files such as
.env, become masked templates; login files are not mapped. Detection goes by file
name and key name, so it cannot prove that other scripts or settings are free of
credentials: review the staged folder before sending it. If the staging folder
already holds an unmasked copy where a template belongs (left by a dots that
copied that file as is, such as web-search.json), backup stops without removing
anything: stage into a new empty folder with --out.

The built-in inventory maps personal configs with home-relative paths: the shell
(the Oh My Zsh custom folder without the plugin and theme folders), editors,
terminals and the AI agent homes (settings, instructions, prompts, extensions and
named preference files). It does not map sessions, caches or telemetry. A manifest
written by an older dots is never rewritten: backup lists the built-in paths it lacks.

Apps and paths whose os list excludes this system are not backed up.

A full backup (no app named) also discovers what the manifest does not
declare: Homebrew formulae installed on request, casks and taps (on macOS, compared
with the manifest recipes and the repository Brewfiles) and ~/.config entries no
manifest path covers. Each is added to the staged manifest only, never the
repository one, with the systems it works on: casks and formulae with only macOS
bottles or a macOS requirement get os [darwin]; formulae with Linux bottles or
built from source, taps and configs get [darwin, linux]. The run prints them as a "New:" list. Folders that may hold
credentials or caches (gcloud, configstore, github-copilot, gh-copilot, gh, any
name containing "cache") are listed under "Skipped:" and not copied. A discovery
error is reported and the manifest is staged unchanged.

With engram installed, a full backup exports every Engram memory
(engram export --all) to configs/engram/memories.json in the stage, so --send
carries it. Without engram this is reported, not a failure.

Links into Homebrew casks and formulae are staged as links; --send carries them
as package references, and restore points them at the package installed on the
receiving machine. Other absolute links do not travel.

Move the backup to another machine over Taildrop:
  dots backup --send <device>   prints a short code
  dots backup --receive <code>  on the other machine, then
  dots restore --received --all --dry-run

Or do every step from this machine with dots sync <device>.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Changed, not a non-empty check: --send '' must fail here, never fall
		// through to the normal backup.
		if cmd.Flags().Changed("send") {
			return runSend(sendFlag, outFlag)
		}
		if receiveFlag {
			code := sha256Flag
			if code == "" && len(args) > 0 {
				code = args[0]
			}
			return runReceive(code, intoFlag)
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
			printMissingDefaultPaths(repoDir, m, targetApp)
			res, err := backup.BackupApp(targetApp, app, outDir)
			if err != nil {
				return err
			}
			printAppBackupResult(res)
			return nil
		}

		printMissingDefaultPaths(repoDir, m, "")
		return runFullBackup(m, repoDir, outDir, true)
	},
}

func runFullBackup(m *manifest.Manifest, repoDir, outDir string, sendHint bool) error {
	total, err := backup.BackupAll(m, outDir)
	if err != nil {
		return err
	}
	found, discovered, err := stageDiscovered(m, repoDir, outDir)
	if err != nil {
		return err
	}

	names := make([]string, 0, len(total.Results))
	for name := range total.Results {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		printAppBackupResult(total.Results[name])
	}
	for _, res := range discovered {
		printAppBackupResult(res)
		total.Total += len(res.Copied) + len(res.SecretsMasked)
	}
	if lines := found.Lines(); len(lines) > 0 {
		fmt.Println()
		for _, l := range lines {
			fmt.Println(l)
		}
	}

	fmt.Println()
	if line, err := engramsync.Export(context.Background(), engramOptions(), outDir); err != nil {
		total.Warnings = append(total.Warnings, fmt.Sprintf("Engram memories not exported: %v", err))
	} else {
		fmt.Println(line)
	}

	fmt.Printf("\n✨ Backup staged. Files processed: %d\n", total.Total)
	if sendHint {
		fmt.Printf("   Send it with: dots backup --send <device>\n")
	}

	if len(total.Warnings) > 0 {
		fmt.Println("\n⚠️  Warnings:")
		for _, w := range total.Warnings {
			fmt.Printf("   - %s\n", w)
		}
	}
	return nil
}

// stageDiscovered backs up the configs discovery finds and stages the
// manifest with the discovered apps. A discovery failure is not a backup
// failure: it is reported and the manifest is staged unchanged.
func stageDiscovered(m *manifest.Manifest, repoDir, outDir string) (*discover.Result, []*backup.Result, error) {
	found, err := discover.Run(context.Background(), m, discoverOptions(repoDir))
	if err != nil {
		fmt.Printf("⚠️  Discovery skipped: %v\n", err)
		found = &discover.Result{}
	}
	results, err := backup.BackupDiscovered(found.Configs, outDir)
	if err != nil {
		return nil, nil, err
	}
	if err := backup.StageManifestApps(repoDir, outDir, found.Apps); err != nil {
		return nil, nil, err
	}
	return found, results, nil
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

// printMissingDefaultPaths names the built-in config paths the manifest does
// not map, for one app or, when only is empty, every declared app.
func printMissingDefaultPaths(repoDir string, m *manifest.Manifest, only string) {
	var lines []string
	for _, entry := range backup.MissingDefaultPaths(m) {
		if only == "" || strings.HasPrefix(entry, only+": ") {
			lines = append(lines, entry)
		}
	}
	if len(lines) == 0 {
		return
	}
	fmt.Printf("ℹ️  %s does not map these built-in config paths, so they are not backed up\n", getManifestPath(repoDir))
	fmt.Println("   (the manifest is never rewritten; add the paths you want to it):")
	for _, l := range lines {
		fmt.Printf("   - %s\n", l)
	}
	fmt.Println()
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

	code, err := sendStage(stage, device)
	if err != nil {
		return err
	}
	fmt.Println("   On the receiving machine:")
	fmt.Printf("     dots backup --receive %s\n", code)
	fmt.Println("     dots restore --received --all --dry-run")
	return nil
}

func sendStage(stage, device string) (string, error) {
	tmp, err := os.MkdirTemp("", "dots-send-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	archive := filepath.Join(tmp, transfer.ArchivePrefix+time.Now().Format("20060102-150405")+".tar.gz")
	sum, err := transfer.Archive(stage, archive)
	if err != nil {
		return "", err
	}
	if err := transfer.Send(transferRunner, archive, device); err != nil {
		return "", err
	}
	code := transfer.ShortCode(sum)
	fmt.Printf("✈️  Sent %s to %s\n", filepath.Base(archive), device)
	fmt.Printf("   Code: %s (sha256 %s)\n", code, sum)
	return code, nil
}

// runReceive takes the backup from the Taildrop inbox. It needs the code (or
// the full sha256) the sender printed; nothing is extracted unless it matches.
// The extracted backup is recorded as the latest for `dots restore --received`.
func runReceive(code, into string) error {
	if code == "" {
		return fmt.Errorf("the code is required: run `dots backup --receive <code>` with the code the sender printed")
	}
	incoming, err := transfer.IncomingDir()
	if err != nil {
		return err
	}
	if into == "" {
		name := strings.ToLower(code)
		if len(name) > 12 {
			name = name[:12]
		}
		into = filepath.Join(incoming, name)
	}
	unclaimed, err := transfer.Receive(transferRunner, into, code)
	for _, u := range unclaimed {
		fmt.Printf("📦 Kept another backup from the inbox: %s\n", u)
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(incoming, 0o755); err != nil {
		return err
	}
	if err := transfer.RecordLatest(incoming, into); err != nil {
		return err
	}
	fmt.Printf("📥 Received and verified (code %s)\n", code)
	fmt.Printf("   Extracted into %s\n", into)
	fmt.Println("   Review the plan, then apply it:")
	fmt.Println("     dots restore --received --all --dry-run")
	fmt.Println("     dots restore --received --all")
	return nil
}

func init() {
	backupCmd.Flags().StringVar(&outFlag, "out", "", "Staging directory outside the repository (default ~/.cache/dots/backup)")
	backupCmd.Flags().StringVar(&sendFlag, "send", "", "Send the staged backup to a Tailscale device name")
	backupCmd.Flags().BoolVar(&receiveFlag, "receive", false, "Receive the backup from the Taildrop inbox: dots backup --receive <code>")
	backupCmd.Flags().StringVar(&sha256Flag, "sha256", "", "Full sha256 printed by the sender, instead of the short code")
	backupCmd.Flags().StringVar(&intoFlag, "into", "", "Directory to extract into (default ~/.cache/dots/incoming/<code>)")
	rootCmd.AddCommand(backupCmd)
}
