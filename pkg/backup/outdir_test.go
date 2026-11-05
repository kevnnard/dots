package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckOutsideRepoRejectsInsideAndSame(t *testing.T) {
	repo := t.TempDir()

	for _, out := range []string{repo, filepath.Join(repo, "configs"), filepath.Join(repo, "a", "b")} {
		if err := CheckOutsideRepo(repo, out); err == nil {
			t.Errorf("CheckOutsideRepo(%q, %q) = nil, want error", repo, out)
		}
	}
}

func TestCheckOutsideRepoAcceptsSiblingAndPrefixLookalike(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "dots")
	for _, out := range []string{filepath.Join(base, "backup"), filepath.Join(base, "dots-backup")} {
		if err := CheckOutsideRepo(repo, out); err != nil {
			t.Errorf("CheckOutsideRepo(%q, %q) = %v, want nil", repo, out, err)
		}
	}
}

func TestStageManifestCopiesRepoManifest(t *testing.T) {
	repo := t.TempDir()
	out := filepath.Join(t.TempDir(), "backup")
	want := []byte("apps: {}\n")
	if err := os.WriteFile(filepath.Join(repo, "manifest.yaml"), want, 0644); err != nil {
		t.Fatal(err)
	}

	if err := StageManifest(repo, out); err != nil {
		t.Fatalf("StageManifest: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(out, "manifest.yaml"))
	if err != nil {
		t.Fatalf("manifest not staged: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("staged manifest = %q, want %q", got, want)
	}
}
