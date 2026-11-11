package discover

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/clean"
	"github.com/kevnnard/dots/pkg/manifest"
)

// fakeExec answers fixed argv only; any other command fails the test.
type fakeExec struct {
	t     *testing.T
	out   map[string]string
	calls []string
}

func (f *fakeExec) Run(_ context.Context, argv []string) (clean.Result, error) {
	key := strings.Join(argv, " ")
	f.calls = append(f.calls, key)
	out, ok := f.out[key]
	if !ok {
		f.t.Errorf("unexpected command %q", key)
		return clean.Result{}, errors.New("unexpected command")
	}
	return clean.Result{Stdout: []byte(out)}, nil
}

const infoJSON = `{"formulae":[
 {"name":"ripgrep","full_name":"ripgrep","tap":"homebrew/core","bottle":{"stable":{"files":{"arm64_sonoma":{},"x86_64_linux":{}}}},"requirements":[]},
 {"name":"mas","full_name":"mas","tap":"homebrew/core","bottle":{"stable":{"files":{"arm64_sonoma":{},"sonoma":{}}}},"requirements":[]},
 {"name":"swiftfmt","full_name":"swiftfmt","tap":"homebrew/core","bottle":{},"requirements":[{"name":"macos"}]},
 {"name":"fpm","full_name":"acme/tools/fpm","tap":"acme/tools","bottle":{},"requirements":[{"name":"linux"}]}
],"casks":[
 {"token":"godot-mono","full_token":"godot-mono","tap":"homebrew/cask"},
 {"token":"menubar","full_token":"acme/apps/menubar","tap":"acme/apps"}
]}`

func brewFixture(t *testing.T) (*manifest.Manifest, Options, *fakeExec) {
	t.Helper()
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"atuin":       {Install: map[string]manifest.InstallRecipe{"brew": {Manager: "brew", Package: "atuin"}}},
		"engram":      {Install: map[string]manifest.InstallRecipe{"darwin": {Manager: "brew", Package: "gentleman-programming/tap/engram"}}},
		"smartwindow": {Install: map[string]manifest.InstallRecipe{"darwin": {Manager: "brew", Cask: "kevnnard/tap/smartwindow"}}},
	}}
	brewfile := filepath.Join(t.TempDir(), "Brewfile")
	if err := os.WriteFile(brewfile, []byte("tap \"steipete/tap\"\n# comment\ncask \"steipete/tap/codexbar\", trusted: true\nbrew \"jq\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeExec{t: t, out: map[string]string{
		"brew leaves --installed-on-request": "atuin\ngentleman-programming/tap/engram\njq\nripgrep\nmas\nswiftfmt\nghostly\nacme/tools/fpm\n",
		"brew list --cask":                   "smartwindow\ncodexbar\ngodot-mono\nmenubar\n",
		"brew tap":                           "gentleman-programming/tap\nkevnnard/tap\nsteipete/tap\nacme/tools\nacme/apps\nlonely/tap\nhomebrew/core\n",
		"brew info --json=v2 acme/tools/fpm ghostly mas ripgrep swiftfmt godot-mono menubar": infoJSON,
	}}
	return m, Options{Exec: f, Brewfiles: []string{brewfile}}, f
}

func TestPackagesNotDeclaredAreClassifiedBySystem(t *testing.T) {
	m, opts, f := brewFixture(t)
	res, err := Run(context.Background(), m, opts)
	if err != nil {
		t.Fatal(err)
	}
	both, mac := []string{"darwin", "linux"}, []string{"darwin"}
	want := []Item{
		{Name: "acme/apps/menubar", Kind: "cask", OS: mac},
		{Name: "acme/tools/fpm", Kind: "formula", OS: both}, // linux requirement
		{Name: "ghostly", Kind: "formula", OS: mac},         // no data
		{Name: "godot-mono", Kind: "cask", OS: mac},
		{Name: "lonely/tap", Kind: "tap", OS: both},
		{Name: "mas", Kind: "formula", OS: mac},      // macOS-only bottles
		{Name: "ripgrep", Kind: "formula", OS: both}, // linux bottle
		{Name: "swiftfmt", Kind: "formula", OS: mac}, // macos requirement
	}
	if !reflect.DeepEqual(res.New, want) {
		t.Fatalf("new =\n%+v\nwant\n%+v", res.New, want)
	}
	if n := len(f.calls); n != 4 {
		t.Errorf("want 4 brew queries (one info batch), got %v", f.calls)
	}

	for name, wantApp := range map[string]manifest.AppConfig{
		"ripgrep":        {OS: both, Install: map[string]manifest.InstallRecipe{"brew": {Manager: "brew", Package: "ripgrep"}}},
		"mas":            {OS: mac, Install: map[string]manifest.InstallRecipe{"darwin": {Manager: "brew", Package: "mas"}}},
		"fpm":            {OS: both, Install: map[string]manifest.InstallRecipe{"brew": {Manager: "brew", Package: "acme/tools/fpm"}}},
		"godot-mono":     {OS: mac, Install: map[string]manifest.InstallRecipe{"darwin": {Manager: "brew", Cask: "godot-mono"}}},
		"menubar":        {OS: mac, Install: map[string]manifest.InstallRecipe{"darwin": {Manager: "brew", Cask: "acme/apps/menubar"}}},
		"tap-lonely-tap": {OS: both, Install: map[string]manifest.InstallRecipe{"brew": {Manager: "brew", Command: "brew tap lonely/tap"}}},
	} {
		got, ok := res.Apps[name]
		if !ok {
			t.Errorf("app %s not staged", name)
			continue
		}
		if !reflect.DeepEqual(got.OS, wantApp.OS) || !reflect.DeepEqual(got.Install, wantApp.Install) {
			t.Errorf("app %s = os %v install %+v; want os %v install %+v", name, got.OS, got.Install, wantApp.OS, wantApp.Install)
		}
	}
	for _, declared := range []string{"atuin", "engram", "smartwindow", "codexbar", "jq"} {
		if _, ok := res.Apps[declared]; ok {
			t.Errorf("%s is declared and must not be staged again", declared)
		}
	}
}

func TestNoBrewQueriesWithoutExecutor(t *testing.T) {
	res, err := Run(context.Background(), &manifest.Manifest{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.New) != 0 || len(res.Skipped) != 0 || len(res.Lines()) != 0 {
		t.Errorf("nothing to discover must print nothing: %+v", res)
	}
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func configHome(t *testing.T) string {
	home := t.TempDir()
	for _, rel := range []string{
		"atuin/config.toml",   // covered by a file mapping inside it
		"linuxonly/x.conf",    // covered through src_os
		"wezterm/wezterm.lua", // uncovered
		"starship.toml",       // uncovered file
		"btop/btop.conf",      // uncovered, same name as a declared app
		"gcloud/credentials.db",
		"configstore/update-notifier.json",
		"github-copilot/apps.json",
		"gh-copilot/config.yml",
		"some-cache/blob",
		".DS_Store",
	} {
		writeFile(t, filepath.Join(home, ".config", rel), "x\n")
	}
	return home
}

func configManifest() *manifest.Manifest {
	return &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"atuin":  {Paths: []manifest.PathMapping{{Src: "~/.config/atuin/config.toml", Dest: "configs/atuin/config.toml"}}},
		"linuxy": {Paths: []manifest.PathMapping{{Src: "~/Library/linuxonly", Dest: "configs/linuxonly", SrcOS: map[string]string{"linux": "~/.config/linuxonly"}}}},
		"btop":   {Install: map[string]manifest.InstallRecipe{"brew": {Manager: "brew", Package: "btop"}}, Exclude: []string{"*.bak"}},
	}}
}

func TestConfigEntriesNotCoveredBecomeConfigApps(t *testing.T) {
	home := configHome(t)
	res, err := Run(context.Background(), configManifest(), Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	both := []string{"darwin", "linux"}
	wantNew := []Item{
		{Name: "btop", Kind: "config", OS: both},
		{Name: "starship", Kind: "config", OS: both},
		{Name: "wezterm", Kind: "config", OS: both},
	}
	if !reflect.DeepEqual(res.New, wantNew) {
		t.Fatalf("new = %+v\nwant %+v", res.New, wantNew)
	}
	cred := "may hold credentials"
	wantSkip := []Skip{
		{Name: "configstore", Reason: cred},
		{Name: "gcloud", Reason: cred},
		{Name: "gh-copilot", Reason: cred},
		{Name: "github-copilot", Reason: cred},
		{Name: "some-cache", Reason: "cache"},
	}
	if !reflect.DeepEqual(res.Skipped, wantSkip) {
		t.Fatalf("skipped = %+v\nwant %+v", res.Skipped, wantSkip)
	}

	wez := res.Apps["wezterm"]
	if !reflect.DeepEqual(wez.OS, both) || !reflect.DeepEqual(wez.Paths, []manifest.PathMapping{{Src: "~/.config/wezterm", Dest: "configs/wezterm"}}) {
		t.Errorf("wezterm app = %+v", wez)
	}
	star := res.Apps["starship"]
	if !reflect.DeepEqual(star.Paths, []manifest.PathMapping{{Src: "~/.config/starship.toml", Dest: "configs/starship/starship.toml"}}) {
		t.Errorf("starship app = %+v", star)
	}
	// A declared app with the same name keeps its recipe and gains the path.
	btop := res.Apps["btop"]
	if btop.Install["brew"].Package != "btop" || len(btop.Paths) != 1 || btop.Paths[0].Src != "~/.config/btop" ||
		!reflect.DeepEqual(btop.Paths[0].OS, both) {
		t.Errorf("btop app = %+v", btop)
	}
	if c := res.Configs["btop"]; len(c.Paths) != 1 || !contains(c.Exclude, "*.bak") {
		t.Errorf("btop backup must hold only the new path and keep its excludes: %+v", c)
	}
	for _, never := range []string{"gcloud", "configstore", "github-copilot", "gh-copilot", "some-cache", "atuin", "linuxonly"} {
		if _, ok := res.Configs[never]; ok {
			t.Errorf("%s must not be staged", never)
		}
	}
}

func TestConfigOfDiscoveredPackageAttachesToThatApp(t *testing.T) {
	m, opts, _ := brewFixture(t)
	opts.Home = t.TempDir()
	writeFile(t, filepath.Join(opts.Home, ".config", "ripgrep", "config"), "--smart-case\n")
	res, err := Run(context.Background(), m, opts)
	if err != nil {
		t.Fatal(err)
	}
	rg := res.Apps["ripgrep"]
	if rg.Install["brew"].Package != "ripgrep" || len(rg.Paths) != 1 || rg.Paths[0].Dest != "configs/ripgrep" {
		t.Errorf("ripgrep app = %+v", rg)
	}
	var kinds []string
	for _, it := range res.New {
		if it.Name == "ripgrep" {
			kinds = append(kinds, it.Kind)
		}
	}
	if !reflect.DeepEqual(kinds, []string{"config", "formula"}) {
		t.Errorf("ripgrep items = %v", kinds)
	}
}

func TestLinesListNewThenSkipped(t *testing.T) {
	res := &Result{
		New:     []Item{{Name: "godot-mono", Kind: "cask", OS: []string{"darwin"}}, {Name: "wezterm", Kind: "config", OS: []string{"darwin", "linux"}}},
		Skipped: []Skip{{Name: "gcloud", Reason: "may hold credentials"}},
	}
	want := []string{
		"New:",
		"  - godot-mono (cask): darwin",
		"  - wezterm (config): darwin, linux",
		"Skipped:",
		"  - gcloud: may hold credentials",
	}
	if got := res.Lines(); !reflect.DeepEqual(got, want) {
		t.Errorf("lines =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
