package tui

import (
	"testing"

	"github.com/Gigoo25/youtube-music-tui/internal/api"
)

// TestNextShuffleIdxSingleTrack guards F05: a one-track shuffle has nothing to
// pick, and must say so with -1 rather than handing back the playing index —
// which is the back-to-back repeat the doc comment promises never happens.
func TestNextShuffleIdxSingleTrack(t *testing.T) {
	m := newTestModel()
	m.queue = []api.Track{{ID: "a", Title: "only"}}
	m.queuePos = 0

	if got := m.nextShuffleIdx(); got != -1 {
		t.Fatalf("nextShuffleIdx() on a one-track queue = %d, want -1", got)
	}
}

// TestNextShuffleIdxNeverRepeatsCurrent guards F05: with a real queue the pick
// must be in range and must never be the slot already playing.
func TestNextShuffleIdxNeverRepeatsCurrent(t *testing.T) {
	m := newTestModel()
	m.queue = []api.Track{{ID: "a"}, {ID: "b"}, {ID: "c"}}

	for pos := range m.queue {
		m.queuePos = pos
		// Random pick: sample enough to catch a biased or off-by-one mapping.
		for range 200 {
			got := m.nextShuffleIdx()
			if got < 0 || got >= len(m.queue) {
				t.Fatalf("queuePos=%d: nextShuffleIdx() = %d, out of range", pos, got)
			}
			if got == pos {
				t.Fatalf("queuePos=%d: nextShuffleIdx() returned the playing slot", pos)
			}
		}
	}
}
