package tui

import (
	"testing"

	"github.com/Gigoo25/youtube-music-tui/internal/api"
	"github.com/Gigoo25/youtube-music-tui/internal/config"
)

func TestFilterHelpers(t *testing.T) {
	m := newTestModel()
	tracks := []api.Track{{ID: "a", Title: "Hello", Artist: "World"}, {ID: "b", Title: "Other"}}
	if got := m.filt(tracks); len(got) != len(tracks) {
		t.Fatalf("unfiltered tracks = %v", got)
	}
	m.activeView, m.filter = viewQueue, "world"
	if got := m.filt(tracks); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("filtered tracks = %v", got)
	}
	if got := m.trackVisibleIndices(tracks); len(got) != 1 || got[0] != 0 {
		t.Fatalf("visible indices = %v", got)
	}

	albums := []api.AlbumRef{{Title: "First", Artist: "Artist"}, {Title: "Second"}}
	m.filter = "second"
	if got := m.filtAlbums(albums); len(got) != 1 || got[0].Title != "Second" {
		t.Fatalf("filtered albums = %v", got)
	}
	history := []config.HistoryEntry{{Track: tracks[0]}, {Track: tracks[1]}}
	m.filter = "hello"
	if got := m.filtHistory(history); len(got) != 1 || got[0].Track.ID != "a" {
		t.Fatalf("filtered history = %v", got)
	}
}

func TestCursorAndLengthHelpers(t *testing.T) {
	m := newTestModel()
	for _, v := range []view{viewHome, viewQueue, viewFavorites, viewHistory, viewAlbum, viewArtist, viewPlaylists, viewPlaylistDetail} {
		m.activeView = v
		if m.activeCursorPtr() == nil {
			t.Fatalf("activeCursorPtr(%v) = nil", v)
		}
	}
	m.activeView = viewHelp
	if m.activeCursorPtr() != nil || m.activeFilteredLen() != 0 {
		t.Fatal("non-list view has cursor or selectable rows")
	}
}
