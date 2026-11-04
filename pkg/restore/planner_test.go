package restore

import (
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

func stepNames(steps []Step) []string {
	var out []string
	for _, s := range steps {
		out = append(out, s.AppName)
	}
	return out
}

func stepByName(t *testing.T, steps []Step, name string) Step {
	t.Helper()
	for _, s := range steps {
		if s.AppName == name {
			return s
		}
	}
	t.Fatalf("step %q not planned: %v", name, stepNames(steps))
	return Step{}
}

func plainArch() *detector.SystemInfo {
	return &detector.SystemInfo{
		OS:             "linux",
		Distro:         detector.DistroArch,
		PrimaryManager: detector.ManagerPacman,
		Managers:       []detector.PackageManager{detector.ManagerPacman},
	}
}

func npmToolManifest() *manifest.Manifest {
	return &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"node": {Provides: []string{"npm"}, Install: map[string]manifest.InstallRecipe{
			"pacman": {Manager: "pacman", Package: "nodejs npm"},
		}},
		"pi": {Requires: []string{"node"}, Install: map[string]manifest.InstallRecipe{
			"default": {Manager: "npm", Package: "@earendil-works/pi-coding-agent"},
		}},
		"zed": {Install: map[string]manifest.InstallRecipe{
			"pacman": {Manager: "pacman", Package: "zed"},
		}},
	}}
}

func TestBuildPlanPullsDependencyClosureInOrder(t *testing.T) {
	steps, err := BuildPlan(npmToolManifest(), []string{"pi"}, plainArch(), PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stepNames(steps), ","); got != "node,pi" {
		t.Fatalf("plan = %s, want node,pi", got)
	}
	if steps[0].Requested || !steps[1].Requested {
		t.Errorf("node must be pulled as dependency and pi requested: %+v", steps)
	}
	// npm is not on the system yet, but node earlier in the plan provides it.
	if steps[1].Status != StatusInstall || !strings.Contains(steps[1].InstallCmd, "@earendil-works/pi-coding-agent") {
		t.Errorf("pi step = %+v", steps[1])
	}
}

func TestBuildPlanSkipsRecipeWhoseNativeManagerIsAbsent(t *testing.T) {
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"ghostty": {Install: map[string]manifest.InstallRecipe{
			"arch":   {Manager: "yay", Package: "ghostty"},
			"pacman": {Manager: "pacman", Package: "ghostty"},
		}},
		"bruno": {Install: map[string]manifest.InstallRecipe{
			"yay": {Manager: "yay", Package: "bruno-bin"},
		}},
	}}
	steps, err := BuildPlan(m, []string{"ghostty", "bruno"}, plainArch(), PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if s := stepByName(t, steps, "ghostty"); s.Status != StatusInstall || s.InstallCmd != "sudo pacman -S --noconfirm ghostty" {
		t.Errorf("ghostty = %+v", s)
	}
	if s := stepByName(t, steps, "bruno"); s.Status != StatusManagerMissing || s.Manager != "yay" || s.InstallCmd != "" {
		t.Errorf("bruno = %+v", s)
	}
}

func TestBuildPlanSeparatesUnsupportedFromConfigOnly(t *testing.T) {
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"mole":       {Install: map[string]manifest.InstallRecipe{"darwin": {Manager: "brew", Package: "mole"}}},
		"flashspace": {Paths: []manifest.PathMapping{{Src: "~/.config/flashspace", Dest: "configs/flashspace"}}},
		"bitwarden":  {Install: map[string]manifest.InstallRecipe{"brew": {Manager: "brew", Cask: "bitwarden"}}},
		"tool":       {Requires: []string{"mole"}, Install: map[string]manifest.InstallRecipe{"pacman": {Manager: "pacman", Package: "tool"}}},
	}}
	linuxbrew := plainArch()
	linuxbrew.Managers = append(linuxbrew.Managers, detector.ManagerBrew)
	steps, err := BuildPlan(m, []string{"mole", "flashspace", "bitwarden", "tool"}, linuxbrew, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]PlanStatus{
		"mole":       StatusUnsupported,
		"flashspace": StatusConfigOnly,
		"bitwarden":  StatusUnsupported, // casks only exist on macOS
		"tool":       StatusBlocked,
	}
	for name, status := range want {
		if s := stepByName(t, steps, name); s.Status != status || s.InstallCmd != "" {
			t.Errorf("%s = %+v, want %s", name, s, status)
		}
	}
}

func TestBuildPlanSkipsAppsTheProbeReportsInstalled(t *testing.T) {
	probe := func(name string, _ manifest.AppConfig) bool { return name == "node" }
	sys := plainArch()
	sys.Managers = append(sys.Managers, detector.ManagerNpm)
	steps, err := BuildPlan(npmToolManifest(), []string{"pi"}, sys, PlanOptions{Probe: probe})
	if err != nil {
		t.Fatal(err)
	}
	if s := stepByName(t, steps, "node"); s.Status != StatusInstalled || s.InstallCmd != "" {
		t.Errorf("node = %+v", s)
	}
	if s := stepByName(t, steps, "pi"); s.Status != StatusInstall {
		t.Errorf("pi = %+v", s)
	}
}

func TestBuildPlanConfigRequiresOnlyWithConfig(t *testing.T) {
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"zsh":           {ConfigRequires: []string{"powerlevel10k"}, Install: map[string]manifest.InstallRecipe{"pacman": {Manager: "pacman", Package: "zsh"}}},
		"powerlevel10k": {Requires: []string{"zsh"}, Install: map[string]manifest.InstallRecipe{"pacman": {Manager: "pacman", Package: "p10k"}}},
	}}
	stock, err := BuildPlan(m, []string{"zsh"}, plainArch(), PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stepNames(stock), ","); got != "zsh" {
		t.Errorf("stock install must not pull config plugins: %s", got)
	}
	restored, err := BuildPlan(m, []string{"zsh"}, plainArch(), PlanOptions{WithConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stepNames(restored), ","); got != "zsh,powerlevel10k" {
		t.Errorf("restoring config must install zsh before its plugins: %s", got)
	}
}

func TestBuildPlanRejectsBrokenGraphs(t *testing.T) {
	cases := map[string]*manifest.Manifest{
		"missing dependency":        {Apps: map[string]manifest.AppConfig{"a": {Requires: []string{"ghost"}}}},
		"missing config dependency": {Apps: map[string]manifest.AppConfig{"a": {ConfigRequires: []string{"ghost"}}}},
		"cycle": {Apps: map[string]manifest.AppConfig{
			"a": {Requires: []string{"b"}},
			"b": {Requires: []string{"a"}},
		}},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateGraph(m); err == nil {
				t.Error("ValidateGraph accepted a broken graph")
			}
			if _, err := BuildPlan(m, []string{"a"}, plainArch(), PlanOptions{WithConfig: true}); err == nil {
				t.Error("BuildPlan accepted a broken graph")
			}
		})
	}
	if _, err := BuildPlan(npmToolManifest(), []string{"nope"}, plainArch(), PlanOptions{}); err == nil {
		t.Error("BuildPlan accepted an unknown app")
	}
}

func TestSelectFullExcludesOptionalApps(t *testing.T) {
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"zsh":     {},
		"ollama":  {Profile: "ai", Optional: true},
		"act":     {Profile: "core,infra"},
		"postman": {Optional: true},
	}}
	if got := strings.Join(Select(m, "all"), ","); got != "act,zsh" {
		t.Errorf("full = %s", got)
	}
	if got := strings.Join(Select(m, "infra"), ","); got != "act" {
		t.Errorf("infra = %s", got)
	}
}

func TestCommandProbeChecksCommandsAndPaths(t *testing.T) {
	bundle := t.TempDir()
	if !CommandProbe("gui", manifest.AppConfig{Check: []string{"definitely-missing-cmd", bundle}}) {
		t.Error("existing path entry must count as installed")
	}
	if CommandProbe("definitely-missing-cmd", manifest.AppConfig{}) {
		t.Error("missing command must not count as installed")
	}
	if !CommandProbe("sh", manifest.AppConfig{}) {
		t.Error("app name must be used as the command when Check is empty")
	}
}

func pacmanRecipe(pkg string) map[string]manifest.InstallRecipe {
	return map[string]manifest.InstallRecipe{"pacman": {Manager: "pacman", Package: pkg}}
}

func TestBuildPlanOrdersConfigDependenciesBeforeConsumers(t *testing.T) {
	// Names are chosen so alphabetical order alone would place the consumer
	// first; only the config edge can put the helpers ahead of it.
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"aa-editor": {ConfigRequires: []string{"zz-search"}, Install: pacmanRecipe("editor")},
		"zz-search": {Requires: []string{"mm-lib"}, Install: pacmanRecipe("search")},
		"mm-lib":    {Install: pacmanRecipe("lib")},
	}}
	stock, err := BuildPlan(m, []string{"aa-editor"}, plainArch(), PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stepNames(stock), ","); got != "aa-editor" {
		t.Errorf("stock install must not pull config dependencies: %s", got)
	}
	restored, err := BuildPlan(m, []string{"aa-editor"}, plainArch(), PlanOptions{WithConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stepNames(restored), ","); got != "mm-lib,zz-search,aa-editor" {
		t.Errorf("config dependencies must precede their consumer: %s", got)
	}
	if s := stepByName(t, restored, "zz-search"); s.Requested {
		t.Errorf("config dependency must not be marked requested: %+v", s)
	}
}

// Config edges order the plan and gate configuration deployment (see Run),
// but never block installing the consumer's own package.
func TestBuildPlanConfigDependencyNeverBlocksPackageInstall(t *testing.T) {
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"aa-editor": {ConfigRequires: []string{"zz-mac-only"}, Install: pacmanRecipe("editor")},
		"zz-mac-only": {Install: map[string]manifest.InstallRecipe{
			"darwin": {Manager: "brew", Package: "helper"},
		}},
		"bb-shell": {ConfigRequires: []string{"aa-editor"}, Install: pacmanRecipe("shell")},
	}}
	stock, err := BuildPlan(m, []string{"aa-editor"}, plainArch(), PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if s := stepByName(t, stock, "aa-editor"); s.Status != StatusInstall {
		t.Errorf("stock install must ignore config dependencies: %+v", s)
	}
	restored, err := BuildPlan(m, []string{"bb-shell"}, plainArch(), PlanOptions{WithConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stepNames(restored), ","); got != "zz-mac-only,aa-editor,bb-shell" {
		t.Fatalf("plan = %s", got)
	}
	if s := stepByName(t, restored, "zz-mac-only"); s.Status != StatusUnsupported {
		t.Errorf("zz-mac-only = %+v", s)
	}
	for _, name := range []string{"aa-editor", "bb-shell"} {
		if s := stepByName(t, restored, name); s.Status != StatusInstall || s.InstallCmd == "" {
			t.Errorf("%s package must stay installable despite its config dependency: %+v", name, s)
		}
	}
}

func TestBuildPlanBlocksOnlyHardRequirements(t *testing.T) {
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"aa-lib": {Install: map[string]manifest.InstallRecipe{
			"darwin": {Manager: "brew", Package: "lib"},
		}},
		"bb-tool": {Requires: []string{"aa-lib"}, Install: pacmanRecipe("tool")},
	}}
	steps, err := BuildPlan(m, []string{"bb-tool"}, plainArch(), PlanOptions{WithConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if s := stepByName(t, steps, "bb-tool"); s.Status != StatusBlocked || !strings.Contains(s.Reason, "aa-lib") {
		t.Errorf("bb-tool must be blocked by its hard requirement: %+v", s)
	}
}

func TestBuildPlanKeepsPluginsAfterTheAppTheyExtend(t *testing.T) {
	// A plugin that requires its consumer cannot precede it; the hard
	// requirement wins and the config edge is satisfied after the install.
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"zsh":      {ConfigRequires: []string{"aa-theme", "fzf"}, Install: pacmanRecipe("zsh")},
		"aa-theme": {Requires: []string{"zsh"}, Install: pacmanRecipe("theme")},
		"fzf":      {Install: pacmanRecipe("fzf")},
	}}
	if err := ValidateGraph(m); err != nil {
		t.Fatalf("plugin back-requirement is not a cycle: %v", err)
	}
	steps, err := BuildPlan(m, []string{"zsh"}, plainArch(), PlanOptions{WithConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stepNames(steps), ","); got != "fzf,zsh,aa-theme" {
		t.Errorf("plan = %s, want fzf,zsh,aa-theme", got)
	}
}

func TestValidateGraphRejectsConfigCycles(t *testing.T) {
	cases := map[string]*manifest.Manifest{
		"config cycle": {Apps: map[string]manifest.AppConfig{
			"a": {ConfigRequires: []string{"b"}},
			"b": {ConfigRequires: []string{"a"}},
		}},
		"config self reference": {Apps: map[string]manifest.AppConfig{
			"a": {ConfigRequires: []string{"a"}},
		}},
		"combined cycle": {Apps: map[string]manifest.AppConfig{
			"a": {ConfigRequires: []string{"b"}},
			"b": {Requires: []string{"c"}},
			"c": {ConfigRequires: []string{"a"}},
		}},
		"missing dependency of config dependency": {Apps: map[string]manifest.AppConfig{
			"a": {ConfigRequires: []string{"b"}},
			"b": {Requires: []string{"ghost"}},
		}},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateGraph(m); err == nil {
				t.Error("ValidateGraph accepted a broken graph")
			}
			if _, err := BuildPlan(m, []string{"a"}, plainArch(), PlanOptions{WithConfig: true}); err == nil {
				t.Error("BuildPlan accepted a broken graph")
			}
		})
	}
}
