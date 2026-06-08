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

// TestViewSwitching: number keys + tab route to views when not typing.
func TestViewSwitching(t *testing.T) {
	m := newTestModel()
	// leave the search-typing state by jumping to Home first
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
}

// TestShuffleRepeatToggle: s/r work outside the search input.
func TestShuffleRepeatToggle(t *testing.T) {
	m := newTestModel()
	press(m, "1") // Home, not typing
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

// TestViewsRenderWithoutPanic: every view renders.
func TestViewsRenderWithoutPanic(t *testing.T) {
	m := newTestModel()
	m.queue = []api.Track{{ID: "x", Title: "Song A", Artist: "Artist A", Duration: "3:21"}}
	m.searchResults = m.queue
	for _, v := range []view{viewHome, viewSearch, viewQueue, viewFavorites, viewHelp} {
		m.activeView = v
		out := m.View()
		if out == "" {
			t.Fatalf("view %v rendered empty", v)
		}
	}
	// Home header should reference the app name.
	m.activeView = viewHome
	if !strings.Contains(m.View(), "youtube-music-cli") {
		t.Error("home view missing app title")
	}
}
