package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
	"github.com/kevnnard/dots/pkg/restore"
	"github.com/kevnnard/dots/pkg/transfer"
)

func TestUIModelNavigation(t *testing.T) {
	m := manifest.DefaultManifest()
	sys := detector.Detect()
	repoDir := "/tmp/mock-dotfiles"

	model := NewModel(m, sys, repoDir)

	// Initial view should be main
	if model.State != viewMain {
		t.Errorf("expected viewMain, got %v", model.State)
	}

	// Press down arrow
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	mUpdated := updated.(Model)
	if mUpdated.Cursor != 1 {
		t.Errorf("expected cursor 1, got %d", mUpdated.Cursor)
	}

	// Press up arrow
	updated, _ = mUpdated.Update(tea.KeyMsg{Type: tea.KeyUp})
	mUpdated = updated.(Model)
	if mUpdated.Cursor != 0 {
		t.Errorf("expected cursor 0, got %d", mUpdated.Cursor)
	}

	// Press enter on cursor 0 (Restore)
	updated, _ = mUpdated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mUpdated = updated.(Model)
	if mUpdated.State != viewRestorePreset {
		t.Errorf("expected viewRestorePreset, got %v", mUpdated.State)
	}

	// Render view
	viewOutput := mUpdated.View()
	if !strings.Contains(viewOutput, "WORKSTATION & DOTFILES MANAGER") {
		t.Errorf("expected header in view output, got: %s", viewOutput)
	}
}

func TestUICustomAppsToggle(t *testing.T) {
	m := manifest.DefaultManifest()
	sys := detector.Detect()
	model := NewModel(m, sys, "/tmp/mock-dotfiles")

	// Transition to custom apps view
	model.State = viewCustomApps
	model.Cursor = 0

	initialChecked := model.AppItems[0].Checked

	// Press space to toggle
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeySpace})
	mUpdated := updated.(Model)
	if mUpdated.AppItems[0].Checked == initialChecked {
		t.Error("expected checkbox to toggle on space")
	}

	// Press 'a' to toggle all
	updated, _ = mUpdated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	mUpdated = updated.(Model)
	for _, item := range mUpdated.AppItems {
		if !item.Checked {
			t.Error("expected all items to be checked after pressing 'a'")
		}
	}
}

type tuiFixture struct {
	target string
	model  Model
	calls  []string
	reqs   []restore.ExecRequest
	fail   map[string]bool
	// suspended counts terminal handoffs; stdin and out stand in for the
	// terminal Bubble Tea hands to the install commands.
	suspended int
	stdin     *strings.Reader
	out       bytes.Buffer
}

func pacman(pkg string) map[string]manifest.InstallRecipe {
	return map[string]manifest.InstallRecipe{"pacman": {Manager: "pacman", Package: pkg}}
}

// newTUIFixture builds a model whose package probe and executor are fakes.
func newTUIFixture(t *testing.T) *tuiFixture {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	f := &tuiFixture{target: filepath.Join(root, "home", ".zshrc"), fail: map[string]bool{}}
	t.Setenv("HOME", filepath.Join(root, "home"))
	os.MkdirAll(filepath.Join(repo, "configs", "zsh"), 0755)
	os.WriteFile(filepath.Join(repo, "configs", "zsh", ".zshrc"), []byte("# backed up\n"), 0644)
	m := &manifest.Manifest{Apps: map[string]manifest.AppConfig{
		"zsh": {
			ConfigRequires: []string{"zsh-theme"},
			Paths:          []manifest.PathMapping{{Src: f.target, Dest: "configs/zsh/.zshrc"}},
			Install:        pacman("zsh"),
		},
		// Optional: only the config toggle pulls it, through config_requires.
		"zsh-theme": {Optional: true, Requires: []string{"zsh"}, Install: pacman("theme")},
		"extra":     {Optional: true, Install: pacman("extra")},
	}}
	sys := &detector.SystemInfo{OS: "linux", Distro: detector.DistroArch, PrimaryManager: detector.ManagerPacman,
		Managers: []detector.PackageManager{detector.ManagerPacman}}
	f.model = NewModel(m, sys, repo)
	f.model.Probe = func(string, manifest.AppConfig) bool { return false }
	f.model.Exec = func(req restore.ExecRequest) error {
		f.calls = append(f.calls, req.AppName)
		f.reqs = append(f.reqs, req)
		if f.fail[req.AppName] {
			return errors.New("exit status 1")
		}
		return nil
	}
	f.stdin = strings.NewReader("")
	f.model.Suspend = f.suspend
	return f
}

// suspend mimics tea.Exec: it hands the fake terminal to the command, runs
// it and reports through the callback.
func (f *tuiFixture) suspend(c tea.ExecCommand, fn tea.ExecCallback) tea.Cmd {
	return func() tea.Msg {
		f.suspended++
		c.SetStdin(f.stdin)
		c.SetStdout(&f.out)
		c.SetStderr(&f.out)
		return fn(c.Run())
	}
}

// drain runs the commands Update returns and feeds their messages back, the
// way a Bubble Tea program would.
func drain(m tea.Model, cmd tea.Cmd) tea.Model {
	for i := 0; cmd != nil && i < 10; i++ {
		msg := cmd()
		if msg == nil {
			return m
		}
		if _, quit := msg.(tea.QuitMsg); quit {
			return m
		}
		m, cmd = m.Update(msg)
	}
	return m
}

func press(m tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "space":
			msg = tea.KeyMsg{Type: tea.KeySpace}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEscape}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		m = drain(m, cmd)
	}
	return m
}

func TestTUIFullSetupKeepsStockConfigAndSkipsOptional(t *testing.T) {
	f := newTUIFixture(t)
	view := press(f.model, "enter").View()
	if !strings.Contains(view, "Restore backed-up configs") || !strings.Contains(view, "off") {
		t.Errorf("config toggle must be visible and off by default:\n%s", view)
	}
	done := press(f.model, "enter", "enter").(Model)
	if got := strings.Join(f.calls, ","); got != "zsh" {
		t.Errorf("full setup ran %q, want zsh (no config deps, no optional apps)", got)
	}
	if _, err := os.Stat(f.target); !os.IsNotExist(err) {
		t.Error("full setup without the config toggle deployed the repository config")
	}
	if !strings.Contains(done.View(), "stock configuration") {
		t.Errorf("summary must say configs were not restored:\n%s", done.View())
	}
}

func TestTUIConfigToggleRestoresAfterPluginsAndKeepsLocalFiles(t *testing.T) {
	f := newTUIFixture(t)
	os.MkdirAll(filepath.Dir(f.target), 0755)
	os.WriteFile(f.target, []byte("# mine\n"), 0644)
	preset := press(f.model, "enter", "c")
	if !strings.Contains(preset.View(), "kept") {
		t.Errorf("config toggle must state that local files are kept:\n%s", preset.View())
	}
	done := press(preset, "enter").(Model)
	if got := strings.Join(f.calls, ","); got != "zsh,zsh-theme" {
		t.Errorf("ran %q", got)
	}
	if data, _ := os.ReadFile(f.target); string(data) != "# mine\n" {
		t.Errorf("local config overwritten without the replace toggle: %q", data)
	}
	if !strings.Contains(done.View(), "kept") {
		t.Errorf("summary must name the kept file:\n%s", done.View())
	}

	f.calls = nil
	press(f.model, "enter", "c", "r", "enter")
	if data, _ := os.ReadFile(f.target); string(data) != "# backed up\n" {
		t.Errorf("replace toggle did not deploy: %q", data)
	}
	if backups, _ := filepath.Glob(f.target + ".*.bk"); len(backups) != 1 {
		t.Errorf("safety backups = %v", backups)
	}
}

func TestTUIReportsFailuresHonestly(t *testing.T) {
	f := newTUIFixture(t)
	f.fail["zsh-theme"] = true
	done := press(f.model, "enter", "c", "enter").(Model)
	view := done.View()
	if strings.Contains(view, "successfully") || !strings.Contains(view, "problem") || !strings.Contains(view, "incomplete") {
		t.Errorf("summary must report the failure and incomplete config:\n%s", view)
	}
	if done.LastRun == nil || len(done.LastRun.Problems()) == 0 {
		t.Error("the shared run result must be exposed with its problems")
	}
	if _, err := os.Stat(f.target); !os.IsNotExist(err) {
		t.Error("config deployed although its plugin failed")
	}
}

func TestTUICustomSelectionCanPickOptionalApps(t *testing.T) {
	f := newTUIFixture(t)
	m := press(f.model, "enter").(Model)
	m.Cursor = 4 // Custom Selection
	custom := press(m, "enter").(Model)
	for i := range custom.AppItems {
		custom.AppItems[i].Checked = custom.AppItems[i].Name == "extra"
	}
	press(custom, "enter")
	if got := strings.Join(f.calls, ","); got != "extra" {
		t.Errorf("custom selection ran %q, want extra", got)
	}
}

func TestTUIAppItemsAreSorted(t *testing.T) {
	f := newTUIFixture(t)
	var names []string
	for _, it := range f.model.AppItems {
		names = append(names, it.Name)
	}
	if got := strings.Join(names, ","); got != "extra,zsh,zsh-theme" {
		t.Errorf("items = %s", got)
	}
}

func TestTUIRestoreHandsTerminalToInstallCommands(t *testing.T) {
	f := newTUIFixture(t)
	preset := press(f.model, "enter")
	updated, cmd := preset.Update(tea.KeyMsg{Type: tea.KeyEnter}) // Full Setup
	running := updated.(Model)
	if len(f.calls) != 0 || f.suspended != 0 {
		t.Fatalf("Update ran install commands inline (calls=%v, handoffs=%d)", f.calls, f.suspended)
	}
	if running.State != viewExecuting || cmd == nil {
		t.Fatalf("restore must return a terminal handoff command; state=%v cmd=%v", running.State, cmd != nil)
	}
	if !strings.Contains(running.View(), "terminal") {
		t.Errorf("executing view must explain the terminal handoff:\n%s", running.View())
	}
	// Keys are ignored while the install runs.
	if again, c := running.Update(tea.KeyMsg{Type: tea.KeyEnter}); again.(Model).State != viewExecuting || c != nil {
		t.Error("a key press while executing must not start another run")
	}

	done := drain(running, cmd).(Model)
	if f.suspended != 1 {
		t.Errorf("terminal handoffs = %d, want 1 for the whole run", f.suspended)
	}
	if len(f.reqs) != 1 || f.reqs[0].Stdin != io.Reader(f.stdin) || f.reqs[0].Stdout != io.Writer(&f.out) {
		t.Fatalf("install commands must get the handed-off terminal for sudo prompts: %+v", f.reqs)
	}
	if !strings.Contains(f.out.String(), "▶ [zsh]") {
		t.Errorf("each command must be announced in the terminal before it runs:\n%s", f.out.String())
	}
	if done.State != viewFinished || done.LastRun == nil || !strings.Contains(done.View(), "Finished without problems") {
		t.Errorf("summary must be shown after the handoff:\n%s", done.View())
	}
}

func TestTUIReportsTerminalHandoffFailure(t *testing.T) {
	f := newTUIFixture(t)
	f.model.Suspend = func(c tea.ExecCommand, fn tea.ExecCallback) tea.Cmd {
		return func() tea.Msg { return fn(errors.New("could not release terminal")) }
	}
	done := press(f.model, "enter", "enter").(Model)
	view := done.View()
	if len(f.calls) != 0 {
		t.Errorf("commands ran although the terminal was not handed off: %v", f.calls)
	}
	if done.State != viewFinished || done.LastRun != nil {
		t.Errorf("state=%v LastRun=%v", done.State, done.LastRun)
	}
	if !strings.Contains(view, "could not release terminal") || strings.Contains(view, "without problems") {
		t.Errorf("handoff failure must be reported honestly:\n%s", view)
	}
	if back := press(done, "enter").(Model); back.State != viewMain {
		t.Error("the user must be able to leave the failure screen")
	}
}

// finishOnSummary quits the program once the restore summary is shown.
type finishOnSummary struct{ Model }

func (w finishOnSummary) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := w.Model.Update(msg)
	if mm := m.(Model); mm.State == viewFinished {
		return finishOnSummary{mm}, tea.Quit
	}
	return finishOnSummary{m.(Model)}, cmd
}

// The default handoff is Bubble Tea's own Exec: a real program releases its
// input and passes it to the install commands. The executor stays fake.
func TestTUIDefaultHandoffUsesBubbleTeaExec(t *testing.T) {
	f := newTUIFixture(t)
	f.model.Suspend = NewModel(f.model.Manifest, f.model.SysInfo, f.model.RepoDir).Suspend
	in, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	defer w.Close()
	var screen bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := tea.NewProgram(finishOnSummary{f.model}, tea.WithInput(in), tea.WithOutput(&screen), tea.WithContext(ctx))
	go func() {
		p.Send(tea.KeyMsg{Type: tea.KeyEnter}) // Restore / Install
		p.Send(tea.KeyMsg{Type: tea.KeyEnter}) // Full Setup
	}()
	final, err := p.Run()
	if err != nil {
		t.Fatalf("program: %v", err)
	}
	done := final.(finishOnSummary).Model
	if got := strings.Join(f.calls, ","); got != "zsh" {
		t.Fatalf("ran %q", got)
	}
	if f.reqs[0].Stdin != io.Reader(in) {
		t.Errorf("install command stdin = %v, want the program input %v", f.reqs[0].Stdin, in)
	}
	if done.LastRun == nil || !strings.Contains(screen.String(), "▶ [zsh]") {
		t.Errorf("run result missing or command not announced on the terminal:\n%s", screen.String())
	}
}

func TestTUIReportsPlanErrorsAfterHandoff(t *testing.T) {
	f := newTUIFixture(t)
	// A requires cycle makes the shared planner refuse the run.
	f.model.Manifest.Apps["zsh"] = manifest.AppConfig{Requires: []string{"zsh-theme"}, Install: pacman("zsh")}
	done := press(f.model, "enter", "enter").(Model)
	if len(f.calls) != 0 || done.LastRun != nil {
		t.Errorf("an unplannable run must execute nothing: calls=%v", f.calls)
	}
	if view := done.View(); !strings.Contains(view, "Cannot plan restore") || strings.Contains(view, "without problems") {
		t.Errorf("plan error must be reported:\n%s", view)
	}
}

func TestTUIFinishedViewShowsReadinessWithoutValues(t *testing.T) {
	f := newTUIFixture(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	agent := filepath.Join(home, ".agent", "mcp.json")
	os.MkdirAll(filepath.Dir(agent), 0755)
	os.WriteFile(agent, []byte(`{"[SYNTHETIC_CANARY\nFORGED]":"YOUR_TEST_HERE","mcpServers": {"safe": {"command": "/fake/SYNTHETIC_CANARY"}, "db": {"env": {"DATABASE_URL": "YOUR_DB_URL_HERE", "TOKEN": "tui-real-secret-789"}}}}`), 0600)
	zsh := f.model.Manifest.Apps["zsh"]
	zsh.Paths = append(zsh.Paths, manifest.PathMapping{Src: "~/.agent/mcp.json", Dest: "configs/agent/mcp.json"})
	f.model.Manifest.Apps["zsh"] = zsh
	// ~/.agent/mcp.json is not in the built-in inventory: it is named by its
	// manifest position.
	where := fmt.Sprintf("path #%d of zsh", len(zsh.Paths))

	done := press(f.model, "enter", "enter").(Model)
	view := done.View()
	// Failures never print the view: it may carry the planted canaries.
	if !strings.Contains(view, "Readiness") || !strings.Contains(view, "field mcpServers.<key #1>.env.DATABASE_URL in "+where) ||
		!strings.Contains(view, "field <key #1> in "+where) || strings.Contains(view, "~/.agent/") {
		t.Errorf("finished view must show the shared readiness report")
	}
	if !strings.Contains(view, "MCP server command (mcpServers.<key #2>.command in "+where+")") {
		t.Errorf("finished view must name the field and file of a missing MCP command")
	}
	for _, leak := range []string{"tui-real-secret-789", "YOUR_DB_URL_HERE", "SYNTHETIC_CANARY", "FORGED", "/fake/"} {
		if strings.Contains(view, leak) {
			t.Errorf("finished view leaks a planted fixture value")
		}
	}
}

func TestTUIFinishedViewEscapesControlsFromManifestAndErrors(t *testing.T) {
	f := newTUIFixture(t)
	t.Setenv("HOME", t.TempDir())
	name := "evil\x1b[2J"
	f.model.Manifest.Apps[name] = manifest.AppConfig{Install: pacman("evil\x1b]0;SYNTH\x07")}
	f.model.AppItems = append(f.model.AppItems, CheckItem{Name: name, Profile: "core"})
	f.model.Exec = func(req restore.ExecRequest) error {
		f.calls = append(f.calls, req.AppName)
		if req.AppName == name {
			return errors.New("exit\x1b]0;SYNTH\x07 1")
		}
		return nil
	}

	done := press(f.model, "enter", "enter").(Model)
	view := done.View()
	// Failures never print the view: it carries the planted sequences.
	if !strings.Contains(strings.Join(f.calls, ","), name) {
		t.Errorf("the executor must receive the app's own identity")
	}
	// Styles emit their own SGR sequences; the planted ones must not survive.
	for _, seq := range []string{"\x1b[2J", "\x1b]0;", "\x07"} {
		if strings.Contains(view, seq) {
			t.Errorf("finished view keeps a raw planted control sequence")
		}
	}
	if !strings.Contains(view, `evil\u001b[2J`) {
		t.Errorf("finished view must show the escaped app name")
	}
}

// The finished view shows the shared repair report: a kept stale zsh file is
// a problem, an authorized replacement reports the repair.
func TestTUIReportsKnownZshRepairThroughSharedSummary(t *testing.T) {
	f := newTUIFixture(t)
	legacy := "source $(dirname $BREW_BIN)/share/powerlevel10k/powerlevel10k.zsh-theme\n"
	t.Setenv("HOME", filepath.Dir(f.target))
	os.MkdirAll(filepath.Dir(f.target), 0755)
	os.WriteFile(f.target, []byte(legacy), 0644)
	os.WriteFile(filepath.Join(f.model.RepoDir, "configs", "zsh", ".zshrc"), []byte(legacy), 0644)

	kept := press(f.model, "enter", "c", "enter").(Model)
	view := kept.View()
	if strings.Contains(view, "without problems") || !strings.Contains(view, "problem") ||
		!strings.Contains(view, "repair not applied: "+f.target+": existing local file kept") || strings.Contains(view, "🩹") {
		t.Errorf("kept stale file must be reported as not repaired:\n%s", view)
	}
	if data, _ := os.ReadFile(f.target); string(data) != legacy {
		t.Errorf("local file changed without the replace toggle: %q", data)
	}

	replaced := press(f.model, "enter", "c", "r", "enter").(Model)
	view = replaced.View()
	if !strings.Contains(view, "🩹 repaired backup content: "+f.target) || !strings.Contains(view, "Finished without problems") {
		t.Errorf("authorized replacement must report the repair:\n%s", view)
	}
	if data, _ := os.ReadFile(f.target); strings.Contains(string(data), legacy) {
		t.Errorf("deployed config not repaired: %q", data)
	}
	if backups, _ := filepath.Glob(f.target + ".*.bk"); len(backups) != 1 {
		t.Errorf("safety backups = %v", backups)
	}
}

// With a backup received from another machine, the config toggle deploys that
// backup, like `dots restore --received`; without one it uses the repository.
func TestTUIConfigToggleUsesLastReceivedBackup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	incoming, err := transfer.IncomingDir()
	if err != nil {
		t.Fatal(err)
	}
	received := filepath.Join(incoming, "f4ca8131")
	os.MkdirAll(filepath.Join(received, "configs", "zsh"), 0755)
	os.WriteFile(filepath.Join(received, "manifest.yaml"), []byte("version: 1\n"), 0644)
	os.WriteFile(filepath.Join(received, "configs", "zsh", ".zshrc"), []byte("# received\n"), 0644)
	if err := transfer.RecordLatest(incoming, received); err != nil {
		t.Fatal(err)
	}
	f := newTUIFixture(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	f.model = NewModel(f.model.Manifest, f.model.SysInfo, f.model.RepoDir)
	f.model.Probe = func(string, manifest.AppConfig) bool { return false }
	f.model.Exec = func(req restore.ExecRequest) error { f.calls = append(f.calls, req.AppName); return nil }
	f.model.Suspend = f.suspend
	f.stdin = strings.NewReader("")

	preset := press(f.model, "enter", "c")
	if !strings.Contains(preset.View(), received) {
		t.Errorf("config toggle must name the received backup %s:\n%s", received, preset.View())
	}
	press(preset, "enter")
	if data, _ := os.ReadFile(f.target); string(data) != "# received\n" {
		t.Errorf("deployed %q, want the received backup", data)
	}
}
