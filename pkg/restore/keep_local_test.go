package restore

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

func keepLocalApp() manifest.AppConfig {
	return manifest.AppConfig{Paths: []manifest.PathMapping{
		{Src: "~/.tool/signing", Dest: "configs/tool/signing", KeepLocal: true},
		{Src: "~/.tool/aliases", Dest: "configs/tool/aliases"},
	}}
}

func TestApplyFilesKeepsMachineLocalFileEvenWhenReplacing(t *testing.T) {
	root, home := isolatedHome(t)
	repo := filepath.Join(root, "repo")
	mustWrite(t, filepath.Join(repo, "configs", "tool", "signing"), "key = MAC\n")
	mustWrite(t, filepath.Join(repo, "configs", "tool", "aliases"), "co = checkout\n")
	signing := filepath.Join(home, ".tool", "signing")
	aliases := filepath.Join(home, ".tool", "aliases")
	mustWrite(t, signing, "key = LINUX\n")
	mustWrite(t, aliases, "old\n")

	for _, dry := range []bool{true, false} {
		res, err := ApplyAppFilesWith("tool", keepLocalApp(), repo, ApplyOptions{DryRun: dry, Replace: true})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(res.Kept, ",") != signing || strings.Join(res.MachineLocal, ",") != signing {
			t.Errorf("dry=%v kept=%v machineLocal=%v", dry, res.Kept, res.MachineLocal)
		}
		if strings.Join(res.Restored, ",") != aliases || len(res.Warnings) != 0 {
			t.Errorf("dry=%v other mappings must still be replaced: %+v", dry, res)
		}
		for _, b := range res.LocalBackups {
			if strings.HasPrefix(b, signing) {
				t.Errorf("a machine-local file needs no safety backup: %v", res.LocalBackups)
			}
		}
	}
	if got := readOrEmpty(signing); got != "key = LINUX\n" {
		t.Errorf("machine-local file was replaced: %q", got)
	}
	if got := readOrEmpty(aliases); got != "co = checkout\n" {
		t.Errorf("shared file was not replaced: %q", got)
	}
}

func TestApplyFilesDeploysMissingMachineLocalFile(t *testing.T) {
	root, home := isolatedHome(t)
	repo := filepath.Join(root, "repo")
	mustWrite(t, filepath.Join(repo, "configs", "tool", "signing"), "key = MAC\n")

	res, err := ApplyAppFilesWith("tool", keepLocalApp(), repo, ApplyOptions{Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	signing := filepath.Join(home, ".tool", "signing")
	if got := readOrEmpty(signing); got != "key = MAC\n" || len(res.Kept) != 0 || len(res.MachineLocal) != 0 {
		t.Errorf("a missing machine-local file is deployed on first setup: %q %+v", got, res)
	}
}

func TestConfigLinesNameMachineLocalFiles(t *testing.T) {
	s := StepResult{AppName: "git", Config: ConfigKeptExisting, Files: &ApplyResult{
		Kept:         []string{"/h/.gitconfig", "/h/.config/git/x"},
		MachineLocal: []string{"/h/.gitconfig"},
	}}
	got := strings.Join(configLines(s), "\n")
	for _, want := range []string{
		"      ▲ kept machine-specific file: /h/.gitconfig",
		"      ▲ kept local file: /h/.config/git/x",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "kept local file: /h/.gitconfig") {
		t.Errorf("machine-specific file listed twice:\n%s", got)
	}
}
