package ui

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/clean"
	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/engramsync"
	"github.com/kevnnard/dots/pkg/manifest"
)

type engramFake struct{ calls [][]string }

func (f *engramFake) Run(_ context.Context, argv []string) (clean.Result, error) {
	f.calls = append(f.calls, argv)
	if len(argv) > 2 && argv[1] == "export" {
		return clean.Result{}, os.WriteFile(argv[2], []byte(`{}`), 0o644)
	}
	return clean.Result{}, nil
}

func (f *engramFake) options() engramsync.Options {
	return engramsync.Options{Exec: f, LookPath: func(string) (string, error) { return "/bin/engram", nil }}
}

func engramBackupModel(t *testing.T) Model {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()
	model := NewModel(&manifest.Manifest{Version: "1.0.0", Apps: map[string]manifest.AppConfig{}}, &detector.SystemInfo{OS: "darwin"}, repo)
	model.Action = "backup"
	return model
}

func TestBackupExportsEngramMemories(t *testing.T) {
	model := engramBackupModel(t)
	f := &engramFake{}
	model.Engram = f.options()
	done, _ := model.executeAction()

	file := filepath.Join(os.Getenv("HOME"), ".cache", "dots", "backup", engramsync.StagedFile)
	if !reflect.DeepEqual(f.calls, [][]string{{"engram", "export", file, "--all"}}) {
		t.Errorf("engram argv = %q", f.calls)
	}
	if logs := strings.Join(done.(Model).Logs, "\n"); !strings.Contains(logs, "Engram memories exported") {
		t.Errorf("logs lack the export line:\n%s", logs)
	}
}

func TestBackupWithoutEngramSaysSo(t *testing.T) {
	done, _ := engramBackupModel(t).executeAction()
	logs := strings.Join(done.(Model).Logs, "\n")
	if strings.Count(logs, "Engram not installed") != 1 {
		t.Errorf("want one engram line:\n%s", logs)
	}
}

func restoreAll(f *tuiFixture) Model {
	for i := range f.model.AppItems {
		f.model.AppItems[i].Checked = true
	}
	f.model.Action = "restore"
	m, cmd := f.model.executeAction()
	return drain(m, cmd).(Model)
}

func TestRestoreWithConfigImportsEngramMemories(t *testing.T) {
	tf := newTUIFixture(t)
	file := filepath.Join(tf.model.RepoDir, engramsync.StagedFile)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &engramFake{}
	tf.model.Engram = f.options()
	tf.model.WithConfig = true
	tf.model.ConfigDir = tf.model.RepoDir
	done := restoreAll(tf)

	if !reflect.DeepEqual(f.calls, [][]string{{"engram", "import", file}}) {
		t.Errorf("engram argv = %q", f.calls)
	}
	if logs := strings.Join(done.Logs, "\n"); !strings.Contains(logs, "Engram memories imported from "+file) {
		t.Errorf("logs lack the import line:\n%s", logs)
	}
}

func TestRestoreWithoutConfigDoesNotImport(t *testing.T) {
	tf := newTUIFixture(t)
	f := &engramFake{}
	tf.model.Engram = f.options()
	restoreAll(tf)
	if len(f.calls) != 0 {
		t.Errorf("ran %q", f.calls)
	}
}
