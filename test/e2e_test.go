package test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEndToEndCLI(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "dots")
	dotfilesDir := filepath.Join(tmpDir, "dotfiles_repo")

	// 1. Build the binary
	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/dots")
	buildCmd.Dir = ".."
	output, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed building binary: %s: %v", string(output), err)
	}

	// 2. Test 'dots init --dir <dotfilesDir>'
	initCmd := exec.Command(binPath, "init", "--dir", dotfilesDir)
	output, err = initCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed 'dots init': %s: %v", string(output), err)
	}
	if !strings.Contains(string(output), "Initialized dotfiles repository") {
		t.Errorf("expected initialization message, got: %s", string(output))
	}

	// Verify manifest.yaml exists
	manifestPath := filepath.Join(dotfilesDir, "manifest.yaml")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Fatalf("expected manifest.yaml at %s", manifestPath)
	}

	// 3. Test 'dots status --dir <dotfilesDir>'
	statusCmd := exec.Command(binPath, "status", "--dir", dotfilesDir)
	output, err = statusCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed 'dots status': %s: %v", string(output), err)
	}
	if !strings.Contains(string(output), "System:") {
		t.Errorf("expected status header, got: %s", string(output))
	}

	// 4. Test 'dots add'
	dummyConfig := filepath.Join(tmpDir, "dummy.conf")
	os.WriteFile(dummyConfig, []byte("setting=on\n"), 0644)

	addCmd := exec.Command(binPath, "add", "mytool", dummyConfig, "--dir", dotfilesDir)
	addCmd.Env = append(os.Environ(), "HOME="+tmpDir)
	output, err = addCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed 'dots add': %s: %v", string(output), err)
	}
	if !strings.Contains(string(output), "Added path") {
		t.Errorf("expected add confirmation, got: %s", string(output))
	}

	// dummy.conf is staged in the private backup folder, never in the repository
	stagedFile := filepath.Join(tmpDir, ".cache", "dots", "backup", "configs", "mytool", "dummy.conf")
	if _, err := os.Stat(stagedFile); os.IsNotExist(err) {
		t.Fatalf("expected staged file at %s", stagedFile)
	}
	if _, err := os.Stat(filepath.Join(dotfilesDir, "configs", "mytool")); !os.IsNotExist(err) {
		t.Fatalf("dots add must never copy a config into the repository")
	}

	// 5. Test 'dots restore --dry-run --dir <dotfilesDir>'
	restoreCmd := exec.Command(binPath, "restore", "mytool", "--dry-run", "--dir", dotfilesDir)
	output, err = restoreCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed 'dots restore --dry-run': %s: %v", string(output), err)
	}
	if !strings.Contains(string(output), "DRY RUN MODE") {
		t.Errorf("expected dry run notice, got: %s", string(output))
	}
}

// TestEndToEndRestoreConfigOptIn drives the built binary with a temporary HOME.
// No --install-packages is passed, so no install command can run.
func TestEndToEndRestoreConfigOptIn(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "dots")
	repo := filepath.Join(tmpDir, "repo")
	home := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(repo, "configs", "mytool"), 0755)
	os.MkdirAll(home, 0755)
	os.WriteFile(filepath.Join(repo, "configs", "mytool", ".toolrc"), []byte("from-backup\n"), 0644)
	os.WriteFile(filepath.Join(repo, "manifest.yaml"), []byte(`version: 1.0.0
apps:
  mytool:
    paths:
      - src: ~/.toolrc
        dest: configs/mytool/.toolrc
  needy:
    config_requires: [helper]
    paths:
      - src: ~/.needyrc
        dest: configs/mytool/.toolrc
  helper:
    check: [dots-e2e-missing-helper]
    install:
      default: {manager: script, command: "false"}
`), 0644)

	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/dots")
	buildCmd.Dir = ".."
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed building binary: %s: %v", output, err)
	}
	run := func(args ...string) (string, error) {
		cmd := exec.Command(binPath, append(append([]string{"restore"}, args...), "--dir", repo)...)
		cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "DOTFILES_DIR=")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	target := filepath.Join(home, ".toolrc")

	if out, err := run("mytool"); err != nil {
		t.Fatalf("stock restore: %v\n%s", err, out)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("stock restore deployed the backed-up config")
	}

	if out, err := run("mytool", "--with-config"); err != nil {
		t.Fatalf("--with-config: %v\n%s", err, out)
	}
	if data, _ := os.ReadFile(target); string(data) != "from-backup\n" {
		t.Fatalf("--with-config did not deploy: %q", data)
	}

	os.WriteFile(target, []byte("mine\n"), 0644)
	if out, err := run("mytool", "--with-config"); err != nil || !strings.Contains(out, "kept") {
		t.Fatalf("existing file must be kept: %v\n%s", err, out)
	}
	if data, _ := os.ReadFile(target); string(data) != "mine\n" {
		t.Fatalf("existing file overwritten: %q", data)
	}
	if out, err := run("mytool", "--with-config", "--replace-existing"); err != nil {
		t.Fatalf("--replace-existing: %v\n%s", err, out)
	}
	if backups, _ := filepath.Glob(target + ".*.bk"); len(backups) != 1 {
		t.Fatalf("safety backups = %v", backups)
	}

	out, err := run("needy", "--with-config")
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
		t.Fatalf("missing config prerequisite must exit nonzero, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "incomplete") || !strings.Contains(out, "helper") {
		t.Errorf("output must explain the incomplete config:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".needyrc")); !os.IsNotExist(err) {
		t.Error("incomplete config was deployed")
	}
}
