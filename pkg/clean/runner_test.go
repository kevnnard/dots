package clean

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

const removeAllSummary = `Package cleanup plan:
  remove codex: pnpm remove -g @openai/codex
  remove claude: brew uninstall --cask claude-code
  remove node: brew uninstall --formula node
3 to remove, 0 blocked
`

func removeAll() *fakeProbe {
	return &fakeProbe{ev: map[string]Evidence{
		"node":   brewOwned("node"),
		"codex":  {Present: true, Owners: []Owner{{Manager: "pnpm", Package: "@openai/codex"}}},
		"claude": {Present: true, Owners: []Owner{{Manager: "brew", Cask: "claude-code"}}},
	}}
}

func runner(t *testing.T, f *fakeProbe, out map[string]Result) (PackageRunner, *fakeExec, *strings.Builder) {
	e := &fakeExec{t: t, out: out}
	var b strings.Builder
	return PackageRunner{Manifest: fixture(), Sys: darwin(), Exec: e, Recheck: f.probe, Out: &b}, e, &b
}

func TestRunnerDeniesWithoutConfirmation(t *testing.T) {
	f := removeAll()
	p := plan(t, Request{Packages: []string{"node", "codex", "claude"}}, f)
	for name, confirm := range map[string]func(string) bool{
		"default": nil,
		"decline": func(s string) bool { return false },
	} {
		r, e, out := runner(t, f, nil)
		r.Confirm = confirm
		rep, err := r.Run(context.Background(), p)
		if err != nil || !rep.Cancelled || len(e.calls) != 0 {
			t.Fatalf("%s: report %+v, err %v, calls %v", name, rep, err, e.calls)
		}
		if want := removeAllSummary + "Cancelled: nothing was changed.\n"; out.String() != want {
			t.Fatalf("%s: output\n%s\nwant\n%s", name, out, want)
		}
	}
}

func TestRunnerDryRunNeverExecutes(t *testing.T) {
	f := &fakeProbe{ev: map[string]Evidence{"fzf": brewOwned("fzf"), "zsh": brewOwned("zsh"), "p10k": brewOwned("powerlevel10k")}}
	p := plan(t, Request{Packages: []string{"fd", "fzf", "zsh"}}, f)
	r, e, out := runner(t, f, nil)
	r.DryRun = true
	r.Confirm = func(string) bool { t.Fatal("dry run asked for confirmation"); return true }
	rep, err := r.Run(context.Background(), p)
	if err != nil || !rep.DryRun || len(e.calls) != 0 {
		t.Fatalf("report %+v, err %v, calls %v", rep, err, e.calls)
	}
	want := `Package cleanup plan:
  blocked zsh: required by p10k, which is kept
  remove fzf: brew uninstall --formula fzf
  warning fzf: zsh configuration expects fzf
  not-installed fd
1 to remove, 1 blocked
Dry run: nothing was changed.
`
	if out.String() != want {
		t.Fatalf("output\n%s\nwant\n%s", out, want)
	}
}

func TestRunnerRechecksAndRunsDependentsFirst(t *testing.T) {
	f := removeAll()
	p := plan(t, Request{Packages: []string{"node", "codex", "claude"}}, f)
	r, e, out := runner(t, f, map[string]Result{
		"pnpm remove -g @openai/codex":      {},
		"brew uninstall --cask claude-code": {},
		"brew uninstall --formula node":     {},
	})
	var seen string
	r.Confirm = func(s string) bool { seen = s; return true }
	f.called = nil
	if _, err := r.Run(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if seen != removeAllSummary {
		t.Fatalf("confirmation saw %q", seen)
	}
	if got := strings.Join(e.calls, ";"); got != "pnpm remove -g @openai/codex;brew uninstall --cask claude-code;brew uninstall --formula node" {
		t.Fatalf("calls = %s", got)
	}
	if !f.called["codex"] || !f.called["claude"] || !f.called["node"] {
		t.Fatalf("preflight recheck skipped: %v", f.called)
	}
	if !strings.HasSuffix(out.String(), "removed codex\nremoved claude\nremoved node\n") {
		t.Fatalf("output = %s", out)
	}
}

func TestRunnerStopsAfterFailureOrChangedOwnership(t *testing.T) {
	f := removeAll()
	p := plan(t, Request{Packages: []string{"node", "codex", "claude"}}, f)
	r, e, out := runner(t, f, map[string]Result{
		"pnpm remove -g @openai/codex": {ExitCode: 1, Stderr: []byte("token=abc")},
	})
	r.Confirm = func(string) bool { return true }
	rep, err := r.Run(context.Background(), p)
	if err == nil || len(e.calls) != 1 || rep.Items[2].Outcome != OutcomeNotRun {
		t.Fatalf("report %+v, err %v, calls %v", rep, err, e.calls)
	}
	tail := "failed codex: exit status 1\nnot-run claude: stopped after an earlier failure\nnot-run node: stopped after an earlier failure\n"
	if !strings.HasSuffix(out.String(), tail) || strings.Contains(out.String(), "token") {
		t.Fatalf("output = %s", out)
	}

	f.ev["codex"] = Evidence{Present: true, Owners: []Owner{{Manager: "npm", Package: "@openai/codex", Prefix: "/x"}}}
	r, e, out = runner(t, f, nil)
	r.Confirm = func(string) bool { return true }
	if _, err := r.Run(context.Background(), p); err == nil || len(e.calls) != 0 ||
		!strings.Contains(out.String(), "failed codex: ownership changed since the plan") {
		t.Fatalf("changed owner: err %v, calls %v, output %s", err, e.calls, out)
	}
}

func TestRunnerRejectsTamperedPlanBeforeConfirmation(t *testing.T) {
	f := removeAll()
	for name, tamper := range map[string]func(*Plan){
		"argv":     func(p *Plan) { p.Packages[0].Argv = []string{"sh", "-c", "rm -rf ~"} },
		"owner":    func(p *Plan) { p.Packages[0].Owner = &Owner{Manager: "brew", Package: "git"} },
		"no owner": func(p *Plan) { p.Packages[0].Owner = nil },
		"unknown":  func(p *Plan) { p.Packages[0].AppName = "ghost" },
	} {
		p := plan(t, Request{Packages: []string{"node", "codex", "claude"}}, f)
		tamper(p)
		r, e, out := runner(t, f, nil)
		r.Confirm = func(string) bool { t.Fatalf("%s: tampered plan reached confirmation", name); return true }
		if _, err := r.Run(context.Background(), p); err == nil || len(e.calls) != 0 || out.Len() != 0 {
			t.Fatalf("%s: err %v, calls %v, output %q", name, err, e.calls, out)
		}
	}
}

func TestRunnerWithNothingToRemoveNeverAsks(t *testing.T) {
	f := &fakeProbe{errs: map[string]error{"fzf": errors.New("brew list failed")}}
	r, e, out := runner(t, f, nil)
	r.Confirm = func(string) bool { t.Fatal("asked to confirm an empty removal"); return true }
	rep, err := r.Run(context.Background(), plan(t, Request{Packages: []string{"fzf"}}, f))
	want := "Package cleanup plan:\n  blocked fzf: ownership unknown: brew list failed\n0 to remove, 1 blocked\nNothing to remove.\n"
	if err != nil || rep.Cancelled || len(e.calls) != 0 || out.String() != want {
		t.Fatalf("report %+v, err %v, output %q", rep, err, out)
	}
}

// An Omarchy yay recipe is planned, rechecked and removed as its pacman
// database record, and the shared-package and dependency guards still hold.
func TestRunnerRemovesYayRecipeThroughPacmanDatabase(t *testing.T) {
	yay := func(pkg string) map[string]manifest.InstallRecipe {
		return map[string]manifest.InstallRecipe{"yay": {Manager: "yay", Package: pkg}}
	}
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"herdr":  {Install: yay("herdr-bin")},
		"alias":  {Install: pacmanRecipe("herdr-bin")},
		"bruno":  {Install: yay("bruno-bin")},
		"plugin": {Requires: []string{"bruno"}, Install: yay("bruno-plugin")},
	}}
	p, e := nativeProbe(t, arch(), map[string]Result{
		"pacman -Q herdr-bin":      ok("herdr-bin 0.3-1\n"),
		"pacman -Q bruno-bin":      ok("bruno-bin 2.0-1\n"),
		"pacman -Q bruno-plugin":   ok("bruno-plugin 1.0-1\n"),
		"sudo pacman -R herdr-bin": {},
	})
	pl, err := BuildPlan(m, Request{Packages: []string{"herdr", "alias", "bruno"}}, arch(), p.Probe)
	if err != nil {
		t.Fatal(err)
	}
	want := `Package cleanup plan:
  remove herdr: sudo pacman -R herdr-bin
  blocked bruno: required by plugin, which is kept
  duplicate alias: same package as herdr
1 to remove, 1 blocked
`
	if got := Summary(pl); got != want {
		t.Fatalf("summary\n%s\nwant\n%s", got, want)
	}
	kept, err := BuildPlan(m, Request{Packages: []string{"herdr"}}, arch(), p.Probe)
	if err != nil || pkgItem(t, kept, "herdr").Reason != "same package is declared by alias, which is kept" {
		t.Fatalf("kept sharer: %+v, %v", kept, err)
	}
	e.calls = nil
	var out strings.Builder
	r := PackageRunner{Manifest: m, Sys: arch(), Exec: e, Recheck: p.Probe, Out: &out,
		Confirm: func(string) bool { return true }}
	if _, err := r.Run(context.Background(), pl); err != nil {
		t.Fatalf("%v\n%s", err, &out)
	}
	if got := strings.Join(e.calls, ";"); got != "pacman -Q herdr-bin;sudo pacman -R herdr-bin" {
		t.Fatalf("calls = %s", got)
	}
}

func pacmanRecipe(pkg string) map[string]manifest.InstallRecipe {
	return map[string]manifest.InstallRecipe{"arch": {Manager: "pacman", Package: pkg}}
}
