package backup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

func TestBackupAllForIgnoresAppsAndPathsNotForSystem(t *testing.T) {
	root := t.TempDir()
	stage, home := filepath.Join(root, "stage"), filepath.Join(root, "home")
	t.Setenv("HOME", home)
	for p, body := range map[string]string{
		filepath.Join(home, ".config", "tool", "conf"): "linux copy\n",
		filepath.Join(home, "Library", "tool", "conf"): "mac copy\n",
		filepath.Join(home, ".smartwindow"):            "mac app\n",
		filepath.Join(home, "Library", "only-mac"):     "mac only\n",
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"smartwindow": {OS: []string{"darwin"}, Paths: []manifest.PathMapping{{Src: "~/.smartwindow", Dest: "configs/sw"}}},
		"tool": {Paths: []manifest.PathMapping{
			{Src: "~/Library/tool/conf", Dest: "configs/tool/conf", SrcOS: map[string]string{"linux": "~/.config/tool/conf"}},
			{Src: "~/Library/only-mac", Dest: "configs/tool/mac", OS: []string{"darwin"}},
		}},
	}}
	total, err := BackupAllFor(m, stage, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := total.Results["smartwindow"]; ok {
		t.Errorf("an app not for linux must be ignored")
	}
	if _, err := os.Lstat(filepath.Join(stage, "configs", "sw")); !os.IsNotExist(err) {
		t.Errorf("an app not for linux was staged")
	}
	if _, err := os.Lstat(filepath.Join(stage, "configs", "tool", "mac")); !os.IsNotExist(err) {
		t.Errorf("a path not for linux was staged")
	}
	if data, err := os.ReadFile(filepath.Join(stage, "configs", "tool", "conf")); err != nil || string(data) != "linux copy\n" {
		t.Errorf("staged conf = %q, %v; want the src_os source at the unchanged dest", data, err)
	}
	if total.Total != 1 {
		t.Errorf("total = %d, want 1", total.Total)
	}
}
