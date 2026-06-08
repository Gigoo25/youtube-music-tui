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
	press(m, "esc") // leave the default search-typing state
	press(m, "2")
	if m.activeView != viewQueue {
		t.Fatalf("expected viewQueue, got %v", m.activeView)
	}
	press(m, "3")
	if m.activeView != viewFavorites {
		t.Fatalf("expected viewFavorites, got %v", m.activeView)
	}
	press(m, "4")
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

// TestPlaybackKeysDoNotFireWhileTyping: 's' typed into search must not toggle shuffle.
func TestPlaybackKeysDoNotFireWhileTyping(t *testing.T) {
	m := newTestModel()
	press(m, "/") // focus search input -> typing
	if !m.typing() {
		t.Fatal("expected typing mode after '/'")
	}
	press(m, "s")
	if m.shuffle {
		t.Fatal("'s' while typing must not toggle shuffle")
	}
}

// TestStartsAtSearch: the app opens on the Search view.
func TestStartsAtSearch(t *testing.T) {
	m := newTestModel()
	if m.activeView != viewSearch {
		t.Fatalf("expected to start on viewSearch, got %v", m.activeView)
	}
}

// TestViewsRenderWithoutPanic: every view renders.
func TestViewsRenderWithoutPanic(t *testing.T) {
	m := newTestModel()
	m.queue = []api.Track{{ID: "x", Title: "Song A", Artist: "Artist A", Duration: "3:21"}}
	m.searchResults = m.queue
	for _, v := range []view{viewSearch, viewQueue, viewFavorites, viewHistory, viewTrending, viewHelp} {
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
