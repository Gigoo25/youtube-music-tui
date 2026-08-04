package tui

import (
	"testing"

	"github.com/Gigoo25/youtube-music-tui/internal/api"
)

// TestFilterClearedOnViewSwitch: every view transition must clear the local
// "/" filter, or the new view opens showing a filtered subset with no filter
// line visible. Without this, a filter applied in the queue view would carry
// over to the favorites view on switch, showing a cryptic empty (or near-
// empty) list with no indication of why.
func TestFilterClearedOnViewSwitch(t *testing.T) {
	m := newTestModel()
	m.filter = "zzz"

	views := []view{
		viewHome,
		viewQueue,
		viewFavorites,
		viewHistory,
		viewAlbum,
		viewArtist,
		viewPlaylists,
	}

	for _, v := range views {
		m.filter = "zzz" // reset for each iteration
		m.activateView(v)
		if m.filter != "" {
			t.Errorf("after activateView(%d), filter = %q, want empty", v, m.filter)
		}
	}
}

// TestFilterClearedOnPlaylistPicker: opening the add-to-playlist picker must
// also clear the filter. Without this, the picker would open filtered, and
// the user couldn't find the playlist they're looking for.
func TestFilterClearedOnPlaylistPicker(t *testing.T) {
	m := newTestModel()
	m.filter = "zzz"

	m.openPlaylistPicker(api.Track{ID: "x", Title: "X"})

	if m.filter != "" {
		t.Fatalf("after openPlaylistPicker, filter = %q, want \"\"", m.filter)
	}
}
