package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/setup"
)

func TestAskShellDefaultsToZshAndAcceptsBash(t *testing.T) {
	var out bytes.Buffer
	if got, err := askShell(strings.NewReader("\n"), &out); err != nil || got != "zsh" {
		t.Errorf("Enter = %q, %v; want zsh", got, err)
	}
	if got, err := askShell(strings.NewReader("2\n"), &out); err != nil || got != "bash" {
		t.Errorf("2 = %q, %v; want bash", got, err)
	}
	if _, err := askShell(strings.NewReader("9\n"), &out); err == nil {
		t.Errorf("9 must be rejected")
	}
}

// The final line counts each outcome, and any failed step makes the command fail.
func TestPrintSetupResultsCountsAndFailsOnFailedStep(t *testing.T) {
	var out bytes.Buffer
	err := printSetupResults(&out, []setup.Result{
		{ID: "shell", Title: "shell", Status: setup.StatusDone, Detail: "ok"},
		{ID: "claude", Title: "claude", Status: setup.StatusManual, Detail: "check yourself"},
		{ID: "gh", Title: "gh", Status: setup.StatusFailed, Detail: "status failed"},
	})
	if err == nil {
		t.Errorf("a failed step must make setup fail")
	}
	if !strings.Contains(out.String(), "📊 Done: 1 · Manual: 1 · Failed: 1") {
		t.Errorf("counts line missing:\n%s", out.String())
	}
}

// The checklist shows a titled header with the progress, one glyph per step,
// the selected shell, and the counts line that the summary already used.
func TestRenderChecklistShowsHeaderGlyphsAndCounts(t *testing.T) {
	out := renderChecklist([]setup.Result{
		{ID: "shell", Title: "Default shell", Status: setup.StatusDone, Detail: "zsh"},
		{ID: "zsh-p10k", Title: "zsh with powerlevel10k", Status: setup.StatusDone, Detail: "in place"},
		{ID: "claude", Title: "Claude Code login", Status: setup.StatusManual, Detail: "check yourself"},
		{ID: "gh", Title: "GitHub CLI login", Status: setup.StatusFailed, Detail: "status failed"},
	}, 4)
	for _, want := range []string{"dots setup · 2/4", "✓ Default shell", "◇ Claude Code login", "✗ GitHub CLI login", "📊 Done: 2 · Manual: 1 · Failed: 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("checklist missing %q:\n%s", want, out)
		}
	}
}

func TestShellChoiceMarksTheSelectedShell(t *testing.T) {
	if got := renderShellChoice("zsh"); !strings.Contains(got, "● zsh") || !strings.Contains(got, "○ bash") {
		t.Errorf("zsh choice = %q", got)
	}
	if got := renderShellChoice("bash"); !strings.Contains(got, "○ zsh") || !strings.Contains(got, "● bash") {
		t.Errorf("bash choice = %q", got)
	}
}
