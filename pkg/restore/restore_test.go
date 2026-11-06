package restore

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

func TestPlanRestore(t *testing.T) {
	app := manifest.AppConfig{
		Description: "Neovim",
		Paths: []manifest.PathMapping{
			{Src: "~/.config/nvim", Dest: "configs/nvim"},
		},
		Install: map[string]manifest.InstallRecipe{
			"darwin": {Manager: "brew", Package: "neovim"},
			"debian": {Manager: "apt", Package: "neovim"},
		},
	}

	sysDarwin := &detector.SystemInfo{
		OS:             "darwin",
		Distro:         detector.DistroDarwin,
		PrimaryManager: detector.ManagerBrew,
	}

	plan := PlanApp("nvim", app, sysDarwin)
	if plan.InstallCmd == "" {
		t.Error("expected install command for darwin")
	}
	if plan.InstallCmd != "brew install neovim" {
		t.Errorf("expected 'brew install neovim', got %s", plan.InstallCmd)
	}

	sysDebian := &detector.SystemInfo{
		OS:             "linux",
		Distro:         detector.DistroDebian,
		PrimaryManager: detector.ManagerApt,
	}

	planDeb := PlanApp("nvim", app, sysDebian)
	if planDeb.InstallCmd != "sudo apt-get install -y neovim" {
		t.Errorf("expected 'sudo apt-get install -y neovim', got %s", planDeb.InstallCmd)
	}
}

func TestApplyFiles(t *testing.T) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "dotfiles")
	targetHome := filepath.Join(tmpDir, "home")

	// Setup mock repo files
	nvimRepo := filepath.Join(repoDir, "configs", "nvim")
	os.MkdirAll(nvimRepo, 0755)
	os.WriteFile(filepath.Join(nvimRepo, "init.lua"), []byte("print('restored')\n"), 0644)

	app := manifest.AppConfig{
		Paths: []manifest.PathMapping{
			{Src: filepath.Join(targetHome, ".config", "nvim"), Dest: "configs/nvim"},
		},
	}

	res, err := ApplyAppFiles("nvim", app, repoDir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Restored) != 1 {
		t.Fatalf("expected 1 restored file, got %d", len(res.Restored))
	}

	targetFile := filepath.Join(targetHome, ".config", "nvim", "init.lua")
	data, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("failed reading restored file: %v", err)
	}
	if string(data) != "print('restored')\n" {
		t.Errorf("expected 'print('restored')\\n', got %s", string(data))
	}
}

func TestApplyFilesCreatesSafetyBackup(t *testing.T) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "dotfiles")
	targetHome := filepath.Join(tmpDir, "home")

	// 1. Setup existing local file with old content
	targetFile := filepath.Join(targetHome, ".zshrc")
	os.MkdirAll(filepath.Dir(targetFile), 0755)
	os.WriteFile(targetFile, []byte("echo 'my old local zsh'\n"), 0644)

	// 2. Setup repo file with new content
	repoFile := filepath.Join(repoDir, "configs", "zsh", ".zshrc")
	os.MkdirAll(filepath.Dir(repoFile), 0755)
	os.WriteFile(repoFile, []byte("echo 'restored from repo'\n"), 0644)

	app := manifest.AppConfig{
		Paths: []manifest.PathMapping{
			{Src: targetFile, Dest: "configs/zsh/.zshrc"},
		},
	}

	res, err := ApplyAppFiles("zsh", app, repoDir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.LocalBackups) != 1 {
		t.Fatalf("expected 1 local backup created, got %d", len(res.LocalBackups))
	}

	backupCreated := res.LocalBackups[0]
	data, err := os.ReadFile(backupCreated)
	if err != nil {
		t.Fatalf("failed reading backup file %s: %v", backupCreated, err)
	}
	if string(data) != "echo 'my old local zsh'\n" {
		t.Errorf("expected old content preserved in backup, got: %s", string(data))
	}
}

func TestApplyFilesNeverReplacesWithoutSafetyBackup(t *testing.T) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "dotfiles")
	homeDir := filepath.Join(tmpDir, "home")
	targetFile := filepath.Join(homeDir, ".zshrc")
	os.MkdirAll(homeDir, 0755)
	os.WriteFile(targetFile, []byte("mine\n"), 0644)
	repoFile := filepath.Join(repoDir, "configs", "zsh", ".zshrc")
	os.MkdirAll(filepath.Dir(repoFile), 0755)
	os.WriteFile(repoFile, []byte("repo\n"), 0644)
	// The target stays writable, but no backup can be created beside it.
	if err := os.Chmod(homeDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(homeDir, 0755)

	app := manifest.AppConfig{Paths: []manifest.PathMapping{{Src: targetFile, Dest: "configs/zsh/.zshrc"}}}
	res, err := ApplyAppFiles("zsh", app, repoDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(targetFile); string(data) != "mine\n" {
		t.Errorf("file replaced without a safety backup: %q", data)
	}
	if len(res.Restored) != 0 || len(res.Warnings) != 1 {
		t.Errorf("restored=%v warnings=%v", res.Restored, res.Warnings)
	}
}

func TestApplyFilesPreservesExistingFilesUnlessReplaceAuthorized(t *testing.T) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "dotfiles")
	target := filepath.Join(tmpDir, "home", ".config", "nvim")
	os.MkdirAll(filepath.Join(repoDir, "configs", "nvim"), 0755)
	os.WriteFile(filepath.Join(repoDir, "configs", "nvim", "init.lua"), []byte("repo\n"), 0644)
	os.WriteFile(filepath.Join(repoDir, "configs", "nvim", "new.lua"), []byte("new\n"), 0644)
	os.Symlink("init.lua", filepath.Join(repoDir, "configs", "nvim", "link.lua"))
	os.MkdirAll(target, 0755)
	os.WriteFile(filepath.Join(target, "init.lua"), []byte("mine\n"), 0644)
	os.WriteFile(filepath.Join(target, "link.lua"), []byte("my real file\n"), 0644)

	app := manifest.AppConfig{Paths: []manifest.PathMapping{{Src: target, Dest: "configs/nvim"}}}
	res, err := ApplyAppFilesWith("nvim", app, repoDir, ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(target, "init.lua")); string(data) != "mine\n" {
		t.Errorf("init.lua overwritten: %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(target, "link.lua")); string(data) != "my real file\n" {
		t.Errorf("regular file replaced by symlink without authorization: %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(target, "new.lua")); string(data) != "new\n" {
		t.Errorf("missing file must still be deployed: %q", data)
	}
	if len(res.Kept) != 2 || len(res.LocalBackups) != 0 {
		t.Errorf("kept=%v backups=%v", res.Kept, res.LocalBackups)
	}

	res, err = ApplyAppFilesWith("nvim", app, repoDir, ApplyOptions{Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.LocalBackups) != 2 || len(res.Kept) != 0 {
		t.Fatalf("kept=%v backups=%v warnings=%v", res.Kept, res.LocalBackups, res.Warnings)
	}
	if link, err := os.Readlink(filepath.Join(target, "link.lua")); err != nil || link != "init.lua" {
		t.Errorf("link.lua = %q %v", link, err)
	}
}

func TestCheckedInZshrcHasNoUnguardedBrewSources(t *testing.T) {
	path := filepath.Join("..", "..", "configs", "zsh", ".zshrc")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "BREW_BIN") {
		t.Error(".zshrc still sources plugins through the undefined $BREW_BIN")
	}
	// Arch's AUR package zsh-theme-powerlevel10k installs here.
	if !strings.Contains(text, "/usr/share/zsh-theme-powerlevel10k/powerlevel10k.zsh-theme") {
		t.Error(".zshrc misses the AUR powerlevel10k theme path")
	}
	if zsh, err := exec.LookPath("zsh"); err == nil {
		if out, err := exec.Command(zsh, "-n", path).CombinedOutput(); err != nil {
			t.Errorf("zsh -n %s: %v\n%s", path, err, out)
		}
	}
}

// isolatedHome points HOME at a fresh temporary directory and returns the
// scratch root that also holds paths outside that home.
func isolatedHome(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return root, home
}

func mustWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

func readOrEmpty(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestApplyFilesBacksUpExistingSymlinkAsLinkBeforeReplacing(t *testing.T) {
	root, home := isolatedHome(t)
	repo := filepath.Join(root, "repo")
	mustWrite(t, filepath.Join(repo, "configs", "nvim", "init.lua"), "repo\n")
	if err := os.Symlink("init.lua", filepath.Join(repo, "configs", "nvim", "link.lua")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".config", "nvim")
	mustWrite(t, filepath.Join(target, "old.lua"), "old target\n")
	if err := os.Symlink("old.lua", filepath.Join(target, "link.lua")); err != nil {
		t.Fatal(err)
	}

	app := manifest.AppConfig{Paths: []manifest.PathMapping{{Src: "~/.config/nvim", Dest: "configs/nvim"}}}
	res, err := ApplyAppFilesWith("nvim", app, repo, ApplyOptions{Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 0 || len(res.LocalBackups) != 1 {
		t.Fatalf("backups=%v warnings=%v", res.LocalBackups, res.Warnings)
	}
	bk := res.LocalBackups[0]
	if link, err := os.Readlink(bk); err != nil || link != "old.lua" {
		t.Errorf("backup must preserve the old link itself, got %q %v", link, err)
	}
	if link, err := os.Readlink(filepath.Join(target, "link.lua")); err != nil || link != "init.lua" {
		t.Errorf("link.lua = %q %v", link, err)
	}
	if got := readOrEmpty(filepath.Join(target, "old.lua")); got != "old target\n" {
		t.Errorf("old link target changed: %q", got)
	}
}

func TestApplyFilesKeepsExistingSymlinkWhenLinkBackupFails(t *testing.T) {
	root, home := isolatedHome(t)
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "configs", "nvim"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("init.lua", filepath.Join(repo, "configs", "nvim", "link.lua")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".config", "nvim")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("old.lua", filepath.Join(target, "link.lua")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(target, 0755) })

	app := manifest.AppConfig{Paths: []manifest.PathMapping{{Src: "~/.config/nvim", Dest: "configs/nvim"}}}
	res, err := ApplyAppFilesWith("nvim", app, repo, ApplyOptions{Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if link, err := os.Readlink(filepath.Join(target, "link.lua")); err != nil || link != "old.lua" {
		t.Errorf("link replaced although its backup failed: %q %v", link, err)
	}
	if len(res.Warnings) != 1 || len(res.Restored) != 0 || len(res.LocalBackups) != 0 {
		t.Errorf("restored=%v backups=%v warnings=%v", res.Restored, res.LocalBackups, res.Warnings)
	}
	if names := listDir(t, target); len(names) != 1 {
		t.Errorf("unexpected entries beside the link: %v", names)
	}
}

func TestApplyFilesRefusesParentSymlinkEscapingHome(t *testing.T) {
	root, home := isolatedHome(t)
	repo := filepath.Join(root, "repo")
	outside := filepath.Join(root, "outside")
	mustWrite(t, filepath.Join(repo, "configs", "nvim", "init.lua"), "repo\n")
	mustWrite(t, filepath.Join(repo, "configs", "nvim", "lua", "x.lua"), "x\n")
	mustWrite(t, filepath.Join(repo, "configs", "app", "conf"), "conf\n")
	mustWrite(t, filepath.Join(repo, "configs", "app", "tpl.template"), "tpl\n")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, ".config")); err != nil {
		t.Fatal(err)
	}

	app := manifest.AppConfig{Paths: []manifest.PathMapping{
		{Src: "~/.config/nvim", Dest: "configs/nvim"},
		{Src: "~/.config/app/conf", Dest: "configs/app/conf"},
		{Src: "~/.config/app/tpl", Dest: "configs/app/tpl"},
	}}
	for _, replace := range []bool{false, true} {
		res, err := ApplyAppFilesWith("nvim", app, repo, ApplyOptions{Replace: replace})
		if err != nil {
			t.Fatal(err)
		}
		if names := listDir(t, outside); len(names) != 0 {
			t.Errorf("replace=%v wrote outside home: %v", replace, names)
		}
		if len(res.Restored)+len(res.Kept)+len(res.LocalBackups)+len(res.TemplatesInit) != 0 {
			t.Errorf("replace=%v rejected paths must not be counted: %+v", replace, res)
		}
		if len(res.Warnings) != 3 {
			t.Errorf("replace=%v warnings=%v", replace, res.Warnings)
		}
	}
}

func TestApplyFilesRefusesNestedSymlinkEscapingHome(t *testing.T) {
	root, home := isolatedHome(t)
	repo := filepath.Join(root, "repo")
	outside := filepath.Join(root, "outside")
	mustWrite(t, filepath.Join(repo, "configs", "nvim", "init.lua"), "repo\n")
	mustWrite(t, filepath.Join(repo, "configs", "nvim", "lua", "deep", "x.lua"), "x\n")
	target := filepath.Join(home, ".config", "nvim")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(target, "lua")); err != nil {
		t.Fatal(err)
	}

	app := manifest.AppConfig{Paths: []manifest.PathMapping{{Src: "~/.config/nvim", Dest: "configs/nvim"}}}
	res, err := ApplyAppFilesWith("nvim", app, repo, ApplyOptions{Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if names := listDir(t, outside); len(names) != 0 {
		t.Errorf("wrote through a nested symlink outside home: %v", names)
	}
	if got := readOrEmpty(filepath.Join(target, "init.lua")); got != "repo\n" {
		t.Errorf("confined sibling must still deploy: %q", got)
	}
	if len(res.Restored) != 1 || len(res.Warnings) == 0 {
		t.Errorf("restored=%v warnings=%v", res.Restored, res.Warnings)
	}
}

func TestApplyFilesNeverWritesThroughDestinationSymlink(t *testing.T) {
	root, home := isolatedHome(t)
	repo := filepath.Join(root, "repo")
	external := filepath.Join(root, "outside", "zshrc")
	mustWrite(t, filepath.Join(repo, "configs", "zsh", ".zshrc"), "repo\n")
	mustWrite(t, external, "external\n")
	dest := filepath.Join(home, ".zshrc")
	if err := os.Symlink(external, dest); err != nil {
		t.Fatal(err)
	}
	app := manifest.AppConfig{Paths: []manifest.PathMapping{{Src: "~/.zshrc", Dest: "configs/zsh/.zshrc"}}}

	res, err := ApplyAppFilesWith("zsh", app, repo, ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Kept) != 1 || readOrEmpty(external) != "external\n" {
		t.Fatalf("kept=%v external=%q", res.Kept, readOrEmpty(external))
	}

	res, err = ApplyAppFilesWith("zsh", app, repo, ApplyOptions{Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := readOrEmpty(external); got != "external\n" {
		t.Errorf("external symlink target was overwritten: %q", got)
	}
	if len(res.Warnings) != 0 || len(res.LocalBackups) != 1 {
		t.Fatalf("backups=%v warnings=%v", res.LocalBackups, res.Warnings)
	}
	if link, err := os.Readlink(res.LocalBackups[0]); err != nil || link != external {
		t.Errorf("backup must keep the old link: %q %v", link, err)
	}
	if info, err := os.Lstat(dest); err != nil || !info.Mode().IsRegular() || readOrEmpty(dest) != "repo\n" {
		t.Errorf("dest must become the repository file: %v %v %q", info, err, readOrEmpty(dest))
	}
}

func TestApplyFilesLeavesEquivalentReadOnlyFileUntouched(t *testing.T) {
	root, home := isolatedHome(t)
	repo := filepath.Join(root, "repo")
	mustWrite(t, filepath.Join(repo, "configs", "tool", "settings.json"), `{"hook":"__HOME__/.tool/hook.sh"}`)
	dest := filepath.Join(home, ".tool", "settings.json")
	mustWrite(t, dest, `{"hook":"`+home+`/.tool/hook.sh"}`)
	if err := os.Chmod(dest, 0444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dest, 0644) })
	before, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	app := manifest.AppConfig{Paths: []manifest.PathMapping{{Src: "~/.tool/settings.json", Dest: "configs/tool/settings.json"}}}

	for _, replace := range []bool{false, true} {
		res, err := ApplyAppFilesWith("tool", app, repo, ApplyOptions{Replace: replace})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Warnings) != 0 || len(res.LocalBackups) != 0 || len(res.Kept) != 0 || len(res.Restored) != 1 {
			t.Errorf("replace=%v result=%+v", replace, res)
		}
		after, err := os.Stat(dest)
		if err != nil {
			t.Fatal(err)
		}
		if !after.ModTime().Equal(before.ModTime()) || after.Mode() != before.Mode() {
			t.Errorf("replace=%v equivalent file was rewritten", replace)
		}
	}
	if names := listDir(t, filepath.Dir(dest)); len(names) != 1 {
		t.Errorf("unexpected entries: %v", names)
	}
}

func TestApplyFilesNeverClobbersExistingBackup(t *testing.T) {
	root, home := isolatedHome(t)
	repo := filepath.Join(root, "repo")
	mustWrite(t, filepath.Join(repo, "configs", "zsh", ".zshrc"), "repo\n")
	dest := filepath.Join(home, ".zshrc")
	mustWrite(t, dest, "mine\n")
	now := time.Now()
	var taken []string
	for i := 0; i < 3; i++ {
		p := dest + "." + now.Add(time.Duration(i)*time.Second).Format("20060102_150405") + ".bk"
		mustWrite(t, p, "older backup\n")
		taken = append(taken, p)
	}
	app := manifest.AppConfig{Paths: []manifest.PathMapping{{Src: "~/.zshrc", Dest: "configs/zsh/.zshrc"}}}

	res, err := ApplyAppFilesWith("zsh", app, repo, ApplyOptions{Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range taken {
		if got := readOrEmpty(p); got != "older backup\n" {
			t.Errorf("existing backup %s clobbered: %q", p, got)
		}
	}
	if len(res.LocalBackups) != 1 || readOrEmpty(res.LocalBackups[0]) != "mine\n" {
		t.Fatalf("backups=%v warnings=%v", res.LocalBackups, res.Warnings)
	}
	if got := readOrEmpty(dest); got != "repo\n" {
		t.Errorf("dest = %q", got)
	}
}

// Regression: symlinks that stay inside home (stow-like layouts) still deploy.
func TestApplyFilesFollowsParentSymlinkInsideHome(t *testing.T) {
	root, home := isolatedHome(t)
	repo := filepath.Join(root, "repo")
	mustWrite(t, filepath.Join(repo, "configs", "nvim", "init.lua"), "repo\n")
	real := filepath.Join(home, "dotfiles", "config")
	if err := os.MkdirAll(real, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(home, ".config")); err != nil {
		t.Fatal(err)
	}
	app := manifest.AppConfig{Paths: []manifest.PathMapping{{Src: "~/.config/nvim", Dest: "configs/nvim"}}}
	res, err := ApplyAppFilesWith("nvim", app, repo, ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 0 || readOrEmpty(filepath.Join(real, "nvim", "init.lua")) != "repo\n" {
		t.Errorf("restored=%v warnings=%v", res.Restored, res.Warnings)
	}
}

// A backed-up symlink is recreated only when it points inside home: an
// absolute target or one that climbs out of home is refused with a warning.
func TestApplyFilesRefusesSymlinkTargetOutsideHome(t *testing.T) {
	root, home := isolatedHome(t)
	repo := filepath.Join(root, "repo")
	skills := filepath.Join(repo, "configs", "pi", "skills")
	mustWrite(t, filepath.Join(home, ".agents", "skills", "terminal-browser", "SKILL.md"), "skill\n")
	if err := os.MkdirAll(skills, 0755); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		"terminal-browser": "../../../.agents/skills/terminal-browser",
		"abs":              "/etc",
		"climb":            "../../../../../../etc",
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(skills, name)); err != nil {
			t.Fatal(err)
		}
	}
	app := manifest.AppConfig{Paths: []manifest.PathMapping{{Src: "~/.pi/agent/skills", Dest: "configs/pi/skills"}}}
	res, err := ApplyAppFilesWith("pi", app, repo, ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(home, ".pi", "agent", "skills")
	if link, err := os.Readlink(filepath.Join(dest, "terminal-browser")); err != nil || link != links["terminal-browser"] {
		t.Errorf("terminal-browser = %q %v, want the in-home link", link, err)
	}
	for _, name := range []string{"abs", "climb"} {
		if _, err := os.Lstat(filepath.Join(dest, name)); !os.IsNotExist(err) {
			t.Errorf("%s points outside home and must not be created", name)
		}
	}
	warned := strings.Join(res.Warnings, "\n")
	for _, name := range []string{"abs", "climb"} {
		if !strings.Contains(warned, filepath.Join(dest, name)+" not restored: link points outside") {
			t.Errorf("missing warning for %s:\n%s", name, warned)
		}
	}
}
