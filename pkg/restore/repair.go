package restore

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kevnnard/dots/pkg/homepath"
)

// Known repairs migrate exactly recognized incompatible content of specific
// configuration files. Anything not recognized is left byte for byte; this is
// not a generic validator or cleaner.

// repairOutcome is the result of one known content repair.
type repairOutcome struct {
	data    []byte // repaired content; nil when nothing changed
	applied string // what the repair changed
	issue   string // known problem left unchanged
}

// knownRepair is the migration for one file, named relative to the home.
type knownRepair struct {
	rel string
	fix func([]byte) repairOutcome
}

var knownRepairs = []knownRepair{
	{rel: ".zshrc", fix: repairZshrc},
}

// repairFor returns the known repair for a target path, if any.
func repairFor(target string) *knownRepair {
	home, err := homepath.Current()
	if err != nil || home == "" {
		return nil
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return nil
	}
	rel, err := filepath.Rel(home, abs)
	if err != nil {
		return nil
	}
	for i := range knownRepairs {
		if filepath.ToSlash(rel) == knownRepairs[i].rel {
			return &knownRepairs[i]
		}
	}
	return nil
}

// zshPlugins maps each plugin sourced by the legacy backup through
// $(dirname $BREW_BIN) to the system locations tried after Homebrew.
var zshPlugins = []struct{ file, fallbacks string }{
	{"zsh-syntax-highlighting/zsh-syntax-highlighting.zsh",
		"/usr/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh /usr/share/zsh/plugins/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh"},
	{"zsh-autosuggestions/zsh-autosuggestions.zsh",
		"/usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh /usr/share/zsh/plugins/zsh-autosuggestions/zsh-autosuggestions.zsh"},
	{"powerlevel10k/powerlevel10k.zsh-theme",
		`"$HOME/powerlevel10k/powerlevel10k.zsh-theme" /usr/share/zsh-theme-powerlevel10k/powerlevel10k.zsh-theme /usr/share/powerlevel10k/powerlevel10k.zsh-theme`},
}

// legacyZshLines maps each exact unguarded line of the legacy zsh backup to
// its guarded replacement. Matching ignores only surrounding whitespace.
var legacyZshLines = func() map[string]string {
	lines := map[string]string{
		`eval "$($BREW_BIN/brew shellenv)"`: `[[ -x "$BREW_BIN/brew" ]] && eval "$("$BREW_BIN/brew" shellenv)"`,
		`source $ZSH/oh-my-zsh.sh`:          `[[ -r "$ZSH/oh-my-zsh.sh" ]] && source "$ZSH/oh-my-zsh.sh"`,
		`source <(carapace _carapace)`:      `(( $+commands[carapace] )) && source <(carapace _carapace)`,
	}
	for _, tool := range []string{"fzf --zsh", "atuin init zsh", "zoxide init zsh"} {
		name := strings.Fields(tool)[0]
		lines[fmt.Sprintf(`eval "$(%s)"`, tool)] = fmt.Sprintf(`(( $+commands[%s] )) && eval "$(%s)"`, name, tool)
	}
	// A macOS backup pins pnpm under the absolute ~/Library/pnpm, which does
	// not exist on Linux; the switch keeps that path on macOS and uses XDG
	// elsewhere. The $HOME form is left alone: it is how such a switch
	// already spells the macOS branch.
	lines[`export PNPM_HOME="`+homepath.Token+`/Library/pnpm"`] = `if [[ "$OSTYPE" == darwin* ]]; then export PNPM_HOME="$HOME/Library/pnpm"; else export PNPM_HOME="${XDG_DATA_HOME:-$HOME/.local/share}/pnpm"; fi`
	for _, p := range zshPlugins {
		lines["source $(dirname $BREW_BIN)/share/"+p.file] = fmt.Sprintf(
			`for _dots_plugin in ${BREW_BIN:+"$(dirname "$BREW_BIN")/share/%s"} %s; do [[ -r "$_dots_plugin" ]] && { source "$_dots_plugin"; break; }; done; unset _dots_plugin`,
			p.file, p.fallbacks)
	}
	return lines
}()

// repairZshrc guards the known legacy plugin and tool lines, keeping their
// indentation and line endings and every other line unchanged.
func repairZshrc(data []byte) repairOutcome {
	lines := bytes.SplitAfter(data, []byte("\n"))
	n := 0
	for i, line := range lines {
		body := bytes.TrimRight(line, "\r\n")
		trimmed := bytes.TrimSpace(body)
		repl, ok := legacyZshLines[string(trimmed)]
		if !ok {
			continue
		}
		indent := body[:len(body)-len(bytes.TrimLeft(body, " \t"))]
		lines[i] = append(append(append([]byte{}, indent...), repl...), line[len(body):]...)
		n++
	}
	if n == 0 {
		return repairOutcome{}
	}
	return repairOutcome{
		data:    bytes.Join(lines, nil),
		applied: fmt.Sprintf("guarded %d legacy plugin or tool lines", n),
	}
}
