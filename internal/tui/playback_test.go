package tui

import (
	"testing"

	"github.com/Gigoo25/youtube-music-tui/internal/api"
)

// TestNextTrackEmptyQueueClearsCurrent: with the queue emptied (the playing
// entry was deleted and no tracks remain), a track end must not leave
// hasCurrent true — that phantom keeps the now-bar and MPRIS reporting a dead
// track. Auto-continue is off here.
func TestNextTrackEmptyQueueClearsCurrent(t *testing.T) {
	m := newTestModel()
	m.current = api.Track{ID: "a", Title: "A"}
	m.hasCurrent = true
	m.queuePos = -1

	if cmd := m.nextTrack(); cmd != nil {
		t.Fatal("nextTrack with an empty queue returned a command")
	}
	if m.hasCurrent {
		t.Fatal("hasCurrent must be cleared when there is nothing left to play")
	}
	if m.status != "queue ended" {
		t.Fatalf("status = %q, want %q", m.status, "queue ended")
	}
}

// TestNextTrackEmptyQueueAutoContinues: with auto-continue on, an emptied queue
// still gets a radio fetch instead of being dropped, and hasCurrent is kept so
// the fetch's seed survives (the autoContinueMsg handler owns it from here).
func TestNextTrackEmptyQueueAutoContinues(t *testing.T) {
	m := newTestModel()
	m.cfg.AutoContinue = true
	m.current = api.Track{ID: "a", Title: "A"}
	m.hasCurrent = true
	m.queuePos = -1

	if cmd := m.nextTrack(); cmd == nil {
		t.Fatal("auto-continue must still fetch when the queue is empty")
	}
	if !m.hasCurrent {
		t.Fatal("hasCurrent must survive until the radio fetch answers")
	}
}

// TestPlaybackFailureEmptyQueueClearsCurrent: a stream error with nothing left
// in the queue must drop the phantom now-playing state, while the error message
// stays on screen.
func TestPlaybackFailureEmptyQueueClearsCurrent(t *testing.T) {
	m := newTestModel()
	m.current = api.Track{ID: "a", Title: "A"}
	m.hasCurrent = true
	m.queuePos = -1

	if cmd := m.handlePlaybackFailure("stream error"); cmd != nil {
		t.Fatal("handlePlaybackFailure with an empty queue returned a command")
	}
	if m.hasCurrent {
		t.Fatal("hasCurrent must be cleared when a failure leaves nothing to play")
	}
	if m.status != "stream error" {
		t.Fatalf("status = %q, want the failure message preserved", m.status)
	}
}
