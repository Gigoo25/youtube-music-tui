package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorRed    = lipgloss.Color("#FF3333")
	colorMuted  = lipgloss.Color("#666666")
	colorWhite  = lipgloss.Color("#FFFFFF")
	colorGreen  = lipgloss.Color("#44DD88")
	colorPink   = lipgloss.Color("#FF6B6B")
	colorText   = lipgloss.Color("#CCCCCC")
	colorArtist = lipgloss.Color("#888888")
	colorSelBg  = lipgloss.Color("#222222")

	styleTitle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	styleSubtitle = lipgloss.NewStyle().
			Foreground(colorArtist)

	styleSelected = lipgloss.NewStyle().
			Foreground(colorWhite).
			Background(colorSelBg).
			Bold(true)

	styleNormal = lipgloss.NewStyle().
			Foreground(colorText)

	styleFavorite = lipgloss.NewStyle().
			Foreground(colorPink)

	styleHelp = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleStatus = lipgloss.NewStyle().
			Foreground(colorGreen)

	styleError = lipgloss.NewStyle().
			Foreground(colorPink)

	styleProgressFull = lipgloss.NewStyle().
				Foreground(colorRed)

	styleProgressEmpty = lipgloss.NewStyle().
				Foreground(colorMuted)

	styleNowPlaying = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)
)
