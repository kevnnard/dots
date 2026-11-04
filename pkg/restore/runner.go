package restore

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

// ExecRequest is one install command the runner asks to execute.
type ExecRequest struct {
	AppName string
	Command string
	Dir     string // working directory: the dotfiles repository
	// Stdin, Stdout and Stderr are the terminal handed to the command, e.g.
	// by the TUI while it is suspended. Nil keeps the executor defaults.
	Stdin  io.Reader `json:"-"`
	Stdout io.Writer `json:"-"`
	Stderr io.Writer `json:"-"`
}

// Executor runs an install command. Tests inject fakes; ShellExecutor is the
// real implementation.
type Executor func(ExecRequest) error

// ShellExecutor runs the command through the platform shell inside req.Dir,
// streaming its output to the terminal: the request's streams when set,
// otherwise the process stdout and stderr (stdin stays unset).
func ShellExecutor(req ExecRequest) error {
	name, args := shellInvocation(runtime.GOOS, req.Command)
	cmd := exec.Command(name, args...)
	cmd.Dir = req.Dir
	cmd.Stdin = req.Stdin
	cmd.Stdout = os.Stdout
	if req.Stdout != nil {
		cmd.Stdout = req.Stdout
	}
	cmd.Stderr = os.Stderr
	if req.Stderr != nil {
		cmd.Stderr = req.Stderr
	}
	return cmd.Run()
}

// InstallOutcome is what happened to an app's package.
type InstallOutcome string

const (
	OutcomeInstalled        InstallOutcome = "installed"
	OutcomeAlreadyInstalled InstallOutcome = "already-installed"
	OutcomeNothingToInstall InstallOutcome = "nothing-to-install" // config-only app
	OutcomePlanned          InstallOutcome = "planned"            // dry run or package installation disabled
	OutcomeFailed           InstallOutcome = "failed"
	OutcomeBlocked          InstallOutcome = "blocked" // a required app is not available
	OutcomeUnsupported      InstallOutcome = "unsupported"
	OutcomeManagerMissing   InstallOutcome = "manager-missing"
)

// ConfigOutcome is what happened to an app's backed-up configuration.
type ConfigOutcome string

const (
	ConfigNotRequested ConfigOutcome = ""              // stock install, or the app has no config paths
	ConfigDeployed     ConfigOutcome = "deployed"      // every repository file is in place
	ConfigWouldDeploy  ConfigOutcome = "would-deploy"  // dry run
	ConfigKeptExisting ConfigOutcome = "kept-existing" // differing local files were preserved
	ConfigNoBackup     ConfigOutcome = "no-backup"     // repository has no config: stock config kept
	ConfigIncomplete   ConfigOutcome = "incomplete"    // a config prerequisite is not installed
	ConfigFailed       ConfigOutcome = "failed"        // file errors or refused replacements
)

// StepResult is the outcome of one planned app. It is the shared result
// rendered by the CLI and the TUI.
type StepResult struct {
	Step
	Install      InstallOutcome `json:"install"`
	Error        string         `json:"error,omitempty"`
	Config       ConfigOutcome  `json:"config,omitempty"`
	ConfigReason string         `json:"config_reason,omitempty"`
	Files        *ApplyResult   `json:"files,omitempty"`
	// failedChain marks an app blocked, directly or transitively, by a
	// package whose install command failed during this run.
	failedChain bool
}

// RunOptions tunes Run.
type RunOptions struct {
	RepoDir string
	// WithConfig deploys the configuration backed up in RepoDir and pulls each
	// app's config_requires. Stock installs leave it false.
	WithConfig bool
	// ReplaceExisting authorizes replacing differing local config files after
	// a successful safety backup. Without it they are kept.
	ReplaceExisting bool
	// InstallPackages executes install commands; otherwise they stay planned.
	InstallPackages bool
	// DryRun executes nothing and writes nothing.
	DryRun bool
	// Explicit marks a selection of named apps: a requested app that cannot be
	// installed on this platform is then a problem rather than a skip.
	Explicit bool
	Probe    InstalledProbe
	Exec     Executor
	// Stdin, Stdout and Stderr are passed to every ExecRequest.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Readiness tunes the read-only end-of-run readiness scan.
	Readiness ReadinessOptions
}

// RunResult is the outcome of a whole run, in plan order.
type RunResult struct {
	Steps           []StepResult `json:"steps"`
	DryRun          bool         `json:"dry_run"`
	WithConfig      bool         `json:"with_config"`
	InstallPackages bool         `json:"install_packages"`
	Explicit        bool         `json:"explicit"`
	// Readiness lists, per app, missing keys, pending logins and missing
	// commands found in the apps' files after the run.
	Readiness *ReadinessReport `json:"readiness,omitempty"`
}

// Run plans selected apps with BuildPlan and executes the plan: packages are
// installed in order (a failure blocks the apps that require it, unrelated
// apps continue), then, when WithConfig is set, each app's configuration is
// deployed only if the app and all its requires and config_requires are
// installed.
func Run(m *manifest.Manifest, selected []string, sys *detector.SystemInfo, opts RunOptions) (*RunResult, error) {
	steps, err := BuildPlan(m, selected, sys, PlanOptions{WithConfig: opts.WithConfig, Probe: opts.Probe})
	if err != nil {
		return nil, err
	}
	exe := opts.Exec
	if exe == nil {
		exe = ShellExecutor
	}
	res := &RunResult{DryRun: opts.DryRun, WithConfig: opts.WithConfig, InstallPackages: opts.InstallPackages, Explicit: opts.Explicit}
	outcome := map[string]InstallOutcome{}
	failed := map[string]bool{}

	for _, step := range steps {
		r := StepResult{Step: step}
		switch step.Status {
		case StatusInstalled:
			r.Install = OutcomeAlreadyInstalled
		case StatusConfigOnly:
			r.Install = OutcomeNothingToInstall
		case StatusUnsupported:
			r.Install = OutcomeUnsupported
		case StatusManagerMissing:
			r.Install = OutcomeManagerMissing
		case StatusBlocked:
			r.Install = OutcomeBlocked
		case StatusInstall:
			if dep, why := unmetRequirement(m.Apps[step.AppName].Requires, outcome); dep != "" {
				r.Install = OutcomeBlocked
				r.Reason = fmt.Sprintf("requires %s (%s)", dep, why)
				r.failedChain = failed[dep]
				break
			}
			if opts.DryRun || !opts.InstallPackages {
				r.Install = OutcomePlanned
				break
			}
			if err := exe(ExecRequest{
				AppName: step.AppName, Command: step.InstallCmd, Dir: opts.RepoDir,
				Stdin: opts.Stdin, Stdout: opts.Stdout, Stderr: opts.Stderr,
			}); err != nil {
				r.Install = OutcomeFailed
				r.Error = err.Error()
			} else {
				r.Install = OutcomeInstalled
			}
		}
		outcome[step.AppName] = r.Install
		failed[step.AppName] = r.Install == OutcomeFailed || r.failedChain
		res.Steps = append(res.Steps, r)
	}

	if opts.WithConfig {
		deployConfigs(m, res, outcome, opts)
	}
	res.Readiness = ScanReadiness(m, res.Steps, opts.Readiness)
	return res, nil
}

// deployConfigs deploys each app's backed-up configuration once the app and
// all its prerequisites are installed.
func deployConfigs(m *manifest.Manifest, res *RunResult, outcome map[string]InstallOutcome, opts RunOptions) {
	for i := range res.Steps {
		r := &res.Steps[i]
		app := m.Apps[r.AppName]
		if len(app.Paths) == 0 {
			continue
		}
		if dep, why := unmetConfigPrerequisite(m, r.AppName, outcome, opts.DryRun); dep != "" {
			r.Config = ConfigIncomplete
			r.ConfigReason = fmt.Sprintf("%s is %s", dep, why)
			continue
		}
		files, err := ApplyAppFilesWith(r.AppName, app, opts.RepoDir, ApplyOptions{DryRun: opts.DryRun, Replace: opts.ReplaceExisting})
		if err != nil {
			r.Config = ConfigFailed
			r.ConfigReason = err.Error()
			continue
		}
		r.Files = files
		r.Config = configOutcome(files, opts.DryRun)
	}
}

// unmetRequirement returns the first required app whose install did not
// succeed. Planned requirements count as met: their dependents stay planned.
func unmetRequirement(deps []string, outcome map[string]InstallOutcome) (string, InstallOutcome) {
	for _, dep := range deps {
		switch outcome[dep] {
		case OutcomeFailed, OutcomeBlocked, OutcomeUnsupported, OutcomeManagerMissing:
			return dep, outcome[dep]
		}
	}
	return "", ""
}

// unmetConfigPrerequisite returns the first app among name, its requires and
// its config_requires (with their requires) that is not installed. In a dry
// run a planned install counts as installed.
func unmetConfigPrerequisite(m *manifest.Manifest, name string, outcome map[string]InstallOutcome, dryRun bool) (string, string) {
	needed := map[string]bool{}
	var collect func(string)
	collect = func(n string) {
		if needed[n] {
			return
		}
		needed[n] = true
		for _, dep := range m.Apps[n].Requires {
			collect(dep)
		}
	}
	collect(name)
	for _, dep := range m.Apps[name].ConfigRequires {
		collect(dep)
	}
	names := make([]string, 0, len(needed))
	for n := range needed {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		switch o := outcome[n]; o {
		case OutcomeInstalled, OutcomeAlreadyInstalled, OutcomeNothingToInstall:
		case OutcomePlanned:
			if !dryRun {
				return n, "not installed (package installation not enabled)"
			}
		default:
			return n, string(o)
		}
	}
	return "", ""
}

func configOutcome(files *ApplyResult, dryRun bool) ConfigOutcome {
	switch {
	case len(files.Warnings) > 0:
		return ConfigFailed
	case len(files.Kept) > 0:
		return ConfigKeptExisting
	case len(files.Restored) == 0 && len(files.TemplatesInit) == 0:
		return ConfigNoBackup
	case dryRun:
		return ConfigWouldDeploy
	default:
		return ConfigDeployed
	}
}

// Problems lists everything that makes the run unsuccessful: failed
// installs, apps blocked by them, and configuration that could not be fully
// deployed. Platform skips only count for explicitly requested apps.
func (r *RunResult) Problems() []string {
	var out []string
	for _, s := range r.Steps {
		switch s.Install {
		case OutcomeFailed:
			out = append(out, fmt.Sprintf("%s: install failed: %s", s.AppName, s.Error))
		case OutcomeBlocked:
			if s.failedChain || r.Explicit && s.Requested {
				out = append(out, fmt.Sprintf("%s: blocked: %s", s.AppName, s.Reason))
			}
		case OutcomeUnsupported, OutcomeManagerMissing:
			if r.Explicit && s.Requested {
				out = append(out, fmt.Sprintf("%s: %s: %s", s.AppName, s.Install, s.Reason))
			}
		}
		switch s.Config {
		case ConfigIncomplete:
			out = append(out, fmt.Sprintf("%s: configuration incomplete: %s", s.AppName, s.ConfigReason))
		case ConfigFailed:
			reason := s.ConfigReason
			if s.Files != nil {
				reason = strings.Join(s.Files.Warnings, "; ")
			}
			out = append(out, fmt.Sprintf("%s: configuration failed: %s", s.AppName, reason))
		}
	}
	return out
}

// Summary renders the result as plain lines shared by the CLI and the TUI.
// Manifest names, commands, paths and errors may hold control sequences, so
// every line is escaped before it reaches a terminal.
func (r *RunResult) Summary() []string {
	lines := r.summaryLines()
	for i, l := range lines {
		lines[i] = escapeControls(l)
	}
	return lines
}

func (r *RunResult) summaryLines() []string {
	var lines []string
	title := "📦 Packages:"
	if r.DryRun {
		title = "📦 Packages (dry run, nothing executed):"
	}
	lines = append(lines, title)
	for _, s := range r.Steps {
		lines = append(lines, fmt.Sprintf("  %s %-24s %s", installIcon(s.Install), s.AppName, installDetail(s)))
	}

	lines = append(lines, "", "📁 Configuration:")
	if !r.WithConfig {
		lines = append(lines, "  Not restored: apps keep their stock configuration and local files are untouched.")
	} else {
		any := false
		for _, s := range r.Steps {
			if s.Files == nil && s.Config == ConfigNotRequested {
				continue
			}
			any = true
			lines = append(lines, configLines(s)...)
		}
		if !any {
			lines = append(lines, "  No selected app has configuration paths.")
		}
	}

	if r.Readiness != nil {
		lines = append(lines, "")
		lines = append(lines, r.Readiness.Lines(r.DryRun)...)
	}

	lines = append(lines, "")
	if problems := r.Problems(); len(problems) > 0 {
		lines = append(lines, fmt.Sprintf("❌ Finished with %d problem(s):", len(problems)))
		for _, p := range problems {
			lines = append(lines, "  • "+p)
		}
	} else if r.DryRun {
		lines = append(lines, "🔍 Dry run complete: no commands executed, no files written.")
	} else {
		lines = append(lines, "✨ Finished without problems.")
	}
	if n := r.Readiness.Count(); n > 0 {
		lines = append(lines, fmt.Sprintf("🔑 %d readiness item(s) above need your action before those apps are fully usable.", n))
	}
	return lines
}

func installIcon(o InstallOutcome) string {
	switch o {
	case OutcomeInstalled, OutcomeAlreadyInstalled:
		return "✓"
	case OutcomeFailed:
		return "✗"
	case OutcomeBlocked:
		return "⊘"
	case OutcomePlanned:
		return "•"
	default:
		return "-"
	}
}

func installDetail(s StepResult) string {
	switch s.Install {
	case OutcomeInstalled:
		return "installed: " + s.InstallCmd
	case OutcomeAlreadyInstalled:
		return "already installed, skipped"
	case OutcomeNothingToInstall:
		return "configuration only, nothing to install"
	case OutcomePlanned:
		return "planned: " + s.InstallCmd
	case OutcomeFailed:
		return fmt.Sprintf("failed: %s (%s)", s.InstallCmd, s.Error)
	default:
		return fmt.Sprintf("%s: %s", s.Install, s.Reason)
	}
}

func configLines(s StepResult) []string {
	var lines []string
	switch s.Config {
	case ConfigDeployed:
		lines = append(lines, fmt.Sprintf("  ✓ %-24s deployed from backup", s.AppName))
	case ConfigWouldDeploy:
		lines = append(lines, fmt.Sprintf("  • %-24s would deploy from backup", s.AppName))
	case ConfigKeptExisting:
		lines = append(lines, fmt.Sprintf("  ▲ %-24s existing local files kept (not replaced)", s.AppName))
	case ConfigNoBackup:
		lines = append(lines, fmt.Sprintf("  - %-24s no backup in repository; stock configuration kept", s.AppName))
	case ConfigIncomplete:
		lines = append(lines, fmt.Sprintf("  ✗ %-24s incomplete, not deployed: %s", s.AppName, s.ConfigReason))
	case ConfigFailed:
		lines = append(lines, fmt.Sprintf("  ✗ %-24s failed", s.AppName))
	}
	if s.Files == nil {
		return lines
	}
	for _, f := range s.Files.Restored {
		lines = append(lines, "      ✓ "+f)
	}
	for _, f := range s.Files.LocalBackups {
		lines = append(lines, "      🛡️ safety backup: "+f)
	}
	for _, f := range s.Files.Kept {
		lines = append(lines, "      ▲ kept local file: "+f)
	}
	for _, f := range s.Files.TemplatesInit {
		lines = append(lines, "      🔒 secret template: "+f+" (fill in real values)")
	}
	for _, w := range s.Files.Warnings {
		lines = append(lines, "      ⚠️ "+w)
	}
	return lines
}
