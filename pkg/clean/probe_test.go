package clean

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

// fakeExec answers fixed argv only; any other command fails the test, so no
// unexpected query or mutation can go unnoticed.
type fakeExec struct {
	t     *testing.T
	out   map[string]Result
	calls []string
}

func (f *fakeExec) Run(_ context.Context, argv []string) (Result, error) {
	key := strings.Join(argv, " ")
	f.calls = append(f.calls, key)
	r, ok := f.out[key]
	if !ok {
		f.t.Errorf("unexpected command %q", key)
		return Result{}, errors.New("unexpected command")
	}
	return r, nil
}

func ok(stdout string) Result      { return Result{Stdout: []byte(stdout)} }
func missing(stderr string) Result { return Result{Stderr: []byte(stderr), ExitCode: 1} }
func exit1(stdout string) Result   { return Result{Stdout: []byte(stdout), ExitCode: 1} }

const (
	npmPrefix = "npm prefix -g"
	npmLocal  = "npm ls -g --prefix /home/u/.local --depth=0 --json @earendil-works/pi-coding-agent"
	npmGlobal = "npm ls -g --prefix /usr/local --depth=0 --json @earendil-works/pi-coding-agent"
	pnpmLs    = "pnpm ls -g --depth=0 --json @earendil-works/pi-coding-agent"
	piJSON    = `{"dependencies":{"@earendil-works/pi-coding-agent":{"version":"1.0.0"}}}`
)

func nativeProbe(t *testing.T, sys *detector.SystemInfo, out map[string]Result) (NativeProbe, *fakeExec) {
	f := &fakeExec{t: t, out: out}
	return NativeProbe{Sys: sys, Exec: f, Home: "/home/u",
		Present: func(string, manifest.AppConfig) bool { return true }}, f
}

func arch() *detector.SystemInfo {
	return &detector.SystemInfo{OS: "linux", Distro: detector.DistroArch, PrimaryManager: detector.ManagerYay,
		Managers: []detector.PackageManager{detector.ManagerYay, detector.ManagerPacman}}
}

func TestProbeBrewSeparatesFormulaAndCaskIdentity(t *testing.T) {
	p, f := nativeProbe(t, darwin(), map[string]Result{
		"brew list --cask --versions claude-code": ok("claude-code 2.0.1\n"),
		npmPrefix: ok("/usr/local\n"),
		"npm ls -g --prefix /usr/local --depth=0 --json @anthropic-ai/claude-code":     exit1("{}"),
		"npm ls -g --prefix /home/u/.local --depth=0 --json @anthropic-ai/claude-code": exit1("{}"),
		"pnpm ls -g --depth=0 --json @anthropic-ai/claude-code":                        ok("[]"),
	})
	ev, err := p.Probe("claude", fixture().Apps["claude"])
	if err != nil {
		t.Fatal(err)
	}
	if want := []Owner{{Manager: "brew", Cask: "claude-code"}}; !reflect.DeepEqual(ev.Owners, want) || !ev.Present {
		t.Fatalf("evidence = %+v", ev)
	}
	for _, c := range f.calls {
		if !strings.HasSuffix(c, "claude-code") && !strings.HasSuffix(c, "@anthropic-ai/claude-code") && c != npmPrefix {
			t.Fatalf("query %q is not about a declared package", c)
		}
	}
}

func TestProbeArchQueriesPacmanDatabaseForYayRecipes(t *testing.T) {
	m := map[string]manifest.AppConfig{
		"herdr": {Install: map[string]manifest.InstallRecipe{"yay": {Manager: "yay", Package: "herdr-bin"}}},
		"zsh": {Install: map[string]manifest.InstallRecipe{
			"arch": {Manager: "pacman", Package: "zsh"}, "yay": {Manager: "yay", Package: "zsh"}}},
	}
	p, f := nativeProbe(t, arch(), map[string]Result{
		"pacman -Q herdr-bin": ok("herdr-bin 0.3-1\n"),
		"pacman -Q zsh":       ok("zsh 5.9-5\n"),
	})
	ev, err := p.Probe("herdr", m["herdr"])
	if err != nil || !reflect.DeepEqual(ev.Owners, []Owner{{Manager: "pacman", Package: "herdr-bin"}}) {
		t.Fatalf("herdr = %+v, %v (a yay recipe is owned by its pacman database record)", ev, err)
	}
	ev, err = p.Probe("zsh", m["zsh"])
	if err != nil || !reflect.DeepEqual(ev.Owners, []Owner{{Manager: "pacman", Package: "zsh"}}) {
		t.Fatalf("zsh = %+v, %v (one database record, not two owners)", ev, err)
	}
	for _, c := range f.calls {
		if !strings.HasPrefix(c, "pacman -Q ") {
			t.Fatalf("unexpected query %q", c)
		}
	}
}

func TestProbeNpmReportsExactPrefixAndAmbiguity(t *testing.T) {
	pi := fixture().Apps["pi"]
	p, _ := nativeProbe(t, darwin(), map[string]Result{
		npmPrefix: ok("/usr/local\n"), npmGlobal: exit1("{}"), npmLocal: ok(piJSON), pnpmLs: ok("[]"),
	})
	ev, err := p.Probe("pi", pi)
	if want := []Owner{{Manager: "npm", Package: "@earendil-works/pi-coding-agent", Prefix: "/home/u/.local"}}; err != nil || !reflect.DeepEqual(ev.Owners, want) {
		t.Fatalf("pi = %+v, %v", ev, err)
	}
	p, _ = nativeProbe(t, darwin(), map[string]Result{
		npmPrefix: ok("/usr/local\n"), npmGlobal: exit1("{}"), npmLocal: ok(piJSON),
		pnpmLs: ok(`[{"path":"/home/u/.pnpm","dependencies":{"@earendil-works/pi-coding-agent":{"version":"1.0.0"}}}]`),
	})
	if ev, err := p.Probe("pi", pi); err != nil || len(ev.Owners) != 2 {
		t.Fatalf("npm and pnpm records = %+v, %v, want both owners", ev, err)
	}
}

func TestProbeUncertainQueriesAreErrorsNotAbsence(t *testing.T) {
	for name, out := range map[string]map[string]Result{
		"invalid json":   {npmPrefix: ok("/usr/local\n"), npmGlobal: ok("not json"), npmLocal: exit1("{}"), pnpmLs: ok("[]")},
		"npm problems":   {npmPrefix: ok("/usr/local\n"), npmGlobal: exit1(`{"problems":["invalid"]}`), npmLocal: exit1("{}"), pnpmLs: ok("[]")},
		"relative":       {npmPrefix: ok("usr/local\n")},
		"pnpm failure":   {npmPrefix: ok("/usr/local\n"), npmGlobal: exit1("{}"), npmLocal: exit1("{}"), pnpmLs: {ExitCode: 1}},
		"missing marker": {npmPrefix: ok("/usr/local\n"), npmGlobal: exit1(`{"dependencies":{"@earendil-works/pi-coding-agent":{"missing":true}}}`), npmLocal: exit1("{}"), pnpmLs: ok("[]")},
	} {
		p, _ := nativeProbe(t, darwin(), out)
		if ev, err := p.Probe("pi", fixture().Apps["pi"]); err == nil {
			t.Fatalf("%s: evidence %+v accepted", name, ev)
		}
	}
	p, _ := nativeProbe(t, darwin(), map[string]Result{"brew list --formula --versions node": missing("Error: brew is broken")})
	if _, err := p.Probe("node", fixture().Apps["node"]); err == nil {
		t.Fatal("brew error treated as absence")
	}
	p, _ = nativeProbe(t, darwin(), map[string]Result{"brew list --formula --versions node": ok("nodejs 1\n")})
	if _, err := p.Probe("node", fixture().Apps["node"]); err == nil {
		t.Fatal("different brew record accepted as owner")
	}
	p, _ = nativeProbe(t, arch(), map[string]Result{"pacman -Q zsh": missing("error: database not readable")})
	if _, err := p.Probe("zsh", manifest.AppConfig{Install: map[string]manifest.InstallRecipe{"arch": {Manager: "pacman", Package: "zsh"}}}); err == nil {
		t.Fatal("pacman error treated as absence")
	}
}

func TestProbeAbsenceUnsupportedManagersAndPresence(t *testing.T) {
	p, f := nativeProbe(t, darwin(), map[string]Result{"brew list --formula --versions node": missing("")})
	p.Present = func(string, manifest.AppConfig) bool { return false }
	if ev, err := p.Probe("node", fixture().Apps["node"]); err != nil || ev.Present || len(ev.Owners) != 0 {
		t.Fatalf("absent node = %+v, %v", ev, err)
	}
	if ev, err := p.Probe("tailscale", fixture().Apps["tailscale"]); err != nil || len(ev.Owners) != 0 || len(f.calls) != 1 {
		t.Fatalf("script recipe = %+v, %v; calls %v (scripts are never queried or owned)", ev, err, f.calls)
	}
	ubuntu := &detector.SystemInfo{OS: "linux", Managers: []detector.PackageManager{detector.ManagerApt}}
	p, _ = nativeProbe(t, ubuntu, nil)
	app := manifest.AppConfig{Install: map[string]manifest.InstallRecipe{"debian": {Manager: "apt", Package: "zsh"}}}
	if _, err := p.Probe("zsh", app); err == nil || !strings.Contains(err.Error(), "apt") {
		t.Fatalf("unsupported manager err = %v", err)
	}
	p.Present = nil
	if _, err := p.Probe("zsh", app); err == nil {
		t.Fatal("missing presence check accepted")
	}
	p, _ = nativeProbe(t, darwin(), nil)
	if _, err := p.Probe("dash", fixture().Apps["dash"]); err == nil {
		t.Fatal("flag-like package queried")
	}
}

func TestPathPresenceUsesHomeAndPathWithoutFollowingLinks(t *testing.T) {
	home := t.TempDir()
	if err := os.Symlink(filepath.Join(home, "nowhere"), filepath.Join(home, "link")); err != nil {
		t.Fatal(err)
	}
	present := PathPresence(home)
	for check, want := range map[string]bool{"~/link": true, "~/absent": false, "dots-no-such-command-xyz": false} {
		if got := present("x", manifest.AppConfig{Check: []string{check}}); got != want {
			t.Fatalf("%s present = %v, want %v", check, got, want)
		}
	}
}
