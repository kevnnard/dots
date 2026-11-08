package backup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

// A mapped source that is itself a link into an installed package is staged
// as that link, never as the package's contents; a mapped source linking
// anywhere else is still copied through the link as before.
func TestBackupStagesMappedRootPackageLinkAsLink(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	prefix := filepath.Join(root, "custom-brew")
	dir := filepath.Join(prefix, "Caskroom", "orbit-tool", "4.0", "orbit-tool", "skills", "orbit")
	file := filepath.Join(prefix, "Cellar", "libquark", "2.1", "share", "quark", "quark.conf")
	plain := filepath.Join(root, "elsewhere", "plain.conf")
	for p, data := range map[string]string{filepath.Join(dir, "SKILL.md"): "orbit\n", file: "quark\n", plain: "plain\n"} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	links := map[string]string{"orbit": dir, "quark.conf": file, "plain.conf": plain}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(home, name)); err != nil {
			t.Fatal(err)
		}
	}
	app := manifest.AppConfig{Paths: []manifest.PathMapping{
		{Src: filepath.Join(home, "orbit"), Dest: "configs/orbit"},
		{Src: filepath.Join(home, "quark.conf"), Dest: "configs/quark.conf"},
		{Src: filepath.Join(home, "plain.conf"), Dest: "configs/plain.conf"},
	}}
	stage := filepath.Join(root, "stage")
	res, err := BackupApp("orbit", app, stage)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Copied) != 3 || len(res.Warnings) != 0 {
		t.Fatalf("result = %+v", res)
	}
	for _, name := range []string{"orbit", "quark.conf"} {
		if got, err := os.Readlink(filepath.Join(stage, "configs", name)); err != nil || got != links[name] {
			t.Errorf("%s staged as %q %v, want the link to %s", name, got, err, links[name])
		}
	}
	info, err := os.Lstat(filepath.Join(stage, "configs", "plain.conf"))
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("plain.conf must still be copied as a file: %v %v", info, err)
	}
	if data, _ := os.ReadFile(filepath.Join(stage, "configs", "plain.conf")); string(data) != "plain\n" {
		t.Errorf("plain.conf = %q", data)
	}
}
