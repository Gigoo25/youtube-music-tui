package tui

import "github.com/charmbracelet/lipgloss"

// ─── Icons (exact glyphs from the reference interface) ─────────────────────────

const (
	iconPlay      = "▶"
	iconPause     = "‖"
	iconNext      = "▶|"
	iconPrev      = "|◀"
	iconShuffle   = "⇄"
	iconRepeatAll = "↻"
	iconRepeatOne = "↺"
	iconPlaylist  = "☰"
	iconSearch    = "/"
	iconHelp      = "?"
	iconDownload  = "↓"
	iconQuit      = "×"
	iconVolume    = "♪"
	iconHeart     = "♥"
	iconAutoplay  = "∞"
)

// ─── Color theme (dark) ────────────────────────────────────────────────────────

var (
	colorPrimary    = lipgloss.Color("#22D3EE") // cyan
	colorSecondary  = lipgloss.Color("#3B82F6") // blue
	colorText       = lipgloss.Color("#FFFFFF") // white
	colorAccent     = lipgloss.Color("#EAB308") // yellow
	colorDim        = lipgloss.Color("#6B7280") // gray
	colorError      = lipgloss.Color("#EF4444") // red
	colorSuccess    = lipgloss.Color("#22C55E") // green
	colorWarning    = lipgloss.Color("#EAB308") // yellow
	colorBackground = lipgloss.Color("#000000") // black
)

// ─── Base text styles ──────────────────────────────────────────────────────────

var (
	stylePrimary   = lipgloss.NewStyle().Foreground(colorPrimary)
	styleSecondary = lipgloss.NewStyle().Foreground(colorSecondary)
	styleText      = lipgloss.NewStyle().Foreground(colorText)
	styleAccent    = lipgloss.NewStyle().Foreground(colorAccent)
	styleDim       = lipgloss.NewStyle().Foreground(colorDim)
	styleError     = lipgloss.NewStyle().Foreground(colorError)
	styleSuccess   = lipgloss.NewStyle().Foreground(colorSuccess)

	stylePrimaryBold   = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	styleSecondaryBold = lipgloss.NewStyle().Foreground(colorSecondary).Bold(true)

	styleHeart = lipgloss.NewStyle().Foreground(colorError)

	// Selected list row: primary background, black foreground.
	styleSelected = lipgloss.NewStyle().
			Foreground(colorBackground).
			Background(colorPrimary).
			Bold(true)
)

// ─── Border / box styles ───────────────────────────────────────────────────────

var (
	// Outer app shell: single border in primary color.
	styleShell = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorPrimary)

	// Sidebar (Quick Links) box: rounded border in dim.
	styleSidebarBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorDim).
			Padding(0, 1)

	// Search bar box: single border in secondary, horizontal padding.
	styleSearchBox = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorSecondary).
			Padding(0, 1)

	// Shortcuts bar: single border in dim.
	styleShortcutsBox = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(colorDim)

	// Generic bordered content box (help screen etc.).
	styleContentBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary).
			Padding(0, 1)
)
