package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteDefaultBrewfiles(t *testing.T) {
	tmpDir := t.TempDir()

	if err := WriteDefaultBrewfiles(tmpDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedFiles := []struct {
		name     string
		contains string
	}{
		{"Brewfile", "font-caskaydia-cove-nerd-font"},
		{"Brewfile", "dotnet-sdk"},
		{"Brewfile.mobile", "openjdk@17"},
		{"Brewfile.mobile", "cocoapods"},
		{"Brewfile.mobile", "android-studio"},
		{"Brewfile.infra", "caddy"},
		{"Brewfile.infra", "mkcert"},
		{"Brewfile.ai", "ollama"},
	}

	for _, ef := range expectedFiles {
		path := filepath.Join(tmpDir, ef.name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("file %s not found: %v", ef.name, err)
		}
		if !strings.Contains(string(data), ef.contains) {
			t.Errorf("expected %s to contain %s", ef.name, ef.contains)
		}
	}
}
