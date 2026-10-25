package test

import (
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
	output, err = addCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed 'dots add': %s: %v", string(output), err)
	}
	if !strings.Contains(string(output), "Added path") {
		t.Errorf("expected add confirmation, got: %s", string(output))
	}

	// Verify that dummy.conf was copied to configs/mytool/dummy.conf
	backedUpFile := filepath.Join(dotfilesDir, "configs", "mytool", "dummy.conf")
	if _, err := os.Stat(backedUpFile); os.IsNotExist(err) {
		t.Fatalf("expected backed up file at %s", backedUpFile)
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
