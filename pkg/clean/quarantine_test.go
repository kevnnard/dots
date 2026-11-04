package clean

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

const configSummary = `Configuration cleanup plan:
  blocked brew: not a path inside home: Brewfile
  quarantine claude: ~/.claude/settings.json
  blocked claude: ~/.claude/history.jsonl: matches a preserved pattern
  keep claude: mcp.json, history.jsonl
  quarantine gh: ~/.config/gh/config.yml
  blocked gh: ~/.config/gh/hosts.yml: may hold secrets, credentials or data
  blocked ghostty: ~/.config/ghostty: directories are not supported yet
  missing gone: ~/.gone
  blocked link: ~/.linked: is a symlink
  blocked link: ~/lnk/conf: a parent directory is a symlink
  no-config node
  blocked repo: ~/src/dots/conf: overlaps the repository
  quarantine zsh: ~/.zshrc
3 to quarantine, 7 blocked
`

func configManifest() *manifest.Manifest {
	paths := func(srcs ...string) []manifest.PathMapping {
		var out []manifest.PathMapping
		for _, s := range srcs {
			out = append(out, manifest.PathMapping{Src: s, Dest: "configs/x"})
		}
		return out
	}
	return &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"brew":    {Paths: paths("Brewfile")},
		"claude":  {Paths: paths("~/.claude/settings.json", "~/.claude/mcp.json", "~/.claude/history.jsonl"), Secrets: []string{"mcp.json"}, Exclude: []string{"history.jsonl"}},
		"gh":      {Paths: paths("~/.config/gh/config.yml", "~/.config/gh/hosts.yml")},
		"ghostty": {Paths: paths("~/.config/ghostty")},
		"gone":    {Paths: paths("~/.gone")},
		"link":    {Paths: paths("~/.linked", "~/lnk/conf")},
		"node":    {},
		"repo":    {Paths: paths("~/src/dots/conf")},
		"zsh":     {Paths: paths("~/.zshrc")},
	}}
}

// configHome builds an isolated home; the live home is never read.
func configHome(t *testing.T) (home, repo string) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{".zshrc", ".claude/settings.json", ".claude/mcp.json", ".claude/history.jsonl",
		".config/gh/config.yml", ".config/gh/hosts.yml", ".config/ghostty/config", ".target", "real/conf", "src/dots/conf"} {
		writeFile(t, filepath.Join(home, f), f)
	}
	for link, target := range map[string]string{".linked": ".target", "lnk": "real"} {
		if err := os.Symlink(filepath.Join(home, target), filepath.Join(home, link)); err != nil {
			t.Fatal(err)
		}
	}
	return home, filepath.Join(home, "src/dots")
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		b, _ := os.ReadFile(p) // directories and links read as empty
		out[p] = d.Type().String() + string(b)
		return err
	})
	return out
}

func configPlan(t *testing.T) *Plan {
	t.Helper()
	m := configManifest()
	p, err := BuildPlan(m, Request{Configs: Candidates(m)}, darwin(), (&fakeProbe{}).probe)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func configRunner(home, repo string) (ConfigRunner, *strings.Builder, *int) {
	var out strings.Builder
	renames := new(int)
	return ConfigRunner{Manifest: configManifest(), Home: home, Repo: repo, Out: &out,
		Rename: func(o, n string) error { *renames++; return os.Rename(o, n) }}, &out, renames
}

func yes(string) bool { return true }

func TestConfigRunnerPreviewDryRunAndCancelWriteNothing(t *testing.T) {
	home, repo := configHome(t)
	p := configPlan(t)
	before := snapshot(t, home)
	for name, tc := range map[string]struct {
		set  func(*ConfigRunner)
		tail string
	}{
		"dry-run": {func(r *ConfigRunner) {
			r.DryRun, r.Confirm = true, func(string) bool { t.Fatal("dry run asked for confirmation"); return true }
		}, "Dry run: nothing was changed.\n"},
		"default": {func(*ConfigRunner) {}, "Cancelled: nothing was changed.\n"},
		"decline": {func(r *ConfigRunner) { r.Confirm = func(string) bool { return false } }, "Cancelled: nothing was changed.\n"},
	} {
		r, out, renames := configRunner(home, repo)
		tc.set(&r)
		rep, err := r.Run(context.Background(), p)
		if err != nil || out.String() != configSummary+tc.tail || *renames != 0 || len(rep.Moved) != 0 || rep.Quarantine != "" {
			t.Fatalf("%s: err %v, renames %d, report %+v, output\n%s", name, err, *renames, rep, out)
		}
		if len(rep.Candidates) != 10 || rep.DryRun != (name == "dry-run") || rep.Cancelled != (name != "dry-run") {
			t.Fatalf("%s: report %+v", name, rep)
		}
		if !reflect.DeepEqual(before, snapshot(t, home)) {
			t.Fatalf("%s: home changed", name)
		}
	}
}

func TestConfigRunnerRejectsHostileInputWithoutWrites(t *testing.T) {
	home, repo := configHome(t)
	before := snapshot(t, home)
	edit := func(f func(*Plan)) *Plan { p := configPlan(t); f(p); return p }
	plans := map[string]*Plan{
		"traversal":  edit(func(p *Plan) { p.Configs[8].Paths = []string{"~/../etc/passwd"} }),
		"added path": edit(func(p *Plan) { p.Configs[8].Paths = append(p.Configs[8].Paths, "~/.ssh/config") }),
		"unblocked":  edit(func(p *Plan) { p.Configs[0].Status = ConfigQuarantine }),
		"dropped":    edit(func(p *Plan) { p.Configs[1].Preserve = nil }),
		"unknown":    edit(func(p *Plan) { p.Configs = append(p.Configs, ConfigItem{AppName: "ssh", Status: ConfigQuarantine}) }),
	}
	for name, p := range plans {
		r, out, renames := configRunner(home, repo)
		r.Confirm = func(string) bool { t.Fatalf("%s: asked for confirmation", name); return true }
		if rep, err := r.Run(context.Background(), p); err == nil || rep != nil || out.Len() != 0 || *renames != 0 {
			t.Fatalf("%s: report %+v, err %v, output %q", name, rep, err, out)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, tc := range map[string]struct {
		home, repo string
		ctx        context.Context
	}{
		"root home":       {"/", "", context.Background()},
		"relative home":   {"home", "", context.Background()},
		"repo is home":    {home, home, context.Background()},
		"cancelled first": {home, repo, cancelled},
	} {
		r, out, renames := configRunner(tc.home, tc.repo)
		r.Confirm = yes
		if rep, err := r.Run(tc.ctx, configPlan(t)); err == nil || rep != nil || out.Len() != 0 || *renames != 0 {
			t.Fatalf("%s: report %+v, err %v", name, rep, err)
		}
	}
	if !reflect.DeepEqual(before, snapshot(t, home)) {
		t.Fatal("home changed")
	}
}

func TestConfigRunnerQuarantinesAndRecoversWithoutOverwriting(t *testing.T) {
	home, repo := configHome(t)
	r, out, _ := configRunner(home, repo)
	r.Confirm = yes
	rep, err := r.Run(context.Background(), configPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := rep.Quarantine
	moved := []string{filepath.Join(home, ".claude/settings.json"), filepath.Join(home, ".config/gh/config.yml"), filepath.Join(home, ".zshrc")}
	if filepath.Dir(dir) != filepath.Join(home, QuarantineDir) || !reflect.DeepEqual(rep.Moved, moved) || len(rep.Failures) != 0 {
		t.Fatalf("report %+v", rep)
	}
	if want := configSummary + "quarantined ~/.claude/settings.json\nquarantined ~/.config/gh/config.yml\nquarantined ~/.zshrc\nRecover from " + dir + "\n"; out.String() != want {
		t.Fatalf("output\n%s\nwant\n%s", out, want)
	}
	for p, mode := range map[string]fs.FileMode{dir: fs.ModeDir | 0o700, filepath.Join(dir, "record.json"): 0o600} {
		if fi, err := os.Lstat(p); err != nil || fi.Mode() != mode {
			t.Fatalf("%s: %v %v", p, fi.Mode(), err)
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "2")); err != nil || string(b) != ".zshrc" {
		t.Fatalf("stored zshrc %q %v", b, err)
	}
	for _, p := range moved {
		if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s still present: %v", p, err)
		}
	}

	writeFile(t, filepath.Join(home, ".zshrc"), "new")
	recovery := func(r Recovery) (*ConfigReport, string, error) {
		var b strings.Builder
		r.Home, r.Repo, r.Dir, r.Out = home, repo, dir, &b
		rep, err := Recover(context.Background(), r)
		return rep, b.String(), err
	}
	preview := "Configuration recovery from " + dir + ":\n  restore claude: ~/.claude/settings.json\n  restore gh: ~/.config/gh/config.yml\n  conflict zsh: ~/.zshrc: destination exists\n2 to restore, 1 blocked\n"
	before := snapshot(t, home)
	for name, tc := range map[string]struct {
		r    Recovery
		tail string
	}{
		"dry-run": {Recovery{DryRun: true}, "Dry run: nothing was changed.\n"},
		"default": {Recovery{}, "Cancelled: nothing was changed.\n"},
	} {
		if rep, got, err := recovery(tc.r); err != nil || got != preview+tc.tail || len(rep.Moved) != 0 {
			t.Fatalf("%s: err %v report %+v output\n%s", name, err, rep, got)
		}
	}
	if !reflect.DeepEqual(before, snapshot(t, home)) {
		t.Fatal("preview changed home")
	}
	rep, got, err := recovery(Recovery{Confirm: yes})
	if err != nil || got != preview+"restored ~/.claude/settings.json\nrestored ~/.config/gh/config.yml\n" || !reflect.DeepEqual(rep.Moved, moved[:2]) {
		t.Fatalf("err %v report %+v output\n%s", err, rep, got)
	}
	if b, _ := os.ReadFile(moved[2]); string(b) != "new" {
		t.Fatalf("conflict overwritten: %q", b)
	}
	if b, _ := os.ReadFile(moved[0]); string(b) != ".claude/settings.json" {
		t.Fatalf("restored content %q", b)
	}
	if _, err := os.Lstat(filepath.Join(dir, "2")); err != nil {
		t.Fatalf("conflicting entry left the quarantine: %v", err)
	}
}

func TestRecoverRejectsForeignOrTamperedRecords(t *testing.T) {
	home, repo := configHome(t)
	r, _, _ := configRunner(home, repo)
	r.Confirm = yes
	rep, err := r.Run(context.Background(), configPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{home, filepath.Join(home, ".claude"), rep.Quarantine + "/../x"} {
		if _, err := Recover(context.Background(), Recovery{Home: home, Dir: dir, Confirm: yes, Out: &strings.Builder{}}); err == nil {
			t.Fatalf("%s: accepted", dir)
		}
	}
	recPath := filepath.Join(rep.Quarantine, "record.json")
	var rec record
	data, _ := os.ReadFile(recPath)
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	rec.Entries[0].Original, rec.Entries[1].Original, rec.Entries[2].Original = "/etc/passwd", home+"/x/../.ssh/config", filepath.Join(repo, "conf")
	data, _ = json.Marshal(rec)
	writeFile(t, recPath, string(data))
	var out strings.Builder
	got, err := Recover(context.Background(), Recovery{Home: home, Repo: repo, Dir: rep.Quarantine, Confirm: yes, Out: &out})
	want := "Configuration recovery from " + rep.Quarantine + ":\n" +
		"  blocked claude: /etc/passwd: outside home\n" +
		"  blocked gh: " + home + "/x/../.ssh/config: not a clean absolute path\n" +
		"  blocked zsh: ~/src/dots/conf: overlaps the repository\n0 to restore, 3 blocked\nNothing to restore.\n"
	if err != nil || out.String() != want || len(got.Moved) != 0 {
		t.Fatalf("err %v output\n%s\nwant\n%s", err, out.String(), want)
	}
}

func TestConfigRunnerRollsBackEveryMoveOnFailure(t *testing.T) {
	home, repo := configHome(t)
	before := snapshot(t, home)
	run := func(t *testing.T, set func(*ConfigRunner)) (*ConfigReport, error) {
		r, _, _ := configRunner(home, repo)
		r.Confirm = yes
		set(&r)
		return r.Run(context.Background(), configPlan(t))
	}
	calls := 0
	rep, err := run(t, func(r *ConfigRunner) {
		r.Rename = func(o, n string) error {
			if calls++; calls == 3 {
				return errors.New("injected")
			}
			return os.Rename(o, n)
		}
	})
	if err == nil || len(rep.Moved) != 0 || !reflect.DeepEqual(rep.Failures, []string{"~/.zshrc: injected"}) {
		t.Fatalf("err %v report %+v", err, rep)
	}
	after := snapshot(t, home)
	for p := range after {
		if strings.HasPrefix(p, filepath.Join(home, QuarantineDir)) {
			delete(after, p) // the empty record stays as evidence
		}
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rollback did not restore home")
	}

	settings := filepath.Join(home, ".claude/settings.json")
	rep, err = run(t, func(r *ConfigRunner) {
		r.Rename = func(o, n string) error {
			if strings.HasSuffix(o, "config.yml") {
				writeFile(t, settings, "intruder") // rollback must not replace it
				return errors.New("injected")
			}
			return os.Rename(o, n)
		}
	})
	want := []string{"~/.config/gh/config.yml: injected", "~/.claude/settings.json: not restored: destination exists"}
	if err == nil || !reflect.DeepEqual(rep.Failures, want) || !reflect.DeepEqual(rep.Moved, []string{settings}) {
		t.Fatalf("err %v report %+v", err, rep)
	}
	if b, _ := os.ReadFile(settings); string(b) != "intruder" {
		t.Fatalf("rollback overwrote %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(rep.Quarantine, "0")); string(b) != ".claude/settings.json" {
		t.Fatalf("original lost: %q", b)
	}
}

func TestConfigRunnerRefusesDriftAndCancellation(t *testing.T) {
	home, repo := configHome(t)
	zshrc := filepath.Join(home, ".zshrc")
	ctx, cancel := context.WithCancel(context.Background())
	for name, tc := range map[string]struct {
		ctx     context.Context
		confirm func(string) bool
		want    string
	}{
		"drift": {context.Background(), func(string) bool {
			writeFile(t, zshrc+".new", "swapped")
			return os.Rename(zshrc+".new", zshrc) == nil
		}, "~/.zshrc: changed since the preview"},
		"cancel": {ctx, func(string) bool { cancel(); return true }, "~/.claude/settings.json: context canceled"},
	} {
		before := snapshot(t, home)
		r, _, _ := configRunner(home, repo)
		r.Confirm = tc.confirm
		rep, err := r.Run(tc.ctx, configPlan(t))
		if err == nil || len(rep.Moved) != 0 || !reflect.DeepEqual(rep.Failures, []string{tc.want}) {
			t.Fatalf("%s: err %v report %+v", name, err, rep)
		}
		for p, v := range before {
			if got := snapshot(t, home)[p]; got != v && filepath.Base(p) != ".zshrc" {
				t.Fatalf("%s: %s not restored", name, p)
			}
		}
	}
}

func TestConfigRunnerPreservesSlashPatternsAndBlocksUncertainOnes(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{".app/settings.json", ".app/plugins/local.json", ".app/nested/dir/x.json", ".app/state.lock", ".app/keys.json", ".odd/conf.json"} {
		writeFile(t, filepath.Join(home, f), f)
	}
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"app": {Paths: []manifest.PathMapping{{Src: "~/.app/settings.json"}, {Src: "~/.app/plugins/local.json"},
			{Src: "~/.app/nested/dir/x.json"}, {Src: "~/.app/state.lock"}, {Src: "~/.app/keys.json"}},
			Secrets: []string{"keys.json"}, Exclude: []string{"plugins/local.json", "nested/dir/**", "*.lock"}},
		"odd": {Paths: []manifest.PathMapping{{Src: "~/.odd/conf.json"}}, Exclude: []string{"a/**/b", "/abs"}},
	}}
	p, err := BuildPlan(m, Request{Configs: Candidates(m)}, darwin(), (&fakeProbe{}).probe)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	r := ConfigRunner{Manifest: m, Home: home, Out: &out, Confirm: yes}
	rep, err := r.Run(context.Background(), p)
	want := `Configuration cleanup plan:
  quarantine app: ~/.app/settings.json
  blocked app: ~/.app/plugins/local.json: matches a preserved pattern
  blocked app: ~/.app/nested/dir/x.json: matches a preserved pattern
  blocked app: ~/.app/state.lock: matches a preserved pattern
  keep app: keys.json, plugins/local.json, nested/dir/**, *.lock
  blocked odd: ~/.odd/conf.json: uncertain preserve pattern: a/**/b
  keep odd: a/**/b, /abs
1 to quarantine, 4 blocked
quarantined ~/.app/settings.json
Recover from ` + rep.Quarantine + "\n"
	if err != nil || out.String() != want || !reflect.DeepEqual(rep.Moved, []string{filepath.Join(home, ".app/settings.json")}) {
		t.Fatalf("err %v moved %v output\n%s\nwant\n%s", err, rep.Moved, out.String(), want)
	}
	for _, f := range []string{".app/plugins/local.json", ".app/nested/dir/x.json", ".app/state.lock", ".app/keys.json", ".odd/conf.json"} {
		if b, err := os.ReadFile(filepath.Join(home, f)); err != nil || string(b) != f {
			t.Fatalf("%s moved or changed: %q %v", f, b, err)
		}
	}
}
