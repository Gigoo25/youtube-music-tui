package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/Gigoo25/youtube-music-tui/internal/api"
	"github.com/Gigoo25/youtube-music-tui/internal/config"
)

func TestRenderNarrowDimensions(t *testing.T) {
	nasty := []api.Track{
		{ID: "x1", Title: strings.Repeat("VeryLongTitle ", 20), Artist: strings.Repeat("LongArtist ", 10), Duration: "12:34"},
		{ID: "x2", Title: "夜に駆ける 夜に駆ける 夜に駆ける", Artist: "YOASOBIとずっと真夜中でいいのに。", Album: "アルバム", Duration: "4:23"},
	}
	for w := 8; w <= 40; w++ {
		for _, h := range []int{10, 16, 24} {
			m := newTestModel()
			m.width, m.height = w, h
			m.queue = append([]api.Track(nil), nasty...)
			m.searchResults = m.queue
			m.homeListenAgain = m.queue
			m.albumTracks, m.albumTitle = m.queue, nasty[0].Title
			m.artistSongs, m.artistName = m.queue, nasty[1].Artist
			m.artistAlbums = []api.AlbumRef{{ID: "a", Title: nasty[0].Title, Year: "2020"}}
			m.cfg.History = []config.HistoryEntry{{Track: nasty[1]}}
			m.cfg.Playlists = []config.Playlist{{Name: nasty[0].Title, Tracks: m.queue}}
			m.cfg.Favorites = m.queue
			m.current, m.hasCurrent, m.queuePos = nasty[1], true, 1
			m.searchTyping = true
			m.searchInput.SetValue(strings.Repeat("query ", 20))
			for _, v := range []view{viewHome, viewSearch, viewQueue, viewFavorites, viewHistory, viewAlbum, viewArtist, viewGenres, viewPlaylists, viewPlaylistPick, viewHelp} {
				m.activeView = v
				m.pickTrack = nasty[0]
				for _, focus := range []focusArea{focusPanel, focusSidebar} {
					m.focus = focus
					m.sbCache, m.scCache = "", ""
					out := m.View()
					if got := lipgloss.Height(out); got != h {
						t.Fatalf("%dx%d view %v focus %v: height %d", w, h, v, focus, got)
					}
					if got := lipgloss.Width(out); got != w {
						t.Fatalf("%dx%d view %v focus %v: width %d", w, h, v, focus, got)
					}
				}
			}
		}
	}
}

// Help must reach its last section: with enough height every binding is present.
func TestRenderHelpFitsAndShowsQuit(t *testing.T) {
	m := newTestModel()
	m.activeView = viewHelp
	for _, size := range [][2]int{{40, 8}, {60, 20}, {100, 60}} {
		out := m.renderHelp(size[0], size[1])
		if lipgloss.Height(out) > size[1] {
			t.Fatalf("%v: help height %d > %d", size, lipgloss.Height(out), size[1])
		}
		if lipgloss.Width(out) > size[0] {
			t.Fatalf("%v: help width %d > %d", size, lipgloss.Width(out), size[0])
		}
	}
	if !strings.Contains(m.renderHelp(100, 60), "quit") {
		t.Fatal("tall help should reach the App section")
	}
}

// Home/artist windowing keeps the selection visible at either end of the list.
func TestRenderFlatWindowKeepsSelection(t *testing.T) {
	m := newTestModel()
	m.focus = focusPanel
	var songs []api.Track
	for i := range 40 {
		songs = append(songs, api.Track{ID: string(rune('a' + i%26)), Title: "Song" + strings.Repeat("!", i%3) + fmtDur(float64(i)), Duration: "1:00"})
	}
	m.homeListenAgain = songs
	m.artistSongs = songs
	m.artistName = "A"
	m.artistAlbums = []api.AlbumRef{{ID: "z", Title: "Alb", Year: "1999"}}
	for _, c := range []int{0, 1, 20, 39} {
		m.homeCursor, m.artistCursor = c, c
		for _, fn := range []func(int, int) string{m.renderHome, m.renderArtist} {
			out := fn(60, 10)
			if lipgloss.Height(out) > 10 {
				t.Fatalf("cursor %d: height %d", c, lipgloss.Height(out))
			}
			if !strings.Contains(out, songs[c].Title) {
				t.Fatalf("cursor %d: selected row %q not in window:\n%s", c, songs[c].Title, out)
			}
		}
	}
	// Artist: the flat cursor past the songs selects the album row.
	m.artistCursor = len(songs)
	if out := m.renderArtist(60, 10); !strings.Contains(out, "Alb") {
		t.Fatalf("album selection not visible:\n%s", out)
	}

}

// TestHelpScrollsFromFirstPress guards F27: helpCursor is a scroll offset, not a
// centred cursor. Passing it to a centring window swallowed every press until it
// passed half a screen, so the first few j presses looked dead.
func TestHelpScrollsFromFirstPress(t *testing.T) {
	m := newTestModel()
	h := helpRowCount() / 2 // guarantee the body is taller than the window
	// Line 0 is the box border; line 1 is the first content row.
	firstRow := func(s string) string {
		rows := strings.Split(s, "\n")
		if len(rows) < 2 {
			t.Fatalf("renderHelp emitted %d rows, want a bordered body", len(rows))
		}
		return rows[1]
	}

	m.helpCursor = 0
	first := firstRow(m.renderHelp(80, h))
	m.helpCursor = 1
	second := firstRow(m.renderHelp(80, h))

	if first == second {
		t.Fatalf("helpCursor 0 and 1 render the same first row (%q): help does not scroll on the first press", first)
	}
}
