package restore

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kevnnard/dots/pkg/manifest"
)

// legacyZshrc reproduces the unguarded lines of the zsh backup published
// before the portable install, around user customizations.
const legacyZshrc = `# user header
if [[ "$(uname)" == "Darwin" ]]; then
    BREW_BIN="/opt/homebrew/bin"
else
    BREW_BIN="/home/linuxbrew/.linuxbrew/bin"
fi
eval "$($BREW_BIN/brew shellenv)"
# source $(dirname $BREW_BIN)/share/zsh-autocomplete/zsh-autocomplete.plugin.zsh
source $(dirname $BREW_BIN)/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh
source $(dirname $BREW_BIN)/share/zsh-autosuggestions/zsh-autosuggestions.zsh
  source $(dirname $BREW_BIN)/share/powerlevel10k/powerlevel10k.zsh-theme
alias mine='echo custom'
source $ZSH/oh-my-zsh.sh
source <(carapace _carapace)
eval "$(fzf --zsh)"
eval "$(atuin init zsh)"
eval "$(zoxide init zsh)"
source ~/.custom-plugin.zsh
[[ ! -f ~/.p10k.zsh ]] || source ~/.p10k.zsh
`

const repairedZshrc = `# user header
if [[ "$(uname)" == "Darwin" ]]; then
    BREW_BIN="/opt/homebrew/bin"
else
    BREW_BIN="/home/linuxbrew/.linuxbrew/bin"
fi
[[ -x "$BREW_BIN/brew" ]] && eval "$("$BREW_BIN/brew" shellenv)"
# source $(dirname $BREW_BIN)/share/zsh-autocomplete/zsh-autocomplete.plugin.zsh
for _dots_plugin in ${BREW_BIN:+"$(dirname "$BREW_BIN")/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh"} /usr/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh /usr/share/zsh/plugins/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh; do [[ -r "$_dots_plugin" ]] && { source "$_dots_plugin"; break; }; done; unset _dots_plugin
for _dots_plugin in ${BREW_BIN:+"$(dirname "$BREW_BIN")/share/zsh-autosuggestions/zsh-autosuggestions.zsh"} /usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh /usr/share/zsh/plugins/zsh-autosuggestions/zsh-autosuggestions.zsh; do [[ -r "$_dots_plugin" ]] && { source "$_dots_plugin"; break; }; done; unset _dots_plugin
  for _dots_plugin in ${BREW_BIN:+"$(dirname "$BREW_BIN")/share/powerlevel10k/powerlevel10k.zsh-theme"} "$HOME/powerlevel10k/powerlevel10k.zsh-theme" /usr/share/zsh-theme-powerlevel10k/powerlevel10k.zsh-theme /usr/share/powerlevel10k/powerlevel10k.zsh-theme; do [[ -r "$_dots_plugin" ]] && { source "$_dots_plugin"; break; }; done; unset _dots_plugin
alias mine='echo custom'
[[ -r "$ZSH/oh-my-zsh.sh" ]] && source "$ZSH/oh-my-zsh.sh"
(( $+commands[carapace] )) && source <(carapace _carapace)
(( $+commands[fzf] )) && eval "$(fzf --zsh)"
(( $+commands[atuin] )) && eval "$(atuin init zsh)"
(( $+commands[zoxide] )) && eval "$(zoxide init zsh)"
source ~/.custom-plugin.zsh
[[ ! -f ~/.p10k.zsh ]] || source ~/.p10k.zsh
`

const zshRepairNote = "guarded 9 legacy plugin or tool lines"

const conflictingPiSettings = `{
  "defaultModel": "x",
  "packages": [
    "npm:gentle-pi",
    {
      "source": "npm:gentle-engram",
      "extensions": [
        "+index.ts"
      ]
    },
    "npm:pi-btw",
    "npm:gentle-engram@0.3.0"
  ],
  "theme": "t"
}
`

const repairedPiSettings = `{
  "defaultModel": "x",
  "packages": [
    "npm:gentle-pi",
    {
      "source": "npm:gentle-engram",
      "extensions": [
        "+index.ts"
      ]
    },
    "npm:pi-btw"
  ],
  "theme": "t"
}
`

const piRepairNote = "removed obsolete duplicate npm:gentle-engram@0.3.0 package declaration"

// repairFixture maps one backed-up file to a target inside an isolated HOME.
func repairFixture(t *testing.T, homeRel, dest, backup string) (manifest.AppConfig, string, string) {
	t.Helper()
	root, home := isolatedHome(t)
	repo := filepath.Join(root, "repo")
	mustWrite(t, filepath.Join(repo, dest), backup)
	target := filepath.Join(home, filepath.FromSlash(homeRel))
	app := manifest.AppConfig{Paths: []manifest.PathMapping{{Src: target, Dest: dest}}}
	return app, repo, target
}

func TestApplyRepairsLegacyZshBackupBeforeDeploying(t *testing.T) {
	app, repo, target := repairFixture(t, ".zshrc", "configs/zsh/.zshrc", legacyZshrc)

	res, err := ApplyAppFilesWith("zsh", app, repo, ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := readOrEmpty(target); got != repairedZshrc {
		t.Fatalf("deployed .zshrc:\n%s\nwant:\n%s", got, repairedZshrc)
	}
	if want := []string{target + ": " + zshRepairNote}; !reflect.DeepEqual(res.Repaired, want) {
		t.Errorf("repaired = %q, want %q", res.Repaired, want)
	}
	if len(res.NotRepaired) != 0 || len(res.Warnings) != 0 {
		t.Errorf("not repaired = %q warnings = %q", res.NotRepaired, res.Warnings)
	}
	if data := readOrEmpty(filepath.Join(repo, "configs/zsh/.zshrc")); data != legacyZshrc {
		t.Error("repository backup must never be rewritten")
	}
	if zsh, err := exec.LookPath("zsh"); err == nil {
		if out, err := exec.Command(zsh, "-n", target).CombinedOutput(); err != nil {
			t.Errorf("zsh -n: %v\n%s", err, out)
		}
	}

	// A second run finds the equivalent repaired file and leaves it alone.
	res, err = ApplyAppFilesWith("zsh", app, repo, ApplyOptions{Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Restored, []string{target}) || len(res.Repaired) != 0 || len(res.LocalBackups) != 0 {
		t.Errorf("rerun restored=%q repaired=%q backups=%q", res.Restored, res.Repaired, res.LocalBackups)
	}
	if got := listDir(t, filepath.Dir(target)); !reflect.DeepEqual(got, []string{".zshrc"}) {
		t.Errorf("home = %q", got)
	}
}

func TestApplyKeepsStaleLocalZshrcAndReportsRepairNotApplied(t *testing.T) {
	app, repo, target := repairFixture(t, ".zshrc", "configs/zsh/.zshrc", "# current backup\n")
	mustWrite(t, target, legacyZshrc)

	for _, opts := range []ApplyOptions{{}, {DryRun: true}} {
		res, err := ApplyAppFilesWith("zsh", app, repo, opts)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{target + ": existing local file kept, repair not applied (would have " + zshRepairNote + "); authorize replacing existing files to repair it after a safety backup"}
		if !reflect.DeepEqual(res.NotRepaired, want) {
			t.Errorf("dry=%v not repaired = %q, want %q", opts.DryRun, res.NotRepaired, want)
		}
		if !reflect.DeepEqual(res.Kept, []string{target}) || len(res.Repaired) != 0 {
			t.Errorf("dry=%v kept=%q repaired=%q", opts.DryRun, res.Kept, res.Repaired)
		}
	}
	if readOrEmpty(target) != legacyZshrc {
		t.Error("kept local file was modified")
	}
	if got := listDir(t, filepath.Dir(target)); !reflect.DeepEqual(got, []string{".zshrc"}) {
		t.Errorf("home = %q", got)
	}
}

func TestApplyReplacesStaleLocalZshrcWithRepairedBackupAfterSafetyBackup(t *testing.T) {
	app, repo, target := repairFixture(t, ".zshrc", "configs/zsh/.zshrc", legacyZshrc)
	mustWrite(t, target, "# local\n")

	res, err := ApplyAppFilesWith("zsh", app, repo, ApplyOptions{Replace: true, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if readOrEmpty(target) != "# local\n" || len(listDir(t, filepath.Dir(target))) != 1 || len(res.LocalBackups) != 0 {
		t.Fatalf("dry run wrote files: backups=%q", res.LocalBackups)
	}

	res, err = ApplyAppFilesWith("zsh", app, repo, ApplyOptions{Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.LocalBackups) != 1 || readOrEmpty(res.LocalBackups[0]) != "# local\n" {
		t.Fatalf("safety backup = %q", res.LocalBackups)
	}
	if readOrEmpty(target) != repairedZshrc {
		t.Errorf("target not repaired:\n%s", readOrEmpty(target))
	}
	if want := []string{target + ": " + zshRepairNote}; !reflect.DeepEqual(res.Repaired, want) {
		t.Errorf("repaired = %q", res.Repaired)
	}
}

func TestApplyRemovesOnlyKnownObsoleteEngramDuplicate(t *testing.T) {
	app, repo, target := repairFixture(t, ".pi/agent/settings.json", "configs/pi/settings.json", conflictingPiSettings)

	res, err := ApplyAppFilesWith("pi", app, repo, ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := readOrEmpty(target); got != repairedPiSettings {
		t.Fatalf("deployed settings:\n%s", got)
	}
	if want := []string{target + ": " + piRepairNote}; !reflect.DeepEqual(res.Repaired, want) {
		t.Errorf("repaired = %q", res.Repaired)
	}

	res, err = ApplyAppFilesWith("pi", app, repo, ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Restored, []string{target}) || len(res.Repaired)+len(res.Kept)+len(res.NotRepaired) != 0 {
		t.Errorf("rerun = %+v", res)
	}
}

func TestRepairPiSettingsTriangulation(t *testing.T) {
	canonical := `{"source":"npm:gentle-engram","extensions":["+index.ts"]}`
	cases := []struct {
		name, in, want, applied, issue string
	}{
		{"duplicate first", `{"packages":["npm:gentle-engram@0.3.0",` + canonical + `,"npm:x"]}`,
			`{"packages":[` + canonical + `,"npm:x"]}`, piRepairNote, ""},
		{"single declaration", `{"packages":[` + canonical + `]}`, "", "", ""},
		{"other version", `{"packages":[` + canonical + `,"npm:gentle-engram@0.4.0"]}`, "", "",
			"conflicting Engram package declarations in an unrecognized combination left unchanged; keep a single declaration manually"},
		{"different filter", `{"packages":[{"source":"npm:gentle-engram","extensions":["-index.ts"]},"npm:gentle-engram@0.3.0"]}`, "", "",
			"conflicting Engram package declarations in an unrecognized combination left unchanged; keep a single declaration manually"},
		{"invalid json", `{"packages":[`, "", "", "not valid JSON; left unchanged"},
		{"no packages", `{"theme":"t"}`, "", "", ""},
	}
	for _, c := range cases {
		out := repairPiSettings([]byte(c.in))
		if string(out.data) != c.want || out.applied != c.applied || out.issue != c.issue {
			t.Errorf("%s: data=%q applied=%q issue=%q", c.name, out.data, out.applied, out.issue)
		}
	}
}

func TestApplyPreservesUnknownPiConflictAndInvalidJSON(t *testing.T) {
	for _, backup := range []string{
		`{"packages":[{"source":"npm:gentle-engram","extensions":["+index.ts"]},"npm:gentle-engram@0.4.0"]}`,
		`{"packages": [`,
	} {
		app, repo, target := repairFixture(t, ".pi/agent/settings.json", "configs/pi/settings.json", backup)
		res, err := ApplyAppFilesWith("pi", app, repo, ApplyOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if readOrEmpty(target) != backup {
			t.Errorf("backup bytes changed on deployment: %q", readOrEmpty(target))
		}
		if len(res.Repaired) != 0 || len(res.NotRepaired) != 1 || !strings.HasPrefix(res.NotRepaired[0], target+": backup deployed without repair: ") {
			t.Errorf("repaired=%q not repaired=%q", res.Repaired, res.NotRepaired)
		}
		if strings.Contains(strings.Join(res.NotRepaired, ""), "0.4.0") {
			t.Error("diagnostics must not echo configuration values")
		}
	}
}

func TestApplyLeavesUnknownFilesWithSimilarContentUntouched(t *testing.T) {
	app, repo, target := repairFixture(t, ".config/tool/settings.json", "configs/tool/settings.json", conflictingPiSettings)
	res, err := ApplyAppFilesWith("tool", app, repo, ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if readOrEmpty(target) != conflictingPiSettings || len(res.Repaired)+len(res.NotRepaired) != 0 {
		t.Errorf("unknown file changed: repaired=%q not repaired=%q", res.Repaired, res.NotRepaired)
	}
}

func TestCheckedInPiSettingsHaveNoEngramConflict(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "configs", "pi", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if out := repairPiSettings(data); out.data != nil || out.issue != "" || out.conflict != "" {
		t.Errorf("checked-in Pi settings need repair: applied=%q issue=%q", out.applied, out.issue)
	}
}

// A macOS backup pins pnpm under ~/Library/pnpm, which does not exist on
// Linux and breaks pnpm detection there. Restore rewrites that exact line to a
// switch that keeps the macOS path on macOS and uses the XDG path elsewhere.
func TestApplyMakesMacOnlyPnpmHomePortable(t *testing.T) {
	const portable = `if [[ "$OSTYPE" == darwin* ]]; then export PNPM_HOME="$HOME/Library/pnpm"; else export PNPM_HOME="${XDG_DATA_HOME:-$HOME/.local/share}/pnpm"; fi`
	for _, line := range []string{`export PNPM_HOME="__HOME__/Library/pnpm"`} {
		backup := "# pnpm\n" + line + "\nexport A=1\n"
		app, repo, target := repairFixture(t, ".zshrc", "configs/zsh/.zshrc", backup)
		res, err := ApplyAppFilesWith("zsh", app, repo, ApplyOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := readOrEmpty(target), "# pnpm\n"+portable+"\nexport A=1\n"; got != want {
			t.Errorf("%s: deployed\n%s\nwant\n%s", line, got, want)
		}
		if len(res.Repaired) != 1 {
			t.Errorf("%s: repaired = %q", line, res.Repaired)
		}
		if zsh, err := exec.LookPath("zsh"); err == nil {
			if out, err := exec.Command(zsh, "-n", target).CombinedOutput(); err != nil {
				t.Errorf("zsh -n: %v\n%s", err, out)
			}
		}
	}
}

// The portable $HOME form inside an OS switch is already correct: restore
// must not wrap it in a second switch.
func TestApplyLeavesGuardedPnpmHomeAlone(t *testing.T) {
	const guarded = "if [[ \"$OSTYPE\" == darwin* ]]; then\n  export PNPM_HOME=\"$HOME/Library/pnpm\"\nelse\n  export PNPM_HOME=\"${XDG_DATA_HOME:-$HOME/.local/share}/pnpm\"\nfi\n"
	app, repo, target := repairFixture(t, ".zshrc", "configs/zsh/.zshrc", guarded)
	res, err := ApplyAppFilesWith("zsh", app, repo, ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := readOrEmpty(target); got != guarded {
		t.Errorf("deployed\n%s\nwant unchanged\n%s", got, guarded)
	}
	if len(res.Repaired) != 0 {
		t.Errorf("repaired = %q, want none", res.Repaired)
	}
}
