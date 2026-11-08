package restore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
	"github.com/kevnnard/dots/pkg/packagelink"
)

// brewFixture answers read-only Homebrew queries for a prefix laid out under
// the test root, with an unrelated cask and formula installed. Nothing real runs.
type brewFixture struct {
	prefix  string
	info    map[string]string
	missing bool
}

func (b *brewFixture) Run(name string, args ...string) ([]byte, error) {
	if b.missing || name != "brew" {
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

type packageFixture struct {
	repo, home  string
	brew        *brewFixture
	app         manifest.AppConfig
	notes, conf string // where the links must point on this machine
}

// newPackageFixture is a received backup with a legacy absolute link from a
// macOS prefix (notes) and, for a second mapping whose folder the archive
// could not carry, a portable reference only (theme.conf).
func newPackageFixture(t *testing.T) *packageFixture {
	t.Helper()
	root, home := isolatedHome(t)
	f := &packageFixture{repo: filepath.Join(root, "repo"), home: home}
	f.brew = &brewFixture{prefix: filepath.Join(root, "linuxbrew"), info: map[string]string{
		"--cask acme-notes": `{"casks":[{"token":"acme-notes","installed":"2.0.0","version":"2.0.0"}]}`,
		"--formula widgetd": `{"formulae":[{"name":"widgetd","linked_keg":"3.1","installed":[{"version":"3.1"}]}]}`,
	}}
	f.notes = filepath.Join(f.brew.prefix, "Caskroom", "acme-notes", "2.0.0", "acme-notes", "skills", "notes")
	f.conf = filepath.Join(f.brew.prefix, "Cellar", "widgetd", "3.1", "share", "widgetd", "theme.conf")
	mustWrite(t, filepath.Join(f.notes, "SKILL.md"), "notes\n")
	mustWrite(t, f.conf, "theme\n")
	mustWrite(t, filepath.Join(f.repo, "configs", "claude", "skills", "mine", "SKILL.md"), "mine\n")
	if err := os.Symlink("/opt/homebrew/Caskroom/acme-notes/1.0.0/acme-notes/skills/notes", filepath.Join(f.repo, "configs", "claude", "skills", "notes")); err != nil {
		t.Fatal(err)
	}
	f.app = manifest.AppConfig{
		Paths: []manifest.PathMapping{
			{Src: "~/.claude/skills", Dest: "configs/claude/skills"},
			{Src: "~/.config/widgetd", Dest: "configs/widgetd"},
		},
		PackageLinks: []manifest.PackageLink{{Path: "configs/widgetd/theme.conf", Manager: "homebrew", Kind: "formula", Package: "widgetd", Version: "3.0", Resource: "share/widgetd/theme.conf", Type: "file"}},
	}
	return f
}

func (f *packageFixture) apply(t *testing.T, opts ApplyOptions) *ApplyResult {
	t.Helper()
	opts.Packages = packagelink.NewResolver(f.brew)
	res, err := ApplyAppFilesWith("claude", f.app, f.repo, opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (f *packageFixture) dests() (string, string) {
	return filepath.Join(f.home, ".claude", "skills", "notes"), filepath.Join(f.home, ".config", "widgetd", "theme.conf")
}

func TestApplyFilesResolvesPackageLinksOnThisMachine(t *testing.T) {
	f := newPackageFixture(t)
	notes, conf := f.dests()

	dry := f.apply(t, ApplyOptions{DryRun: true, Replace: true})
	if len(dry.Warnings) != 0 || !strings.Contains(strings.Join(dry.Restored, "\n"), notes) || !strings.Contains(strings.Join(dry.Restored, "\n"), conf) {
		t.Fatalf("dry run = %+v", dry)
	}
	for _, p := range []string{notes, conf, filepath.Join(f.home, ".config")} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("dry run wrote %s", p)
		}
	}

	for run := 0; run < 2; run++ {
		res := f.apply(t, ApplyOptions{Replace: true})
		if len(res.Warnings) != 0 || len(res.LocalBackups) != 0 {
			t.Fatalf("run %d: %+v", run, res)
		}
		if got, _ := os.Readlink(notes); got != f.notes {
			t.Errorf("run %d: notes -> %q, want %q", run, got, f.notes)
		}
		if got, _ := os.Readlink(conf); got != f.conf {
			t.Errorf("run %d: theme.conf -> %q, want %q", run, got, f.conf)
		}
		if readOrEmpty(filepath.Join(f.home, ".claude", "skills", "mine", "SKILL.md")) != "mine\n" {
			t.Errorf("run %d: ordinary file not restored", run)
		}
	}
}

func TestApplyFilesKeepsOrReplacesExistingPackageLink(t *testing.T) {
	f := newPackageFixture(t)
	notes, _ := f.dests()
	if err := os.MkdirAll(filepath.Dir(notes), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("elsewhere", notes); err != nil {
		t.Fatal(err)
	}

	kept := f.apply(t, ApplyOptions{})
	if got, _ := os.Readlink(notes); got != "elsewhere" || strings.Join(kept.Kept, "\n") != notes {
		t.Fatalf("without replacement the local link must be kept: %q %+v", got, kept)
	}

	res := f.apply(t, ApplyOptions{Replace: true})
	if got, _ := os.Readlink(notes); got != f.notes {
		t.Errorf("notes -> %q, want %q", got, f.notes)
	}
	if len(res.LocalBackups) != 1 {
		t.Fatalf("want one safety backup, got %+v", res)
	}
	if got, _ := os.Readlink(res.LocalBackups[0]); got != "elsewhere" {
		t.Errorf("safety backup -> %q, want the previous link", got)
	}
}

// Nothing that cannot be proven on this machine is linked, and the local
// file at the destination stays as it was, without a safety backup.
func TestApplyFilesRefusesUnverifiablePackageLinks(t *testing.T) {
	cases := []struct {
		name, want string
		setup      func(f *packageFixture)
	}{
		{"manager missing", "Homebrew", func(f *packageFixture) { f.brew.missing = true }},
		{"package not installed", "not installed", func(f *packageFixture) {
			f.brew.info["--cask acme-notes"] = `{"casks":[{"token":"acme-notes","installed":null}]}`
		}},
		{"resource missing in the installed version", "missing", func(f *packageFixture) {
			os.RemoveAll(f.notes)
		}},
		{"reference contradicts the backed-up link", "contradicts", func(f *packageFixture) {
			f.app.PackageLinks = append(f.app.PackageLinks, manifest.PackageLink{Path: "configs/claude/skills/notes", Manager: "homebrew", Kind: "formula", Package: "widgetd", Resource: "share/widgetd"})
		}},
		{"plain absolute link", "link points outside", func(f *packageFixture) {
			link := filepath.Join(f.repo, "configs", "claude", "skills", "notes")
			os.Remove(link)
			os.Symlink("/etc", link)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPackageFixture(t)
			tc.setup(f)
			notes, _ := f.dests()
			mustWrite(t, notes, "local\n")
			res := f.apply(t, ApplyOptions{Replace: true})
			warned := strings.Join(res.Warnings, "\n")
			if !strings.Contains(warned, notes+" not restored") || !strings.Contains(warned, tc.want) {
				t.Errorf("warnings = %q, want %q for %s", warned, tc.want, notes)
			}
			if readOrEmpty(notes) != "local\n" {
				t.Errorf("local file changed")
			}
			if names := listDir(t, filepath.Dir(notes)); strings.Join(names, ",") != "mine,notes" {
				t.Errorf("unexpected entries beside the local file: %v", names)
			}
		})
	}
}

// Forged or colliding references refuse the whole app's configuration before
// anything is written: the local file stays, no safety backup is made, and
// no other file of the app is deployed.
func TestApplyFilesRefusesAppWithForgedOrCollidingReferences(t *testing.T) {
	cases := map[string]func(f *packageFixture){
		"reference outside the app's paths": func(f *packageFixture) {
			f.app.PackageLinks = append(f.app.PackageLinks, manifest.PackageLink{Path: "configs/codex/skills/notes", Manager: "homebrew", Kind: "cask", Package: "acme-notes", Resource: "acme-notes/skills/notes"})
		},
		"nested reference where the backup holds a file": func(f *packageFixture) {
			mustWrite(t, filepath.Join(f.repo, "configs", "widgetd", "theme.conf"), "real\n")
		},
		"mapped-root reference where the backup holds a file": func(f *packageFixture) {
			f.app.Paths = append(f.app.Paths, manifest.PathMapping{Src: "~/.quark.conf", Dest: "configs/quark.conf"})
			f.app.PackageLinks = append(f.app.PackageLinks, manifest.PackageLink{Path: "configs/quark.conf", Manager: "homebrew", Kind: "formula", Package: "widgetd", Resource: "share/widgetd/theme.conf"})
			mustWrite(t, filepath.Join(f.repo, "configs", "quark.conf"), "real\n")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newPackageFixture(t)
			setup(f)
			_, conf := f.dests()
			root := filepath.Join(f.home, ".quark.conf")
			mustWrite(t, conf, "local\n")
			mustWrite(t, root, "local root\n")
			res := f.apply(t, ApplyOptions{Replace: true})
			if len(res.Warnings) == 0 || len(res.Restored)+len(res.LocalBackups)+len(res.Kept)+len(res.TemplatesInit) != 0 {
				t.Errorf("want only warnings, got %+v", res)
			}
			if readOrEmpty(conf) != "local\n" || readOrEmpty(root) != "local root\n" {
				t.Errorf("local files changed")
			}
			for _, dir := range []string{filepath.Dir(conf), f.home} {
				if bks, _ := filepath.Glob(filepath.Join(dir, "*.bk")); len(bks) != 0 {
					t.Errorf("safety backups made: %v", bks)
				}
			}
			for _, p := range []string{filepath.Join(f.home, ".claude"), filepath.Join(f.home, ".codex")} {
				if _, err := os.Lstat(p); !os.IsNotExist(err) {
					t.Errorf("%s was written", p)
				}
			}
		})
	}
}

// rootFixture maps a directory and a file that are themselves links into
// installed packages, an unrelated cask and formula.
func rootFixture(t *testing.T, raw bool) (*packageFixture, string, string) {
	t.Helper()
	f := newPackageFixture(t)
	f.brew.info["--cask orbit-tool"] = `{"casks":[{"token":"orbit-tool","installed":"4.1"}]}`
	f.brew.info["--formula libquark"] = `{"formulae":[{"name":"libquark","linked_keg":"2.1","installed":[{"version":"2.1"}]}]}`
	dir := filepath.Join(f.brew.prefix, "Caskroom", "orbit-tool", "4.1", "orbit-tool", "skills", "orbit")
	file := filepath.Join(f.brew.prefix, "Cellar", "libquark", "2.1", "share", "quark", "quark.conf")
	mustWrite(t, filepath.Join(dir, "SKILL.md"), "orbit\n")
	mustWrite(t, file, "quark\n")
	f.app = manifest.AppConfig{Paths: []manifest.PathMapping{
		{Src: "~/.config/orbit/skill", Dest: "configs/orbit/skill"},
		{Src: "~/.quark.conf", Dest: "configs/orbit/quark.conf"},
	}}
	if raw {
		if err := os.MkdirAll(filepath.Join(f.repo, "configs", "orbit"), 0755); err != nil {
			t.Fatal(err)
		}
		os.Symlink("/opt/homebrew/Caskroom/orbit-tool/4.0/orbit-tool/skills/orbit", filepath.Join(f.repo, "configs", "orbit", "skill"))
		os.Symlink("/usr/local/Cellar/libquark/2.0/share/quark/quark.conf", filepath.Join(f.repo, "configs", "orbit", "quark.conf"))
	} else {
		f.app.PackageLinks = []manifest.PackageLink{
			{Path: "configs/orbit/skill", Manager: "homebrew", Kind: "cask", Package: "orbit-tool", Version: "4.0", Resource: "orbit-tool/skills/orbit", Type: "dir"},
			{Path: "configs/orbit/quark.conf", Manager: "homebrew", Kind: "formula", Package: "libquark", Version: "2.0", Resource: "share/quark/quark.conf", Type: "file"},
		}
	}
	return f, dir, file
}

func TestApplyFilesResolvesMappedRootPackageLinks(t *testing.T) {
	for _, raw := range []bool{true, false} {
		f, dir, file := rootFixture(t, raw)
		res := f.apply(t, ApplyOptions{Replace: true})
		if len(res.Warnings) != 0 || len(res.Restored) != 2 {
			t.Fatalf("raw=%v: %+v", raw, res)
		}
		if got, _ := os.Readlink(filepath.Join(f.home, ".config", "orbit", "skill")); got != dir {
			t.Errorf("raw=%v: skill -> %q, want %q", raw, got, dir)
		}
		if got, _ := os.Readlink(filepath.Join(f.home, ".quark.conf")); got != file {
			t.Errorf("raw=%v: .quark.conf -> %q, want %q", raw, got, file)
		}
	}
}

// A mapped-root link never replaces a local directory, and a resource whose
// type differs from the recorded one is refused.
func TestApplyFilesMappedRootPackageLinkKeepsTypeAndDirectories(t *testing.T) {
	f, _, _ := rootFixture(t, false)
	f.app.PackageLinks[1].Type = "dir"
	skill := filepath.Join(f.home, ".config", "orbit", "skill")
	mustWrite(t, filepath.Join(skill, "local.md"), "local\n")
	res := f.apply(t, ApplyOptions{Replace: true})
	warned := strings.Join(res.Warnings, "\n")
	if !strings.Contains(warned, skill+" is a directory; not replaced") || !strings.Contains(warned, "is a file here but was a directory") {
		t.Errorf("warnings = %s", warned)
	}
	if readOrEmpty(filepath.Join(skill, "local.md")) != "local\n" {
		t.Errorf("local directory changed")
	}
	if _, err := os.Lstat(filepath.Join(f.home, ".quark.conf")); !os.IsNotExist(err) {
		t.Errorf("a resource of the wrong type was linked")
	}
}

// Proof that a package resource exists never lets a write follow a
// destination parent out of home, not even into that package.
func TestApplyFilesPackageLinkKeepsDestinationParentGuard(t *testing.T) {
	f := newPackageFixture(t)
	pkgDir := filepath.Dir(f.notes)
	if err := os.MkdirAll(filepath.Join(f.home, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(pkgDir, filepath.Join(f.home, ".claude", "skills")); err != nil {
		t.Fatal(err)
	}
	res := f.apply(t, ApplyOptions{Replace: true})
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "outside "+f.home) {
		t.Errorf("want the destination parent refused, got %+v", res.Warnings)
	}
	if names := listDir(t, pkgDir); strings.Join(names, ",") != "notes" {
		t.Errorf("wrote into the package: %v", names)
	}
}
