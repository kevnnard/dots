package ui

import "github.com/charmbracelet/lipgloss"

var (
	// Brand colors
	ColorPrimary   = lipgloss.Color("#7D56F4") // Vibrant Purple
	ColorSecondary = lipgloss.Color("#00D7D7") // Neon Cyan
	ColorSuccess   = lipgloss.Color("#04B575") // Emerald Green
	ColorWarning   = lipgloss.Color("#FFB86C") // Amber / Gold
	ColorDanger    = lipgloss.Color("#FF5555") // Coral Red
	ColorMuted     = lipgloss.Color("#6272A4") // Slate Blue / Gray
	ColorBgDark    = lipgloss.Color("#1E1E2E") // Dark Charcoal
	ColorTextLight = lipgloss.Color("#F8F8F2")

	// Styles
	StyleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorTextLight).
			Background(ColorPrimary).
			Padding(0, 1).
			MarginRight(1)

	StyleSubtitle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true)

	StyleBadge = lipgloss.NewStyle().
			Foreground(ColorTextLight).
			Background(ColorBgDark).
			Padding(0, 1).
			MarginRight(1)

	StyleBadgeAccent = lipgloss.NewStyle().
				Foreground(ColorTextLight).
				Background(ColorSecondary).
				Bold(true).
				Padding(0, 1).
				MarginRight(1)

	StyleSelected = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true)

	StyleNormal = lipgloss.NewStyle().
			Foreground(ColorTextLight)

	StyleMuted = lipgloss.NewStyle().
			Foreground(ColorMuted)

	StyleSuccess = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true)

	StyleWarning = lipgloss.NewStyle().
			Foreground(ColorWarning)

	StyleError = lipgloss.NewStyle().
			Foreground(ColorDanger).
			Bold(true)

	StyleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary).
			Padding(1, 2)
)
