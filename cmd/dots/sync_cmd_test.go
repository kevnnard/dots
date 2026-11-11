package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/transfer"
	"github.com/spf13/pflag"
)

type sshCall struct {
	name string
	args []string
}

type fakeStreamer struct {
	calls []sshCall
	fail  map[int]error
}

func (f *fakeStreamer) Stream(out io.Writer, name string, args ...string) error {
	f.calls = append(f.calls, sshCall{name, append([]string(nil), args...)})
	n := len(f.calls)
	io.WriteString(out, "remote step "+string(rune('0'+n))+"\n")
	return f.fail[n]
}

type failingSend struct{ calls int }

func (f *failingSend) Run(name string, args ...string) ([]byte, error) {
	f.calls++
	return []byte("no such peer"), errors.New("exit status 1")
}

type exitErr struct{ code int }

func (e exitErr) Error() string { return "exit status 255" }
func (e exitErr) ExitCode() int { return e.code }

type syncEnv struct {
	repo   string
	ssh    *fakeStreamer
	inbox  *inboxRunner
	device string
}

func newSyncEnv(t *testing.T) *syncEnv {
	t.Helper()
	f := newCLIFixture(t)
	t.Setenv("HOME", t.TempDir())
	writeTestFile(t, f.target, []byte("# zsh\n"))
	e := &syncEnv{repo: f.repo, ssh: &fakeStreamer{fail: map[int]error{}}, inbox: &inboxRunner{}, device: "kevnnard-station"}
	oldRunner, oldSync := transferRunner, syncRunner
	t.Cleanup(func() {
		transferRunner, syncRunner = oldRunner, oldSync
		rootCmd.SetIn(nil)
	})
	transferRunner, syncRunner = e.inbox, e.ssh
	return e
}

func (e *syncEnv) code(t *testing.T) string {
	t.Helper()
	return transfer.ShortCode(sha256Hex(t, e.inbox.archive))
}

func runSync(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	syncYesFlag, syncDryRunFlag, syncNoInstallFlag, dotfilesDirFlag = false, false, false, ""
	syncCmd.Flags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
	rootCmd.SetIn(strings.NewReader(stdin))
	old := os.Stdout
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = wr
	rootCmd.SetOut(wr)
	defer rootCmd.SetOut(nil)
	done := make(chan []byte)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, rd)
		done <- buf.Bytes()
	}()
	rootCmd.SetArgs(append([]string{"sync"}, args...))
	runErr := rootCmd.Execute()
	wr.Close()
	os.Stdout = old
	return string(<-done), runErr
}

func remoteScript(inner string) []string {
	return []string{"kevnnard-station", `"$SHELL" -lic '` + inner + `'`}
}

func TestSyncDryRunStopsAfterRemoteDryRun(t *testing.T) {
	e := newSyncEnv(t)
	out, err := runSync(t, "y\n", e.device, "--dry-run", "--dir", e.repo)
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	if len(e.inbox.archive) == 0 {
		t.Fatal("the backup was not sent")
	}
	want := []sshCall{
		{"ssh", remoteScript("dots backup --receive " + e.code(t))},
		{"ssh", remoteScript("dots restore --received --all --replace-existing --install-packages --dry-run")},
	}
	if !reflect.DeepEqual(e.ssh.calls, want) {
		t.Fatalf("ssh calls = %q\nwant %q", e.ssh.calls, want)
	}
	if strings.Contains(out, "Apply on") {
		t.Errorf("--dry-run must not ask:\n%s", out)
	}
	if !strings.Contains(out, "remote step 1") || !strings.Contains(out, "remote step 2") {
		t.Errorf("remote output not shown:\n%s", out)
	}
}

func TestSyncAppliesWhenAnsweredYes(t *testing.T) {
	for _, answer := range []string{"y\n", "yes\n", "Y\n"} {
		e := newSyncEnv(t)
		out, err := runSync(t, answer, e.device, "--dir", e.repo)
		if err != nil {
			t.Fatalf("sync: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Apply on kevnnard-station? [y/N]") {
			t.Errorf("no prompt:\n%s", out)
		}
		want := []sshCall{
			{"ssh", remoteScript("dots backup --receive " + e.code(t))},
			{"ssh", remoteScript("dots restore --received --all --replace-existing --install-packages --dry-run")},
			{"ssh", remoteScript("dots restore --received --all --replace-existing --install-packages")},
		}
		if !reflect.DeepEqual(e.ssh.calls, want) {
			t.Fatalf("answer %q: ssh calls = %q\nwant %q", answer, e.ssh.calls, want)
		}
	}
}

func TestSyncDoesNotApplyUnlessAnsweredYes(t *testing.T) {
	for _, answer := range []string{"n\n", "\n", "", "yep\n"} {
		e := newSyncEnv(t)
		out, err := runSync(t, answer, e.device, "--dir", e.repo)
		if err != nil {
			t.Fatalf("sync: %v\n%s", err, out)
		}
		if len(e.ssh.calls) != 2 {
			t.Fatalf("answer %q must not apply: %q", answer, e.ssh.calls)
		}
		if !strings.Contains(out, "Not applied") {
			t.Errorf("answer %q: no notice:\n%s", answer, out)
		}
	}
}

func TestSyncYesAppliesWithoutAskingAndNoInstallDropsPackages(t *testing.T) {
	e := newSyncEnv(t)
	out, err := runSync(t, "", e.device, "--yes", "--no-install", "--dir", e.repo)
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	if strings.Contains(out, "Apply on") {
		t.Errorf("--yes must not ask:\n%s", out)
	}
	want := []sshCall{
		{"ssh", remoteScript("dots backup --receive " + e.code(t))},
		{"ssh", remoteScript("dots restore --received --all --replace-existing --dry-run")},
		{"ssh", remoteScript("dots restore --received --all --replace-existing")},
	}
	if !reflect.DeepEqual(e.ssh.calls, want) {
		t.Fatalf("ssh calls = %q\nwant %q", e.ssh.calls, want)
	}
}

func TestSyncSSHFailureStopsAndNamesSSH(t *testing.T) {
	e := newSyncEnv(t)
	e.ssh.fail[1] = exitErr{255}
	out, err := runSync(t, "y\n", e.device, "--dir", e.repo)
	if err == nil || !strings.Contains(err.Error(), "ssh kevnnard-station") {
		t.Fatalf("want error naming ssh kevnnard-station, got %v\n%s", err, out)
	}
	if len(e.ssh.calls) != 1 {
		t.Fatalf("nothing may run after a failed step: %q", e.ssh.calls)
	}
}

func TestSyncApplyFailureIsNonzero(t *testing.T) {
	e := newSyncEnv(t)
	e.ssh.fail[3] = errors.New("exit status 1")
	_, err := runSync(t, "", e.device, "--yes", "--dir", e.repo)
	if err == nil || !strings.Contains(err.Error(), "ssh kevnnard-station") {
		t.Fatalf("want apply error, got %v", err)
	}
}

func TestSyncSendFailureRunsNoSSH(t *testing.T) {
	e := newSyncEnv(t)
	send := &failingSend{}
	transferRunner = send
	_, err := runSync(t, "y\n", e.device, "--yes", "--dir", e.repo)
	if err == nil || !strings.Contains(err.Error(), "tailscale file cp") {
		t.Fatalf("want send error, got %v", err)
	}
	if send.calls != 1 || len(e.ssh.calls) != 0 {
		t.Fatalf("send calls %d, ssh calls %q", send.calls, e.ssh.calls)
	}
}

func TestSyncRejectsDeviceThatLooksLikeAnOption(t *testing.T) {
	e := newSyncEnv(t)
	_, err := runSync(t, "", "--dir", e.repo, "--", "-oProxyCommand=x")
	if err == nil {
		t.Fatal("want error")
	}
	if len(e.ssh.calls) != 0 || len(e.inbox.archive) != 0 {
		t.Fatal("nothing may run for an invalid device")
	}
}

func TestSyncStagesIntoFreshPrivateFolder(t *testing.T) {
	e := newSyncEnv(t)
	if _, err := runSync(t, "", e.device, "--dry-run", "--dir", e.repo); err != nil {
		t.Fatal(err)
	}
	def := filepath.Join(os.Getenv("HOME"), ".cache", "dots", "backup")
	if _, err := os.Stat(def); !os.IsNotExist(err) {
		t.Errorf("sync must not stage into the default folder %s", def)
	}
}

func TestSyncReceiveFailureStopsBeforeDryRun(t *testing.T) {
	e := newSyncEnv(t)
	e.ssh.fail[1] = errors.New("exit status 1")
	_, err := runSync(t, "y\n", e.device, "--dir", e.repo)
	if err == nil || !strings.Contains(err.Error(), "ssh kevnnard-station") {
		t.Fatalf("want receive error, got %v", err)
	}
	if len(e.ssh.calls) != 1 {
		t.Fatalf("an older received backup must never be restored: %q", e.ssh.calls)
	}
}

func TestSyncDryRunWithProblemsStillAsks(t *testing.T) {
	e := newSyncEnv(t)
	e.ssh.fail[2] = errors.New("exit status 1")
	out, err := runSync(t, "y\n", e.device, "--dir", e.repo)
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	if !strings.Contains(out, "reported problems") || !strings.Contains(out, "Apply on kevnnard-station? [y/N]") {
		t.Errorf("problems must be reported and the question asked:\n%s", out)
	}
	if len(e.ssh.calls) != 3 {
		t.Fatalf("answer y must apply: %q", e.ssh.calls)
	}
}

func TestSyncYesStopsWhenDryRunReportsProblems(t *testing.T) {
	e := newSyncEnv(t)
	e.ssh.fail[2] = errors.New("exit status 1")
	_, err := runSync(t, "", e.device, "--yes", "--dir", e.repo)
	if err == nil || !strings.Contains(err.Error(), "without --yes") {
		t.Fatalf("want stop, got %v", err)
	}
	if len(e.ssh.calls) != 2 {
		t.Fatalf("--yes must not apply a plan with problems: %q", e.ssh.calls)
	}
}

func TestSyncDryRunSSHFailureStops(t *testing.T) {
	e := newSyncEnv(t)
	e.ssh.fail[2] = exitErr{255}
	_, err := runSync(t, "y\n", e.device, "--dir", e.repo)
	if err == nil || !strings.Contains(err.Error(), "ssh kevnnard-station") {
		t.Fatalf("want ssh error, got %v", err)
	}
	if len(e.ssh.calls) != 2 {
		t.Fatalf("nothing may run after a lost connection: %q", e.ssh.calls)
	}
}

func TestBackupSendOutputUnchanged(t *testing.T) {
	f := newCLIFixture(t)
	t.Setenv("HOME", t.TempDir())
	r := &inboxRunner{}
	oldRunner := transferRunner
	t.Cleanup(func() { transferRunner = oldRunner })
	transferRunner = r
	out, err := runBackup(t, "--send", "station", "--out", f.repo, "--dir", t.TempDir())
	if err != nil {
		t.Fatalf("send: %v\n%s", err, out)
	}
	sum := sha256Hex(t, r.archive)
	code := transfer.ShortCode(sum)
	name := strings.TrimSuffix(strings.TrimPrefix(strings.SplitN(out, "\n", 2)[0], "✈️  Sent "), " to station")
	if !strings.HasPrefix(name, transfer.ArchivePrefix) || !strings.HasSuffix(name, ".tar.gz") {
		t.Fatalf("unexpected archive name %q in:\n%s", name, out)
	}
	want := "✈️  Sent " + name + " to station\n" +
		"   Code: " + code + " (sha256 " + sum + ")\n" +
		"   On the receiving machine:\n" +
		"     dots backup --receive " + code + "\n" +
		"     dots restore --received --all --dry-run\n"
	if out != want {
		t.Fatalf("send output changed:\n%q\nwant\n%q", out, want)
	}
}
