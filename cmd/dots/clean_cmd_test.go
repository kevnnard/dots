package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/clean"
	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

// cleanFixture is an Omarchy machine whose package database answers fixed
// queries only; every other command fails the test.
type cleanFixture struct {
	t                *testing.T
	repo, home, rc   string
	results          map[string]clean.Result
	calls            []string
	interactive      bool
	beforeConfirming func()
}

func (f *cleanFixture) Run(_ context.Context, argv []string) (clean.Result, error) {
	key := strings.Join(argv, " ")
	f.calls = append(f.calls, key)
	r, ok := f.results[key]
	if !ok {
		f.t.Errorf("unexpected command %q", key)
	}
	return r, nil
}

func newCleanFixture(t *testing.T) *cleanFixture {
	t.Helper()
	root := t.TempDir()
	f := &cleanFixture{t: t, repo: filepath.Join(root, "repo"), home: filepath.Join(root, "home"), interactive: true,
		results: map[string]clean.Result{
			"pacman -Q herdr-bin": {Stdout: []byte("herdr-bin 0.3-1\n")},
			"pacman -Q zsh":       {Stdout: []byte("zsh 5.9-5\n")},
			"pacman -Q extra":     {Stderr: []byte("error: package 'extra' was not found\n"), ExitCode: 1},
		}}
	f.rc = filepath.Join(f.home, ".zshrc")
	m := &manifest.Manifest{Version: "1.0.0", Apps: map[string]manifest.AppConfig{
		"herdr": {Install: map[string]manifest.InstallRecipe{"yay": {Manager: "yay", Package: "herdr-bin"}}},
		"zsh": {Install: pacman("zsh"), Secrets: []string{".zsh_secrets"},
			Paths: []manifest.PathMapping{{Src: "~/.zshrc", Dest: "configs/zsh/.zshrc"}}},
		"tailscale": {Install: map[string]manifest.InstallRecipe{"default": {Manager: "script", Command: "curl x | sh"}}},
		"extra":     {Optional: true, Install: pacman("extra")},
	}}
	if err := m.Save(filepath.Join(f.repo, "manifest.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.rc, []byte("# local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.home, _ = filepath.EvalSymlinks(f.home)
	f.rc = filepath.Join(f.home, ".zshrc")

	oldDetect, oldExec, oldHome, oldPresence, oldInteractive := detectSystem, cleanExecutor, cleanHome, cleanPresence, cleanInteractive
	t.Cleanup(func() {
		detectSystem, cleanExecutor, cleanHome, cleanPresence, cleanInteractive = oldDetect, oldExec, oldHome, oldPresence, oldInteractive
	})
	detectSystem = func() *detector.SystemInfo {
		return &detector.SystemInfo{OS: "linux", Distro: detector.DistroArch, PrimaryManager: detector.ManagerYay,
			Managers: []detector.PackageManager{detector.ManagerYay, detector.ManagerPacman}}
	}
	cleanExecutor = f
	cleanHome = func() (string, error) { return f.home, nil }
	cleanPresence = func(string) func(string, manifest.AppConfig) bool {
		return func(name string, _ manifest.AppConfig) bool { return name != "extra" }
	}
	cleanInteractive = func(io.Reader) bool {
		if f.beforeConfirming != nil {
			f.beforeConfirming()
		}
		return f.interactive
	}
	t.Setenv("DOTFILES_DIR", "")
	t.Setenv("DOTFILES_REPO_URL", filepath.Join(root, "no-such-remote"))
	return f
}

// runClean executes `dots clean` with fresh flags, separate output streams
// and stdin.
func runClean(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	cleanAllFlag, cleanPackagesFlag, cleanConfigsFlag, cleanBackupsFlag, cleanDryRunFlag, dotfilesDirFlag = false, false, false, false, false, ""
	var out, errb bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errb)
	rootCmd.SetIn(strings.NewReader(stdin))
	rootCmd.SetArgs(append([]string{"clean"}, args...))
	err := rootCmd.Execute()
	return out.String(), errb.String(), err
}

func (f *cleanFixture) removals() string {
	var got []string
	for _, c := range f.calls {
		if !strings.HasPrefix(c, "pacman -Q ") {
			got = append(got, c)
		}
	}
	return strings.Join(got, ";")
}

func (f *cleanFixture) rcIntact(t *testing.T) {
	t.Helper()
	if data, err := os.ReadFile(f.rc); err != nil || string(data) != "# local\n" {
		t.Fatalf("~/.zshrc changed: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(f.home, clean.QuarantineDir)); !os.IsNotExist(err) {
		t.Fatalf("quarantine created: %v", err)
	}
}

const cleanPreview = `🧹 Cleanup on linux (arch) for herdr, zsh
Package cleanup plan:
  remove zsh: sudo pacman -R zsh
  remove herdr: sudo pacman -R herdr-bin
2 to remove, 0 blocked
Configuration cleanup plan:
  no-config herdr
  quarantine zsh: ~/.zshrc
  keep zsh: .zsh_secrets
1 to quarantine, 0 blocked
`

func TestCleanRejectsMissingSelectionOrScope(t *testing.T) {
	f := newCleanFixture(t)
	for _, tc := range []struct{ args, err string }{
		{"--packages", "name the apps to clean or use --all"},
		{"zsh --all --packages", "name apps or use --all, not both"},
		{"zsh", "nothing selected: add --packages, --configs, --backups or any combination (nothing was changed)"},
		{"--all", "nothing selected: add --packages, --configs, --backups or any combination (nothing was changed)"},
		{"ghost --configs", `unknown app "ghost"`},
	} {
		out, _, err := runClean(t, "yes\n", append(strings.Fields(tc.args), "--dir", f.repo)...)
		if err == nil || err.Error() != tc.err || strings.Contains(out, "cleanup plan") {
			t.Errorf("%s: err %v, output %q", tc.args, err, out)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("rejected runs queried %v", f.calls)
	}
	f.rcIntact(t)
}

func TestCleanDryRunPreviewsBothScopesWithoutChanges(t *testing.T) {
	f := newCleanFixture(t)
	f.interactive = false
	out, stderr, err := runClean(t, "yes\n", "herdr", "zsh", "--packages", "--configs", "-n", "--dir", f.repo)
	if err != nil || stderr != "" {
		t.Fatalf("err %v, stderr %q", err, stderr)
	}
	want := strings.Replace(cleanPreview, "2 to remove, 0 blocked\n", "2 to remove, 0 blocked\n"+dryRunLine, 1) + dryRunLine
	if out != want {
		t.Fatalf("output\n%s\nwant\n%s", out, want)
	}
	if f.removals() != "" {
		t.Fatalf("dry run ran %s", f.removals())
	}
	f.rcIntact(t)
}

func TestCleanAppliesBothScopesAfterOneConfirmation(t *testing.T) {
	f := newCleanFixture(t)
	f.results["sudo pacman -R zsh"] = clean.Result{Stdout: []byte("removing zsh")}
	f.results["sudo pacman -R herdr-bin"] = clean.Result{}
	out, stderr, err := runClean(t, "yes\n", "herdr", "zsh", "--packages", "--configs", "--dir", f.repo)
	if err != nil || stderr != "" {
		t.Fatalf("err %v, stderr %q\n%s", err, stderr, out)
	}
	if got := strings.Join(f.calls, ";"); got != "pacman -Q zsh;pacman -Q herdr-bin;pacman -Q zsh;sudo pacman -R zsh;pacman -Q herdr-bin;sudo pacman -R herdr-bin" {
		t.Fatalf("calls = %s (ownership must be rechecked before each removal)", got)
	}
	matches, _ := filepath.Glob(filepath.Join(f.home, clean.QuarantineDir, "clean-*"))
	if len(matches) != 1 {
		t.Fatalf("quarantines = %v", matches)
	}
	dir := matches[0]
	want := cleanPreview + "Remove 2 package(s) and quarantine 1 file(s)? Type yes to continue: " +
		"quarantined ~/.zshrc\nRecover from " + dir + "\nremoved zsh\nremoved herdr\n" +
		"Undo configurations with: dots clean recover " + dir + "\n" +
		"Applied: 2 package(s) removed, 1 file(s) quarantined; 0 item(s) left untouched (blocked or unsupported).\n"
	if out != want {
		t.Fatalf("output\n%s\nwant\n%s", out, want)
	}
	if _, err := os.Lstat(f.rc); !os.IsNotExist(err) {
		t.Fatalf("~/.zshrc still in place: %v", err)
	}
}

func TestCleanWithoutConfirmationChangesNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		stdin       string
		interactive bool
		err         string
	}{
		"declined":       {"no\n", true, ""},
		"no answer":      {"", true, ""},
		"no terminal":    {"yes\n", false, "confirmation needs an interactive terminal; nothing was changed"},
		"yes is literal": {"y\n", true, ""},
	} {
		f := newCleanFixture(t)
		f.interactive = tc.interactive
		out, _, err := runClean(t, tc.stdin, "herdr", "zsh", "--packages", "--configs", "--dir", f.repo)
		if (tc.err == "" && err != nil) || (tc.err != "" && (err == nil || err.Error() != tc.err)) {
			t.Fatalf("%s: err %v", name, err)
		}
		if tc.err == "" && !strings.HasSuffix(out, "Type yes to continue: Cancelled: nothing was changed.\n") {
			t.Fatalf("%s: output\n%s", name, out)
		}
		if f.removals() != "" {
			t.Fatalf("%s: ran %s", name, f.removals())
		}
		f.rcIntact(t)
	}
}

func TestCleanStopsPackagesWhenConfigsChangedAfterConfirmation(t *testing.T) {
	f := newCleanFixture(t)
	f.beforeConfirming = func() { os.Remove(f.rc) }
	out, _, err := runClean(t, "yes\n", "zsh", "--packages", "--configs", "--dir", f.repo)
	if err == nil || !strings.Contains(err.Error(), "no package was removed") {
		t.Fatalf("err %v\n%s", err, out)
	}
	if f.removals() != "" || !strings.Contains(out, "  missing zsh: ~/.zshrc\n") || strings.Contains(out, "Applied") {
		t.Fatalf("ran %q, output\n%s", f.removals(), out)
	}
}

func TestCleanReportsFailuresAndUnsupportedWithoutSuccess(t *testing.T) {
	f := newCleanFixture(t)
	f.results["sudo pacman -R herdr-bin"] = clean.Result{ExitCode: 1, Stderr: []byte("error: target is required by x")}
	out, stderr, err := runClean(t, "yes\n", "herdr", "--packages", "--dir", f.repo)
	if err == nil || err.Error() != "package cleanup stopped: herdr failed" {
		t.Fatalf("err %v", err)
	}
	if !strings.HasSuffix(out, "Configurations: not selected (add --configs).\nRemove 1 package(s) and quarantine 0 file(s)? Type yes to continue: failed herdr: exit status 1\n") ||
		strings.Contains(out+stderr, "required by x") {
		t.Fatalf("output\n%s\nstderr %q", out, stderr)
	}

	f = newCleanFixture(t)
	out, _, err = runClean(t, "yes\n", "tailscale", "extra", "--packages", "--configs", "--dir", f.repo)
	if err != nil || !strings.Contains(out, "  blocked tailscale: present but no native package owns it\n") ||
		!strings.HasSuffix(out, "Nothing to change (1 item(s) blocked or unsupported).\n") {
		t.Fatalf("err %v, output\n%s", err, out)
	}
	if strings.Contains(out, "Type yes") || f.removals() != "" {
		t.Fatalf("asked or ran %q:\n%s", f.removals(), out)
	}
}

func TestCleanAllMeansDeclaredAppsOnly(t *testing.T) {
	f := newCleanFixture(t)
	f.interactive = false
	out, _, err := runClean(t, "", "--all", "--packages", "--dry-run", "--dir", f.repo)
	if err != nil || !strings.HasPrefix(out, "🧹 Cleanup on linux (arch) for all 4 declared apps (optional included, nothing else on this machine)\n") {
		t.Fatalf("err %v, output\n%s", err, out)
	}
	for _, c := range f.calls {
		if c != "pacman -Q herdr-bin" && c != "pacman -Q zsh" && c != "pacman -Q extra" {
			t.Fatalf("queried an undeclared package: %q", c)
		}
	}
	if !strings.HasSuffix(out, "Configurations: not selected (add --configs).\n") {
		t.Fatalf("output\n%s", out)
	}
}

func TestCleanRecoverNeverOverwritesAndNeedsConfirmation(t *testing.T) {
	f := newCleanFixture(t)
	if _, _, err := runClean(t, "yes\n", "zsh", "--configs", "--dir", f.repo); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(f.home, clean.QuarantineDir, "clean-*"))
	if len(matches) != 1 {
		t.Fatalf("quarantines = %v", matches)
	}
	dir := matches[0]
	preview := "Configuration recovery from " + dir + ":\n  restore zsh: ~/.zshrc\n1 to restore, 0 blocked\n"

	out, _, err := runClean(t, "yes\n", "recover", dir, "--dry-run", "--dir", f.repo)
	if err != nil || out != preview+dryRunLine {
		t.Fatalf("dry run: err %v, output\n%s", err, out)
	}
	f.interactive = false
	if _, _, err := runClean(t, "yes\n", "recover", dir, "--dir", f.repo); err == nil {
		t.Fatal("recovered without a terminal")
	}
	if err := os.WriteFile(f.rc, []byte("# new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.interactive = true
	out, _, err = runClean(t, "yes\n", "recover", dir, "--dir", f.repo)
	if err != nil || !strings.Contains(out, "  conflict zsh: ~/.zshrc: destination exists\n") {
		t.Fatalf("conflict: err %v, output\n%s", err, out)
	}
	if data, _ := os.ReadFile(f.rc); string(data) != "# new\n" {
		t.Fatalf("recovery replaced an existing file: %q", data)
	}
	os.Remove(f.rc)
	out, _, err = runClean(t, "yes\n", "recover", dir, "--dir", f.repo)
	if err != nil || out != preview+"Restore these files? Type yes to continue: restored ~/.zshrc\n" {
		t.Fatalf("restore: err %v, output\n%s", err, out)
	}
	if data, _ := os.ReadFile(f.rc); string(data) != "# local\n" {
		t.Fatalf("restored %q", data)
	}
}

// oldBackups places two restore backups of ~/.zshrc; the newer must always stay.
func (f *cleanFixture) oldBackups(t *testing.T) (older, newer string) {
	t.Helper()
	older, newer = f.rc+".20250101_000000.bk", f.rc+".20250102_000000.bk"
	for _, p := range []string{older, newer} {
		if err := os.WriteFile(p, []byte("# old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return older, newer
}

func TestCleanWithoutBackupsFlagLeavesBackupsAlone(t *testing.T) {
	f := newCleanFixture(t)
	older, newer := f.oldBackups(t)
	out, _, err := runClean(t, "yes\n", "zsh", "--configs", "--dir", f.repo)
	if err != nil || strings.Contains(out, ".bk") || strings.Contains(out, "backup") {
		t.Fatalf("err %v, output\n%s", err, out)
	}
	for _, p := range []string{older, newer} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("%s moved without --backups: %v", p, err)
		}
	}
}

func TestCleanBackupsKeepsNewestAfterConfirmation(t *testing.T) {
	f := newCleanFixture(t)
	older, newer := f.oldBackups(t)
	plan := `Old backup cleanup plan (the most recent backup of every file always stays):
  keep zsh: ~/.zshrc.20250102_000000.bk: most recent backup of ~/.zshrc
  quarantine zsh: ~/.zshrc.20250101_000000.bk: older backup of ~/.zshrc
1 to quarantine, 1 kept, 0 blocked
`
	out, _, err := runClean(t, "yes\n", "zsh", "--backups", "-n", "--dir", f.repo)
	if err != nil || !strings.HasSuffix(out, plan+dryRunLine) {
		t.Fatalf("dry run: err %v, output\n%s", err, out)
	}
	if _, err := os.Lstat(older); err != nil {
		t.Fatalf("dry run moved %s", older)
	}

	out, _, err = runClean(t, "yes\n", "zsh", "--backups", "--dir", f.repo)
	if err != nil {
		t.Fatalf("err %v\n%s", err, out)
	}
	matches, _ := filepath.Glob(filepath.Join(f.home, clean.QuarantineDir, "clean-*"))
	if len(matches) != 1 {
		t.Fatalf("quarantines = %v", matches)
	}
	dir := matches[0]
	want := "Packages: not selected (add --packages).\nConfigurations: not selected (add --configs).\n" + plan +
		"Remove 0 package(s) and quarantine 1 file(s)? Type yes to continue: " +
		"quarantined ~/.zshrc.20250101_000000.bk\nRecover from " + dir + "\n" +
		"Undo old backups with: dots clean recover " + dir + "\n" +
		"Applied: 0 package(s) removed, 1 file(s) quarantined; 0 item(s) left untouched (blocked or unsupported).\n"
	if !strings.HasSuffix(out, want) {
		t.Fatalf("output\n%s\nwant suffix\n%s", out, want)
	}
	if _, err := os.Lstat(older); !os.IsNotExist(err) {
		t.Fatalf("older backup still in place: %v", err)
	}
	if _, err := os.Lstat(newer); err != nil {
		t.Fatalf("newest backup moved: %v", err)
	}
	if data, err := os.ReadFile(f.rc); err != nil || string(data) != "# local\n" {
		t.Fatalf("~/.zshrc changed: %q, %v", data, err)
	}
}
