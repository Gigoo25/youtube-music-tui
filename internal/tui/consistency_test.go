package tui

import (
	"strings"
	"testing"

	"github.com/Gigoo25/youtube-music-tui/internal/api"
)

// TestHelpBackKeysReturnToOrigin: Help is contextual — "?" already returned to
// where it was opened from, but h/esc dumped focus on the sidebar and left Help
// on screen. All three must agree.
func TestHelpBackKeysReturnToOrigin(t *testing.T) {
	for _, k := range []string{"?", "h", "esc"} {
		m := newTestModel()
		m.activateView(viewQueue)
		press(m, "?")
		if m.activeView != viewHelp {
			t.Fatalf("? did not open help")
		}
		press(m, k)
		if m.activeView != viewQueue || m.focus != focusPanel {
			t.Fatalf("%q in help: view=%v focus=%v, want Queue with the panel focused", k, m.activeView, m.focus)
		}
	}
}

// TestShortcutsBarBackLabel: the bar's h/esc hint must describe what those keys
// do in the view — "back" where backFromContextual steps back, "menu" elsewhere.
func TestShortcutsBarBackLabel(t *testing.T) {
	m := newTestModel()
	m.focus = focusPanel
	for _, v := range []view{viewHome, viewSearch, viewQueue, viewFavorites, viewHistory, viewPlaylists} {
		m.activeView = v
		if bar := m.buildShortcutsBar(400); !strings.Contains(bar, "[h] menu") {
			t.Errorf("view %v: bar lacks [h] menu", v)
		}
	}
	for _, v := range []view{viewAlbum, viewArtist, viewGenres, viewHelp, viewPlaylistDetail, viewPlaylistPick} {
		m.activeView = v
		if bar := m.buildShortcutsBar(400); !strings.Contains(bar, "[h/esc] back") {
			t.Errorf("view %v: bar lacks [h/esc] back", v)
		}
	}
}

// TestFavoritesQueueAll: e queues the whole list in Favorites like it does in
// every other collection view.
func TestFavoritesQueueAll(t *testing.T) {
	m := newTestModel()
	m.cfg.ToggleFavorite(api.Track{ID: "a", Title: "A"})
	m.cfg.ToggleFavorite(api.Track{ID: "b", Title: "B"})
	m.hasCurrent = true // something is playing: e only appends
	m.current = api.Track{ID: "x"}
	m.activateView(viewFavorites)

	press(m, "e")
	if len(m.queue) != 2 {
		t.Fatalf("queue = %d tracks after e in Favorites, want 2", len(m.queue))
	}
}

// TestQueueReorderWhileFilteredExplains: J/K are disabled under a filter; say
// so instead of silently ignoring the key.
func TestQueueReorderWhileFilteredExplains(t *testing.T) {
	m := newTestModel()
	m.queue = []api.Track{{ID: "a", Title: "Alpha"}, {ID: "b", Title: "Alps"}}
	m.activateView(viewQueue)
	m.filter = "al"

	press(m, "J")
	if m.queue[0].ID != "a" {
		t.Fatal("J reordered a filtered queue")
	}
	if !m.statusErr || !strings.Contains(m.status, "filter") {
		t.Fatalf("status = %q, want a hint to clear the filter", m.status)
	}
}
