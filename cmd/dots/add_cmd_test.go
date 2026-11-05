package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// `dots add` records the path in the manifest and stages the file in the
// private backup folder, never inside the repository.
func TestAddStagesIntoPrivateBackupNotRepository(t *testing.T) {
	f := newCLIFixture(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, ".config", "starship.toml")
	writeTestFile(t, src, []byte("format = \"$all\"\n"))

	autoBackupFlag, dotfilesDirFlag = true, ""
	addCmd.Flags().VisitAll(func(fl *pflag.Flag) { fl.Changed = false })
	old := os.Stdout
	rd, wr, _ := os.Pipe()
	os.Stdout = wr
	rootCmd.SetArgs([]string{"add", "starship", src, "--dir", f.repo})
	err := rootCmd.Execute()
	wr.Close()
	os.Stdout = old
	out, _ := readAllString(rd)
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	manifest, _ := os.ReadFile(filepath.Join(f.repo, "manifest.yaml"))
	if !strings.Contains(string(manifest), "configs/starship/starship.toml") {
		t.Errorf("manifest must record the new path:\n%s", manifest)
	}
	staged := filepath.Join(home, ".cache", "dots", "backup", "configs", "starship", "starship.toml")
	if data, err := os.ReadFile(staged); err != nil || string(data) != "format = \"$all\"\n" {
		t.Errorf("file not staged in the private backup folder: %q %v\n%s", data, err, out)
	}
	if _, err := os.Stat(filepath.Join(f.repo, "configs", "starship")); !os.IsNotExist(err) {
		t.Errorf("add must never copy a config into the repository")
	}
}

func readAllString(f *os.File) (string, error) {
	data, err := io.ReadAll(f)
	return string(data), err
}
