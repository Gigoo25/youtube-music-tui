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

// padRight pads s with spaces (or crops it) so its display width is exactly n.
// Display-width aware, so glyphs like ▸/♪ don't cause a background-filled style
// to overflow onto a second line.
func padRight(s string, n int) string {
	if n < 0 {
		n = 0
	}
	w := lipgloss.Width(s)
	if w > n {
		return truncate2(s, n)
	}
	return s + strings.Repeat(" ", n-w)
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
	nowbar := m.renderNowBar(innerW)

	// Height budget: outer border (2) + shortcuts + status + now-playing bar.
	used := 2 + lipgloss.Height(shortcuts)
	if status != "" {
		used += lipgloss.Height(status)
	}
	if nowbar != "" {
		used += lipgloss.Height(nowbar)
	}
	contentH := m.height - used
	if contentH < 1 {
		contentH = 1
	}

	// Two-column layout: Quick Links sidebar | main content panel.
	sidebarW := innerW / 4
	if sidebarW < 18 {
		sidebarW = 18
	}
	if sidebarW > 30 {
		sidebarW = 30
	}
	if sidebarW > innerW-10 {
		sidebarW = innerW - 10
		if sidebarW < 1 {
			sidebarW = 1
		}
	}
	panelW := innerW - sidebarW
	if panelW < 1 {
		panelW = 1
	}

	sidebar := m.renderSidebar(sidebarW, contentH)
	panel := padToHeight(m.renderPanel(panelW, contentH), contentH)
	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, panel)

	content := padToHeight(body, contentH)

	// Footer stack: content, persistent now-playing bar, status, shortcuts.
	parts := []string{content}
	if nowbar != "" {
		parts = append(parts, nowbar)
	}
	if status != "" {
		parts = append(parts, status)
	}
	parts = append(parts, shortcuts)
	inner := lipgloss.JoinVertical(lipgloss.Left, parts...)

	// Pin the shell to the full terminal height (border consumes 2 rows).
	return styleShell.Width(innerW).Height(m.height - 2).Render(inner)
}

// renderSidebar renders the persistent Quick Links navigation column.
func (m *model) renderSidebar(w, h int) string {
	inner := w - 4 // rounded border (2) + padding (2)
	if inner < 1 {
		inner = 1
	}

	var rows []string
	rows = append(rows, stylePrimaryBold.Render(truncate2(iconVolume+" ytmusic", inner)))
	rows = append(rows, styleSecondaryBold.Render("Quick Links"))
	sidebarFocused := m.focus == focusSidebar
	for i, e := range navEntries {
		switch {
		case sidebarFocused && i == m.navCursor:
			// Strong cursor highlight only when the sidebar has focus. Pad with a
			// display-width-aware helper so the background fills exactly one line
			// (a rune-count truncate miscounts glyphs like ▸ and wraps).
			rows = append(rows, styleSelected.Render(padRight("> "+e.label, inner)))
		case e.view == m.activeView:
			// Marks the active view while the panel is focused.
			rows = append(rows, stylePrimary.Render(truncate2("> "+e.label, inner)))
		default:
			rows = append(rows, styleText.Render(truncate2("  "+e.label, inner)))
		}
	}

	body := padToHeight(strings.Join(rows, "\n"), h-2)
	return styleSidebarBox.Width(inner).Height(h - 2).Render(body)
}

// renderPanel renders the active view's content into the main panel.
func (m *model) renderPanel(w, h int) string {
	switch m.activeView {
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
	case viewAlbum:
		return m.renderAlbum(w, h)
	case viewHelp:
		return m.renderHelp(w, h)
	}
	return ""
}

// renderAlbum renders the album-of-a-track view.
func (m *model) renderAlbum(w, h int) string {
	heading := stylePrimaryBold.Render("Album: " + m.albumTitle)

	switch {
	case m.albumLoading:
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleAccent.Render("Loading…"))
	case m.albumErr != "":
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleError.Render(m.albumErr))
	case len(m.albumTracks) == 0:
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleDim.Render("Album not found."))
	}

	footer := styleDim.Render("Enter play album from here • p play whole album • a album of track")
	listH := h - lipgloss.Height(heading) - lipgloss.Height(footer)
	if listH < 1 {
		listH = 1
	}
	focused := m.focus == focusPanel
	start, end := windowBounds(m.albumCursor, len(m.albumTracks), listH)
	var rows []string
	for i := start; i < end; i++ {
		rows = append(rows, m.renderResultRow(i+1, m.albumTracks[i], i == m.albumCursor, focused, w, false))
	}
	return lipgloss.JoinVertical(lipgloss.Left, heading, strings.Join(rows, "\n"), footer)
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
		rows = append(rows, m.renderResultRow(i+1, bs.tracks[i], i == bs.cursor, m.focus == focusPanel, w, false))
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

// ─── Persistent now-playing bar ──────────────────────────────────────────────────

// renderNowBar renders a compact single-line now-playing status bar shown at the
// bottom (above the shortcuts bar) whenever a track is loaded. Returns "" when
// nothing is playing.
func (m *model) renderNowBar(w int) string {
	if m.current == nil {
		return ""
	}
	s := m.playerState
	t := m.current

	icon, istyle := iconPlay, styleSuccess
	if s.Paused {
		icon, istyle = iconPause, styleDim
	}

	fav := ""
	if m.cfg.IsFavorite(t.ID) {
		fav = styleHeart.Render(iconHeart) + " "
	}
	left := istyle.Render(icon) + " " + fav + stylePrimaryBold.Render(t.Title)
	if t.Artist != "" {
		left += styleDim.Render(" • " + t.Artist)
	}

	pct := 0
	if s.Duration > 0 {
		pct = int(s.Position / s.Duration * 100)
	}
	right := ""
	if s.Loading {
		right += styleAccent.Render("loading… ")
	}
	right += styleDim.Render(fmt.Sprintf("%s / %s [%d%%]", fmtDur(s.Position), fmtDur(s.Duration), pct))
	if m.shuffle {
		right += stylePrimary.Render(" " + iconShuffle)
	}
	if m.repeat != repeatOff {
		g := iconRepeatAll
		if m.repeat == repeatOne {
			g = iconRepeatOne
		}
		right += styleSecondary.Render(" " + g)
	}

	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		left = truncate2(left, w-lipgloss.Width(right)-1)
		gap = w - lipgloss.Width(left) - lipgloss.Width(right)
		if gap < 1 {
			gap = 1
		}
	}
	return left + strings.Repeat(" ", gap) + right
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
		rows = append(rows, m.renderResultRow(i+1, t, selected, m.focus == focusPanel, w, false))
	}
	return strings.Join(rows, "\n")
}

// renderResultRow renders a numbered track row with a cursor marker, optional
// heart, title, artist, and right-aligned duration. The selected row gets a
// full-width inverse highlight only when its pane is focused, so the cursor is
// unambiguous; an unfocused selection shows a subtle marker instead.
func (m *model) renderResultRow(n int, t api.Track, selected, focused bool, w int, forceHeart bool) string {
	marker := "  "
	if selected {
		if focused {
			marker = "▸ "
		} else {
			marker = "· "
		}
	}

	numStr := fmt.Sprintf("%d. ", n)
	hasHeart := forceHeart || m.cfg.IsFavorite(t.ID)
	heartGlyph := ""
	if hasHeart {
		heartGlyph = iconHeart + " "
	}
	dur := t.Duration
	artistPart := ""
	if t.Artist != "" {
		artistPart = " • " + t.Artist
	}

	// Focused + selected: whole-row inverse highlight (single color).
	if selected && focused {
		plain := marker + numStr + heartGlyph + t.Title + artistPart
		gap := w - lipgloss.Width(plain) - lipgloss.Width(dur)
		if gap < 1 {
			plain = truncate(plain, w-lipgloss.Width(dur)-1)
			gap = w - lipgloss.Width(plain) - lipgloss.Width(dur)
			if gap < 1 {
				gap = 1
			}
		}
		return styleSelected.Width(w).Render(plain + strings.Repeat(" ", gap) + dur)
	}

	// Otherwise a normal coloured row.
	markerStr := styleDim.Render(marker)
	if selected {
		markerStr = stylePrimary.Render(marker)
	}
	heart := ""
	if hasHeart {
		heart = styleHeart.Render(iconHeart) + " "
	}
	left := markerStr + styleDim.Render(numStr) + heart + styleText.Render(t.Title) + styleDim.Render(artistPart)
	durStr := styleDim.Render(dur)

	gap := w - lipgloss.Width(left) - lipgloss.Width(durStr)
	if gap < 1 {
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

	focused := m.focus == focusPanel
	start, end := windowBounds(m.queueCursor, len(m.queue), listH)
	var rows []string
	for i := start; i < end; i++ {
		t := m.queue[i]
		selected := i == m.queueCursor

		nowMark := "  "
		if i == m.queuePos {
			nowMark = iconPlay + " "
		}
		artistPart := ""
		if t.Artist != "" {
			artistPart = " • " + t.Artist
		}

		if selected && focused {
			plain := nowMark + fmt.Sprintf("%d. ", i+1) + t.Title + artistPart
			rows = append(rows, styleSelected.Width(w).Render(truncate(plain, w)))
			continue
		}

		nowStr := styleDim.Render(nowMark)
		if i == m.queuePos {
			nowStr = stylePrimary.Render(nowMark)
		}
		heart := ""
		if m.cfg.IsFavorite(t.ID) {
			heart = styleHeart.Render(iconHeart) + " "
		}
		marker := "  "
		if selected {
			marker = stylePrimary.Render("▸ ")
		}
		row := marker + nowStr + styleDim.Render(fmt.Sprintf("%d. ", i+1)) + heart +
			styleText.Render(t.Title) + styleDim.Render(artistPart)
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
		rows = append(rows, m.renderResultRow(i+1, favs[i], i == m.favCursor, m.focus == focusPanel, w, true))
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

	focused := m.focus == focusPanel
	tsLayout := "Jan 2 15:04"
	start, end := windowBounds(m.historyCursor, len(hist), listH)
	var rows []string
	for i := start; i < end; i++ {
		e := hist[i]
		selected := i == m.historyCursor
		tsText := e.PlayedAt.Format(tsLayout) + "  "
		artistPart := ""
		if e.Track.Artist != "" {
			artistPart = " • " + e.Track.Artist
		}

		if selected && focused {
			plain := "▸ " + tsText + e.Track.Title + artistPart
			rows = append(rows, styleSelected.Width(w).Render(truncate(plain, w)))
			continue
		}

		marker := "  "
		if selected {
			marker = stylePrimary.Render("▸ ")
		}
		heart := ""
		if m.cfg.IsFavorite(e.Track.ID) {
			heart = styleHeart.Render(iconHeart) + " "
		}
		body := marker + styleSecondary.Render(tsText) + heart +
			styleText.Render(e.Track.Title) + styleDim.Render(artistPart)
		rows = append(rows, truncate2(body, w))
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
		{"a", "open the track's album (Enter to play it)"},
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
