package ui

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kevnnard/dots/pkg/backup"
	"github.com/kevnnard/dots/pkg/clean"
	"github.com/kevnnard/dots/pkg/detector"
	"github.com/kevnnard/dots/pkg/manifest"
	"github.com/kevnnard/dots/pkg/restore"
	"github.com/kevnnard/dots/pkg/status"
)

type currentView int

const (
	viewMain currentView = iota
	viewRestorePreset
	viewCustomApps
	viewStatus
	viewExecuting
	viewFinished
	viewCleanMode
	viewCleanApps
	viewCleanPreview
)

type MainOption int

const (
	OptRestore MainOption = iota
	OptBackup
	OptStatus
	OptQuit
)

type CheckItem struct {
	Name        string
	Description string
	Profile     string
	Checked     bool
}

type Model struct {
	Manifest    *manifest.Manifest
	SysInfo     *detector.SystemInfo
	RepoDir     string
	State       currentView
	PrevState   currentView
	Cursor      int
	SelectedOpt MainOption
	AppItems    []CheckItem
	Action      string // "restore" or "backup"
	Logs        []string
	Err         error
	Width       int
	Height      int
	// Restore settings shared with `dots restore`: WithConfig deploys the
	// backed-up configs, ReplaceExisting authorizes replacing differing local
	// files after a safety backup.
	WithConfig      bool
	ReplaceExisting bool
	// Probe and Exec detect installed apps and run install commands.
	Probe restore.InstalledProbe
	Exec  restore.Executor
	// Suspend hands the terminal to a blocking command and resumes the TUI
	// when it returns. It is tea.Exec; tests inject a fake terminal.
	Suspend func(tea.ExecCommand, tea.ExecCallback) tea.Cmd
	// LastRun is the result of the latest restore, for the summary view.
	LastRun *restore.RunResult
	// explicit marks a custom selection of named apps.
	explicit bool
	// CleanExec runs the cleaner's package queries and removals; CleanHome
	// and CleanPresence locate configs and presence signals. Tests inject
	// fakes so no package manager or real home is touched.
	CleanExec     clean.Executor
	CleanHome     func() (string, error)
	CleanPresence func(home string) func(string, manifest.AppConfig) bool
	clean         cleanState
	// cleanGen identifies the latest preview request, so a stale probe
	// result can never become the confirmed plan.
	cleanGen int
}

func NewModel(m *manifest.Manifest, sys *detector.SystemInfo, repoDir string) Model {
	var items []CheckItem
	for name, app := range m.Apps {
		prof := app.Profile
		if prof == "" {
			prof = "core"
		}
		items = append(items, CheckItem{
			Name:        name,
			Description: app.Description,
			Profile:     prof,
			Checked:     !app.Optional && app.MatchesProfile("core"), // default core checked unless optional
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })

	return Model{
		Manifest: m,
		SysInfo:  sys,
		RepoDir:  repoDir,
		State:    viewMain,
		AppItems: items,
		Cursor:   0,
		Probe:    restore.CommandProbe,
		Exec:     restore.ShellExecutor,
		Suspend:  tea.Exec,

		CleanExec:     clean.OSExecutor{},
		CleanHome:     os.UserHomeDir,
		CleanPresence: clean.PathPresence,
	}
}

// checkProfile checks the apps restore.Select picks for profile, the same
// selection `dots restore --profile` uses.
func (m *Model) checkProfile(profile string) {
	selected := map[string]bool{}
	for _, name := range restore.Select(m.Manifest, profile) {
		selected[name] = true
	}
	for i := range m.AppItems {
		m.AppItems[i].Checked = selected[m.AppItems[i].Name]
	}
	m.explicit = false
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case restoreDoneMsg:
		return m.finishRestore(msg), nil

	case cleanPreviewMsg:
		if msg.gen == m.cleanGen && m.State == viewCleanPreview {
			m.clean.preview, m.clean.err = msg.preview, msg.err
		}
		return m, nil

	case cleanDoneMsg:
		return m.finishClean(msg), nil

	case tea.KeyMsg:
		if m.State == viewExecuting {
			// Input is handed to the install commands until they finish.
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}

		switch m.State {
		case viewMain:
			return m.updateMain(msg)
		case viewRestorePreset:
			return m.updateRestorePreset(msg)
		case viewCustomApps:
			return m.updateCustomApps(msg)
		case viewStatus:
			return m.updateStatus(msg)
		case viewCleanMode:
			return m.updateCleanMode(msg)
		case viewCleanApps:
			return m.updateCleanApps(msg)
		case viewCleanPreview:
			return m.updateCleanPreview(msg)
		case viewFinished:
			if msg.String() == "enter" || msg.String() == "esc" || msg.String() == "q" {
				m.State = viewMain
				m.Cursor = 0
				m.Logs = nil
			}
			return m, nil
		}
	}

	return m, nil
}

func (m Model) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "down", "j":
		if m.Cursor < 4 {
			m.Cursor++
		}
	case "enter":
		switch m.Cursor {
		case 0: // Restore / Install
			m.Action = "restore"
			m.State = viewRestorePreset
			m.Cursor = 0
		case 1: // Backup
			m.Action = "backup"
			m.State = viewCustomApps
			// For backup, default all existing locally to checked
			for i := range m.AppItems {
				m.AppItems[i].Checked = true
			}
			m.Cursor = 0
		case 2: // Status & Drift
			m.State = viewStatus
		case 3: // Clean
			return m.openClean(), nil
		case 4: // Quit
			return m, tea.Quit
		}
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateRestorePreset(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "down", "j":
		if m.Cursor < 4 {
			m.Cursor++
		}
	case "c":
		m.WithConfig = !m.WithConfig
		if !m.WithConfig {
			m.ReplaceExisting = false
		}
	case "r":
		if m.WithConfig {
			m.ReplaceExisting = !m.ReplaceExisting
		}
	case "esc":
		m.State = viewMain
		m.Cursor = 0
	case "enter":
		switch m.Cursor {
		case 0: // Full Setup (All profiles, optional apps excluded)
			m.checkProfile("all")
			return m.executeAction()
		case 1: // Core Only
			m.checkProfile("core")
			return m.executeAction()
		case 2: // AI & Agents
			m.checkProfile("ai")
			return m.executeAction()
		case 3: // Mobile Dev
			m.checkProfile("mobile")
			return m.executeAction()
		case 4: // Custom Selection
			m.State = viewCustomApps
			m.Cursor = 0
			m.explicit = true
		}
	}
	return m, nil
}

func (m Model) updateCustomApps(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "down", "j":
		if m.Cursor < len(m.AppItems)-1 {
			m.Cursor++
		}
	case " ":
		m.AppItems[m.Cursor].Checked = !m.AppItems[m.Cursor].Checked
	case "a":
		// Toggle all
		allChecked := true
		for _, item := range m.AppItems {
			if !item.Checked {
				allChecked = false
				break
			}
		}
		for i := range m.AppItems {
			m.AppItems[i].Checked = !allChecked
		}
	case "esc":
		m.State = viewMain
		m.Cursor = 0
	case "enter":
		return m.executeAction()
	}
	return m, nil
}

func (m Model) updateStatus(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "enter", "b":
		m.State = viewMain
		m.Cursor = 0
	}
	return m, nil
}

func (m Model) executeAction() (tea.Model, tea.Cmd) {
	m.State = viewFinished
	m.Logs = nil

	if m.Action == "backup" {
		m.Logs = append(m.Logs, StyleSubtitle.Render("📦 Running Backup..."))
		totalCopied := 0
		for _, item := range m.AppItems {
			if !item.Checked {
				continue
			}
			app := m.Manifest.Apps[item.Name]
			res, err := backup.BackupApp(item.Name, app, m.RepoDir)
			if err != nil {
				m.Logs = append(m.Logs, StyleError.Render(fmt.Sprintf("✗ %s: %v", item.Name, err)))
				continue
			}
			for _, c := range res.Copied {
				m.Logs = append(m.Logs, StyleSuccess.Render(fmt.Sprintf("  ✓ Copied: %s", c)))
				totalCopied++
			}
			for _, s := range res.SecretsMasked {
				m.Logs = append(m.Logs, StyleWarning.Render(fmt.Sprintf("  🔒 Secret masked: %s", s)))
				totalCopied++
			}
		}
		m.Logs = append(m.Logs, "")
		m.Logs = append(m.Logs, StyleSuccess.Render(fmt.Sprintf("✨ Backup complete! Total items saved: %d", totalCopied)))

		// Create Git snapshot commit and push
		snap, snapErr := backup.CreateGitSnapshot(m.RepoDir, true)
		if snapErr == nil && snap.Committed {
			m.Logs = append(m.Logs, StyleSelected.Render(fmt.Sprintf("📸 Git snapshot committed: %s [%s]", snap.Message, snap.CommitHash)))
			if snap.Pushed {
				m.Logs = append(m.Logs, StyleSuccess.Render("🚀 Pushed snapshot to remote repository!"))
			} else if snap.Error != "" {
				m.Logs = append(m.Logs, StyleMuted.Render(fmt.Sprintf("ℹ️ Remote push: %s", snap.Error)))
			}
		}
		return m, nil
	}
	return m.startRestore()
}

// restoreDoneMsg reports a restore run that held the terminal.
type restoreDoneMsg struct {
	res *restore.RunResult
	err error
	ran bool // false when the terminal could not be handed off
}

// installRun is the tea.ExecCommand that runs the restore while Bubble Tea
// has released the terminal, so install output and sudo prompts use the real
// terminal instead of corrupting the alternate screen.
type installRun struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	run            func(stdin io.Reader, stdout, stderr io.Writer) (*restore.RunResult, error)
	res            *restore.RunResult
	ran            bool
}

func (c *installRun) SetStdin(r io.Reader)  { c.stdin = r }
func (c *installRun) SetStdout(w io.Writer) { c.stdout = w }
func (c *installRun) SetStderr(w io.Writer) { c.stderr = w }

func (c *installRun) Run() error {
	c.ran = true
	fmt.Fprintln(c.stdout, "\n🚀 dots: running install commands in this terminal; the TUI resumes when they finish.")
	res, err := c.run(c.stdin, c.stdout, c.stderr)
	c.res = res
	fmt.Fprintln(c.stdout, "dots: install commands finished, returning to the TUI.")
	return err
}

// startRestore hands the terminal to the same planner and runner as
// `dots restore -i`; the summary is rendered when the run returns.
func (m Model) startRestore() (tea.Model, tea.Cmd) {
	var selected []string
	for _, item := range m.AppItems {
		if item.Checked {
			selected = append(selected, item.Name)
		}
	}
	mf, sys, exe := m.Manifest, m.SysInfo, m.Exec
	opts := restore.RunOptions{
		RepoDir:         m.RepoDir,
		WithConfig:      m.WithConfig,
		ReplaceExisting: m.ReplaceExisting,
		InstallPackages: true,
		Explicit:        m.explicit,
		Probe:           m.Probe,
	}
	c := &installRun{run: func(stdin io.Reader, stdout, stderr io.Writer) (*restore.RunResult, error) {
		o := opts
		o.Stdin, o.Stdout, o.Stderr = stdin, stdout, stderr
		o.Exec = func(req restore.ExecRequest) error {
			fmt.Fprintf(stdout, "▶ [%s] %s\n", req.AppName, req.Command)
			return exe(req)
		}
		return restore.Run(mf, selected, sys, o)
	}}
	suspend := m.Suspend
	if suspend == nil {
		suspend = tea.Exec
	}
	m.State = viewExecuting
	m.LastRun = nil
	return m, suspend(c, func(err error) tea.Msg {
		return restoreDoneMsg{res: c.res, err: err, ran: c.ran}
	})
}

func (m Model) finishRestore(msg restoreDoneMsg) Model {
	m.State = viewFinished
	m.LastRun = msg.res
	m.Logs = []string{StyleSubtitle.Render("🚀 Restore & Provisioning")}
	if !msg.ran {
		m.Logs = append(m.Logs, StyleError.Render(fmt.Sprintf("✗ Could not hand the terminal to the install commands; nothing ran: %v", msg.err)))
		return m
	}
	if msg.res == nil {
		m.Logs = append(m.Logs, StyleError.Render(fmt.Sprintf("✗ Cannot plan restore: %v", msg.err)))
		return m
	}
	failed := len(msg.res.Problems()) > 0
	for _, line := range msg.res.Summary() {
		style := StyleNormal
		switch {
		case strings.HasPrefix(line, "❌"):
			style = StyleError
		case strings.HasPrefix(line, "✨"):
			style = StyleSuccess
		case failed && strings.HasPrefix(line, "  • "):
			style = StyleError
		case strings.HasPrefix(line, "    ✗"), strings.HasPrefix(line, "    ⏳"), strings.HasPrefix(line, "    ⊘"), strings.HasPrefix(line, "🔑 "):
			// Readiness actions: missing keys, pending logins, missing commands.
			style = StyleWarning
		}
		m.Logs = append(m.Logs, style.Render(line))
	}
	if msg.err != nil {
		// The run finished but Bubble Tea could not fully restore the terminal.
		m.Logs = append(m.Logs, StyleWarning.Render(fmt.Sprintf("⚠️ Terminal restore after install: %v", msg.err)))
	}
	return m
}

func (m Model) View() string {
	var s strings.Builder

	// 8-bit ASCII Banner & System Pills
	s.WriteString("\n")
	s.WriteString(StyleBanner.Render(AsciiBanner))
	s.WriteString("\n\n")

	subHeader := StyleSubtitle.Render("⚡️ WORKSTATION & DOTFILES MANAGER")
	osPill := StyleBadgeAccent.Render("os: " + m.SysInfo.OS)
	archPill := StyleBadge.Render("arch: " + m.SysInfo.Arch)
	pkgPill := StyleBadge.Render("pkg: " + string(m.SysInfo.PrimaryManager))
	repoPill := StyleMuted.Render(fmt.Sprintf("repo: %s", m.RepoDir))

	s.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, subHeader, "  ", osPill, archPill, pkgPill, repoPill))
	s.WriteString("\n\n")

	switch m.State {
	case viewMain:
		s.WriteString(StyleSubtitle.Render("Main Menu:") + "\n\n")
		options := []string{
			"🚀  Restore / Install Setup    (Deploy configs & install packages)",
			"📦  Backup Current Machine     (Snapshot local configs to repo)",
			"📊  Status & Drift Detection   (Inspect sync state vs local machine)",
			"🧹  Clean Up Apps              (Uninstall packages / quarantine configs, after a preview)",
			"❌  Quit",
		}
		for i, opt := range options {
			cursor := "  "
			if m.Cursor == i {
				cursor = StyleSelected.Render("❯ ")
				s.WriteString(cursor + StyleSelected.Render(opt) + "\n")
			} else {
				s.WriteString(cursor + StyleNormal.Render(opt) + "\n")
			}
		}
		s.WriteString("\n" + StyleMuted.Render("Navigate: ↑/↓ · Select: Enter · Quit: q"))

	case viewRestorePreset:
		s.WriteString(StyleSubtitle.Render("Select Installation Mode:") + "\n\n")
		presets := []string{
			"🌟  Full Setup       (All Profiles: Core + Mobile + Infra + AI; optional apps via Custom)",
			"⚡️  Core Only        (Workstation default: Shell, Neovim, Ghostty, Brew, Web)",
			"🤖  AI & Agents      (Pi, Claude Code, Codex, and Ollama local runtime)",
			"📱  Mobile Dev       (React Native, OpenJDK 17, CocoaPods, Fastlane)",
			"🛠️   Custom Selection (Pick and choose individual apps with checkboxes)",
		}
		for i, p := range presets {
			cursor := "  "
			if m.Cursor == i {
				cursor = StyleSelected.Render("❯ ")
				s.WriteString(cursor + StyleSelected.Render(p) + "\n")
			} else {
				s.WriteString(cursor + StyleNormal.Render(p) + "\n")
			}
		}
		s.WriteString("\n" + m.configToggles())
		s.WriteString("\n" + StyleMuted.Render("Navigate: ↑/↓ · Select: Enter · Configs: c · Replace local files: r · Back: Esc"))

	case viewCustomApps:
		modeName := "Restore"
		if m.Action == "backup" {
			modeName = "Backup"
		}
		s.WriteString(StyleSubtitle.Render(fmt.Sprintf("Select Apps to %s:", modeName)) + "\n\n")

		for i, item := range m.AppItems {
			cursor := "  "
			if m.Cursor == i {
				cursor = StyleSelected.Render("❯ ")
			}

			check := "[ ]"
			if item.Checked {
				check = StyleSuccess.Render("[✓]")
			}

			nameStr := fmt.Sprintf("%-12s", item.Name)
			profileBadge := StyleMuted.Render(fmt.Sprintf("(%s)", item.Profile))

			line := fmt.Sprintf("%s %s %s - %s %s", cursor, check, StyleNormal.Render(nameStr), StyleMuted.Render(item.Description), profileBadge)
			if m.Cursor == i {
				line = fmt.Sprintf("%s %s %s - %s %s", cursor, check, StyleSelected.Render(nameStr), StyleNormal.Render(item.Description), profileBadge)
			}
			s.WriteString(line + "\n")
		}

		s.WriteString("\n" + StyleMuted.Render("Navigate: ↑/↓ · Toggle: Space · Toggle All: a · Confirm: Enter · Back: Esc"))

	case viewStatus:
		s.WriteString(StyleSubtitle.Render("Synchronization & Drift Status:") + "\n\n")
		overall := status.CheckAll(m.Manifest, m.RepoDir)
		for name, st := range overall.Apps {
			badge := StyleSuccess.Render("[✓ Installed]")
			if !st.Installed {
				badge = StyleMuted.Render("[✗ Not installed]")
			}

			driftBadge := StyleSuccess.Render("● In Sync")
			if st.HasDrift {
				driftBadge = StyleWarning.Render("▲ Drift Detected")
			}

			s.WriteString(fmt.Sprintf("  %-12s %s  %s\n", StyleSelected.Render(name), badge, driftBadge))
			for _, p := range st.Paths {
				pState := StyleMuted.Render(string(p.State))
				if p.State == status.StateSynced {
					pState = StyleSuccess.Render("synced")
				} else if p.State == status.StateModified {
					pState = StyleWarning.Render("modified")
				}
				s.WriteString(fmt.Sprintf("     • %-35s -> %s\n", p.Src, pState))
			}
		}
		s.WriteString("\n" + StyleMuted.Render("Back: Esc / Enter / q"))

	case viewCleanMode, viewCleanApps, viewCleanPreview:
		s.WriteString(m.cleanView())

	case viewExecuting:
		if m.Action == "clean" {
			s.WriteString(StyleSubtitle.Render("Cleaning...") + "\n\n")
			s.WriteString(StyleNormal.Render("The terminal is handed to the confirmed cleanup (sudo may ask for your password).") + "\n")
			s.WriteString(StyleMuted.Render("The summary appears here when it finishes."))
			break
		}
		s.WriteString(StyleSubtitle.Render("Installing...") + "\n\n")
		s.WriteString(StyleNormal.Render("The terminal is handed to the install commands (sudo may ask for your password).") + "\n")
		s.WriteString(StyleMuted.Render("The summary appears here when they finish."))

	case viewFinished:
		for _, log := range m.Logs {
			s.WriteString(log + "\n")
		}
		s.WriteString("\n" + StyleMuted.Render("Press Enter or Esc to return to the Main Menu..."))
	}

	return StyleBox.Render(s.String()) + "\n"
}

// configToggles renders the restore settings shown on the preset screen.
func (m Model) configToggles() string {
	cfg := StyleMuted.Render("off — apps keep their stock configuration; local files are untouched")
	if m.WithConfig {
		cfg = StyleSelected.Render(fmt.Sprintf("on — deploy configs from %s; differing local files are kept", m.RepoDir))
	}
	replace := StyleMuted.Render("off — differing local files are kept")
	if m.ReplaceExisting {
		replace = StyleWarning.Render("on — differing local files are replaced after a .bk safety backup")
	}
	lines := "[c] Restore backed-up configs: " + cfg + "\n"
	if m.WithConfig {
		lines += "[r] Replace differing local files: " + replace + "\n"
	}
	return lines
}
