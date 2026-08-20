package tui

import (
	"testing"

	"github.com/Gigoo25/youtube-music-tui/internal/api"
	"github.com/Gigoo25/youtube-music-tui/internal/config"
)

func TestRenderPanelViewsDoNotPanic(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	m.queue = []api.Track{{ID: "q", Title: "Queue", Artist: "Artist"}}
	m.cfg.Favorites = append([]api.Track(nil), m.queue...)
	m.cfg.History = []config.HistoryEntry{{Track: m.queue[0]}}
	m.albumTracks = m.queue
	m.searchResults = m.queue
	m.artistSongs = m.queue
	m.artistAlbums = []api.AlbumRef{{ID: "al", Title: "Album", Artist: "Artist"}}
	m.cfg.SavePlaylist("List", m.queue)
	m.openPlaylist = "List"
	m.pickTrack = m.queue[0]
	m.genreCursor = 0

	for _, v := range []view{
		viewHome, viewArtist, viewSearch, viewQueue, viewFavorites, viewHistory,
		viewAlbum, viewGenres, viewPlaylists, viewPlaylistDetail, viewPlaylistPick, viewHelp,
	} {
		m.activeView = v
		if got := m.renderPanelBody(60, 18); got == "" {
			t.Fatalf("renderPanelBody(%v) returned empty output", v)
		}
	}
}
