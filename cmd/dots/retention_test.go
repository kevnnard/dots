package main

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kevnnard/dots/pkg/transfer"
)

func bkFixture(t *testing.T, local string) *cliFixture {
	t.Helper()
	f := newCLIFixture(t)
	t.Setenv("HOME", filepath.Dir(f.target))
	writeTestFile(t, f.target, []byte(local))
	return f
}

func writeCopies(t *testing.T, target string, suffixes ...string) {
	t.Helper()
	for _, s := range suffixes {
		writeTestFile(t, target+s, []byte("old "+s))
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func safetyCopies(t *testing.T, target string) []string {
	t.Helper()
	var out []string
	for _, n := range dirNames(t, filepath.Dir(target)) {
		if strings.HasPrefix(n, filepath.Base(target)+".") && strings.HasSuffix(n, ".bk") {
			out = append(out, n)
		}
	}
	return out
}

func TestRestoreKeepsThreeNewestSafetyCopies(t *testing.T) {
	f := bkFixture(t, "# mine\n")
	writeCopies(t, f.target, ".20200101_000000.bk", ".20200102_000000.bk", ".20200103_000000.bk")
	out, err := runRestore(t, "zsh", "-i", "--with-config", "--replace-existing", "--dir", f.repo)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	got := safetyCopies(t, f.target)
	if len(got) != 3 {
		t.Fatalf("copies = %q", got)
	}
	if _, err := os.Lstat(f.target + ".20200101_000000.bk"); !os.IsNotExist(err) {
		t.Errorf("the oldest copy must be deleted: %q", got)
	}
	for _, keep := range []string{".20200102_000000.bk", ".20200103_000000.bk"} {
		if _, err := os.Lstat(f.target + keep); err != nil {
			t.Errorf("%s must remain: %v", keep, err)
		}
	}
	var fresh string
	for _, n := range got {
		if !strings.HasPrefix(n, ".zshrc.2020") {
			fresh = n
		}
	}
	if data, _ := os.ReadFile(filepath.Join(filepath.Dir(f.target), fresh)); string(data) != "# mine\n" {
		t.Errorf("new safety copy %q = %q", fresh, data)
	}
	if !strings.Contains(out, "🧹 removed 1 older safety backup(s) of "+f.target) {
		t.Errorf("output must report the removed count:\n%s", out)
	}
}

func TestRestoreOrdersSafetyCopiesByTimestampThenNumber(t *testing.T) {
	f := bkFixture(t, "# mine\n")
	writeCopies(t, f.target, ".20200101_000000.bk", ".20200102_000000.bk", ".20200102_000000.2.bk", ".20200102_000000.10.bk")
	out, err := runRestore(t, "zsh", "-i", "--with-config", "--replace-existing", "--dir", f.repo)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	for _, gone := range []string{".20200101_000000.bk", ".20200102_000000.bk"} {
		if _, err := os.Lstat(f.target + gone); !os.IsNotExist(err) {
			t.Errorf("%s must be deleted", gone)
		}
	}
	for _, keep := range []string{".20200102_000000.2.bk", ".20200102_000000.10.bk"} {
		if _, err := os.Lstat(f.target + keep); err != nil {
			t.Errorf("%s must remain: %v", keep, err)
		}
	}
	if len(safetyCopies(t, f.target)) != 3 {
		t.Errorf("copies = %q", safetyCopies(t, f.target))
	}
	if !strings.Contains(out, "🧹 removed 2 older safety backup(s) of "+f.target) {
		t.Errorf("output must report the removed count:\n%s", out)
	}
}

func TestRestoreSafetyCopyRetentionTouchesOnlyDotsCopiesOfThatFile(t *testing.T) {
	f := bkFixture(t, "# mine\n")
	dir := filepath.Dir(f.target)
	writeCopies(t, f.target, ".20200102_000000.bk", ".20200103_000000.bk", ".20200104_000000.bk")
	decoys := []string{
		".zshrc.20190101_000000.bk-1719",
		".zshrc.20190101_000000.bk.old",
		".zshrc.20191301_000000.bk",
		".zshrc.2019_old.bk",
		".zshrc.20190101_000000.x.bk",
		".zshrc.20190101_000000.0.bk",
		".zshrc.bk",
		".zshrc.old.20190101_000000.bk",
		".bashrc.20190101_000000.bk",
		"zshrc.20190101_000000.bk",
	}
	for _, d := range decoys {
		writeTestFile(t, filepath.Join(dir, d), []byte("decoy"))
	}
	asDir := filepath.Join(dir, ".zshrc.20190101_000000.bk")
	writeTestFile(t, filepath.Join(asDir, "inside"), []byte("keep"))
	outside := filepath.Join(t.TempDir(), "target")
	writeTestFile(t, outside, []byte("link target"))
	link := filepath.Join(dir, ".zshrc.20190101_000001.bk")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	out, err := runRestore(t, "zsh", "-i", "--with-config", "--replace-existing", "--dir", f.repo)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	for _, d := range decoys {
		if data, err := os.ReadFile(filepath.Join(dir, d)); err != nil || string(data) != "decoy" {
			t.Errorf("%s must stay untouched: %q %v", d, data, err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(asDir, "inside")); err != nil || string(data) != "keep" {
		t.Errorf("a directory named like a copy must stay: %v", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "link target" {
		t.Errorf("the link target must survive: %q %v", data, err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("the oldest copy, a link, should be removed as an entry: %v", err)
	}
	if _, err := os.Lstat(f.target + ".20200102_000000.bk"); !os.IsNotExist(err) {
		t.Errorf("the oldest regular copy must be deleted")
	}
	for _, keep := range []string{".20200103_000000.bk", ".20200104_000000.bk"} {
		if _, err := os.Lstat(f.target + keep); err != nil {
			t.Errorf("%s must remain: %v", keep, err)
		}
	}
	if !strings.Contains(out, "🧹 removed 2 older safety backup(s) of "+f.target) {
		t.Errorf("output must report the removed count:\n%s", out)
	}
}

func TestRestoreDryRunRemovesNoSafetyCopy(t *testing.T) {
	f := bkFixture(t, "# mine\n")
	copies := []string{".20200101_000000.bk", ".20200102_000000.bk", ".20200103_000000.bk", ".20200104_000000.bk"}
	writeCopies(t, f.target, copies...)
	before := dirNames(t, filepath.Dir(f.target))
	out, err := runRestore(t, "zsh", "-n", "-i", "--with-config", "--replace-existing", "--dir", f.repo)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if after := dirNames(t, filepath.Dir(f.target)); strings.Join(after, ",") != strings.Join(before, ",") {
		t.Errorf("dry run changed the folder: %q -> %q", before, after)
	}
	if strings.Contains(out, "🧹") {
		t.Errorf("dry run must not report removals:\n%s", out)
	}
}

func TestRestoreWithoutOldCopiesReportsNoRemoval(t *testing.T) {
	f := bkFixture(t, "# mine\n")
	out, err := runRestore(t, "zsh", "-i", "--with-config", "--replace-existing", "--dir", f.repo)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if len(safetyCopies(t, f.target)) != 1 || strings.Contains(out, "🧹") {
		t.Errorf("copies = %q\n%s", safetyCopies(t, f.target), out)
	}
}

func receiveSetup(t *testing.T) (*cliFixture, string) {
	t.Helper()
	f := newCLIFixture(t)
	t.Setenv("HOME", t.TempDir())
	archive := filepath.Join(t.TempDir(), "a.tar.gz")
	if _, err := transfer.Archive(f.repo, archive); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	oldRunner := transferRunner
	t.Cleanup(func() { transferRunner = oldRunner })
	transferRunner = &inboxRunner{archive: data}
	return f, transfer.ShortCode(sha256Hex(t, data))
}

func oldReceived(t *testing.T, incoming string, names ...string) {
	t.Helper()
	for i, n := range names {
		p := filepath.Join(incoming, n)
		writeTestFile(t, filepath.Join(p, "manifest.yaml"), []byte("version: old\n"))
		at := time.Date(2020, 1, 1+i, 0, 0, 0, 0, time.UTC)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReceiveKeepsThreeNewestReceivedBackups(t *testing.T) {
	_, code := receiveSetup(t)
	incoming, _ := transfer.IncomingDir()
	oldReceived(t, incoming, "aaaaaaaa1111", "bbbbbbbb", "cccccccc2222", "dddddddd")
	writeTestFile(t, filepath.Join(incoming, "notes.txt"), []byte("mine"))
	writeTestFile(t, filepath.Join(incoming, "My Backup", "file"), []byte("mine"))
	writeTestFile(t, filepath.Join(incoming, "unclaimed", "x.tar.gz"), []byte("mine"))
	writeTestFile(t, filepath.Join(incoming, "ABCDEF12", "file"), []byte("mine"))
	writeTestFile(t, filepath.Join(incoming, "abcdef1234567", "file"), []byte("mine"))
	writeTestFile(t, filepath.Join(incoming, "eeeeeeee"), []byte("a file named like a code"))
	linked := filepath.Join(t.TempDir(), "linked")
	writeTestFile(t, filepath.Join(linked, "file"), []byte("mine"))
	if err := os.Symlink(linked, filepath.Join(incoming, "ffffffff")); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	os.Chtimes(filepath.Join(incoming, "ABCDEF12"), at, at)

	out, err := runBackup(t, "--receive", code)
	if err != nil {
		t.Fatalf("receive: %v\n%s", err, out)
	}
	want := []string{"ABCDEF12", "My Backup", "abcdef1234567", "cccccccc2222", code, "dddddddd", "eeeeeeee", "ffffffff", "latest", "notes.txt", "unclaimed"}
	sort.Strings(want)
	if got := dirNames(t, incoming); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("incoming = %q\nwant %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(linked, "file")); err != nil {
		t.Errorf("the link target must survive: %v", err)
	}
	if latest, err := transfer.LatestReceived(incoming); err != nil || latest != filepath.Join(incoming, code) {
		t.Errorf("latest = %q, %v", latest, err)
	}
	if !strings.Contains(out, "🧹 Removed 2 older received backup(s)\n") {
		t.Errorf("output must report the removal:\n%s", out)
	}
}

func TestReceiveAlwaysKeepsTheBackupJustReceived(t *testing.T) {
	_, code := receiveSetup(t)
	incoming, _ := transfer.IncomingDir()
	future := []string{"aaaaaaaa", "bbbbbbbb", "cccccccc"}
	oldReceived(t, incoming, future...)
	for i, n := range future {
		at := time.Now().Add(time.Duration(i+1) * time.Hour)
		os.Chtimes(filepath.Join(incoming, n), at, at)
	}
	out, err := runBackup(t, "--receive", code)
	if err != nil {
		t.Fatalf("receive: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(incoming, code, "manifest.yaml")); err != nil {
		t.Fatalf("the backup just received must be kept: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(incoming, "aaaaaaaa")); !os.IsNotExist(err) {
		t.Errorf("the oldest received backup must be removed")
	}
	if !strings.Contains(out, "🧹 Removed 1 older received backup(s)") {
		t.Errorf("output:\n%s", out)
	}
}

func TestReceiveWithFewBackupsRemovesNothing(t *testing.T) {
	_, code := receiveSetup(t)
	incoming, _ := transfer.IncomingDir()
	oldReceived(t, incoming, "aaaaaaaa", "bbbbbbbb")
	out, err := runBackup(t, "--receive", code)
	if err != nil {
		t.Fatalf("receive: %v\n%s", err, out)
	}
	if got := dirNames(t, incoming); len(got) != 4 {
		t.Errorf("incoming = %q", got)
	}
	if strings.Contains(out, "🧹") {
		t.Errorf("nothing removed, nothing reported:\n%s", out)
	}
}

func TestReceiveFailureRemovesNothing(t *testing.T) {
	_, code := receiveSetup(t)
	incoming, _ := transfer.IncomingDir()
	oldReceived(t, incoming, "aaaaaaaa", "bbbbbbbb", "cccccccc", "dddddddd")
	bad := "0000000000"
	if strings.HasPrefix(code, "00000000") {
		bad = "1111111111"
	}
	if _, err := runBackup(t, "--receive", bad); err == nil {
		t.Fatal("a wrong code must fail")
	}
	for _, n := range []string{"aaaaaaaa", "bbbbbbbb", "cccccccc", "dddddddd"} {
		if _, err := os.Stat(filepath.Join(incoming, n)); err != nil {
			t.Errorf("%s removed by a failed receive", n)
		}
	}
}

func syncStages(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.Getenv("HOME"), ".cache", "dots", "sync-*"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestSyncRemovesItsStageAfterSuccessfulSend(t *testing.T) {
	cases := []struct {
		name  string
		stdin string
		args  []string
		fail  map[int]error
		err   bool
	}{
		{"applied", "y\n", nil, nil, false},
		{"not applied", "n\n", nil, nil, false},
		{"dry run", "", []string{"--dry-run"}, nil, false},
		{"receive failure", "y\n", nil, map[int]error{1: errors.New("exit status 1")}, true},
		{"apply failure", "", []string{"--yes"}, map[int]error{3: errors.New("exit status 1")}, true},
	}
	for _, c := range cases {
		e := newSyncEnv(t)
		cache := filepath.Join(os.Getenv("HOME"), ".cache", "dots")
		writeTestFile(t, filepath.Join(cache, "sync-older", "keep"), []byte("x"))
		writeTestFile(t, filepath.Join(cache, "backup", "keep"), []byte("x"))
		writeTestFile(t, filepath.Join(cache, "incoming", "keep"), []byte("x"))
		for k, v := range c.fail {
			e.ssh.fail[k] = v
		}
		out, err := runSync(t, c.stdin, append([]string{e.device, "--dir", e.repo}, c.args...)...)
		if (err != nil) != c.err {
			t.Fatalf("%s: err = %v\n%s", c.name, err, out)
		}
		if len(e.inbox.archive) == 0 {
			t.Fatalf("%s: nothing sent", c.name)
		}
		if got := syncStages(t); len(got) != 1 || filepath.Base(got[0]) != "sync-older" {
			t.Errorf("%s: stages left = %q", c.name, got)
		}
		for _, keep := range []string{"sync-older", "backup", "incoming"} {
			if _, err := os.Stat(filepath.Join(cache, keep, "keep")); err != nil {
				t.Errorf("%s: %s must stay: %v", c.name, keep, err)
			}
		}
	}
}

func TestSyncKeepsAndPrintsStageWhenSendFails(t *testing.T) {
	e := newSyncEnv(t)
	transferRunner = &failingSend{}
	out, err := runSync(t, "", e.device, "--yes", "--dir", e.repo)
	if err == nil {
		t.Fatal("want send error")
	}
	got := syncStages(t)
	if len(got) != 1 {
		t.Fatalf("the stage must stay: %q", got)
	}
	if _, err := os.Stat(filepath.Join(got[0], "manifest.yaml")); err != nil {
		t.Errorf("stage content gone: %v", err)
	}
	if !strings.Contains(out, "Stage kept: "+got[0]+"\n") {
		t.Errorf("output must print the kept stage:\n%s", out)
	}
}
