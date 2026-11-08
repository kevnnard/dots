package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
	"github.com/kevnnard/dots/pkg/restore"
	"github.com/kevnnard/dots/pkg/transfer"
)

// fakeBrew answers the read-only Homebrew queries of the receiving machine
// for a prefix laid out under the test root. Nothing real runs.
type fakeBrew struct {
	prefix  string
	info    map[string]string
	missing bool
	calls   []string
}

func (b *fakeBrew) Run(name string, args ...string) ([]byte, error) {
	b.calls = append(b.calls, strings.Join(append([]string{name}, args...), " "))
	if b.missing {
		return nil, errors.New(`exec: "brew": executable file not found in $PATH`)
	}
	switch {
	case len(args) == 1 && args[0] == "--caskroom":
		return []byte(filepath.Join(b.prefix, "Caskroom") + "\n"), nil
	case len(args) == 1 && args[0] == "--cellar":
		return []byte(filepath.Join(b.prefix, "Cellar") + "\n"), nil
	case len(args) == 4 && args[0] == "info":
		if out, ok := b.info[args[2]+" "+args[3]]; ok {
			return []byte(out), nil
		}
	}
	return nil, errors.New("exit status 1")
}

type linkMachines struct {
	repo, srcHome, dstHome string
	brew                   *fakeBrew
	notes, theme           string // the resources on the receiving machine
	orbit, quark           string // resources linked by mapped paths themselves
}

// newLinkMachines sets up a source home whose skills and widget config link
// into an unrelated cask and a formula dependency under a custom Homebrew
// prefix, and a receiving machine with another prefix and newer versions.
func newLinkMachines(t *testing.T) *linkMachines {
	t.Helper()
	root := t.TempDir()
	lm := &linkMachines{repo: filepath.Join(root, "repo"), srcHome: filepath.Join(root, "mac"), dstHome: filepath.Join(root, "linux")}
	m := &manifest.Manifest{Version: "1.0.0", Apps: map[string]manifest.AppConfig{
		"claude":  {Paths: []manifest.PathMapping{{Src: "~/.claude/skills", Dest: "configs/claude/skills"}}},
		"widgetd": {Paths: []manifest.PathMapping{{Src: "~/.config/widgetd", Dest: "configs/widgetd"}}},
		"orbit": {Paths: []manifest.PathMapping{
			{Src: "~/.config/orbit", Dest: "configs/orbit/skill"},
			{Src: "~/.quark.conf", Dest: "configs/orbit/quark.conf"},
		}},
	}}
	if err := m.Save(filepath.Join(lm.repo, "manifest.yaml")); err != nil {
		t.Fatal(err)
	}
	srcPrefix := filepath.Join(root, "custom-brew")
	srcNotes := filepath.Join(srcPrefix, "Caskroom", "acme-notes", "1.0.0", "acme-notes", "skills", "notes")
	srcTheme := filepath.Join(srcPrefix, "Cellar", "libwidget", "1.0", "share", "libwidget", "theme.conf")
	writeTestFile(t, filepath.Join(srcNotes, "SKILL.md"), []byte("notes\n"))
	writeTestFile(t, srcTheme, []byte("theme\n"))
	writeTestFile(t, filepath.Join(lm.srcHome, ".claude", "skills", "mine", "SKILL.md"), []byte("mine\n"))
	writeTestFile(t, filepath.Join(lm.srcHome, ".config", "widgetd", "widgetd.conf"), []byte("conf\n"))
	mustLink(t, srcNotes, filepath.Join(lm.srcHome, ".claude", "skills", "notes"))
	mustLink(t, srcTheme, filepath.Join(lm.srcHome, ".config", "widgetd", "theme.conf"))
	srcOrbit := filepath.Join(srcPrefix, "Caskroom", "orbit-tool", "4.0", "orbit-tool", "skills", "orbit")
	srcQuark := filepath.Join(srcPrefix, "Cellar", "libquark", "2.0", "share", "quark", "quark.conf")
	writeTestFile(t, filepath.Join(srcOrbit, "SKILL.md"), []byte("orbit\n"))
	writeTestFile(t, srcQuark, []byte("quark\n"))
	mustLink(t, srcOrbit, filepath.Join(lm.srcHome, ".config", "orbit"))
	mustLink(t, srcQuark, filepath.Join(lm.srcHome, ".quark.conf"))

	lm.brew = &fakeBrew{prefix: filepath.Join(root, "linuxbrew"), info: map[string]string{
		"--cask acme-notes":   `{"formulae":[],"casks":[{"token":"acme-notes","installed":"2.0.0","version":"2.0.0"}]}`,
		"--formula libwidget": `{"formulae":[{"name":"libwidget","linked_keg":"1.1","installed":[{"version":"1.1"}]}],"casks":[]}`,
		"--cask orbit-tool":   `{"casks":[{"token":"orbit-tool","installed":"4.1"}]}`,
		"--formula libquark":  `{"formulae":[{"name":"libquark","linked_keg":"2.1","installed":[{"version":"2.1"}]}]}`,
	}}
	lm.notes = filepath.Join(lm.brew.prefix, "Caskroom", "acme-notes", "2.0.0", "acme-notes", "skills", "notes")
	lm.theme = filepath.Join(lm.brew.prefix, "Cellar", "libwidget", "1.1", "share", "libwidget", "theme.conf")
	writeTestFile(t, filepath.Join(lm.notes, "SKILL.md"), []byte("notes\n"))
	writeTestFile(t, lm.theme, []byte("theme\n"))
	lm.orbit = filepath.Join(lm.brew.prefix, "Caskroom", "orbit-tool", "4.1", "orbit-tool", "skills", "orbit")
	lm.quark = filepath.Join(lm.brew.prefix, "Cellar", "libquark", "2.1", "share", "quark", "quark.conf")
	writeTestFile(t, filepath.Join(lm.orbit, "SKILL.md"), []byte("orbit\n"))
	writeTestFile(t, lm.quark, []byte("quark\n"))

	oldDetect, oldProbe, oldExec, oldQuery, oldRunner := detectSystem, installProbe, installExecutor, packageQuery, transferRunner
	t.Cleanup(func() {
		detectSystem, installProbe, installExecutor, packageQuery, transferRunner = oldDetect, oldProbe, oldExec, oldQuery, oldRunner
	})
	detectSystem = archSystem
	installProbe = func(string, manifest.AppConfig) bool { return true }
	installExecutor = func(req restore.ExecRequest) error { t.Errorf("nothing may be installed: %s", req.Command); return nil }
	packageQuery = lm.brew
	t.Setenv("DOTFILES_DIR", "")
	t.Setenv("DOTFILES_REPO_URL", filepath.Join(root, "no-such-remote"))
	return lm
}

func mustLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func (lm *linkMachines) receiverHome(t *testing.T) {
	t.Setenv("HOME", lm.dstHome)
	t.Setenv("USERPROFILE", lm.dstHome)
}

func (lm *linkMachines) dests() (string, string) {
	return filepath.Join(lm.dstHome, ".claude", "skills", "notes"), filepath.Join(lm.dstHome, ".config", "widgetd", "theme.conf")
}

// stage runs `dots backup` on the source machine and returns the staged folder.
func (lm *linkMachines) stage(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", lm.srcHome)
	t.Setenv("USERPROFILE", lm.srcHome)
	stage := filepath.Join(t.TempDir(), "stage")
	if out, err := runBackup(t, "--dir", lm.repo, "--out", stage); err != nil {
		t.Fatalf("backup: %v\n%s", err, out)
	}
	return stage
}

// A native backup -> send -> receive -> restore relinks every package-owned
// resource to the package installed on the receiving machine, whatever its
// prefix or installed version.
func TestPackageLinksSurviveNativeBackupTransferRestore(t *testing.T) {
	lm := newLinkMachines(t)
	stage := lm.stage(t)
	r := &inboxRunner{}
	transferRunner = r
	if out, err := runBackup(t, "--send", "station", "--out", stage, "--dir", lm.repo); err != nil {
		t.Fatalf("send: %v\n%s", err, out)
	}

	lm.receiverHome(t)
	received := filepath.Join(t.TempDir(), "received")
	if out, err := runBackup(t, "--receive", transfer.ShortCode(sha256Hex(t, r.archive)), "--into", received); err != nil {
		t.Fatalf("receive: %v\n%s", err, out)
	}
	notes, theme := lm.dests()

	out, err := runRestore(t, "--dir", received, "--all", "--with-config", "--dry-run")
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "• claude") || !strings.Contains(out, "would deploy from backup") || !strings.Contains(out, "      ✓ "+notes) {
		t.Errorf("dry run must plan the links:\n%s", out)
	}
	if _, err := os.Lstat(filepath.Join(lm.dstHome, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote into home")
	}

	out, err = runRestore(t, "--dir", received, "--all", "--with-config")
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if got, _ := os.Readlink(notes); got != lm.notes {
		t.Errorf("notes -> %q, want %q\n%s", got, lm.notes, out)
	}
	if got, _ := os.Readlink(theme); got != lm.theme {
		t.Errorf("theme.conf -> %q, want %q\n%s", got, lm.theme, out)
	}
	lm.assertMappedRoots(t, out)
	for p, want := range map[string]string{filepath.Join(lm.dstHome, ".claude", "skills", "mine", "SKILL.md"): "mine\n", filepath.Join(lm.dstHome, ".config", "widgetd", "widgetd.conf"): "conf\n"} {
		if data, _ := os.ReadFile(p); string(data) != want {
			t.Errorf("%s = %q, want %q", p, data, want)
		}
	}
}

// A staged folder copied as is (SSH) still holds the absolute source links:
// they are resolved the same way, and a differing local link is replaced
// only with --replace-existing, after a .bk safety backup of the link.
func TestRestoreResolvesLegacyAbsolutePackageLinks(t *testing.T) {
	lm := newLinkMachines(t)
	stage := lm.stage(t)
	lm.receiverHome(t)
	notes, theme := lm.dests()
	mustLink(t, "old-notes", notes)

	out, err := runRestore(t, "--dir", stage, "--all", "--with-config")
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if got, _ := os.Readlink(notes); got != "old-notes" || !strings.Contains(out, "▲ kept local file: "+notes) {
		t.Errorf("without --replace-existing the local link must be kept: %q\n%s", got, out)
	}

	out, err = runRestore(t, "--dir", stage, "--all", "--with-config", "--replace-existing")
	if err != nil {
		t.Fatalf("replace: %v\n%s", err, out)
	}
	if got, _ := os.Readlink(notes); got != lm.notes {
		t.Errorf("notes -> %q, want %q", got, lm.notes)
	}
	if got, _ := os.Readlink(theme); got != lm.theme {
		t.Errorf("theme.conf -> %q, want %q", got, lm.theme)
	}
	lm.assertMappedRoots(t, out)
	backups, _ := filepath.Glob(notes + ".*.bk")
	if len(backups) != 1 {
		t.Fatalf("want one safety backup, got %v\n%s", backups, out)
	}
	if got, _ := os.Readlink(backups[0]); got != "old-notes" {
		t.Errorf("safety backup -> %q, want the previous link", got)
	}
}

// When the package cannot be confirmed on this machine, restore fails with
// the reason and leaves the local files and their folder as they were.
func TestRestoreFailsClosedWhenPackageCannotBeConfirmed(t *testing.T) {
	lm := newLinkMachines(t)
	stage := lm.stage(t)
	lm.receiverHome(t)
	lm.brew.info["--cask acme-notes"] = `{"formulae":[],"casks":[{"token":"acme-notes","installed":null}]}`
	notes, _ := lm.dests()
	writeTestFile(t, notes, []byte("local\n"))

	out, err := runRestore(t, "--dir", stage, "--all", "--with-config", "--replace-existing")
	if err == nil || err.Error() != "restore finished with 1 problem(s)" {
		t.Fatalf("want exactly one problem, got %v\n%s", err, out)
	}
	if !strings.Contains(out, notes+" not restored: package link to Homebrew cask acme-notes") || !strings.Contains(out, "not installed") {
		t.Errorf("output must name the unconfirmed package:\n%s", out)
	}
	if data, _ := os.ReadFile(notes); string(data) != "local\n" {
		t.Errorf("local file changed: %q", data)
	}
	if backups, _ := filepath.Glob(notes + ".*.bk"); len(backups) != 0 {
		t.Errorf("no safety backup may be made for a refused link: %v", backups)
	}
	for _, c := range lm.brew.calls {
		if strings.Contains(c, "install") || strings.Contains(c, "fetch") {
			t.Errorf("only read-only queries may run, ran %q", c)
		}
	}
}

// assertMappedRoots checks that the mapped paths that were themselves package
// links are links to the receiving machine's packages.
func (lm *linkMachines) assertMappedRoots(t *testing.T, out string) {
	t.Helper()
	if got, _ := os.Readlink(filepath.Join(lm.dstHome, ".config", "orbit")); got != lm.orbit {
		t.Errorf(".config/orbit -> %q, want %q\n%s", got, lm.orbit, out)
	}
	if got, _ := os.Readlink(filepath.Join(lm.dstHome, ".quark.conf")); got != lm.quark {
		t.Errorf(".quark.conf -> %q, want %q\n%s", got, lm.quark, out)
	}
}

// homeSnapshot records every entry under the receiving home: kind and link
// target or bytes, so a refused run can be proven to change nothing.
func homeSnapshot(t *testing.T, home string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	filepath.Walk(home, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			link, _ := os.Readlink(p)
			snap[p] = "link:" + link
		case info.Mode().IsRegular():
			data, _ := os.ReadFile(p)
			snap[p] = info.Mode().String() + ":" + string(data)
		default:
			snap[p] = info.Mode().String()
		}
		return nil
	})
	return snap
}

func assertSameHome(t *testing.T, before, after map[string]string) {
	t.Helper()
	for p, v := range after {
		if before[p] != v {
			t.Errorf("%s changed: %q -> %q", p, before[p], v)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			t.Errorf("%s removed", p)
		}
	}
}

// An installed package whose package or version directory is an alias of
// another package in the same Caskroom never confirms a link.
func TestRestoreRefusesPackageAliasedIntoAnotherPackage(t *testing.T) {
	for _, form := range []string{"version", "package"} {
		t.Run(form, func(t *testing.T) {
			lm := newLinkMachines(t)
			stage := lm.stage(t)
			lm.receiverHome(t)
			caskroom := filepath.Join(lm.brew.prefix, "Caskroom")
			if err := os.RemoveAll(filepath.Join(caskroom, "acme-notes")); err != nil {
				t.Fatal(err)
			}
			for _, v := range []string{"3", "2.0.0"} {
				writeTestFile(t, filepath.Join(caskroom, "unrelated", v, "acme-notes", "skills", "notes", "SKILL.md"), []byte("other\n"))
			}
			if form == "version" {
				mustLink(t, filepath.Join(caskroom, "unrelated", "3"), filepath.Join(caskroom, "acme-notes", "2.0.0"))
			} else {
				mustLink(t, filepath.Join(caskroom, "unrelated"), filepath.Join(caskroom, "acme-notes"))
			}
			notes, _ := lm.dests()
			writeTestFile(t, filepath.Join(lm.dstHome, ".claude", "skills", "kept.md"), []byte("kept\n"))
			// The app's ordinary file is already current, so only the refused
			// link could change the home.
			writeTestFile(t, filepath.Join(lm.dstHome, ".claude", "skills", "mine", "SKILL.md"), []byte("mine\n"))
			before := homeSnapshot(t, lm.dstHome)
			out, err := runRestore(t, "--dir", stage, "claude", "--with-config", "--replace-existing")
			if err == nil || err.Error() != "restore finished with 1 problem(s)" {
				t.Fatalf("want exactly one problem, got %v\n%s", err, out)
			}
			if !strings.Contains(out, notes+" not restored: package link to Homebrew cask acme-notes") || !strings.Contains(out, "not the installed") {
				t.Errorf("output must refuse the aliased package:\n%s", out)
			}
			assertSameHome(t, before, homeSnapshot(t, lm.dstHome))
		})
	}
}

// A package reference colliding with a staged regular file refuses the app
// before anything is written, even with --replace-existing.
func TestRestoreCollidingReferenceChangesNothing(t *testing.T) {
	lm := newLinkMachines(t)
	stage := lm.stage(t)
	mf := filepath.Join(stage, "manifest.yaml")
	m, err := manifest.Load(mf)
	if err != nil {
		t.Fatal(err)
	}
	app := m.Apps["claude"]
	app.PackageLinks = []manifest.PackageLink{{Path: "configs/claude/skills/mine/SKILL.md", Manager: "homebrew", Kind: "cask", Package: "acme-notes", Resource: "acme-notes/skills/notes/SKILL.md", Type: "file"}}
	m.Apps["claude"] = app
	if err := m.Save(mf); err != nil {
		t.Fatal(err)
	}
	lm.receiverHome(t)
	local := filepath.Join(lm.dstHome, ".claude", "skills", "mine", "SKILL.md")
	writeTestFile(t, local, []byte("local\n"))
	if err := os.Chmod(local, 0600); err != nil {
		t.Fatal(err)
	}
	before := homeSnapshot(t, lm.dstHome)

	out, err := runRestore(t, "--dir", stage, "claude", "--with-config", "--replace-existing")
	if err == nil || err.Error() != "restore finished with 1 problem(s)" {
		t.Fatalf("want exactly one problem, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "configs/claude/skills/mine/SKILL.md: the backup holds an entry there") {
		t.Errorf("output must name the collision:\n%s", out)
	}
	assertSameHome(t, before, homeSnapshot(t, lm.dstHome))
}
