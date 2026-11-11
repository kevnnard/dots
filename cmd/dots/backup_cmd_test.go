package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/transfer"
	"github.com/spf13/pflag"
)

// inboxRunner plays `tailscale file`: cp records the archive, get drops it
// into the requested inbox.
type inboxRunner struct{ archive []byte }

func (r *inboxRunner) Run(name string, args ...string) ([]byte, error) {
	switch args[1] {
	case "cp":
		data, err := os.ReadFile(args[2])
		r.archive = data
		return nil, err
	case "get":
		return nil, os.WriteFile(filepath.Join(args[len(args)-1], transfer.ArchivePrefix+"test.tar.gz"), r.archive, 0o644)
	}
	return nil, nil
}

// runBackup executes `dots backup` with fresh flag values and captures stdout.
func runBackup(t *testing.T, args ...string) (string, error) {
	t.Helper()
	sendFlag, receiveFlag, sha256Flag, intoFlag, outFlag, dotfilesDirFlag = "", false, "", "", "", ""
	// cobra keeps Changed between runs, and --send is dispatched on it.
	backupCmd.Flags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
	old := os.Stdout
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = wr
	rootCmd.SetArgs(append([]string{"backup"}, args...))
	runErr := rootCmd.Execute()
	wr.Close()
	os.Stdout = old
	var out bytes.Buffer
	io.Copy(&out, rd)
	return out.String(), runErr
}

// The sender prints a short code and the two commands to run on the receiver;
// the receiver types only that code and restore finds the backup by itself.
func TestSendPrintsShortCodeAndReceiveAcceptsIt(t *testing.T) {
	f := newCLIFixture(t)
	t.Setenv("HOME", t.TempDir())
	r := &inboxRunner{}
	oldRunner := transferRunner
	t.Cleanup(func() { transferRunner = oldRunner })
	transferRunner = r

	stage := filepath.Join(t.TempDir(), "stage")
	for _, rel := range []string{"manifest.yaml", "configs/zsh/.zshrc"} {
		data, err := os.ReadFile(filepath.Join(f.repo, rel))
		if err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(stage, rel), data)
	}
	out, err := runBackup(t, "--send", "station", "--out", stage, "--dir", f.repo)
	if err != nil {
		t.Fatalf("send: %v\n%s", err, out)
	}
	code := transfer.ShortCode(sha256Hex(t, r.archive))
	for _, want := range []string{
		"Code: " + code,
		"dots backup --receive " + code,
		"dots restore --received --all --dry-run",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("send output lacks %q:\n%s", want, out)
		}
	}

	out, err = runBackup(t, "--receive", code)
	if err != nil {
		t.Fatalf("receive: %v\n%s", err, out)
	}
	incoming, _ := transfer.IncomingDir()
	want := filepath.Join(incoming, code)
	if _, err := os.Stat(filepath.Join(want, "configs", "zsh", ".zshrc")); err != nil {
		t.Fatalf("backup not extracted into %s: %v", want, err)
	}
	if got, err := transfer.LatestReceived(incoming); err != nil || got != want {
		t.Errorf("latest received = %q, %v; want %q", got, err, want)
	}
	if !strings.Contains(out, "dots restore --received --all --dry-run") {
		t.Errorf("receive output must give the restore command:\n%s", out)
	}
}

// The full sha256 through --sha256 keeps working.
func TestReceiveStillAcceptsFullSha256Flag(t *testing.T) {
	f := newCLIFixture(t)
	t.Setenv("HOME", t.TempDir())
	archive := filepath.Join(t.TempDir(), "a.tar.gz")
	sum, err := transfer.Archive(f.repo, archive)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(archive)
	oldRunner := transferRunner
	t.Cleanup(func() { transferRunner = oldRunner })
	transferRunner = &inboxRunner{archive: data}

	out, err := runBackup(t, "--receive", "--sha256", sum)
	if err != nil {
		t.Fatalf("receive: %v\n%s", err, out)
	}
	incoming, _ := transfer.IncomingDir()
	if _, err := os.Stat(filepath.Join(incoming, sum[:12], "manifest.yaml")); err != nil {
		t.Fatalf("full sha256 must extract into the 12-character folder: %v", err)
	}
}

func TestReceiveWithoutCodeFailsBeforeTouchingInbox(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	r := &inboxRunner{}
	oldRunner := transferRunner
	t.Cleanup(func() { transferRunner = oldRunner })
	transferRunner = r
	_, err := runBackup(t, "--receive")
	if err == nil || !strings.Contains(err.Error(), "dots backup --receive <code>") {
		t.Fatalf("want missing code error, got %v", err)
	}
	incoming, _ := transfer.IncomingDir()
	if _, err := os.Stat(incoming); !os.IsNotExist(err) {
		t.Error("nothing may be written without a code")
	}
}

// stageApp runs `dots backup <app>` against the repository manifest with a
// synthetic home, and returns the staging directory.
func stageApp(t *testing.T, repo, app string) string {
	t.Helper()
	stage := filepath.Join(t.TempDir(), "stage")
	out, err := runBackup(t, app, "--dir", repo, "--out", stage)
	if err != nil {
		t.Fatalf("backup %s: %v\n%s", app, err, out)
	}
	return stage
}

// repoWithShippedManifest is a dotfiles directory holding the repository
// manifest.yaml, the inventory a real backup reads.
func repoWithShippedManifest(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "manifest.yaml"), data)
	return repo
}

// Personal configs that the loaders read from documented locations travel;
// installed plugins, scratch, telemetry and leftovers never do, on any OS.
func TestBackupStagesPortablePersonalConfigs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := repoWithShippedManifest(t)
	for _, rel := range []string{
		".zshrc",
		".oh-my-zsh/custom/aliases.zsh",
		".oh-my-zsh/custom/lib/history.zsh",
		".oh-my-zsh/custom/example.zsh",
		".oh-my-zsh/custom/plugins/zsh-autosuggestions/zsh-autosuggestions.zsh",
		".oh-my-zsh/custom/themes/powerlevel10k/powerlevel10k.zsh-theme",
		".gentle-shell/agent/skills/review/SKILL.md",
		".gentle-shell/agent/extensions/rtk.ts",
		".gentle-shell/agent/extensions/rtk.ts.bak-linux-guard",
		".gentle-shell/agent/prompts/review.md",
		".gentle-shell/gentle-ai/vim.json",
		".gentle-shell/gentle-ai/persona.json",
		".gentle-shell/gentle-ai/telemetry.json",
		".gentle-shell/gentle-ai/scratch/note.md",
		".gentle-shell/agent/settings.json",
		".gentle-shell/agent/APPEND_SYSTEM.md",
		".gentle-shell/config/gentle-ai/star-prompt.json",
		".gentle-shell/gentle-ai/background-subagents.json",
		".gentle-shell/gentle-ai/state.json",
		".config/gga/config",
		".claude/hooks/herdr-agent-state.sh",
		".claude/output-styles/neutral.md",
		".codex/engram-instructions.md",
		".codex/herdr-agent-state.sh",
	} {
		writeTestFile(t, filepath.Join(home, rel), []byte("{}\n"))
	}

	staged := map[string]bool{}
	for _, app := range []string{"zsh", "gentle-shell", "gga", "claude", "codex"} {
		stage := stageApp(t, repo, app)
		filepath.Walk(stage, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				rel, _ := filepath.Rel(stage, p)
				staged[filepath.ToSlash(rel)] = true
			}
			return nil
		})
	}

	for _, want := range []string{
		"configs/zsh/.zshrc",
		"configs/zsh/oh-my-zsh-custom/aliases.zsh",
		"configs/zsh/oh-my-zsh-custom/lib/history.zsh",
		"configs/gentle-shell/skills/review/SKILL.md",
		"configs/gentle-shell/extensions/rtk.ts",
		"configs/gentle-shell/prompts/review.md",
		"configs/gentle-shell/gentle-ai/vim.json",
		"configs/gentle-shell/gentle-ai/persona.json",
		"configs/gentle-shell/settings.json",
		"configs/gentle-shell/APPEND_SYSTEM.md",
		"configs/gentle-shell/config/gentle-ai/star-prompt.json",
		"configs/gentle-shell/gentle-ai/background-subagents.json",
		"configs/gga/config",
		"configs/claude/hooks/herdr-agent-state.sh",
		"configs/claude/output-styles/neutral.md",
		"configs/codex/engram-instructions.md",
		"configs/codex/herdr-agent-state.sh",
	} {
		if !staged[want] {
			t.Errorf("%s was not staged", want)
		}
	}
	for p := range staged {
		for _, never := range []string{"plugins/", "themes/", "example.zsh", ".bak", "telemetry.json", "scratch/", "state.json"} {
			if strings.Contains(p, never) {
				t.Errorf("%s must not be staged", p)
			}
		}
	}
}

// Codex config names the files it loads by absolute path. The staged copies
// point at the home placeholder, so restore expands them for the new home.
func TestBackupTokenizesCodexReferencesToMappedFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := repoWithShippedManifest(t)
	writeTestFile(t, filepath.Join(home, ".codex/config.toml"),
		[]byte(`model_instructions_file = "`+home+`/.codex/engram-instructions.md"`+"\n"))
	writeTestFile(t, filepath.Join(home, ".codex/hooks.json"),
		[]byte(`{"command":"bash '`+home+`/.codex/herdr-agent-state.sh' session"}`))
	writeTestFile(t, filepath.Join(home, ".codex/engram-instructions.md"), []byte("# engram\n"))
	writeTestFile(t, filepath.Join(home, ".codex/herdr-agent-state.sh"), []byte("#!/bin/sh\n"))

	stage := stageApp(t, repo, "codex")
	for rel, want := range map[string]string{
		"configs/codex/config.toml": "__HOME__/.codex/engram-instructions.md",
		"configs/codex/hooks.json":  "__HOME__/.codex/herdr-agent-state.sh",
	} {
		data, err := os.ReadFile(filepath.Join(stage, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) || strings.Contains(string(data), home) {
			t.Errorf("%s must reference %s, not the source home:\n%s", rel, want, data)
		}
		if _, err := os.Stat(filepath.Join(stage, "configs/codex", filepath.Base(want))); err != nil {
			t.Errorf("the referenced file %s was not staged: %v", filepath.Base(want), err)
		}
	}
}

// web-search.json is the web search credential store: it travels only as a
// masked template, so a key never reaches the backup.
func TestBackupMasksWebSearchCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := repoWithShippedManifest(t)
	config := []byte(`{"provider":"brave","braveApiKey":"fixture-placeholder"}`)
	writeTestFile(t, filepath.Join(home, ".gentle-shell/agent/web-search.json"), config)

	for app, dest := range map[string]string{"gentle-shell": "configs/gentle-shell/web-search.json"} {
		stage := stageApp(t, repo, app)
		if _, err := os.Stat(filepath.Join(stage, dest)); !os.IsNotExist(err) {
			t.Errorf("%s: %s must not be copied as is", app, dest)
		}
		data, err := os.ReadFile(filepath.Join(stage, dest+".template"))
		if err != nil {
			t.Fatalf("%s: template missing: %v", app, err)
		}
		if strings.Contains(string(data), "fixture-placeholder") || !strings.Contains(string(data), `"brave"`) {
			t.Errorf("%s: template must mask the key and keep the provider:\n%s", app, data)
		}
		// The usual case: the next backup reuses the folder holding the template.
		if out, err := runBackup(t, app, "--dir", repo, "--out", stage); err != nil {
			t.Errorf("%s: a staged template must not block the next backup: %v\n%s", app, err, out)
		}
	}
}

// A staging folder from a dots that copied web-search.json as is still holds
// that raw copy, and restore prefers it to the template. Backup refuses the
// folder instead of sending both, and leaves the old entry exactly as it was.
func TestBackupRefusesStageHoldingRawSecretCopy(t *testing.T) {
	for _, kind := range []string{"file", "dangling symlink"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			repo := repoWithShippedManifest(t)
			writeTestFile(t, filepath.Join(home, ".gentle-shell/agent/web-search.json"),
				[]byte(`{"provider":"brave","braveApiKey":"fixture-placeholder"}`))

			stage := filepath.Join(t.TempDir(), "stage")
			raw := filepath.Join(stage, "configs/gentle-shell/web-search.json")
			stale := []byte(`{"provider":"brave","braveApiKey":"stale-fixture-value"}`)
			if kind == "file" {
				writeTestFile(t, raw, stale)
			} else {
				if err := os.MkdirAll(filepath.Dir(raw), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("missing-target", raw); err != nil {
					t.Fatal(err)
				}
			}

			out, err := runBackup(t, "gentle-shell", "--dir", repo, "--out", stage)
			want := raw + " already holds an unmasked copy of a file that is now backed up as a template; nothing was removed or sent. Stage into a new empty folder: dots backup --out <new-folder>"
			if err == nil || err.Error() != want {
				t.Fatalf("error = %v\nwant %s", err, want)
			}
			for _, leak := range []string{"fixture-placeholder", "stale-fixture-value"} {
				if strings.Contains(out+err.Error(), leak) {
					t.Errorf("output exposes a credential value:\n%s", out)
				}
			}
			if strings.Contains(out, "Secret masked") {
				t.Errorf("output must not claim the secret was masked:\n%s", out)
			}
			if _, err := os.Lstat(raw + ".template"); !os.IsNotExist(err) {
				t.Error("no template may be written next to the raw copy")
			}
			if kind == "file" {
				if data, _ := os.ReadFile(raw); string(data) != string(stale) {
					t.Errorf("raw copy changed: %q", data)
				}
			} else if target, err := os.Readlink(raw); err != nil || target != "missing-target" {
				t.Errorf("symlink changed: %q, %v", target, err)
			}
		})
	}
}

// A manifest written by an older dots is never rewritten, but backup names
// the built-in config paths it lacks, so nothing is silently left behind.
func TestBackupReportsBuiltInPathsMissingFromManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := t.TempDir()
	local := []byte("version: 1.0.0\napps:\n    zsh:\n        paths:\n            - src: ~/.zshrc\n              dest: configs/zsh/.zshrc\n")
	writeTestFile(t, filepath.Join(repo, "manifest.yaml"), local)
	writeTestFile(t, filepath.Join(home, ".zshrc"), []byte("# zsh\n"))

	out, err := runBackup(t, "zsh", "--dir", repo, "--out", filepath.Join(t.TempDir(), "stage"))
	if err != nil {
		t.Fatalf("backup: %v\n%s", err, out)
	}
	if !strings.Contains(out, "zsh: ~/.oh-my-zsh/custom") {
		t.Errorf("output must name the missing built-in path:\n%s", out)
	}
	if strings.Contains(out, "gentle-shell:") {
		t.Errorf("apps the manifest does not declare stay undeclared:\n%s", out)
	}
	if data, _ := os.ReadFile(filepath.Join(repo, "manifest.yaml")); string(data) != string(local) {
		t.Error("the local manifest must not be rewritten")
	}
}

func writeTestFile(t *testing.T, p string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func sha256Hex(t *testing.T, data []byte) string {
	t.Helper()
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
