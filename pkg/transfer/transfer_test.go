package transfer

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner records every command and answers from a map. Nothing real runs.
type fakeRunner struct {
	calls []string
	// onGet, when set, simulates the inbox delivering files to the target dir.
	onGet func(args []string)
	err   error
}

func (f *fakeRunner) Run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.Join(append([]string{name}, args...), " "))
	if f.err != nil {
		return nil, f.err
	}
	if len(args) > 1 && args[0] == "file" && args[1] == "get" && f.onGet != nil {
		f.onGet(args)
	}
	return nil, nil
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixtureRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "manifest.yaml"), "version: 1\n")
	writeFile(t, filepath.Join(repo, "configs", "zsh", ".zshrc"), "export A=1\n")
	writeFile(t, filepath.Join(repo, ".git", "HEAD"), "ref\n")
	writeFile(t, filepath.Join(repo, "bin", "private-mac-backup", "x"), "secret\n")
	writeFile(t, filepath.Join(repo, "design", "d.html"), "<html>\n")
	return repo
}

func listTar(t *testing.T, archive string) map[string]bool {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	names := map[string]bool{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names[h.Name] = true
	}
	return names
}

func testSHA(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// The archive carries only configs/ and manifest.yaml: never the git data,
// private scratch backups, or design files.
func TestArchiveIncludesOnlyConfigsAndManifest(t *testing.T) {
	repo := fixtureRepo(t)
	out := filepath.Join(t.TempDir(), "dots-backup-test.tar.gz")
	if _, err := Archive(repo, out); err != nil {
		t.Fatal(err)
	}
	names := listTar(t, out)
	if !names["manifest.yaml"] || !names["configs/zsh/.zshrc"] {
		t.Fatalf("missing expected entries: %v", names)
	}
	for n := range names {
		if strings.HasPrefix(n, ".git") || strings.HasPrefix(n, "bin/") || strings.HasPrefix(n, "design/") {
			t.Fatalf("archive leaked %q", n)
		}
	}
}

func TestSendUsesTailscaleFileCpToNamedDevice(t *testing.T) {
	r := &fakeRunner{}
	archive := filepath.Join(t.TempDir(), "dots-backup-x.tar.gz")
	writeFile(t, archive, "data")
	if err := Send(r, archive, "kevnnard-station"); err != nil {
		t.Fatal(err)
	}
	want := "tailscale file cp " + archive + " kevnnard-station:"
	if len(r.calls) != 1 || r.calls[0] != want {
		t.Fatalf("calls = %v, want [%s]", r.calls, want)
	}
}

func TestSendRejectsMissingDevice(t *testing.T) {
	r := &fakeRunner{}
	if err := Send(r, "/tmp/x.tar.gz", ""); err == nil {
		t.Fatal("empty device must be rejected")
	}
	if len(r.calls) != 0 {
		t.Fatalf("nothing should run, got %v", r.calls)
	}
}

// Receive accepts only the expected archive name and a matching sha256, and
// refuses to overwrite an existing destination.
func TestReceiveVerifiesShaAndExtracts(t *testing.T) {
	repo := fixtureRepo(t)
	src := filepath.Join(t.TempDir(), "dots-backup-ok.tar.gz")
	sum, err := Archive(repo, src)
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{onGet: func(args []string) {
		data, _ := os.ReadFile(src)
		writeFile(t, filepath.Join(args[len(args)-1], "dots-backup-ok.tar.gz"), string(data))
	}}
	dest := filepath.Join(t.TempDir(), "incoming")
	if err := Receive(r, dest, sum); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "manifest.yaml")); err != nil {
		t.Fatalf("manifest not extracted: %v", err)
	}
}

func TestReceiveRejectsShaMismatch(t *testing.T) {
	repo := fixtureRepo(t)
	src := filepath.Join(t.TempDir(), "dots-backup-bad.tar.gz")
	if _, err := Archive(repo, src); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{onGet: func(args []string) {
		data, _ := os.ReadFile(src)
		writeFile(t, filepath.Join(args[len(args)-1], "dots-backup-bad.tar.gz"), string(data))
	}}
	dest := filepath.Join(t.TempDir(), "incoming")
	err := Receive(r, dest, strings.Repeat("0", 64))
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("want sha256 mismatch error, got %v", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatal("nothing may be extracted after a sha mismatch")
	}
}

func TestReceiveIgnoresOtherFilesAndNeverOverwrites(t *testing.T) {
	r := &fakeRunner{onGet: func(args []string) {
		writeFile(t, filepath.Join(args[len(args)-1], "photo.png"), "x")
	}}
	dest := filepath.Join(t.TempDir(), "incoming")
	err := Receive(r, dest, strings.Repeat("0", 64))
	if err == nil || !strings.Contains(err.Error(), "no dots backup") {
		t.Fatalf("want no-backup error, got %v", err)
	}

	existing := t.TempDir()
	if err := Receive(&fakeRunner{}, existing, strings.Repeat("0", 64)); err == nil {
		t.Fatal("existing destination must be refused")
	}
}

func TestRejectsPathTraversalInArchive(t *testing.T) {
	src := filepath.Join(t.TempDir(), "dots-backup-evil.tar.gz")
	f, _ := os.Create(src)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := []byte("x")
	_ = tw.WriteHeader(&tar.Header{Name: "../escape", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()
	r := &fakeRunner{onGet: func(args []string) {
		data, _ := os.ReadFile(src)
		writeFile(t, filepath.Join(args[len(args)-1], "dots-backup-evil.tar.gz"), string(data))
	}}
	dest := filepath.Join(t.TempDir(), "incoming")
	if err := Receive(r, dest, testSHA(t, src)); err == nil {
		t.Fatal("path traversal must be rejected")
	}
}
