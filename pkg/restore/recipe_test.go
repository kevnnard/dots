package restore

import (
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
