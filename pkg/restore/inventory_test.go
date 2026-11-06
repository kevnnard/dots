package restore

import (
	"bufio"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

var brewfileEntry = regexp.MustCompile(`^(brew|cask|npm)\s+"([^"]+)"`)

// declarations returns every brew/cask/npm entry of the repository
// Brewfiles keyed as "kind:name".
func declarations(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob("../../Brewfile*")
	if err != nil || len(files) == 0 {
		t.Fatalf("no Brewfiles found: %v", err)
	}
	out := map[string]bool{}
	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			if m := brewfileEntry.FindStringSubmatch(scanner.Text()); m != nil {
				out[m[1]+":"+m[2]] = true
			}
		}
		f.Close()
	}
	return out
}

// owners maps each declaration to the manifest apps whose recipes install it.
func owners(m *manifest.Manifest) map[string][]string {
	out := map[string][]string{}
	for name, app := range m.Apps {
		seen := map[string]bool{}
		for _, r := range app.Install {
			var key string
			switch {
			case r.Manager == "brew" && r.Cask != "":
				key = "cask:" + r.Cask
			case r.Manager == "brew" && r.Package != "":
				key = "brew:" + r.Package
			case r.Manager == "npm":
				key = "npm:" + r.Package
			}
			if key != "" && !seen[key] {
				seen[key] = true
				out[key] = append(out[key], name)
			}
		}
	}
	return out
}

func loadRepoManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Load("../../manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestEveryBrewfileDeclarationHasExactlyOneOwner(t *testing.T) {
	decls := declarations(t)
	if len(decls) != 62 {
		t.Errorf("expected 62 distinct Brewfile declarations, got %d", len(decls))
	}
	own := owners(loadRepoManifest(t))
	var keys []string
	for k := range decls {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if n := len(own[k]); n != 1 {
			t.Errorf("%s owned by %d apps %v, want exactly 1", k, n, own[k])
		}
	}
}

func TestRepositoryManifestGraphIsValid(t *testing.T) {
	m := loadRepoManifest(t)
	if err := ValidateGraph(m); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"fd", "bat", "ripgrep"} {
		if _, ok := m.Apps[name]; !ok {
			t.Errorf("config prerequisite %q missing from inventory", name)
		}
	}
	// Bundle apps must not reinstall packages that are owned individually.
	for _, name := range []string{"brew", "infra", "mobile"} {
		if len(m.Apps[name].Install) != 0 {
			t.Errorf("%s must not run a bundle install recipe", name)
		}
	}
}

func TestRepositoryConfigRestoreOrdersShellAndEditorHelpers(t *testing.T) {
	m := loadRepoManifest(t)
	darwin := &detector.SystemInfo{
		OS:             "darwin",
		PrimaryManager: detector.ManagerBrew,
		Managers:       []detector.PackageManager{detector.ManagerBrew},
	}
	for label, sys := range map[string]*detector.SystemInfo{"arch": plainArch(), "darwin": darwin} {
		t.Run(label, func(t *testing.T) {
			stock, err := BuildPlan(m, []string{"zsh", "nvim"}, sys, PlanOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range stock {
				if !s.Requested {
					t.Errorf("stock install pulled config dependency %s", s.AppName)
				}
			}
			steps, err := BuildPlan(m, []string{"zsh", "nvim"}, sys, PlanOptions{WithConfig: true})
			if err != nil {
				t.Fatal(err)
			}
			pos := map[string]int{}
			for i, s := range steps {
				pos[s.AppName] = i
			}
			for _, helper := range m.Apps["zsh"].ConfigRequires {
				if _, ok := pos[helper]; !ok {
					t.Errorf("zsh config dependency %s not planned", helper)
				}
				if m.Apps[helper].Requires == nil && pos[helper] > pos["zsh"] {
					t.Errorf("standalone helper %s planned after zsh", helper)
				}
			}
			for _, plugin := range []string{"powerlevel10k", "zsh-autosuggestions", "zsh-syntax-highlighting"} {
				if pos[plugin] < pos["zsh"] {
					t.Errorf("plugin %s planned before zsh", plugin)
				}
			}
			if pos["ripgrep"] > pos["nvim"] {
				t.Error("ripgrep must precede nvim when its config is restored")
			}
		})
	}
}

// Two apps sharing a probe entry would both report installed when only one
// of them is present.
func TestRepositoryInstallProbesAreUnambiguous(t *testing.T) {
	m := loadRepoManifest(t)
	byProbe := map[string][]string{}
	for name, app := range m.Apps {
		if len(app.Install) == 0 {
			continue
		}
		checks := app.Check
		if len(checks) == 0 {
			checks = []string{name}
		}
		for _, c := range checks {
			byProbe[c] = append(byProbe[c], name)
		}
	}
	for probe, apps := range byProbe {
		if len(apps) > 1 {
			sort.Strings(apps)
			t.Errorf("probe %q shared by %v", probe, apps)
		}
	}
}

// powerlevel10k ships no command, so the default PATH probe could never see
// it and every run would reinstall it. Its check must name the theme files
// the brew formula (every Homebrew prefix) and the AUR package install.
func TestPowerlevel10kProbeUsesInstalledThemeFiles(t *testing.T) {
	m := loadRepoManifest(t)
	want := []string{
		"/opt/homebrew/share/powerlevel10k/powerlevel10k.zsh-theme",
		"/usr/local/share/powerlevel10k/powerlevel10k.zsh-theme",
		"/home/linuxbrew/.linuxbrew/share/powerlevel10k/powerlevel10k.zsh-theme",
		"/usr/share/zsh-theme-powerlevel10k/powerlevel10k.zsh-theme",
	}
	app := m.Apps["powerlevel10k"]
	got := map[string]bool{}
	for _, c := range app.Check {
		got[c] = true
	}
	for _, p := range want {
		if !got[p] {
			t.Errorf("powerlevel10k check misses %s (got %v)", p, app.Check)
		}
	}
	if len(app.Check) != len(want) {
		t.Errorf("powerlevel10k check = %v, want only the theme files %v", app.Check, want)
	}
	if !reflect.DeepEqual(manifest.DefaultManifest().Apps["powerlevel10k"].Check, app.Check) {
		t.Error("the canonical default manifest must carry the same powerlevel10k check")
	}
}

// rootedProbe runs CommandProbe with absolute check entries moved under root,
// so fixtures can stand in for system install locations. "~" entries resolve
// against $HOME, which the caller points at a temporary directory.
func rootedProbe(root string) InstalledProbe {
	return func(name string, app manifest.AppConfig) bool {
		var checks []string
		for _, c := range app.Check {
			if strings.HasPrefix(c, "/") {
				c = filepath.Join(root, c)
			}
			checks = append(checks, c)
		}
		app.Check = checks
		return CommandProbe(name, app)
	}
}

func testSystem(osName string, distro detector.Distro, mgr detector.PackageManager) *detector.SystemInfo {
	return &detector.SystemInfo{OS: osName, Distro: distro, PrimaryManager: mgr, Managers: []detector.PackageManager{mgr}}
}

// Shell plugins and fonts ship no command, so without file checks the PATH
// probe falls back to a nonexistent command and every run reinstalls them.
// Each case is one supported target: the file its recipe installs there must
// count as installed, and its absence must plan that recipe.
func TestNonCommandPackagesProbeInstalledFiles(t *testing.T) {
	darwin := testSystem("darwin", detector.DistroDarwin, detector.ManagerBrew)
	linuxbrew := testSystem("linux", detector.DistroUnknown, detector.ManagerBrew)
	arch := plainArch()
	debian := testSystem("linux", detector.DistroDebian, detector.ManagerApt)
	ubuntu := testSystem("linux", detector.DistroUbuntu, detector.ManagerApt)
	fedora := testSystem("linux", detector.DistroFedora, detector.ManagerDnf)

	cases := []struct {
		app, platform string
		sys           *detector.SystemInfo
		file, manager string
	}{
		{"zsh-autosuggestions", "darwin", darwin, "/opt/homebrew/share/zsh-autosuggestions/zsh-autosuggestions.zsh", "brew"},
		{"zsh-autosuggestions", "darwin-intel", darwin, "/usr/local/share/zsh-autosuggestions/zsh-autosuggestions.zsh", "brew"},
		{"zsh-autosuggestions", "linuxbrew", linuxbrew, "/home/linuxbrew/.linuxbrew/share/zsh-autosuggestions/zsh-autosuggestions.zsh", "brew"},
		{"zsh-autosuggestions", "arch", arch, "/usr/share/zsh/plugins/zsh-autosuggestions/zsh-autosuggestions.zsh", "pacman"},
		{"zsh-autosuggestions", "debian", debian, "/usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh", "apt"},
		{"zsh-autosuggestions", "ubuntu", ubuntu, "/usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh", "apt"},
		{"zsh-autosuggestions", "fedora", fedora, "/usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh", "dnf"},
		{"zsh-syntax-highlighting", "darwin", darwin, "/opt/homebrew/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh", "brew"},
		{"zsh-syntax-highlighting", "darwin-intel", darwin, "/usr/local/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh", "brew"},
		{"zsh-syntax-highlighting", "linuxbrew", linuxbrew, "/home/linuxbrew/.linuxbrew/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh", "brew"},
		{"zsh-syntax-highlighting", "arch", arch, "/usr/share/zsh/plugins/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh", "pacman"},
		{"zsh-syntax-highlighting", "debian", debian, "/usr/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh", "apt"},
		{"zsh-syntax-highlighting", "ubuntu", ubuntu, "/usr/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh", "apt"},
		{"zsh-syntax-highlighting", "fedora", fedora, "/usr/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh", "dnf"},
		{"zsh-autocomplete", "darwin", darwin, "/opt/homebrew/share/zsh-autocomplete/zsh-autocomplete.plugin.zsh", "brew"},
		{"zsh-autocomplete", "darwin-intel", darwin, "/usr/local/share/zsh-autocomplete/zsh-autocomplete.plugin.zsh", "brew"},
		{"zsh-autocomplete", "linuxbrew", linuxbrew, "/home/linuxbrew/.linuxbrew/share/zsh-autocomplete/zsh-autocomplete.plugin.zsh", "brew"},
		{"font-caskaydia-cove", "darwin", darwin, "~/Library/Fonts/CaskaydiaCoveNerdFont-Regular.ttf", "brew"},
		{"font-iosevka-term", "darwin", darwin, "~/Library/Fonts/IosevkaTermNerdFont-Regular.ttf", "brew"},
		{"openjdk-17", "darwin", darwin, "/opt/homebrew/opt/openjdk@17", "brew"},
		{"openjdk-17", "linuxbrew", linuxbrew, "/home/linuxbrew/.linuxbrew/opt/openjdk@17", "brew"},
		{"openjdk-17", "arch", arch, "/usr/lib/jvm/java-17-openjdk", "pacman"},
	}

	m := loadRepoManifest(t)
	defaults := manifest.DefaultManifest()
	for _, tc := range cases {
		t.Run(tc.app+"/"+tc.platform, func(t *testing.T) {
			if !reflect.DeepEqual(defaults.Apps[tc.app].Check, m.Apps[tc.app].Check) {
				t.Errorf("canonical default manifest check for %s drifted", tc.app)
			}
			root := t.TempDir()
			home := filepath.Join(root, "home")
			t.Setenv("HOME", home)
			t.Setenv("PATH", filepath.Join(root, "empty-bin")) // no command can satisfy the probe
			probe := rootedProbe(root)

			absent, err := BuildPlan(m, []string{tc.app}, tc.sys, PlanOptions{Probe: probe})
			if err != nil {
				t.Fatal(err)
			}
			if s := stepByName(t, absent, tc.app); s.Status != StatusInstall || s.Manager != tc.manager {
				t.Fatalf("absent %s: status=%s manager=%s reason=%q, want install via %s", tc.app, s.Status, s.Manager, s.Reason, tc.manager)
			}

			fixture := filepath.Join(root, tc.file)
			if strings.HasPrefix(tc.file, "~/") {
				fixture = filepath.Join(home, tc.file[2:])
			}
			if err := os.MkdirAll(filepath.Dir(fixture), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fixture, []byte("# fixture\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			present, err := BuildPlan(m, []string{tc.app}, tc.sys, PlanOptions{Probe: probe})
			if err != nil {
				t.Fatal(err)
			}
			if s := stepByName(t, present, tc.app); s.Status != StatusInstalled {
				t.Fatalf("installed %s (%s): status=%s, want %s (check %v)", tc.app, tc.file, s.Status, StatusInstalled, m.Apps[tc.app].Check)
			}
		})
	}
}
