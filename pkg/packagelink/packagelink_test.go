package packagelink

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

// fakeBrew answers the read-only Homebrew queries from fixtures and records
// every call as exact arguments. Nothing real runs.
type fakeBrew struct {
	caskroom, cellar string
	info             map[string]string // "--cask name" or "--formula name" -> stdout
	missing          bool              // brew is not installed
	calls            []string
}

func (f *fakeBrew) Run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.Join(append([]string{name}, args...), "\x00"))
	if f.missing || name != "brew" {
		return nil, errors.New(`exec: "brew": executable file not found in $PATH`)
	}
	switch {
	case len(args) == 1 && args[0] == "--caskroom":
		return []byte(f.caskroom + "\n"), nil
	case len(args) == 1 && args[0] == "--cellar":
		return []byte(f.cellar + "\n"), nil
	case len(args) == 4 && args[0] == "info" && args[1] == "--json=v2":
		if out, ok := f.info[args[2]+" "+args[3]]; ok {
			return []byte(out), nil
		}
		return nil, errors.New("exit status 1")
	}
	return nil, errors.New("unexpected brew call")
}

func mkfile(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// receiver lays out a Homebrew prefix of this machine with an unrelated cask
// installed at 2.0.0 and a formula plus its dependency.
func receiver(t *testing.T) *fakeBrew {
	t.Helper()
	prefix := t.TempDir()
	f := &fakeBrew{
		caskroom: filepath.Join(prefix, "Caskroom"),
		cellar:   filepath.Join(prefix, "Cellar"),
		info: map[string]string{
			"--cask acme-notes":  `{"formulae":[],"casks":[{"token":"acme-notes","installed":"2.0.0","version":"2.1.0"}]}`,
			"--formula widgetd":  `{"formulae":[{"name":"widgetd","linked_keg":"3.1_1","installed":[{"version":"3.1_1"}]}],"casks":[]}`,
			"--formula libgizmo": `{"formulae":[{"name":"libgizmo","linked_keg":"1.9","installed":[{"version":"1.8"},{"version":"1.9"}]}],"casks":[]}`,
			"--formula libsplit": `{"formulae":[{"name":"libsplit","linked_keg":null,"installed":[{"version":"1"},{"version":"2"}]}],"casks":[]}`,
			"--cask gone-app":    `{"formulae":[],"casks":[{"token":"gone-app","installed":null,"version":"1.0"}]}`,
			"--cask other-token": `{"formulae":[],"casks":[{"token":"impostor","installed":"1.0","version":"1.0"}]}`,
			"--cask odd-json":    `{"casks":[{"token":"odd-json","installed":{"v":"1"}}]}`,
		},
	}
	mkfile(t, filepath.Join(f.caskroom, "acme-notes", "2.0.0", "acme-notes", "skills", "notes", "SKILL.md"))
	mkfile(t, filepath.Join(f.cellar, "widgetd", "3.1_1", "share", "widgetd", "theme.conf"))
	mkfile(t, filepath.Join(f.cellar, "libgizmo", "1.9", "share", "gizmo", "SKILL.md"))
	mkfile(t, filepath.Join(f.cellar, "libsplit", "2", "share", "x"))
	mkfile(t, filepath.Join(f.caskroom, "other-token", "1.0", "res", "x"))
	mkfile(t, filepath.Join(f.caskroom, "odd-json", "1", "res", "x"))
	return f
}

func cask(pkg, version, resource string) manifest.PackageLink {
	return manifest.PackageLink{Manager: "homebrew", Kind: "cask", Package: pkg, Version: version, Resource: resource}
}

func formula(pkg, version, resource string) manifest.PackageLink {
	return manifest.PackageLink{Manager: "homebrew", Kind: "formula", Package: pkg, Version: version, Resource: resource}
}

// Any Homebrew prefix of the source (Apple silicon, Intel, Linuxbrew or a
// custom one) reduces to the package identity and the unchanged resource
// path; nothing else is recognized.
func TestParseRecognizesOnlyHomebrewPackageResources(t *testing.T) {
	ok := map[string]manifest.PackageLink{
		"/opt/homebrew/Caskroom/acme-notes/1.0.0/acme-notes/skills/notes":    cask("acme-notes", "1.0.0", "acme-notes/skills/notes"),
		"/usr/local/Caskroom/acme-notes/1.0.0/acme-notes/skills/notes/":      cask("acme-notes", "1.0.0", "acme-notes/skills/notes"),
		"/home/linuxbrew/.linuxbrew/Cellar/widgetd/3.0/share/widgetd/a.conf": formula("widgetd", "3.0", "share/widgetd/a.conf"),
		"/Users/me/brew/Cellar/libgizmo/1.8/share/gizmo":                     formula("libgizmo", "1.8", "share/gizmo"),
		"/opt/homebrew/Caskroom/font-x/2024.1,7/res":                         cask("font-x", "2024.1,7", "res"),
	}
	for target, want := range ok {
		got, found := Parse(target)
		if !found || got != want {
			t.Errorf("Parse(%q) = %+v %v, want %+v", target, got, found, want)
		}
	}
	for _, target := range []string{
		"relative/Caskroom/a/1/res",
		"/etc/passwd",
		"/opt/homebrew/opt/widgetd/share/x",
		"/opt/homebrew/Caskroom/acme-notes/1.0.0",
		"/opt/homebrew/Caskroom/acme-notes/1.0.0/../../../../etc",
		"/opt/homebrew/Caskroom/../Caskroom/a/1/res",
		"/opt/homebrew/Caskroom/a/1/x/Cellar/b/2/res",
		"/opt/homebrew/Caskroom/bad name/1/res",
		"/opt/homebrew/Caskroom/.hidden/1/res",
		"/opt/homebrew/Cellar/a/../res",
	} {
		if got, found := Parse(target); found {
			t.Errorf("Parse(%q) = %+v, want not recognized", target, got)
		}
	}
}

func TestResolveUsesInstalledPackageOfThisMachine(t *testing.T) {
	f := receiver(t)
	r := NewResolver(f)
	cases := []struct {
		name string
		link manifest.PackageLink
		want string
	}{
		{"cask at a different installed version", cask("acme-notes", "1.0.0", "acme-notes/skills/notes"), filepath.Join(f.caskroom, "acme-notes", "2.0.0", "acme-notes", "skills", "notes")},
		{"formula file", formula("widgetd", "3.0", "share/widgetd/theme.conf"), filepath.Join(f.cellar, "widgetd", "3.1_1", "share", "widgetd", "theme.conf")},
		{"dependency with several kegs picks the linked one", formula("libgizmo", "1.8", "share/gizmo"), filepath.Join(f.cellar, "libgizmo", "1.9", "share", "gizmo")},
	}
	for _, tc := range cases {
		got, err := r.Resolve(tc.link)
		if err != nil || got != tc.want {
			t.Errorf("%s: Resolve = %q %v, want %q", tc.name, got, err, tc.want)
		}
	}
	want := []string{
		"brew\x00--caskroom",
		"brew\x00info\x00--json=v2\x00--cask\x00acme-notes",
		"brew\x00--cellar",
		"brew\x00info\x00--json=v2\x00--formula\x00widgetd",
		"brew\x00info\x00--json=v2\x00--formula\x00libgizmo",
	}
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("brew calls =\n%q\nwant\n%q", f.calls, want)
	}
}

func TestResolveFailsClosed(t *testing.T) {
	f := receiver(t)
	escape := t.TempDir()
	mkfile(t, filepath.Join(escape, "secret"))
	if err := os.Symlink(escape, filepath.Join(f.caskroom, "acme-notes", "2.0.0", "acme-notes", "skills", "evil")); err != nil {
		t.Fatal(err)
	}
	dirAsFile := cask("acme-notes", "2.0.0", "acme-notes/skills/notes")
	dirAsFile.Type = "file"
	cases := []struct {
		name, want string
		link       manifest.PackageLink
		missing    bool
	}{
		{"manager missing", "Homebrew", cask("acme-notes", "1", "acme-notes/skills/notes"), true},
		{"package not installed", "not installed", cask("gone-app", "1.0", "res"), false},
		{"package unknown to Homebrew", "not installed", formula("nope", "1", "res"), false},
		{"metadata for another package", "not installed", cask("other-token", "1.0", "res/x"), false},
		{"unexpected metadata", "metadata", cask("odd-json", "1", "res/x"), false},
		{"several kegs, none linked", "ambiguous", formula("libsplit", "1", "share/x"), false},
		{"resource missing", "missing", cask("acme-notes", "2.0.0", "acme-notes/skills/other"), false},
		{"nested link escapes package", "outside", cask("acme-notes", "2.0.0", "acme-notes/skills/evil/secret"), false},
		{"type changed", "is a directory", dirAsFile, false},
		{"unknown manager", "unsupported", manifest.PackageLink{Manager: "nix", Kind: "cask", Package: "a", Resource: "r"}, false},
		{"traversal in resource", "invalid", cask("acme-notes", "2.0.0", "../../acme-notes/2.0.0/x"), false},
		{"absolute resource", "invalid", cask("acme-notes", "2.0.0", "/etc/passwd"), false},
		{"package with a slash", "invalid", cask("../acme-notes", "2.0.0", "x"), false},
	}
	for _, tc := range cases {
		f.missing = tc.missing
		got, err := NewResolver(f).Resolve(tc.link)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Resolve = %q %v, want error containing %q", tc.name, got, err, tc.want)
		}
	}
}

// A reported Caskroom must be a real absolute directory: a relative or empty
// answer never makes a path relative to the current directory.
func TestResolveRejectsUnusableManagerRoot(t *testing.T) {
	for _, root := range []string{"", "relative/Caskroom", "/no/such/Caskroom"} {
		f := receiver(t)
		f.caskroom = root
		if got, err := NewResolver(f).Resolve(cask("acme-notes", "2.0.0", "acme-notes/skills/notes")); err == nil {
			t.Errorf("caskroom %q: Resolve = %q, want an error", root, got)
		}
	}
}

// References are scoped to the app's own backed-up paths: anything malformed,
// duplicated or outside them is reported and never returned.
func TestScopedKeepsOnlyReferencesUnderTheAppPaths(t *testing.T) {
	good := cask("acme-notes", "1", "acme-notes/skills/notes")
	good.Path = "configs/claude/skills/notes"
	at := func(p string) manifest.PackageLink { l := good; l.Path = p; return l }
	app := manifest.AppConfig{
		Paths: []manifest.PathMapping{{Src: "~/.claude/skills", Dest: "configs/claude/skills"}},
		PackageLinks: []manifest.PackageLink{
			good,
			at("configs/claude/skills"),
			at("configs/claude/skills/../../codex/x"),
			at("/configs/claude/skills/abs"),
			at("configs/codex/skills/notes"),
			at("configs/claude/skillsx/notes"),
			at("configs/claude/skills/dup"),
			at("configs/claude/skills/dup"),
			{Path: "configs/claude/skills/bad", Manager: "homebrew", Kind: "keg", Package: "a", Resource: "r"},
		},
	}
	refs, problems := Scoped(app)
	root := at("configs/claude/skills")
	if len(refs) != 2 || refs[good.Path] != good || refs[root.Path] != root {
		t.Errorf("refs = %+v, want %s and the mapped root itself", refs, good.Path)
	}
	if len(problems) != 6 {
		t.Errorf("problems = %d, want 6:\n%s", len(problems), strings.Join(problems, "\n"))
	}
}

// The installed package and version reported by the manager pin the exact
// package/version directory: an alias of either into another package of the
// same root is refused, while a symlinked root itself is fine.
func TestResolveRejectsPackageAndVersionAliases(t *testing.T) {
	for _, kind := range []string{"cask", "formula"} {
		for _, form := range []string{"version", "package"} {
			f := receiver(t)
			base := f.caskroom
			if kind == "formula" {
				base = f.cellar
			}
			f.info["--"+kind+" nebula-dependency"] = map[string]string{
				"cask":    `{"casks":[{"token":"nebula-dependency","installed":"9.2"}]}`,
				"formula": `{"formulae":[{"name":"nebula-dependency","linked_keg":"9.2","installed":[{"version":"9.2"}]}]}`,
			}[kind]
			mkfile(t, filepath.Join(base, "unrelated", "3", "res", "x"))
			mkfile(t, filepath.Join(base, "unrelated", "9.2", "res", "x"))
			alias, target := filepath.Join(base, "nebula-dependency", "9.2"), filepath.Join(base, "unrelated", "3")
			if form == "package" {
				alias, target = filepath.Join(base, "nebula-dependency"), filepath.Join(base, "unrelated")
			}
			if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, alias); err != nil {
				t.Fatal(err)
			}
			link := manifest.PackageLink{Manager: "homebrew", Kind: kind, Package: "nebula-dependency", Version: "9.2", Resource: "res/x"}
			if got, err := NewResolver(f).Resolve(link); err == nil || !strings.Contains(err.Error(), "not the installed") {
				t.Errorf("%s %s alias: Resolve = %q %v, want refused", kind, form, got, err)
			}
		}
	}

	f := receiver(t)
	linked := filepath.Join(t.TempDir(), "Caskroom")
	if err := os.Symlink(f.caskroom, linked); err != nil {
		t.Fatal(err)
	}
	f.caskroom = linked
	want := filepath.Join(linked, "acme-notes", "2.0.0", "acme-notes", "skills", "notes")
	if got, err := NewResolver(f).Resolve(cask("acme-notes", "2.0.0", "acme-notes/skills/notes")); err != nil || got != want {
		t.Errorf("symlinked root: Resolve = %q %v, want %q", got, err, want)
	}
}
