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
