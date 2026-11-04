package clean

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

func brewRecipe(pkg string) map[string]manifest.InstallRecipe {
	return map[string]manifest.InstallRecipe{"darwin": {Manager: "brew", Package: pkg}}
}

func fixture() *manifest.Manifest {
	return &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"node": {Install: brewRecipe("node")},
		"pi": {Requires: []string{"node"}, Install: map[string]manifest.InstallRecipe{
			"default": {Manager: "npm", Package: "@earendil-works/pi-coding-agent"},
		}},
		"claude": {
			Requires: []string{"node"},
			Paths: []manifest.PathMapping{
				{Src: "~/.claude/settings.json", Dest: "configs/claude/settings.json"},
				{Src: "~/.claude/mcp.json", Dest: "configs/claude/mcp.json"},
			},
			Secrets: []string{"mcp.json"},
			Exclude: []string{"sessions/**"},
			Install: map[string]manifest.InstallRecipe{
				"darwin":  {Manager: "brew", Cask: "claude-code"},
				"default": {Manager: "npm", Package: "@anthropic-ai/claude-code"},
			},
		},
		"zsh": {
			ConfigRequires: []string{"fzf", "p10k"},
			Paths:          []manifest.PathMapping{{Src: "~/.zshrc", Dest: "configs/zsh/.zshrc"}},
			Install:        brewRecipe("zsh"),
		},
		"p10k":      {Optional: true, Requires: []string{"zsh"}, Install: brewRecipe("powerlevel10k")},
		"fzf":       {Install: brewRecipe("fzf")},
		"brew":      {Paths: []manifest.PathMapping{{Src: "Brewfile", Dest: "Brewfile"}}},
		"tailscale": {Install: map[string]manifest.InstallRecipe{"darwin": {Manager: "script", Command: "curl -fsSL https://x | sh"}}},
		"evil":      {Install: brewRecipe("x; rm -rf ~")},
		"dash":      {Install: brewRecipe("-rf")},
		"fd":        {Install: brewRecipe("fd")},
		"fd-alias":  {Install: brewRecipe("fd")},
	}}
}

func darwin() *detector.SystemInfo {
	return &detector.SystemInfo{OS: "darwin", Distro: detector.DistroDarwin, PrimaryManager: detector.ManagerBrew,
		Managers: []detector.PackageManager{detector.ManagerBrew, detector.ManagerNpm, detector.ManagerPnpm}}
}

// fakeProbe answers from fixed evidence and records every queried app.
type fakeProbe struct {
	ev     map[string]Evidence
	errs   map[string]error
	called map[string]bool
}

func (f *fakeProbe) probe(name string, _ manifest.AppConfig) (Evidence, error) {
	if f.called == nil {
		f.called = map[string]bool{}
	}
	f.called[name] = true
	return f.ev[name], f.errs[name]
}

func brewOwned(pkg string) Evidence {
	return Evidence{Present: true, Owners: []Owner{{Manager: "brew", Package: pkg}}}
}

func plan(t *testing.T, req Request, f *fakeProbe) *Plan {
	t.Helper()
	p, err := BuildPlan(fixture(), req, darwin(), f.probe)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func pkgItem(t *testing.T, p *Plan, name string) PackageItem {
	t.Helper()
	for _, it := range p.Packages {
		if it.AppName == name {
			return it
		}
	}
	t.Fatalf("package %q not planned: %+v", name, p.Packages)
	return PackageItem{}
}

func pkgNames(p *Plan) string {
	var out []string
	for _, it := range p.Packages {
		out = append(out, it.AppName)
	}
	return strings.Join(out, ",")
}

func TestCandidatesListEveryDeclaredAppIncludingOptional(t *testing.T) {
	got := Candidates(fixture())
	if len(got) != len(fixture().Apps) || got[0] != "brew" || !strings.Contains(strings.Join(got, ","), "p10k") {
		t.Fatalf("candidates = %v", got)
	}
}

func TestExactSelectionKeepsScopesIndependent(t *testing.T) {
	f := &fakeProbe{ev: map[string]Evidence{
		"pi": {Present: true, Owners: []Owner{{Manager: "npm", Package: "@earendil-works/pi-coding-agent", Prefix: "/home/u/.local"}}},
	}}
	p := plan(t, Request{Packages: []string{"pi", "pi"}, Configs: []string{"zsh"}}, f)
	if pkgNames(p) != "pi" {
		t.Fatalf("packages = %s, want exactly pi (node must not be added)", pkgNames(p))
	}
	want := []string{"npm", "uninstall", "-g", "--prefix", "/home/u/.local", "@earendil-works/pi-coding-agent"}
	if it := pkgItem(t, p, "pi"); it.Status != PackageRemove || !reflect.DeepEqual(it.Argv, want) {
		t.Fatalf("pi = %+v", it)
	}
	if len(p.Configs) != 1 || p.Configs[0].AppName != "zsh" || p.Configs[0].Status != ConfigQuarantine {
		t.Fatalf("configs = %+v, want only zsh", p.Configs)
	}
	if f.called["zsh"] || f.called["fzf"] {
		t.Fatal("configuration scope must not probe packages")
	}
}

func TestSelectedDependentsComeBeforeTheirRuntime(t *testing.T) {
	f := &fakeProbe{ev: map[string]Evidence{
		"node":   brewOwned("node"),
		"pi":     {Present: true, Owners: []Owner{{Manager: "pnpm", Package: "@earendil-works/pi-coding-agent"}}},
		"claude": {Present: true, Owners: []Owner{{Manager: "brew", Cask: "claude-code"}}},
	}}
	p := plan(t, Request{Packages: []string{"node", "pi", "claude"}}, f)
	if got := pkgNames(p); got != "pi,claude,node" {
		t.Fatalf("order = %s, want dependents before node", got)
	}
	for _, it := range p.Packages {
		if it.Status != PackageRemove {
			t.Fatalf("%s = %+v", it.AppName, it)
		}
	}
	if got := pkgItem(t, p, "claude").Argv; !reflect.DeepEqual(got, []string{"brew", "uninstall", "--cask", "claude-code"}) {
		t.Fatalf("claude argv = %v", got)
	}
}

func TestRetainedDependentBlocksRuntime(t *testing.T) {
	for name, f := range map[string]*fakeProbe{
		"present":  {ev: map[string]Evidence{"node": brewOwned("node"), "pi": {Present: true}}},
		"unknown":  {ev: map[string]Evidence{"node": brewOwned("node")}, errs: map[string]error{"claude": errors.New("query failed")}},
		"selected": {ev: map[string]Evidence{"node": brewOwned("node"), "pi": {Present: true}}},
	} {
		req := Request{Packages: []string{"node"}}
		if name == "selected" {
			req.Packages = append(req.Packages, "pi") // pi has no owner, so it stays
		}
		it := pkgItem(t, plan(t, req, f), "node")
		if it.Status != PackageBlocked || it.Argv != nil || !strings.Contains(it.Reason, "required by") {
			t.Fatalf("%s: node = %+v", name, it)
		}
	}
}

func TestUnknownOrAmbiguousOwnershipBlocks(t *testing.T) {
	f := &fakeProbe{
		ev: map[string]Evidence{
			"zsh":    {Present: true, Owners: []Owner{{Manager: "brew", Package: "zsh"}, {Manager: "pacman", Package: "zsh"}}},
			"claude": {Present: true}, // a command alone is not ownership
			"node":   brewOwned("nodejs-other"),
		},
		errs: map[string]error{"fzf": errors.New("brew list failed")},
	}
	p := plan(t, Request{Packages: []string{"zsh", "claude", "node", "fzf"}}, f)
	for _, name := range []string{"zsh", "claude", "node", "fzf"} {
		if it := pkgItem(t, p, name); it.Status != PackageBlocked || it.Argv != nil {
			t.Fatalf("%s = %+v, want blocked", name, it)
		}
	}
	if it := plan(t, Request{Packages: []string{"fd"}}, &fakeProbe{}); it.Packages[0].Status != PackageNotInstalled {
		t.Fatalf("absent fd = %+v", it.Packages[0])
	}
}

func TestBundleAppIsConfigOnlyAndNeverOwnership(t *testing.T) {
	f := &fakeProbe{}
	p := plan(t, Request{Packages: []string{"brew"}, Configs: []string{"brew"}}, f)
	if it := pkgItem(t, p, "brew"); it.Status != PackageConfigOnly || it.Argv != nil {
		t.Fatalf("brew package = %+v", it)
	}
	if p.Configs[0].Status != ConfigBlocked {
		t.Fatalf("Brewfile is a repository file, not home config: %+v", p.Configs[0])
	}
	if len(f.called) != 0 {
		t.Fatalf("bundle app probed: %v", f.called)
	}
}

func TestScriptInstallsHaveNoInverse(t *testing.T) {
	for _, ev := range []Evidence{{Present: true}, {Present: true, Owners: []Owner{{Manager: "script"}}}} {
		it := plan(t, Request{Packages: []string{"tailscale"}}, &fakeProbe{ev: map[string]Evidence{"tailscale": ev}}).Packages[0]
		if (it.Status != PackageBlocked && it.Status != PackageUnsupported) || it.Argv != nil {
			t.Fatalf("tailscale = %+v", it)
		}
	}
}

func TestPackageNamesStaySingleArguments(t *testing.T) {
	f := &fakeProbe{ev: map[string]Evidence{"evil": brewOwned("x; rm -rf ~"), "dash": brewOwned("-rf")}}
	p := plan(t, Request{Packages: []string{"evil", "dash"}}, f)
	if got := pkgItem(t, p, "evil").Argv; !reflect.DeepEqual(got, []string{"brew", "uninstall", "--formula", "x; rm -rf ~"}) {
		t.Fatalf("evil argv = %q", got)
	}
	if it := pkgItem(t, p, "dash"); it.Status != PackageBlocked || it.Argv != nil {
		t.Fatalf("flag-like package = %+v", it)
	}
	for _, prefix := range []string{"--global-style", "relative/dir"} {
		f := &fakeProbe{ev: map[string]Evidence{"pi": {Present: true, Owners: []Owner{{Manager: "npm", Package: "@earendil-works/pi-coding-agent", Prefix: prefix}}}}}
		if it := plan(t, Request{Packages: []string{"pi"}}, f).Packages[0]; it.Status != PackageBlocked || it.Argv != nil {
			t.Fatalf("prefix %q = %+v", prefix, it)
		}
	}
}

func TestSharedPackageIsRemovedOnce(t *testing.T) {
	f := &fakeProbe{ev: map[string]Evidence{"fd": brewOwned("fd"), "fd-alias": brewOwned("fd")}}
	p := plan(t, Request{Packages: []string{"fd", "fd-alias"}}, f)
	statuses := pkgItem(t, p, "fd").Status + "," + pkgItem(t, p, "fd-alias").Status
	if statuses != "duplicate,remove" && statuses != "remove,duplicate" {
		t.Fatalf("statuses = %s", statuses)
	}
	if it := plan(t, Request{Packages: []string{"fd"}}, f).Packages[0]; it.Status != PackageBlocked || !strings.Contains(it.Reason, "fd-alias") {
		t.Fatalf("fd shared with kept fd-alias = %+v", it)
	}
}

func TestConfigRequiresWarnsWhilePluginRequiresBlocks(t *testing.T) {
	f := &fakeProbe{ev: map[string]Evidence{"fzf": brewOwned("fzf"), "zsh": brewOwned("zsh"), "p10k": brewOwned("powerlevel10k")}}
	p := plan(t, Request{Packages: []string{"fzf", "zsh"}}, f)
	if it := pkgItem(t, p, "fzf"); it.Status != PackageRemove || len(it.Warnings) != 1 || !strings.Contains(it.Warnings[0], "zsh") {
		t.Fatalf("fzf = %+v, want removal with zsh config warning", it)
	}
	if it := pkgItem(t, p, "zsh"); it.Status != PackageBlocked || !strings.Contains(it.Reason, "p10k") {
		t.Fatalf("zsh = %+v, want blocked by installed plugin p10k", it)
	}
}

func TestConfigPlanPreservesSecretsAndExcludes(t *testing.T) {
	c := plan(t, Request{Configs: []string{"claude", "node"}}, &fakeProbe{}).Configs
	if c[0].AppName != "claude" || !reflect.DeepEqual(c[0].Paths, []string{"~/.claude/settings.json"}) ||
		!reflect.DeepEqual(c[0].Preserve, []string{"mcp.json", "sessions/**"}) {
		t.Fatalf("claude config = %+v", c[0])
	}
	if c[1].Status != ConfigNone {
		t.Fatalf("node config = %+v", c[1])
	}
}

func TestUnknownAppIsAnError(t *testing.T) {
	if _, err := BuildPlan(fixture(), Request{Configs: []string{"nope"}}, darwin(), (&fakeProbe{}).probe); err == nil {
		t.Fatal("unknown app accepted")
	}
}
