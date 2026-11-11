// Package transfer moves a dots backup between machines over Taildrop. The
// archive carries only the repository's configs/ tree and manifest.yaml. The
// receiver accepts one archive, checks it against the code the sender printed
// (the sha256 or its first MinCodeLength characters) before extracting, and
// never overwrites an existing destination. Links into installed packages
// travel as checked references in the archived manifest (see pkg/packagelink).
package transfer

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kevnnard/dots/pkg/manifest"
	"github.com/kevnnard/dots/pkg/packagelink"
)

// ArchivePrefix names every archive so the receiver can pick it out of the inbox.
const ArchivePrefix = "dots-backup-"

// MinCodeLength is the shortest sha256 prefix the receiver may type.
const MinCodeLength = 8

// latestFile, inside the incoming directory, names the last received backup.
const latestFile = "latest"

// ShortCode returns the code the receiver types: the first MinCodeLength
// characters of the archive sha256.
func ShortCode(sum string) string {
	return sum[:MinCodeLength]
}

// checkCode refuses anything but MinCodeLength to 64 hexadecimal characters.
func checkCode(code string) error {
	if len(code) < MinCodeLength || len(code) > 64 {
		return fmt.Errorf("the code must be the %d-character code (or the full sha256) the sender printed, got %d characters", MinCodeLength, len(code))
	}
	if strings.Trim(strings.ToLower(code), "0123456789abcdef") != "" {
		return fmt.Errorf("the code must be hexadecimal, as the sender printed it: %q", code)
	}
	return nil
}

// Runner runs one external command and returns its combined output.
type Runner interface {
	Run(name string, args ...string) ([]byte, error)
}

// ExecRunner runs real commands.
type ExecRunner struct{}

// Run starts name with args and waits for it.
func (ExecRunner) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// Archive writes manifest.yaml and configs/ from repo into out and returns the
// sha256 of the written file. Relative symlinks travel as links (skills shared
// through ~/.agents/skills); absolute ones, which would not be portable, and
// other non-regular files are skipped. An absolute link into an installed
// package is recorded instead as a reference in the archived manifest; the
// staged manifest itself is not changed.
func Archive(repo, out string) (string, error) {
	if err := writeArchive(repo, out); err != nil {
		return "", err
	}
	return sha256File(out)
}

func writeArchive(repo, out string) (err error) {
	manifestData, err := archivedManifest(repo)
	if err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := addData(tw, "manifest.yaml", manifestData); err != nil {
		return err
	}
	if err := addTree(tw, repo, "configs"); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// archivedManifest returns the staged manifest with, for each app, a
// reference to every staged link into an installed package below the app's
// paths. Without such links the manifest is returned unchanged.
func archivedManifest(repo string) ([]byte, error) {
	manifestPath := filepath.Join(repo, "manifest.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	found, err := stagedPackageLinks(repo)
	if err != nil || len(found) == 0 {
		return data, err
	}
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("cannot record package links: %w", err)
	}
	links := map[string][]manifest.PackageLink{}
	for name, app := range m.Apps {
		byPath := map[string]manifest.PackageLink{}
		for _, l := range app.PackageLinks {
			byPath[l.Path] = l
		}
		owned := false
		for _, l := range found {
			if packagelink.Owns(app, l.Path) {
				byPath[l.Path] = l
				owned = true
			}
		}
		if !owned {
			continue
		}
		paths := make([]string, 0, len(byPath))
		for p := range byPath {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			links[name] = append(links[name], byPath[p])
		}
	}
	if len(links) == 0 {
		return data, nil
	}
	return manifest.WithPackageLinks(data, links)
}

// stagedPackageLinks lists the staged links under configs/ whose absolute
// target is a resource of a supported package. The target is only inspected
// for its type, never read.
func stagedPackageLinks(repo string) ([]manifest.PackageLink, error) {
	var found []manifest.PackageLink
	err := filepath.WalkDir(filepath.Join(repo, "configs"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.Type()&fs.ModeSymlink == 0 {
			return err
		}
		target, err := os.Readlink(p)
		if err != nil {
			return err
		}
		link, ok := packagelink.Parse(target)
		if !ok {
			return nil
		}
		rel, err := filepath.Rel(repo, p)
		if err != nil {
			return err
		}
		link.Path = filepath.ToSlash(rel)
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			link.Type = "dir"
		} else if err == nil && info.Mode().IsRegular() {
			link.Type = "file"
		}
		found = append(found, link)
		return nil
	})
	return found, err
}

func addFile(tw *tar.Writer, root, rel string) error {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	return addData(tw, rel, data)
}

func addData(tw *tar.Writer, rel string, data []byte) error {
	if err := tw.WriteHeader(&tar.Header{Name: rel, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

func addTree(tw *tar.Writer, root, rel string) error {
	return filepath.WalkDir(filepath.Join(root, rel), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return addLink(tw, p, filepath.ToSlash(name))
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return addFile(tw, root, filepath.ToSlash(name))
	})
}

func addLink(tw *tar.Writer, p, rel string) error {
	target, err := os.Readlink(p)
	if err != nil {
		return err
	}
	if filepath.IsAbs(target) {
		return nil
	}
	return tw.WriteHeader(&tar.Header{Name: rel, Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: filepath.ToSlash(target)})
}

// Send copies archive to device with `tailscale file cp`. The device is a
// Tailscale device name, not an IP address.
func Send(r Runner, archive, device string) error {
	if strings.TrimSpace(device) == "" {
		return errors.New("a Tailscale device name is required")
	}
	if out, err := r.Run("tailscale", "file", "cp", archive, device+":"); err != nil {
		return fmt.Errorf("tailscale file cp to %s: %w: %s", device, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Receive takes the dots backups from the Taildrop inbox and extracts the one
// whose sha256 starts with code into dest, which must not exist. Taildrop
// hands over every pending file at once: copies of the matching backup are
// dropped, and any other backup is kept in unclaimed/ next to dest, never
// lost. It returns the kept paths.
func Receive(r Runner, dest, code string) ([]string, error) {
	if err := checkCode(code); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(dest); err == nil {
		return nil, fmt.Errorf("destination %s already exists; nothing was received", dest)
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	inbox, err := os.MkdirTemp("", "dots-inbox-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(inbox)
	if out, err := r.Run("tailscale", "file", "get", "--conflict=skip", inbox); err != nil {
		return nil, fmt.Errorf("tailscale file get: %w: %s", err, strings.TrimSpace(string(out)))
	}

	matches, err := filepath.Glob(filepath.Join(inbox, ArchivePrefix+"*.tar.gz"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, errors.New("no dots backup found in the Taildrop inbox")
	}
	parent := filepath.Dir(dest)
	var match, firstSum string
	var unclaimed []string
	for _, m := range matches {
		got, err := sha256File(m)
		if err != nil {
			return unclaimed, err
		}
		if strings.HasPrefix(got, strings.ToLower(code)) {
			if match == "" {
				match = m
			}
			continue
		}
		if firstSum == "" {
			firstSum = got
		}
		kept, err := keepUnclaimed(m, filepath.Join(parent, "unclaimed"))
		if err != nil {
			return unclaimed, err
		}
		unclaimed = append(unclaimed, kept)
	}
	if match == "" {
		return unclaimed, fmt.Errorf("the received backup does not match code %s (its sha256 is %s); nothing was extracted, it was kept in %s", code, firstSum, filepath.Join(parent, "unclaimed"))
	}

	if err := os.MkdirAll(parent, 0o755); err != nil {
		return unclaimed, err
	}
	stage, err := os.MkdirTemp(parent, ".dots-receive-")
	if err != nil {
		return unclaimed, err
	}
	if err := extract(match, stage); err != nil {
		os.RemoveAll(stage)
		return unclaimed, err
	}
	if err := checkPackageLinks(stage); err != nil {
		os.RemoveAll(stage)
		return unclaimed, err
	}
	if err := os.Rename(stage, dest); err != nil {
		os.RemoveAll(stage)
		return unclaimed, err
	}
	return unclaimed, nil
}

// keepUnclaimed copies archive into dir under a name that is not taken yet.
// The inbox is usually on another filesystem, so it is copied, not renamed.
func keepUnclaimed(archive, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		return "", err
	}
	base := strings.TrimSuffix(filepath.Base(archive), ".tar.gz")
	for i := 0; i < 1000; i++ {
		name := base + ".tar.gz"
		if i > 0 {
			name = fmt.Sprintf("%s.%d.tar.gz", base, i)
		}
		target := filepath.Join(dir, name)
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			return "", err
		}
		return target, f.Close()
	}
	return "", fmt.Errorf("too many unclaimed backups named %s in %s", base, dir)
}

// IncomingDir is where received backups are extracted: ~/.cache/dots/incoming.
func IncomingDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "dots", "incoming"), nil
}

// RecordLatest remembers dest as the last backup received into incoming.
func RecordLatest(incoming, dest string) error {
	abs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(incoming, latestFile), []byte(abs+"\n"), 0o644)
}

const keptReceived = 3

func receivedName(name string) bool {
	if len(name) < MinCodeLength || len(name) > 12 {
		return false
	}
	return strings.Trim(name, "0123456789abcdef") == ""
}

func PruneReceived(incoming, keep string) ([]string, error) {
	keepAbs, err := filepath.Abs(keep)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(incoming)
	if err != nil {
		return nil, err
	}
	type received struct {
		path string
		info os.FileInfo
	}
	var all []received
	for _, e := range entries {
		if !e.IsDir() || !receivedName(e.Name()) {
			continue
		}
		p := filepath.Join(incoming, e.Name())
		info, err := os.Lstat(p)
		if err != nil || !info.IsDir() {
			continue
		}
		all = append(all, received{p, info})
	}
	isKeep := func(p string) bool {
		abs, err := filepath.Abs(p)
		return err != nil || abs == keepAbs
	}
	sort.Slice(all, func(i, j int) bool {
		if ki, kj := isKeep(all[i].path), isKeep(all[j].path); ki != kj {
			return ki
		}
		if !all[i].info.ModTime().Equal(all[j].info.ModTime()) {
			return all[i].info.ModTime().After(all[j].info.ModTime())
		}
		return all[i].path > all[j].path
	})
	var removed []string
	for i := keptReceived; i < len(all); i++ {
		p := all[i].path
		if isKeep(p) {
			continue
		}
		info, err := os.Lstat(p)
		if err != nil {
			return removed, err
		}
		if !info.IsDir() {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			return removed, err
		}
		removed = append(removed, p)
	}
	return removed, nil
}

// LatestReceived returns the last backup recorded by RecordLatest, provided it
// still holds a manifest.
func LatestReceived(incoming string) (string, error) {
	data, err := os.ReadFile(filepath.Join(incoming, latestFile))
	if os.IsNotExist(err) {
		return "", errors.New("no received backup yet: run `dots backup --receive <code>` first")
	} else if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(string(data))
	if _, err := os.Stat(filepath.Join(dir, "manifest.yaml")); err != nil {
		return "", fmt.Errorf("the last received backup %s is gone: run `dots backup --receive <code>` again", dir)
	}
	return dir, nil
}

// extract unpacks archive under root. Entries that escape root are refused
// before anything is written for them. Symlinks must be relative; they are
// created after every file, and no entry may sit under one, so nothing is
// ever written through a link. Where a link points is checked on restore.
func extract(archive, root string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	links := map[string]string{}
	var names []string
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return placeLinks(root, links, names)
		}
		if err != nil {
			return err
		}
		name := path.Clean(h.Name)
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("unsafe path in archive: %q", h.Name)
		}
		names = append(names, name)
		target := filepath.Join(root, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeEntry(target, tr); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if h.Linkname == "" || path.IsAbs(h.Linkname) || strings.ContainsRune(h.Linkname, 0) {
				return fmt.Errorf("unsafe symlink in archive: %q -> %q", h.Name, h.Linkname)
			}
			links[name] = h.Linkname
		default:
			return fmt.Errorf("unsupported entry type in archive: %q", h.Name)
		}
	}
}

// checkPackageLinks refuses an extracted backup whose manifest holds a
// package link reference that is malformed, outside its app's paths, or at a
// path where the archive itself holds an entry: archives never carry the
// links that references stand for. An unreadable manifest is left to restore.
func checkPackageLinks(root string) error {
	m, err := manifest.Load(filepath.Join(root, "manifest.yaml"))
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(m.Apps))
	for name := range m.Apps {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		refs, problems := packagelink.Scoped(m.Apps[name])
		if len(problems) > 0 {
			return fmt.Errorf("unsafe package link reference in archive (app %s): %s; nothing was extracted", name, problems[0])
		}
		for p := range refs {
			if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(p))); !os.IsNotExist(err) {
				return fmt.Errorf("unsafe package link reference in archive (app %s): the archive holds an entry at %s; nothing was extracted", name, p)
			}
		}
	}
	return nil
}

// placeLinks creates the archive's symlinks once every file is written,
// refusing any entry that would sit under one of them.
func placeLinks(root string, links map[string]string, names []string) error {
	for _, name := range names {
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			if _, ok := links[dir]; ok {
				return fmt.Errorf("unsafe path in archive: %q is under symlink %q", name, dir)
			}
		}
	}
	for name, target := range links {
		dest := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.Symlink(filepath.FromSlash(target), dest); err != nil {
			return err
		}
	}
	return nil
}

func writeEntry(target string, body io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, body); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func sha256File(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
