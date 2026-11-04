// Package setup holds the ordered steps of `dots setup`. The order is data, so
// the command can show it and tests can check it without running anything.
package setup

// Step is one guided setup action. Login steps need the user to finish a
// login in the terminal; dots never reads or stores the resulting credential.
// Verify is the tool's own status command; when it is empty the step is
// reported as manual, never as done.
type Step struct {
	ID      string
	Title   string
	Login   bool
	Command []string
	Verify  []string
}

// Plan returns the steps in priority order: the default shell first, then
// steps that need no token, then login steps.
func Plan() []Step {
	return []Step{
		{ID: "shell", Title: "Default shell (zsh or bash)"},
		{ID: "zsh-p10k", Title: "zsh with powerlevel10k"},
		{ID: "claude", Title: "Claude Code login", Login: true, Command: []string{"claude"}},
		{ID: "copilot", Title: "Copilot (Neovim) sign-in", Login: true, Command: []string{"nvim"}},
		{ID: "gh", Title: "GitHub CLI login", Login: true, Command: []string{"gh", "auth", "login"}, Verify: []string{"gh", "auth", "status"}},
	}
}
