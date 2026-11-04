package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kevnnard/dots/pkg/clean"
)

// cleanState is the clean flow's selection. It is rebuilt every time the
// Clean entry is opened, so nothing carries over from an earlier run.
type cleanState struct {
	packages, configs bool // independent scopes, both off by default
	backups           bool // move older restore backups; off by default
	items             []CheckItem
	notice            string
	typed             string // the confirmation typed so far
	preview           *cleanPreview
	err               error
}

// cleanPreview is the single combined plan the user confirms. Execution runs
// this exact plan and only accepts the summaries shown here.
type cleanPreview struct {
	plan                     *clean.Plan
	pkgs                     clean.PackageRunner
	configs                  clean.ConfigRunner
	backups                  clean.BackupRunner
	pkgSummary, cfgSummary   string
	bkSummary                string
	removals, moves, skipped int
	bkMoves                  int
}

type cleanPreviewMsg struct {
	gen     int
	preview *cleanPreview
	err     error
}

type cleanDoneMsg struct {
	res cleanResult
	err error
	ran bool
}

// cleanResult is what the confirmed run did.
type cleanResult struct {
	quarantine   string
	bkQuarantine string
	moved        int
	items        []clean.ItemResult
	failures     []string
	err          error
}

const dryRunLine = "Dry run: nothing was changed.\n"

func (m Model) openClean() Model {
	m.clean = cleanState{}
	for _, name := range clean.Candidates(m.Manifest) {
		m.clean.items = append(m.clean.items, CheckItem{Name: name, Description: m.Manifest.Apps[name].Description})
	}
	m.State, m.Cursor = viewCleanMode, 0
	return m
}

func (m Model) updateCleanMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.clean.notice = ""
	switch msg.String() {
	case "up", "k":
		m.Cursor = 0
	case "down", "j":
		m.Cursor = 1
	case "p":
		m.clean.packages = !m.clean.packages
	case "c":
		m.clean.configs = !m.clean.configs
	case "b":
		m.clean.backups = !m.clean.backups
	case "esc":
		m.State, m.Cursor, m.clean = viewMain, 0, cleanState{}
	case "enter":
		switch {
		case !m.clean.packages && !m.clean.configs && !m.clean.backups:
			m.clean.notice = "nothing selected: turn on packages (p), configurations (c), old backups (b) or any combination"
		case m.Cursor == 0:
			return m.requestPreview(clean.Candidates(m.Manifest))
		default:
			m.State, m.Cursor = viewCleanApps, 0
		}
	}
	return m, nil
}

func (m Model) updateCleanApps(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.clean.notice = ""
	items := m.clean.items
	switch msg.String() {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "down", "j":
		if m.Cursor < len(items)-1 {
			m.Cursor++
		}
	case " ":
		if len(items) > 0 {
			items[m.Cursor].Checked = !items[m.Cursor].Checked
		}
	case "esc":
		m.State, m.Cursor = viewCleanMode, 1
	case "enter":
		var names []string
		for _, it := range items {
			if it.Checked {
				names = append(names, it.Name)
			}
		}
		if len(names) == 0 {
			m.clean.notice = "no app selected: toggle apps with Space"
			return m, nil
		}
		return m.requestPreview(names)
	}
	return m, nil
}

// requestPreview probes ownership off the update loop. Probe commands only
// query package databases; nothing changes before confirmation.
func (m Model) requestPreview(names []string) (tea.Model, tea.Cmd) {
	m.cleanGen++
	m.State, m.clean.preview, m.clean.err, m.clean.typed = viewCleanPreview, nil, nil, ""
	gen, mm := m.cleanGen, m
	return m, func() tea.Msg {
		p, err := mm.buildCleanPreview(names)
		return cleanPreviewMsg{gen: gen, preview: p, err: err}
	}
}

// buildCleanPreview builds the plan with the same planner, probe and runners
// as `dots clean`, and renders both scopes as dry runs.
func (m Model) buildCleanPreview(names []string) (*cleanPreview, error) {
	if m.CleanExec == nil || m.CleanHome == nil || m.CleanPresence == nil {
		return nil, errors.New("cleaner is not configured")
	}
	home, err := m.CleanHome()
	if err != nil {
		return nil, err
	}
	probe := clean.NativeProbe{Sys: m.SysInfo, Exec: m.CleanExec, Home: home, Present: m.CleanPresence(home)}
	var req clean.Request
	if m.clean.packages {
		req.Packages = names
	}
	if m.clean.configs {
		req.Configs = names
	}
	plan, err := clean.BuildPlan(m.Manifest, req, m.SysInfo, probe.Probe)
	if err != nil {
		return nil, err
	}
	p := &cleanPreview{plan: plan,
		pkgs:    clean.PackageRunner{Manifest: m.Manifest, Sys: m.SysInfo, Exec: m.CleanExec, Recheck: probe.Probe},
		configs: clean.ConfigRunner{Manifest: m.Manifest, Home: home},
		backups: clean.BackupRunner{Manifest: m.Manifest, Home: home, Apps: names}}
	if _, err := os.Stat(m.RepoDir); err == nil {
		p.configs.Repo = m.RepoDir // nothing inside the repository ever moves
		p.backups.Repo = m.RepoDir
	}
	ctx := context.Background()
	if m.clean.packages {
		var b bytes.Buffer
		r := p.pkgs
		r.DryRun, r.Out = true, &b
		if _, err := r.Run(ctx, plan); err != nil {
			return nil, err
		}
		p.pkgSummary = strings.TrimSuffix(b.String(), dryRunLine)
		for _, it := range plan.Packages {
			switch it.Status {
			case clean.PackageRemove:
				p.removals++
			case clean.PackageBlocked, clean.PackageUnsupported:
				p.skipped++
			}
		}
	}
	if m.clean.configs {
		var b bytes.Buffer
		r := p.configs
		r.DryRun, r.Out = true, &b
		rep, err := r.Run(ctx, plan)
		if err != nil {
			return nil, err
		}
		p.cfgSummary = strings.TrimSuffix(b.String(), dryRunLine)
		for _, it := range plan.Configs {
			if it.Status == clean.ConfigBlocked {
				p.skipped++
			}
		}
		for _, c := range rep.Candidates {
			switch c.Status {
			case clean.CandidateQuarantine:
				p.moves++
			case clean.CandidateBlocked:
				p.skipped++
			}
		}
	}
	if m.clean.backups {
		var b bytes.Buffer
		r := p.backups
		r.DryRun, r.Out = true, &b
		rep, err := r.Run(ctx)
		if err != nil {
			return nil, err
		}
		p.bkSummary = strings.TrimSuffix(b.String(), dryRunLine)
		for _, c := range rep.Candidates {
			switch c.Status {
			case clean.CandidateQuarantine:
				p.bkMoves++
			case clean.CandidateBlocked:
				p.skipped++
			}
		}
	}
	return p, nil
}

func (m Model) updateCleanPreview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.cleanGen++ // drop a probe still in flight
		m.State, m.Cursor, m.clean.preview, m.clean.typed = viewCleanMode, 0, nil, ""
	case tea.KeyBackspace:
		if n := len(m.clean.typed); n > 0 {
			m.clean.typed = m.clean.typed[:n-1]
		}
	case tea.KeyRunes:
		if len(m.clean.typed) < 8 {
			m.clean.typed += string(msg.Runes)
		}
	case tea.KeyEnter:
		p, typed := m.clean.preview, m.clean.typed
		m.clean.typed = ""
		if p != nil && p.removals+p.moves+p.bkMoves > 0 && typed == "yes" {
			return m.startClean(p)
		}
	}
	return m, nil
}

// cleanRun applies the confirmed plan while Bubble Tea has released the
// terminal, so sudo can prompt for a password.
type cleanRun struct {
	stdout io.Writer
	p      *cleanPreview
	res    cleanResult
	ran    bool
}

func (c *cleanRun) SetStdin(io.Reader)    {}
func (c *cleanRun) SetStdout(w io.Writer) { c.stdout = w }
func (c *cleanRun) SetStderr(io.Writer)   {}

func (c *cleanRun) Run() error {
	c.ran = true
	fmt.Fprintln(c.stdout, "\n🧹 dots: applying the confirmed cleanup in this terminal; sudo may ask for your password.")
	c.res = c.p.apply(context.Background(), c.stdout)
	fmt.Fprintln(c.stdout, "dots: cleanup finished, returning to the TUI.")
	return nil
}

// apply runs configurations and then old backups first, because their
// quarantine can be undone, and packages only when every confirmed file moved. Each runner accepts
// the confirmation only for exactly the summary that was previewed.
func (p *cleanPreview) apply(ctx context.Context, out io.Writer) cleanResult {
	var r cleanResult
	if p.moves > 0 {
		cfg := p.configs
		cfg.Out = out
		cfg.Confirm = func(s string) bool { return s == p.cfgSummary }
		rep, err := cfg.Run(ctx, p.plan)
		if rep != nil {
			r.quarantine, r.moved, r.failures = rep.Quarantine, len(rep.Moved), rep.Failures
		}
		switch {
		case err != nil:
			r.err = fmt.Errorf("%w; no package was removed", err)
			return r
		case rep.Cancelled || len(rep.Moved) != p.moves:
			r.err = errors.New("configurations changed after confirmation; nothing was changed and no package was removed")
			return r
		}
	}
	if p.bkMoves > 0 {
		bk := p.backups
		bk.Out = out
		bk.Confirm = func(s string) bool { return s == p.bkSummary }
		rep, err := bk.Run(ctx)
		if rep != nil {
			r.bkQuarantine, r.moved, r.failures = rep.Quarantine, r.moved+len(rep.Moved), append(r.failures, rep.Failures...)
		}
		switch {
		case err != nil:
			r.err = fmt.Errorf("%w; no package was removed", err)
			return r
		case rep.Cancelled || len(rep.Moved) != p.bkMoves:
			r.err = errors.New("old backups changed after confirmation; no backup was moved and no package was removed")
			return r
		}
	}
	if p.removals > 0 {
		pk := p.pkgs
		pk.Out = out
		pk.Confirm = func(s string) bool { return s == p.pkgSummary }
		rep, err := pk.Run(ctx, p.plan)
		if rep != nil {
			r.items = rep.Items
		}
		if err == nil && rep.Cancelled {
			err = errors.New("package plan changed after confirmation; no package was removed")
		}
		r.err = err
	}
	return r
}

func (m Model) startClean(p *cleanPreview) (tea.Model, tea.Cmd) {
	c := &cleanRun{p: p}
	suspend := m.Suspend
	if suspend == nil {
		suspend = tea.Exec
	}
	m.State, m.Action = viewExecuting, "clean"
	return m, suspend(c, func(err error) tea.Msg {
		return cleanDoneMsg{res: c.res, err: err, ran: c.ran}
	})
}

func (m Model) finishClean(msg cleanDoneMsg) Model {
	m.State, m.clean = viewFinished, cleanState{}
	m.Logs = []string{StyleSubtitle.Render("🧹 Cleanup")}
	log := func(style interface{ Render(...string) string }, s string) {
		m.Logs = append(m.Logs, style.Render(escape(s)))
	}
	if !msg.ran {
		log(StyleError, fmt.Sprintf("✗ Could not hand the terminal to the cleanup; nothing ran: %v", msg.err))
		return m
	}
	res := msg.res
	removed := 0
	for _, it := range res.items {
		line := "  " + string(it.Outcome) + " " + it.AppName
		if it.Detail != "" {
			line += ": " + it.Detail
		}
		if it.Outcome == clean.OutcomeRemoved {
			removed++
			log(StyleSuccess, line)
		} else {
			log(StyleError, line)
		}
	}
	for _, f := range res.failures {
		log(StyleError, "  failed "+f)
	}
	if res.err != nil {
		log(StyleError, "✗ "+res.err.Error())
	}
	for _, q := range []struct{ name, dir string }{{"configurations", res.quarantine}, {"old backups", res.bkQuarantine}} {
		switch {
		case q.dir == "" || res.moved == 0:
		case res.err != nil:
			log(StyleWarning, fmt.Sprintf("Moved %s stay quarantined; undo with: dots clean recover %s", q.name, q.dir))
		default:
			log(StyleSelected, fmt.Sprintf("Undo %s with: dots clean recover %s", q.name, q.dir))
		}
	}
	if res.err == nil {
		log(StyleSuccess, fmt.Sprintf("✨ Applied: %d package(s) removed, %d file(s) quarantined.", removed, res.moved))
	}
	if msg.err != nil {
		log(StyleWarning, fmt.Sprintf("⚠️ Terminal restore after cleanup: %v", msg.err))
	}
	return m
}

func (m Model) cleanView() string {
	var s strings.Builder
	scope := func(key, name, what string, on bool) {
		state := StyleMuted.Render(name + ": off")
		if on {
			state = StyleWarning.Render(name + ": on")
		}
		s.WriteString(fmt.Sprintf("[%s] %s %s\n", key, state, StyleMuted.Render("— "+what)))
	}
	switch m.State {
	case viewCleanMode:
		s.WriteString(StyleSubtitle.Render("Clean Up Apps:") + "\n\n")
		for i, opt := range []string{
			"🧹  All declared apps   (every manifest app, optional ones included; nothing else on this machine)",
			"🛠️   Custom Selection    (pick individual apps with checkboxes)",
		} {
			if m.Cursor == i {
				s.WriteString(StyleSelected.Render("❯ "+opt) + "\n")
			} else {
				s.WriteString("  " + StyleNormal.Render(opt) + "\n")
			}
		}
		s.WriteString("\n")
		scope("p", "Packages", "uninstall with the native package manager", m.clean.packages)
		scope("c", "Configurations", "move config files into a recoverable quarantine", m.clean.configs)
		scope("b", "Old backups", "quarantine older restore backups (*.bk); the newest of each file always stays", m.clean.backups)
		s.WriteString("\n" + StyleMuted.Render("Navigate: ↑/↓ · Packages: p · Configurations: c · Old backups: b · Preview: Enter · Back: Esc"))
	case viewCleanApps:
		s.WriteString(StyleSubtitle.Render("Select Apps to Clean:") + "\n\n")
		for i, it := range m.clean.items {
			cursor, check := "  ", "[ ]"
			if m.Cursor == i {
				cursor = StyleSelected.Render("❯ ")
			}
			if it.Checked {
				check = StyleWarning.Render("[✓]")
			}
			s.WriteString(fmt.Sprintf("%s %s %s\n", cursor, check, escape(it.Name)))
		}
		s.WriteString("\n" + StyleMuted.Render("Navigate: ↑/↓ · Toggle: Space · Preview: Enter · Back: Esc"))
	case viewCleanPreview:
		s.WriteString(m.previewView())
	}
	if m.clean.notice != "" {
		s.WriteString("\n" + StyleWarning.Render(m.clean.notice))
	}
	return s.String()
}

func (m Model) previewView() string {
	var s strings.Builder
	s.WriteString(StyleSubtitle.Render("Cleanup Preview:") + "\n\n")
	p := m.clean.preview
	switch {
	case m.clean.err != nil:
		return s.String() + StyleError.Render(escape("✗ Cannot plan cleanup: "+m.clean.err.Error())) +
			"\n\n" + StyleMuted.Render("Nothing was changed. Back: Esc")
	case p == nil:
		return s.String() + StyleMuted.Render("Checking package ownership (read-only)... Back: Esc")
	}
	for _, part := range []struct {
		on         bool
		name, text string
	}{
		{m.clean.packages, "Packages", p.pkgSummary},
		{m.clean.configs, "Configurations", p.cfgSummary},
		{m.clean.backups, "Old backups", p.bkSummary},
	} {
		if !part.on {
			s.WriteString(StyleMuted.Render(part.name+": not selected") + "\n")
			continue
		}
		s.WriteString(StyleNormal.Render(escape(part.text)) + "\n")
	}
	if p.removals+p.moves+p.bkMoves == 0 {
		s.WriteString("\n" + StyleWarning.Render(fmt.Sprintf("Nothing to change (%d item(s) blocked or unsupported). Back: Esc", p.skipped)))
		return s.String()
	}
	s.WriteString("\n" + StyleWarning.Render(fmt.Sprintf("Remove %d package(s) and quarantine %d file(s); %d item(s) stay untouched.", p.removals, p.moves+p.bkMoves, p.skipped)))
	s.WriteString("\n" + StyleSelected.Render("Type yes and press Enter to apply · Esc cancels: "+escape(m.clean.typed)))
	return s.String()
}

// escape replaces control and format characters, so manifest names or
// command errors can never drive the terminal.
func escape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '\n':
			b.WriteRune(r)
		case r == utf8.RuneError && size <= 1:
			fmt.Fprintf(&b, "\\x%02x", s[i])
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}
