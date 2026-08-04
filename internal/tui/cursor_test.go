package tui

import (
	"testing"
	"time"

	"github.com/Gigoo25/youtube-music-tui/internal/api"
	"github.com/Gigoo25/youtube-music-tui/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// TestQueueCursorClampedAfterShrink: removing the last item of the queue must
// not leave the cursor out of range. Without clampActiveCursor, deleting the
// last track while it was selected would leave queueCursor = len(queue), and
// the next render would panic on queue[queueCursor]. This is an index-out-of-
// range panic class.
func TestQueueCursorClampedAfterShrink(t *testing.T) {
	m := newTestModel()

	// Fill queue with 5 tracks.
	for i := 0; i < 5; i++ {
		m.queue = append(m.queue, api.Track{ID: string(rune('a' + i)), Title: "Track"})
	}
	m.queueCursor = 4 // point at the last entry

	// Simulate "d" key (delete current queue item) via the key handler.
	_, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})

	if m.queueCursor >= len(m.queue) {
		t.Fatalf("queueCursor = %d, len(queue) = %d — cursor out of range", m.queueCursor, len(m.queue))
	}
	if m.queueCursor < 0 {
		t.Fatalf("queueCursor = %d, want >= 0", m.queueCursor)
	}

	// View() must not panic after the cursor was clamped.
	_ = m.View()
}

// TestFavCursorClampedAfterShrink: same guarantee for the favorites view.
// Favoriting/unfavoriting the last item must not leave the cursor dangling.
func TestFavCursorClampedAfterShrink(t *testing.T) {
	m := newTestModel()
	m.activeView = viewFavorites

	for i := 0; i < 5; i++ {
		m.cfg.Favorites = append(m.cfg.Favorites, api.Track{ID: string(rune('a' + i)), Title: "Track"})
	}
	m.favCursor = 4

	// Remove the last favorite.
	m.cfg.Favorites = m.cfg.Favorites[:4]
	m.clampActiveCursor()

	if m.favCursor >= len(m.cfg.Favorites) {
		t.Fatalf("favCursor = %d, len(Favorites) = %d — cursor out of range", m.favCursor, len(m.cfg.Favorites))
	}

	_ = m.View()
}

// TestHistoryCursorClampedAfterShrink: same guarantee for the history view.
func TestHistoryCursorClampedAfterShrink(t *testing.T) {
	m := newTestModel()
	m.activeView = viewHistory

	for i := 0; i < 5; i++ {
		m.cfg.History = append(m.cfg.History, config.HistoryEntry{
			Track:    api.Track{ID: string(rune('a' + i)), Title: "Track"},
			PlayedAt: now(),
		})
	}
	m.historyCursor = 4

	m.cfg.History = m.cfg.History[:4]
	m.clampActiveCursor()

	if m.historyCursor >= len(m.cfg.History) {
		t.Fatalf("historyCursor = %d, len(History) = %d — cursor out of range", m.historyCursor, len(m.cfg.History))
	}

	_ = m.View()
}

// TestViewDoesNotPanicAfterShrink: after any cursor-clamping shrink, View()
// must not panic. A panic in View would crash the entire TUI — the most
// visible failure mode.
func TestViewDoesNotPanicAfterShrink(t *testing.T) {
	m := newTestModel()

	for i := 0; i < 3; i++ {
		m.queue = append(m.queue, api.Track{ID: string(rune('a' + i)), Title: "Track"})
	}
	m.queueCursor = 2

	// Remove all items one by one.
	for len(m.queue) > 0 {
		m.queue = m.queue[1:]
		m.queueCursor = max(0, m.queueCursor-1)
		if m.queueCursor >= len(m.queue) && len(m.queue) > 0 {
			m.queueCursor = len(m.queue) - 1
		}
	}

	// Must not panic.
	_ = m.View()
}

// now returns the current time. Helper to avoid importing time in the test
// package (we only need it for config.HistoryEntry).
func now() time.Time {
	return time.Now()
}
