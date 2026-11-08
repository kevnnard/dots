package transfer

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

const linkManifest = `version: "1.0.0"
apps:
    claude:
        paths:
            - src: ~/.claude/skills
              dest: configs/claude/skills
`

func receiveArchive(t *testing.T, src string) (string, error) {
	t.Helper()
	r := &fakeRunner{onGet: func(args []string) {
		data, _ := os.ReadFile(src)
		writeFile(t, filepath.Join(args[len(args)-1], filepath.Base(src)), string(data))
	}}
	dest := filepath.Join(t.TempDir(), "incoming")
	_, err := Receive(r, dest, testSHA(t, src))
	return dest, err
}

// An absolute link into an installed package cannot travel as a link, so the
// archive carries it as a reference in the received manifest instead; the
// staged folder itself is left as it is.
func TestArchiveCarriesPackageLinksAsReferences(t *testing.T) {
	stage := t.TempDir()
	writeFile(t, filepath.Join(stage, "manifest.yaml"), linkManifest)
	skills := filepath.Join(stage, "configs", "claude", "skills")
	writeFile(t, filepath.Join(skills, "mine", "SKILL.md"), "mine\n")
	source := filepath.Join(t.TempDir(), "Caskroom", "acme-notes", "1.0.0", "acme-notes", "skills", "notes")
	writeFile(t, filepath.Join(source, "SKILL.md"), "notes\n")
	for name, target := range map[string]string{"notes": source, "plain": "/etc", "shared": "mine"} {
		if err := os.Symlink(target, filepath.Join(skills, name)); err != nil {
			t.Fatal(err)
		}
	}

	src := filepath.Join(t.TempDir(), "dots-backup-pkg.tar.gz")
	if _, err := Archive(stage, src); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(stage, "manifest.yaml")); string(data) != linkManifest {
		t.Errorf("the staged manifest must not change:\n%s", data)
	}
	dest, err := receiveArchive(t, src)
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(filepath.Join(dest, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := []manifest.PackageLink{{Path: "configs/claude/skills/notes", Manager: "homebrew", Kind: "cask", Package: "acme-notes", Version: "1.0.0", Resource: "acme-notes/skills/notes", Type: "dir"}}
	if got := m.Apps["claude"].PackageLinks; !reflect.DeepEqual(got, want) {
		t.Errorf("package_links = %+v, want %+v", got, want)
	}
	for _, name := range []string{"notes", "plain"} {
		if _, err := os.Lstat(filepath.Join(dest, "configs", "claude", "skills", name)); !os.IsNotExist(err) {
			t.Errorf("absolute link %s must not be extracted", name)
		}
	}
	if link, err := os.Readlink(filepath.Join(dest, "configs", "claude", "skills", "shared")); err != nil || link != "mine" {
		t.Errorf("relative link = %q %v", link, err)
	}
}

// A backup without package links is archived with its manifest unchanged.
func TestArchiveKeepsManifestWithoutPackageLinks(t *testing.T) {
	dest, err := receiveArchive(t, archiveOf(t, fixtureRepo(t)))
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dest, "manifest.yaml")); string(data) != "version: 1\n" {
		t.Errorf("manifest = %q", data)
	}
}

func archiveOf(t *testing.T, repo string) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "dots-backup-plain.tar.gz")
	if _, err := Archive(repo, src); err != nil {
		t.Fatal(err)
	}
	return src
}

// References are data the receiver checks before accepting the archive:
// malformed ones, ones outside the app's paths and ones that contradict the
// archive's own entries are refused and nothing is extracted.
func TestReceiveRejectsForgedPackageReferences(t *testing.T) {
	ref := func(path string) string {
		return linkManifest + "        package_links:\n            - path: " + path + "\n              manager: homebrew\n              kind: cask\n              package: acme-notes\n              resource: acme-notes/skills/notes\n"
	}
	cases := map[string]string{
		"traversal":            ref("configs/claude/skills/../../../.ssh/x"),
		"outside app paths":    ref("configs/codex/skills/notes"),
		"absolute":             ref("/etc/notes"),
		"entry in the archive": ref("configs/claude/skills/mine"),
	}
	for name, mf := range cases {
		src := filepath.Join(t.TempDir(), "dots-backup-forged.tar.gz")
		f, _ := os.Create(src)
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)
		for _, e := range [][2]string{{"manifest.yaml", mf}, {"configs/claude/skills/mine", "mine\n"}} {
			_ = tw.WriteHeader(&tar.Header{Name: e[0], Mode: 0o644, Size: int64(len(e[1])), Typeflag: tar.TypeReg})
			_, _ = tw.Write([]byte(e[1]))
		}
		_ = tw.Close()
		_ = gz.Close()
		_ = f.Close()
		dest, err := receiveArchive(t, src)
		if err == nil || !strings.Contains(err.Error(), "package link") {
			t.Errorf("%s: want a package link rejection, got %v", name, err)
		}
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Errorf("%s: nothing may be extracted", name)
		}
	}
}

// A mapped path that is itself a package link, file or directory, travels as
// a reference for exactly that mapped path.
func TestArchiveCarriesMappedRootPackageLinks(t *testing.T) {
	stage := t.TempDir()
	mf := `version: "1.0.0"
apps:
    orbit:
        paths:
            - src: ~/.config/orbit
              dest: configs/orbit/skill
            - src: ~/.quark.conf
              dest: configs/orbit/quark.conf
`
	writeFile(t, filepath.Join(stage, "manifest.yaml"), mf)
	prefix := t.TempDir()
	dir := filepath.Join(prefix, "Caskroom", "orbit-tool", "4.0", "orbit-tool", "skills", "orbit")
	file := filepath.Join(prefix, "Cellar", "libquark", "2.1", "share", "quark", "quark.conf")
	writeFile(t, filepath.Join(dir, "SKILL.md"), "orbit\n")
	writeFile(t, file, "quark\n")
	if err := os.MkdirAll(filepath.Join(stage, "configs", "orbit"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"skill": dir, "quark.conf": file} {
		if err := os.Symlink(target, filepath.Join(stage, "configs", "orbit", name)); err != nil {
			t.Fatal(err)
		}
	}
	src := filepath.Join(t.TempDir(), "dots-backup-root.tar.gz")
	if _, err := Archive(stage, src); err != nil {
		t.Fatal(err)
	}
	dest, err := receiveArchive(t, src)
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(filepath.Join(dest, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := []manifest.PackageLink{
		{Path: "configs/orbit/quark.conf", Manager: "homebrew", Kind: "formula", Package: "libquark", Version: "2.1", Resource: "share/quark/quark.conf", Type: "file"},
		{Path: "configs/orbit/skill", Manager: "homebrew", Kind: "cask", Package: "orbit-tool", Version: "4.0", Resource: "orbit-tool/skills/orbit", Type: "dir"},
	}
	if got := m.Apps["orbit"].PackageLinks; !reflect.DeepEqual(got, want) {
		t.Errorf("package_links = %+v, want %+v", got, want)
	}
}
