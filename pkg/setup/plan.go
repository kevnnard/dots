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
	// Guide is what the user runs or does, in their own terminal or app, to
	// give the tool a credential. dots never runs a guide and never reads the
	// credential; it only shows the guide and checks Verify afterwards.
	Guide string
}

// Plan returns the steps in priority order: the default shell first, then
// steps that need no token, then login steps.
func Plan() []Step {
	return []Step{
		{ID: "shell", Title: "Default shell (zsh or bash)"},
		{ID: "zsh-p10k", Title: "zsh with powerlevel10k"},
		{ID: "gentle-shell", Title: "Gentle Shell (bundled install)", Guide: "download gentle-shell-installer for your system from https://github.com/Gentleman-Programming/gentle-shell/releases/latest, extract it and run `sh install-gentle-shell.sh` (it asks before changing anything); then restore its configs with `dots restore gentle-shell --received` or `--with-config`", Verify: []string{"gentle-shell", "--version"}},
		{ID: "claude", Title: "Claude Code login", Login: true, Command: []string{"claude"}},
		{ID: "copilot", Title: "Copilot (Neovim) sign-in", Login: true, Command: []string{"nvim"}},
		{ID: "gh", Title: "GitHub CLI login", Login: true, Command: []string{"gh", "auth", "login"}, Verify: []string{"gh", "auth", "status"}},
		{ID: "codex", Title: "Codex login", Login: true, Guide: "run `codex login` in your terminal", Verify: []string{"codex", "login", "status"}},
		{ID: "antigravity", Title: "Antigravity sign-in", Login: true, Guide: "run `agy` in your terminal and sign in when it asks"},
		{ID: "gentle-shell-login", Title: "Gentle Shell login", Login: true, Guide: "run `gentle-shell` and use /login for each provider; login files never travel in backups"},
		{ID: "linear", Title: "Linear MCP sign-in", Login: true, Guide: "in pi or gentle-shell, sign in to the linear MCP server (OAuth) with /mcp"},
		{ID: "postgres", Title: "Postgres MCP connection", Login: true, Guide: "run `npx -y @microsoft/postgres-mcp connection add <name> \"<URI without password>\"`, then `connection set-password <name>` and type the password at its prompt"},
	}
}
