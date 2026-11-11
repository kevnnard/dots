package manifest

import "testing"

// A local manifest copied by an older dots lacks install recipes the binary
// ships. Effective adds those missing recipes, keeps every definition the
// local manifest already has, and keeps apps only the user defined.
func TestEffectiveAddsMissingRecipesAndKeepsLocalDefinitions(t *testing.T) {
	local := &Manifest{Version: "1.0.0", Apps: map[string]AppConfig{
		"zed": {Install: map[string]InstallRecipe{
			"darwin": {Manager: "brew", Cask: "zed"},
		}},
		"mytool": {Install: map[string]InstallRecipe{
			"default": {Manager: "script", Command: "true"},
		}},
	}}

	eff, extended := Effective(local)

	zed := eff.Apps["zed"]
	if _, ok := zed.Install["pacman"]; !ok {
		t.Fatalf("zed should gain the shipped pacman recipe, got %v", zed.Install)
	}
	if zed.Install["darwin"].Cask != "zed" {
		t.Fatalf("local darwin recipe must be kept, got %v", zed.Install["darwin"])
	}
	if _, ok := eff.Apps["mytool"]; !ok {
		t.Fatal("local-only app mytool must be kept")
	}
	if len(extended) != 1 || extended[0] != "zed" {
		t.Fatalf("extended = %v, want [zed]", extended)
	}
}

func TestEffectiveNeverOverridesLocalPackageNames(t *testing.T) {
	local := &Manifest{Apps: map[string]AppConfig{
		"zsh": {Install: map[string]InstallRecipe{
			"pacman": {Manager: "pacman", Package: "my-zsh-build"},
		}},
	}}
	eff, _ := Effective(local)
	if got := eff.Apps["zsh"].Install["pacman"].Package; got != "my-zsh-build" {
		t.Fatalf("local package overridden: %q", got)
	}
}

// A local manifest written before keep_local existed still keeps the shipped
// machine-specific files on restore, without dropping its own mappings.
func TestEffectiveAddsShippedKeepLocal(t *testing.T) {
	local := &Manifest{Apps: map[string]AppConfig{
		"git": {Paths: []PathMapping{
			{Src: "~/.gitconfig", Dest: "configs/git/.gitconfig"},
			{Src: "~/.my-git-extra", Dest: "configs/git/extra"},
		}},
	}}

	eff, _ := Effective(local)

	paths := eff.Apps["git"].Paths
	if len(paths) != 2 || !paths[0].KeepLocal || paths[1].KeepLocal {
		t.Fatalf("paths = %+v", paths)
	}
	if local.Apps["git"].Paths[0].KeepLocal {
		t.Error("the local manifest must not be modified")
	}
}

func TestShippedMachineSpecificFilesAreKeepLocal(t *testing.T) {
	want := map[string]bool{
		"git ~/.gitconfig": true,
		"gentle-shell ~/.gentle-shell/agent/subagents.json": true,
	}
	m := DefaultManifest()
	for name, app := range m.Apps {
		for _, p := range app.Paths {
			key := name + " " + p.Src
			if p.KeepLocal != want[key] {
				t.Errorf("%s keep_local = %v", key, p.KeepLocal)
			}
			delete(want, key)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing mappings: %v", want)
	}
}
