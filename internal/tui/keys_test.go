package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/Gigoo25/youtube-music-tui/internal/api"
	"github.com/Gigoo25/youtube-music-tui/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var errFake = errors.New("network down")

// newTestModel builds a model without a real player/mpv process.
func newTestModel() *model {
	m := New(nil, &config.Config{Volume: 100})
	m.width, m.height = 80, 24
	return m
}

func key(s string) tea.KeyMsg {
	switch s {
	case " ":
		// Terminals report space with its rune attached; textinput inserts from
		// Runes, so include it.
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case "ctrl+w":
		return tea.KeyMsg{Type: tea.KeyCtrlW}
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
	if m.queuePos != 0 {
		t.Fatalf("queuePos = %d after removing the playing last track, want 0 (one before end)", m.queuePos)
	}
}

// TestQueueRemovePlayingTrackDoesNotSkipNext: removing the playing track must
// leave queuePos one before the track that shifted into its slot, so the next
// advance plays that track instead of skipping it.
func TestQueueRemovePlayingTrackDoesNotSkipNext(t *testing.T) {
	m := newTestModel()
	m.activeView = viewQueue
	m.focus = focusPanel
	m.queue = []api.Track{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}, {ID: "c", Title: "C"}}
	m.queuePos = 0 // A is playing
	m.hasCurrent = true
	m.queueCursor = 0 // cursor on the playing track

	press(m, "d") // remove A while it plays

	if m.queuePos != -1 {
		t.Fatalf("queuePos = %d after removing playing head, want -1 so the next advance plays B", m.queuePos)
	}
	if len(m.queue) != 2 || m.queue[0].ID != "b" {
		t.Fatalf("queue = %v, want [b c]", m.queue)
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

// TestShuffleRepeatToggle: s/r work outside the search input and mark the
// config dirty so the toggle survives a debounced mid-session save (not just a
// clean exit) — matching the auto-continue toggle.
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
	if !m.cfgDirty {
		t.Fatal("'s' should mark the config dirty so shuffle persists")
	}
	m.cfgDirty = false
	press(m, "r")
	if m.repeat != repeatAll {
		t.Fatalf("'r' should advance repeat to repeatAll, got %v", m.repeat)
	}
	if !m.cfgDirty {
		t.Fatal("'r' should mark the config dirty so repeat persists")
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

// TestStaleSearchResponseDropped: a slow response from an older search must not
// clobber the results of a newer one.
func TestStaleSearchResponseDropped(t *testing.T) {
	m := newTestModel()
	m.searchGen = 2
	m.searchResults = []api.Track{{ID: "new", Title: "New"}}
	m.Update(searchDoneMsg{gen: 1, tracks: []api.Track{{ID: "old", Title: "Old"}}})
	if len(m.searchResults) != 1 || m.searchResults[0].ID != "new" {
		t.Fatalf("stale search response overwrote newer results: %v", m.searchResults)
	}
	m.Update(searchDoneMsg{gen: 2, tracks: []api.Track{{ID: "cur", Title: "Cur"}}})
	if len(m.searchResults) != 1 || m.searchResults[0].ID != "cur" {
		t.Fatalf("current search response not applied: %v", m.searchResults)
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
	press(m, "esc") // contextual back
	if m.activeView != viewSearch {
		t.Fatalf("after esc activeView = %v, want viewSearch", m.activeView)
	}
}

// TestContextualBackChain: hopping album → artist → album must unwind with esc
// back to the originating view, not ping-pong between the two contextual views
// (regression: a single prevView slot got clobbered on the second hop).
func TestContextualBackChain(t *testing.T) {
	m := newTestModel()
	m.activeView = viewSearch
	m.focus = focusPanel
	m.searchResults = []api.Track{{ID: "s1", Title: "Song", Artist: "Foo", Album: "Bar"}}
	m.searchCursor = 0

	press(m, "a") // search → album
	if m.activeView != viewAlbum {
		t.Fatalf("activeView = %v, want viewAlbum", m.activeView)
	}
	m.albumLoading = false
	m.albumTracks = []api.Track{{ID: "t1", Title: "Track", Artist: "Foo", Album: "Bar"}}

	press(m, "A") // album → artist
	if m.activeView != viewArtist {
		t.Fatalf("activeView = %v, want viewArtist", m.activeView)
	}

	press(m, "esc") // artist → album
	if m.activeView != viewAlbum {
		t.Fatalf("after first esc activeView = %v, want viewAlbum", m.activeView)
	}
	press(m, "esc") // album → search (the originating view, not artist again)
	if m.activeView != viewSearch {
		t.Fatalf("after second esc activeView = %v, want viewSearch", m.activeView)
	}
}

// TestSidebarJumpClearsBackChain: direct navigation abandons the contextual
// return path, so a later esc doesn't resurrect a stale album/artist view.
func TestSidebarJumpClearsBackChain(t *testing.T) {
	m := newTestModel()
	m.activeView = viewSearch
	m.focus = focusPanel
	m.searchResults = []api.Track{{ID: "s1", Title: "Song", Artist: "Foo", Album: "Bar"}}
	m.searchCursor = 0

	press(m, "a") // search → album (pushes viewSearch)
	press(m, "3") // jump straight to queue
	if m.activeView != viewQueue {
		t.Fatalf("activeView = %v, want viewQueue", m.activeView)
	}
	if len(m.viewStack) != 0 {
		t.Fatalf("viewStack = %v, want empty after direct navigation", m.viewStack)
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

// TestArtistEnterQueuesNotReplaces: pressing enter on an artist song appends it
// to the existing queue (with the artist filled in) instead of replacing the
// queue and hijacking playback.
func TestArtistEnterQueuesNotReplaces(t *testing.T) {
	m := newTestModel()
	m.queue = []api.Track{{ID: "q1", Title: "Existing"}}
	m.queuePos = 0
	m.current = m.queue[0]
	m.hasCurrent = true // avoids playAt (nil player) and proves playback isn't disturbed
	m.activeView = viewArtist
	m.focus = focusPanel
	m.artistName = "The Artist"
	m.artistSongs = []api.Track{{ID: "a1", Title: "ArtSong"}}
	m.artistCursor = 0

	press(m, "enter")

	if len(m.queue) != 2 || m.queue[1].ID != "a1" {
		t.Fatalf("enter should append a1, got queue %v", m.queue)
	}
	if m.queue[1].Artist != "The Artist" {
		t.Fatalf("artist name not filled: %q", m.queue[1].Artist)
	}
	if m.current.ID != "q1" {
		t.Fatalf("playback disturbed: current = %q", m.current.ID)
	}
}

// TestAlbumEnterQueues: enter on an album track appends it (album name filled),
// matching the artist-view behavior.
func TestAlbumEnterQueues(t *testing.T) {
	m := newTestModel()
	m.queue = []api.Track{{ID: "q1", Title: "Existing"}}
	m.hasCurrent = true
	m.activeView = viewAlbum
	m.focus = focusPanel
	m.albumTitle = "Greatest Hits"
	m.albumTracks = []api.Track{{ID: "t1", Title: "Track 1"}}
	m.albumCursor = 0

	press(m, "enter")

	if len(m.queue) != 2 || m.queue[1].ID != "t1" {
		t.Fatalf("enter should append t1, got queue %v", m.queue)
	}
	if m.queue[1].Album != "Greatest Hits" {
		t.Fatalf("album name not filled: %q", m.queue[1].Album)
	}
}

// TestPlaylistSaveAndAppend: S names+saves the queue; the Playlists view's 'e'
// appends a saved playlist to the queue.
func TestPlaylistSaveAndAppend(t *testing.T) {
	m := newTestModel()
	m.queue = []api.Track{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}}

	press(m, "S")
	if !m.naming {
		t.Fatal("S should enter naming mode")
	}
	m.playlistInput.SetValue("Road Trip")
	press(m, "enter")
	if m.naming {
		t.Fatal("enter should leave naming mode")
	}
	if len(m.cfg.Playlists) != 1 || m.cfg.Playlists[0].Name != "Road Trip" ||
		len(m.cfg.Playlists[0].Tracks) != 2 {
		t.Fatalf("playlist not saved correctly: %v", m.cfg.Playlists)
	}

	// Append it onto a queue that's already playing (hasCurrent avoids playAt).
	m.queue = []api.Track{{ID: "x", Title: "X"}}
	m.hasCurrent = true
	m.activeView = viewPlaylists
	m.focus = focusPanel
	m.playlistCursor = 0
	press(m, "e")
	if len(m.queue) != 3 {
		t.Fatalf("e should append the 2 playlist tracks, got queue %v", m.queue)
	}
}

// TestSessionRestoreAndSnapshot: New restores a saved session; SnapshotSession
// writes the live state back for next time.
func TestSessionRestoreAndSnapshot(t *testing.T) {
	cfg := &config.Config{
		Volume:   100,
		Queue:    []api.Track{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		QueuePos: 2, Shuffle: true, Repeat: int(repeatAll),
	}
	m := New(nil, cfg)
	if len(m.queue) != 3 || m.queuePos != 2 || m.queueCursor != 2 {
		t.Fatalf("queue not restored: len=%d pos=%d cursor=%d", len(m.queue), m.queuePos, m.queueCursor)
	}
	if !m.shuffle || m.repeat != repeatAll {
		t.Fatalf("toggles not restored: shuffle=%v repeat=%v", m.shuffle, m.repeat)
	}

	m.queue = m.queue[:1]
	m.queuePos = 0
	m.shuffle = false
	m.repeat = repeatOne
	m.SnapshotSession()
	if len(m.cfg.Queue) != 1 || m.cfg.QueuePos != 0 || m.cfg.Shuffle || m.cfg.Repeat != int(repeatOne) {
		t.Fatalf("snapshot wrong: %+v", m.cfg)
	}
}

// TestClearHistoryConfirmation: 'c' in History asks for confirmation; 'y'
// clears history (and Listen Again), any other key cancels and keeps it.
func TestClearHistoryConfirmation(t *testing.T) {
	m := newTestModel()
	m.activeView = viewHistory
	m.focus = focusPanel
	m.cfg.History = []config.HistoryEntry{
		{Track: api.Track{ID: "a", Title: "A"}},
		{Track: api.Track{ID: "b", Title: "B"}},
	}
	m.refreshListenAgain()

	// Cancel path: any key other than y aborts.
	press(m, "c")
	if m.confirmFn == nil || m.confirmPrompt == "" {
		t.Fatal("'c' should arm a confirmation")
	}
	press(m, "j")
	if m.confirmFn != nil {
		t.Fatal("non-y key should cancel the confirmation")
	}
	if len(m.cfg.History) != 2 {
		t.Fatalf("cancel must keep history, got %d entries", len(m.cfg.History))
	}
	if m.historyCursor != 0 {
		t.Fatalf("the cancelling key must be consumed, not act (cursor=%d)", m.historyCursor)
	}

	// Confirm path.
	press(m, "c")
	press(m, "y")
	if len(m.cfg.History) != 0 {
		t.Fatalf("y should clear history, got %d entries", len(m.cfg.History))
	}
	if len(m.homeListenAgain) != 0 {
		t.Fatal("clearing history should also empty Listen Again")
	}
	if m.confirmFn != nil || m.confirmPrompt != "" {
		t.Fatal("confirmation state should be reset after running")
	}
}

// TestLoadMoreErrorRestoresToken: a failed load-more puts the consumed
// continuation token back so the next scroll-to-bottom retries instead of
// pagination dying on one transient failure.
func TestLoadMoreErrorRestoresToken(t *testing.T) {
	m := newTestModel()
	m.searchGen = 1
	m.searchResults = []api.Track{{ID: "a", Title: "A"}}
	m.searchContinuation = "tok"

	cmd := m.loadMoreSearch()
	if cmd == nil {
		t.Fatal("loadMoreSearch should fire with a token present")
	}
	if m.searchContinuation != "" || !m.searchMoreLoading {
		t.Fatalf("dispatch should consume token and set loading, got token=%q loading=%v",
			m.searchContinuation, m.searchMoreLoading)
	}
	// Guard against double-fire while in flight.
	if m.loadMoreSearch() != nil {
		t.Fatal("a second load-more while one is in flight must not fire")
	}

	m.Update(searchMoreMsg{gen: 1, token: "tok", err: errFake})
	if m.searchContinuation != "tok" {
		t.Fatalf("error should restore the token, got %q", m.searchContinuation)
	}
	if m.searchMoreLoading {
		t.Fatal("error should clear the loading flag")
	}
}

// TestSearchFooterStates: the pagination footer reflects loading / more / end,
// and is absent when there are no results.
func TestSearchFooterStates(t *testing.T) {
	m := newTestModel()
	if got := m.renderSearchFooter(60); got != "" {
		t.Fatalf("no results should yield no footer, got %q", got)
	}
	m.searchResults = []api.Track{{ID: "a", Title: "A"}}

	m.searchContinuation = "tok"
	m.searchMoreLoading = false
	if !strings.Contains(m.renderSearchFooter(60), "more") {
		t.Errorf("with a continuation token the footer should mention more: %q", m.renderSearchFooter(60))
	}

	m.searchMoreLoading = true
	if !strings.Contains(m.renderSearchFooter(60), "loading") {
		t.Errorf("while loading the footer should say loading: %q", m.renderSearchFooter(60))
	}

	m.searchMoreLoading = false
	m.searchContinuation = ""
	if !strings.Contains(m.renderSearchFooter(60), "end of results") {
		t.Errorf("exhausted results should show end-of-results: %q", m.renderSearchFooter(60))
	}
}

// TestViewDimensions: every view renders to exactly the terminal size, even with
// wide (CJK) glyphs and overlong titles. A row that overflows its width is
// wrapped by lipgloss onto an extra (background-colored) line, corrupting the
// layout in a real terminal — so width and height must both match exactly.
func TestViewDimensions(t *testing.T) {
	nasty := []api.Track{
		{ID: "x1", Title: "Song A", Artist: "Artist A", Album: "Album A", Duration: "3:21"},
		{ID: "x2", Title: "夜に駆ける 夜に駆ける 夜に駆ける 夜に駆ける 夜に駆ける", Artist: "YOASOBIとずっと真夜中でいいのに。", Album: "アルバム", Duration: "4:23"},
		{ID: "x3", Title: strings.Repeat("VeryLongTitle ", 20), Artist: strings.Repeat("LongArtist ", 10), Duration: "12:34"},
	}
	for _, size := range [][2]int{{80, 24}, {120, 40}, {46, 16}} {
		m := newTestModel()
		m.width, m.height = size[0], size[1]
		m.queue = append([]api.Track(nil), nasty...)
		m.searchResults = m.queue
		m.homeListenAgain = m.queue
		m.albumTracks = m.queue
		m.albumTitle = nasty[1].Title
		m.artistName = nasty[1].Artist
		m.artistSongs = m.queue
		m.artistAlbums = []api.AlbumRef{{ID: "MPREb1", Title: nasty[1].Title, Year: "2020"}}
		m.cfg.History = []config.HistoryEntry{{Track: nasty[1]}}
		m.cfg.Playlists = []config.Playlist{{Name: nasty[1].Title, Tracks: m.queue}}
		m.current = nasty[1]
		m.hasCurrent = true
		m.queuePos = 1
		for _, v := range []view{viewHome, viewSearch, viewQueue, viewFavorites, viewHistory, viewAlbum, viewArtist, viewGenres, viewPlaylists, viewHelp} {
			m.activeView = v
			m.queueCursor, m.homeCursor, m.searchCursor = 1, 1, 1
			m.albumCursor, m.artistCursor, m.historyCursor = 1, 1, 0
			m.playlistCursor = 0
			for _, focus := range []focusArea{focusPanel, focusSidebar} {
				m.focus = focus
				m.sbCache, m.scCache = "", ""
				out := m.View()
				if gotH := lipgloss.Height(out); gotH != m.height {
					t.Errorf("%dx%d view %v focus %v: height %d, want %d", m.width, m.height, v, focus, gotH, m.height)
				}
				if gotW := lipgloss.Width(out); gotW != m.width {
					t.Errorf("%dx%d view %v focus %v: width %d, want %d", m.width, m.height, v, focus, gotW, m.width)
				}
			}
		}
	}
}

// TestStatusLineDoesNotShiftLayout: a transient status appearing/expiring must
// not change the overall height (the line is always reserved).
func TestStatusLineDoesNotShiftLayout(t *testing.T) {
	m := newTestModel()
	m.queue = []api.Track{{ID: "x", Title: "Song A", Artist: "Artist A"}}
	m.current = m.queue[0]
	m.hasCurrent = true
	m.activeView = viewQueue

	without := m.View()
	m.setStatus("queued: Song A")
	with := m.View()
	if lipgloss.Height(without) != lipgloss.Height(with) {
		t.Fatalf("status changed layout height: %d -> %d",
			lipgloss.Height(without), lipgloss.Height(with))
	}
	if lipgloss.Height(with) != m.height {
		t.Fatalf("height with status = %d, want %d", lipgloss.Height(with), m.height)
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
	for _, v := range []view{viewHome, viewSearch, viewQueue, viewFavorites, viewHistory, viewAlbum, viewArtist, viewGenres, viewPlaylists, viewHelp} {
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

// ── Playlist detail / add-to-playlist flows ─────────────────────────────────────

func TestPlaylistDetailViewAndRemove(t *testing.T) {
	m := newTestModel()
	m.cfg.Playlists = []config.Playlist{{Name: "road trip", Tracks: []api.Track{
		{ID: "id1", Title: "One", Artist: "A"},
		{ID: "id2", Title: "Two", Artist: "B"},
	}}}
	m.activeView = viewPlaylists
	m.focus = focusPanel

	press(m, "enter")
	if m.activeView != viewPlaylistDetail || m.openPlaylist != "road trip" {
		t.Fatalf("enter did not open playlist detail: view=%v open=%q", m.activeView, m.openPlaylist)
	}
	if !strings.Contains(m.View(), "road trip") {
		t.Fatal("detail header missing")
	}

	press(m, "j") // select Two
	press(m, "d")
	pl := m.cfg.PlaylistByName("road trip")
	if len(pl.Tracks) != 1 || pl.Tracks[0].ID != "id1" {
		t.Fatalf("remove failed: %+v", pl.Tracks)
	}

	press(m, "esc")
	if m.activeView != viewPlaylists {
		t.Fatalf("esc did not return to playlists: %v", m.activeView)
	}
}

func TestAddToPlaylistPicker(t *testing.T) {
	m := newTestModel()
	m.cfg.Playlists = []config.Playlist{{Name: "mix", Tracks: []api.Track{{ID: "x", Title: "X"}}}}
	m.searchResults = []api.Track{{ID: "id9", Title: "Nine", Artist: "N"}}
	m.activeView = viewSearch
	m.focus = focusPanel

	press(m, "P")
	if m.activeView != viewPlaylistPick || m.pickTrack.ID != "id9" {
		t.Fatalf("P did not open picker: view=%v track=%+v", m.activeView, m.pickTrack)
	}

	press(m, "enter") // first row = "mix"
	if m.activeView != viewSearch {
		t.Fatalf("picker did not return to origin view: %v", m.activeView)
	}
	pl := m.cfg.PlaylistByName("mix")
	if len(pl.Tracks) != 2 || pl.Tracks[1].ID != "id9" {
		t.Fatalf("track not added: %+v", pl.Tracks)
	}

	// Same again: dedupe, no double add.
	press(m, "P")
	press(m, "enter")
	if pl := m.cfg.PlaylistByName("mix"); len(pl.Tracks) != 2 {
		t.Fatalf("duplicate added: %+v", pl.Tracks)
	}
}

func TestAddToNewPlaylistViaNaming(t *testing.T) {
	m := newTestModel()
	m.searchResults = []api.Track{{ID: "id5", Title: "Five"}}
	m.activeView = viewSearch
	m.focus = focusPanel

	press(m, "P")
	press(m, "j") // move to "new playlist…" (no playlists exist: row 0 is it already; j clamps)
	press(m, "enter")
	if !m.naming || m.nameTrack == nil {
		t.Fatalf("new-playlist row did not open naming: naming=%v", m.naming)
	}
	for _, r := range "fresh" {
		press(m, string(r))
	}
	press(m, "enter")
	pl := m.cfg.PlaylistByName("fresh")
	if pl == nil || len(pl.Tracks) != 1 || pl.Tracks[0].ID != "id5" {
		t.Fatalf("naming flow did not create playlist: %+v", pl)
	}
}

// ── Navigation consistency ──────────────────────────────────────────────────────

func TestContextualBackWithH(t *testing.T) {
	m := newTestModel()
	// Album opened from search: h steps back to search, like esc.
	m.activeView = viewSearch
	m.viewStack = []view{viewSearch}
	m.activeView = viewAlbum
	m.focus = focusPanel
	press(m, "h")
	if m.activeView != viewSearch || m.focus != focusPanel {
		t.Fatalf("h in album did not step back: view=%v focus=%v", m.activeView, m.focus)
	}

	// Playlist detail: h steps back to the playlist list.
	m.cfg.Playlists = []config.Playlist{{Name: "mix", Tracks: []api.Track{{ID: "a", Title: "A"}}}}
	m.activeView = viewPlaylists
	press(m, "enter")
	if m.activeView != viewPlaylistDetail {
		t.Fatalf("setup: detail not opened: %v", m.activeView)
	}
	press(m, "h")
	if m.activeView != viewPlaylists {
		t.Fatalf("h in playlist detail did not step back: %v", m.activeView)
	}

	// Top-level view: h hands focus to the sidebar.
	m.activeView = viewFavorites
	m.focus = focusPanel
	press(m, "h")
	if m.focus != focusSidebar {
		t.Fatalf("h in top-level view did not focus sidebar: %v", m.focus)
	}
}

func TestHistoryRemoveEntry(t *testing.T) {
	m := newTestModel()
	m.cfg.History = []config.HistoryEntry{
		{Track: api.Track{ID: "one", Title: "One"}},
		{Track: api.Track{ID: "two", Title: "Two"}},
	}
	m.activeView = viewHistory
	m.focus = focusPanel
	press(m, "j") // select second entry
	press(m, "d")
	if len(m.cfg.History) != 1 || m.cfg.History[0].Track.ID != "one" {
		t.Fatalf("history remove failed: %+v", m.cfg.History)
	}
}

func TestArtistAlbumOpensWithL(t *testing.T) {
	m := newTestModel()
	m.activeView = viewArtist
	m.focus = focusPanel
	m.artistSongs = []api.Track{{ID: "s1", Title: "Song"}}
	m.artistAlbums = []api.AlbumRef{{ID: "MPREb_x", Title: "Alb"}}
	m.artistCursor = 1 // album row (after the one song)
	press(m, "l")
	if m.activeView != viewAlbum {
		t.Fatalf("l on artist album did not open album view: %v", m.activeView)
	}
	// On a song row, l must not open anything.
	m2 := newTestModel()
	m2.activeView = viewArtist
	m2.focus = focusPanel
	m2.artistSongs = []api.Track{{ID: "s1", Title: "Song"}}
	m2.artistCursor = 0
	press(m2, "l")
	if m2.activeView != viewArtist {
		t.Fatalf("l on song row changed view: %v", m2.activeView)
	}
}

// TestSearchTypingCtrlUClears: ctrl+u in the search box clears the query
// (vim/readline kill-line), so starting a fresh search is one keystroke. The
// binding comes from bubbles' textinput defaults — this guards that the key
// handler keeps passing it through while typing.
func TestSearchTypingCtrlUClears(t *testing.T) {
	m := newTestModel()
	m.activeView = viewSearch
	m.focus = focusPanel
	m.searchTyping = true
	m.searchInput.Focus()
	for _, r := range "old query" {
		press(m, string(r))
	}
	if m.searchInput.Value() != "old query" {
		t.Fatalf("setup: query = %q", m.searchInput.Value())
	}

	press(m, "ctrl+u")
	if m.searchInput.Value() != "" {
		t.Fatalf("ctrl+u did not clear the query: %q", m.searchInput.Value())
	}

	// ctrl+w deletes the word before the cursor.
	for _, r := range "two words" {
		press(m, string(r))
	}
	press(m, "ctrl+w")
	if m.searchInput.Value() != "two " {
		t.Fatalf("ctrl+w did not delete last word: %q", m.searchInput.Value())
	}
}

// TestSearchEscDropsContinuation: esc-clearing search results must also drop the
// pagination token — otherwise j/k on the now-empty list fetches a page of the
// old query as orphan results.
func TestSearchEscDropsContinuation(t *testing.T) {
	m := newTestModel()
	m.activeView = viewSearch
	m.focus = focusPanel
	m.searchResults = []api.Track{{ID: "s1", Title: "Song"}}
	m.searchContinuation = "tok123"

	press(m, "esc")
	if len(m.searchResults) != 0 {
		t.Fatalf("esc did not clear results: %v", m.searchResults)
	}
	if m.searchContinuation != "" {
		t.Fatalf("esc left a stale continuation token: %q", m.searchContinuation)
	}

	// Belt and braces: even with a stale token, moving on an empty list must
	// not fire a load-more.
	m.searchContinuation = "tok123"
	_, cmd := m.handleKey(key("j"))
	if cmd != nil {
		t.Fatal("j on empty results fired a load-more command")
	}
}

// TestPickerReopenKeepsReturnView: P inside the add-to-playlist picker (with a
// playing track) re-targets it but must keep the original return view — pickPrev
// pointing at the picker itself made esc loop forever.
func TestPickerReopenKeepsReturnView(t *testing.T) {
	m := newTestModel()
	m.activeView = viewQueue
	m.focus = focusPanel
	m.queue = []api.Track{{ID: "a", Title: "A"}}
	m.queueCursor = 0
	m.current = m.queue[0]
	m.hasCurrent = true

	press(m, "P") // queue → picker
	if m.activeView != viewPlaylistPick || m.pickPrev != viewQueue {
		t.Fatalf("picker setup: view=%v pickPrev=%v", m.activeView, m.pickPrev)
	}
	press(m, "P") // P again inside the picker
	if m.pickPrev != viewQueue {
		t.Fatalf("pickPrev clobbered to %v, want viewQueue", m.pickPrev)
	}
	press(m, "esc")
	if m.activeView != viewQueue {
		t.Fatalf("esc did not leave the picker: %v", m.activeView)
	}
}

// TestPlaylistDeleteConfirms: deleting a playlist asks for confirmation; y
// deletes, anything else keeps it.
func TestPlaylistDeleteConfirms(t *testing.T) {
	m := newTestModel()
	m.cfg.Playlists = []config.Playlist{{Name: "keep", Tracks: []api.Track{{ID: "a", Title: "A"}}}}
	m.activeView = viewPlaylists
	m.focus = focusPanel

	press(m, "d")
	if m.cfg.PlaylistByName("keep") == nil {
		t.Fatal("playlist deleted without confirmation")
	}
	if m.confirmFn == nil {
		t.Fatal("d on a playlist did not arm a confirmation")
	}
	press(m, "n") // decline
	if m.cfg.PlaylistByName("keep") == nil {
		t.Fatal("declined confirmation still deleted the playlist")
	}

	press(m, "d")
	press(m, "y") // confirm
	if m.cfg.PlaylistByName("keep") != nil {
		t.Fatal("confirmed delete did not remove the playlist")
	}
}

func TestPlaylistDetailFilter(t *testing.T) {
	m := newTestModel()
	m.cfg.Playlists = []config.Playlist{{Name: "mix", Tracks: []api.Track{
		{ID: "a", Title: "Alpha"},
		{ID: "b", Title: "Beta"},
		{ID: "g", Title: "Gamma"},
	}}}
	m.activeView = viewPlaylistDetail
	m.openPlaylist = "mix"
	m.focus = focusPanel
	m.filter = "gam" // as if typed via "/"
	m.plDetailCursor = 0
	press(m, "d") // must remove Gamma (the only visible row), not Alpha
	pl := m.cfg.PlaylistByName("mix")
	if len(pl.Tracks) != 2 || pl.Tracks[0].ID != "a" || pl.Tracks[1].ID != "b" {
		t.Fatalf("filtered remove hit wrong track: %+v", pl.Tracks)
	}
}
