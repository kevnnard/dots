package clean

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
	"github.com/kevnnard/dots/pkg/restore"
)

// Candidates returns every declared app, optional ones included, sorted. It
// is the full selection offered to "all"; nothing outside the manifest is
// ever a cleanup candidate.
func Candidates(m *manifest.Manifest) []string {
	names := make([]string, 0, len(m.Apps))
	for name := range m.Apps {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// BuildPlan previews the cleanup of exactly the requested apps. Package items
// are ordered dependents first; a package is only removable when probe
// evidence shows a single declared native owner and no kept app requires it.
// Configuration items never consult the probe.
func BuildPlan(m *manifest.Manifest, req Request, sys *detector.SystemInfo, probe OwnershipProbe) (*Plan, error) {
	if sys == nil || probe == nil {
		return nil, fmt.Errorf("system information and an ownership probe are required")
	}
	if err := restore.ValidateGraph(m); err != nil {
		return nil, err
	}
	pkgs, err := exact(m, req.Packages)
	if err != nil {
		return nil, err
	}
	configs, err := exact(m, req.Configs)
	if err != nil {
		return nil, err
	}
	p := &Plan{Packages: []PackageItem{}, Configs: []ConfigItem{}}
	for _, name := range configs {
		p.Configs = append(p.Configs, planConfig(name, m.Apps[name]))
	}

	pl := packagePlanner{m: m, sys: sys, probe: probe, evidence: map[string]result{},
		removing: map[string]bool{}, removedBy: map[Owner]string{}, selected: set(pkgs), configs: set(configs)}
	for _, name := range dependentsFirst(m, pkgs) {
		p.Packages = append(p.Packages, pl.plan(name))
	}
	return p, nil
}

// exact de-duplicates names and rejects unknown apps without adding any.
func exact(m *manifest.Manifest, names []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, name := range names {
		if _, ok := m.Apps[name]; !ok {
			return nil, fmt.Errorf("unknown app %q", name)
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

func set(names []string) map[string]bool {
	s := map[string]bool{}
	for _, n := range names {
		s[n] = true
	}
	return s
}

// dependentsFirst orders the selected apps by inverse Requires, following
// edges through unselected apps so transitive order is kept.
func dependentsFirst(m *manifest.Manifest, selected []string) []string {
	want := set(selected)
	done := map[string]bool{}
	var post []string
	var visit func(string)
	visit = func(name string) {
		if done[name] {
			return
		}
		done[name] = true // ValidateGraph already rejected cycles
		for _, dep := range m.Apps[name].Requires {
			visit(dep)
		}
		if want[name] {
			post = append(post, name)
		}
	}
	for _, name := range selected {
		visit(name)
	}
	for i, j := 0, len(post)-1; i < j; i, j = i+1, j-1 {
		post[i], post[j] = post[j], post[i]
	}
	return post
}

type result struct {
	ev  Evidence
	err error
}

type packagePlanner struct {
	m         *manifest.Manifest
	sys       *detector.SystemInfo
	probe     OwnershipProbe
	evidence  map[string]result
	removing  map[string]bool
	removedBy map[Owner]string
	selected  map[string]bool
	configs   map[string]bool
}

func (pl *packagePlanner) evidenceFor(name string) result {
	r, ok := pl.evidence[name]
	if !ok {
		r.ev, r.err = pl.probe(name, pl.m.Apps[name])
		pl.evidence[name] = r
	}
	return r
}

func (pl *packagePlanner) plan(name string) PackageItem {
	app := pl.m.Apps[name]
	it := PackageItem{AppName: name}
	if len(app.Install) == 0 {
		it.Status = PackageConfigOnly // bundle-style apps own no packages
		return it
	}
	r := pl.evidenceFor(name)
	switch {
	case r.err != nil:
		return blocked(it, "ownership unknown: "+r.err.Error())
	case len(r.ev.Owners) > 1:
		return blocked(it, fmt.Sprintf("ambiguous ownership: %d package records", len(r.ev.Owners)))
	case len(r.ev.Owners) == 0 && r.ev.Present:
		return blocked(it, "present but no native package owns it")
	case len(r.ev.Owners) == 0:
		it.Status = PackageNotInstalled
		return it
	}
	owner := r.ev.Owners[0]
	it.Owner = &owner
	if owner.Manager == "script" {
		it.Status, it.Reason = PackageUnsupported, "script installs have no safe inverse"
		return it
	}
	if !declared(app, owner) {
		return blocked(it, "owner does not match any declared recipe")
	}
	if first, ok := pl.removedBy[owner]; ok {
		it.Status, it.Reason = PackageDuplicate, "same package as "+first
		return it
	}
	if reason := pl.keptDependent(name); reason != "" {
		return blocked(it, reason)
	}
	if reason := pl.keptSharer(name, owner); reason != "" {
		return blocked(it, reason)
	}
	argv, status, reason := uninstallArgv(owner, pl.sys)
	if argv == nil {
		it.Status, it.Reason = status, reason
		return it
	}
	it.Status, it.Argv = PackageRemove, argv
	pl.removing[name] = true
	pl.removedBy[owner] = name
	for _, host := range Candidates(pl.m) {
		for _, dep := range pl.m.Apps[host].ConfigRequires {
			if dep == name && !pl.configs[host] {
				it.Warnings = append(it.Warnings, host+" configuration expects "+name)
			}
		}
	}
	return it
}

// keptDependent explains why name must stay: an app that requires it is not
// being removed and is installed, or its evidence is unknown.
func (pl *packagePlanner) keptDependent(name string) string {
	for _, dep := range Candidates(pl.m) {
		if pl.removing[dep] || !contains(pl.m.Apps[dep].Requires, name) {
			continue
		}
		r := pl.evidenceFor(dep)
		if r.err != nil || r.ev.Present || len(r.ev.Owners) > 0 {
			return "required by " + dep + ", which is kept"
		}
	}
	return ""
}

// keptSharer explains why owner must stay: an unselected app declares the
// same package and is installed, or its evidence is unknown.
func (pl *packagePlanner) keptSharer(name string, owner Owner) string {
	for _, other := range Candidates(pl.m) {
		if other == name || pl.selected[other] || !declared(pl.m.Apps[other], owner) {
			continue
		}
		r := pl.evidenceFor(other)
		if r.err != nil || r.ev.Present || len(r.ev.Owners) > 0 {
			return "same package is declared by " + other + ", which is kept"
		}
	}
	return ""
}

func blocked(it PackageItem, reason string) PackageItem {
	it.Status, it.Reason = PackageBlocked, reason
	return it
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// declared reports whether owner is the identity of one of the app's native
// recipes. npm recipes may be installed through pnpm (see restore), and yay
// recipes are owned by their pacman database record (see NativeProbe).
func declared(app manifest.AppConfig, o Owner) bool {
	for _, r := range app.Install {
		if r.Command != "" || !(r.Manager == o.Manager || r.Manager == "npm" && o.Manager == "pnpm" ||
			r.Manager == "yay" && o.Manager == "pacman") {
			continue
		}
		if o.Cask != "" && o.Package == "" && r.Cask == o.Cask {
			return true
		}
		if o.Package != "" && o.Cask == "" && r.Cask == "" && r.Package == o.Package {
			return true
		}
	}
	return false
}

// uninstallArgv maps an owner to its tested native uninstall. It never uses
// force, zap, purge or autoremove flags, so managers keep refusing to remove
// packages other packages still depend on.
func uninstallArgv(o Owner, sys *detector.SystemInfo) ([]string, PackageStatus, string) {
	id := o.Package
	if o.Cask != "" {
		id = o.Cask
	}
	if strings.HasPrefix(id, "-") {
		return nil, PackageBlocked, "package name looks like a flag"
	}
	if o.Prefix != "" && !path.IsAbs(o.Prefix) {
		return nil, PackageBlocked, "prefix is not an absolute path"
	}
	if !sys.HasManager(detector.PackageManager(o.Manager)) {
		return nil, PackageBlocked, o.Manager + " is not available"
	}
	switch {
	case o.Manager == "brew" && o.Cask != "" && sys.OS == "darwin":
		return []string{"brew", "uninstall", "--cask", id}, "", ""
	case o.Manager == "brew" && o.Cask == "":
		return []string{"brew", "uninstall", "--formula", id}, "", ""
	case o.Manager == "pacman":
		return []string{"sudo", "pacman", "-R", id}, "", ""
	case o.Manager == "npm" && o.Prefix != "":
		return []string{"npm", "uninstall", "-g", "--prefix", o.Prefix, id}, "", ""
	case o.Manager == "npm":
		return []string{"npm", "uninstall", "-g", id}, "", ""
	case o.Manager == "pnpm" && o.Prefix == "":
		return []string{"pnpm", "remove", "-g", id}, "", ""
	}
	return nil, PackageUnsupported, "no tested uninstall for " + o.Manager
}

// planConfig lists the home paths a runner may quarantine. Secrets stay in
// place; repository files and paths that escape home block the whole item.
func planConfig(name string, app manifest.AppConfig) ConfigItem {
	it := ConfigItem{AppName: name, Status: ConfigQuarantine}
	if len(app.Paths) == 0 {
		it.Status = ConfigNone
		return it
	}
	it.Preserve = append(append([]string{}, app.Secrets...), app.Exclude...)
	for _, p := range app.Paths {
		clean := path.Clean(p.Src)
		if !strings.HasPrefix(p.Src, "~/") || clean == "~" || strings.Contains(p.Src, "..") {
			it.Status, it.Paths, it.Reason = ConfigBlocked, nil, "not a path inside home: "+p.Src
			return it
		}
		if contains(app.Secrets, path.Base(clean)) {
			continue
		}
		it.Paths = append(it.Paths, clean)
	}
	return it
}
