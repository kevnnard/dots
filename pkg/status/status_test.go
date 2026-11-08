package status

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

func TestCheckAppStatus(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	repoDir := filepath.Join(tmpDir, "dotfiles")

	// 1. Synced file
	srcSynced := filepath.Join(homeDir, "synced.conf")
	destSynced := filepath.Join(repoDir, "configs", "synced.conf")
	os.MkdirAll(filepath.Dir(srcSynced), 0755)
	os.MkdirAll(filepath.Dir(destSynced), 0755)
	os.WriteFile(srcSynced, []byte("val=1\n"), 0644)
	os.WriteFile(destSynced, []byte("val=1\n"), 0644)

	// 2. Modified file
	srcMod := filepath.Join(homeDir, "mod.conf")
	destMod := filepath.Join(repoDir, "configs", "mod.conf")
	os.WriteFile(srcMod, []byte("val=2\n"), 0644)
	os.WriteFile(destMod, []byte("val=old\n"), 0644)

	app := manifest.AppConfig{
		Paths: []manifest.PathMapping{
			{Src: srcSynced, Dest: "configs/synced.conf"},
			{Src: srcMod, Dest: "configs/mod.conf"},
		},
	}

	res := CheckAppStatus("testapp", app, repoDir)
	if len(res.Paths) != 2 {
		t.Fatalf("expected 2 path results, got %d", len(res.Paths))
	}

	if res.Paths[0].State != StateSynced {
		t.Errorf("expected StateSynced for synced.conf, got %s", res.Paths[0].State)
	}
	if res.Paths[1].State != StateModified {
		t.Errorf("expected StateModified for mod.conf, got %s", res.Paths[1].State)
	}
}

func TestAddAppPath(t *testing.T) {
	m := manifest.DefaultManifest()
	err := AddAppPath(m, "custom-tool", "~/.config/custom/config.json", "configs/custom/config.json")
	if err != nil {
		t.Fatalf("unexpected error adding app: %v", err)
	}

	app, ok := m.Apps["custom-tool"]
	if !ok {
		t.Fatal("expected custom-tool to be in manifest")
	}
	if len(app.Paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(app.Paths))
	}
	if app.Paths[0].Src != "~/.config/custom/config.json" {
		t.Errorf("expected src to match, got %s", app.Paths[0].Src)
	}
}

func TestStatusTreatsHomePlaceholderAsSynced(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repoDir := t.TempDir()

	local := filepath.Join(home, ".tool", "settings.json")
	repoFile := filepath.Join(repoDir, "configs", "settings.json")
	os.MkdirAll(filepath.Dir(local), 0755)
	os.MkdirAll(filepath.Dir(repoFile), 0755)
	os.WriteFile(local, []byte(`{"hook":"`+home+`/x.sh"}`), 0644)
	os.WriteFile(repoFile, []byte(`{"hook":"__HOME__/x.sh"}`), 0644)

	app := manifest.AppConfig{Paths: []manifest.PathMapping{{Src: local, Dest: "configs/settings.json"}}}
	st := CheckAppStatus("tool", app, repoDir)
	if len(st.Paths) != 1 || st.Paths[0].State != StateSynced {
		t.Errorf("expected synced, got %+v", st.Paths)
	}
}

func TestStatusIgnoresBrewPrefix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	prefix := filepath.Join(t.TempDir(), "linuxbrew")
	os.MkdirAll(filepath.Join(prefix, "bin"), 0755)
	os.WriteFile(filepath.Join(prefix, "bin", "brew"), nil, 0755)
	t.Setenv("HOMEBREW_PREFIX", prefix)
	repoDir := t.TempDir()

	local := filepath.Join(home, ".tool", "config.toml")
	repoFile := filepath.Join(repoDir, "configs", "config.toml")
	os.MkdirAll(filepath.Dir(local), 0755)
	os.MkdirAll(filepath.Dir(repoFile), 0755)
	os.WriteFile(local, []byte(`command = "`+prefix+`/bin/engram"`), 0644)
	os.WriteFile(repoFile, []byte(`command = "__BREW_PREFIX__/bin/engram"`), 0644)

	app := manifest.AppConfig{Paths: []manifest.PathMapping{{Src: local, Dest: "configs/config.toml"}}}
	st := CheckAppStatus("tool", app, repoDir)
	if len(st.Paths) != 1 || st.Paths[0].State != StateSynced {
		t.Errorf("expected synced, got %+v", st.Paths)
	}
}
