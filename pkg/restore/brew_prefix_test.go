package restore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

// brewPrefixAt makes dir a verified Homebrew prefix for this test.
func brewPrefixAt(t *testing.T, dir string) string {
	t.Helper()
	mustWrite(t, filepath.Join(dir, "bin", "brew"), "#!/bin/sh\n")
	t.Setenv("HOMEBREW_PREFIX", dir)
	return dir
}

func brewApp() manifest.AppConfig {
	return manifest.AppConfig{Paths: []manifest.PathMapping{
		{Src: "~/.tool/config.toml", Dest: "configs/tool/config.toml"},
		{Src: "~/.tool/init.lua", Dest: "configs/tool/init.lua"},
	}}
}

const brewConfig = `command = "__BREW_PREFIX__/bin/engram"` + "\n"

func TestApplyFilesExpandsBrewPrefix(t *testing.T) {
	root, home := isolatedHome(t)
	prefix := brewPrefixAt(t, filepath.Join(root, "linuxbrew"))
	repo := filepath.Join(root, "repo")
	mustWrite(t, filepath.Join(repo, "configs", "tool", "config.toml"), brewConfig)
	mustWrite(t, filepath.Join(repo, "configs", "tool", "init.lua.template"), `node = "__BREW_PREFIX__/bin/node"`)

	res, err := ApplyAppFilesWith("tool", brewApp(), repo, ApplyOptions{Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", res.Warnings)
	}
	if got, want := readOrEmpty(filepath.Join(home, ".tool", "config.toml")), `command = "`+prefix+`/bin/engram"`+"\n"; got != want {
		t.Errorf("config.toml = %q, want %q", got, want)
	}
	if got, want := readOrEmpty(filepath.Join(home, ".tool", "init.lua")), `node = "`+prefix+`/bin/node"`; got != want {
		t.Errorf("template = %q, want %q", got, want)
	}

	// A second run finds the deployed file equivalent and leaves it alone.
	res, err = ApplyAppFilesWith("tool", brewApp(), repo, ApplyOptions{Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.LocalBackups) != 0 || len(res.Warnings) != 0 {
		t.Errorf("equivalent file must not be replaced: %+v", res)
	}
}

func TestApplyFilesWarnsWhenBrewPrefixIsUnknown(t *testing.T) {
	root, home := isolatedHome(t)
	t.Setenv("HOMEBREW_PREFIX", filepath.Join(root, "no-brew"))
	repo := filepath.Join(root, "repo")
	mustWrite(t, filepath.Join(repo, "configs", "tool", "config.toml"), brewConfig)
	mustWrite(t, filepath.Join(repo, "configs", "tool", "init.lua.template"), `node = "__BREW_PREFIX__/bin/node"`)

	res, err := ApplyAppFilesWith("tool", brewApp(), repo, ApplyOptions{Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(home, ".tool", "config.toml")
	if got := readOrEmpty(dest); got != brewConfig {
		t.Errorf("the token must be kept without a verified prefix, got %q", got)
	}
	want := []string{
		dest + ": no Homebrew prefix verified on this machine; __BREW_PREFIX__ left unexpanded",
		filepath.Join(home, ".tool", "init.lua") + ": no Homebrew prefix verified on this machine; __BREW_PREFIX__ left unexpanded",
	}
	if strings.Join(res.Warnings, "\n") != strings.Join(want, "\n") {
		t.Errorf("warnings = %q, want %q", res.Warnings, want)
	}
	if len(res.Restored) != 1 || len(res.TemplatesInit) != 1 {
		t.Errorf("files must still be deployed: %+v", res)
	}
}

func TestApplyFilesDryRunWarnsWhenBrewPrefixIsUnknown(t *testing.T) {
	root, home := isolatedHome(t)
	t.Setenv("HOMEBREW_PREFIX", filepath.Join(root, "no-brew"))
	repo := filepath.Join(root, "repo")
	mustWrite(t, filepath.Join(repo, "configs", "tool", "config.toml"), brewConfig)

	res, err := ApplyAppFilesWith("tool", brewApp(), repo, ApplyOptions{DryRun: true, Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "__BREW_PREFIX__ left unexpanded") {
		t.Errorf("warnings = %q", res.Warnings)
	}
	if _, err := os.Lstat(filepath.Join(home, ".tool")); !os.IsNotExist(err) {
		t.Errorf("dry run must write nothing: %v", err)
	}
}
