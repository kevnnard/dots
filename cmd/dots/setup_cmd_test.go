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
