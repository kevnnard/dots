package restore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

func omarchy() *detector.SystemInfo {
	return &detector.SystemInfo{
		OS:             "linux",
		Distro:         detector.DistroArch,
		PrimaryManager: detector.ManagerYay,
		Managers:       []detector.PackageManager{detector.ManagerYay, detector.ManagerPacman},
	}
}

func TestRecipeResolutionOrder(t *testing.T) {
	cases := []struct {
		name     string
		recipes  map[string]manifest.InstallRecipe
		sys      *detector.SystemInfo
		expected string
		found    bool
	}{
		{
			name: "primary manager key beats os script",
			recipes: map[string]manifest.InstallRecipe{
				"linux": {Manager: "script", Command: "curl -fsSL https://x | sh"},
				"yay":   {Manager: "yay", Package: "neovim"},
			},
			sys:      omarchy(),
			expected: "yay -S --noconfirm neovim",
			found:    true,
		},
		{
			name: "secondary available manager used when primary has no recipe",
			recipes: map[string]manifest.InstallRecipe{
				"linux":  {Manager: "script", Command: "curl -fsSL https://x | sh"},
				"pacman": {Manager: "pacman", Package: "neovim"},
			},
			sys:      omarchy(),
			expected: "sudo pacman -S --noconfirm neovim",
			found:    true,
		},
		{
			name: "exact distro key beats manager key",
			recipes: map[string]manifest.InstallRecipe{
				"arch": {Manager: "pacman", Package: "from-distro"},
				"yay":  {Manager: "yay", Package: "from-manager"},
			},
			sys:      omarchy(),
			expected: "sudo pacman -S --noconfirm from-distro",
			found:    true,
		},
		{
			name: "unavailable manager key is ignored",
			recipes: map[string]manifest.InstallRecipe{
				"apt":   {Manager: "apt", Package: "neovim"},
				"linux": {Manager: "script", Command: "echo fallback"},
			},
			sys:      omarchy(),
			expected: "echo fallback",
			found:    true,
		},
		{
			name: "default key is the last resort",
			recipes: map[string]manifest.InstallRecipe{
				"darwin":  {Manager: "brew", Package: "x"},
				"default": {Manager: "script", Command: "echo any-os"},
			},
			sys:      omarchy(),
			expected: "echo any-os",
			found:    true,
		},
		{
			name: "no matching recipe",
			recipes: map[string]manifest.InstallRecipe{
				"darwin": {Manager: "brew", Package: "x"},
			},
			sys:   omarchy(),
			found: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := manifest.AppConfig{Install: tc.recipes}
			plan := PlanApp("app", app, tc.sys)
			if plan.RecipeFound != tc.found {
				t.Fatalf("RecipeFound = %v, want %v", plan.RecipeFound, tc.found)
			}
			if plan.InstallCmd != tc.expected {
				t.Errorf("InstallCmd = %q, want %q", plan.InstallCmd, tc.expected)
			}
		})
	}
}

func TestBuildInstallCommandManagers(t *testing.T) {
	cases := []struct {
		recipe   manifest.InstallRecipe
		expected string
	}{
		{manifest.InstallRecipe{Manager: "zypper", Package: "git"}, "sudo zypper install -y git"},
		{manifest.InstallRecipe{Manager: "winget", Package: "Git.Git"}, "winget install --id Git.Git -e --accept-source-agreements --accept-package-agreements"},
		{manifest.InstallRecipe{Manager: "scoop", Package: "git"}, "scoop install git"},
		{manifest.InstallRecipe{Manager: "choco", Package: "git"}, "choco install -y git"},
		{manifest.InstallRecipe{Manager: "cargo", Package: "ripgrep"}, "cargo install ripgrep"},
	}
	for _, tc := range cases {
		t.Run(tc.recipe.Manager, func(t *testing.T) {
			if got := buildInstallCommand(tc.recipe, nil); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestShellInvocation(t *testing.T) {
	name, args := shellInvocation("windows", "echo hi")
	if name != "powershell" || len(args) < 3 || args[len(args)-1] != "echo hi" {
		t.Errorf("windows: got %s %v", name, args)
	}
	name, args = shellInvocation("linux", "echo hi")
	if name != "sh" || len(args) != 2 || args[0] != "-c" || args[1] != "echo hi" {
		t.Errorf("linux: got %s %v", name, args)
	}
}

func planNames(plans []*AppPlan) []string {
	var out []string
	for _, p := range plans {
		out = append(out, p.AppName)
	}
	return out
}

func indexOf(list []string, name string) int {
	for i, n := range list {
		if n == name {
			return i
		}
	}
	return -1
}

func TestPlanAllOrdersDependenciesFirst(t *testing.T) {
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"codex": {Requires: []string{"pnpm"}},
		"pnpm":  {Requires: []string{"node"}},
		"node":  {},
		"aaa":   {},
		"zzz":   {Requires: []string{"ghost"}}, // unknown dependency is ignored
	}}
	names := planNames(PlanAll(m, omarchy()))
	if len(names) != 5 {
		t.Fatalf("expected 5 plans, got %v", names)
	}
	if !(indexOf(names, "node") < indexOf(names, "pnpm") && indexOf(names, "pnpm") < indexOf(names, "codex")) {
		t.Errorf("dependencies must come first: %v", names)
	}
	again := planNames(PlanAll(m, omarchy()))
	for i := range names {
		if names[i] != again[i] {
			t.Fatalf("order is not deterministic: %v vs %v", names, again)
		}
	}
}

func TestPlanAllSurvivesDependencyCycle(t *testing.T) {
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"a": {Requires: []string{"b"}},
		"b": {Requires: []string{"a"}},
	}}
	if got := planNames(PlanAll(m, omarchy())); len(got) != 2 {
		t.Errorf("expected both apps planned, got %v", got)
	}
}

func TestNpmRecipeAvoidsRootGlobalInstall(t *testing.T) {
	recipe := manifest.InstallRecipe{Manager: "npm", Package: "@scope/tool"}

	linux := &detector.SystemInfo{OS: "linux"}
	if got := buildInstallCommand(recipe, linux); got != `npm install -g --prefix "$HOME/.local" @scope/tool` {
		t.Errorf("linux: got %q", got)
	}

	darwin := &detector.SystemInfo{OS: "darwin"}
	if got := buildInstallCommand(recipe, darwin); got != `npm install -g --prefix "$HOME/.local" @scope/tool` {
		t.Errorf("darwin: got %q", got)
	}

	windows := &detector.SystemInfo{OS: "windows"}
	if got := buildInstallCommand(recipe, windows); got != "npm install -g @scope/tool" {
		t.Errorf("windows: got %q", got)
	}

	withPnpm := &detector.SystemInfo{OS: "linux", Managers: []detector.PackageManager{detector.ManagerPnpm}}
	if got := buildInstallCommand(recipe, withPnpm); got != "pnpm add -g @scope/tool" {
		t.Errorf("pnpm preferred: got %q", got)
	}
}

func TestRestoreExpandsHomePlaceholder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	repo := t.TempDir()
	repoFile := filepath.Join(repo, "configs/tool/settings.json")
	if err := os.MkdirAll(filepath.Dir(repoFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repoFile, []byte(`{"hook":"__HOME__/.tool/hook.sh"}`), 0644); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(home, ".tool", "settings.json")
	app := manifest.AppConfig{Paths: []manifest.PathMapping{{Src: target, Dest: "configs/tool/settings.json"}}}
	if _, err := ApplyAppFiles("tool", app, repo, false); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"hook":"` + home + `/.tool/hook.sh"}`
	if string(data) != want {
		t.Errorf("got %q, want %q", data, want)
	}

	// Restoring again must not create a safety backup: contents are equivalent.
	res, err := ApplyAppFiles("tool", app, repo, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.LocalBackups) != 0 {
		t.Errorf("expected no safety backup for identical content, got %v", res.LocalBackups)
	}
}
