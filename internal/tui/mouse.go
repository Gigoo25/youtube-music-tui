package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Mouse support: the wheel scrolls whichever pane is under the pointer, a click
// selects (sidebar: opens) a row, a double-click runs the row's primary action,
// and a click on the progress bar seeks. Everything funnels into the same key
// handlers the keyboard uses, so mouse and keys can't drift apart.

// layout is the screen geometry of the last rendered frame (set in View and
// renderPanel). Screen coordinates are 0-based; row/col 0 is the shell border.
type layout struct {
	sidebarW   int // sidebar width, starting at x=1
	innerW     int // width inside the shell border (sidebar + panel)
	contentH   int // rows of the sidebar/panel area, starting at y=1
	panelBodyY int // screen row of the panel body's first line (below any filter/naming line)
	progressY  int // screen row of the progress bar; -1 when nothing is playing
}

// hitSearchBar marks panel lines showing the search query box (a click starts
// editing the query). Non-negative hit values are list cursor indices.
const hitSearchBar = -2

// doubleClickWindow is how quickly a second click on the same row must follow
// the first to count as a double-click.
const doubleClickWindow = 400 * time.Millisecond

// lastClick remembers the previous row click, for double-click detection.
type lastClick struct {
	view view
	idx  int
	at   time.Time
}

// hit records that panel body line `line` shows list item idx (or a hit* marker).
func (m *model) hit(line, idx int) {
	if line < 0 {
		return
	}
	for len(m.panelHits) <= line {
		m.panelHits = append(m.panelHits, -1)
	}
	m.panelHits[line] = idx
}

// hitRange records that consecutive panel lines from `line` show items start..end-1.
func (m *model) hitRange(line, start, end int) {
	for i := start; i < end; i++ {
		m.hit(line+i-start, i)
	}
}

// keyPress builds the KeyMsg the keyboard would send for s, so mouse actions
// reuse the key handlers.
func keyPress(s string) tea.KeyMsg {
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// leaveInputs stops editing the search query or the filter (keeping an applied
// filter), so a mouse action lands on the list rather than being typed.
func (m *model) leaveInputs() {
	if m.typing() {
		m.searchTyping = false
		m.searchInput.Blur()
	}
	if m.filtering {
		m.filtering = false
		m.filterInput.Blur()
	}
}

// listCursorPtr is activeCursorPtr plus the lists that have no filter (search
// results and the pickers) — every list a click can land on.
func (m *model) listCursorPtr() *int {
	switch m.activeView {
	case viewSearch:
		return &m.searchCursor
	case viewGenres:
		return &m.genreCursor
	case viewPlaylistPick:
		return &m.pickCursor
	}
	return m.activeCursorPtr()
}

// primaryAction is the key a double-click presses on row idx: play the song in
// song lists, open/pick the entry everywhere else.
func (m *model) primaryAction(idx int) string {
	switch m.activeView {
	case viewPlaylists, viewGenres, viewPlaylistPick:
		return "enter"
	case viewArtist:
		if idx >= len(m.filt(m.artistSongs)) {
			return "enter" // an album row: open it
		}
	}
	return "p"
}

func (m *model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Modal states own input; the keyboard is the way out of them.
	if m.confirmFn != nil || m.naming || m.width == 0 {
		return m, nil
	}
	L := m.lay
	inContent := msg.Y >= 1 && msg.Y < 1+L.contentH
	inSidebar := inContent && msg.X >= 1 && msg.X < 1+L.sidebarW
	inPanel := inContent && msg.X >= 1+L.sidebarW && msg.X < 1+L.innerW

	if tea.MouseEvent(msg).IsWheel() {
		k := "j"
		if msg.Button == tea.MouseButtonWheelUp {
			k = "k"
		} else if msg.Button != tea.MouseButtonWheelDown {
			return m, nil // horizontal wheel
		}
		switch {
		case inSidebar:
			m.leaveInputs()
			m.focus = focusSidebar
		case inPanel:
			m.leaveInputs()
			m.focus = focusPanel
		default:
			return m, nil
		}
		return m.handleKey(keyPress(k))
	}

	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}

	switch {
	case msg.Y == L.progressY && L.progressY >= 0 && msg.X >= 1 && msg.X < 1+L.innerW:
		// Seek to the clicked fraction of the track.
		if m.player != nil && m.playerState.Duration > 0 {
			frac := float64(msg.X-1) / float64(max(L.innerW, 1))
			m.player.SeekAbs(frac * m.playerState.Duration)
		}
		return m, nil

	case inSidebar:
		entry := m.sidebarEntryAt(msg.Y)
		if entry < 0 {
			return m, nil
		}
		m.leaveInputs()
		m.focus = focusSidebar
		m.navCursor = entry
		return m.handleKey(keyPress("enter"))

	case inPanel:
		line := msg.Y - L.panelBodyY
		if line < 0 || line >= len(m.panelHits) {
			return m, nil
		}
		idx := m.panelHits[line]
		if idx == hitSearchBar {
			m.focus = focusPanel
			return m.handleKey(keyPress("/")) // edit the query
		}
		cp := m.listCursorPtr()
		if idx < 0 || cp == nil {
			return m, nil
		}
		m.leaveInputs()
		m.focus = focusPanel
		*cp = idx
		m.pendingG = false
		now := time.Now()
		double := m.click.view == m.activeView && m.click.idx == idx && now.Sub(m.click.at) < doubleClickWindow
		if double {
			m.click = lastClick{} // a third click starts over
			return m.handleKey(keyPress(m.primaryAction(idx)))
		}
		m.click = lastClick{view: m.activeView, idx: idx, at: now}
		// Clicking the last loaded search result pages in more, like j does.
		if m.activeView == viewSearch && idx >= len(m.searchResults)-1 && m.searchContinuation != "" {
			return m, m.loadMoreSearch()
		}
	}
	return m, nil
}

// sidebarEntryAt maps a screen row to a sidebar entry index, or -1. Mirrors
// buildSidebar: the box's top border, then the title, the "Quick Links"
// heading and one row per entry, scrolled by windowRows around the cursor.
func (m *model) sidebarEntryAt(y int) int {
	h := m.lay.contentH
	if h <= 2 {
		return -1
	}
	total := 2 + len(navEntries)
	cursorIdx := min(m.navCursor+2, total-1)
	start, _ := windowBounds(cursorIdx, total, h-2)
	row := y - 2 + start // y=1 is the box's top border
	entry := row - 2
	if y < 2 || y >= h || entry < 0 || entry >= len(navEntries) { // y=h is the bottom border
		return -1
	}
	return entry
}
