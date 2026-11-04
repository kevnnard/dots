package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kevnnard/dots/pkg/clean"
	"github.com/spf13/cobra"
)

var (
	cleanAllFlag      bool
	cleanPackagesFlag bool
	cleanConfigsFlag  bool
	cleanDryRunFlag   bool

	// External effects of clean, replaced by tests.
	cleanExecutor    clean.Executor = clean.OSExecutor{}
	cleanHome                       = os.UserHomeDir
	cleanPresence                   = clean.PathPresence
	cleanInteractive                = isTerminal
)

const dryRunLine = "Dry run: nothing was changed.\n"

var cleanCmd = &cobra.Command{
	Use:   "clean [app-name...] | --all",
	Short: "Uninstall declared packages and quarantine their configs, after one confirmation",
	Long: `Plans the cleanup of the named apps, or of every app in the manifest with --all,
and applies it only after you type "yes" once for the whole plan.

Packages and configurations are chosen separately: --packages uninstalls the
apps' packages, --configs moves their config files into a recoverable
quarantine. Without either flag nothing is planned or changed.

--all means every app declared in the manifest, optional ones included; it never
means every package installed on this machine. Dependencies are never added.

Packages are removed only when the package database proves a single declared
owner, with the manager's plain uninstall (no force, zap, purge or recursion),
dependents first. Omarchy yay recipes are checked and removed through the pacman
database (sudo pacman -R <pkg>). Script installs and managers without a tested
uninstall (apt, dnf, zypper, winget, scoop, choco, cargo) are reported as
unsupported or blocked and never touched. Ownership is checked again right
before each removal; command output is not printed.

Only regular config files the manifest maps are moved into
~/.dots-quarantine/clean-*; nothing is deleted. Directories are not supported
yet, and files named in secrets, matched by exclude or with names that may hold
credentials or data stay in place. That name check is a heuristic: it does not
prove a moved file holds no secret. Undo with "dots clean recover <dir>".

Checks and changes are separate steps, so a process changing a file or package
between the last check and the change can still win the race.

Confirmation needs an interactive terminal; there is no flag to skip it.`,
	Example: `  dots clean zsh --configs --dry-run
  dots clean herdr fzf --packages
  dots clean --all --packages --configs`,
	RunE: func(cmd *cobra.Command, args []string) error {
		switch {
		case cleanAllFlag && len(args) > 0:
			return fmt.Errorf("name apps or use --all, not both")
		case !cleanAllFlag && len(args) == 0:
			return fmt.Errorf("name the apps to clean or use --all")
		case !cleanPackagesFlag && !cleanConfigsFlag:
			return fmt.Errorf("nothing selected: add --packages, --configs or both (nothing was changed)")
		}
		cmd.SilenceUsage = true
		out := cmd.OutOrStdout()
		repoDir, err := resolveDotfilesDir()
		if err != nil {
			return err
		}
		m, err := loadManifestReadOnly(out, repoDir)
		if err != nil {
			return err
		}
		home, err := cleanHome()
		if err != nil {
			return err
		}
		names := args
		if cleanAllFlag {
			names = clean.Candidates(m)
		}
		var req clean.Request
		if cleanPackagesFlag {
			req.Packages = names
		}
		if cleanConfigsFlag {
			req.Configs = names
		}
		sys := detectSystem()
		probe := clean.NativeProbe{Sys: sys, Exec: cleanExecutor, Home: home, Present: cleanPresence(home)}
		plan, err := clean.BuildPlan(m, req, sys, probe.Probe)
		if err != nil {
			return err
		}
		scope := strings.Join(names, ", ")
		if cleanAllFlag {
			scope = fmt.Sprintf("all %d declared apps (optional included, nothing else on this machine)", len(names))
		}
		fmt.Fprintf(out, "🧹 Cleanup on %s (%s) for %s\n", sys.OS, sys.Distro, scope)

		pkgs := clean.PackageRunner{Manifest: m, Sys: sys, Exec: cleanExecutor, Recheck: probe.Probe}
		configs := clean.ConfigRunner{Manifest: m, Home: home}
		if fileExists(repoDir) {
			configs.Repo = repoDir // nothing inside the repository ever moves
		}
		if cleanDryRunFlag {
			return dryRunClean(out, plan, pkgs, configs)
		}
		return applyClean(cmd, plan, pkgs, configs)
	},
}

func dryRunClean(out io.Writer, plan *clean.Plan, pkgs clean.PackageRunner, configs clean.ConfigRunner) error {
	pkgs.DryRun, pkgs.Out, configs.DryRun, configs.Out = true, out, true, out
	if !cleanPackagesFlag {
		fmt.Fprintln(out, "Packages: not selected (add --packages).")
	} else if _, err := pkgs.Run(context.Background(), plan); err != nil {
		return err
	}
	if !cleanConfigsFlag {
		fmt.Fprintln(out, "Configurations: not selected (add --configs).")
	} else if _, err := configs.Run(context.Background(), plan); err != nil {
		return err
	}
	return nil
}

// applyClean previews both scopes, asks once and only then runs them. Each
// runner asks again for its own summary; that answer is the cached one, and
// only for exactly the summary the user confirmed. Configurations run first
// because their quarantine can be undone; packages never run after a config
// failure or a config preview that changed.
func applyClean(cmd *cobra.Command, plan *clean.Plan, pkgs clean.PackageRunner, configs clean.ConfigRunner) error {
	out := cmd.OutOrStdout()
	ctx := context.Background()
	var pkgSummary, cfgSummary string
	removals, moves, skipped := 0, 0, 0
	if cleanPackagesFlag {
		var b bytes.Buffer
		pkgs.DryRun, pkgs.Out = true, &b
		if _, err := pkgs.Run(ctx, plan); err != nil {
			return err
		}
		pkgSummary = strings.TrimSuffix(b.String(), dryRunLine)
		for _, it := range plan.Packages {
			switch it.Status {
			case clean.PackageRemove:
				removals++
			case clean.PackageBlocked, clean.PackageUnsupported:
				skipped++
			}
		}
		fmt.Fprint(out, pkgSummary)
	} else {
		fmt.Fprintln(out, "Packages: not selected (add --packages).")
	}
	if cleanConfigsFlag {
		var b bytes.Buffer
		configs.DryRun, configs.Out = true, &b
		rep, err := configs.Run(ctx, plan)
		if err != nil {
			return err
		}
		cfgSummary = strings.TrimSuffix(b.String(), dryRunLine)
		for _, it := range plan.Configs {
			if it.Status == clean.ConfigBlocked {
				skipped++
			}
		}
		for _, c := range rep.Candidates {
			switch c.Status {
			case clean.CandidateQuarantine:
				moves++
			case clean.CandidateBlocked:
				skipped++
			}
		}
		fmt.Fprint(out, cfgSummary)
	} else {
		fmt.Fprintln(out, "Configurations: not selected (add --configs).")
	}
	if removals+moves == 0 {
		fmt.Fprintf(out, "Nothing to change (%d item(s) blocked or unsupported).\n", skipped)
		return nil
	}
	if !cleanInteractive(cmd.InOrStdin()) {
		return fmt.Errorf("confirmation needs an interactive terminal; nothing was changed")
	}
	fmt.Fprintf(out, "Remove %d package(s) and quarantine %d file(s)? Type yes to continue: ", removals, moves)
	if !confirmed(cmd.InOrStdin()) {
		fmt.Fprintln(out, "Cancelled: nothing was changed.")
		return nil
	}

	var quarantine string
	if moves > 0 {
		configs.DryRun, configs.Out = false, &previewed{w: out, skip: cfgSummary}
		configs.Confirm = func(s string) bool { return s == cfgSummary }
		rep, err := configs.Run(ctx, plan)
		switch {
		case err != nil:
			return fmt.Errorf("%w; no package was removed", err)
		case rep.Cancelled || len(rep.Moved) != moves:
			// A drift to nothing movable is not reported as Cancelled.
			return fmt.Errorf("configurations changed after confirmation; nothing was changed and no package was removed")
		}
		quarantine = rep.Quarantine
	}
	if removals > 0 {
		pkgs.DryRun, pkgs.Out = false, &previewed{w: out, skip: pkgSummary}
		pkgs.Confirm = func(s string) bool { return s == pkgSummary }
		rep, err := pkgs.Run(ctx, plan)
		if err == nil && rep.Cancelled {
			err = fmt.Errorf("package plan changed after confirmation; no package was removed")
		}
		if err != nil {
			if quarantine != "" {
				fmt.Fprintf(out, "Configurations stay quarantined; undo with: dots clean recover %s\n", quarantine)
			}
			return err
		}
	}
	if quarantine != "" {
		fmt.Fprintf(out, "Undo configurations with: dots clean recover %s\n", quarantine)
	}
	fmt.Fprintf(out, "Applied: %d package(s) removed, %d file(s) quarantined; %d item(s) left untouched (blocked or unsupported).\n", removals, moves, skipped)
	return nil
}

var cleanRecoverCmd = &cobra.Command{
	Use:   "recover <quarantine-dir>",
	Short: "Move the files of one cleanup quarantine back, never replacing existing files",
	Long: `Restores one ~/.dots-quarantine/clean-* directory reported by "dots clean".
Every file goes back to its recorded path only if that path is still free and
safe; existing files are conflicts and stay in the quarantine. Nothing changes
until you type "yes"; --dry-run only shows the preview.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		out := cmd.OutOrStdout()
		home, err := cleanHome()
		if err != nil {
			return err
		}
		repo := ""
		if dir, err := resolveDotfilesDir(); err == nil && fileExists(dir) {
			repo = dir
		}
		noTerminal := false
		rep, err := clean.Recover(context.Background(), clean.Recovery{
			Home: home, Repo: repo, Dir: args[0], DryRun: cleanDryRunFlag, Out: out,
			Confirm: func(string) bool {
				if !cleanInteractive(cmd.InOrStdin()) {
					noTerminal = true
					return false
				}
				fmt.Fprint(out, "Restore these files? Type yes to continue: ")
				return confirmed(cmd.InOrStdin())
			},
		})
		switch {
		case err != nil:
			return err
		case noTerminal:
			return fmt.Errorf("confirmation needs an interactive terminal; nothing was changed")
		case len(rep.Moved) < len(rep.Candidates) && !rep.DryRun && !rep.Cancelled:
			fmt.Fprintf(out, "%d file(s) stay in %s.\n", len(rep.Candidates)-len(rep.Moved), args[0])
		}
		return nil
	},
}

// previewed drops a runner's reprint of the summary already shown before the
// combined confirmation; any other output passes through.
type previewed struct {
	w    io.Writer
	skip string
}

func (p *previewed) Write(b []byte) (int, error) {
	if p.skip != "" && string(b) == p.skip {
		p.skip = ""
		return len(b), nil
	}
	p.skip = ""
	return p.w.Write(b)
}

// confirmed reads one answer; only "yes" agrees, and a missing answer denies.
func confirmed(in io.Reader) bool {
	line, _ := bufio.NewReader(in).ReadString('\n')
	return strings.TrimSpace(line) == "yes"
}

func isTerminal(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func init() {
	cleanCmd.Flags().BoolVarP(&cleanAllFlag, "all", "a", false, "Clean every app declared in the manifest, optional ones included")
	cleanCmd.Flags().BoolVar(&cleanPackagesFlag, "packages", false, "Uninstall the apps' packages")
	cleanCmd.Flags().BoolVar(&cleanConfigsFlag, "configs", false, "Move the apps' config files into a recoverable quarantine")
	cleanCmd.PersistentFlags().BoolVarP(&cleanDryRunFlag, "dry-run", "n", false, "Show the plan without changing anything")
	cleanCmd.AddCommand(cleanRecoverCmd)
	rootCmd.AddCommand(cleanCmd)
}
