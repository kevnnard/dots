package clean

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kevnnard/dots/pkg/manifest"
)

// CandidateKeep marks a restore backup that always stays in place.
const CandidateKeep CandidateStatus = "keep"

// backupName matches the safety backups restore writes beside a replaced
// file: <original>.<YYYYMMDD_HHMMSS>[.<n>].bk.
var backupName = regexp.MustCompile(`^(.+)\.(\d{8}_\d{6})(?:\.\d+)?\.bk$`)

const backupStamp = "20060102_150405"

// BackupRunner moves superseded restore backups (*.bk) of the selected apps'
// mapped files into the same recoverable quarantine as ConfigRunner. The most
// recent backup of every file always stays; backups whose age is not certain
// (unrecognized names, equal timestamps) stay too. Nothing is deleted.
type BackupRunner struct {
	Manifest *manifest.Manifest
	Home     string // absolute
	Repo     string // optional repository root; nothing inside it moves
	Apps     []string
	DryRun   bool
	// Confirm receives the printed summary. A nil Confirm denies.
	Confirm func(summary string) bool
	Out     io.Writer
	// Rename moves one file into the quarantine; nil means os.Rename.
	Rename func(oldpath, newpath string) error
}

// backupFile is one *.bk found for an app. original is the file it backs up;
// for an unrecognized name it is only used to group and sort.
type backupFile struct {
	app, path, original string
	stamp               time.Time
	unknown             bool
	preserve            []string
}

// Run scans the apps' mapped paths for backups, prints which one of every
// file stays and which older ones would move and, only when confirmed, moves
// them. Every candidate is checked again right before its move.
func (r BackupRunner) Run(ctx context.Context) (*ConfigReport, error) {
	if r.Manifest == nil || r.Out == nil {
		return nil, fmt.Errorf("backup runner is not fully configured")
	}
	g, err := newGuard(r.Home, r.Repo)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	apps, err := exact(r.Manifest, r.Apps)
	if err != nil {
		return nil, err
	}
	rep := &ConfigReport{Candidates: []ConfigCandidate{}, Moved: []string{}, Failures: []string{}}
	var b strings.Builder
	b.WriteString("Old backup cleanup plan (the most recent backup of every file always stays):\n")
	var moves []ConfigCandidate
	kept, blocked := 0, 0
	seen := map[string]bool{}
	for _, name := range apps {
		it := planConfig(name, r.Manifest.Apps[name])
		if it.Status != ConfigQuarantine {
			continue // no mapped path inside home, so no backup restore made
		}
		groups := map[string][]backupFile{}
		for _, src := range it.Paths {
			p := filepath.Join(g.home, filepath.FromSlash(strings.TrimPrefix(src, "~/")))
			files, reason := g.findBackups(p)
			if reason != "" {
				b.WriteString(line(string(CandidateBlocked), name, g.show(p)+": "+reason))
				blocked++
			}
			for _, f := range files {
				if !seen[f.path] {
					seen[f.path] = true
					f.app, f.preserve = name, it.Preserve
					groups[f.original] = append(groups[f.original], f)
				}
			}
		}
		keys := make([]string, 0, len(groups))
		for k := range groups {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			for _, c := range g.sortBackups(groups[k]) {
				rep.Candidates = append(rep.Candidates, c)
				b.WriteString(g.line(c))
				switch c.Status {
				case CandidateKeep:
					kept++
				case CandidateBlocked:
					blocked++
				case CandidateQuarantine:
					moves = append(moves, c)
				}
			}
		}
	}
	if len(rep.Candidates) == 0 && blocked == 0 {
		b.WriteString("  no backups found\n")
	}
	fmt.Fprintf(&b, "%d to quarantine, %d kept, %d blocked\n", len(moves), kept, blocked)
	if !gate(r.Out, b.String(), r.DryRun, len(moves), r.Confirm, rep, "Nothing to quarantine.") {
		return rep, nil
	}
	return ConfigRunner{Out: r.Out, Rename: r.Rename}.move(ctx, g, rep, moves)
}

// sortBackups decides one file's backups, newest first. Only a backup that is
// strictly older than another kept backup may move; every backup sharing a
// timestamp with another, and every unrecognized name, stays.
func (g guard) sortBackups(files []backupFile) []ConfigCandidate {
	sort.Slice(files, func(i, j int) bool {
		a, b := files[i], files[j]
		switch {
		case a.unknown != b.unknown:
			return !a.unknown
		case !a.stamp.Equal(b.stamp):
			return a.stamp.After(b.stamp)
		}
		return a.path < b.path
	})
	known, stamps := 0, map[int64]int{}
	for _, f := range files {
		if !f.unknown {
			known++
			stamps[f.stamp.Unix()]++
		}
	}
	out := make([]ConfigCandidate, 0, len(files))
	for i, f := range files {
		c := ConfigCandidate{AppName: f.app, Path: f.path, Status: CandidateKeep}
		of := " of " + g.show(f.original)
		switch {
		case f.unknown:
			c.Reason = "unrecognized backup name"
		case stamps[f.stamp.Unix()] > 1:
			c.Reason = "same timestamp as another backup" + of
		case known == 1:
			c.Reason = "only backup" + of
		case i == 0:
			c.Reason = "most recent backup" + of
		default:
			c = g.inspectBackup(f)
			if c.Status == CandidateQuarantine {
				c.Reason = "older backup" + of
			}
		}
		out = append(out, c)
	}
	return out
}

// inspectBackup applies the configuration rules to an older backup: a backup
// of a preserved file, a symlink or a sensitive name never moves.
func (g guard) inspectBackup(f backupFile) ConfigCandidate {
	rel, _ := filepath.Rel(g.home, f.original)
	segs := strings.Split(filepath.ToSlash(rel), "/")
	for _, pat := range f.preserve {
		if match, ok := preserved(pat, segs); match || !ok {
			return ConfigCandidate{AppName: f.app, Path: f.path, Status: CandidateBlocked, Reason: "backup of a preserved file"}
		}
	}
	return g.inspect(f.app, f.path, f.preserve)
}

// findBackups lists the backups restore may have written for mapped path p:
// beside p for a file, anywhere below p for a directory. A non-empty reason
// means some backups could not be inspected; they are never moved.
func (g guard) findBackups(p string) ([]backupFile, string) {
	if st, reason := g.check(p); reason != "" {
		if st == CandidateMissing {
			return nil, ""
		}
		return nil, reason
	}
	fi, err := os.Lstat(p)
	switch {
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return nil, "cannot inspect"
	case err == nil && fi.IsDir():
		return g.walkBackups(p)
	}
	dir, base := filepath.Dir(p), filepath.Base(p)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ""
	} else if err != nil {
		return nil, "cannot inspect the directory"
	}
	var out []backupFile
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, base+".") || !strings.HasSuffix(n, ".bk") {
			continue
		}
		orig, stamp, ok := parseBackup(dir, n)
		switch {
		case ok && orig != p:
			continue // a backup of another file sharing the prefix
		case ok:
			out = append(out, backupFile{path: filepath.Join(dir, n), original: p, stamp: stamp})
		default:
			out = append(out, backupFile{path: filepath.Join(dir, n), original: p, unknown: true})
		}
	}
	return out, ""
}

func (g guard) walkBackups(root string) ([]backupFile, string) {
	var out []backupFile
	reason := ""
	filepath.WalkDir(root, func(q string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			reason = "cannot inspect " + g.show(q)
			return nil // an unreadable directory is skipped
		case d.IsDir():
			if q != root && (within(g.root, q) || (g.repo != "" && within(g.repo, q))) {
				return filepath.SkipDir
			}
			return nil
		case !strings.HasSuffix(d.Name(), ".bk"):
			return nil
		}
		if orig, stamp, ok := parseBackup(filepath.Dir(q), d.Name()); ok {
			out = append(out, backupFile{path: q, original: orig, stamp: stamp})
		} else {
			out = append(out, backupFile{path: q, original: strings.TrimSuffix(q, ".bk"), unknown: true})
		}
		return nil
	})
	return out, reason
}

// parseBackup returns the original path and timestamp a backup name encodes.
func parseBackup(dir, name string) (string, time.Time, bool) {
	m := backupName.FindStringSubmatch(name)
	if m == nil {
		return "", time.Time{}, false
	}
	stamp, err := time.Parse(backupStamp, m[2])
	if err != nil {
		return "", time.Time{}, false
	}
	return filepath.Join(dir, m[1]), stamp, true
}
