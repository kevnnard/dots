package setup

import (
	"strings"
	"testing"
)

// The shell choice comes first, then steps that need no token, then steps
// that need a login the user completes in the terminal.
func TestPlanStartsWithShellThenNoTokenThenLogins(t *testing.T) {
	steps := Plan()
	if len(steps) == 0 || steps[0].ID != "shell" {
		t.Fatalf("first step must be the default shell, got %+v", steps)
	}
	seenLogin := false
	for _, s := range steps {
		if s.Login {
			seenLogin = true
			continue
		}
		if seenLogin {
			t.Errorf("no-token step %q comes after a login step", s.ID)
		}
	}
	if !seenLogin {
		t.Errorf("plan has no login step")
	}
}

// Services that take a credential the user enters themselves are guided: dots
// shows where to do it and never runs it.
func TestCredentialServicesAreGuidedNotRun(t *testing.T) {
	want := map[string]bool{"postgres": false, "linear": false, "codex": false, "antigravity": false}
	for _, s := range Plan() {
		if _, ok := want[s.ID]; !ok {
			continue
		}
		want[s.ID] = true
		if s.Guide == "" || len(s.Command) != 0 {
			t.Errorf("%s must have a guide and no command, got guide=%q command=%v", s.ID, s.Guide, s.Command)
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("plan is missing guided step %q", id)
		}
	}
}

// Gentle Shell is installed with its own installer, which dots never runs:
// the step shows where to get it and checks `gentle-shell --version`. It
// needs no token, so it comes before the login steps; its login follows them.
func TestPlanGuidesGentleShellInstallAndLogin(t *testing.T) {
	var install, login *Step
	for i, s := range Plan() {
		switch s.ID {
		case "gentle-shell":
			install = &Plan()[i]
		case "gentle-shell-login":
			login = &Plan()[i]
		}
	}
	if install == nil || login == nil {
		t.Fatalf("plan must have gentle-shell and gentle-shell-login steps")
	}
	if install.Login || len(install.Command) != 0 || !strings.Contains(install.Guide, "install-gentle-shell.sh") || strings.Join(install.Verify, " ") != "gentle-shell --version" {
		t.Errorf("install step = %+v", *install)
	}
	if !login.Login || len(login.Command) != 0 || !strings.Contains(login.Guide, "/login") {
		t.Errorf("login step = %+v", *login)
	}
}
