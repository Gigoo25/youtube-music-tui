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

// TestEscClearingFilterKeepsSelection: esc drops the filter but must leave the
// cursor on the same track in the full list, not jump back to the top — both
// from an applied filter and while still typing it.
func TestEscClearingFilterKeepsSelection(t *testing.T) {
	for _, typing := range []bool{false, true} {
		m := newTestModel()
		m.queue = []api.Track{{ID: "a", Title: "Alpha"}, {ID: "b", Title: "Beta"},
			{ID: "c", Title: "Gamma"}, {ID: "d", Title: "Delta"}}
		m.activateView(viewQueue)
		press(m, "/")
		for _, r := range "ta" {
			press(m, string(r))
		}
		if !typing {
			press(m, "enter")
		}
		// Filtered rows: Beta, Delta — select Delta. (While typing, j would be
		// text, so set the cursor directly in both cases.)
		m.queueCursor = 1
		press(m, "esc")
		if m.filter != "" {
			t.Fatalf("typing=%v: esc left the filter %q", typing, m.filter)
		}
		if m.queueCursor != 3 {
			t.Fatalf("typing=%v: cursor = %d after esc, want 3 (Delta)", typing, m.queueCursor)
		}
	}
}

// TestEscClearingFilterKeepsSelectionHome: Home's cursor is flat across both
// sections, so a Quick Picks match must map past the Listen Again rows.
func TestEscClearingFilterKeepsSelectionHome(t *testing.T) {
	m := newTestModel()
	m.homeListenAgain = []api.Track{{ID: "a", Title: "Alpha"}, {ID: "b", Title: "Beta"}}
	m.homeQuickPicks = []api.Track{{ID: "c", Title: "Gamma"}, {ID: "d", Title: "Omega"}}
	m.activeView, m.focus = viewHome, focusPanel
	m.filter = "ga"  // matches Gamma, Omega
	m.homeCursor = 1 // second match: Omega
	press(m, "esc")
	if m.homeCursor != 3 {
		t.Fatalf("home cursor = %d after esc, want 3 (Omega)", m.homeCursor)
	}
}
