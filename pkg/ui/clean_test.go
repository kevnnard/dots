package ui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kevnnard/dots/pkg/clean"
	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

// cleanFixture drives the clean flow with a fake package database and a
// temporary home; no real package command or user file is ever touched.
type cleanFixture struct {
	home      string
	zshrc     string
	model     Model
	installed map[string]bool
	fail      map[string]bool
	effects   []string // removals, and whether ~/.zshrc was still home then
	suspended int
	out       bytes.Buffer
}

func (f *cleanFixture) Run(_ context.Context, argv []string) (clean.Result, error) {
	switch {
	case len(argv) == 3 && argv[0] == "pacman" && argv[1] == "-Q":
		if f.installed[argv[2]] {
			return clean.Result{Stdout: []byte(argv[2] + " 1.0\n")}, nil
		}
		return clean.Result{ExitCode: 1, Stderr: []byte("error: package '" + argv[2] + "' was not found")}, nil
	case len(argv) == 4 && argv[0] == "sudo":
		if _, err := os.Lstat(f.zshrc); err == nil {
			f.effects = append(f.effects, "zshrc still in home")
		}
		f.effects = append(f.effects, strings.Join(argv, " "))
		if f.fail[argv[3]] {
			return clean.Result{ExitCode: 1}, nil
		}
		delete(f.installed, argv[3])
		return clean.Result{}, nil
	}
	return clean.Result{ExitCode: 2}, errors.New("unexpected command")
}

func newCleanFixture(t *testing.T) *cleanFixture {
	t.Helper()
	f := &cleanFixture{home: t.TempDir(), fail: map[string]bool{},
		installed: map[string]bool{"zsh": true, "fzf": true, "extra": true}}
	// The runner reports resolved paths (macOS temp dirs are symlinked).
	f.home, _ = filepath.EvalSymlinks(f.home)
	t.Setenv("HOME", f.home)
	f.zshrc = filepath.Join(f.home, ".zshrc")
	os.WriteFile(f.zshrc, []byte("# mine\n"), 0644)
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"zsh": {Paths: []manifest.PathMapping{{Src: "~/.zshrc", Dest: "configs/zsh/.zshrc"}}, Install: pacman("zsh")},
		"fzf": {Install: pacman("fzf")},
		// Optional and installed through yay: "all" still includes it.
		"extra": {Optional: true, Install: map[string]manifest.InstallRecipe{"yay": {Manager: "yay", Package: "extra"}}},
	}}
	sys := &detector.SystemInfo{OS: "linux", Distro: detector.DistroArch, PrimaryManager: detector.ManagerPacman,
		Managers: []detector.PackageManager{detector.ManagerPacman, detector.ManagerYay}}
	f.model = NewModel(m, sys, filepath.Join(t.TempDir(), "no-repo"))
	f.model.CleanExec = f
	f.model.CleanHome = func() (string, error) { return f.home, nil }
	f.model.CleanPresence = func(string) func(string, manifest.AppConfig) bool {
		return func(string, manifest.AppConfig) bool { return false }
	}
	f.model.Suspend = func(c tea.ExecCommand, fn tea.ExecCallback) tea.Cmd {
		return func() tea.Msg {
			f.suspended++
			c.SetStdin(strings.NewReader(""))
			c.SetStdout(&f.out)
			c.SetStderr(&f.out)
			return fn(c.Run())
		}
	}
	return f
}

// open selects the Clean entry of the main menu.
func (f *cleanFixture) open() tea.Model { return press(f.model, "down", "down", "down", "enter") }

func (f *cleanFixture) removals() []string {
	var out []string
	for _, e := range f.effects {
		if strings.HasPrefix(e, "sudo") {
			out = append(out, e)
		}
	}
	return out
}

func (f *cleanFixture) untouched(t *testing.T) {
	t.Helper()
	if len(f.effects) != 0 || f.suspended != 0 {
		t.Errorf("effects=%v handoffs=%d, want none", f.effects, f.suspended)
	}
	if data, _ := os.ReadFile(f.zshrc); string(data) != "# mine\n" {
		t.Errorf("~/.zshrc changed: %q", data)
	}
	if _, err := os.Stat(filepath.Join(f.home, clean.QuarantineDir)); !os.IsNotExist(err) {
		t.Error("a quarantine was created")
	}
}

func TestTUIMainMenuIndicesWithClean(t *testing.T) {
	f := newCleanFixture(t)
	if v := f.model.View(); !strings.Contains(v, "Clean") {
		t.Errorf("main menu must offer Clean:\n%s", v)
	}
	if s := press(f.model, "enter").(Model).State; s != viewRestorePreset {
		t.Errorf("index 0 = %v, want restore", s)
	}
	if b := press(f.model, "down", "enter").(Model); b.State != viewCustomApps || b.Action != "backup" {
		t.Errorf("index 1 = %v/%s, want backup", b.State, b.Action)
	}
	if s := press(f.model, "down", "down", "enter").(Model).State; s != viewStatus {
		t.Errorf("index 2 = %v, want status", s)
	}
	if s := f.open().(Model).State; s != viewCleanMode {
		t.Errorf("index 3 = %v, want clean", s)
	}
	m := press(f.model, "down", "down", "down", "down")
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Error("index 4 must quit")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("index 4 must quit")
	}
	f.untouched(t)
}

func TestTUICleanNeedsScopeAndApps(t *testing.T) {
	f := newCleanFixture(t)
	mode := f.open()
	if v := mode.View(); !strings.Contains(v, "Packages: off") || !strings.Contains(v, "Configurations: off") {
		t.Errorf("both scopes must start off:\n%s", v)
	}
	if m := press(mode, "enter").(Model); m.State != viewCleanMode || !strings.Contains(m.View(), "nothing selected") {
		t.Errorf("all without a scope must stay put:\n%s", m.View())
	}
	custom := press(mode, "p", "down", "enter").(Model)
	if custom.State != viewCleanApps {
		t.Fatalf("custom = %v", custom.State)
	}
	for _, it := range custom.clean.items {
		if it.Checked {
			t.Errorf("%s starts checked; custom cleanup must start empty", it.Name)
		}
	}
	if m := press(custom, "enter").(Model); m.State != viewCleanApps || !strings.Contains(m.View(), "no app selected") {
		t.Errorf("empty custom selection must stay put:\n%s", m.View())
	}
	f.untouched(t)
}

func TestTUICleanPreviewThenCancelChangesNothing(t *testing.T) {
	f := newCleanFixture(t)
	preview := press(f.open(), "p", "c", "enter").(Model)
	v := preview.View()
	for _, want := range []string{"sudo pacman -R extra", "sudo pacman -R fzf", "quarantine", ".zshrc", "Type yes"} {
		if !strings.Contains(v, want) {
			t.Errorf("preview misses %q:\n%s", want, v)
		}
	}
	if m := press(preview, "n", "o", "enter").(Model); m.State != viewCleanPreview || m.clean.typed != "" {
		t.Errorf("a non-yes answer must not run: state=%v", m.State)
	}
	if m := press(preview, "y", "e", "esc").(Model); m.State != viewCleanMode {
		t.Errorf("esc must leave the preview: %v", m.State)
	}
	f.untouched(t)
}

func TestTUICleanAllRunsConfigsThenPackagesInOneHandoff(t *testing.T) {
	f := newCleanFixture(t)
	preview := press(f.open(), "p", "c", "enter")
	updated, cmd := press(preview, "y", "e", "s").Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(f.effects) != 0 || updated.(Model).State != viewExecuting || cmd == nil {
		t.Fatalf("confirmation must hand off the terminal before any effect: %v", f.effects)
	}
	done := drain(updated, cmd).(Model)
	if f.suspended != 1 {
		t.Errorf("handoffs = %d, want 1", f.suspended)
	}
	// Dependents first: the planner orders removals in reverse.
	want := "sudo pacman -R zsh,sudo pacman -R fzf,sudo pacman -R extra"
	if got := strings.Join(f.effects, ","); got != want {
		t.Errorf("effects = %s, want configs moved before %s", got, want)
	}
	recs, _ := filepath.Glob(filepath.Join(f.home, clean.QuarantineDir, "clean-*"))
	v := done.View()
	if len(recs) != 1 || !strings.Contains(v, "dots clean recover "+recs[0]) || !strings.Contains(v, "3 package(s) removed, 1 file(s) quarantined") {
		t.Errorf("summary must report what changed and how to undo it (records %v):\n%s", recs, v)
	}
	if back := press(done, "enter").(Model); back.State != viewMain || back.clean.preview != nil {
		t.Error("leaving the summary must drop the clean state")
	}
}

func TestTUICleanCustomPackagesOnly(t *testing.T) {
	f := newCleanFixture(t)
	custom := press(f.open(), "p", "down", "enter")
	// Items are sorted: extra, fzf, zsh.
	done := press(custom, "down", "space", "enter", "yes", "enter").(Model)
	if got := strings.Join(f.effects, ","); got != "zshrc still in home,sudo pacman -R fzf" {
		t.Errorf("effects = %s, want only fzf", got)
	}
	if !strings.Contains(done.View(), "1 package(s) removed, 0 file(s) quarantined") {
		t.Errorf("summary:\n%s", done.View())
	}
}

func TestTUICleanConfigDriftStopsPackages(t *testing.T) {
	f := newCleanFixture(t)
	preview := press(f.open(), "p", "c", "enter")
	os.Remove(f.zshrc)
	done := press(preview, "yes", "enter").(Model)
	if len(f.removals()) != 0 {
		t.Errorf("packages removed after the config plan changed: %v", f.effects)
	}
	if v := done.View(); !strings.Contains(v, "no package was removed") || strings.Contains(v, "Applied") {
		t.Errorf("drift must be reported, never as success:\n%s", v)
	}
}

func TestTUICleanPackageFailureKeepsRecoveryPath(t *testing.T) {
	f := newCleanFixture(t)
	f.fail["fzf"] = true
	done := press(f.open(), "p", "c", "enter", "yes", "enter").(Model)
	if got := strings.Join(f.removals(), ","); got != "sudo pacman -R zsh,sudo pacman -R fzf" {
		t.Errorf("removals = %s; the run must stop at the first failure", got)
	}
	v := done.View()
	for _, want := range []string{"failed fzf", "not-run extra", "stay quarantined", "dots clean recover"} {
		if !strings.Contains(v, want) {
			t.Errorf("summary misses %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "Applied") {
		t.Errorf("a failed run must not read as success:\n%s", v)
	}
}

func TestTUICleanNothingToChangeCannotConfirm(t *testing.T) {
	f := newCleanFixture(t)
	f.installed = map[string]bool{}
	os.Remove(f.zshrc)
	os.Mkdir(f.zshrc, 0755) // directory configs are not supported
	preview := press(f.open(), "p", "c", "enter", "yes", "enter").(Model)
	v := preview.View()
	if preview.State != viewCleanPreview || !strings.Contains(v, "Nothing to change") || !strings.Contains(v, "directories are not supported") {
		t.Errorf("an empty plan must be reported and never confirmable:\n%s", v)
	}
	if f.suspended != 0 || len(f.effects) != 0 {
		t.Errorf("effects=%v handoffs=%d", f.effects, f.suspended)
	}
}

func TestTUICleanIgnoresStalePreviewAndResetsState(t *testing.T) {
	f := newCleanFixture(t)
	mode := press(f.open(), "p", "c")
	pending, cmd := mode.Update(tea.KeyMsg{Type: tea.KeyEnter})
	back := press(pending, "esc", "esc") // leave clean before the probe returns
	stale := cmd()
	again := press(back, "down", "down", "down", "enter")
	m, _ := again.Update(stale)
	if mm := m.(Model); mm.State != viewCleanMode || mm.clean.preview != nil || mm.clean.packages || mm.clean.configs {
		t.Errorf("re-entering clean must start fresh and drop stale previews: state=%v", mm.State)
	}
	f.untouched(t)
}

func TestTUICleanReportsHandoffFailure(t *testing.T) {
	f := newCleanFixture(t)
	f.model.Suspend = func(c tea.ExecCommand, fn tea.ExecCallback) tea.Cmd {
		return func() tea.Msg { return fn(errors.New("could not release terminal")) }
	}
	done := press(f.open(), "p", "c", "enter", "yes", "enter").(Model)
	if v := done.View(); !strings.Contains(v, "nothing ran") || strings.Contains(v, "Applied") {
		t.Errorf("handoff failure must be honest:\n%s", v)
	}
	f.untouched(t)
}

// oldBackups places two restore backups of ~/.zshrc; the newer must always stay.
func (f *cleanFixture) oldBackups(t *testing.T) (older, newer string) {
	t.Helper()
	older, newer = f.zshrc+".20250101_000000.bk", f.zshrc+".20250102_000000.bk"
	for _, p := range []string{older, newer} {
		if err := os.WriteFile(p, []byte("# old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return older, newer
}

func TestTUICleanOldBackupsOffByDefaultMovesNothing(t *testing.T) {
	f := newCleanFixture(t)
	older, newer := f.oldBackups(t)
	mode := f.open()
	if v := mode.View(); !strings.Contains(v, "[b]") || !strings.Contains(v, "Old backups: off") {
		t.Errorf("the old backups toggle must start off:\n%s", v)
	}
	preview := press(mode, "c", "enter").(Model)
	if v := preview.View(); strings.Contains(v, ".bk") || !strings.Contains(v, "Old backups: not selected") {
		t.Errorf("backups must not be planned when off:\n%s", v)
	}
	press(preview, "yes", "enter")
	for _, p := range []string{older, newer} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s moved with the option off: %v", p, err)
		}
	}
}

func TestTUICleanOldBackupsKeepsNewest(t *testing.T) {
	f := newCleanFixture(t)
	older, newer := f.oldBackups(t)
	preview := press(f.open(), "b", "enter").(Model)
	v := preview.View()
	for _, want := range []string{"keep zsh: ~/.zshrc.20250102_000000.bk: most recent backup of ~/.zshrc",
		"quarantine zsh: ~/.zshrc.20250101_000000.bk: older backup of ~/.zshrc", "Packages: not selected", "Type yes"} {
		if !strings.Contains(v, want) {
			t.Errorf("preview misses %q:\n%s", want, v)
		}
	}
	done := press(preview, "yes", "enter").(Model)
	if len(f.effects) != 0 {
		t.Errorf("packages touched: %v", f.effects)
	}
	if _, err := os.Lstat(older); !os.IsNotExist(err) {
		t.Errorf("older backup still in place: %v", err)
	}
	if _, err := os.Lstat(newer); err != nil {
		t.Errorf("newest backup moved: %v", err)
	}
	if data, _ := os.ReadFile(f.zshrc); string(data) != "# mine\n" {
		t.Errorf("~/.zshrc changed: %q", data)
	}
	recs, _ := filepath.Glob(filepath.Join(f.home, clean.QuarantineDir, "clean-*"))
	if v := done.View(); len(recs) != 1 || !strings.Contains(v, "Undo old backups with: dots clean recover "+recs[0]) ||
		!strings.Contains(v, "0 package(s) removed, 1 file(s) quarantined") {
		t.Errorf("summary must report the move and how to undo it (records %v):\n%s", recs, v)
	}
}

func TestTUICleanOldBackupsSingleBackupCannotConfirm(t *testing.T) {
	f := newCleanFixture(t)
	older, _ := f.oldBackups(t)
	os.Remove(older)
	preview := press(f.open(), "b", "enter", "yes", "enter").(Model)
	if v := preview.View(); preview.State != viewCleanPreview || !strings.Contains(v, "only backup of ~/.zshrc") || !strings.Contains(v, "Nothing to change") {
		t.Errorf("a single backup must stay and leave nothing to confirm:\n%s", v)
	}
	f.untouched(t)
}
