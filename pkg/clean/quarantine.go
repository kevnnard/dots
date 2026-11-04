package clean

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/kevnnard/dots/pkg/manifest"
)

// QuarantineDir is the home-relative directory holding one 0700 record
// directory per configuration cleanup.
const QuarantineDir = ".dots-quarantine"

const recordName = "record.json"

// CandidateStatus is what a configuration run may do with one path.
type CandidateStatus string

const (
	CandidateQuarantine CandidateStatus = "quarantine" // moved into the quarantine
	CandidateRestore    CandidateStatus = "restore"    // moved back from the quarantine
	CandidateMissing    CandidateStatus = "missing"    // nothing to move
	CandidateConflict   CandidateStatus = "conflict"   // the destination exists and is never replaced
	CandidateBlocked    CandidateStatus = "blocked"    // unsafe to move; Reason says why
)

// ConfigCandidate is one path a configuration run inspected.
type ConfigCandidate struct {
	AppName string          `json:"app_name"`
	Path    string          `json:"path"` // absolute
	Status  CandidateStatus `json:"status"`
	Reason  string          `json:"reason,omitempty"`
	info    fs.FileInfo     // identity observed at preview time
	entry   int             // record entry index, for recovery
}

// ConfigReport is the result of a configuration cleanup or recovery. Moved
// lists the original paths whose files are in the quarantine after a cleanup,
// or back in place after a recovery. DryRun and Cancelled runs move nothing.
type ConfigReport struct {
	Candidates []ConfigCandidate `json:"candidates"`
	Quarantine string            `json:"quarantine,omitempty"` // record directory, once created
	Moved      []string          `json:"moved"`
	Failures   []string          `json:"failures"`
	DryRun     bool              `json:"dry_run"`
	Cancelled  bool              `json:"cancelled"`
}

// ConfigRunner moves the quarantine items of a plan into a new record
// directory under home. Only regular files are moved, by rename; nothing is
// deleted. It is shared by the CLI and TUI, which supply Confirm and Out.
type ConfigRunner struct {
	Manifest *manifest.Manifest
	Home     string // absolute
	Repo     string // optional repository root; nothing inside it moves
	DryRun   bool
	// Confirm receives the printed summary. A nil Confirm denies.
	Confirm func(summary string) bool
	Out     io.Writer
	// Rename moves one file into the quarantine; nil means os.Rename.
	Rename func(oldpath, newpath string) error
}

// Recovery moves the files of one quarantine record back, never replacing a
// path that exists again.
type Recovery struct {
	Home    string
	Repo    string
	Dir     string // a record directory reported by ConfigRunner
	DryRun  bool
	Confirm func(summary string) bool // nil denies
	Out     io.Writer
}

type record struct {
	Version int           `json:"version"`
	Home    string        `json:"home"`
	Entries []recordEntry `json:"entries"`
}

type recordEntry struct {
	App      string `json:"app"`
	Original string `json:"original"`
	Stored   string `json:"stored"` // file name inside the record directory
}

// Run validates the plan against the manifest, prints every candidate and,
// only when confirmed, writes the record and moves the files. Any failure
// moves every already-moved file back unless its original path is taken.
func (r ConfigRunner) Run(ctx context.Context, p *Plan) (*ConfigReport, error) {
	if r.Manifest == nil || r.Out == nil || p == nil {
		return nil, fmt.Errorf("config runner is not fully configured")
	}
	g, err := newGuard(r.Home, r.Repo)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, it := range p.Configs {
		if app, ok := r.Manifest.Apps[it.AppName]; !ok || !sameItem(it, planConfig(it.AppName, app)) {
			return nil, fmt.Errorf("refusing plan item %q: it does not match the manifest", it.AppName)
		}
	}
	rep := &ConfigReport{Candidates: []ConfigCandidate{}, Moved: []string{}, Failures: []string{}}
	var b strings.Builder
	b.WriteString("Configuration cleanup plan:\n")
	seen := map[string]string{}
	var moves []ConfigCandidate
	blocked := 0
	for _, it := range p.Configs {
		if it.Status == ConfigBlocked {
			blocked++
		}
		if it.Status != ConfigQuarantine {
			b.WriteString(line(string(it.Status), it.AppName, it.Reason))
			continue
		}
		for _, src := range it.Paths {
			c := g.inspect(it.AppName, filepath.Join(g.home, filepath.FromSlash(strings.TrimPrefix(src, "~/"))), it.Preserve)
			if first, dup := seen[c.Path]; dup {
				c.Status, c.Reason, c.info = CandidateBlocked, "already listed by "+first, nil
			}
			seen[c.Path] = it.AppName
			rep.Candidates = append(rep.Candidates, c)
			b.WriteString(g.line(c))
			switch c.Status {
			case CandidateBlocked:
				blocked++
			case CandidateQuarantine:
				moves = append(moves, c)
			}
		}
		if len(it.Preserve) > 0 {
			b.WriteString(line("keep", it.AppName, strings.Join(it.Preserve, ", ")))
		}
	}
	fmt.Fprintf(&b, "%d to quarantine, %d blocked\n", len(moves), blocked)
	if !gate(r.Out, b.String(), r.DryRun, len(moves), r.Confirm, rep, "Nothing to quarantine.") {
		return rep, nil
	}
	return r.move(ctx, g, rep, moves)
}

func (r ConfigRunner) move(ctx context.Context, g guard, rep *ConfigReport, moves []ConfigCandidate) (*ConfigReport, error) {
	dir, err := g.newRecordDir()
	if err != nil {
		return rep, fmt.Errorf("could not create the quarantine: %w", err)
	}
	rep.Quarantine = dir
	rec := record{Version: 1, Home: g.home}
	for i, c := range moves {
		rec.Entries = append(rec.Entries, recordEntry{App: c.AppName, Original: c.Path, Stored: strconv.Itoa(i)})
	}
	if err := writeRecord(dir, rec); err != nil {
		return rep, fmt.Errorf("could not write the quarantine record: %w", err)
	}
	rename := r.Rename
	if rename == nil {
		rename = os.Rename
	}
	for i, c := range moves {
		err := ctx.Err()
		if now := g.inspect(c.AppName, c.Path, nil); err == nil && (now.Status != CandidateQuarantine || !sameFile(c.info, now.info)) {
			err = errors.New("changed since the preview") // identity, size, mode or mtime drifted
		}
		if err == nil {
			err = rename(c.Path, filepath.Join(dir, rec.Entries[i].Stored))
		}
		if err != nil {
			rep.Failures, rep.Moved = append(rep.Failures, g.show(c.Path)+": "+err.Error()), []string{}
			for j := i - 1; j >= 0; j-- {
				if err := moveNoReplace(filepath.Join(dir, rec.Entries[j].Stored), moves[j].Path); err != nil {
					rep.Failures = append(rep.Failures, g.show(moves[j].Path)+": not restored: "+describe(err))
					rep.Moved = append(rep.Moved, moves[j].Path)
				}
			}
			slices.Reverse(rep.Moved)
			for _, f := range rep.Failures {
				fmt.Fprintln(r.Out, "failed "+f)
			}
			fmt.Fprintln(r.Out, "Recovery record: "+dir)
			return rep, fmt.Errorf("configuration cleanup failed; %d file(s) remain in %s", len(rep.Moved), dir)
		}
		rep.Moved = append(rep.Moved, c.Path)
		fmt.Fprintln(r.Out, "quarantined "+g.show(c.Path))
	}
	fmt.Fprintln(r.Out, "Recover from "+dir)
	return rep, nil
}

// Recover previews one quarantine record and, only when confirmed, moves its
// files back. Conflicts, tampered entries and unsafe paths stay in the
// quarantine. The first failure stops the run.
func Recover(ctx context.Context, r Recovery) (*ConfigReport, error) {
	if r.Out == nil {
		return nil, fmt.Errorf("recovery is not fully configured")
	}
	g, err := newGuard(r.Home, r.Repo)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rec, err := g.readRecord(r.Dir)
	if err != nil {
		return nil, fmt.Errorf("refusing quarantine record: %w", err)
	}
	rep := &ConfigReport{Candidates: []ConfigCandidate{}, Quarantine: r.Dir, Moved: []string{}, Failures: []string{}}
	var b strings.Builder
	b.WriteString("Configuration recovery from " + r.Dir + ":\n")
	var moves []ConfigCandidate
	for i, e := range rec.Entries {
		c := g.inspectRestore(r.Dir, i, e)
		c.entry = i
		rep.Candidates = append(rep.Candidates, c)
		b.WriteString(g.line(c))
		if c.Status == CandidateRestore {
			moves = append(moves, c)
		}
	}
	fmt.Fprintf(&b, "%d to restore, %d blocked\n", len(moves), len(rep.Candidates)-len(moves))
	if !gate(r.Out, b.String(), r.DryRun, len(moves), r.Confirm, rep, "Nothing to restore.") {
		return rep, nil
	}
	for _, c := range moves {
		e := rec.Entries[c.entry]
		err := ctx.Err()
		if now := g.inspectRestore(r.Dir, c.entry, e); err == nil && (now.Status != CandidateRestore || !sameFile(c.info, now.info)) {
			err = errors.New("changed since the preview")
		}
		if err == nil {
			err = moveNoReplace(filepath.Join(r.Dir, e.Stored), c.Path)
		}
		if err != nil {
			rep.Failures = append(rep.Failures, g.show(c.Path)+": "+describe(err))
			fmt.Fprintln(r.Out, "failed "+rep.Failures[0])
			return rep, fmt.Errorf("recovery stopped; remaining files stay in %s", r.Dir)
		}
		rep.Moved = append(rep.Moved, c.Path)
		fmt.Fprintln(r.Out, "restored "+g.show(c.Path))
	}
	return rep, nil
}

// gate prints the summary and reports whether the run may change files:
// never on a dry run or with nothing to do, and only after Confirm.
func gate(out io.Writer, summary string, dryRun bool, n int, confirm func(string) bool, rep *ConfigReport, none string) bool {
	fmt.Fprint(out, summary)
	switch {
	case dryRun:
		rep.DryRun = true
		fmt.Fprintln(out, "Dry run: nothing was changed.")
	case n == 0:
		fmt.Fprintln(out, none)
	case confirm == nil || !confirm(summary):
		rep.Cancelled = true
		fmt.Fprintln(out, "Cancelled: nothing was changed.")
	default:
		return true
	}
	return false
}

// guard holds the resolved roots every path is checked against.
type guard struct{ home, repo, root string }

func newGuard(home, repo string) (guard, error) {
	if !filepath.IsAbs(home) {
		return guard{}, fmt.Errorf("home must be an absolute path")
	}
	h, err := filepath.EvalSymlinks(home)
	if err != nil {
		return guard{}, err
	}
	if filepath.Dir(h) == h {
		return guard{}, fmt.Errorf("home must not be the filesystem root")
	}
	g := guard{home: h, root: filepath.Join(h, QuarantineDir)}
	if repo != "" {
		if g.repo, err = filepath.EvalSymlinks(repo); err != nil || !filepath.IsAbs(g.repo) {
			return guard{}, fmt.Errorf("repository must be an existing absolute path")
		}
		if within(g.repo, g.root) || within(g.root, g.repo) {
			return guard{}, fmt.Errorf("repository overlaps the quarantine")
		}
	}
	return g, nil
}

// within reports whether p is parent or below it.
func within(parent, p string) bool {
	rel, err := filepath.Rel(parent, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// check applies the path rules shared by cleanup and recovery. A missing
// parent directory is reported as CandidateMissing.
func (g guard) check(p string) (CandidateStatus, string) {
	switch {
	case !filepath.IsAbs(p) || filepath.Clean(p) != p:
		return CandidateBlocked, "not a clean absolute path"
	case p == g.home || !within(g.home, p):
		return CandidateBlocked, "outside home"
	case within(g.root, p):
		return CandidateBlocked, "inside the quarantine"
	case g.repo != "" && (within(g.repo, p) || within(p, g.repo)):
		return CandidateBlocked, "overlaps the repository"
	}
	for dir := filepath.Dir(p); dir != g.home; dir = filepath.Dir(dir) {
		fi, err := os.Lstat(dir)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return CandidateMissing, "parent directory is missing"
		case err != nil:
			return CandidateBlocked, "cannot inspect a parent directory"
		case fi.Mode()&fs.ModeSymlink != 0:
			return CandidateBlocked, "a parent directory is a symlink"
		case !fi.IsDir():
			return CandidateBlocked, "a parent is not a directory"
		}
	}
	return "", ""
}

// sensitiveWords mark names that may hold secrets, credentials or data; they
// never move, even when the manifest maps them.
var sensitiveWords = []string{"secret", "credential", "token", "auth", "passw", "cookie", "session", "history",
	"cache", "hosts.yml", ".netrc", ".env", ".key", ".pem", ".p12", "id_rsa", "id_ed25519", ".db", ".sqlite"}

func (g guard) inspect(app, p string, preserve []string) ConfigCandidate {
	c := ConfigCandidate{AppName: app, Path: p, Status: CandidateBlocked}
	if st, reason := g.check(p); reason != "" {
		c.Status, c.Reason = st, reason
		if st == CandidateMissing {
			c.Reason = ""
		}
		return c
	}
	rel, _ := filepath.Rel(g.home, p)
	segs := strings.Split(filepath.ToSlash(rel), "/")
	for _, pat := range preserve {
		match, ok := preserved(pat, segs)
		if !ok {
			c.Reason = "uncertain preserve pattern: " + pat
			return c
		}
		if match {
			c.Reason = "matches a preserved pattern"
			return c
		}
	}
	for _, s := range segs {
		for _, w := range sensitiveWords {
			if strings.Contains(strings.ToLower(s), w) {
				c.Reason = "may hold secrets, credentials or data"
				return c
			}
		}
	}
	fi, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		c.Status = CandidateMissing
	case err != nil:
		c.Reason = "cannot inspect"
	case fi.Mode()&fs.ModeSymlink != 0:
		c.Reason = "is a symlink"
	case fi.IsDir():
		c.Reason = "directories are not supported yet"
	case !fi.Mode().IsRegular():
		c.Reason = "not a regular file"
	default:
		c.Status, c.info = CandidateQuarantine, fi
	}
	return c
}

// preserved reports whether a secrets or exclude pattern covers the file
// whose home-relative path segments are segs. As in backup, a pattern is
// relative to the mapped directory and a matching directory preserves all
// below it; because the mapped directory of a file is not known, every
// ancestor counts, so any contiguous run of segments may match. A trailing
// "/**" means the directory itself. ok is false for patterns whose meaning is
// uncertain (absolute, "~", "..", ".", empty segments, inner "**",
// backslashes, malformed globs); callers must then keep the file.
func preserved(pattern string, segs []string) (match, ok bool) {
	pat := strings.TrimSuffix(pattern, "/**")
	parts := strings.Split(pat, "/")
	if strings.ContainsAny(pat, "\\~") || strings.Contains(pat, "**") ||
		slices.Contains(parts, "") || slices.Contains(parts, ".") || slices.Contains(parts, "..") {
		return false, false
	}
	if _, err := path.Match(pat, ""); err != nil {
		return false, false
	}
	for i := 0; i+len(parts) <= len(segs); i++ {
		if m, _ := path.Match(pat, strings.Join(segs[i:i+len(parts)], "/")); m {
			return true, true
		}
	}
	return false, true
}

func sameFile(a, b fs.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// inspectRestore checks one record entry: the stored file must be a regular
// file and its original path free and safe.
func (g guard) inspectRestore(dir string, i int, e recordEntry) ConfigCandidate {
	c := ConfigCandidate{AppName: e.App, Path: e.Original, Status: CandidateBlocked}
	if e.Stored != strconv.Itoa(i) {
		c.Reason = "unexpected stored name"
		return c
	}
	if _, reason := g.check(e.Original); reason != "" {
		c.Reason = reason
		return c
	}
	fi, err := os.Lstat(filepath.Join(dir, e.Stored))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		c.Status = CandidateMissing
		return c
	case err != nil || !fi.Mode().IsRegular():
		c.Reason = "stored entry is not a regular file"
		return c
	}
	if _, err := os.Lstat(e.Original); err == nil {
		c.Status, c.Reason = CandidateConflict, "destination exists"
	} else if !errors.Is(err, fs.ErrNotExist) {
		c.Reason = "cannot inspect the destination"
	} else {
		c.Status, c.info = CandidateRestore, fi
	}
	return c
}

// newRecordDir creates a unique 0700 record directory under a real root.
func (g guard) newRecordDir() (string, error) {
	if err := os.Mkdir(g.root, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	if fi, err := os.Lstat(g.root); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%s is not a real directory", g.root)
	}
	return os.MkdirTemp(g.root, "clean-")
}

// writeRecord persists the original paths exclusively and durably before any
// file moves.
func writeRecord(dir string, rec record) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, recordName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (g guard) readRecord(dir string) (*record, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || filepath.Dir(dir) != g.root || !strings.HasPrefix(filepath.Base(dir), "clean-") {
		return nil, fmt.Errorf("not a record directory in %s", g.root)
	}
	for _, p := range []string{g.root, dir} {
		if fi, err := os.Lstat(p); err != nil || !fi.IsDir() {
			return nil, fmt.Errorf("%s is not a real directory", p)
		}
	}
	p := filepath.Join(dir, recordName)
	if fi, err := os.Lstat(p); err != nil || !fi.Mode().IsRegular() || fi.Size() > 1<<20 {
		return nil, fmt.Errorf("record file is missing or not a regular file")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var rec record
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rec); err != nil || rec.Version != 1 || rec.Home != g.home {
		return nil, fmt.Errorf("record is invalid or belongs to another home")
	}
	return &rec, nil
}

// moveNoReplace links then unlinks, so an existing destination is never
// replaced.
func moveNoReplace(src, dst string) error {
	if err := os.Link(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func describe(err error) string {
	if errors.Is(err, fs.ErrExist) {
		return "destination exists"
	}
	return err.Error()
}

func sameItem(a, b ConfigItem) bool {
	return a.AppName == b.AppName && a.Status == b.Status && a.Reason == b.Reason &&
		slices.Equal(a.Paths, b.Paths) && slices.Equal(a.Preserve, b.Preserve)
}

func (g guard) show(p string) string {
	if filepath.Clean(p) == p && p != g.home && within(g.home, p) {
		rel, _ := filepath.Rel(g.home, p)
		return "~/" + filepath.ToSlash(rel)
	}
	return p
}

func (g guard) line(c ConfigCandidate) string {
	detail := g.show(c.Path)
	if c.Reason != "" {
		detail += ": " + c.Reason
	}
	return line(string(c.Status), c.AppName, detail)
}

func line(status, app, detail string) string {
	if detail == "" {
		return "  " + status + " " + app + "\n"
	}
	return "  " + status + " " + app + ": " + detail + "\n"
}
