package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

func TestBackupSingleFile(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "source.conf")
	if err := os.WriteFile(srcFile, []byte("theme = dark\n"), 0644); err != nil {
		t.Fatal(err)
	}

	destDir := filepath.Join(tmpDir, "dotfiles")
	app := manifest.AppConfig{
		Paths: []manifest.PathMapping{
			{Src: srcFile, Dest: "configs/app/source.conf"},
		},
	}

	res, err := BackupApp("testapp", app, destDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Copied) != 1 {
		t.Fatalf("expected 1 copied file, got %d", len(res.Copied))
	}

	destFile := filepath.Join(destDir, "configs/app/source.conf")
	data, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("destination file not found: %v", err)
	}
	if string(data) != "theme = dark\n" {
		t.Errorf("expected 'theme = dark\\n', got %s", string(data))
	}
}

func TestBackupDirectoryWithExclusions(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "nvim")
	os.MkdirAll(filepath.Join(srcDir, "lua"), 0755)
	os.MkdirAll(filepath.Join(srcDir, ".git"), 0755)

	os.WriteFile(filepath.Join(srcDir, "init.lua"), []byte("print('hello')\n"), 0644)
	os.WriteFile(filepath.Join(srcDir, "lua", "plugin.lua"), []byte("return {}\n"), 0644)
	os.WriteFile(filepath.Join(srcDir, ".git", "config"), []byte("[core]\n"), 0644)
	os.WriteFile(filepath.Join(srcDir, ".DS_Store"), []byte("junk"), 0644)

	destDir := filepath.Join(tmpDir, "dotfiles")
	app := manifest.AppConfig{
		Paths: []manifest.PathMapping{
			{Src: srcDir, Dest: "configs/nvim"},
		},
		Exclude: []string{
			".git/**",
			".git",
			".DS_Store",
		},
	}

	res, err := BackupApp("nvim", app, destDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should copy init.lua and plugin.lua
	if len(res.Copied) < 2 {
		t.Errorf("expected at least 2 copied files, got %d", len(res.Copied))
	}

	// Should NOT copy .git/config or .DS_Store
	gitConfig := filepath.Join(destDir, "configs/nvim/.git/config")
	if _, err := os.Stat(gitConfig); !os.IsNotExist(err) {
		t.Error("expected .git/config to be excluded, but it was copied")
	}

	dsStore := filepath.Join(destDir, "configs/nvim/.DS_Store")
	if _, err := os.Stat(dsStore); !os.IsNotExist(err) {
		t.Error("expected .DS_Store to be excluded, but it was copied")
	}
}

func TestSanitizeSecretFile(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	content := "OPENAI_API_KEY=sk-1234567890abcdef\nPORT=3000\nDATABASE_URL=postgres://user:secret@localhost:5432/db\n"
	os.WriteFile(envFile, []byte(content), 0644)

	destDir := filepath.Join(tmpDir, "dotfiles")
	app := manifest.AppConfig{
		Paths: []manifest.PathMapping{
			{Src: envFile, Dest: "configs/app/.env"},
		},
		Secrets: []string{".env"},
	}

	res, err := BackupApp("app", app, destDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.SecretsMasked) != 1 {
		t.Errorf("expected 1 secret masked, got %d", len(res.SecretsMasked))
	}

	// The destination file should be .env.template or sanitized
	templatePath := filepath.Join(destDir, "configs/app/.env.template")
	data, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("expected template file at %s: %v", templatePath, err)
	}

	templateStr := string(data)
	if strings.Contains(templateStr, "sk-1234567890abcdef") || strings.Contains(templateStr, "secret") {
		t.Errorf("template still contains raw secret! Output:\n%s", templateStr)
	}
	if !strings.Contains(templateStr, "OPENAI_API_KEY=") {
		t.Errorf("template should keep the key name. Output:\n%s", templateStr)
	}
}

func TestSanitizeJSONSecretFile(t *testing.T) {
	tmpDir := t.TempDir()
	jsonFile := filepath.Join(tmpDir, "auth.json")
	content := `{
		"anthropic": {
			"apiKey": "sk-ant-api03-secret123456"
		},
		"openai": {
			"apiKey": "sk-proj-secret789"
		}
	}`
	os.WriteFile(jsonFile, []byte(content), 0644)

	destDir := filepath.Join(tmpDir, "dotfiles")
	app := manifest.AppConfig{
		Paths: []manifest.PathMapping{
			{Src: jsonFile, Dest: "configs/pi/auth.json"},
		},
		Secrets: []string{"auth.json"},
	}

	res, err := BackupApp("pi", app, destDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.SecretsMasked) != 1 {
		t.Fatalf("expected 1 secret masked, got %d", len(res.SecretsMasked))
	}

	templatePath := filepath.Join(destDir, "configs/pi/auth.json.template")
	data, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("expected template at %s: %v", templatePath, err)
	}

	output := string(data)
	if strings.Contains(output, "secret123456") || strings.Contains(output, "secret789") {
		t.Errorf("template still contains raw secret! Output:\n%s", output)
	}
	if !strings.Contains(output, "YOUR_APIKEY_HERE") && !strings.Contains(output, "YOUR_API_KEY_HERE") {
		t.Errorf("expected placeholder in JSON template. Output:\n%s", output)
	}
}
