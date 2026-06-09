package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rob/ytmusic/internal/api"
	"github.com/rob/ytmusic/internal/config"
)

// newTestModel builds a model without a real player/mpv process.
func newTestModel() *model {
	m := New(nil, &config.Config{Volume: 100})
	m.width, m.height = 80, 24
	return m
}

func key(s string) tea.KeyMsg {
	switch s {
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(m *model, s string) {
	m.handleKey(key(s))
}

// TestQueueRemovalKeepsPlayingTrack: deleting a track above the playing one
// shifts queuePos so the now-playing marker stays on the same song.
func TestQueueRemovalKeepsPlayingTrack(t *testing.T) {
	m := newTestModel()
	m.activeView = viewQueue
	m.focus = focusPanel
	m.queue = []api.Track{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}, {ID: "c", Title: "C"}}
	m.queuePos = 1 // B is playing
	m.hasCurrent = true
	m.queueCursor = 0 // cursor on A

	press(m, "d") // remove A

	if len(m.queue) != 2 || m.queue[m.queuePos].ID != "b" {
		t.Fatalf("expected B still at queuePos after removing A, got pos=%d queue=%v", m.queuePos, m.queue)
	}
	if !m.hasCurrent {
		t.Fatal("removing a non-playing track should keep hasCurrent")
	}
}

// TestQueueRemovePlayingTrackDropsMarker: removing the playing track clears the
// now-playing marker (audio keeps going, but it's no longer in the queue).
func TestQueueRemovePlayingTrackDropsMarker(t *testing.T) {
	m := newTestModel()
	m.activeView = viewQueue
	m.focus = focusPanel
	m.queue = []api.Track{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}}
	m.queuePos = 1
	m.hasCurrent = true
	m.queueCursor = 1 // cursor on the playing track

	press(m, "d")

	if m.hasCurrent {
		t.Fatal("removing the playing track should clear hasCurrent")
	}
	if m.queuePos >= len(m.queue) {
		t.Fatalf("queuePos %d out of range after removal (len %d)", m.queuePos, len(m.queue))
	}
}

// TestCurrentIsValueNotSlicePointer: m.current must survive queue reallocation.
func TestCurrentIsValueNotSlicePointer(t *testing.T) {
	m := newTestModel()
	m.queue = []api.Track{{ID: "x", Title: "X", Artist: "Ax"}}
	m.current = m.queue[0]
	m.hasCurrent = true
	// Force a reallocation of the queue backing array.
	for i := 0; i < 100; i++ {
		m.queue = append(m.queue, api.Track{ID: "n", Title: "N"})
	}
	if m.current.Title != "X" || m.current.Artist != "Ax" {
		t.Fatalf("current changed after queue growth: %+v", m.current)
	}
}

// TestViewSwitching: number keys route to views when not typing.
func TestViewSwitching(t *testing.T) {
	m := newTestModel()
	press(m, "1")
	if m.activeView != viewHome {
		t.Fatalf("expected viewHome, got %v", m.activeView)
	}
	press(m, "3")
	if m.activeView != viewQueue {
		t.Fatalf("expected viewQueue, got %v", m.activeView)
	}
	press(m, "4")
	if m.activeView != viewFavorites {
		t.Fatalf("expected viewFavorites, got %v", m.activeView)
	}
	press(m, "5")
	if m.activeView != viewHistory {
		t.Fatalf("expected viewHistory, got %v", m.activeView)
	}
}

// TestShuffleRepeatToggle: s/r work outside the search input.
func TestShuffleRepeatToggle(t *testing.T) {
	m := newTestModel()
	press(m, "esc") // leave typing
	if m.shuffle {
		t.Fatal("shuffle should start off")
	}
	press(m, "s")
	if !m.shuffle {
		t.Fatal("'s' should toggle shuffle on")
	}
	press(m, "r")
	if m.repeat != repeatAll {
		t.Fatalf("'r' should advance repeat to repeatAll, got %v", m.repeat)
	}
}

// TestPlaybackKeysDoNotFireWhileTyping: 's' typed into the global search box must
// not toggle shuffle.
func TestPlaybackKeysDoNotFireWhileTyping(t *testing.T) {
	m := newTestModel()
	press(m, "2") // global Search view
	press(m, "/") // focus the query input -> typing
	if !m.typing() {
		t.Fatal("expected typing mode after '/' in Search view")
	}
	press(m, "s")
	if m.shuffle {
		t.Fatal("'s' while typing must not toggle shuffle")
	}
}

// TestLocalFilterCapturesKeys: '/' filters the current pane; typed keys go to the
// filter (not playback) and narrow the list.
func TestLocalFilterCapturesKeys(t *testing.T) {
	m := newTestModel()
	m.activeView = viewQueue
	m.focus = focusPanel
	m.queue = []api.Track{
		{ID: "a", Title: "Yellow", Artist: "Coldplay"},
		{ID: "b", Title: "Bohemian Rhapsody", Artist: "Queen"},
	}
	press(m, "/")
	if !m.filtering {
		t.Fatal("expected filtering mode after '/'")
	}
	press(m, "s") // goes to the filter, not shuffle
	if m.shuffle {
		t.Fatal("'s' while filtering must not toggle shuffle")
	}
	press(m, "p") // build filter "sp"... actually type to match Coldplay
	m.filter = "cold"
	if got := m.activeFilteredLen(); got != 1 {
		t.Fatalf("filtered queue len = %d, want 1", got)
	}
	press(m, "esc") // clears filter
	if m.filter != "" || m.filtering {
		t.Fatal("esc should clear the filter")
	}
}

// TestStartsAtHome: the app opens on the Home view (not typing).
func TestStartsAtHome(t *testing.T) {
	m := newTestModel()
	if m.activeView != viewHome {
		t.Fatalf("expected to start on viewHome, got %v", m.activeView)
	}
	if m.typing() {
		t.Fatal("should not start in search-typing mode")
	}
}

// TestHomeNavSpansSections: the Home cursor moves across Listen Again into
// Quick Picks as one flat list, and contextTrack resolves the right track.
func TestHomeNavSpansSections(t *testing.T) {
	m := newTestModel()
	m.activeView = viewHome
	m.focus = focusPanel
	m.homeListenAgain = []api.Track{{ID: "la1", Title: "LA1", Artist: "X"}}
	m.homeQuickPicks = []api.Track{{ID: "qp1", Title: "QP1", Artist: "Y"}}

	if got := m.homeLen(); got != 2 {
		t.Fatalf("homeLen = %d, want 2", got)
	}
	press(m, "j") // into Quick Picks
	if m.homeCursor != 1 {
		t.Fatalf("homeCursor = %d, want 1", m.homeCursor)
	}
	if ct := m.contextTrack(); ct == nil || ct.ID != "qp1" {
		t.Fatalf("contextTrack = %v, want qp1", ct)
	}
}

// TestGoToArtistRouting: 'A' opens the artist view for the selection's primary
// artist and remembers where to return.
func TestGoToArtistRouting(t *testing.T) {
	m := newTestModel()
	m.activeView = viewSearch
	m.focus = focusPanel
	m.searchResults = []api.Track{{ID: "s1", Title: "Song", Artist: "Foo & Bar"}}
	m.searchCursor = 0

	press(m, "A")
	if m.activeView != viewArtist {
		t.Fatalf("activeView = %v, want viewArtist", m.activeView)
	}
	if m.artistName != "Foo" {
		t.Fatalf("artistName = %q, want Foo", m.artistName)
	}
	if m.prevView != viewSearch {
		t.Fatalf("prevView = %v, want viewSearch", m.prevView)
	}

	press(m, "esc") // contextual back
	if m.activeView != viewSearch {
		t.Fatalf("after esc activeView = %v, want viewSearch", m.activeView)
	}
}

func TestFirstArtistOf(t *testing.T) {
	cases := map[string]string{
		"Foo":           "Foo",
		"Foo & Bar":     "Foo",
		"Foo, Bar":      "Foo",
		"Foo feat. Baz": "Foo",
		"Foo x Bar":     "Foo",
	}
	for in, want := range cases {
		if got := firstArtistOf(in); got != want {
			t.Errorf("firstArtistOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestThemeCycle: 'T' advances and persists the color theme.
func TestThemeCycle(t *testing.T) {
	m := newTestModel()
	start := m.themeIdx
	press(m, "T")
	if m.themeIdx != (start+1)%len(themes) {
		t.Fatalf("themeIdx = %d, want %d", m.themeIdx, (start+1)%len(themes))
	}
	if m.cfg.Theme != themes[m.themeIdx].name {
		t.Fatalf("cfg.Theme = %q, want %q", m.cfg.Theme, themes[m.themeIdx].name)
	}
}

// TestViewsRenderWithoutPanic: every view renders.
func TestViewsRenderWithoutPanic(t *testing.T) {
	m := newTestModel()
	m.queue = []api.Track{{ID: "x", Title: "Song A", Artist: "Artist A", Duration: "3:21"}}
	m.searchResults = m.queue
	m.homeListenAgain = m.queue
	m.artistName = "Artist A"
	m.artistSongs = m.queue
	m.artistAlbums = []api.AlbumRef{{ID: "MPREb1", Title: "Album One", Year: "2020"}}
	for _, v := range []view{viewHome, viewSearch, viewQueue, viewFavorites, viewHistory, viewAlbum, viewArtist, viewGenres, viewHelp} {
		m.activeView = v
		out := m.View()
		if out == "" {
			t.Fatalf("view %v rendered empty", v)
		}
	}
	// The persistent sidebar should always be present.
	if !strings.Contains(m.View(), "Quick Links") {
		t.Error("sidebar missing from view")
	}
}
