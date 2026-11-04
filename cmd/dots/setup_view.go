package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/kevnnard/dots/pkg/setup"
	"github.com/kevnnard/dots/pkg/ui"
)

// statusGlyph maps each step outcome to the glyph the checklist shows.
var statusGlyph = map[setup.Status]string{
	setup.StatusDone:   "✓",
	setup.StatusFailed: "✗",
	setup.StatusManual: "◇",
}

// renderChecklist draws the terminal checklist: a header with progress, one
// line per step, and the counts line. Only verified (done) steps count as progress;
// manual steps are checked by the user, so dots does not count them.
func renderChecklist(results []setup.Result, total int) string {
	counts := map[setup.Status]int{}
	for _, r := range results {
		counts[r.Status]++
	}
	var b strings.Builder
	b.WriteString(ui.StyleTitle.Render(fmt.Sprintf("dots setup · %d/%d", counts[setup.StatusDone], total)))
	b.WriteString("\n")
	for _, r := range results {
		b.WriteString(styleFor(r.Status).Render(statusGlyph[r.Status] + " " + r.Title))
		b.WriteString(ui.StyleMuted.Render("  " + r.Detail))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("📊 Done: %d · Manual: %d · Failed: %d\n", counts[setup.StatusDone], counts[setup.StatusManual], counts[setup.StatusFailed]))
	return b.String()
}

// renderShellChoice shows both shells, marking the selected one.
func renderShellChoice(selected string) string {
	mark := func(name string) string {
		if name == selected {
			return ui.StyleSelected.Render("● " + name)
		}
		return ui.StyleMuted.Render("○ " + name)
	}
	return mark("zsh") + "   " + mark("bash")
}

func styleFor(s setup.Status) lipgloss.Style {
	switch s {
	case setup.StatusDone:
		return ui.StyleSuccess
	case setup.StatusFailed:
		return ui.StyleError
	default:
		return ui.StyleWarning
	}
}
