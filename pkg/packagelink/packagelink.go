// Package packagelink makes backed-up symlinks into installed package
// resources portable between machines. A link such as
// /opt/homebrew/Caskroom/<cask>/<version>/<resource> only means something on
// the machine that made it, so it is reduced to the package identity and the
// unchanged resource path, and resolved again on the receiving machine from
// what its package manager reports as installed.
//
// Only Homebrew casks (Caskroom) and formulae, dependencies included (Cellar),
// are supported, under any prefix. Everything else, such as /opt/homebrew/opt
// or share links, is not recognized. Every input is untrusted: a resolution
// needs the manager present, the package installed with one unambiguous
// version, and the resource present, of the expected type, inside that
// package's version directory once every link is followed.
package packagelink

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kevnnard/dots/pkg/manifest"
)

// Homebrew is the only supported manager.
const Homebrew = "homebrew"

// Runner runs one read-only package manager query and returns its stdout.
type Runner interface {
	Run(name string, args ...string) ([]byte, error)
}

// ExecRunner runs real commands, with arguments and no shell.
type ExecRunner struct{}

// Run starts name with args and returns its standard output.
func (ExecRunner) Run(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ANALYTICS=1")
	return cmd.Output()
}

// kinds maps each Homebrew layout directory to the package kind it holds.
var kinds = map[string]string{"Caskroom": "cask", "Cellar": "formula"}

var (
	validName    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._+-]*$`)
	validVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+,-]*$`)
)

// Parse recognizes target as a resource inside a Homebrew cask or formula
// version directory, under any prefix, and returns its identity. It never
// resolves anything: the result only names what to look for.
func Parse(target string) (manifest.PackageLink, bool) {
	if !filepath.IsAbs(target) {
		return manifest.PackageLink{}, false
	}
	parts := strings.Split(strings.Trim(filepath.ToSlash(target), "/"), "/")
	at := -1
	for i, p := range parts {
		if p == "." || p == ".." {
			return manifest.PackageLink{}, false
		}
		if _, ok := kinds[p]; ok {
			if at >= 0 {
				return manifest.PackageLink{}, false
			}
			at = i
		}
	}
	if at < 0 || len(parts) < at+4 {
		return manifest.PackageLink{}, false
	}
	link := manifest.PackageLink{
		Manager:  Homebrew,
		Kind:     kinds[parts[at]],
		Package:  parts[at+1],
		Version:  parts[at+2],
		Resource: strings.Join(parts[at+3:], "/"),
	}
	if validate(link) != nil {
		return manifest.PackageLink{}, false
	}
	return link, true
}

// Same reports whether a and b name the same resource of the same package.
// The source version is not compared: it only says what the source had.
func Same(a, b manifest.PackageLink) bool {
	return a.Manager == b.Manager && a.Kind == b.Kind && a.Package == b.Package && a.Resource == b.Resource
}

// Describe names the package of link for diagnostics.
func Describe(link manifest.PackageLink) string {
	if link.Manager == Homebrew {
		return fmt.Sprintf("Homebrew %s %s", link.Kind, link.Package)
	}
	return fmt.Sprintf("%s %s %s", link.Manager, link.Kind, link.Package)
}

func validate(link manifest.PackageLink) error {
	if link.Manager != Homebrew {
		return fmt.Errorf("unsupported package manager %q: only Homebrew casks and formulae are resolved", link.Manager)
	}
	if link.Kind != "cask" && link.Kind != "formula" {
		return fmt.Errorf("invalid Homebrew package kind %q", link.Kind)
	}
	if !validName.MatchString(link.Package) {
		return fmt.Errorf("invalid package name %q", link.Package)
	}
	if link.Version != "" && !validVersion.MatchString(link.Version) {
		return fmt.Errorf("invalid package version %q", link.Version)
	}
	if link.Type != "" && link.Type != "dir" && link.Type != "file" {
		return fmt.Errorf("invalid resource type %q", link.Type)
	}
	if !cleanRel(link.Resource) {
		return fmt.Errorf("invalid resource path %q", link.Resource)
	}
	return nil
}

// cleanRel reports whether p is a non-empty, clean, slash-separated relative
// path with no "." or ".." component.
func cleanRel(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00") || path.Clean(p) != p {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

// Scoped returns the app's package link references keyed by staged path,
// keeping only well-formed ones at or below one of the app's path dests.
// Every other reference, and every path referenced twice, is described in
// problems and left out.
func Scoped(app manifest.AppConfig) (map[string]manifest.PackageLink, []string) {
	refs := map[string]manifest.PackageLink{}
	seen := map[string]int{}
	var problems []string
	for _, link := range app.PackageLinks {
		switch {
		case !cleanRel(link.Path):
			problems = append(problems, fmt.Sprintf("package link reference %q: invalid path", link.Path))
			continue
		case !Owns(app, link.Path):
			problems = append(problems, fmt.Sprintf("package link reference %s is outside the app's backed-up paths", link.Path))
			continue
		}
		if err := validate(link); err != nil {
			problems = append(problems, fmt.Sprintf("package link reference %s: %v", link.Path, err))
			continue
		}
		seen[link.Path]++
		refs[link.Path] = link
	}
	var dups []string
	for p, n := range seen {
		if n > 1 {
			delete(refs, p)
			dups = append(dups, p)
		}
	}
	sort.Strings(dups)
	for _, p := range dups {
		problems = append(problems, fmt.Sprintf("package link reference %s is given more than once", p))
	}
	return refs, problems
}

// Owns reports whether the staged path is one of the app's path dests, as
// when the mapped path itself is a package link, or lies below one.
func Owns(app manifest.AppConfig, staged string) bool {
	for _, m := range app.Paths {
		dest := path.Clean(filepath.ToSlash(m.Dest))
		if staged == dest || strings.HasPrefix(staged, dest+"/") {
			return true
		}
	}
	return false
}

// Resolver resolves package links on this machine. It caches the manager's
// answers for one run.
type Resolver struct {
	run   Runner
	roots map[string]rootAnswer
	vers  map[string]versionAnswer
}

type rootAnswer struct {
	dir string
	err error
}

type versionAnswer struct {
	version string
	err     error
}

// NewResolver returns a Resolver querying the package manager through run.
func NewResolver(run Runner) *Resolver {
	return &Resolver{run: run, roots: map[string]rootAnswer{}, vers: map[string]versionAnswer{}}
}

// Resolve returns the path on this machine of the resource link names: the
// same resource path below the version of the package installed here.
func (r *Resolver) Resolve(link manifest.PackageLink) (string, error) {
	if err := validate(link); err != nil {
		return "", err
	}
	base, err := r.root(link.Kind)
	if err != nil {
		return "", err
	}
	version, err := r.installed(link.Kind, link.Package)
	if err != nil {
		return "", err
	}
	versionDir := filepath.Join(base, link.Package, version)
	candidate := filepath.Join(versionDir, filepath.FromSlash(link.Resource))

	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("cannot resolve %s: %v", base, err)
	}
	resolvedDir, err := filepath.EvalSymlinks(versionDir)
	if err != nil {
		return "", fmt.Errorf("installed version directory %s is missing: %v", versionDir, err)
	}
	// The root may itself be a link, but the package and version directories
	// must be the installed ones, not aliases of another package.
	if resolvedDir != filepath.Join(resolvedBase, link.Package, version) {
		return "", fmt.Errorf("%s resolves to %s, not the installed %s %s", versionDir, resolvedDir, link.Package, version)
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resource %s is missing in the installed version %s", link.Resource, version)
	}
	if !within(resolvedDir, resolved) {
		return "", fmt.Errorf("resource %s resolves to %s, outside the package directory %s", link.Resource, resolved, versionDir)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	kind := ""
	switch {
	case info.IsDir():
		kind = "dir"
	case info.Mode().IsRegular():
		kind = "file"
	default:
		return "", fmt.Errorf("resource %s is not a file or directory", link.Resource)
	}
	if link.Type != "" && link.Type != kind {
		names := map[string]string{"dir": "a directory", "file": "a file"}
		return "", fmt.Errorf("resource %s is %s here but was %s in the backup", link.Resource, names[kind], names[link.Type])
	}
	return candidate, nil
}

// root returns the Caskroom or Cellar directory Homebrew reports here.
func (r *Resolver) root(kind string) (string, error) {
	if a, ok := r.roots[kind]; ok {
		return a.dir, a.err
	}
	flag := "--caskroom"
	if kind == "formula" {
		flag = "--cellar"
	}
	var a rootAnswer
	out, err := r.run.Run("brew", flag)
	dir := strings.TrimSpace(string(out))
	switch {
	case err != nil:
		a.err = fmt.Errorf("Homebrew is not available on this machine (brew %s: %v)", flag, err)
	case dir == "" || strings.ContainsRune(dir, '\n') || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir:
		a.err = fmt.Errorf("Homebrew reported an unusable directory for brew %s: %q", flag, dir)
	default:
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			a.err = fmt.Errorf("Homebrew directory %s from brew %s is not a directory", dir, flag)
		} else {
			a.dir = dir
		}
	}
	r.roots[kind] = a
	return a.dir, a.err
}

// brewInfo holds the fields of `brew info --json=v2` that are used. Unknown
// fields are ignored; a field of an unexpected type fails the decoding.
type brewInfo struct {
	Formulae []struct {
		Name      string  `json:"name"`
		LinkedKeg *string `json:"linked_keg"`
		Installed []struct {
			Version string `json:"version"`
		} `json:"installed"`
	} `json:"formulae"`
	Casks []struct {
		Token     string  `json:"token"`
		Installed *string `json:"installed"`
	} `json:"casks"`
}

// installed returns the single installed version of the package.
func (r *Resolver) installed(kind, pkg string) (string, error) {
	key := kind + " " + pkg
	if a, ok := r.vers[key]; ok {
		return a.version, a.err
	}
	version, err := r.queryInstalled(kind, pkg)
	r.vers[key] = versionAnswer{version, err}
	return version, err
}

func (r *Resolver) queryInstalled(kind, pkg string) (string, error) {
	out, err := r.run.Run("brew", "info", "--json=v2", "--"+kind, pkg)
	if err != nil {
		return "", fmt.Errorf("%s is not installed or unknown to Homebrew here (brew info: %v)", pkg, err)
	}
	var info brewInfo
	dec := json.NewDecoder(bytes.NewReader(out))
	if err := dec.Decode(&info); err != nil {
		return "", fmt.Errorf("unexpected brew info metadata for %s: %v", pkg, err)
	}
	var versions []string
	linked := ""
	found := 0
	if kind == "cask" {
		for _, c := range info.Casks {
			if c.Token == pkg {
				found++
				if c.Installed != nil && *c.Installed != "" {
					versions = append(versions, *c.Installed)
				}
			}
		}
	} else {
		for _, f := range info.Formulae {
			if f.Name == pkg {
				found++
				for _, v := range f.Installed {
					versions = append(versions, v.Version)
				}
				if f.LinkedKeg != nil {
					linked = *f.LinkedKeg
				}
			}
		}
	}
	switch {
	case found > 1:
		return "", fmt.Errorf("brew info lists %s more than once; ambiguous", pkg)
	case len(versions) == 0:
		return "", fmt.Errorf("%s is not installed here", pkg)
	}
	version := versions[0]
	if len(versions) > 1 {
		version = ""
		for _, v := range versions {
			if v == linked {
				version = v
			}
		}
		if version == "" {
			return "", fmt.Errorf("%s has several installed versions (%s) and none is linked; ambiguous", pkg, strings.Join(versions, ", "))
		}
	}
	if !validVersion.MatchString(version) {
		return "", fmt.Errorf("unexpected brew info metadata for %s: installed version %q", pkg, version)
	}
	return version, nil
}

// within reports whether p is root or lies below it.
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
