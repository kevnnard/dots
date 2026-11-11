package status

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

func TestCheckAllForIgnoresAppsAndPathsNotForSystem(t *testing.T) {
	root := t.TempDir()
	repo, home := filepath.Join(root, "repo"), filepath.Join(root, "home")
	t.Setenv("HOME", home)
	linuxSrc := filepath.Join(home, ".config", "tool", "conf")
	for _, p := range []string{linuxSrc, filepath.Join(repo, "configs", "tool", "conf")} {
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("same\n"), 0644); err != nil {
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
	st := CheckAllFor(m, repo, "linux")
	if _, ok := st.Apps["smartwindow"]; ok {
		t.Errorf("an app not for linux must be ignored")
	}
	tool := st.Apps["tool"]
	if tool == nil || len(tool.Paths) != 1 {
		t.Fatalf("tool = %+v, want only the path for linux", tool)
	}
	if p := tool.Paths[0]; p.Src != "~/.config/tool/conf" || p.State != StateSynced || tool.HasDrift {
		t.Errorf("path = %+v drift=%v, want the src_os entry synced", p, tool.HasDrift)
	}
}
