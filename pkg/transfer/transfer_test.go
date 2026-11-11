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
	if _, err := Receive(r, dest, sum); err != nil {
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
	_, err := Receive(r, dest, strings.Repeat("0", 64))
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
	_, err := Receive(r, dest, strings.Repeat("0", 64))
	if err == nil || !strings.Contains(err.Error(), "no dots backup") {
		t.Fatalf("want no-backup error, got %v", err)
	}

	existing := t.TempDir()
	if _, err := Receive(&fakeRunner{}, existing, strings.Repeat("0", 64)); err == nil {
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
	if _, err := Receive(r, dest, testSHA(t, src)); err == nil {
		t.Fatal("path traversal must be rejected")
	}
}

// The receiver may type only the short code the sender prints: its first
// characters still have to match the archive, and nothing else is accepted.
func TestReceiveAcceptsShortCode(t *testing.T) {
	repo := fixtureRepo(t)
	src := filepath.Join(t.TempDir(), "dots-backup-ok.tar.gz")
	sum, err := Archive(repo, src)
	if err != nil {
		t.Fatal(err)
	}
	code := ShortCode(sum)
	if len(code) != MinCodeLength || !strings.HasPrefix(sum, code) {
		t.Fatalf("ShortCode(%s) = %q, want its first %d characters", sum, code, MinCodeLength)
	}
	r := &fakeRunner{onGet: func(args []string) {
		data, _ := os.ReadFile(src)
		writeFile(t, filepath.Join(args[len(args)-1], "dots-backup-ok.tar.gz"), string(data))
	}}
	dest := filepath.Join(t.TempDir(), "incoming")
	if _, err := Receive(r, dest, strings.ToUpper(code)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "manifest.yaml")); err != nil {
		t.Fatalf("manifest not extracted: %v", err)
	}
}

func TestReceiveRejectsShortCodeMismatch(t *testing.T) {
	repo := fixtureRepo(t)
	src := filepath.Join(t.TempDir(), "dots-backup-bad.tar.gz")
	sum, err := Archive(repo, src)
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{onGet: func(args []string) {
		data, _ := os.ReadFile(src)
		writeFile(t, filepath.Join(args[len(args)-1], "dots-backup-bad.tar.gz"), string(data))
	}}
	wrong := "00000000"
	if strings.HasPrefix(sum, wrong) {
		wrong = "11111111"
	}
	dest := filepath.Join(t.TempDir(), "incoming")
	_, err = Receive(r, dest, wrong)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("want code mismatch error, got %v", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatal("nothing may be extracted after a code mismatch")
	}
}

func TestReceiveRejectsMalformedCodeBeforeTouchingInbox(t *testing.T) {
	for _, code := range []string{"", "1b6d846", "zzzzzzzz", strings.Repeat("a", 65)} {
		r := &fakeRunner{}
		dest := filepath.Join(t.TempDir(), "incoming")
		_, err := Receive(r, dest, code)
		if err == nil || !strings.Contains(err.Error(), "code") {
			t.Errorf("code %q: want malformed code error, got %v", code, err)
		}
		if len(r.calls) != 0 {
			t.Errorf("code %q: the Taildrop inbox must not be read, got %v", code, r.calls)
		}
	}
}

func TestLatestReceivedRoundTrip(t *testing.T) {
	root := t.TempDir()
	if _, err := LatestReceived(root); err == nil || !strings.Contains(err.Error(), "no received backup") {
		t.Fatalf("want no-received-backup error, got %v", err)
	}
	dest := filepath.Join(root, "1b6d8464")
	writeFile(t, filepath.Join(dest, "manifest.yaml"), "version: 1\n")
	if err := RecordLatest(root, dest); err != nil {
		t.Fatal(err)
	}
	got, err := LatestReceived(root)
	if err != nil || got != dest {
		t.Fatalf("LatestReceived = %q, %v; want %q", got, err, dest)
	}
	if err := os.RemoveAll(dest); err != nil {
		t.Fatal(err)
	}
	if _, err := LatestReceived(root); err == nil {
		t.Fatal("a removed received backup must not be reported as latest")
	}
}

// Symlinks travel as links, so skills shared through ~/.agents/skills arrive.
func TestArchiveCarriesSymlinksAndReceiveRecreatesThem(t *testing.T) {
	repo := fixtureRepo(t)
	skills := filepath.Join(repo, "configs", "gentle-shell", "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../.agents/skills/terminal-browser", filepath.Join(skills, "terminal-browser")); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "dots-backup-links.tar.gz")
	sum, err := Archive(repo, src)
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{onGet: func(args []string) {
		data, _ := os.ReadFile(src)
		writeFile(t, filepath.Join(args[len(args)-1], "dots-backup-links.tar.gz"), string(data))
	}}
	dest := filepath.Join(t.TempDir(), "incoming")
	if _, err := Receive(r, dest, sum); err != nil {
		t.Fatal(err)
	}
	if link, err := os.Readlink(filepath.Join(dest, "configs", "gentle-shell", "skills", "terminal-browser")); err != nil || link != "../../../.agents/skills/terminal-browser" {
		t.Fatalf("symlink not recreated: %q %v", link, err)
	}
}

// An archive may hold only relative links, and nothing is ever written
// through one: links are created after every file, never under another link.
func TestReceiveRejectsUnsafeSymlinks(t *testing.T) {
	cases := map[string][]*tar.Header{
		"absolute target": {{Name: "configs/x", Typeflag: tar.TypeSymlink, Linkname: "/etc"}},
		"file through link": {
			{Name: "configs/dir", Typeflag: tar.TypeSymlink, Linkname: "../.."},
			{Name: "configs/dir/evil", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1},
		},
		"link under link": {
			{Name: "configs/dir", Typeflag: tar.TypeSymlink, Linkname: "../.."},
			{Name: "configs/dir/evil", Typeflag: tar.TypeSymlink, Linkname: "x"},
		},
	}
	for name, headers := range cases {
		src := filepath.Join(t.TempDir(), "dots-backup-evil.tar.gz")
		f, _ := os.Create(src)
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)
		for _, h := range headers {
			_ = tw.WriteHeader(h)
			if h.Typeflag == tar.TypeReg {
				_, _ = tw.Write([]byte("x"))
			}
		}
		_ = tw.Close()
		_ = gz.Close()
		_ = f.Close()
		r := &fakeRunner{onGet: func(args []string) {
			data, _ := os.ReadFile(src)
			writeFile(t, filepath.Join(args[len(args)-1], "dots-backup-evil.tar.gz"), string(data))
		}}
		parent := t.TempDir()
		dest := filepath.Join(parent, "incoming")
		if _, err := Receive(r, dest, testSHA(t, src)); err == nil {
			t.Errorf("%s: must be rejected", name)
		}
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Errorf("%s: nothing may be extracted", name)
		}
		if _, err := os.Lstat(filepath.Join(parent, "evil")); !os.IsNotExist(err) {
			t.Errorf("%s: wrote outside the destination", name)
		}
	}
}

// Taildrop hands over every pending file at once. Copies of the matching
// backup are fine; any other backup is kept in unclaimed/, never lost.
func TestReceivePicksMatchingBackupAndKeepsOthers(t *testing.T) {
	repo := fixtureRepo(t)
	src := filepath.Join(t.TempDir(), "a.tar.gz")
	sum, err := Archive(repo, src)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "b.tar.gz")
	writeFile(t, filepath.Join(repo, "configs", "zsh", ".zshrc"), "export B=2\n")
	if _, err := Archive(repo, other); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{onGet: func(args []string) {
		inbox := args[len(args)-1]
		data, _ := os.ReadFile(src)
		writeFile(t, filepath.Join(inbox, "dots-backup-1.tar.gz"), string(data))
		writeFile(t, filepath.Join(inbox, "dots-backup-2.tar.gz"), string(data))
		odata, _ := os.ReadFile(other)
		writeFile(t, filepath.Join(inbox, "dots-backup-3.tar.gz"), string(odata))
	}}
	parent := t.TempDir()
	dest := filepath.Join(parent, "code")
	unclaimed, err := Receive(r, dest, ShortCode(sum))
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dest, "configs", "zsh", ".zshrc")); string(data) != "export A=1\n" {
		t.Errorf("extracted the wrong backup: %q", data)
	}
	want := filepath.Join(parent, "unclaimed", "dots-backup-3.tar.gz")
	if len(unclaimed) != 1 || unclaimed[0] != want {
		t.Fatalf("unclaimed = %v, want [%s]", unclaimed, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("the other backup must be kept: %v", err)
	}
}

func TestReceiveWithoutMatchKeepsEveryBackup(t *testing.T) {
	repo := fixtureRepo(t)
	src := filepath.Join(t.TempDir(), "a.tar.gz")
	if _, err := Archive(repo, src); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{onGet: func(args []string) {
		data, _ := os.ReadFile(src)
		writeFile(t, filepath.Join(args[len(args)-1], "dots-backup-1.tar.gz"), string(data))
	}}
	parent := t.TempDir()
	dest := filepath.Join(parent, "code")
	unclaimed, err := Receive(r, dest, "00000000")
	if err == nil || !strings.Contains(err.Error(), "does not match code 00000000") {
		t.Fatalf("want mismatch error, got %v", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("nothing may be extracted without a match")
	}
	if len(unclaimed) != 1 {
		t.Fatalf("unclaimed = %v, want the received backup kept", unclaimed)
	}
	if _, err := os.Stat(unclaimed[0]); err != nil {
		t.Errorf("backup lost: %v", err)
	}
}
