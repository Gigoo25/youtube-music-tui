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

// TestRepeatOneAfterDeletingPlayingLastTrack: deleting the playing entry steps
// queuePos back onto its predecessor. Repeat-one must not then "repeat" that
// predecessor — with nothing after the deleted slot, the queue has ended.
func TestRepeatOneAfterDeletingPlayingLastTrack(t *testing.T) {
	m := newTestModel()
	m.queue = []api.Track{{ID: "a", Title: "A"}} // "b" was playing at index 1 and got deleted
	m.queuePos = 0
	m.current = api.Track{ID: "b", Title: "B"}
	m.hasCurrent = true
	m.repeat = repeatOne

	// The old code called playAt(0) here, replaying "a" (and, with the nil test
	// player, panicking).
	if cmd := m.nextTrack(); cmd != nil {
		t.Fatal("nextTrack returned a command with auto-continue off")
	}
	if m.hasCurrent {
		t.Fatal("repeat-one replayed the deleted track's predecessor instead of ending")
	}
	if m.status != "queue ended" {
		t.Fatalf("status = %q, want %q", m.status, "queue ended")
	}
}

// TestTakeShuffleNextUsesPreroll: a shuffle advance plays the pick prefetchNext
// warmed, and re-rolls once that pick has become the playing track.
func TestTakeShuffleNextUsesPreroll(t *testing.T) {
	m := newTestModel()
	m.queue = []api.Track{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	m.queuePos = 0
	m.current = m.queue[0]
	m.hasCurrent = true

	m.shuffleNextID = "c"
	if i := m.takeShuffleNext(); i != 2 {
		t.Fatalf("takeShuffleNext = %d, want the pre-rolled pick 2", i)
	}
	if m.shuffleNextID != "" {
		t.Fatal("the pick must be consumed")
	}
	m.shuffleNextID = "a" // the playing track
	if i := m.takeShuffleNext(); i <= 0 {
		t.Fatalf("takeShuffleNext = %d, want a fresh pick other than the playing track", i)
	}
	m.shuffleNextID = "gone" // removed from the queue
	if i := m.takeShuffleNext(); i <= 0 {
		t.Fatalf("takeShuffleNext = %d, want a fresh pick", i)
	}
}

// TestAutoContinueAheadOnLastTrack: the auto-continue batch is fetched once
// while the last queued track plays, held rather than queued, and only then.
func TestAutoContinueAheadOnLastTrack(t *testing.T) {
	m := newTestModel()
	m.cfg.AutoContinue = true
	m.queue = []api.Track{{ID: "a"}, {ID: "b"}}
	m.queuePos = 0
	m.current = m.queue[0]
	m.hasCurrent = true

	if m.autoContinueAhead() != nil {
		t.Fatal("not the last track: no lookahead")
	}
	m.queuePos = 1
	m.current = m.queue[1]
	if m.autoContinueAhead() == nil {
		t.Fatal("last track: lookahead must fetch")
	}
	if m.autoContinueAhead() != nil {
		t.Fatal("one lookahead per seed")
	}
	m.Update(autoAheadMsg{seed: "b", tracks: []api.Track{{ID: "b"}, {ID: "r1"}, {ID: ""}}})
	if len(m.aheadTracks) != 1 || m.aheadTracks[0].ID != "r1" {
		t.Fatalf("aheadTracks = %v, want just r1 (seed and empty ids dropped)", m.aheadTracks)
	}
	if len(m.queue) != 2 {
		t.Fatal("the held batch must not be queued before the queue runs out")
	}
	m.Update(autoAheadMsg{seed: "stale", tracks: []api.Track{{ID: "x"}}})
	if m.aheadTracks[0].ID != "r1" {
		t.Fatal("a stale lookahead must be dropped")
	}
}

// TestContinueRadioIgnoresOtherSeedsBatch: a held batch for a different track
// isn't played; running out of queue fetches live instead.
func TestContinueRadioIgnoresOtherSeedsBatch(t *testing.T) {
	m := newTestModel()
	m.cfg.AutoContinue = true
	m.current = api.Track{ID: "a"}
	m.hasCurrent = true
	m.aheadSeed, m.aheadTracks = "other", []api.Track{{ID: "r1"}}

	if m.continueRadio() == nil {
		t.Fatal("want a live fetch")
	}
	if len(m.queue) != 0 {
		t.Fatal("another seed's batch must not be queued")
	}
}
