package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// Status is the outcome of one setup step.
type Status string

const (
	StatusDone   Status = "done"
	StatusFailed Status = "failed"
	StatusManual Status = "manual"
)

// Result is what one step ended with. Detail never holds a credential.
type Result struct {
	ID     string
	Title  string
	Status Status
	Detail string
}

// Env is every external effect a step can have. Tests replace it.
type Env interface {
	// Run starts argv attached to the user's terminal and waits for it.
	Run(argv []string) error
	// Output runs argv quietly and returns its stdout.
	Output(argv []string) (string, error)
	Exists(p string) bool
	ReadFile(p string) ([]byte, error)
	User() string
	Home() string
}

// Execute runs one step. choice is only used by the shell step (zsh or bash).
func Execute(env Env, step Step, choice string) Result {
	res := Result{ID: step.ID, Title: step.Title}
	switch step.ID {
	case "shell":
		return shellStep(env, choice, res)
	case "zsh-p10k":
		if env.Exists(filepath.Join(env.Home(), ".zshrc")) && env.Exists(filepath.Join(env.Home(), ".p10k.zsh")) {
			res.Status, res.Detail = StatusDone, "zsh and powerlevel10k files are in place"
		} else {
			res.Status, res.Detail = StatusFailed, "missing .zshrc or .p10k.zsh; run dots restore zsh --with-config first"
		}
		return res
	}
	if len(step.Command) > 0 {
		if err := env.Run(step.Command); err != nil {
			res.Status, res.Detail = StatusFailed, "command ended with an error: "+strings.Join(step.Command, " ")
			return res
		}
	}
	if len(step.Verify) == 0 {
		res.Status = StatusManual
		res.Detail = "no status command: check it yourself in the tool"
		return res
	}
	if _, err := env.Output(step.Verify); err != nil {
		res.Status, res.Detail = StatusFailed, "status check failed: "+strings.Join(step.Verify, " ")
		return res
	}
	res.Status, res.Detail = StatusDone, "verified with "+strings.Join(step.Verify, " ")
	return res
}

// RunAll executes the steps in order. A failed step does not stop the rest.
func RunAll(env Env, steps []Step, choices map[string]string) []Result {
	var out []Result
	for _, s := range steps {
		out = append(out, Execute(env, s, choices[s.ID]))
	}
	return out
}

// shellStep changes the login shell with chsh and passes only when the
// account database shows the chosen path, so the change is verified, not assumed.
func shellStep(env Env, choice string, res Result) Result {
	shells, _ := env.ReadFile("/etc/shells")
	shellPath := ""
	for _, line := range strings.Split(string(shells), "\n") {
		if strings.HasPrefix(line, "/") && path.Base(line) == choice {
			shellPath = line
			break
		}
	}
	if shellPath == "" {
		res.Status, res.Detail = StatusFailed, fmt.Sprintf("%q is not listed in /etc/shells", choice)
		return res
	}
	if err := env.Run([]string{"chsh", "-s", shellPath}); err != nil {
		res.Status, res.Detail = StatusFailed, "chsh did not finish"
		return res
	}
	entry, err := env.Output([]string{"getent", "passwd", env.User()})
	if err != nil {
		res.Status, res.Detail = StatusFailed, "could not read the account database"
		return res
	}
	fields := strings.Split(strings.TrimSpace(entry), ":")
	if len(fields) < 7 || fields[6] != shellPath {
		res.Status, res.Detail = StatusFailed, "account still lists another shell"
		return res
	}
	res.Status, res.Detail = StatusDone, "default shell is "+shellPath+"; it applies in the next session"
	return res
}

// OSEnv is the real Env, used by `dots setup`.
type OSEnv struct{}

func (OSEnv) Run(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func (OSEnv) Output(argv []string) (string, error) {
	out, err := exec.Command(argv[0], argv[1:]...).Output()
	return string(out), err
}

func (OSEnv) Exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (OSEnv) ReadFile(p string) ([]byte, error) { return os.ReadFile(p) }

func (OSEnv) User() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return os.Getenv("LOGNAME")
}

func (OSEnv) Home() string {
	h, _ := os.UserHomeDir()
	return h
}
