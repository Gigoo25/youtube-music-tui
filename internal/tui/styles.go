package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorRed      = lipgloss.Color("#FF3333")
	colorMuted    = lipgloss.Color("#666666")
	colorSelected = lipgloss.Color("#FFFFFF")
	colorAccent   = lipgloss.Color("#FF6666")
	colorGreen    = lipgloss.Color("#44DD88")
	colorText     = lipgloss.Color("#CCCCCC")
	colorBorder   = lipgloss.Color("#444444")

	styleTitle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	styleSubtitle = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleSelected = lipgloss.NewStyle().
			Foreground(colorSelected).
			Background(lipgloss.Color("#2A2A2A")).
			Bold(true)

	styleNormal = lipgloss.NewStyle().
			Foreground(colorText)

	styleFavorite = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6B6B"))

	styleHelp = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleStatus = lipgloss.NewStyle().
			Foreground(colorGreen)

	styleError = lipgloss.NewStyle().
			Foreground(colorAccent)

	styleProgressFull = lipgloss.NewStyle().
				Foreground(colorRed)

	styleProgressEmpty = lipgloss.NewStyle().
				Foreground(colorBorder)

	styleNowPlaying = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)
)
