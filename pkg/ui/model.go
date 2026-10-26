package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kevnnard/dots/pkg/backup"
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

	return Model{
		Manifest: m,
		SysInfo:  sys,
		RepoDir:  repoDir,
		State:    viewMain,
		AppItems: items,
		Cursor:   0,
	}
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

	case tea.KeyMsg:
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
		if m.Cursor < 3 {
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
		case 3: // Quit
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
	case "esc":
		m.State = viewMain
		m.Cursor = 0
	case "enter":
		switch m.Cursor {
		case 0: // Full Setup (All profiles)
			for i := range m.AppItems {
				app := m.Manifest.Apps[m.AppItems[i].Name]
				m.AppItems[i].Checked = !app.Optional
			}
			return m.executeAction()
		case 1: // Core Only
			for i := range m.AppItems {
				app := m.Manifest.Apps[m.AppItems[i].Name]
				m.AppItems[i].Checked = app.MatchesProfile("core") && !app.Optional
			}
			return m.executeAction()
		case 2: // AI & Agents
			for i := range m.AppItems {
				app := m.Manifest.Apps[m.AppItems[i].Name]
				m.AppItems[i].Checked = app.MatchesProfile("ai")
			}
			return m.executeAction()
		case 3: // Mobile Dev
			for i := range m.AppItems {
				app := m.Manifest.Apps[m.AppItems[i].Name]
				m.AppItems[i].Checked = app.MatchesProfile("mobile")
			}
			return m.executeAction()
		case 4: // Custom Selection
			m.State = viewCustomApps
			m.Cursor = 0
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
	} else {
		// Restore
		m.Logs = append(m.Logs, StyleSubtitle.Render("🚀 Running Restore & Provisioning..."))
		for _, item := range m.AppItems {
			if !item.Checked {
				continue
			}
			app := m.Manifest.Apps[item.Name]
			plan := restore.PlanApp(item.Name, app, m.SysInfo)
			if plan.InstallCmd != "" {
				m.Logs = append(m.Logs, StyleSelected.Render(fmt.Sprintf("📦 [%s] Installing: %s", item.Name, plan.InstallCmd)))
				if err := restore.ExecuteInstallCommand(plan.InstallCmd); err != nil {
					m.Logs = append(m.Logs, StyleError.Render(fmt.Sprintf("  ⚠️ Install failed: %v", err)))
				}
			}
			res, err := restore.ApplyAppFiles(item.Name, app, m.RepoDir, false)
			if err != nil {
				m.Logs = append(m.Logs, StyleError.Render(fmt.Sprintf("✗ %s restore: %v", item.Name, err)))
				continue
			}
			for _, bk := range res.LocalBackups {
				m.Logs = append(m.Logs, StyleWarning.Render(fmt.Sprintf("  🛡️ Safety backup: %s", bk)))
			}
			for _, r := range res.Restored {
				m.Logs = append(m.Logs, StyleSuccess.Render(fmt.Sprintf("  ✓ Restored: %s", r)))
			}
			for _, t := range res.TemplatesInit {
				m.Logs = append(m.Logs, StyleWarning.Render(fmt.Sprintf("  🔒 Template created: %s (add real secrets!)", t)))
			}
		}
		m.Logs = append(m.Logs, "")
		m.Logs = append(m.Logs, StyleSuccess.Render("✨ Restore completed successfully!"))
	}

	return m, nil
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
			"🌟  Full Setup       (All Profiles: Core + Mobile + Infra + AI)",
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
		s.WriteString("\n" + StyleMuted.Render("Navigate: ↑/↓ · Select: Enter · Back: Esc"))

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

	case viewFinished:
		for _, log := range m.Logs {
			s.WriteString(log + "\n")
		}
		s.WriteString("\n" + StyleMuted.Render("Press Enter or Esc to return to the Main Menu..."))
	}

	return StyleBox.Render(s.String()) + "\n"
}
