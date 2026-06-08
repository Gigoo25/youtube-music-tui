package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rob/ytmusic/internal/api"
	"github.com/rob/ytmusic/internal/player"
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
	m.viewportH = contentH // remember for page-scroll key handling

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
// sidebarKey captures every input that affects the sidebar render; when it is
// unchanged the cached string is reused (the sidebar is rebuilt every frame).
type sidebarKey struct {
	w, h      int
	focus     focusArea
	navCursor int
	view      view
}

func (m *model) renderSidebar(w, h int) string {
	key := sidebarKey{w: w, h: h, focus: m.focus, navCursor: m.navCursor, view: m.activeView}
	if m.sbCache != "" && key == m.sbKey {
		return m.sbCache
	}
	s := m.buildSidebar(w, h)
	m.sbKey, m.sbCache = key, s
	return s
}

func (m *model) buildSidebar(w, h int) string {
	inner := w - 4 // rounded border (2) + padding (2)
	if inner < 1 {
		inner = 1
	}

	// The box has Padding(0,1), so the usable text width is inner-2. A coloured
	// (ANSI) selection row wider than this overflows and lipgloss wraps it onto a
	// second line, so every row must be padded/cropped to textW exactly.
	textW := inner - 2
	if textW < 1 {
		textW = 1
	}

	var rows []string
	rows = append(rows, stylePrimaryBold.Render(truncate2(iconVolume+" ytmusic", textW)))
	rows = append(rows, styleSecondaryBold.Render(truncate2("Quick Links", textW)))
	sidebarFocused := m.focus == focusSidebar
	for i, e := range navEntries {
		switch {
		case sidebarFocused && i == m.navCursor:
			// Background highlight already marks the cursor — no ">" needed.
			rows = append(rows, styleSelected.Render(padRight("  "+e.label, textW)))
		case e.view == m.activeView:
			// Marks the active view while the panel is focused.
			rows = append(rows, stylePrimary.Render(truncate2("> "+e.label, textW)))
		default:
			rows = append(rows, styleText.Render(truncate2("  "+e.label, textW)))
		}
	}

	body := padToHeight(strings.Join(rows, "\n"), h-2)
	return styleSidebarBox.Width(inner).Height(h - 2).Render(body)
}

// renderPanel renders the active view's content, with a local-filter line on top
// when a filter is being typed or applied.
func (m *model) renderPanel(w, h int) string {
	if m.filtering || m.filter != "" {
		fl := m.renderFilterLine(w)
		body := m.renderPanelBody(w, h-lipgloss.Height(fl))
		return lipgloss.JoinVertical(lipgloss.Left, fl, body)
	}
	return m.renderPanelBody(w, h)
}

// renderFilterLine shows the active "/" filter and its match count.
func (m *model) renderFilterLine(w int) string {
	n := m.activeFilteredLen()
	count := styleDim.Render(fmt.Sprintf("  (%d)", n))
	if m.filtering {
		return stylePrimary.Render(iconSearch+" ") + m.filterInput.View() + count
	}
	return styleAccent.Render(iconSearch+" "+m.filter) + count +
		styleDim.Render("  esc to clear")
}

// renderPanelBody renders the active view's content into the main panel.
func (m *model) renderPanelBody(w, h int) string {
	switch m.activeView {
	case viewHome:
		return m.renderHome(w, h)
	case viewArtist:
		return m.renderArtist(w, h)
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
	case viewGenres:
		return m.renderGenres(w, h)
	case viewHelp:
		return m.renderHelp(w, h)
	}
	return ""
}

// renderGenres renders the random-genre picker (row 0 = "Any").
func (m *model) renderGenres(w, h int) string {
	heading := styleSecondaryBold.Render(iconShuffle + " Random — pick a genre")
	items := make([]string, 0, len(randomSeeds)+1)
	items = append(items, "Any (surprise me)")
	items = append(items, randomSeeds...)

	listH := h - lipgloss.Height(heading)
	if listH < 1 {
		listH = 1
	}
	focused := m.focus == focusPanel
	start, end := windowBounds(m.genreCursor, len(items), listH)
	var rows []string
	for i := start; i < end; i++ {
		selected := i == m.genreCursor
		if selected && focused {
			rows = append(rows, styleSelected.Width(w).Render(truncate("▸ "+items[i], w)))
			continue
		}
		marker := "  "
		if selected {
			marker = stylePrimary.Render("▸ ")
		}
		rows = append(rows, marker+styleText.Render(items[i]))
	}
	return lipgloss.JoinVertical(lipgloss.Left, heading, strings.Join(rows, "\n"))
}

// renderAlbum renders the album-of-a-track view.
func (m *model) renderAlbum(w, h int) string {
	heading := styleSecondaryBold.Render(iconPlaylist + " Album: " + m.albumTitle)

	switch {
	case m.albumLoading:
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleAccent.Render("Loading…"))
	case m.albumErr != "":
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleError.Render(m.albumErr))
	case len(m.albumTracks) == 0:
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleDim.Render("Album not found."))
	}

	tracks := m.filt(m.albumTracks)
	if len(tracks) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleDim.Render("No matching tracks."))
	}
	listH := h - lipgloss.Height(heading)
	if listH < 1 {
		listH = 1
	}
	focused := m.focus == focusPanel
	start, end := windowBounds(m.albumCursor, len(tracks), listH)
	var rows []string
	for i := start; i < end; i++ {
		rows = append(rows, m.renderResultRow(i+1, tracks[i], i == m.albumCursor, focused, w, false))
	}
	return lipgloss.JoinVertical(lipgloss.Left, heading, strings.Join(rows, "\n"))
}

// renderBrowse renders an async browse view (Trending / New Releases / Explore).
func (m *model) renderBrowse(v view, w, h int) string {
	bs, ok := m.browse[v]
	if !ok {
		return ""
	}
	heading := styleSecondaryBold.Render(iconPlaylist + " " + bs.title)

	switch {
	case bs.loading || !bs.loaded:
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleAccent.Render("Loading..."))
	case bs.err != "":
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleError.Render(bs.err))
	case len(bs.tracks) == 0:
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleDim.Render("Nothing here yet."))
	}

	tracks := m.filt(bs.tracks)
	if len(tracks) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleDim.Render("No matching tracks."))
	}
	listH := h - lipgloss.Height(heading)
	if listH < 1 {
		listH = 1
	}
	start, end := windowBounds(bs.cursor, len(tracks), listH)
	var rows []string
	for i := start; i < end; i++ {
		rows = append(rows, m.renderResultRow(i+1, tracks[i], i == bs.cursor, m.focus == focusPanel, w, false))
	}
	return lipgloss.JoinVertical(lipgloss.Left, heading, strings.Join(rows, "\n"))
}

// ─── Home screen ───────────────────────────────────────────────────────────────

// renderHome renders the drop-in Home view: a Listen Again section (recent
// history) and a Quick Picks section, as one flat-cursor list that scrolls.
func (m *model) renderHome(w, h int) string {
	focused := m.focus == focusPanel
	var rows []string
	selRow, flat := 0, 0

	addTrack := func(n int, t api.Track) {
		sel := flat == m.homeCursor
		if sel {
			selRow = len(rows)
		}
		rows = append(rows, m.renderResultRow(n, t, sel, focused, w, false))
		flat++
	}

	listenAgain, quickPicks := m.homeSections()

	rows = append(rows, styleSecondaryBold.Render(iconRepeatAll+" Listen Again"))
	if len(listenAgain) == 0 {
		rows = append(rows, styleDim.Render("  Nothing yet — play a song and it'll show up here."))
	}
	for i, t := range listenAgain {
		addTrack(i+1, t)
	}

	rows = append(rows, styleSecondaryBold.Render(iconAutoplay+" Quick Picks"))
	switch {
	case len(quickPicks) == 0 && m.homeQPLoading:
		rows = append(rows, styleAccent.Render("  Loading…"))
	case len(quickPicks) == 0 && m.homeQPErr != "":
		rows = append(rows, styleError.Render("  "+m.homeQPErr))
	case len(quickPicks) == 0:
		rows = append(rows, styleDim.Render("  Nothing here yet."))
	}
	for i, t := range quickPicks {
		addTrack(i+1, t)
	}

	return windowRows(rows, selRow, h)
}

// ─── Artist screen ─────────────────────────────────────────────────────────────

// renderArtist renders the artist view: top songs plus albums, one flat cursor
// spanning both (songs first, then albums).
func (m *model) renderArtist(w, h int) string {
	heading := styleSecondaryBold.Render(iconPlaylist + " Artist: " + m.artistName)

	switch {
	case m.artistLoading:
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleAccent.Render("Loading…"))
	case m.artistErr != "":
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleError.Render(m.artistErr))
	case len(m.artistSongs) == 0 && len(m.artistAlbums) == 0:
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleDim.Render("Artist not found."))
	}

	listH := h - lipgloss.Height(heading)
	if listH < 1 {
		listH = 1
	}
	focused := m.focus == focusPanel
	songs := m.filt(m.artistSongs)
	albums := m.filtAlbums(m.artistAlbums)
	var rows []string
	selRow, flat := 0, 0

	if len(songs) > 0 {
		rows = append(rows, styleSecondaryBold.Render(iconPlay+" Top Songs"))
		for i, t := range songs {
			sel := flat == m.artistCursor
			if sel {
				selRow = len(rows)
			}
			rows = append(rows, m.renderResultRow(i+1, t, sel, focused, w, false))
			flat++
		}
	}
	if len(albums) > 0 {
		rows = append(rows, styleSecondaryBold.Render(iconPlaylist+" Albums"))
		for i, a := range albums {
			sel := flat == m.artistCursor
			if sel {
				selRow = len(rows)
			}
			rows = append(rows, m.renderAlbumRefRow(i+1, a, sel, focused, w))
			flat++
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, heading, windowRows(rows, selRow, listH))
}

// renderAlbumRefRow renders an album card row (title + right-aligned year) with
// the same selection styling as a track row.
func (m *model) renderAlbumRefRow(n int, a api.AlbumRef, selected, focused bool, w int) string {
	marker := "  "
	if selected {
		if focused {
			marker = "▸ "
		} else {
			marker = "· "
		}
	}
	numStr := fmt.Sprintf("%d. ", n)
	right := a.Year

	if selected && focused {
		plain := marker + numStr + a.Title
		gap := w - lipgloss.Width(plain) - lipgloss.Width(right)
		if gap < 1 {
			plain = truncate(plain, w-lipgloss.Width(right)-1)
			gap = w - lipgloss.Width(plain) - lipgloss.Width(right)
			if gap < 1 {
				gap = 1
			}
		}
		return styleSelected.Width(w).Render(plain + strings.Repeat(" ", gap) + right)
	}

	markerStr := styleDim.Render(marker)
	if selected {
		markerStr = stylePrimary.Render(marker)
	}
	left := markerStr + styleDim.Render(numStr) + styleText.Render(a.Title)
	rightStr := styleDim.Render(right)
	gap := w - lipgloss.Width(left) - lipgloss.Width(rightStr)
	if gap < 1 {
		left = truncate2(left, w-lipgloss.Width(rightStr)-1)
		gap = w - lipgloss.Width(left) - lipgloss.Width(rightStr)
		if gap < 1 {
			gap = 1
		}
	}
	return left + strings.Repeat(" ", gap) + rightStr
}

// windowRows joins a slice of pre-rendered rows, scrolled so the row at `center`
// stays visible within h lines.
func windowRows(rows []string, center, h int) string {
	if len(rows) == 0 {
		return ""
	}
	if center < 0 {
		center = 0
	}
	start, end := windowBounds(center, len(rows), h)
	return strings.Join(rows[start:end], "\n")
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

// shortcut is one [key] label hint shown in the bottom bar. on highlights an
// active toggle (e.g. shuffle on) in the success colour.
type shortcut struct {
	key   string
	label string
	on    bool
}

// renderShortcutsBar shows every shortcut available in the current context as
// [key] label hints, wrapping across as many lines as needed.
// shortcutsKey captures every input that affects the shortcuts bar; when it is
// unchanged the cached string is reused instead of rebuilding + re-styling every
// segment each frame.
type shortcutsKey struct {
	w          int
	typing     bool
	filtering  bool
	filtered   bool
	hasCurrent bool
	paused     bool
	volume     float64
	shuffle    bool
	repeat     repeatMode
	focus      focusArea
	view       view
}

func (m *model) renderShortcutsBar(w int) string {
	key := shortcutsKey{
		w: w, typing: m.typing(), filtering: m.filtering, filtered: m.filter != "",
		hasCurrent: m.hasCurrent,
		paused:     m.playerState.Paused, volume: m.playerState.Volume,
		shuffle: m.shuffle, repeat: m.repeat, focus: m.focus, view: m.activeView,
	}
	if m.scCache != "" && key == m.scKey {
		return m.scCache
	}
	s := m.buildShortcutsBar(w)
	m.scKey, m.scCache = key, s
	return s
}

func (m *model) buildShortcutsBar(w int) string {
	inner := w - 2 // box has a single border
	if inner < 1 {
		inner = 1
	}

	var segs []shortcut

	// Typing in the search box: only the input controls apply.
	if m.typing() {
		segs = append(segs,
			shortcut{"enter", "search", false},
			shortcut{"esc", "cancel", false},
			shortcut{"tab", "menu", false},
		)
		return styleShortcutsBox.Width(inner).Render(wrapShortcuts(segs, inner))
	}

	// Editing the local filter.
	if m.filtering {
		segs = append(segs,
			shortcut{"type", "filter pane", false},
			shortcut{"enter", "keep", false},
			shortcut{"esc", "clear", false},
		)
		return styleShortcutsBox.Width(inner).Render(wrapShortcuts(segs, inner))
	}

	// Playback globals (always available).
	pp := "play"
	if m.hasCurrent && !m.playerState.Paused {
		pp = "pause"
	}
	segs = append(segs,
		shortcut{"space", pp, false},
		shortcut{"b", "prev", false},
		shortcut{"n", "next", false},
		shortcut{"< >", "seek", false},
		shortcut{"+/-", fmt.Sprintf("vol %d%%", int(m.playerState.Volume)), false},
		shortcut{"s", "shuffle", m.shuffle},
	)
	repLabel := "repeat"
	switch m.repeat {
	case repeatAll:
		repLabel = "repeat all"
	case repeatOne:
		repLabel = "repeat 1"
	}
	segs = append(segs, shortcut{"r", repLabel, m.repeat != repeatOff})

	// Context-specific actions.
	if m.focus == focusSidebar {
		segs = append(segs,
			shortcut{"j/k", "move", false},
			shortcut{"enter", "open", false},
		)
	} else {
		switch m.activeView {
		case viewHome:
			segs = append(segs, shortcut{"j/k", "move", false},
				shortcut{"enter", "queue", false}, shortcut{"p", "play now", false},
				shortcut{"f", "fav", false}, shortcut{"a", "album", false},
				shortcut{"A", "artist", false}, shortcut{"ctrl+r", "refresh", false})
		case viewSearch:
			segs = append(segs, shortcut{"j/k", "move", false},
				shortcut{"enter", "queue", false}, shortcut{"p", "play now", false},
				shortcut{"f", "fav", false}, shortcut{"a", "album", false},
				shortcut{"A", "artist", false})
		case viewQueue:
			segs = append(segs, shortcut{"j/k", "move", false},
				shortcut{"J/K", "reorder", false},
				shortcut{"enter", "play", false}, shortcut{"d", "remove", false},
				shortcut{"c", "clear", false}, shortcut{"f", "fav", false},
				shortcut{"a", "album", false}, shortcut{"A", "artist", false})
		case viewFavorites, viewHistory:
			segs = append(segs, shortcut{"j/k", "move", false},
				shortcut{"enter", "queue", false}, shortcut{"p", "play now", false},
				shortcut{"f", "fav", false}, shortcut{"a", "album", false},
				shortcut{"A", "artist", false})
		case viewTrending, viewNewReleases, viewExplore:
			segs = append(segs, shortcut{"j/k", "move", false},
				shortcut{"enter", "queue", false}, shortcut{"p", "play now", false},
				shortcut{"f", "fav", false}, shortcut{"a", "album", false},
				shortcut{"A", "artist", false}, shortcut{"ctrl+r", "refresh", false})
		case viewAlbum:
			segs = append(segs, shortcut{"j/k", "move", false},
				shortcut{"enter", "play album", false}, shortcut{"p", "play all", false},
				shortcut{"f", "fav", false})
		case viewArtist:
			segs = append(segs, shortcut{"j/k", "move", false},
				shortcut{"enter", "play / open", false}, shortcut{"p", "play songs", false},
				shortcut{"f", "fav", false})
		case viewGenres:
			segs = append(segs, shortcut{"j/k", "move", false},
				shortcut{"enter", "pick & play", false}, shortcut{"esc", "cancel", false})
		}
		segs = append(segs, shortcut{"h", "menu", false})
	}

	// More globals. "/" filters the current pane (or re-focuses the query in the
	// Search view); global YouTube Music search lives on the sidebar / key 2.
	if m.filterableView() {
		segs = append(segs, shortcut{"/", "filter", m.filter != ""})
	}
	segs = append(segs,
		shortcut{"z", "random", false},
		shortcut{"R", "radio", false},
		shortcut{"T", "theme", false},
		shortcut{"?", "help", false},
		shortcut{"q", "quit", false},
	)

	return styleShortcutsBox.Width(inner).Render(wrapShortcuts(segs, inner))
}

// wrapShortcuts renders [key] label segments, greedily wrapping into lines that
// fit within width.
func wrapShortcuts(segs []shortcut, width int) string {
	sep := "  "
	sepW := lipgloss.Width(sep)

	var lines []string
	cur := ""
	curW := 0
	for _, s := range segs {
		labelStyle := styleDim
		if s.on {
			labelStyle = styleSuccess
		}
		seg := stylePrimary.Render("["+s.key+"]") + " " + labelStyle.Render(s.label)
		segW := lipgloss.Width(seg)

		switch {
		case cur == "":
			cur, curW = seg, segW
		case curW+sepW+segW <= width:
			cur += sep + seg
			curW += sepW + segW
		default:
			lines = append(lines, cur)
			cur, curW = seg, segW
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return strings.Join(lines, "\n")
}

// ─── Persistent now-playing bar ──────────────────────────────────────────────────

// renderNowBar renders a compact single-line now-playing status bar shown at the
// bottom (above the shortcuts bar) whenever a track is loaded. Returns "" when
// nothing is playing.
func (m *model) renderNowBar(w int) string {
	if !m.hasCurrent {
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
		left += styleDim.Render(" • ") + styleSecondary.Render(t.Artist)
	}
	if t.Album != "" {
		left += styleDim.Render(" — " + t.Album)
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
	info := left + strings.Repeat(" ", gap) + right
	return info + "\n" + m.renderProgressBar(w, s)
}

// renderProgressBar draws a full-width track progress bar (filled blocks for the
// elapsed fraction, shaded blocks for the rest).
func (m *model) renderProgressBar(w int, s player.State) string {
	if w < 1 {
		w = 1
	}
	frac := 0.0
	if s.Duration > 0 {
		frac = s.Position / s.Duration
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	filled := int(frac * float64(w))
	if filled > w {
		filled = w
	}
	return stylePrimary.Render(strings.Repeat("█", filled)) +
		styleDim.Render(strings.Repeat("░", w-filled))
}

// ─── Search screen ─────────────────────────────────────────────────────────────

func (m *model) renderSearch(w, h int) string {
	var blocks []string
	used := 0
	// (now-playing is shown in the persistent bottom bar)

	heading := styleSecondaryBold.Render(iconSearch + " Search")
	blocks = append(blocks, heading)
	used += lipgloss.Height(heading)

	// Search bar
	searchInner := w - 4 // single border + padding
	if searchInner < 1 {
		searchInner = 1
	}
	var barContent string
	if m.searchTyping {
		barContent = stylePrimary.Render("Search: ") + m.searchInput.View()
	} else {
		barContent = stylePrimary.Render("Search: ") + styleDim.Render("press / to edit query")
	}
	bar := styleSearchBox.Width(searchInner).Render(barContent)
	blocks = append(blocks, bar)
	used += lipgloss.Height(bar)

	if m.searching {
		blocks = append(blocks, styleAccent.Render("Searching..."))
		used++
	}

	// Results list fills the remaining height (shortcuts live in the bottom bar).
	listH := h - used
	if listH < 1 {
		listH = 1
	}
	blocks = append(blocks, m.renderResultList(w, listH))
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
	header := styleSecondaryBold.Render(fmt.Sprintf("%s Up next (%d tracks)", iconPlaylist, len(m.queue)))
	if len(m.queue) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			header,
			styleDim.Render("Queue is empty — search and press Enter to add."),
		)
	}

	listH := h - lipgloss.Height(header)
	if listH < 1 {
		listH = 1
	}

	// vis maps filtered rows to indices in the full queue (identity when no filter).
	vis := m.trackVisibleIndices(m.queue)
	if len(vis) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, header, styleDim.Render("No matching tracks."))
	}

	focused := m.focus == focusPanel
	start, end := windowBounds(m.queueCursor, len(vis), listH)
	var rows []string
	for i := start; i < end; i++ {
		orig := vis[i]
		t := m.queue[orig]
		selected := i == m.queueCursor

		nowMark := "  "
		if orig == m.queuePos {
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
		if orig == m.queuePos {
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
	if len(m.cfg.Favorites) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			heading,
			styleDim.Render("No favorites yet (press f while playing)."),
		)
	}
	favs := m.filt(m.cfg.Favorites)
	if len(favs) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleDim.Render("No matching favorites."))
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
	heading := styleSecondaryBold.Render(iconPlaylist + " Recently Played")
	if len(m.cfg.History) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			heading,
			styleDim.Render("No listening history yet."),
		)
	}
	hist := m.filtHistory(m.cfg.History)
	if len(hist) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, heading, styleDim.Render("No matching history."))
	}

	listH := h - lipgloss.Height(heading)
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
		{"A", "open the track's artist (top songs + albums)"},
		{"z", "random song (pick a genre)"},
		{"R", "start radio from current track"},
		{"T", "cycle color theme"},
		{"enter", "queue / play selected"},
		{"p", "play now"},
		{"d / x", "remove from queue"},
		{"J / K", "move track down / up in queue"},
		{"c", "clear queue"},
		{"ctrl+r", "refresh Home / browse feed"},
		{"/", "filter the current pane (esc clears)"},
		{"2", "global YouTube Music search"},
		{"j / k", "navigate list"},
		{"ctrl+d / ctrl+u", "scroll half page down / up"},
		{"ctrl+f / ctrl+b", "scroll full page down / up"},
		{"gg / G", "jump to top / bottom"},
		{"h / esc", "back to menu / previous"},
		{"tab", "toggle sidebar / panel focus"},
		{"1-8", "jump to view (Home…Explore)"},
		{"? ", "this help"},
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
