package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/clean"
	"github.com/kevnnard/dots/pkg/engramsync"
)

type engramFake struct{ calls [][]string }

func (f *engramFake) Run(_ context.Context, argv []string) (clean.Result, error) {
	f.calls = append(f.calls, argv)
	if len(argv) > 2 && argv[1] == "export" {
		return clean.Result{}, os.WriteFile(argv[2], []byte(`{}`), 0o644)
	}
	return clean.Result{}, nil
}

func useEngram(t *testing.T, installed bool) *engramFake {
	t.Helper()
	f := &engramFake{}
	old := engramOptions
	t.Cleanup(func() { engramOptions = old })
	engramOptions = func() engramsync.Options {
		if !installed {
			return engramsync.Options{}
		}
		return engramsync.Options{Exec: f, LookPath: func(string) (string, error) { return "/bin/engram", nil }}
	}
	return f
}

func engramBackupRepo(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "manifest.yaml"), []byte(discoverManifest))
	writeTestFile(t, filepath.Join(home, ".zshrc"), []byte("# zsh\n"))
	return repo
}

func TestFullBackupExportsEngramMemoriesIntoTheStage(t *testing.T) {
	repo := engramBackupRepo(t)
	f := useEngram(t, true)
	stage := filepath.Join(t.TempDir(), "stage")
	out, err := runBackup(t, "--dir", repo, "--out", stage)
	if err != nil {
		t.Fatalf("backup: %v\n%s", err, out)
	}
	file := filepath.Join(stage, engramsync.StagedFile)
	if !reflect.DeepEqual(f.calls, [][]string{{"engram", "export", file, "--all"}}) {
		t.Errorf("engram argv = %q", f.calls)
	}
	if !strings.Contains(out, "Engram memories exported: "+file) {
		t.Errorf("output lacks the export line:\n%s", out)
	}
}

func TestBackupWithoutEngramSaysSoAndSucceeds(t *testing.T) {
	repo := engramBackupRepo(t)
	useEngram(t, false)
	out, err := runBackup(t, "--dir", repo, "--out", filepath.Join(t.TempDir(), "stage"))
	if err != nil {
		t.Fatalf("backup must not fail without engram: %v\n%s", err, out)
	}
	if strings.Count(out, "Engram not installed") != 1 {
		t.Errorf("want one engram line:\n%s", out)
	}
}

func TestSingleAppBackupDoesNotExportEngram(t *testing.T) {
	repo := engramBackupRepo(t)
	f := useEngram(t, true)
	if out, err := runBackup(t, "zsh", "--dir", repo, "--out", filepath.Join(t.TempDir(), "stage")); err != nil {
		t.Fatalf("backup: %v\n%s", err, out)
	}
	if len(f.calls) != 0 {
		t.Errorf("single app backup ran %q", f.calls)
	}
}

func stageEngramExport(t *testing.T, dir string) string {
	t.Helper()
	file := filepath.Join(dir, engramsync.StagedFile)
	writeTestFile(t, file, []byte(`{}`))
	return file
}

func TestRestoreWithConfigImportsEngramMemories(t *testing.T) {
	fx := newCLIFixture(t)
	file := stageEngramExport(t, fx.repo)
	f := useEngram(t, true)
	out, err := runRestore(t, "-i", "--with-config", "--dir", fx.repo)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if !reflect.DeepEqual(f.calls, [][]string{{"engram", "import", file}}) {
		t.Errorf("engram argv = %q", f.calls)
	}
	if !strings.Contains(out, "Engram memories imported from "+file) {
		t.Errorf("output lacks the import line:\n%s", out)
	}
}

func TestRestoreReceivedImportsEngramMemories(t *testing.T) {
	fx := receivedFixture(t)
	file := stageEngramExport(t, fx.repo)
	f := useEngram(t, true)
	if out, err := runRestore(t, "-i", "--received"); err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if !reflect.DeepEqual(f.calls, [][]string{{"engram", "import", file}}) {
		t.Errorf("engram argv = %q", f.calls)
	}
}

func TestRestoreDryRunImportsNoEngramMemories(t *testing.T) {
	fx := newCLIFixture(t)
	stageEngramExport(t, fx.repo)
	f := useEngram(t, true)
	out, err := runRestore(t, "--with-config", "--dry-run", "--dir", fx.repo)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if len(f.calls) != 0 {
		t.Errorf("dry run ran %q", f.calls)
	}
	if !strings.Contains(out, "Engram: would import memories") {
		t.Errorf("output lacks the dry-run line:\n%s", out)
	}
}

func TestRestoreWithoutEngramExportOrEngramIsReportedNotFailed(t *testing.T) {
	fx := newCLIFixture(t)
	f := useEngram(t, true)
	out, err := runRestore(t, "-i", "--with-config", "--dir", fx.repo)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if len(f.calls) != 0 || !strings.Contains(out, "no Engram export in "+fx.repo) {
		t.Errorf("missing export: calls %q\n%s", f.calls, out)
	}

	stageEngramExport(t, fx.repo)
	useEngram(t, false)
	out, err = runRestore(t, "-i", "--with-config", "--dir", fx.repo)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Engram not installed") {
		t.Errorf("missing engram not reported:\n%s", out)
	}
}

func TestRestoreWithoutConfigOrForOneAppDoesNotImport(t *testing.T) {
	fx := newCLIFixture(t)
	stageEngramExport(t, fx.repo)
	f := useEngram(t, true)
	for _, args := range [][]string{{"--dir", fx.repo}, {"zsh", "-i", "--with-config", "--dir", fx.repo}} {
		if out, err := runRestore(t, args...); err != nil {
			t.Fatalf("restore %q: %v\n%s", args, err, out)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("ran %q", f.calls)
	}
}
