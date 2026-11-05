package restore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kevnnard/dots/pkg/homepath"
)

// Known repairs migrate exactly recognized incompatible content of specific
// configuration files. Anything not recognized is left byte for byte; this is
// not a generic validator or cleaner.

// repairOutcome is the result of one known content repair.
type repairOutcome struct {
	data     []byte // repaired content; nil when nothing changed
	applied  string // what the repair changed
	issue    string // known problem left unchanged
	conflict string // Engram conflict detail; blocks installs while present
}

// knownRepair is the migration for one file, named relative to the home.
type knownRepair struct {
	rel string
	fix func([]byte) repairOutcome
	// preflight makes a conflict in the existing local file block the
	// owning app's install until an authorized replacement repairs it.
	preflight bool
}

var knownRepairs = []knownRepair{
	{rel: ".zshrc", fix: repairZshrc},
	{rel: ".pi/agent/settings.json", fix: repairPiSettings, preflight: true},
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

const (
	engramPackage   = "npm:gentle-engram"
	obsoleteEngram  = "npm:gentle-engram@0.3.0"
	unknownConflict = "conflicting Engram package declarations in an unrecognized combination left unchanged; keep a single declaration manually"
)

// repairPiSettings removes the obsolete npm:gentle-engram@0.3.0 declaration
// only when the sole other Engram declaration is the canonical unversioned
// object filtered to +index.ts. Only that element and its separator are cut,
// so every other byte stays as written.
func repairPiSettings(data []byte) repairOutcome {
	if !json.Valid(data) {
		return repairOutcome{issue: "not valid JSON; left unchanged"}
	}
	base, packages, ok := topLevelValue(data, "packages")
	if !ok {
		return repairOutcome{}
	}
	dec := json.NewDecoder(bytes.NewReader(packages))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return repairOutcome{}
	}
	var starts, ends []int
	var engram []int
	obsolete, canonical := -1, false
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return repairOutcome{}
		}
		end := int(dec.InputOffset())
		starts, ends = append(starts, end-len(raw)), append(ends, end)
		source, isCanonical := engramDeclaration(raw)
		if source == "" {
			continue
		}
		engram = append(engram, len(starts)-1)
		if source == obsoleteEngram {
			obsolete = len(starts) - 1
		}
		canonical = canonical || isCanonical
	}
	if len(engram) < 2 {
		return repairOutcome{}
	}
	if len(engram) != 2 || obsolete < 0 || !canonical {
		return repairOutcome{issue: unknownConflict, conflict: "unrecognized combination"}
	}
	cutFrom, cutTo := starts[obsolete], ends[obsolete]
	switch {
	case obsolete > 0:
		cutFrom = ends[obsolete-1]
	case len(starts) > 1:
		cutTo = starts[1]
	}
	out := append(append([]byte{}, data[:base+cutFrom]...), data[base+cutTo:]...)
	return repairOutcome{
		data:     out,
		applied:  "removed obsolete duplicate " + obsoleteEngram + " package declaration",
		conflict: "obsolete duplicate " + obsoleteEngram,
	}
}

// engramDeclaration returns the Engram source a package entry declares, if
// any, and whether it is exactly the canonical filtered object.
func engramDeclaration(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if s == engramPackage || strings.HasPrefix(s, engramPackage+"@") {
			return s, false
		}
		return "", false
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || json.Unmarshal(obj["source"], &s) != nil {
		return "", false
	}
	if s != engramPackage && !strings.HasPrefix(s, engramPackage+"@") {
		return "", false
	}
	var ext []string
	canonical := s == engramPackage && len(obj) == 2 &&
		json.Unmarshal(obj["extensions"], &ext) == nil && len(ext) == 1 && ext[0] == "+index.ts"
	return s, canonical
}

// topLevelValue returns the offset and raw bytes of key in a top-level JSON
// object. A missing or repeated key reports false.
func topLevelValue(data []byte, key string) (int, json.RawMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return 0, nil, false
	}
	found := false
	var base int
	var value json.RawMessage
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return 0, nil, false
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return 0, nil, false
		}
		if tok != key {
			continue
		}
		if found {
			return 0, nil, false
		}
		found, value, base = true, raw, int(dec.InputOffset())-len(raw)
	}
	return base, value, found
}

// persistRepaired swaps target for data with perm. Tests replace it to
// inject write failures.
var persistRepaired = func(target string, data []byte, perm os.FileMode) error {
	return replaceWith(target, func(tmp string) error {
		if err := writeNew(tmp, data); err != nil {
			return err
		}
		return os.Chmod(tmp, perm)
	})
}

// repairLocal applies repaired content to an existing regular local file
// confined to its write guard. The file is only changed after its safety
// backup succeeds, and the result is read back to prove the conflict is gone.
func repairLocal(target string, info os.FileInfo, data []byte, repair *knownRepair) (string, error) {
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", target)
	}
	guard, err := newWriteGuard(target)
	if err == nil {
		err = guard.check(target)
	}
	if err != nil {
		return "", err
	}
	bk, err := backupExisting(target)
	if err != nil {
		return "", fmt.Errorf("safety backup failed: %v", err)
	}
	if err := persistRepaired(target, data, info.Mode().Perm()); err != nil {
		return "", fmt.Errorf("%v; original kept, safety backup %s", err, bk)
	}
	got, err := os.ReadFile(target)
	after, statErr := os.Lstat(target)
	if err != nil || statErr != nil || !bytes.Equal(got, data) || after.Mode().Perm() != info.Mode().Perm() || repair.fix(got).conflict != "" {
		return "", fmt.Errorf("repaired content did not persist; safety backup %s", bk)
	}
	return bk, nil
}
