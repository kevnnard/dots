package restore

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

// fakeExec records every command the runner asks to execute and fails the
// apps listed in fail.
type fakeExec struct {
	calls []ExecRequest
	fail  map[string]bool
}

func (f *fakeExec) run(req ExecRequest) error {
	f.calls = append(f.calls, req)
	if f.fail[req.AppName] {
		return errors.New("exit status 1")
	}
	return nil
}

func (f *fakeExec) apps() string {
	var names []string
	for _, c := range f.calls {
		names = append(names, c.AppName)
	}
	return strings.Join(names, ",")
}

func noneInstalled(string, manifest.AppConfig) bool { return false }

func resultByName(t *testing.T, r *RunResult, name string) StepResult {
	t.Helper()
	for _, s := range r.Steps {
		if s.AppName == name {
			return s
		}
	}
	t.Fatalf("step %q missing from result", name)
	return StepResult{}
}

// shellFixture is a zsh-like host whose backed-up configuration needs a theme
// plugin (which itself requires the host) and an unrelated helper.
func shellFixture(t *testing.T) (*manifest.Manifest, string, string) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	target := filepath.Join(root, "home", ".zshrc")
	if err := os.MkdirAll(filepath.Join(repo, "configs", "zsh"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "configs", "zsh", ".zshrc"), []byte("# backed up\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"zsh": {
			ConfigRequires: []string{"aa-theme", "fzf"},
			Paths:          []manifest.PathMapping{{Src: target, Dest: "configs/zsh/.zshrc"}},
			Install:        pacmanRecipe("zsh"),
		},
		"aa-theme": {Requires: []string{"zsh"}, Install: pacmanRecipe("theme")},
		"fzf":      {Install: pacmanRecipe("fzf")},
		"extra":    {Optional: true, Install: pacmanRecipe("extra")},
	}}
	return m, repo, target
}

func TestRunStockInstallNeverDeploysRepositoryConfig(t *testing.T) {
	m, repo, target := shellFixture(t)
	fx := &fakeExec{}
	res, err := Run(m, []string{"zsh"}, plainArch(), RunOptions{
		RepoDir: repo, InstallPackages: true, Probe: noneInstalled, Exec: fx.run,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := fx.apps(); got != "zsh" {
		t.Errorf("stock install must not pull config dependencies, ran %s", got)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("stock install wrote %s although configuration was not requested", target)
	}
	if s := resultByName(t, res, "zsh"); s.Install != OutcomeInstalled || s.Config != ConfigNotRequested {
		t.Errorf("zsh = %+v", s)
	}
	if p := res.Problems(); len(p) != 0 {
		t.Errorf("unexpected problems: %v", p)
	}
}

func TestRunWithConfigInstallsHostBeforePluginsThenDeploys(t *testing.T) {
	m, repo, target := shellFixture(t)
	fx := &fakeExec{}
	res, err := Run(m, []string{"zsh"}, plainArch(), RunOptions{
		RepoDir: repo, WithConfig: true, InstallPackages: true, Probe: noneInstalled, Exec: fx.run,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := fx.apps(); got != "fzf,zsh,aa-theme" {
		t.Errorf("install order = %s, want fzf,zsh,aa-theme", got)
	}
	for _, c := range fx.calls {
		if c.Dir != repo {
			t.Errorf("%s ran in %q, want repository %q", c.AppName, c.Dir, repo)
		}
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "# backed up\n" {
		t.Errorf("config not deployed: %q %v", data, err)
	}
	if s := resultByName(t, res, "zsh"); s.Config != ConfigDeployed {
		t.Errorf("zsh config = %+v", s)
	}
	if p := res.Problems(); len(p) != 0 {
		t.Errorf("unexpected problems: %v", p)
	}
}

func TestRunConfigIncompleteWhenPluginFailsEvenIfHostInstalled(t *testing.T) {
	m, repo, target := shellFixture(t)
	m.Apps["other"] = manifest.AppConfig{Install: pacmanRecipe("other")}
	fx := &fakeExec{fail: map[string]bool{"aa-theme": true}}
	res, err := Run(m, []string{"zsh", "other"}, plainArch(), RunOptions{
		RepoDir: repo, WithConfig: true, InstallPackages: true, Probe: noneInstalled, Exec: fx.run,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fx.apps(), "other") {
		t.Errorf("unrelated app must still install, ran %s", fx.apps())
	}
	if s := resultByName(t, res, "zsh"); s.Install != OutcomeInstalled || s.Config != ConfigIncomplete || !strings.Contains(s.ConfigReason, "aa-theme") {
		t.Errorf("zsh = %+v", s)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("config deployed although a config prerequisite failed")
	}
	problems := strings.Join(res.Problems(), "\n")
	if !strings.Contains(problems, "aa-theme") || !strings.Contains(problems, "zsh") {
		t.Errorf("problems must name the failed plugin and incomplete config: %s", problems)
	}
}

func TestRunPropagatesInstallFailureToDependents(t *testing.T) {
	m := npmToolManifest()
	fx := &fakeExec{fail: map[string]bool{"node": true}}
	res, err := Run(m, []string{"pi", "zed"}, plainArch(), RunOptions{
		InstallPackages: true, Probe: noneInstalled, Exec: fx.run,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := fx.apps(); got != "node,zed" {
		t.Errorf("ran %s, want node,zed (pi blocked, zed unrelated)", got)
	}
	if s := resultByName(t, res, "node"); s.Install != OutcomeFailed || s.Error == "" {
		t.Errorf("node = %+v", s)
	}
	if s := resultByName(t, res, "pi"); s.Install != OutcomeBlocked || !strings.Contains(s.Reason, "node") {
		t.Errorf("pi = %+v", s)
	}
	if s := resultByName(t, res, "zed"); s.Install != OutcomeInstalled {
		t.Errorf("zed = %+v", s)
	}
	if len(res.Problems()) != 2 {
		t.Errorf("problems = %v, want node failure and pi blocked", res.Problems())
	}
}

func TestRunSkipsAlreadyInstalledApps(t *testing.T) {
	fx := &fakeExec{}
	probe := func(name string, _ manifest.AppConfig) bool { return name == "node" }
	res, err := Run(npmToolManifest(), []string{"pi"}, plainArch(), RunOptions{
		InstallPackages: true, Probe: probe, Exec: fx.run,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := fx.apps(); got != "pi" {
		t.Errorf("ran %s, want only pi", got)
	}
	if s := resultByName(t, res, "node"); s.Install != OutcomeAlreadyInstalled {
		t.Errorf("node = %+v", s)
	}
}

func TestRunDryRunExecutesAndWritesNothing(t *testing.T) {
	m, repo, target := shellFixture(t)
	fx := &fakeExec{}
	res, err := Run(m, []string{"zsh"}, plainArch(), RunOptions{
		RepoDir: repo, WithConfig: true, InstallPackages: true, DryRun: true, Probe: noneInstalled, Exec: fx.run,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.calls) != 0 {
		t.Errorf("dry run executed %s", fx.apps())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("dry run wrote %s", target)
	}
	if s := resultByName(t, res, "zsh"); s.Install != OutcomePlanned || s.Config != ConfigWouldDeploy {
		t.Errorf("zsh = %+v", s)
	}
	if p := res.Problems(); len(p) != 0 {
		t.Errorf("dry run problems: %v", p)
	}
}

func TestRunWithoutPackageInstallKeepsConfigIncomplete(t *testing.T) {
	m, repo, target := shellFixture(t)
	fx := &fakeExec{}
	res, err := Run(m, []string{"zsh"}, plainArch(), RunOptions{
		RepoDir: repo, WithConfig: true, Probe: noneInstalled, Exec: fx.run,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.calls) != 0 {
		t.Errorf("installs ran without InstallPackages: %s", fx.apps())
	}
	if s := resultByName(t, res, "zsh"); s.Config != ConfigIncomplete {
		t.Errorf("zsh = %+v", s)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("config deployed before its prerequisites were installed")
	}
}

func TestRunReportsMissingBackupAsStockConfig(t *testing.T) {
	m, repo, target := shellFixture(t)
	if err := os.Remove(filepath.Join(repo, "configs", "zsh", ".zshrc")); err != nil {
		t.Fatal(err)
	}
	res, err := Run(m, []string{"zsh"}, plainArch(), RunOptions{
		RepoDir: repo, WithConfig: true, InstallPackages: true, Probe: noneInstalled, Exec: (&fakeExec{}).run,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s := resultByName(t, res, "zsh"); s.Config != ConfigNoBackup {
		t.Errorf("zsh = %+v", s)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("nothing should be written without a backup")
	}
	if p := res.Problems(); len(p) != 0 {
		t.Errorf("missing backup is not a failure: %v", p)
	}
}

func TestRunUnsupportedOnlyFailsExplicitRequests(t *testing.T) {
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"mac-only": {Install: map[string]manifest.InstallRecipe{"darwin": {Manager: "brew", Cask: "x"}}},
		"aur-only": {Install: map[string]manifest.InstallRecipe{"yay": {Manager: "yay", Package: "x"}}},
	}}
	opts := RunOptions{InstallPackages: true, Probe: noneInstalled, Exec: (&fakeExec{}).run}
	res, err := Run(m, []string{"aur-only", "mac-only"}, plainArch(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if s := resultByName(t, res, "mac-only"); s.Install != OutcomeUnsupported {
		t.Errorf("mac-only = %+v", s)
	}
	if s := resultByName(t, res, "aur-only"); s.Install != OutcomeManagerMissing {
		t.Errorf("aur-only = %+v", s)
	}
	if p := res.Problems(); len(p) != 0 {
		t.Errorf("profile installs report unsupported apps as skips, got %v", p)
	}
	opts.Explicit = true
	res, err = Run(m, []string{"mac-only"}, plainArch(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if p := res.Problems(); len(p) != 1 {
		t.Errorf("an explicitly requested unsupported app is a problem, got %v", p)
	}
}

func TestRunSummaryIsHonest(t *testing.T) {
	m, repo, _ := shellFixture(t)
	fx := &fakeExec{fail: map[string]bool{"fzf": true}}
	res, err := Run(m, []string{"zsh"}, plainArch(), RunOptions{
		RepoDir: repo, WithConfig: true, InstallPackages: true, Probe: noneInstalled, Exec: fx.run,
	})
	if err != nil {
		t.Fatal(err)
	}
	summary := strings.Join(res.Summary(), "\n")
	for _, want := range []string{"fzf", "failed", "incomplete", "problem"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q:\n%s", want, summary)
		}
	}
}

// The TUI hands its terminal to the run; every install command must get it
// so sudo can prompt. Without it (the CLI) requests carry no streams.
func TestRunPassesTerminalStreamsToExecutor(t *testing.T) {
	m, repo, _ := shellFixture(t)
	in := strings.NewReader("")
	var out, errOut strings.Builder
	fx := &fakeExec{}
	if _, err := Run(m, []string{"zsh"}, plainArch(), RunOptions{
		RepoDir: repo, WithConfig: true, InstallPackages: true, Probe: noneInstalled, Exec: fx.run,
		Stdin: in, Stdout: &out, Stderr: &errOut,
	}); err != nil {
		t.Fatal(err)
	}
	if len(fx.calls) != 3 {
		t.Fatalf("calls = %s", fx.apps())
	}
	for _, c := range fx.calls {
		if c.Stdin != io.Reader(in) || c.Stdout != io.Writer(&out) || c.Stderr != io.Writer(&errOut) {
			t.Errorf("%s did not get the terminal streams", c.AppName)
		}
	}

	fx = &fakeExec{}
	if _, err := Run(m, []string{"zsh"}, plainArch(), RunOptions{RepoDir: repo, InstallPackages: true, Probe: noneInstalled, Exec: fx.run}); err != nil {
		t.Fatal(err)
	}
	if c := fx.calls[0]; c.Stdin != nil || c.Stdout != nil || c.Stderr != nil {
		t.Error("without streams the executor keeps its defaults")
	}
}

func TestShellExecutorRunsInRequestedDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := ShellExecutor(ExecRequest{AppName: "t", Command: "test -f marker", Dir: dir}); err != nil {
		t.Errorf("command did not run in %s: %v", dir, err)
	}
	if err := ShellExecutor(ExecRequest{AppName: "t", Command: "exit 3", Dir: dir}); err == nil {
		t.Error("a failing command must return an error")
	}
}

func TestRunWithConfigKeepsDifferingLocalFilesByDefault(t *testing.T) {
	m, repo, target := shellFixture(t)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("# mine\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := Run(m, []string{"zsh"}, plainArch(), RunOptions{
		RepoDir: repo, WithConfig: true, InstallPackages: true, Probe: noneInstalled, Exec: (&fakeExec{}).run,
	})
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != "# mine\n" {
		t.Errorf("existing local config was overwritten without authorization: %q", data)
	}
	s := resultByName(t, res, "zsh")
	if s.Config != ConfigKeptExisting || s.Files == nil || len(s.Files.Kept) != 1 || len(s.Files.LocalBackups) != 0 {
		t.Errorf("zsh = %+v files=%+v", s, s.Files)
	}
	if !strings.Contains(strings.Join(res.Summary(), "\n"), target) {
		t.Errorf("summary must name the kept file:\n%s", strings.Join(res.Summary(), "\n"))
	}
}

func TestRunReplaceExistingBacksUpBeforeReplacing(t *testing.T) {
	m, repo, target := shellFixture(t)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("# mine\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := Run(m, []string{"zsh"}, plainArch(), RunOptions{
		RepoDir: repo, WithConfig: true, ReplaceExisting: true, InstallPackages: true, Probe: noneInstalled, Exec: (&fakeExec{}).run,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := resultByName(t, res, "zsh")
	if s.Config != ConfigDeployed || len(s.Files.LocalBackups) != 1 {
		t.Fatalf("zsh = %+v files=%+v", s, s.Files)
	}
	if data, _ := os.ReadFile(s.Files.LocalBackups[0]); string(data) != "# mine\n" {
		t.Errorf("safety backup = %q", data)
	}
	if data, _ := os.ReadFile(target); string(data) != "# backed up\n" {
		t.Errorf("target = %q", data)
	}
}

func TestRunReplaceExistingRefusesWhenSafetyBackupFails(t *testing.T) {
	m, repo, target := shellFixture(t)
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("# mine\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// The file stays writable but no backup can be created next to it.
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0755) })
	res, err := Run(m, []string{"zsh"}, plainArch(), RunOptions{
		RepoDir: repo, WithConfig: true, ReplaceExisting: true, InstallPackages: true, Probe: noneInstalled, Exec: (&fakeExec{}).run,
	})
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(target); string(data) != "# mine\n" {
		t.Errorf("file replaced although its safety backup failed: %q", data)
	}
	if s := resultByName(t, res, "zsh"); s.Config != ConfigFailed {
		t.Errorf("zsh = %+v", s)
	}
	if len(res.Problems()) == 0 {
		t.Error("a refused replacement must be reported as a problem")
	}
}

func TestRunPlatformBlockChainIsNotAFailure(t *testing.T) {
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"aa-mac": {Install: map[string]manifest.InstallRecipe{"darwin": {Manager: "brew", Package: "x"}}},
		"bb-mid": {Requires: []string{"aa-mac"}, Install: pacmanRecipe("mid")},
		"cc-top": {Requires: []string{"bb-mid"}, Install: pacmanRecipe("top")},
	}}
	res, err := Run(m, []string{"cc-top"}, plainArch(), RunOptions{InstallPackages: true, Probe: noneInstalled, Exec: (&fakeExec{}).run})
	if err != nil {
		t.Fatal(err)
	}
	if s := resultByName(t, res, "cc-top"); s.Install != OutcomeBlocked {
		t.Errorf("cc-top = %+v", s)
	}
	if p := res.Problems(); len(p) != 0 {
		t.Errorf("a platform skip chain in a profile run is not a failure: %v", p)
	}
}

func TestRunWithConfigRefusesTargetEscapingHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	outside := filepath.Join(root, "outside")
	repo := filepath.Join(root, "repo")
	for _, d := range []string{home, outside, filepath.Join(repo, "configs", "nvim")} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.WriteFile(filepath.Join(repo, "configs", "nvim", "init.lua"), []byte("repo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, ".config")); err != nil {
		t.Fatal(err)
	}
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"nvim": {Paths: []manifest.PathMapping{{Src: "~/.config/nvim", Dest: "configs/nvim"}}, Install: pacmanRecipe("neovim")},
	}}
	res, err := Run(m, []string{"nvim"}, plainArch(), RunOptions{
		RepoDir: repo, WithConfig: true, ReplaceExisting: true, InstallPackages: true, Probe: noneInstalled, Exec: (&fakeExec{}).run,
	})
	if err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("configuration written outside home: %v", entries)
	}
	if s := resultByName(t, res, "nvim"); s.Config != ConfigFailed {
		t.Errorf("nvim = %+v", s)
	}
}

// controlLines returns the indexes of lines holding a raw control, format or
// invalid character, without their content.
func controlLines(lines []string) []int {
	var bad []int
	for i, l := range lines {
		for _, c := range l {
			if c < 0x20 || c == 0x7f || (c >= 0x80 && c < 0xa0) || c == '\u202e' || c == '\ufffd' {
				bad = append(bad, i)
				break
			}
		}
	}
	return bad
}

func TestSummaryEscapesControlsOnEveryLine(t *testing.T) {
	esc := "\x1b[2J\x1b]0;SYNTH\x07"
	r := &RunResult{WithConfig: true, Explicit: true, Steps: []StepResult{
		{Step: Step{AppName: "app" + esc, InstallCmd: "pacman -S x" + esc, Requested: true}, Install: OutcomeFailed, Error: "boom" + esc,
			Config: ConfigFailed, Files: &ApplyResult{
				Restored: []string{"~/r" + esc}, LocalBackups: []string{"~/b" + esc}, Kept: []string{"~/k" + esc},
				TemplatesInit: []string{"~/t" + esc}, Warnings: []string{"w" + esc},
			}},
		{Step: Step{AppName: "blocked" + esc, Reason: "why" + esc, Requested: true}, Install: OutcomeBlocked,
			Config: ConfigIncomplete, ConfigReason: "dep" + esc},
		{Step: Step{AppName: "planned" + esc, InstallCmd: "x" + esc}, Install: OutcomePlanned, Config: ConfigFailed, ConfigReason: "cr" + esc},
	}}
	if bad := controlLines(r.Summary()); len(bad) > 0 {
		t.Errorf("summary lines %v keep a raw control character", bad)
	}
	if !strings.Contains(strings.Join(r.Summary(), "\n"), `\u001b[2J`) {
		t.Errorf("control characters must stay visible as escapes")
	}
}

func TestRunSummaryEscapesManifestAndExecutorControls(t *testing.T) {
	name := "evil\x1b[2J"
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{name: {Install: pacmanRecipe("evil\x1b]0;t\x07")}}}
	res, err := Run(m, []string{name}, plainArch(), RunOptions{
		RepoDir: t.TempDir(), InstallPackages: true, Explicit: true, Probe: noneInstalled,
		Exec:      func(ExecRequest) error { return errors.New("exit\x1b[31m 1") },
		Readiness: ReadinessOptions{Home: t.TempDir()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Steps[0].AppName != name {
		t.Errorf("internal app identity must be preserved")
	}
	if bad := controlLines(res.Summary()); len(bad) > 0 {
		t.Errorf("summary lines %v keep a raw control character", bad)
	}
}
