package clean

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

// NativeProbe gathers ownership evidence by asking each declared recipe's
// package database about that exact package. It never enumerates unrelated
// packages and never treats a failed or unparsable query as absence.
type NativeProbe struct {
	Sys  *detector.SystemInfo
	Exec Executor
	// Home is the absolute home directory; ~/.local is the npm prefix
	// restore installs into.
	Home string
	// Present reports presence signals (see PathPresence). It is required
	// because unknown presence must not look like absence.
	Present func(name string, app manifest.AppConfig) bool
}

// Probe implements OwnershipProbe. Script recipes are never queried or
// owned; available managers without a tested query make evidence unknown.
func (p NativeProbe) Probe(name string, app manifest.AppConfig) (Evidence, error) {
	if p.Sys == nil || p.Exec == nil || p.Present == nil || !filepath.IsAbs(p.Home) {
		return Evidence{}, fmt.Errorf("probe needs system information, an executor, presence and an absolute home")
	}
	ev := Evidence{Present: p.Present(name, app)}
	keys := make([]string, 0, len(app.Install))
	for k := range app.Install {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	found := map[Owner]bool{}
	for _, k := range keys {
		owners, err := p.query(app.Install[k])
		if err != nil {
			return Evidence{}, err
		}
		for _, o := range owners {
			found[o] = true
		}
	}
	for o := range found {
		ev.Owners = append(ev.Owners, o)
	}
	sort.Slice(ev.Owners, func(i, j int) bool { return fmt.Sprint(ev.Owners[i]) < fmt.Sprint(ev.Owners[j]) })
	return ev, nil
}

func (p NativeProbe) has(m detector.PackageManager) bool { return p.Sys.HasManager(m) }

func (p NativeProbe) query(r manifest.InstallRecipe) ([]Owner, error) {
	if r.Command != "" || r.Manager == "script" {
		return nil, nil
	}
	id := r.Package
	if r.Cask != "" {
		id = r.Cask
	}
	if id == "" || strings.HasPrefix(id, "-") {
		return nil, fmt.Errorf("invalid %s package name %q", r.Manager, id)
	}
	var owners []Owner
	add := func(o Owner, ok bool, err error) error {
		if ok {
			owners = append(owners, o)
		}
		return err
	}
	var err error
	switch r.Manager {
	case "brew":
		switch {
		case !p.has(detector.ManagerBrew), r.Cask != "" && p.Sys.OS != "darwin":
		case r.Cask != "":
			ok, e := p.listed([]string{"brew", "list", "--cask", "--versions", id}, path.Base(id), "")
			err = add(Owner{Manager: "brew", Cask: id}, ok, e)
		default:
			ok, e := p.listed([]string{"brew", "list", "--formula", "--versions", id}, path.Base(id), "")
			err = add(Owner{Manager: "brew", Package: id}, ok, e)
		}
	case "pacman", "yay":
		// AUR helpers install into the pacman database; only it is asked,
		// and the owner is that database record whichever recipe named it.
		if p.has(detector.ManagerPacman) {
			ok, e := p.listed([]string{"pacman", "-Q", id}, id, "was not found")
			err = add(Owner{Manager: "pacman", Package: id}, ok, e)
		} else if p.has(detector.ManagerYay) {
			err = fmt.Errorf("pacman database is not available to verify %s", id)
		}
	case "npm", "pnpm":
		if r.Manager == "npm" && p.has(detector.ManagerNpm) {
			if err = p.npm(id, add); err != nil {
				return nil, err
			}
		}
		if p.has(detector.ManagerPnpm) {
			ok, e := p.pnpm(id)
			err = add(Owner{Manager: "pnpm", Package: id}, ok, e)
		}
	default:
		if p.has(detector.PackageManager(r.Manager)) {
			err = fmt.Errorf("no tested ownership query for %s", r.Manager)
		}
	}
	return owners, err
}

// listed runs a per-package list query. Exit 0 must name exactly want; a
// silent exit 1 (or one whose stderr contains notFound) means absent.
func (p NativeProbe) listed(argv []string, want, notFound string) (bool, error) {
	res, err := p.Exec.Run(context.Background(), argv)
	switch {
	case err != nil:
		return false, fmt.Errorf("%s: %w", argv[0], err)
	case res.ExitCode == 0:
		if f := strings.Fields(string(res.Stdout)); len(f) > 0 && f[0] == want {
			return true, nil
		}
	case res.ExitCode == 1 && len(bytes.TrimSpace(res.Stdout)) == 0:
		stderr := strings.TrimSpace(string(res.Stderr))
		if stderr == "" || notFound != "" && strings.Contains(stderr, notFound) {
			return false, nil
		}
	}
	return false, fmt.Errorf("%s could not confirm whether %s is installed", argv[0], want)
}

type depTree struct {
	Dependencies map[string]struct {
		Version string `json:"version"`
		Missing bool   `json:"missing"`
	} `json:"dependencies"`
	Error    json.RawMessage `json:"error"`
	Problems []string        `json:"problems"`
}

// installed reads one dependency tree; anything but a clean tree is unknown.
func installed(t depTree, id string) (bool, error) {
	if len(t.Error) > 0 || len(t.Problems) > 0 {
		return false, fmt.Errorf("package tree reports problems")
	}
	d, ok := t.Dependencies[id]
	if ok && (d.Missing || d.Version == "") {
		return false, fmt.Errorf("package tree lists %s without an installed version", id)
	}
	return ok, nil
}

// npm checks the configured global prefix and ~/.local, reporting the exact
// prefix holding the package.
func (p NativeProbe) npm(id string, add func(Owner, bool, error) error) error {
	res, err := p.Exec.Run(context.Background(), []string{"npm", "prefix", "-g"})
	prefix := strings.TrimSpace(string(res.Stdout))
	if err != nil || res.ExitCode != 0 || !filepath.IsAbs(prefix) || strings.ContainsAny(prefix, "\n\r") {
		return fmt.Errorf("npm global prefix is unknown")
	}
	prefixes := []string{filepath.Clean(prefix)}
	if local := filepath.Join(p.Home, ".local"); local != prefixes[0] {
		prefixes = append(prefixes, local)
	}
	for _, pre := range prefixes {
		res, err := p.Exec.Run(context.Background(), []string{"npm", "ls", "-g", "--prefix", pre, "--depth=0", "--json", id})
		var t depTree
		if err != nil || res.ExitCode > 1 || json.Unmarshal(res.Stdout, &t) != nil {
			return fmt.Errorf("npm could not list %s in %s", id, pre)
		}
		ok, err := installed(t, id)
		if ok && res.ExitCode != 0 {
			err = fmt.Errorf("npm ls failed for %s in %s", id, pre)
		}
		if err := add(Owner{Manager: "npm", Package: id, Prefix: pre}, ok && err == nil, err); err != nil {
			return err
		}
	}
	return nil
}

func (p NativeProbe) pnpm(id string) (bool, error) {
	res, err := p.Exec.Run(context.Background(), []string{"pnpm", "ls", "-g", "--depth=0", "--json", id})
	var trees []depTree
	if err != nil || res.ExitCode != 0 || json.Unmarshal(res.Stdout, &trees) != nil {
		return false, fmt.Errorf("pnpm could not list %s", id)
	}
	found := false
	for _, t := range trees {
		ok, err := installed(t, id)
		if err != nil {
			return false, err
		}
		found = found || ok
	}
	return found, nil
}

// PathPresence reports whether any check entry (or the app name, when it
// declares none) exists: absolute and ~/ paths are Lstat-ed, other entries
// are looked up on PATH. It only ever adds caution; it is not ownership.
func PathPresence(home string) func(string, manifest.AppConfig) bool {
	return func(name string, app manifest.AppConfig) bool {
		checks := app.Check
		if len(checks) == 0 {
			checks = []string{name}
		}
		for _, c := range checks {
			if strings.HasPrefix(c, "~/") {
				c = filepath.Join(home, c[2:])
			}
			if filepath.IsAbs(c) {
				if _, err := os.Lstat(c); err == nil {
					return true
				}
			} else if _, err := exec.LookPath(c); err == nil {
				return true
			}
		}
		return false
	}
}

// OSExecutor runs argv directly with LC_ALL=C so query output is stable.
type OSExecutor struct{}

func (OSExecutor) Run(ctx context.Context, argv []string) (Result, error) {
	if len(argv) == 0 {
		return Result{}, fmt.Errorf("empty command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.Stdin = os.Stdin // sudo may prompt
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	res := Result{Stdout: out.Bytes(), Stderr: errb.Bytes()}
	if ee, ok := err.(*exec.ExitError); ok {
		res.ExitCode, err = ee.ExitCode(), nil
	}
	return res, err
}
