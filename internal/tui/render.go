package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rob/ytmusic/internal/api"
)

// ─── Helpers ───────────────────────────────────────────────────────────────────

// truncate shortens s to max display runes, appending an ellipsis when cut.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}

// fmtDur formats seconds as M:SS.
func fmtDur(secs float64) string {
	if secs < 0 {
		secs = 0
	}
	s := int(secs)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// padToHeight pads s with blank lines (or truncates) so it occupies exactly
// n terminal rows. Used to push the bottom bars flush against the frame.
func padToHeight(s string, n int) string {
	if n < 1 {
		n = 1
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// ─── Top-level View ────────────────────────────────────────────────────────────

func (m *model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	// Inner width available inside the outer shell border (2 cols for border).
	innerW := m.width - 2
	if innerW < 1 {
		innerW = 1
	}

	shortcuts := m.renderShortcutsBar(innerW)
	status := m.renderStatusLine(innerW)

	// Height budget: outer border (2) + shortcuts + status.
	used := 2 + lipgloss.Height(shortcuts)
	if status != "" {
		used += lipgloss.Height(status)
	}
	contentH := m.height - used
	if contentH < 1 {
		contentH = 1
	}

	// Pad content to the full height budget so the shortcuts bar sits flush
	// at the bottom of the terminal instead of floating up under the content.
	content := padToHeight(m.renderView(innerW, contentH), contentH)

	var inner string
	if status != "" {
		inner = lipgloss.JoinVertical(lipgloss.Left, content, status, shortcuts)
	} else {
		inner = lipgloss.JoinVertical(lipgloss.Left, content, shortcuts)
	}

	// Pin the shell to the full terminal height (border consumes 2 rows).
	return styleShell.Width(innerW).Height(m.height - 2).Render(inner)
}

func (m *model) renderView(w, h int) string {
	switch m.activeView {
	case viewHome:
		return m.renderHome(w, h)
	case viewSearch:
		return m.renderSearch(w, h)
	case viewQueue:
		return m.renderQueue(w, h)
	case viewFavorites:
		return m.renderFavorites(w, h)
	case viewHistory:
		return m.renderHistory(w, h)
	case viewTrending, viewNewReleases, viewExplore:
		return m.renderBrowse(m.activeView, w, h)
	case viewHelp:
		return m.renderHelp(w, h)
	}
	return ""
}

// renderBrowse renders an async browse view (Trending / New Releases / Explore).
func (m *model) renderBrowse(v view, w, h int) string {
	bs, ok := m.browse[v]
	if !ok {
		return ""
	}
	heading := stylePrimaryBold.Render(bs.title)

	switch {
	case bs.loading || !bs.loaded:
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleAccent.Render("Loading..."))
	case bs.err != "":
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleError.Render(bs.err))
	case len(bs.tracks) == 0:
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleDim.Render("Nothing here yet."))
	}

	footer := styleDim.Render("j/k navigate • Enter queue • p play-now • g refresh")
	listH := h - lipgloss.Height(heading) - lipgloss.Height(footer)
	if listH < 1 {
		listH = 1
	}
	start, end := windowBounds(bs.cursor, len(bs.tracks), listH)
	var rows []string
	for i := start; i < end; i++ {
		rows = append(rows, m.renderResultRow(i+1, bs.tracks[i], i == bs.cursor, w, false))
	}
	return lipgloss.JoinVertical(lipgloss.Left, heading, strings.Join(rows, "\n"), footer)
}

// ─── Status line ───────────────────────────────────────────────────────────────

func (m *model) renderStatusLine(w int) string {
	if m.status == "" {
		return ""
	}
	st := styleSuccess
	if m.statusErr {
		st = styleError
	}
	return st.Width(w).Render(truncate(m.status, w))
}

// ─── Shortcuts bar ─────────────────────────────────────────────────────────────

func (m *model) renderShortcutsBar(w int) string {
	playing := !m.playerState.Paused
	playGlyph := iconPlay
	if playing {
		playGlyph = iconPause
	}

	shuffleStyle := styleDim
	if m.shuffle {
		shuffleStyle = stylePrimary
	}

	repeatGlyph := iconRepeatAll
	if m.repeat == repeatOne {
		repeatGlyph = iconRepeatOne
	}
	repeatStyle := styleDim
	if m.repeat != repeatOff {
		repeatStyle = styleSecondary
	}

	sep := styleDim.Render(" • ")
	left := stylePrimary.Render(playGlyph) + styleDim.Render(" [space]") + sep +
		stylePrimary.Render(iconPrev) + styleDim.Render(" [b]") + sep +
		stylePrimary.Render(iconNext) + styleDim.Render(" [n]") + sep +
		shuffleStyle.Render(iconShuffle) + styleDim.Render(" [s]") + sep +
		repeatStyle.Render(repeatGlyph) + styleDim.Render(" [r]") + sep +
		stylePrimary.Render(iconSearch) + styleDim.Render(" [/]") + sep +
		stylePrimary.Render(iconHelp) + styleDim.Render(" [?]")

	vol := int(m.playerState.Volume)
	right := shuffleStyle.Render(iconShuffle) + " " +
		repeatStyle.Render(repeatGlyph) + " " +
		styleDim.Render(iconAutoplay) + "   " +
		stylePrimary.Render(iconVolume) + styleDim.Render(" [+/-] ") +
		stylePrimary.Render(fmt.Sprintf("%d%%", vol))

	inner := w - 2 // shortcuts box has a single border
	if inner < 1 {
		inner = 1
	}
	gap := inner - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right

	return styleShortcutsBox.Width(inner).Render(line)
}

// ─── NowPlaying box ────────────────────────────────────────────────────────────

func (m *model) renderNowPlaying(w int) string {
	inner := w - 4 // rounded border (2) + padding (2)
	if inner < 1 {
		inner = 1
	}

	if m.current == nil {
		return styleDimBox.Width(inner).Render(styleDim.Render("No track playing"))
	}

	t := m.current
	s := m.playerState

	// Line 1: title (heart prefix if fav) • artists
	titlePrefix := ""
	if m.cfg.IsFavorite(t.ID) {
		titlePrefix = styleHeart.Render(iconHeart) + " "
	}
	line1 := titlePrefix +
		stylePrimaryBold.Render(t.Title) +
		styleDim.Render(" • ") +
		styleSecondary.Render(t.Artist)

	lines := []string{line1}

	// Line 2: album
	if t.Album != "" {
		lines = append(lines, styleDim.Render(t.Album))
	}

	// Line 3: progress bar
	barWidth := max(10, m.width-8)
	if barWidth > inner {
		barWidth = inner
	}
	filled := 0
	if s.Duration > 0 {
		filled = int(s.Position / s.Duration * float64(barWidth))
	}
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}
	bar := stylePrimary.Render(strings.Repeat("█", filled)) +
		styleDim.Render(strings.Repeat("░", barWidth-filled))
	lines = append(lines, bar)

	// Line 4: time + flags
	pct := 0
	if s.Duration > 0 {
		pct = int(s.Position / s.Duration * 100)
	}
	line4 := styleText.Render(fmtDur(s.Position)) +
		styleDim.Render(" / "+fmtDur(s.Duration)+" ") +
		styleDim.Render(fmt.Sprintf("[%d%%]", pct))
	if s.Loading {
		line4 += styleAccent.Render(" Loading...")
	}
	if s.Paused && s.Position > 0 {
		line4 += styleDim.Render(" " + iconPause)
	}
	if m.shuffle {
		line4 += stylePrimary.Render(" " + iconShuffle)
	}
	lines = append(lines, line4)

	return styleNowPlayingBox.Width(inner).Render(strings.Join(lines, "\n"))
}

// ─── Home screen ───────────────────────────────────────────────────────────────

func (m *model) renderHome(w, h int) string {
	headerInner := w - 2 // double border
	if headerInner < 1 {
		headerInner = 1
	}
	header := styleHeaderBox.Width(headerInner).Render(
		stylePrimaryBold.Render("🎵 "+iconPlay+" youtube-music-cli "+iconPlay+" 🎵"),
	)

	np := m.renderNowPlaying(w)

	// Quick Links menu
	entries := []string{"Search", "Queue", "Favorites", "Help"}
	var rows []string
	rows = append(rows, styleSecondaryBold.Render("Quick Links"))
	for i, e := range entries {
		if i == m.homeCursor {
			rows = append(rows, styleSelected.Render("> "+e))
		} else {
			rows = append(rows, "  "+styleText.Render(e))
		}
	}
	menuInner := w - 4 // rounded border + padding
	if menuInner < 1 {
		menuInner = 1
	}
	menu := styleMenuBox.Width(menuInner).Render(strings.Join(rows, "\n"))

	return lipgloss.JoinVertical(lipgloss.Left, header, np, menu)
}

// ─── Search screen ─────────────────────────────────────────────────────────────

func (m *model) renderSearch(w, h int) string {
	var blocks []string
	used := 0

	// One-line now-playing indicator
	if m.current != nil {
		glyph := iconPlay
		if m.playerState.Paused {
			glyph = iconPause
		}
		line := styleDim.Render(glyph+" ") +
			stylePrimaryBold.Render(m.current.Title) +
			styleSecondary.Render(" • "+m.current.Artist)
		blocks = append(blocks, truncate2(line, w))
		used++
	}

	// Search bar
	searchInner := w - 4 // single border + padding
	if searchInner < 1 {
		searchInner = 1
	}
	var barContent string
	if m.searchTyping {
		barContent = stylePrimary.Render("Search: ") + m.searchInput.View()
	} else {
		barContent = stylePrimary.Render("Search: ") + styleDim.Render("type / to search...")
	}
	bar := styleSearchBox.Width(searchInner).Render(barContent)
	blocks = append(blocks, bar)
	used += lipgloss.Height(bar)

	if m.searching {
		blocks = append(blocks, styleAccent.Render("Searching..."))
		used++
	}

	// Footer hint
	var hint string
	if m.searchTyping {
		hint = "Type to search, Enter to start, Esc to clear"
	} else {
		hint = "j/k navigate, Enter play, p play-now, f favorite, / search again"
	}
	footer := styleDim.Render(truncate(hint, w))

	// Results list, clamped to remaining height
	listH := h - used - lipgloss.Height(footer)
	if listH < 1 {
		listH = 1
	}
	list := m.renderResultList(w, listH)

	blocks = append(blocks, list, footer)
	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}

func (m *model) renderResultList(w, h int) string {
	if len(m.searchResults) == 0 {
		return styleDim.Render("No results.")
	}
	start, end := windowBounds(m.searchCursor, len(m.searchResults), h)
	var rows []string
	for i := start; i < end; i++ {
		t := m.searchResults[i]
		selected := i == m.searchCursor
		rows = append(rows, m.renderResultRow(i+1, t, selected, w, false))
	}
	return strings.Join(rows, "\n")
}

// renderResultRow renders a numbered track row with index, optional heart, title,
// artist, and right-aligned duration.
func (m *model) renderResultRow(n int, t api.Track, selected bool, w int, forceHeart bool) string {
	idx := styleDim.Render(fmt.Sprintf("%d. ", n))

	heart := ""
	if forceHeart || m.cfg.IsFavorite(t.ID) {
		heart = styleHeart.Render(iconHeart) + " "
	}

	dur := t.Duration
	durStr := styleDim.Render(dur)

	titleStyle := styleText
	if selected {
		titleStyle = stylePrimaryBold
	}

	left := idx + heart + titleStyle.Render(t.Title) + styleDim.Render(" • "+t.Artist)

	gap := w - lipgloss.Width(left) - lipgloss.Width(durStr)
	if gap < 1 {
		// truncate the left side to make room
		avail := w - lipgloss.Width(durStr) - 1
		left = truncate2(left, avail)
		gap = w - lipgloss.Width(left) - lipgloss.Width(durStr)
		if gap < 1 {
			gap = 1
		}
	}
	return left + strings.Repeat(" ", gap) + durStr
}

// ─── Queue screen ──────────────────────────────────────────────────────────────

func (m *model) renderQueue(w, h int) string {
	if len(m.queue) == 0 {
		return styleDim.Render("Queue is empty — search and press Enter to add.")
	}

	header := styleDim.Render(fmt.Sprintf("Up next (%d tracks)", len(m.queue)))
	listH := h - lipgloss.Height(header)
	if listH < 1 {
		listH = 1
	}

	start, end := windowBounds(m.queueCursor, len(m.queue), listH)
	var rows []string
	for i := start; i < end; i++ {
		t := m.queue[i]
		selected := i == m.queueCursor
		idx := styleDim.Render(fmt.Sprintf("%d. ", i+1))

		marker := ""
		if i == m.queuePos {
			marker = stylePrimary.Render(iconPlay + " ")
		}

		heart := ""
		if m.cfg.IsFavorite(t.ID) {
			heart = styleHeart.Render(iconHeart) + " "
		}

		titleStyle := styleText
		if selected {
			titleStyle = stylePrimaryBold
		}

		row := marker + idx + heart + titleStyle.Render(t.Title) + styleDim.Render(" • "+t.Artist)
		rows = append(rows, truncate2(row, w))
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, strings.Join(rows, "\n"))
}

// ─── Favorites screen ──────────────────────────────────────────────────────────

func (m *model) renderFavorites(w, h int) string {
	heading := styleSecondaryBold.Render(iconHeart + " Favorites")
	favs := m.cfg.Favorites
	if len(favs) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			heading,
			styleDim.Render("No favorites yet (press f while playing)."),
		)
	}

	listH := h - lipgloss.Height(heading)
	if listH < 1 {
		listH = 1
	}
	start, end := windowBounds(m.favCursor, len(favs), listH)
	var rows []string
	for i := start; i < end; i++ {
		rows = append(rows, m.renderResultRow(i+1, favs[i], i == m.favCursor, w, true))
	}
	return lipgloss.JoinVertical(lipgloss.Left, heading, strings.Join(rows, "\n"))
}

// ─── History screen ────────────────────────────────────────────────────────────

func (m *model) renderHistory(w, h int) string {
	heading := stylePrimaryBold.Render("Recently Played")
	hist := m.cfg.History
	if len(hist) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			heading,
			styleDim.Render("No listening history yet."),
		)
	}

	footer := styleDim.Render("j/k navigate • Enter queue • p play-now")
	listH := h - lipgloss.Height(heading) - lipgloss.Height(footer)
	if listH < 1 {
		listH = 1
	}

	start, end := windowBounds(m.historyCursor, len(hist), listH)
	var rows []string
	for i := start; i < end; i++ {
		e := hist[i]
		selected := i == m.historyCursor
		ts := styleSecondary.Render(e.PlayedAt.Format("Jan 2 15:04") + "  ")

		titleStyle := styleText
		if selected {
			titleStyle = stylePrimaryBold
		}
		heart := ""
		if m.cfg.IsFavorite(e.Track.ID) {
			heart = styleHeart.Render(iconHeart) + " "
		}
		body := heart + titleStyle.Render(e.Track.Title) + styleDim.Render(" • "+e.Track.Artist)
		row := truncate2(ts+body, w)
		rows = append(rows, row)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		heading,
		strings.Join(rows, "\n"),
		footer,
	)
}

// ─── Help screen ───────────────────────────────────────────────────────────────

func (m *model) renderHelp(w, h int) string {
	keys := [][2]string{
		{"Space", "play / pause"},
		{"n", "next track"},
		{"b", "previous track"},
		{"< / >", "seek backward / forward"},
		{"+ / -", "volume up / down"},
		{"s", "toggle shuffle"},
		{"r", "cycle repeat mode"},
		{"f", "toggle favorite"},
		{"z", "play a random song"},
		{"R", "start radio from current track"},
		{"/", "search"},
		{"j / k", "navigate list"},
		{"tab", "switch view"},
		{"1-8", "jump to view (Home…Explore)"},
		{"g", "refresh browse feed"},
		{"q", "quit"},
	}

	var rows []string
	rows = append(rows, styleSecondaryBold.Render(iconHelp+" Keyboard Shortcuts"))
	for _, k := range keys {
		rows = append(rows, stylePrimary.Render(fmt.Sprintf("  %-8s", k[0]))+styleText.Render(k[1]))
	}

	inner := w - 4
	if inner < 1 {
		inner = 1
	}
	return styleContentBox.Width(inner).Render(strings.Join(rows, "\n"))
}

// ─── Shared list helpers ───────────────────────────────────────────────────────

// windowBounds returns [start,end) for a scrolling window of size h around cursor.
func windowBounds(cursor, total, h int) (int, int) {
	if h >= total {
		return 0, total
	}
	start := cursor - h/2
	if start < 0 {
		start = 0
	}
	if start+h > total {
		start = total - h
	}
	return start, start + h
}

// truncate2 truncates a pre-styled (ANSI-containing) string to a display width.
func truncate2(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	// Fall back to a clamped width container which crops ANSI-safely.
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}
