package restore

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
)

// PlanStatus describes what the installer should do with one app.
type PlanStatus string

const (
	StatusInstall        PlanStatus = "install"         // run InstallCmd
	StatusInstalled      PlanStatus = "installed"       // probe found it; nothing to run
	StatusConfigOnly     PlanStatus = "config-only"     // no package; only configuration
	StatusUnsupported    PlanStatus = "unsupported"     // no recipe for this platform
	StatusManagerMissing PlanStatus = "manager-missing" // recipe exists but its manager is absent
	StatusBlocked        PlanStatus = "blocked"         // a required app cannot be installed
)

// Step is one entry of an ordered installation plan.
type Step struct {
	AppName     string     `json:"app_name"`
	Description string     `json:"description"`
	Status      PlanStatus `json:"status"`
	Manager     string     `json:"manager,omitempty"`
	InstallCmd  string     `json:"install_cmd,omitempty"`
	Reason      string     `json:"reason,omitempty"`
	Requested   bool       `json:"requested"` // false when pulled in as a dependency
}

// InstalledProbe reports whether an app is already present on the machine.
type InstalledProbe func(name string, app manifest.AppConfig) bool

// PlanOptions tunes BuildPlan.
type PlanOptions struct {
	// WithConfig also selects each app's config_requires, for restores that
	// deploy the backed-up configuration. Stock installs leave it false.
	WithConfig bool
	// Probe detects existing installs. Nil treats nothing as installed.
	Probe InstalledProbe
}

// Select returns the sorted non-optional apps matching profile ("" or "all"
// means every app). Optional apps are only installed when named explicitly.
func Select(m *manifest.Manifest, profile string) []string {
	var names []string
	for name, app := range m.Apps {
		if !app.Optional && app.MatchesProfile(profile) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// ValidateGraph rejects references to unknown apps and dependency cycles
// anywhere in the manifest.
func ValidateGraph(m *manifest.Manifest) error {
	names := make([]string, 0, len(m.Apps))
	for name, app := range m.Apps {
		names = append(names, name)
		for _, ref := range append(append([]string{}, app.Requires...), app.ConfigRequires...) {
			if _, ok := m.Apps[ref]; !ok {
				return fmt.Errorf("app %q references unknown app %q", name, ref)
			}
		}
	}
	sort.Strings(names)
	_, err := order(m, names, true)
	return err
}

// BuildPlan expands selected apps with their dependencies (and config
// prerequisites when opts.WithConfig is set), orders every app after the apps
// it requires and resolves the recipe usable on sys.
func BuildPlan(m *manifest.Manifest, selected []string, sys *detector.SystemInfo, opts PlanOptions) ([]Step, error) {
	if sys == nil {
		return nil, fmt.Errorf("system information is required")
	}
	if err := ValidateGraph(m); err != nil {
		return nil, err
	}
	requested := map[string]bool{}
	for _, name := range selected {
		if _, ok := m.Apps[name]; !ok {
			return nil, fmt.Errorf("unknown app %q", name)
		}
		requested[name] = true
	}
	closure := map[string]bool{}
	var collect func(name string)
	collect = func(name string) {
		if closure[name] {
			return
		}
		closure[name] = true
		app := m.Apps[name]
		for _, dep := range app.Requires {
			collect(dep)
		}
		if opts.WithConfig {
			for _, dep := range app.ConfigRequires {
				collect(dep)
			}
		}
	}
	for _, name := range selected {
		collect(name)
	}
	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)
	ordered, err := order(m, names, opts.WithConfig)
	if err != nil {
		return nil, err
	}

	provided := map[string]bool{}
	status := map[string]PlanStatus{}
	steps := make([]Step, 0, len(ordered))
	for _, name := range ordered {
		app := m.Apps[name]
		// Only hard requirements gate the package itself; config_requires
		// order the plan and gate configuration deployment in Run.
		step := planStep(name, app, app.Requires, sys, provided, status, opts.Probe)
		step.Requested = requested[name]
		if step.Status == StatusInstall || step.Status == StatusInstalled {
			for _, p := range app.Provides {
				provided[p] = true
			}
		}
		status[name] = step.Status
		steps = append(steps, step)
	}
	return steps, nil
}

func planStep(name string, app manifest.AppConfig, deps []string, sys *detector.SystemInfo, provided map[string]bool, status map[string]PlanStatus, probe InstalledProbe) Step {
	step := Step{AppName: name, Description: app.Description}
	if len(app.Install) == 0 {
		step.Status = StatusConfigOnly
		return step
	}
	if probe != nil && probe(name, app) {
		step.Status = StatusInstalled
		return step
	}
	for _, dep := range deps {
		switch status[dep] {
		case StatusUnsupported, StatusManagerMissing, StatusBlocked:
			step.Status = StatusBlocked
			step.Reason = fmt.Sprintf("requires %s (%s)", dep, status[dep])
			return step
		}
	}
	recipe, missing, ok := resolveRecipe(app.Install, sys, provided)
	switch {
	case ok:
		step.Status = StatusInstall
		step.Manager = recipe.Manager
		step.InstallCmd = buildInstallCommand(recipe, sys)
	case missing != "":
		step.Status = StatusManagerMissing
		step.Manager = missing
		step.Reason = fmt.Sprintf("recipe needs %s, which is not available", missing)
	default:
		step.Status = StatusUnsupported
		step.Reason = fmt.Sprintf("no install recipe for %s/%s", sys.OS, sys.Distro)
	}
	return step
}

// resolveRecipe walks recipe keys in the same priority as findBestRecipe
// (distro, managers, OS, default), plus the platform's native managers, and
// returns the first recipe whose manager is usable. When recipes target this
// platform but none is usable, it returns the first missing manager.
func resolveRecipe(recipes map[string]manifest.InstallRecipe, sys *detector.SystemInfo, provided map[string]bool) (manifest.InstallRecipe, string, bool) {
	keys := []string{string(sys.Distro), string(sys.PrimaryManager)}
	for _, mgr := range sys.Managers {
		keys = append(keys, string(mgr))
	}
	for _, mgr := range detector.NativeManagers(sys.OS, sys.Distro) {
		keys = append(keys, string(mgr))
	}
	keys = append(keys, sys.OS, "default")

	missing := ""
	seen := map[string]bool{}
	for _, key := range keys {
		r, ok := recipes[key]
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		if r.Cask != "" && sys.OS != "darwin" {
			continue // Homebrew casks only exist on macOS
		}
		if managerUsable(r.Manager, sys, provided) {
			return r, "", true
		}
		if missing == "" {
			missing = r.Manager
		}
	}
	return manifest.InstallRecipe{}, missing, false
}

func managerUsable(mgr string, sys *detector.SystemInfo, provided map[string]bool) bool {
	return mgr == "script" || provided[mgr] || sys.HasManager(detector.PackageManager(mgr))
}

// prerequisites returns the apps that must be planned before name: its
// Requires and, when withConfig is set, its ConfigRequires. A config
// dependency that itself requires name (a plugin of name) is left out, since
// it can only be installed after name; that edge is satisfied afterwards.
func prerequisites(m *manifest.Manifest, name string, withConfig bool) []string {
	deps := append([]string{}, m.Apps[name].Requires...)
	if !withConfig {
		return deps
	}
	for _, dep := range m.Apps[name].ConfigRequires {
		if !requiresTransitively(m, dep, name) {
			deps = append(deps, dep)
		}
	}
	return deps
}

// requiresTransitively reports whether from reaches to through one or more
// Requires edges.
func requiresTransitively(m *manifest.Manifest, from, to string) bool {
	seen := map[string]bool{}
	stack := append([]string{}, m.Apps[from].Requires...)
	for len(stack) > 0 {
		name := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if name == to {
			return true
		}
		if !seen[name] {
			seen[name] = true
			stack = append(stack, m.Apps[name].Requires...)
		}
	}
	return false
}

// order returns names topologically sorted by prerequisites (dependencies
// first), visiting in the given order for determinism. It fails on cycles.
func order(m *manifest.Manifest, names []string, withConfig bool) ([]string, error) {
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]int{}
	var out []string
	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		switch state[name] {
		case done:
			return nil
		case visiting:
			return fmt.Errorf("dependency cycle: %s", strings.Join(append(path, name), " -> "))
		}
		state[name] = visiting
		path = append(append([]string{}, path...), name)
		for _, dep := range prerequisites(m, name, withConfig) {
			if err := visit(dep, path); err != nil {
				return err
			}
		}
		state[name] = done
		out = append(out, name)
		return nil
	}
	for _, name := range names {
		if err := visit(name, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// CommandProbe is the default InstalledProbe: an app counts as installed when
// any Check entry (or the app name when Check is empty) is on PATH. Entries
// starting with "~" or "/" are checked as filesystem paths, which covers
// macOS app bundles. It never runs package-manager queries.
func CommandProbe(name string, app manifest.AppConfig) bool {
	checks := app.Check
	if len(checks) == 0 {
		checks = []string{name}
	}
	for _, c := range checks {
		if strings.HasPrefix(c, "~") || strings.HasPrefix(c, "/") {
			if p, err := manifest.ExpandPath(c); err == nil {
				if _, err := os.Stat(p); err == nil {
					return true
				}
			}
			continue
		}
		if _, err := exec.LookPath(c); err == nil {
			return true
		}
	}
	return false
}
