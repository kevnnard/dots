package discover

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/kevnnard/dots/pkg/clean"
	"github.com/kevnnard/dots/pkg/manifest"
)

type Item struct {
	Name string
	Kind string
	OS   []string
}

type Skip struct {
	Name   string
	Reason string
}

type Options struct {
	Exec      clean.Executor
	Home      string
	Brewfiles []string
}

type Result struct {
	New     []Item
	Skipped []Skip
	Apps    map[string]manifest.AppConfig
	Configs map[string]manifest.AppConfig
}

var (
	both            = []string{"darwin", "linux"}
	macOS           = []string{"darwin"}
	credentialDirs  = map[string]bool{"gcloud": true, "configstore": true, "github-copilot": true, "gh-copilot": true, "gh": true}
	defaultExcludes = []string{".DS_Store", "cache/**", "Cache/**", "logs/**", "*.log"}
	brewfileLine    = regexp.MustCompile(`^\s*(brew|cask|tap)\s+"([^"]+)"`)
)

func ForThisMachine(repoDir string) Options {
	var opts Options
	if home, err := os.UserHomeDir(); err == nil {
		opts.Home = home
	}
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("brew"); err == nil {
			opts.Exec = clean.OSExecutor{}
		}
	}
	opts.Brewfiles, _ = filepath.Glob(filepath.Join(repoDir, "Brewfile*"))
	return opts
}

func Run(ctx context.Context, m *manifest.Manifest, opts Options) (*Result, error) {
	res := &Result{Apps: map[string]manifest.AppConfig{}, Configs: map[string]manifest.AppConfig{}}
	if opts.Exec != nil {
		if err := discoverPackages(ctx, m, opts, res); err != nil {
			return nil, err
		}
	}
	if opts.Home != "" {
		if err := discoverConfigs(m, opts.Home, res); err != nil {
			return nil, err
		}
	}
	sort.Slice(res.New, func(i, j int) bool {
		if res.New[i].Name != res.New[j].Name {
			return res.New[i].Name < res.New[j].Name
		}
		return res.New[i].Kind < res.New[j].Kind
	})
	return res, nil
}

func (r *Result) Lines() []string {
	var lines []string
	if len(r.New) > 0 {
		lines = append(lines, "New:")
		for _, it := range r.New {
			lines = append(lines, fmt.Sprintf("  - %s (%s): %s", it.Name, it.Kind, strings.Join(it.OS, ", ")))
		}
	}
	if len(r.Skipped) > 0 {
		lines = append(lines, "Skipped:")
		for _, s := range r.Skipped {
			lines = append(lines, fmt.Sprintf("  - %s: %s", s.Name, s.Reason))
		}
	}
	return lines
}

func shortName(name string) string { return name[strings.LastIndex(name, "/")+1:] }

func tapOf(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) != 3 {
		return ""
	}
	return strings.ToLower(parts[0] + "/" + parts[1])
}

func declared(m *manifest.Manifest, brewfiles []string) (pkgs, taps map[string]bool) {
	pkgs, taps = map[string]bool{}, map[string]bool{}
	add := func(name string) {
		pkgs[shortName(name)] = true
		if t := tapOf(name); t != "" {
			taps[t] = true
		}
	}
	for name, app := range m.Apps {
		pkgs[name] = true
		for _, r := range app.Install {
			if r.Manager != "brew" {
				continue
			}
			for _, p := range strings.Fields(r.Package) {
				add(p)
			}
			if r.Cask != "" {
				add(r.Cask)
			}
		}
	}
	for _, path := range brewfiles {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			mt := brewfileLine.FindStringSubmatch(sc.Text())
			if mt == nil {
				continue
			}
			if mt[1] == "tap" {
				taps[strings.ToLower(mt[2])] = true
			} else {
				add(mt[2])
			}
		}
		f.Close()
	}
	return pkgs, taps
}

func list(ctx context.Context, ex clean.Executor, argv ...string) ([]string, error) {
	r, err := ex.Run(ctx, argv)
	if err == nil && r.ExitCode != 0 {
		err = fmt.Errorf("exit status %d: %s", r.ExitCode, strings.TrimSpace(string(r.Stderr)))
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
	}
	var out []string
	for _, l := range strings.Split(string(r.Stdout), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

type brewInfo struct {
	Formulae []struct {
		FullName string `json:"full_name"`
		Bottle   struct {
			Stable struct {
				Files map[string]json.RawMessage `json:"files"`
			} `json:"stable"`
		} `json:"bottle"`
		Requirements []struct {
			Name string `json:"name"`
		} `json:"requirements"`
	} `json:"formulae"`
	Casks []struct {
		Token     string `json:"token"`
		FullToken string `json:"full_token"`
		Tap       string `json:"tap"`
	} `json:"casks"`
}

func discoverPackages(ctx context.Context, m *manifest.Manifest, opts Options, res *Result) error {
	formulae, err := list(ctx, opts.Exec, "brew", "list", "--formula", "--installed-on-request", "--full-name")
	if err != nil {
		return err
	}
	casks, err := list(ctx, opts.Exec, "brew", "list", "--cask")
	if err != nil {
		return err
	}
	tapped, err := list(ctx, opts.Exec, "brew", "tap")
	if err != nil {
		return err
	}
	pkgs, taps := declared(m, opts.Brewfiles)
	undeclared := func(names []string) []string {
		var out []string
		for _, n := range names {
			if !pkgs[shortName(n)] {
				out = append(out, n)
			}
		}
		sort.Strings(out)
		return out
	}
	formulae, casks = undeclared(formulae), undeclared(casks)

	var info brewInfo
	if len(formulae)+len(casks) > 0 {
		argv := append([]string{"brew", "info", "--json=v2"}, formulae...)
		if r, err := opts.Exec.Run(ctx, append(argv, casks...)); err == nil && r.ExitCode == 0 {
			_ = json.Unmarshal(r.Stdout, &info)
		}
	}

	for _, name := range formulae {
		osList := macOS
		for _, f := range info.Formulae {
			if f.FullName == name || shortName(f.FullName) == name {
				osList = formulaOS(f.Bottle.Stable.Files, f.Requirements)
			}
		}
		key := "darwin"
		if len(osList) == 2 {
			key = "brew"
		}
		addApp(res, shortName(name), "formula", name, osList, manifest.AppConfig{
			Description: "Homebrew formula discovered by dots backup",
			Install:     map[string]manifest.InstallRecipe{key: {Manager: "brew", Package: name}},
		})
		markTap(taps, name)
	}
	for _, token := range casks {
		full := token
		for _, c := range info.Casks {
			if c.Token == shortName(token) && c.Tap != "" && !strings.HasPrefix(c.Tap, "homebrew/") && c.FullToken != "" {
				full = c.FullToken
			}
		}
		addApp(res, shortName(token), "cask", full, macOS, manifest.AppConfig{
			Description: "Homebrew cask discovered by dots backup",
			Install:     map[string]manifest.InstallRecipe{"darwin": {Manager: "brew", Cask: full}},
		})
		markTap(taps, full)
	}
	for _, tap := range tapped {
		tap = strings.ToLower(tap)
		if taps[tap] || strings.HasPrefix(tap, "homebrew/") {
			continue
		}
		addApp(res, "tap-"+strings.ReplaceAll(tap, "/", "-"), "tap", tap, both, manifest.AppConfig{
			Description: "Homebrew tap discovered by dots backup",
			Install:     map[string]manifest.InstallRecipe{"brew": {Manager: "brew", Command: "brew tap " + tap}},
		})
	}
	return nil
}

func markTap(taps map[string]bool, name string) {
	if t := tapOf(name); t != "" {
		taps[t] = true
	}
}

func formulaOS(files map[string]json.RawMessage, reqs []struct {
	Name string `json:"name"`
}) []string {
	for tag := range files {
		if strings.HasSuffix(tag, "_linux") {
			return both
		}
	}
	macOnly := len(files) > 0
	for _, r := range reqs {
		switch r.Name {
		case "linux":
			return both
		case "macos", "xcode":
			macOnly = true
		}
	}
	if macOnly {
		return macOS
	}
	return both
}

func addApp(res *Result, key, kind, display string, osList []string, app manifest.AppConfig) {
	app.OS = osList
	res.Apps[key] = app
	res.New = append(res.New, Item{Name: display, Kind: kind, OS: osList})
}

func coveredSet(m *manifest.Manifest, home string) []string {
	var srcs []string
	expand := func(s string) {
		switch {
		case s == "~":
			s = home
		case strings.HasPrefix(s, "~/"):
			s = filepath.Join(home, s[2:])
		}
		if filepath.IsAbs(s) {
			srcs = append(srcs, filepath.Clean(s))
		}
	}
	for _, app := range m.Apps {
		for _, p := range app.Paths {
			expand(p.Src)
			for _, s := range p.SrcOS {
				expand(s)
			}
		}
	}
	return srcs
}

func isCovered(entry string, srcs []string) bool {
	sep := string(filepath.Separator)
	for _, s := range srcs {
		if s == entry || strings.HasPrefix(s, entry+sep) || strings.HasPrefix(entry, s+sep) {
			return true
		}
	}
	return false
}

func discoverConfigs(m *manifest.Manifest, home string, res *Result) error {
	dir := filepath.Join(home, ".config")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	srcs := coveredSet(m, home)
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(dir, name)
		switch {
		case strings.HasPrefix(name, "."), isCovered(full, srcs):
			continue
		case credentialDirs[name]:
			res.Skipped = append(res.Skipped, Skip{Name: name, Reason: "may hold credentials"})
			continue
		case strings.Contains(strings.ToLower(name), "cache"):
			res.Skipped = append(res.Skipped, Skip{Name: name, Reason: "cache"})
			continue
		}
		info, err := os.Stat(full)
		if err != nil || !(info.IsDir() || info.Mode().IsRegular()) {
			continue
		}
		key := name
		path := manifest.PathMapping{Src: "~/.config/" + name, Dest: "configs/" + name}
		if !info.IsDir() {
			if trimmed := strings.TrimSuffix(name, filepath.Ext(name)); trimmed != "" {
				key = trimmed
			}
			path.Dest = "configs/" + key + "/" + name
		}
		app, discovered := res.Apps[key]
		existing, declaredApp := m.Apps[key]
		listed := both
		switch {
		case discovered:
			listed = app.OS
		case declaredApp:
			app = existing
			if !installsOnLinux(existing) {
				listed = macOS
				if len(app.OS) == 0 {
					app.OS = macOS
				}
			}
			path.OS = listed
		default:
			app = manifest.AppConfig{Description: "Config discovered by dots backup", OS: both, Exclude: defaultExcludes}
		}
		app.Paths = append(append([]manifest.PathMapping(nil), app.Paths...), path)
		res.Apps[key] = app
		res.Configs[key] = manifest.AppConfig{
			Paths:   []manifest.PathMapping{path},
			Exclude: withDefaults(app.Exclude),
			Secrets: app.Secrets,
		}
		res.New = append(res.New, Item{Name: key, Kind: "config", OS: listed})
	}
	return nil
}

func installsOnLinux(app manifest.AppConfig) bool {
	if len(app.OS) > 0 {
		return app.AppliesTo("linux")
	}
	if len(app.Install) == 0 {
		return true
	}
	for key, r := range app.Install {
		switch key {
		case "darwin", "windows", "winget", "scoop", "choco":
			continue
		}
		if r.Cask == "" {
			return true
		}
	}
	return false
}

func withDefaults(excludes []string) []string {
	out := append([]string(nil), excludes...)
	have := map[string]bool{}
	for _, e := range out {
		have[e] = true
	}
	for _, e := range defaultExcludes {
		if !have[e] {
			out = append(out, e)
		}
	}
	return out
}
