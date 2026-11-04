package clean

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

func backupManifest() *manifest.Manifest {
	return &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"zsh": {Paths: []manifest.PathMapping{{Src: "~/.zshrc", Dest: "configs/zsh/.zshrc"}}},
		"zed": {Paths: []manifest.PathMapping{{Src: "~/.config/zed", Dest: "configs/zed"}}, Secrets: []string{"auth.json"}},
	}}
}

// backupHome builds an isolated home holding the given files.
func backupHome(t *testing.T, files ...string) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		writeFile(t, filepath.Join(home, f), f)
	}
	return home
}

func runBackups(t *testing.T, home string, dryRun bool, confirm func(string) bool) (*ConfigReport, string) {
	t.Helper()
	var out bytes.Buffer
	r := BackupRunner{Manifest: backupManifest(), Home: home, Apps: []string{"zed", "zsh"}, DryRun: dryRun, Confirm: confirm, Out: &out}
	rep, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	return rep, out.String()
}

func exists(t *testing.T, home string, files ...string) {
	t.Helper()
	for _, f := range files {
		if _, err := os.Lstat(filepath.Join(home, f)); err != nil {
			t.Errorf("%s must stay in place: %v", f, err)
		}
	}
}

func TestBackupRunnerKeepsNewestAndQuarantinesOlder(t *testing.T) {
	home := backupHome(t, ".zshrc", ".zshrc.20250101_000000.bk", ".zshrc.20250102_000000.bk", ".zshrc.20250103_000000.bk",
		".config/zed/settings.json", ".config/zed/settings.json.20240101_120000.bk", ".config/zed/settings.json.20240201_120000.bk",
		".zshrc.local.20250101_000000.bk") // another file's backup, never listed
	var shown string
	rep, out := runBackups(t, home, false, func(s string) bool { shown = s; return true })
	want := `Old backup cleanup plan (the most recent backup of every file always stays):
  keep zed: ~/.config/zed/settings.json.20240201_120000.bk: most recent backup of ~/.config/zed/settings.json
  quarantine zed: ~/.config/zed/settings.json.20240101_120000.bk: older backup of ~/.config/zed/settings.json
  keep zsh: ~/.zshrc.20250103_000000.bk: most recent backup of ~/.zshrc
  quarantine zsh: ~/.zshrc.20250102_000000.bk: older backup of ~/.zshrc
  quarantine zsh: ~/.zshrc.20250101_000000.bk: older backup of ~/.zshrc
3 to quarantine, 2 kept, 0 blocked
`
	if shown != want {
		t.Fatalf("summary\n%s\nwant\n%s", shown, want)
	}
	if len(rep.Moved) != 3 || rep.Quarantine == "" || !strings.Contains(out, "Recover from "+rep.Quarantine) {
		t.Fatalf("moved %v into %q:\n%s", rep.Moved, rep.Quarantine, out)
	}
	exists(t, home, ".zshrc", ".zshrc.20250103_000000.bk", ".config/zed/settings.json", ".config/zed/settings.json.20240201_120000.bk",
		".zshrc.local.20250101_000000.bk")
	for _, f := range []string{".zshrc.20250101_000000.bk", ".zshrc.20250102_000000.bk", ".config/zed/settings.json.20240101_120000.bk"} {
		if _, err := os.Lstat(filepath.Join(home, f)); !os.IsNotExist(err) {
			t.Errorf("%s was not moved: %v", f, err)
		}
	}

	// The quarantine is the regular one: recovery brings the backups back.
	var rout bytes.Buffer
	back, err := Recover(context.Background(), Recovery{Home: home, Dir: rep.Quarantine, Out: &rout, Confirm: func(string) bool { return true }})
	if err != nil || len(back.Moved) != 3 {
		t.Fatalf("recover: %v %v\n%s", err, back, rout.String())
	}
	exists(t, home, ".zshrc.20250101_000000.bk", ".zshrc.20250102_000000.bk", ".config/zed/settings.json.20240101_120000.bk")
}

func TestBackupRunnerNeverMovesASingleBackup(t *testing.T) {
	home := backupHome(t, ".zshrc.20250101_000000.bk", ".config/zed/keymap.json.20250101_000000.bk")
	rep, out := runBackups(t, home, false, func(string) bool { t.Error("nothing to move must not ask"); return true })
	want := `Old backup cleanup plan (the most recent backup of every file always stays):
  keep zed: ~/.config/zed/keymap.json.20250101_000000.bk: only backup of ~/.config/zed/keymap.json
  keep zsh: ~/.zshrc.20250101_000000.bk: only backup of ~/.zshrc
0 to quarantine, 2 kept, 0 blocked
Nothing to quarantine.
`
	if out != want || len(rep.Moved) != 0 {
		t.Fatalf("output\n%s\nwant\n%s", out, want)
	}
	exists(t, home, ".zshrc.20250101_000000.bk", ".config/zed/keymap.json.20250101_000000.bk")
	if _, err := os.Stat(filepath.Join(home, QuarantineDir)); !os.IsNotExist(err) {
		t.Fatalf("a quarantine was created: %v", err)
	}
}

func TestBackupRunnerKeepsTiesAndUnrecognizedNames(t *testing.T) {
	kept := []string{".zshrc.20250103_000000.bk", ".zshrc.20250103_000000.1.bk", // newest second is a tie
		".zshrc.20250102_000000.bk", ".zshrc.20250102_000000.1.bk", // an older tie
		".zshrc.old.bk", ".zshrc.20251399_000000.bk", // unrecognized or impossible stamps
		".config/zed/notes.bk",                     // no stamp at all
		".config/zed/auth.json.20250101_000000.bk", // secret: preserved
		".config/zed/auth.json.20250102_000000.bk"}
	home := backupHome(t, append([]string{".zshrc.20250101_000000.bk"}, kept...)...)
	rep, out := runBackups(t, home, false, func(string) bool { return true })
	for _, want := range []string{
		"  keep zed: ~/.config/zed/notes.bk: unrecognized backup name\n",
		"  blocked zed: ~/.config/zed/auth.json.20250101_000000.bk: backup of a preserved file\n",
		"  keep zed: ~/.config/zed/auth.json.20250102_000000.bk: most recent backup of ~/.config/zed/auth.json\n",
		"  keep zsh: ~/.zshrc.20250103_000000.1.bk: same timestamp as another backup of ~/.zshrc\n",
		"  keep zsh: ~/.zshrc.20250103_000000.bk: same timestamp as another backup of ~/.zshrc\n",
		"  keep zsh: ~/.zshrc.20250102_000000.1.bk: same timestamp as another backup of ~/.zshrc\n",
		"  keep zsh: ~/.zshrc.20250102_000000.bk: same timestamp as another backup of ~/.zshrc\n",
		"  quarantine zsh: ~/.zshrc.20250101_000000.bk: older backup of ~/.zshrc\n",
		"  keep zsh: ~/.zshrc.20251399_000000.bk: unrecognized backup name\n",
		"  keep zsh: ~/.zshrc.old.bk: unrecognized backup name\n",
		"1 to quarantine, 8 kept, 1 blocked\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output misses %q:\n%s", want, out)
		}
	}
	if len(rep.Moved) != 1 || rep.Moved[0] != filepath.Join(home, ".zshrc.20250101_000000.bk") {
		t.Fatalf("moved %v", rep.Moved)
	}
	exists(t, home, kept...)
}

func TestBackupRunnerDryRunAndCancelMoveNothing(t *testing.T) {
	files := []string{".zshrc.20250101_000000.bk", ".zshrc.20250102_000000.bk"}
	for _, tc := range []struct {
		name    string
		dryRun  bool
		confirm func(string) bool
		last    string
	}{
		{"dry run", true, func(string) bool { t.Error("dry run must not ask"); return true }, "Dry run: nothing was changed.\n"},
		{"denied", false, func(string) bool { return false }, "Cancelled: nothing was changed.\n"},
		{"no confirm", false, nil, "Cancelled: nothing was changed.\n"},
	} {
		home := backupHome(t, files...)
		rep, out := runBackups(t, home, tc.dryRun, tc.confirm)
		if len(rep.Moved) != 0 || !strings.HasSuffix(out, tc.last) {
			t.Errorf("%s: moved %v\n%s", tc.name, rep.Moved, out)
		}
		exists(t, home, files...)
		if _, err := os.Stat(filepath.Join(home, QuarantineDir)); !os.IsNotExist(err) {
			t.Errorf("%s: a quarantine was created", tc.name)
		}
	}
}

func TestBackupRunnerRejectsUnknownApps(t *testing.T) {
	var out bytes.Buffer
	r := BackupRunner{Manifest: backupManifest(), Home: backupHome(t), Apps: []string{"ghost"}, Out: &out}
	if _, err := r.Run(context.Background()); err == nil || err.Error() != `unknown app "ghost"` {
		t.Fatalf("err = %v", err)
	}
}
