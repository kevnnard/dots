package setup

import "testing"

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
