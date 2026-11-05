package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/transfer"
	"github.com/spf13/pflag"
)

// inboxRunner plays `tailscale file`: cp records the archive, get drops it
// into the requested inbox.
type inboxRunner struct{ archive []byte }

func (r *inboxRunner) Run(name string, args ...string) ([]byte, error) {
	switch args[1] {
	case "cp":
		data, err := os.ReadFile(args[2])
		r.archive = data
		return nil, err
	case "get":
		return nil, os.WriteFile(filepath.Join(args[len(args)-1], transfer.ArchivePrefix+"test.tar.gz"), r.archive, 0o644)
	}
	return nil, nil
}

// runBackup executes `dots backup` with fresh flag values and captures stdout.
func runBackup(t *testing.T, args ...string) (string, error) {
	t.Helper()
	sendFlag, receiveFlag, sha256Flag, intoFlag, outFlag, dotfilesDirFlag = "", false, "", "", "", ""
	// cobra keeps Changed between runs, and --send is dispatched on it.
	backupCmd.Flags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
	old := os.Stdout
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = wr
	rootCmd.SetArgs(append([]string{"backup"}, args...))
	runErr := rootCmd.Execute()
	wr.Close()
	os.Stdout = old
	var out bytes.Buffer
	io.Copy(&out, rd)
	return out.String(), runErr
}

// The sender prints a short code and the two commands to run on the receiver;
// the receiver types only that code and restore finds the backup by itself.
func TestSendPrintsShortCodeAndReceiveAcceptsIt(t *testing.T) {
	f := newCLIFixture(t)
	t.Setenv("HOME", t.TempDir())
	r := &inboxRunner{}
	oldRunner := transferRunner
	t.Cleanup(func() { transferRunner = oldRunner })
	transferRunner = r

	stage := filepath.Join(t.TempDir(), "stage")
	for _, rel := range []string{"manifest.yaml", "configs/zsh/.zshrc"} {
		data, err := os.ReadFile(filepath.Join(f.repo, rel))
		if err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(stage, rel), data)
	}
	out, err := runBackup(t, "--send", "station", "--out", stage, "--dir", f.repo)
	if err != nil {
		t.Fatalf("send: %v\n%s", err, out)
	}
	code := transfer.ShortCode(sha256Hex(t, r.archive))
	for _, want := range []string{
		"Code: " + code,
		"dots backup --receive " + code,
		"dots restore --received --all --dry-run",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("send output lacks %q:\n%s", want, out)
		}
	}

	out, err = runBackup(t, "--receive", code)
	if err != nil {
		t.Fatalf("receive: %v\n%s", err, out)
	}
	incoming, _ := transfer.IncomingDir()
	want := filepath.Join(incoming, code)
	if _, err := os.Stat(filepath.Join(want, "configs", "zsh", ".zshrc")); err != nil {
		t.Fatalf("backup not extracted into %s: %v", want, err)
	}
	if got, err := transfer.LatestReceived(incoming); err != nil || got != want {
		t.Errorf("latest received = %q, %v; want %q", got, err, want)
	}
	if !strings.Contains(out, "dots restore --received --all --dry-run") {
		t.Errorf("receive output must give the restore command:\n%s", out)
	}
}

// The full sha256 through --sha256 keeps working.
func TestReceiveStillAcceptsFullSha256Flag(t *testing.T) {
	f := newCLIFixture(t)
	t.Setenv("HOME", t.TempDir())
	archive := filepath.Join(t.TempDir(), "a.tar.gz")
	sum, err := transfer.Archive(f.repo, archive)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(archive)
	oldRunner := transferRunner
	t.Cleanup(func() { transferRunner = oldRunner })
	transferRunner = &inboxRunner{archive: data}

	out, err := runBackup(t, "--receive", "--sha256", sum)
	if err != nil {
		t.Fatalf("receive: %v\n%s", err, out)
	}
	incoming, _ := transfer.IncomingDir()
	if _, err := os.Stat(filepath.Join(incoming, sum[:12], "manifest.yaml")); err != nil {
		t.Fatalf("full sha256 must extract into the 12-character folder: %v", err)
	}
}

func TestReceiveWithoutCodeFailsBeforeTouchingInbox(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r := &inboxRunner{}
	oldRunner := transferRunner
	t.Cleanup(func() { transferRunner = oldRunner })
	transferRunner = r
	_, err := runBackup(t, "--receive")
	if err == nil || !strings.Contains(err.Error(), "dots backup --receive <code>") {
		t.Fatalf("want missing code error, got %v", err)
	}
	incoming, _ := transfer.IncomingDir()
	if _, err := os.Stat(incoming); !os.IsNotExist(err) {
		t.Error("nothing may be written without a code")
	}
}

func writeTestFile(t *testing.T, p string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func sha256Hex(t *testing.T, data []byte) string {
	t.Helper()
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
