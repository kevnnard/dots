package engramsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/clean"
)

// fakeExec records every argv and never runs a real engram.
type fakeExec struct {
	calls [][]string
	res   clean.Result
	err   error
	// write creates the export file named in argv, as engram would.
	write bool
}

func (f *fakeExec) Run(_ context.Context, argv []string) (clean.Result, error) {
	f.calls = append(f.calls, argv)
	if f.write && len(argv) > 2 {
		if err := os.WriteFile(argv[2], []byte(`{"observations":[]}`), 0o644); err != nil {
			return clean.Result{}, err
		}
	}
	return f.res, f.err
}

func installed(ex clean.Executor) Options {
	return Options{Exec: ex, LookPath: func(string) (string, error) { return "/bin/engram", nil }}
}

func missing(ex clean.Executor) Options {
	return Options{Exec: ex, LookPath: func(string) (string, error) { return "", errors.New("not found") }}
}

func TestExportRunsEngramExportAllIntoTheStage(t *testing.T) {
	stage := t.TempDir()
	ex := &fakeExec{write: true}
	line, err := Export(context.Background(), installed(ex), stage)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(stage, "configs", "engram", "memories.json")
	if !reflect.DeepEqual(ex.calls, [][]string{{"engram", "export", want, "--all"}}) {
		t.Errorf("argv = %q", ex.calls)
	}
	if !strings.Contains(line, want) {
		t.Errorf("line %q does not name %s", line, want)
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("export mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestExportWithoutEngramIsOneLineAndNoFailure(t *testing.T) {
	ex := &fakeExec{}
	line, err := Export(context.Background(), missing(ex), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.calls) != 0 {
		t.Errorf("ran %q without engram", ex.calls)
	}
	if line == "" || strings.Contains(line, "\n") {
		t.Errorf("want one line, got %q", line)
	}
	if !strings.Contains(line, "not installed") {
		t.Errorf("line %q does not say engram is missing", line)
	}
}

func TestExportFailureIsReported(t *testing.T) {
	ex := &fakeExec{res: clean.Result{ExitCode: 1, Stderr: []byte("db locked\n")}}
	_, err := Export(context.Background(), installed(ex), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "db locked") {
		t.Errorf("err = %v, want the engram stderr", err)
	}
}

func stageWithExport(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "configs", "engram", "memories.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, file
}

func TestImportRunsEngramImportOnTheStagedExport(t *testing.T) {
	dir, file := stageWithExport(t)
	ex := &fakeExec{}
	line, err := Import(context.Background(), installed(ex), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ex.calls, [][]string{{"engram", "import", file}}) {
		t.Errorf("argv = %q", ex.calls)
	}
	if !strings.Contains(line, "imported") {
		t.Errorf("line = %q", line)
	}
}

func TestImportDryRunImportsNothing(t *testing.T) {
	dir, file := stageWithExport(t)
	ex := &fakeExec{}
	line, err := Import(context.Background(), installed(ex), dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.calls) != 0 {
		t.Errorf("dry run ran %q", ex.calls)
	}
	if !strings.Contains(line, "would import") || !strings.Contains(line, file) {
		t.Errorf("line = %q", line)
	}
}

func TestImportWithoutExportIsReported(t *testing.T) {
	ex := &fakeExec{}
	line, err := Import(context.Background(), installed(ex), t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.calls) != 0 {
		t.Errorf("ran %q without an export", ex.calls)
	}
	if !strings.Contains(line, "no Engram export") {
		t.Errorf("line = %q", line)
	}
}

func TestImportWithoutEngramIsReported(t *testing.T) {
	dir, _ := stageWithExport(t)
	ex := &fakeExec{}
	line, err := Import(context.Background(), missing(ex), dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.calls) != 0 {
		t.Errorf("ran %q without engram", ex.calls)
	}
	if !strings.Contains(line, "not installed") {
		t.Errorf("line = %q", line)
	}
}

func TestImportFailureIsAnError(t *testing.T) {
	dir, _ := stageWithExport(t)
	ex := &fakeExec{res: clean.Result{ExitCode: 2, Stderr: []byte("bad json")}}
	if _, err := Import(context.Background(), installed(ex), dir, false); err == nil || !strings.Contains(err.Error(), "bad json") {
		t.Errorf("err = %v", err)
	}
}

// The zero value never runs anything: it reports engram as missing.
func TestZeroOptionsRunNothing(t *testing.T) {
	dir, _ := stageWithExport(t)
	if line, err := Import(context.Background(), Options{}, dir, false); err != nil || !strings.Contains(line, "not installed") {
		t.Errorf("import: %q, %v", line, err)
	}
	if line, err := Export(context.Background(), Options{}, t.TempDir()); err != nil || !strings.Contains(line, "not installed") {
		t.Errorf("export: %q, %v", line, err)
	}
}
