// Package transfer moves a dots backup between machines over Taildrop. The
// archive carries only the repository's configs/ tree and manifest.yaml. The
// receiver accepts one archive, checks its sha256 before extracting, and never
// overwrites an existing destination.
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
	"strings"
)

// ArchivePrefix names every archive so the receiver can pick it out of the inbox.
const ArchivePrefix = "dots-backup-"

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
// sha256 of the written file. Symlinks and other non-regular files are skipped.
func Archive(repo, out string) (string, error) {
	if err := writeArchive(repo, out); err != nil {
		return "", err
	}
	return sha256File(out)
}

func writeArchive(repo, out string) (err error) {
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
	if err := addFile(tw, repo, "manifest.yaml"); err != nil {
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

func addFile(tw *tar.Writer, root, rel string) error {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: rel, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err = tw.Write(data)
	return err
}

func addTree(tw *tar.Writer, root, rel string) error {
	return filepath.WalkDir(filepath.Join(root, rel), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		name, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		return addFile(tw, root, filepath.ToSlash(name))
	})
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

// Receive takes the one dots backup in the Taildrop inbox, checks it against
// wantSHA, and extracts it into dest. dest must not exist.
func Receive(r Runner, dest, wantSHA string) error {
	if wantSHA == "" {
		return errors.New("sha256 is required to verify the archive")
	}
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("destination %s already exists; nothing was received", dest)
	} else if !os.IsNotExist(err) {
		return err
	}

	inbox, err := os.MkdirTemp("", "dots-inbox-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(inbox)
	if out, err := r.Run("tailscale", "file", "get", "--conflict=skip", inbox); err != nil {
		return fmt.Errorf("tailscale file get: %w: %s", err, strings.TrimSpace(string(out)))
	}

	matches, err := filepath.Glob(filepath.Join(inbox, ArchivePrefix+"*.tar.gz"))
	if err != nil {
		return err
	}
	switch len(matches) {
	case 0:
		return errors.New("no dots backup found in the Taildrop inbox")
	case 1:
	default:
		return fmt.Errorf("found %d dots backups; receive one at a time", len(matches))
	}

	got, err := sha256File(matches[0])
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, wantSHA) {
		return fmt.Errorf("sha256 mismatch: got %s, want %s; nothing was extracted", got, wantSHA)
	}

	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".dots-receive-")
	if err != nil {
		return err
	}
	if err := extract(matches[0], stage); err != nil {
		os.RemoveAll(stage)
		return err
	}
	if err := os.Rename(stage, dest); err != nil {
		os.RemoveAll(stage)
		return err
	}
	return nil
}

// extract unpacks archive under root. Entries that escape root are refused
// before anything is written for them.
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
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(h.Name)
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("unsafe path in archive: %q", h.Name)
		}
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
		default:
			return fmt.Errorf("unsupported entry type in archive: %q", h.Name)
		}
	}
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
