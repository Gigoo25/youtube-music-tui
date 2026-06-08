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
	iconPlaylist  = "♫"
	iconSearch    = "/"
	iconHelp      = "?"
	iconDownload  = "↓"
	iconQuit      = "×"
	iconVolume    = "♪"
	iconHeart     = "♥"
	iconAutoplay  = "∞"
)

// ─── Themes ────────────────────────────────────────────────────────────────────

// theme is a named color palette. The active one is applied with applyTheme,
// which rebuilds every style below from it.
type theme struct {
	name      string
	primary   string // main accent: headers, selection bg, borders, progress, [keys]
	secondary string
	text      string
	accent    string // filter line / loading
	dim       string
	errc      string
	success   string
	warning   string
	bg        string // text color drawn on top of the primary highlight
}

// themes are the built-in palettes, in cycle order. The first is the default.
var themes = []theme{
	{
		name: "everforest", primary: "#A7C080", secondary: "#7FBBB3", text: "#D3C6AA",
		accent: "#DBBC7F", dim: "#859289", errc: "#E67E80", success: "#83C092",
		warning: "#DBBC7F", bg: "#2D353B",
	},
	{
		name: "tokyo-night", primary: "#7AA2F7", secondary: "#BB9AF7", text: "#C0CAF5",
		accent: "#E0AF68", dim: "#565F89", errc: "#F7768E", success: "#9ECE6A",
		warning: "#E0AF68", bg: "#1A1B26",
	},
	{
		name: "nord", primary: "#88C0D0", secondary: "#81A1C1", text: "#ECEFF4",
		accent: "#EBCB8B", dim: "#4C566A", errc: "#BF616A", success: "#A3BE8C",
		warning: "#EBCB8B", bg: "#2E3440",
	},
	{
		name: "gruvbox", primary: "#83A598", secondary: "#FABD2F", text: "#EBDBB2",
		accent: "#FE8019", dim: "#928374", errc: "#FB4934", success: "#B8BB26",
		warning: "#FABD2F", bg: "#282828",
	},
	{
		name: "catppuccin", primary: "#89B4FA", secondary: "#CBA6F7", text: "#CDD6F4",
		accent: "#F9E2AF", dim: "#6C7086", errc: "#F38BA8", success: "#A6E3A1",
		warning: "#FAB387", bg: "#1E1E2E",
	},
	{
		name: "dracula", primary: "#BD93F9", secondary: "#8BE9FD", text: "#F8F8F2",
		accent: "#F1FA8C", dim: "#6272A4", errc: "#FF5555", success: "#50FA7B",
		warning: "#FFB86C", bg: "#282A36",
	},
}

// themeIndex returns the index of the named theme, or -1.
func themeIndex(name string) int {
	for i, t := range themes {
		if t.name == name {
			return i
		}
	}
	return -1
}

// ─── Active palette + styles (assigned by applyTheme) ───────────────────────────

var (
	colorPrimary    lipgloss.Color
	colorSecondary  lipgloss.Color
	colorText       lipgloss.Color
	colorAccent     lipgloss.Color
	colorDim        lipgloss.Color
	colorError      lipgloss.Color
	colorSuccess    lipgloss.Color
	colorWarning    lipgloss.Color
	colorBackground lipgloss.Color
)

var (
	stylePrimary       lipgloss.Style
	styleSecondary     lipgloss.Style
	styleText          lipgloss.Style
	styleAccent        lipgloss.Style
	styleDim           lipgloss.Style
	styleError         lipgloss.Style
	styleSuccess       lipgloss.Style
	stylePrimaryBold   lipgloss.Style
	styleSecondaryBold lipgloss.Style
	styleHeart         lipgloss.Style
	styleSelected      lipgloss.Style
	styleShell         lipgloss.Style
	styleSidebarBox    lipgloss.Style
	styleSearchBox     lipgloss.Style
	styleShortcutsBox  lipgloss.Style
	styleContentBox    lipgloss.Style
)

func init() { applyTheme(themes[0]) }

// applyTheme sets the active palette and rebuilds every style from it.
func applyTheme(t theme) {
	colorPrimary = lipgloss.Color(t.primary)
	colorSecondary = lipgloss.Color(t.secondary)
	colorText = lipgloss.Color(t.text)
	colorAccent = lipgloss.Color(t.accent)
	colorDim = lipgloss.Color(t.dim)
	colorError = lipgloss.Color(t.errc)
	colorSuccess = lipgloss.Color(t.success)
	colorWarning = lipgloss.Color(t.warning)
	colorBackground = lipgloss.Color(t.bg)

	stylePrimary = lipgloss.NewStyle().Foreground(colorPrimary)
	styleSecondary = lipgloss.NewStyle().Foreground(colorSecondary)
	styleText = lipgloss.NewStyle().Foreground(colorText)
	styleAccent = lipgloss.NewStyle().Foreground(colorAccent)
	styleDim = lipgloss.NewStyle().Foreground(colorDim)
	styleError = lipgloss.NewStyle().Foreground(colorError)
	styleSuccess = lipgloss.NewStyle().Foreground(colorSuccess)
	stylePrimaryBold = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	styleSecondaryBold = lipgloss.NewStyle().Foreground(colorSecondary).Bold(true)
	styleHeart = lipgloss.NewStyle().Foreground(colorError)
	// Selected list row: primary background, bg-colored foreground.
	styleSelected = lipgloss.NewStyle().
		Foreground(colorBackground).
		Background(colorPrimary).
		Bold(true)

	styleShell = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(colorPrimary)
	styleSidebarBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorDim).
		Padding(0, 1)
	styleSearchBox = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(colorSecondary).
		Padding(0, 1)
	styleShortcutsBox = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(colorDim)
	styleContentBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(0, 1)
}
