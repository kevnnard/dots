package restore

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/homepath"
	"github.com/kevnnard/dots/pkg/manifest"
)

type AppPlan struct {
	AppName     string `json:"app_name"`
	Description string `json:"description"`
	InstallCmd  string `json:"install_cmd,omitempty"`
	NeedInstall bool   `json:"need_install"`
	RecipeFound bool   `json:"recipe_found"`
}

type ApplyResult struct {
	AppName       string   `json:"app_name"`
	Restored      []string `json:"restored"`
	LocalBackups  []string `json:"local_backups"`
	TemplatesInit []string `json:"templates_initialized"`
	Skipped       []string `json:"skipped"`
	Kept          []string `json:"kept"`
	Warnings      []string `json:"warnings"`
	// Repaired lists files deployed with known incompatible backup content
	// repaired; NotRepaired lists known problems left in place.
	Repaired    []string `json:"repaired,omitempty"`
	NotRepaired []string `json:"not_repaired,omitempty"`
}

// PlanApp generates an installation plan for a single application based on current system.
func PlanApp(appName string, app manifest.AppConfig, sys *detector.SystemInfo) *AppPlan {
	plan := &AppPlan{
		AppName:     appName,
		Description: app.Description,
	}

	recipe, found := findBestRecipe(app.Install, sys)
	if !found {
		plan.RecipeFound = false
		return plan
	}

	plan.RecipeFound = true
	plan.InstallCmd = buildInstallCommand(recipe, sys)
	return plan
}

// PlanAll builds installation plans for all apps in the manifest. Plans are
// deterministic (sorted by name) and ordered so that every app comes after the
// apps it requires. Unknown dependencies are ignored and cycles are broken.
func PlanAll(m *manifest.Manifest, sys *detector.SystemInfo) []*AppPlan {
	names := make([]string, 0, len(m.Apps))
	for name := range m.Apps {
		names = append(names, name)
	}
	sort.Strings(names)

	var plans []*AppPlan
	visited := make(map[string]bool, len(names))
	var visit func(name string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		app, ok := m.Apps[name]
		if !ok {
			return
		}
		for _, dep := range app.Requires {
			visit(dep)
		}
		plans = append(plans, PlanApp(name, app, sys))
	}
	for _, name := range names {
		visit(name)
	}
	return plans
}

// ApplyOptions tunes ApplyAppFilesWith.
type ApplyOptions struct {
	DryRun bool
	// Replace authorizes replacing existing local files whose content differs
	// from the repository. Each one is replaced only after its timestamped
	// safety backup succeeds. Without it such files are kept and reported.
	Replace bool
}

// ApplyAppFiles copies configuration files from the dotfiles repository to
// the destination, replacing differing local files after a safety backup.
func ApplyAppFiles(appName string, app manifest.AppConfig, repoDir string, dryRun bool) (*ApplyResult, error) {
	return ApplyAppFilesWith(appName, app, repoDir, ApplyOptions{DryRun: dryRun, Replace: true})
}

// ApplyAppFilesWith copies configuration files from the dotfiles repository
// to the destination. Missing files are created, templates are only deployed
// when absent, and existing files are handled according to opts.Replace.
// Writes are confined to the mapping's authorized root (see newWriteGuard);
// paths whose existing ancestry resolves outside it are refused.
func ApplyAppFilesWith(appName string, app manifest.AppConfig, repoDir string, opts ApplyOptions) (*ApplyResult, error) {
	res := &ApplyResult{
		AppName: appName,
	}
	dryRun := opts.DryRun

	for _, mapping := range app.Paths {
		repoPath := filepath.Join(repoDir, mapping.Dest)
		targetExpanded, err := manifest.ExpandPath(mapping.Src)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("cannot expand target path %s: %v", mapping.Src, err))
			continue
		}
		guard, err := newWriteGuard(targetExpanded)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s not restored: %v", targetExpanded, err))
			continue
		}

		info, err := os.Stat(repoPath)
		if err != nil {
			// Check if a .template exists instead
			templatePath := repoPath + ".template"
			if tInfo, tErr := os.Stat(templatePath); tErr == nil && !tInfo.IsDir() {
				if _, targetErr := os.Lstat(targetExpanded); os.IsNotExist(targetErr) {
					deployTemplate(guard, templatePath, targetExpanded, dryRun, res)
					continue
				}
			}

			res.Skipped = append(res.Skipped, fmt.Sprintf("%s (not found in dotfiles repo)", mapping.Dest))
			continue
		}

		if info.IsDir() {
			if err := guard.check(targetExpanded); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s not restored: %v", targetExpanded, err))
				continue
			}
			err := filepath.Walk(repoPath, func(path string, fInfo os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				rel, err := filepath.Rel(repoPath, path)
				if err != nil || rel == "." {
					return nil
				}

				destFile := filepath.Join(targetExpanded, rel)

				if fInfo.Mode()&os.ModeSymlink != 0 {
					placeSymlink(guard, path, destFile, opts, res)
					return nil
				}

				if fInfo.IsDir() {
					if err := guard.check(destFile); err != nil {
						res.Warnings = append(res.Warnings, fmt.Sprintf("%s not restored: %v", destFile, err))
						return filepath.SkipDir
					}
					if !dryRun {
						return os.MkdirAll(destFile, 0755)
					}
					return nil
				}

				if strings.HasSuffix(rel, ".template") {
					realDest := strings.TrimSuffix(destFile, ".template")
					if _, err := os.Lstat(realDest); os.IsNotExist(err) {
						deployTemplate(guard, path, realDest, dryRun, res)
					}
					return nil
				}

				placeFile(guard, path, destFile, opts, res)
				return nil
			})
			if err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("error restoring dir %s: %v", mapping.Dest, err))
			}
		} else {
			placeFile(guard, repoPath, targetExpanded, opts, res)
		}
	}

	return res, nil
}

func deployTemplate(g writeGuard, src, dest string, dryRun bool, res *ApplyResult) {
	if err := g.check(filepath.Dir(dest)); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("template %s not deployed: %v", dest, err))
		return
	}
	if !dryRun {
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("failed deploying template to %s: %v", dest, err))
			return
		}
		// Templates only fill gaps: exclusive creation never follows or
		// truncates anything that appeared at dest.
		if err := homepath.CopyExpandedNew(src, dest, 0644); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("failed deploying template to %s: %v", dest, err))
			return
		}
	}
	res.TemplatesInit = append(res.TemplatesInit, dest)
}

// placeFile restores one repository file to dest. An equivalent local file is
// left as is, a differing one is kept unless replacement is authorized, and
// nothing is replaced without a successful safety backup. An existing
// symlink is replaced itself, never written through. Known incompatible
// backup content is repaired before the comparison, so an equivalent
// repaired local file is left alone and replacements deploy the repair.
func placeFile(g writeGuard, src, dest string, opts ApplyOptions, res *ApplyResult) {
	if err := g.check(filepath.Dir(dest)); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s not restored: %v", dest, err))
		return
	}
	repair := repairFor(dest)
	var fixed repairOutcome
	if repair != nil {
		if data, err := os.ReadFile(src); err == nil {
			fixed = repair.fix(data)
		}
	}
	restored := func() {
		res.Restored = append(res.Restored, dest)
		if fixed.issue != "" {
			res.NotRepaired = append(res.NotRepaired, fmt.Sprintf("%s: backup deployed without repair: %s", dest, fixed.issue))
		}
	}
	info, err := os.Lstat(dest)
	switch {
	case os.IsNotExist(err):
		info = nil
	case err != nil:
		res.Warnings = append(res.Warnings, fmt.Sprintf("cannot inspect %s: %v", dest, err))
		return
	case info.IsDir():
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s is a directory; not replaced", dest))
		return
	case !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0:
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s is not a regular file; not replaced", dest))
		return
	case sameContent(dest, src, fixed.data):
		restored()
		return
	case !opts.Replace:
		res.Kept = append(res.Kept, dest)
		reportKeptRepair(repair, dest, res)
		return
	}
	if !opts.DryRun {
		if info != nil {
			bk, err := backupExisting(dest)
			if err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s not replaced: safety backup failed: %v", dest, err))
				return
			}
			res.LocalBackups = append(res.LocalBackups, bk)
		}
		switch {
		case fixed.data != nil && info != nil && info.Mode()&os.ModeSymlink != 0:
			err = replaceWith(dest, func(tmp string) error { return writeExpandedNew(tmp, fixed.data) })
		case fixed.data != nil:
			err = writeExpanded(dest, fixed.data)
		case info != nil && info.Mode()&os.ModeSymlink != 0:
			err = replaceWith(dest, func(tmp string) error { return homepath.CopyExpandedNew(src, tmp, 0644) })
		default:
			err = copyFile(src, dest)
		}
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("failed copying %s to %s: %v", src, dest, err))
			return
		}
	}
	restored()
	if fixed.data != nil {
		res.Repaired = append(res.Repaired, fmt.Sprintf("%s: %s", dest, fixed.applied))
	}
}

// reportKeptRepair states that a kept local file still holds known
// incompatible content, so it is never mistaken for repaired.
func reportKeptRepair(repair *knownRepair, dest string, res *ApplyResult) {
	if repair == nil {
		return
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		return
	}
	switch local := repair.fix(data); {
	case local.data != nil:
		res.NotRepaired = append(res.NotRepaired, fmt.Sprintf("%s: existing local file kept, repair not applied (would have %s); authorize replacing existing files to repair it after a safety backup", dest, local.applied))
	case local.issue != "":
		res.NotRepaired = append(res.NotRepaired, fmt.Sprintf("%s: existing local file kept: %s", dest, local.issue))
	}
}

// writeExpanded writes repaired content to dest, expanding the home
// placeholder for the current machine.
func writeExpanded(dest string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	home, _ := homepath.Current()
	return os.WriteFile(dest, homepath.Expand(data, home), 0644)
}

// writeExpandedNew is writeExpanded into a newly created path.
func writeExpandedNew(dest string, data []byte) error {
	home, _ := homepath.Current()
	return writeNew(dest, homepath.Expand(data, home))
}

// writeNew writes data into a newly created path, never touching an existing one.
func writeNew(dest string, data []byte) error {
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		// dest was created exclusively above, so only our partial write is removed.
		os.Remove(dest)
	}
	return err
}

// placeSymlink recreates a repository symlink at dest. An existing link or
// regular file is only replaced when authorized and after its safety backup
// succeeds; directories are never removed.
func placeSymlink(g writeGuard, src, dest string, opts ApplyOptions, res *ApplyResult) {
	linkTarget, err := os.Readlink(src)
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("cannot read symlink %s: %v", src, err))
		return
	}
	if err := g.check(filepath.Dir(dest)); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s not restored: %v", dest, err))
		return
	}
	info, err := os.Lstat(dest)
	switch {
	case os.IsNotExist(err):
		info = nil
	case err != nil:
		res.Warnings = append(res.Warnings, fmt.Sprintf("cannot inspect %s: %v", dest, err))
		return
	case info.Mode()&os.ModeSymlink != 0:
		if current, err := os.Readlink(dest); err == nil && current == linkTarget {
			res.Restored = append(res.Restored, dest)
			return
		}
		if !opts.Replace {
			res.Kept = append(res.Kept, dest)
			return
		}
	case info.IsDir():
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s is a directory; not replaced by a symlink", dest))
		return
	case !info.Mode().IsRegular():
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s is not a regular file; not replaced by a symlink", dest))
		return
	default:
		if !opts.Replace {
			res.Kept = append(res.Kept, dest)
			return
		}
	}
	if !opts.DryRun {
		if info != nil {
			bk, err := backupExisting(dest)
			if err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s not replaced: safety backup failed: %v", dest, err))
				return
			}
			res.LocalBackups = append(res.LocalBackups, bk)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("failed creating symlink %s: %v", dest, err))
			return
		}
		if err := replaceWith(dest, func(tmp string) error { return os.Symlink(linkTarget, tmp) }); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("failed creating symlink %s: %v", dest, err))
			return
		}
	}
	res.Restored = append(res.Restored, dest)
}

// sameContent reports whether the local file already matches the repository
// file, or its repaired content when set, once the home placeholder is
// expanded.
func sameContent(local, repo string, repaired []byte) bool {
	if info, err := os.Stat(local); err != nil || !info.Mode().IsRegular() {
		return false
	}
	a, err := os.ReadFile(local)
	if err != nil {
		return false
	}
	b := repaired
	if b == nil {
		if b, err = os.ReadFile(repo); err != nil {
			return false
		}
	}
	home, err := homepath.Current()
	return err == nil && homepath.Equivalent(a, b, home)
}

// writeGuard confines the writes of one mapping to its authorized root: the
// home directory for targets inside it, otherwise the mapping's own parent
// directory, which the manifest names explicitly. The root's own location is
// trusted; below it, existing components may be symlinks only when they
// resolve inside the root.
type writeGuard struct {
	root     string
	resolved string
}

func newWriteGuard(target string) (writeGuard, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return writeGuard{}, err
	}
	root := filepath.Dir(abs)
	if home, err := homepath.Current(); err == nil && home != "" {
		if h, err := filepath.Abs(home); err == nil && within(h, abs) {
			root = h
		}
	}
	resolved, err := resolveExisting(root)
	if err != nil {
		return writeGuard{}, err
	}
	return writeGuard{root: root, resolved: resolved}, nil
}

// check verifies that every existing component of path below the root
// resolves inside the root. Missing components are created later as plain
// directories, so the first missing one ends the walk.
func (g writeGuard) check(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if !within(g.root, abs) {
		return fmt.Errorf("%s is outside %s", path, g.root)
	}
	rel, _ := filepath.Rel(g.root, abs)
	if rel == "." {
		return nil
	}
	cur := g.root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		if _, err := os.Lstat(cur); os.IsNotExist(err) {
			return nil
		} else if err != nil {
			return err
		}
		resolved, err := filepath.EvalSymlinks(cur)
		if err != nil {
			return fmt.Errorf("cannot resolve %s: %v", cur, err)
		}
		if !within(g.resolved, resolved) {
			return fmt.Errorf("%s resolves to %s, outside %s", cur, resolved, g.root)
		}
	}
	return nil
}

// resolveExisting resolves symlinks in the deepest existing ancestor of p
// and appends the missing remainder.
func resolveExisting(p string) (string, error) {
	missing := ""
	for {
		resolved, err := filepath.EvalSymlinks(p)
		if err == nil {
			return filepath.Join(resolved, missing), nil
		}
		parent := filepath.Dir(p)
		if !os.IsNotExist(err) || parent == p {
			return "", err
		}
		missing = filepath.Join(filepath.Base(p), missing)
		p = parent
	}
}

// within reports whether p is root or lies below it.
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ExecuteInstallCommand executes an installation command through shell.
func ExecuteInstallCommand(cmdStr string) error {
	name, args := shellInvocation(runtime.GOOS, cmdStr)
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// shellInvocation returns the shell executable and arguments used to run an
// install command on the given OS.
func shellInvocation(osName, cmdStr string) (string, []string) {
	if osName == "windows" {
		return "powershell", []string{"-NoProfile", "-NonInteractive", "-Command", cmdStr}
	}
	return "sh", []string{"-c", cmdStr}
}

// findBestRecipe resolves the install recipe for the current system. Lookup
// order: exact distro, primary package manager, other available managers,
// OS name, and finally the "default" key. Native managers therefore win over
// generic OS-level script recipes.
func findBestRecipe(recipes map[string]manifest.InstallRecipe, sys *detector.SystemInfo) (manifest.InstallRecipe, bool) {
	if len(recipes) == 0 || sys == nil {
		return manifest.InstallRecipe{}, false
	}

	// 1. Exact distro match (e.g. "ubuntu", "arch", "debian")
	if sys.Distro != "" && sys.Distro != detector.DistroUnknown {
		if r, ok := recipes[string(sys.Distro)]; ok {
			return r, true
		}
	}

	// 2. Package manager match: primary first, then any other available manager
	if sys.PrimaryManager != "" && sys.PrimaryManager != detector.ManagerUnknown {
		if r, ok := recipes[string(sys.PrimaryManager)]; ok {
			return r, true
		}
	}
	for _, m := range sys.Managers {
		if r, ok := recipes[string(m)]; ok {
			return r, true
		}
	}

	// 3. OS match (e.g. "darwin", "linux", "windows")
	if r, ok := recipes[sys.OS]; ok {
		return r, true
	}

	// 4. Any-OS fallback
	if r, ok := recipes["default"]; ok {
		return r, true
	}

	return manifest.InstallRecipe{}, false
}

func buildInstallCommand(recipe manifest.InstallRecipe, sys *detector.SystemInfo) string {
	if recipe.Command != "" {
		return recipe.Command
	}

	switch recipe.Manager {
	case "brew":
		if recipe.Cask != "" {
			return fmt.Sprintf("brew install --cask %s", recipe.Cask)
		}
		if recipe.Package != "" {
			return fmt.Sprintf("brew install %s", recipe.Package)
		}
	case "apt":
		if recipe.Package != "" {
			return fmt.Sprintf("sudo apt-get install -y %s", recipe.Package)
		}
	case "pacman":
		if recipe.Package != "" {
			return fmt.Sprintf("sudo pacman -S --noconfirm %s", recipe.Package)
		}
	case "yay":
		if recipe.Package != "" {
			return fmt.Sprintf("yay -S --noconfirm %s", recipe.Package)
		}
	case "dnf":
		if recipe.Package != "" {
			return fmt.Sprintf("sudo dnf install -y %s", recipe.Package)
		}
	case "zypper":
		if recipe.Package != "" {
			return fmt.Sprintf("sudo zypper install -y %s", recipe.Package)
		}
	case "winget":
		if recipe.Package != "" {
			return fmt.Sprintf("winget install --id %s -e --accept-source-agreements --accept-package-agreements", recipe.Package)
		}
	case "scoop":
		if recipe.Package != "" {
			return fmt.Sprintf("scoop install %s", recipe.Package)
		}
	case "choco":
		if recipe.Package != "" {
			return fmt.Sprintf("choco install -y %s", recipe.Package)
		}
	case "cargo":
		if recipe.Package != "" {
			return fmt.Sprintf("cargo install %s", recipe.Package)
		}
	case "pnpm":
		if recipe.Package != "" {
			return fmt.Sprintf("pnpm add -g %s", recipe.Package)
		}
	case "npm":
		if recipe.Package != "" {
			// Prefer pnpm if available on the system
			if sys != nil && sys.HasManager(detector.ManagerPnpm) {
				return fmt.Sprintf("pnpm add -g %s", recipe.Package)
			}
			// A user-level prefix avoids EACCES with system-wide Node on Linux/macOS.
			if sys != nil && sys.OS != "windows" {
				return fmt.Sprintf("npm install -g --prefix \"$HOME/.local\" %s", recipe.Package)
			}
			return fmt.Sprintf("npm install -g %s", recipe.Package)
		}
	}

	return ""
}

// copyFile restores a file from the dotfiles repository, expanding the home
// placeholder for the current machine.
func copyFile(src, dest string) error {
	return homepath.CopyExpanded(src, dest)
}

// backupExisting preserves dest beside itself under a new timestamped .bk
// name before it is replaced: a symlink is recreated as the same link (its
// target is not dereferenced) and a regular file is copied byte for byte with
// its permissions. Existing backups are never overwritten.
func backupExisting(dest string) (string, error) {
	info, err := os.Lstat(dest)
	if err != nil {
		return "", err
	}
	base := fmt.Sprintf("%s.%s", dest, time.Now().Format("20060102_150405"))
	if info.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(dest)
		if err != nil {
			return "", err
		}
		return createUnique(base, ".bk", func(p string) error { return os.Symlink(link, p) })
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", dest)
	}
	return createUnique(base, ".bk", func(p string) error { return homepath.CopyRawNew(dest, p, info.Mode().Perm()) })
}

// replaceWith creates the replacement at a fresh sibling path and renames it
// over dest, so an existing link is swapped rather than followed.
func replaceWith(dest string, create func(string) error) error {
	tmp, err := createUnique(dest+".dots-new", "", create)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		// tmp was created exclusively above; only it is removed.
		os.Remove(tmp)
		return err
	}
	return nil
}

// createUnique calls create with base+suffix, then base.1+suffix and so on
// until a name is free. create must fail with an "exists" error instead of
// touching an existing path.
func createUnique(base, suffix string, create func(string) error) (string, error) {
	for i := 0; i < 100; i++ {
		p := base + suffix
		if i > 0 {
			p = fmt.Sprintf("%s.%d%s", base, i, suffix)
		}
		err := create(p)
		if err == nil {
			return p, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("no free name for %s%s", base, suffix)
}
