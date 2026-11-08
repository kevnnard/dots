package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kevnnard/dots/pkg/clean"
	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
	"github.com/kevnnard/dots/pkg/restore"
	"github.com/kevnnard/dots/pkg/transfer"
	"github.com/kevnnard/dots/pkg/ui"
)

type cliFixture struct {
	repo, target string
	m            *manifest.Manifest
	calls        []restore.ExecRequest
	fail         map[string]bool
	installed    map[string]bool
	// receipts holds the packages pacman -Q reports installed.
	receipts map[string]bool
}

// Run answers pacman -Q for the fixture's receipts; any other query fails.
func (f *cliFixture) Run(_ context.Context, argv []string) (clean.Result, error) {
	if len(argv) == 3 && argv[0] == "pacman" && argv[1] == "-Q" {
		if f.receipts[argv[2]] {
			return clean.Result{Stdout: []byte(argv[2] + " 1.0-1\n")}, nil
		}
		return clean.Result{Stderr: []byte("error: package '" + argv[2] + "' was not found"), ExitCode: 1}, nil
	}
	return clean.Result{}, errors.New("unexpected query")
}

func pacman(pkg string) map[string]manifest.InstallRecipe {
	return map[string]manifest.InstallRecipe{"pacman": {Manager: "pacman", Package: pkg}}
}

func archSystem() *detector.SystemInfo {
	return &detector.SystemInfo{
		OS:             "linux",
		Distro:         detector.DistroArch,
		PrimaryManager: detector.ManagerPacman,
		Managers:       []detector.PackageManager{detector.ManagerPacman},
	}
}

// newCLIFixture writes a repository whose zsh backup needs a theme plugin.
// Every external effect of the restore command is injected.
func newCLIFixture(t *testing.T) *cliFixture {
	t.Helper()
	root := t.TempDir()
	f := &cliFixture{
		repo:      filepath.Join(root, "repo"),
		target:    filepath.Join(root, "home", ".zshrc"),
		fail:      map[string]bool{},
		installed: map[string]bool{},
		receipts:  map[string]bool{},
	}
	f.m = &manifest.Manifest{Version: "1.0.0", Apps: map[string]manifest.AppConfig{
		"zsh": {
			ConfigRequires: []string{"zsh-theme"},
			Paths:          []manifest.PathMapping{{Src: f.target, Dest: "configs/zsh/.zshrc"}},
			Install:        pacman("zsh"),
		},
		"zsh-theme": {Requires: []string{"zsh"}, Install: pacman("theme")},
		"extra":     {Optional: true, Install: pacman("extra")},
	}}
	if err := f.m.Save(filepath.Join(f.repo, "manifest.yaml")); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(f.repo, "configs", "zsh", ".zshrc")
	if err := os.MkdirAll(filepath.Dir(backup), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("# backed up\n"), 0644); err != nil {
		t.Fatal(err)
	}

	oldDetect, oldProbe, oldExec, oldQuery := detectSystem, installProbe, installExecutor, cleanExecutor
	t.Cleanup(func() {
		detectSystem, installProbe, installExecutor, cleanExecutor = oldDetect, oldProbe, oldExec, oldQuery
	})
	cleanExecutor = f
	detectSystem = archSystem
	installProbe = func(name string, _ manifest.AppConfig) bool { return f.installed[name] }
	installExecutor = f.exec
	t.Setenv("DOTFILES_DIR", "")
	t.Setenv("DOTFILES_REPO_URL", filepath.Join(root, "no-such-remote"))
	return f
}

func (f *cliFixture) exec(req restore.ExecRequest) error {
	f.calls = append(f.calls, req)
	if f.fail[req.AppName] {
		return errors.New("exit status 1")
	}
	return nil
}

func (f *cliFixture) ran() string {
	var names []string
	for _, c := range f.calls {
		names = append(names, c.AppName)
	}
	return strings.Join(names, ",")
}

// runRestore executes `dots restore` with fresh flag values.
func runRestore(t *testing.T, args ...string) (string, error) {
	t.Helper()
	dryRunFlag, installPackagesFlag, allProfilesFlag = false, false, false
	withConfigFlag, replaceExistingFlag, receivedFlag = false, false, false
	profileFlag, dotfilesDirFlag = "core", ""
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(append([]string{"restore"}, args...))
	err := rootCmd.Execute()
	return out.String(), err
}

func TestRestoreStockInstallKeepsStockConfig(t *testing.T) {
	f := newCLIFixture(t)
	out, err := runRestore(t, "zsh", "-i", "--dir", f.repo)
	if err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	if f.ran() != "zsh" {
		t.Errorf("ran %q, want zsh only", f.ran())
	}
	if _, err := os.Stat(f.target); !os.IsNotExist(err) {
		t.Errorf("stock install deployed the repository config")
	}
	if !strings.Contains(out, "stock configuration") {
		t.Errorf("output must state configs were not restored:\n%s", out)
	}
}

func TestRestoreSkipsPackageItsManagerReportsInstalled(t *testing.T) {
	f := newCLIFixture(t)
	f.receipts["zsh"] = true
	out, err := runRestore(t, "zsh", "-i", "--dir", f.repo)
	if err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	if f.ran() != "" {
		t.Errorf("ran %q, want nothing: pacman reports zsh installed", f.ran())
	}
}

func TestRestoreWithConfigInstallsPluginsThenDeploys(t *testing.T) {
	f := newCLIFixture(t)
	out, err := runRestore(t, "zsh", "-i", "--with-config", "--dir", f.repo)
	if err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	if f.ran() != "zsh,zsh-theme" {
		t.Errorf("ran %q, want zsh,zsh-theme", f.ran())
	}
	for _, c := range f.calls {
		if c.Dir != f.repo {
			t.Errorf("%s ran in %q, want %q", c.AppName, c.Dir, f.repo)
		}
	}
	if data, _ := os.ReadFile(f.target); string(data) != "# backed up\n" {
		t.Errorf("config not deployed: %q\n%s", data, out)
	}
}

func TestRestoreFailedPluginBlocksConfigAndFails(t *testing.T) {
	f := newCLIFixture(t)
	f.fail["zsh-theme"] = true
	out, err := runRestore(t, "zsh", "-i", "--with-config", "--dir", f.repo)
	if err == nil {
		t.Fatalf("a failed config prerequisite must produce a nonzero exit:\n%s", out)
	}
	if _, err := os.Stat(f.target); !os.IsNotExist(err) {
		t.Errorf("config deployed although zsh-theme failed")
	}
	if !strings.Contains(out, "incomplete") || !strings.Contains(out, "zsh-theme") {
		t.Errorf("output must report the incomplete config and failed plugin:\n%s", out)
	}
}

func TestRestoreKeepsExistingLocalConfigUnlessReplaceAuthorized(t *testing.T) {
	f := newCLIFixture(t)
	os.MkdirAll(filepath.Dir(f.target), 0755)
	os.WriteFile(f.target, []byte("# mine\n"), 0644)

	out, err := runRestore(t, "zsh", "-i", "--with-config", "--dir", f.repo)
	if err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	if data, _ := os.ReadFile(f.target); string(data) != "# mine\n" {
		t.Errorf("local config overwritten without --replace-existing: %q", data)
	}
	if !strings.Contains(out, "kept") {
		t.Errorf("output must report the kept file:\n%s", out)
	}

	out, err = runRestore(t, "zsh", "-i", "--with-config", "--replace-existing", "--dir", f.repo)
	if err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	if data, _ := os.ReadFile(f.target); string(data) != "# backed up\n" {
		t.Errorf("authorized replacement did not happen: %q", data)
	}
	backups, _ := filepath.Glob(f.target + ".*.bk")
	if len(backups) != 1 {
		t.Fatalf("safety backups = %v", backups)
	}
	if data, _ := os.ReadFile(backups[0]); string(data) != "# mine\n" {
		t.Errorf("safety backup = %q", data)
	}
}

func TestRestoreReplaceExistingRequiresWithConfig(t *testing.T) {
	f := newCLIFixture(t)
	if out, err := runRestore(t, "zsh", "--replace-existing", "--dir", f.repo); err == nil {
		t.Fatalf("--replace-existing without --with-config must be rejected:\n%s", out)
	}
	if len(f.calls) != 0 {
		t.Errorf("ran %s", f.ran())
	}
}

func TestRestoreSkipsAlreadyInstalledApps(t *testing.T) {
	f := newCLIFixture(t)
	f.installed["zsh"] = true
	out, err := runRestore(t, "zsh", "-i", "--dir", f.repo)
	if err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	if len(f.calls) != 0 {
		t.Errorf("reinstalled %s", f.ran())
	}
	if !strings.Contains(out, "already installed") {
		t.Errorf("output must report the skip:\n%s", out)
	}
}

func TestRestoreDryRunMutatesNothing(t *testing.T) {
	f := newCLIFixture(t)
	out, err := runRestore(t, "zsh", "-n", "-i", "--with-config", "--dir", f.repo)
	if err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	if len(f.calls) != 0 {
		t.Errorf("dry run executed %s", f.ran())
	}
	if _, err := os.Stat(f.target); !os.IsNotExist(err) {
		t.Errorf("dry run wrote %s", f.target)
	}
	if !strings.Contains(out, "DRY RUN MODE") {
		t.Errorf("missing dry run notice:\n%s", out)
	}

	missing := filepath.Join(t.TempDir(), "not-cloned")
	if out, err := runRestore(t, "-n", "-i", "--dir", missing); err != nil {
		t.Fatalf("dry run without a repository: %v\n%s", err, out)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("dry run cloned or created %s", missing)
	}
}

func TestRestoreProfilesExcludeOptionalUnlessNamed(t *testing.T) {
	f := newCLIFixture(t)
	if out, err := runRestore(t, "-i", "--dir", f.repo); err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	if f.ran() != "zsh,zsh-theme" {
		t.Errorf("core profile ran %q", f.ran())
	}
	f.calls = nil
	if out, err := runRestore(t, "-a", "-i", "--dir", f.repo); err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	if strings.Contains(f.ran(), "extra") {
		t.Errorf("--all must not install optional apps: %s", f.ran())
	}
	f.calls = nil
	if out, err := runRestore(t, "extra", "-i", "--dir", f.repo); err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	if f.ran() != "extra" {
		t.Errorf("explicit optional app ran %q", f.ran())
	}
}

func TestRestoreWithoutInstallPackagesOnlyPlans(t *testing.T) {
	f := newCLIFixture(t)
	out, err := runRestore(t, "zsh", "--dir", f.repo)
	if err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	if len(f.calls) != 0 {
		t.Errorf("ran %s without --install-packages", f.ran())
	}
	if !strings.Contains(out, "planned") || !strings.Contains(out, "--install-packages") {
		t.Errorf("output must show the plan and the install tip:\n%s", out)
	}
}

func TestRestoreUnknownAppFails(t *testing.T) {
	f := newCLIFixture(t)
	if out, err := runRestore(t, "nope", "--dir", f.repo); err == nil {
		t.Fatalf("unknown app must fail:\n%s", out)
	}
}

// The TUI presets drive the same runner as the CLI.
func TestTUIAndCLIRunTheSamePlan(t *testing.T) {
	f := newCLIFixture(t)
	if out, err := runRestore(t, "-i", "--with-config", "--dir", f.repo); err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	cli := f.ran()
	os.Remove(f.target)
	f.calls = nil

	model := ui.NewModel(f.m, archSystem(), f.repo)
	model.Probe = installProbe
	model.Exec = f.exec
	// A fake terminal handoff runs the restore command synchronously.
	model.Suspend = func(c tea.ExecCommand, fn tea.ExecCallback) tea.Cmd {
		return func() tea.Msg {
			c.SetStdin(strings.NewReader(""))
			c.SetStdout(io.Discard)
			c.SetStderr(io.Discard)
			return fn(c.Run())
		}
	}
	var tm tea.Model = model
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyEnter},                     // Restore / Install
		{Type: tea.KeyDown},                      // Core Only
		{Type: tea.KeyRunes, Runes: []rune{'c'}}, // restore backed-up configs
		{Type: tea.KeyEnter},
	} {
		var cmd tea.Cmd
		tm, cmd = tm.Update(key)
		if cmd != nil {
			tm, _ = tm.Update(cmd())
		}
	}
	if got := f.ran(); got != cli {
		t.Errorf("TUI ran %q, CLI ran %q", got, cli)
	}
	if data, _ := os.ReadFile(f.target); string(data) != "# backed up\n" {
		t.Errorf("TUI did not deploy the config: %q", data)
	}
}

func TestRestoreReportsReadinessWithoutValues(t *testing.T) {
	f := newCLIFixture(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	f.m.Apps["agent"] = manifest.AppConfig{
		Paths:   []manifest.PathMapping{{Src: "~/.agent/mcp.json", Dest: "configs/agent/mcp.json"}},
		Install: pacman("agent"),
	}
	if err := f.m.Save(filepath.Join(f.repo, "manifest.yaml")); err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(home, ".agent", "mcp.json")
	os.MkdirAll(filepath.Dir(agent), 0755)
	os.WriteFile(agent, []byte(`{"[SYNTHETIC_CANARY\nFORGED]":"YOUR_TEST_HERE","mcpServers": {"safe": {"command": "/fake/SYNTHETIC_CANARY"}, "stitch": {"headers": {"X-Goog-Api-Key": "YOUR_X_GOOG_API_KEY_HERE", "Cookie": "cli-real-secret-321"}}}}`), 0600)

	out, err := runRestore(t, "agent", "-i", "--dir", f.repo)
	// Failures never print the output: it may carry the planted canaries.
	if err != nil {
		t.Fatalf("readiness items must not fail the run: %v", err)
	}
	// ~/.agent/mcp.json is not in the built-in inventory, so it is named by
	// its manifest position: a declared path may carry an arbitrary name.
	if !strings.Contains(out, "🔑 Readiness") || !strings.Contains(out, "field mcpServers.stitch.headers.X-Goog-Api-Key in path #1 of app #1") {
		t.Errorf("CLI must print the shared readiness report")
	}
	if strings.Contains(out, "~/.agent/") {
		t.Errorf("CLI must not print a path outside the built-in inventory")
	}
	if !strings.Contains(out, "command MCP server command (mcpServers.<key #1>.command in path #1 of app #1)") {
		t.Errorf("CLI must name the field and file of a missing MCP command")
	}
	for _, leak := range []string{"cli-real-secret-321", "YOUR_X_GOOG_API_KEY_HERE", "SYNTHETIC_CANARY", "FORGED", "/fake/"} {
		if strings.Contains(out, leak) {
			t.Errorf("CLI output leaks a planted fixture value")
		}
	}
}

func TestRestoreSummaryEscapesControlsFromManifestAndErrors(t *testing.T) {
	f := newCLIFixture(t)
	t.Setenv("HOME", t.TempDir())
	name := "evil\x1b[2J"
	f.m.Apps[name] = manifest.AppConfig{Install: pacman("evil\x1b]0;SYNTH\x07")}
	if err := f.m.Save(filepath.Join(f.repo, "manifest.yaml")); err != nil {
		t.Fatal(err)
	}
	installExecutor = func(req restore.ExecRequest) error {
		f.calls = append(f.calls, req)
		return errors.New("exit\x1b[31m 1")
	}

	out, err := runRestore(t, name, "-i", "--dir", f.repo)
	// Failures never print the output: it carries the planted sequences.
	if err == nil {
		t.Errorf("a failed install must fail the run")
	}
	if len(f.calls) != 1 || f.calls[0].AppName != name {
		t.Errorf("the executor must receive the app's own identity")
	}
	_, summary, ok := strings.Cut(out, "📦 Packages")
	if !ok {
		t.Fatalf("CLI must print the shared summary")
	}
	for _, seq := range []string{"\x1b", "\x07"} {
		if strings.Contains(summary, seq) {
			t.Errorf("CLI summary keeps a raw control character")
		}
	}
	if !strings.Contains(summary, `evil\u001b[2J`) {
		t.Errorf("CLI summary must show the escaped app name")
	}
}

const (
	legacyZshLine = "source $(dirname $BREW_BIN)/share/powerlevel10k/powerlevel10k.zsh-theme\n"
	conflictingPi = `{"packages":["npm:x",{"source":"npm:gentle-engram","extensions":["+index.ts"]},"npm:gentle-engram@0.3.0"]}`
	canonicalPi   = `{"packages":["npm:x",{"source":"npm:gentle-engram","extensions":["+index.ts"]}]}`
)

// staleZshFixture places the fixture target in an isolated HOME, where the
// known zsh repair applies, with a stale legacy local file and backup.
func staleZshFixture(t *testing.T) *cliFixture {
	t.Helper()
	f := newCLIFixture(t)
	t.Setenv("HOME", filepath.Dir(f.target))
	os.MkdirAll(filepath.Dir(f.target), 0755)
	os.WriteFile(f.target, []byte("# mine\n"+legacyZshLine), 0644)
	os.WriteFile(filepath.Join(f.repo, "configs", "zsh", ".zshrc"), []byte(legacyZshLine), 0644)
	return f
}

func TestRestoreKeptStaleZshIsReportedNotRepaired(t *testing.T) {
	f := staleZshFixture(t)
	out, err := runRestore(t, "zsh", "-i", "--with-config", "--dir", f.repo)
	if err == nil {
		t.Fatalf("a kept known-incompatible file must produce a nonzero exit:\n%s", out)
	}
	if data, _ := os.ReadFile(f.target); string(data) != "# mine\n"+legacyZshLine {
		t.Errorf("local file changed without --replace-existing: %q", data)
	}
	if backups, _ := filepath.Glob(f.target + ".*.bk"); len(backups) != 0 {
		t.Errorf("safety backups without replacement: %v", backups)
	}
	if !strings.Contains(out, "⚠️ repair not applied: "+f.target+": existing local file kept") || strings.Contains(out, "🩹") {
		t.Errorf("output must state the kept file was NOT repaired:\n%s", out)
	}
}

func TestRestoreReplaceExistingDeploysRepairedZshWithSafetyBackup(t *testing.T) {
	f := staleZshFixture(t)
	out, err := runRestore(t, "zsh", "-i", "--with-config", "--replace-existing", "--dir", f.repo)
	if err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	data, _ := os.ReadFile(f.target)
	if strings.Contains(string(data), legacyZshLine) || !strings.Contains(string(data), `[[ -r "$_dots_plugin" ]]`) {
		t.Errorf("deployed config not repaired: %q", data)
	}
	backups, _ := filepath.Glob(f.target + ".*.bk")
	if len(backups) != 1 {
		t.Fatalf("safety backups = %v", backups)
	}
	if bk, _ := os.ReadFile(backups[0]); string(bk) != "# mine\n"+legacyZshLine {
		t.Errorf("safety backup = %q", bk)
	}
	if !strings.Contains(out, "🩹 repaired backup content: "+f.target+": guarded 1 legacy plugin or tool lines") || strings.Contains(out, "repair not applied") {
		t.Errorf("output must report the repair:\n%s", out)
	}
}

func TestRestoreDryRunReplaceWritesNothingAndRunsNoInstaller(t *testing.T) {
	f := staleZshFixture(t)
	out, err := runRestore(t, "zsh", "-n", "-i", "--with-config", "--replace-existing", "--dir", f.repo)
	if err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	if len(f.calls) != 0 {
		t.Errorf("dry run executed %s", f.ran())
	}
	if names, _ := os.ReadDir(filepath.Dir(f.target)); len(names) != 1 {
		t.Errorf("dry run wrote files: %v", names)
	}
	if data, _ := os.ReadFile(f.target); string(data) != "# mine\n"+legacyZshLine {
		t.Errorf("dry run changed the local file: %q", data)
	}
}

// piCLIFixture adds Pi, whose existing local settings carry the known
// duplicate Engram declaration, and a plugin that requires it.
func piCLIFixture(t *testing.T) (*cliFixture, string) {
	t.Helper()
	f := newCLIFixture(t)
	home := filepath.Dir(f.target)
	t.Setenv("HOME", home)
	target := filepath.Join(home, ".pi", "agent", "settings.json")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte(conflictingPi), 0644)
	backup := filepath.Join(f.repo, "configs", "pi", "settings.json")
	os.MkdirAll(filepath.Dir(backup), 0755)
	os.WriteFile(backup, []byte(canonicalPi), 0644)
	f.m.Apps["pi"] = manifest.AppConfig{
		Optional: true,
		Paths:    []manifest.PathMapping{{Src: target, Dest: "configs/pi/settings.json"}},
		Install:  pacman("pi"),
	}
	f.m.Apps["pi-plugin"] = manifest.AppConfig{Optional: true, Requires: []string{"pi"}, Install: pacman("pi-plugin")}
	if err := f.m.Save(filepath.Join(f.repo, "manifest.yaml")); err != nil {
		t.Fatal(err)
	}
	return f, target
}

func TestRestoreBlocksPiOnKnownConflictByDefault(t *testing.T) {
	f, target := piCLIFixture(t)
	out, err := runRestore(t, "pi-plugin", "-i", "--dir", f.repo)
	if err == nil {
		t.Fatalf("a preflight conflict must produce a nonzero exit:\n%s", out)
	}
	if len(f.calls) != 0 {
		t.Errorf("ran %s before the conflict was resolved", f.ran())
	}
	if data, _ := os.ReadFile(target); string(data) != conflictingPi {
		t.Errorf("local settings changed: %q", data)
	}
	if !strings.Contains(out, "conflicting Engram package declarations") || !strings.Contains(out, "install blocked") {
		t.Errorf("output must explain the blocked install:\n%s", out)
	}
}

func TestRestoreAuthorizedRunRepairsPiBeforeFirstInstaller(t *testing.T) {
	f, target := piCLIFixture(t)
	var seen []string
	installExecutor = func(req restore.ExecRequest) error {
		data, _ := os.ReadFile(target)
		seen = append(seen, req.AppName+"="+string(data))
		return f.exec(req)
	}
	out, err := runRestore(t, "pi-plugin", "-i", "--with-config", "--replace-existing", "--dir", f.repo)
	if err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	if got, want := strings.Join(seen, "\n"), "pi="+canonicalPi+"\npi-plugin="+canonicalPi; got != want {
		t.Errorf("installers saw:\n%s", got)
	}
	backups, _ := filepath.Glob(target + ".*.bk")
	if len(backups) != 1 {
		t.Fatalf("safety backups = %v", backups)
	}
	if bk, _ := os.ReadFile(backups[0]); string(bk) != conflictingPi {
		t.Errorf("safety backup = %q", bk)
	}
	if !strings.Contains(out, "🩹 repaired before install: "+target) || !strings.Contains(out, "safety backup: "+backups[0]) {
		t.Errorf("output must report the pre-install repair and its backup:\n%s", out)
	}
}

// receivedFixture records the fixture repository as the last received backup
// under a temporary HOME, the way `dots backup --receive` leaves it.
func receivedFixture(t *testing.T) *cliFixture {
	t.Helper()
	f := newCLIFixture(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	incoming, err := transfer.IncomingDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(incoming, 0755); err != nil {
		t.Fatal(err)
	}
	if err := transfer.RecordLatest(incoming, f.repo); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRestoreReceivedDeploysLastReceivedBackup(t *testing.T) {
	f := receivedFixture(t)
	out, err := runRestore(t, "zsh", "-i", "--received")
	if err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	if f.ran() != "zsh,zsh-theme" {
		t.Errorf("ran %q, want zsh,zsh-theme", f.ran())
	}
	for _, c := range f.calls {
		if c.Dir != f.repo {
			t.Errorf("%s ran in %q, want the received backup %q", c.AppName, c.Dir, f.repo)
		}
	}
	if data, _ := os.ReadFile(f.target); string(data) != "# backed up\n" {
		t.Errorf("--received must deploy the received configs: %q\n%s", data, out)
	}
	if !strings.Contains(out, "Restoring backed-up configs from "+f.repo) {
		t.Errorf("output must name the received backup:\n%s", out)
	}
}

func TestRestoreReceivedWithoutBackupFailsAndRunsNothing(t *testing.T) {
	f := newCLIFixture(t)
	t.Setenv("HOME", t.TempDir())
	out, err := runRestore(t, "zsh", "-i", "--received")
	if err == nil || !strings.Contains(err.Error(), "no received backup yet: run `dots backup --receive <code>` first") {
		t.Fatalf("want no-received-backup error, got %v\n%s", err, out)
	}
	if len(f.calls) != 0 {
		t.Errorf("nothing may run, ran %q", f.ran())
	}
}

func TestRestoreReceivedRejectsDir(t *testing.T) {
	f := receivedFixture(t)
	out, err := runRestore(t, "zsh", "-i", "--received", "--dir", f.repo)
	if err == nil || !strings.Contains(err.Error(), "--received and --dir cannot be combined") {
		t.Fatalf("want conflict error, got %v\n%s", err, out)
	}
	if len(f.calls) != 0 {
		t.Errorf("nothing may run, ran %q", f.ran())
	}
}

// --received implies --with-config, so --replace-existing is accepted with it.
func TestRestoreReceivedAcceptsReplaceExisting(t *testing.T) {
	f := receivedFixture(t)
	if err := os.MkdirAll(filepath.Dir(f.target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.target, []byte("# local\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := runRestore(t, "zsh", "-i", "--received", "--replace-existing")
	if err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}
	if data, _ := os.ReadFile(f.target); string(data) != "# backed up\n" {
		t.Errorf("--received --replace-existing must replace the local file: %q\n%s", data, out)
	}
}
