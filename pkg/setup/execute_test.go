package setup

import (
	"errors"
	"strings"
	"testing"
)

// fakeEnv records interactive runs and answers reads from maps. Nothing real runs.
type fakeEnv struct {
	user    string
	home    string
	files   map[string]string
	outputs map[string]string
	ran     []string
	failRun map[string]bool
}

func (f *fakeEnv) Run(argv []string) error {
	f.ran = append(f.ran, strings.Join(argv, " "))
	if f.failRun[strings.Join(argv, " ")] {
		return errors.New("exit 1")
	}
	return nil
}

func (f *fakeEnv) Output(argv []string) (string, error) {
	out, ok := f.outputs[strings.Join(argv, " ")]
	if !ok {
		return "", errors.New("not found")
	}
	return out, nil
}

func (f *fakeEnv) ReadFile(path string) ([]byte, error) {
	v, ok := f.files[path]
	if !ok {
		return nil, errors.New("missing")
	}
	return []byte(v), nil
}

func (f *fakeEnv) Exists(path string) bool {
	_, ok := f.files[path]
	return ok
}

func (f *fakeEnv) User() string { return f.user }
func (f *fakeEnv) Home() string { return f.home }

// The shell step runs chsh with the chosen shell and only passes when the
// account database shows that shell.
func TestShellStepPassesOnlyWhenPasswdShowsChosenShell(t *testing.T) {
	env := &fakeEnv{
		user:    "kevnnard",
		files:   map[string]string{"/etc/shells": "/bin/bash\n/bin/zsh\n"},
		outputs: map[string]string{"getent passwd kevnnard": "kevnnard:x:1000:1000::/home/kevnnard:/bin/zsh"},
	}
	res := Execute(env, Plan()[0], "zsh")
	if res.Status != StatusDone {
		t.Fatalf("status = %q, detail %q", res.Status, res.Detail)
	}
	if got := env.ran; len(got) != 1 || got[0] != "chsh -s /bin/zsh" {
		t.Errorf("ran %v, want chsh -s /bin/zsh", got)
	}
}

func TestShellStepRejectsShellNotListedInEtcShells(t *testing.T) {
	env := &fakeEnv{user: "kevnnard", files: map[string]string{"/etc/shells": "/bin/bash\n"}}
	res := Execute(env, Plan()[0], "zsh")
	if res.Status != StatusFailed || len(env.ran) != 0 {
		t.Errorf("want failed without chsh, got %+v ran %v", res, env.ran)
	}
}

// gh has a status command, so dots can verify the login itself.
func TestGHLoginIsVerifiedWithGHAuthStatus(t *testing.T) {
	env := &fakeEnv{outputs: map[string]string{"gh auth status": "Logged in to github.com"}}
	res := Execute(env, Step{ID: "gh", Title: "GitHub CLI login", Login: true, Command: []string{"gh", "auth", "login"}, Verify: []string{"gh", "auth", "status"}}, "")
	if res.Status != StatusDone {
		t.Errorf("status = %q, detail %q", res.Status, res.Detail)
	}
	if len(env.ran) != 1 || env.ran[0] != "gh auth login" {
		t.Errorf("ran %v", env.ran)
	}
}

// A login without a status command is reported as manual, never as done.
func TestLoginWithoutStatusCommandIsManual(t *testing.T) {
	env := &fakeEnv{}
	res := Execute(env, Step{ID: "claude", Title: "Claude Code login", Login: true, Command: []string{"claude"}}, "")
	if res.Status != StatusManual {
		t.Errorf("status = %q, want manual", res.Status)
	}
}

// The zsh step passes only when the files the restore deploys are present.
func TestZshStepNeedsDeployedFiles(t *testing.T) {
	env := &fakeEnv{home: "/h", files: map[string]string{"/h/.zshrc": "x"}}
	if res := Execute(env, Plan()[1], ""); res.Status != StatusFailed {
		t.Errorf("missing .p10k.zsh must fail, got %+v", res)
	}
	env.files["/h/.p10k.zsh"] = "y"
	if res := Execute(env, Plan()[1], ""); res.Status != StatusDone {
		t.Errorf("both files present must pass, got %+v", res)
	}
}

// A failed step does not stop later steps.
func TestRunContinuesAfterFailedStep(t *testing.T) {
	env := &fakeEnv{files: map[string]string{"/etc/shells": "/bin/zsh\n"}, user: "u", home: "/h"}
	results := RunAll(env, []Step{
		{ID: "a", Title: "a", Login: false, Command: []string{"false-cmd"}, Verify: []string{"nope"}},
		{ID: "b", Title: "b", Login: true, Command: []string{"claude"}},
	}, nil)
	if len(results) != 2 || results[0].Status != StatusFailed || results[1].Status != StatusManual {
		t.Errorf("results = %+v", results)
	}
}
