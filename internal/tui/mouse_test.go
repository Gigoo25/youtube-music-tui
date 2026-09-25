package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Gigoo25/youtube-music-tui/internal/api"
	"github.com/Gigoo25/youtube-music-tui/internal/config"
)

// ansiSeq matches the SGR/CSI sequences lipgloss emits.
var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func stripANSI(s string) string { return ansiSeq.ReplaceAllString(s, "") }

// locate renders the frame and returns the screen cell where text first
// appears — clicks are aimed at what is actually drawn, so the tests check the
// layout bookkeeping against real output rather than re-deriving it.
func locate(t *testing.T, m *model, text string) (x, y int) {
	t.Helper()
	for y, line := range strings.Split(m.View(), "\n") {
		plain := stripANSI(line)
		if i := strings.Index(plain, text); i >= 0 {
			return lipgloss.Width(plain[:i]), y
		}
	}
	t.Fatalf("%q not on screen:\n%s", text, stripANSI(m.View()))
	return 0, 0
}

func click(m *model, x, y int) {
	m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
}

func wheel(m *model, x, y int, b tea.MouseButton) {
	m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: b})
}

func queueOf(n int) []api.Track {
	var ts []api.Track
	for i := range n {
		id := string(rune('a' + i))
		ts = append(ts, api.Track{ID: id, Title: "Song " + strings.ToUpper(id)})
	}
	return ts
}

func TestMouseClickSelectsQueueRow(t *testing.T) {
	m := newTestModel()
	m.queue = queueOf(5)
	m.activateView(viewQueue)
	clickOn(t, m, "Song C")
	if m.queueCursor != 2 {
		t.Fatalf("queueCursor = %d, want 2", m.queueCursor)
	}
}

// clickOn clicks wherever text is drawn.
func clickOn(t *testing.T, m *model, text string) {
	t.Helper()
	x, y := locate(t, m, text)
	click(m, x, y)
}

func TestMouseClickUnderFilterLine(t *testing.T) {
	m := newTestModel()
	m.queue = queueOf(5)
	m.activateView(viewQueue)
	m.filter = "song" // adds the filter line above the list
	clickOn(t, m, "Song D")
	if m.queueCursor != 3 {
		t.Fatalf("queueCursor = %d, want 3 (the filter line must offset the rows)", m.queueCursor)
	}
}

func TestMouseClickSidebarOpensView(t *testing.T) {
	m := newTestModel()
	clickOn(t, m, "Favorites")
	if m.activeView != viewFavorites || m.focus != focusPanel {
		t.Fatalf("view=%v focus=%v, want Favorites with the panel focused", m.activeView, m.focus)
	}
	clickOn(t, m, "History")
	if m.activeView != viewHistory {
		t.Fatalf("view=%v, want History", m.activeView)
	}
}

func TestMouseClickHomeQuickPick(t *testing.T) {
	m := newTestModel()
	m.homeListenAgain = queueOf(2)
	m.homeQuickPicks = []api.Track{{ID: "q1", Title: "Pick One"}, {ID: "q2", Title: "Pick Two"}}
	clickOn(t, m, "Pick Two")
	if m.homeCursor != 3 {
		t.Fatalf("homeCursor = %d, want 3 (flat across both sections)", m.homeCursor)
	}
}

func TestMouseClickArtistAlbumRow(t *testing.T) {
	m := newTestModel()
	m.activeView, m.focus = viewArtist, focusPanel
	m.artistName = "X"
	m.artistSongs = queueOf(2)
	m.artistAlbums = []api.AlbumRef{{ID: "MPREb1", Title: "First LP"}}
	clickOn(t, m, "First LP")
	if m.artistCursor != 2 {
		t.Fatalf("artistCursor = %d, want 2 (after both songs)", m.artistCursor)
	}
}

func TestMouseSearchBarAndResults(t *testing.T) {
	m := newTestModel()
	m.activateView(viewSearch)
	m.searchResults = queueOf(3)
	clickOn(t, m, "Song B")
	if m.searchCursor != 1 {
		t.Fatalf("searchCursor = %d, want 1", m.searchCursor)
	}
	clickOn(t, m, "press / to edit query")
	if !m.typing() {
		t.Fatal("clicking the query box did not start editing")
	}
	// Clicking a result while typing leaves the input and selects the row.
	clickOn(t, m, "Song C")
	if m.typing() || m.searchCursor != 2 {
		t.Fatalf("typing=%v cursor=%d, want browsing row 2", m.typing(), m.searchCursor)
	}
}

func TestMouseDoubleClickOpensPlaylist(t *testing.T) {
	m := newTestModel()
	m.cfg.Playlists = []config.Playlist{{Name: "road trip", Tracks: queueOf(2)}, {Name: "gym", Tracks: queueOf(1)}}
	m.activateView(viewPlaylists)
	x, y := locate(t, m, "gym")
	click(m, x, y)
	if m.activeView != viewPlaylists || m.playlistCursor != 1 {
		t.Fatalf("single click: view=%v cursor=%d, want selection only", m.activeView, m.playlistCursor)
	}
	click(m, x, y)
	if m.activeView != viewPlaylistDetail || m.openPlaylist != "gym" {
		t.Fatalf("double click: view=%v open=%q, want gym's tracks", m.activeView, m.openPlaylist)
	}
}

func TestMouseWheelScrollsPaneUnderPointer(t *testing.T) {
	m := newTestModel()
	m.queue = queueOf(5)
	m.activateView(viewQueue)
	x, y := locate(t, m, "Song A")
	wheel(m, x, y, tea.MouseButtonWheelDown)
	wheel(m, x, y, tea.MouseButtonWheelDown)
	if m.queueCursor != 2 {
		t.Fatalf("queueCursor = %d after two wheel-downs, want 2", m.queueCursor)
	}
	sx, sy := locate(t, m, "Home")
	wheel(m, sx, sy, tea.MouseButtonWheelDown)
	if m.focus != focusSidebar || m.queueCursor != 2 {
		t.Fatalf("wheel over sidebar: focus=%v queueCursor=%d, want sidebar focus, queue untouched", m.focus, m.queueCursor)
	}
}

func TestMouseIgnoredWhileConfirming(t *testing.T) {
	m := newTestModel()
	m.queue = queueOf(3)
	m.activateView(viewQueue)
	x, y := locate(t, m, "Song C")
	press(m, "c") // clear-queue confirmation
	click(m, x, y)
	if m.confirmFn == nil || m.queueCursor != 0 {
		t.Fatal("a click during a confirmation must do nothing")
	}
}
