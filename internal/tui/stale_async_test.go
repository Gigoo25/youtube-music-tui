package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/Gigoo25/youtube-music-tui/internal/api"
)

// TestReqCtxCancelsSupersededRequest: dropping a stale response is not enough —
// the superseded request keeps its socket for the full RequestTimeout unless the
// new request in the same lane cancels it. Other lanes must survive untouched.
func TestReqCtxCancelsSupersededRequest(t *testing.T) {
	m := newTestModel()

	first := m.reqCtx("search")
	other := m.reqCtx("album")
	second := m.reqCtx("search")

	if err := first.Err(); err != context.Canceled {
		t.Fatalf("superseded search ctx Err = %v, want context.Canceled", err)
	}
	if err := other.Err(); err != nil {
		t.Fatalf("album ctx cancelled by a search request: %v", err)
	}
	if err := second.Err(); err != nil {
		t.Fatalf("current search ctx already done: %v", err)
	}
}

// TestAutoContinueDropsStaleResponse: the queue-ran-out radio fetch takes
// seconds. If the user starts a different track meanwhile, the late response
// must not append to the queue or hijack playback — the same staleness rule
// radio/album/artist/search responses already follow.
func TestAutoContinueDropsStaleResponse(t *testing.T) {
	m := newTestModel()
	m.queue = []api.Track{{ID: "user-pick", Title: "User Pick"}}
	m.current, m.hasCurrent = m.queue[0], true

	// Dispatched while "old-seed" was playing; "user-pick" is playing now.
	m.Update(autoContinueMsg{seed: "old-seed", tracks: []api.Track{{ID: "r1", Title: "Radio One"}}})

	if len(m.queue) != 1 || m.queue[0].ID != "user-pick" {
		t.Fatalf("stale auto-continue mutated the queue: %+v", m.queue)
	}
	if m.current.ID != "user-pick" {
		t.Fatalf("stale auto-continue changed the playing track to %q", m.current.ID)
	}
}

// TestRadioDropsResponseAfterQueueEnded: hasCurrent goes false when the queue
// ends but current stays populated, so a seed-ID compare alone lets a late radio
// response rebuild the queue with nothing ever playing.
func TestRadioDropsResponseAfterQueueEnded(t *testing.T) {
	m := newTestModel()
	m.queue = []api.Track{{ID: "seed", Title: "Seed"}}
	m.current, m.hasCurrent = m.queue[0], false // queue ended, seed retained

	m.Update(radioDoneMsg{seed: "seed", tracks: []api.Track{{ID: "r1", Title: "Radio One"}}})

	if len(m.queue) != 1 || m.queue[0].ID != "seed" {
		t.Fatalf("radio response applied after the queue ended: %+v", m.queue)
	}
}

// TestHelpRowCountMatchesRender: helpRowCount clamps the help scroll cursor and
// is derived by hand from renderHelp's row layout. Adding a section or heading
// row to one and not the other silently breaks scrolling to the bottom.
func TestHelpRowCountMatchesRender(t *testing.T) {
	m := newTestModel()
	// A tall render window so nothing is windowed away, and a wide one so no row
	// wraps into two lines.
	out := m.renderHelp(200, helpRowCount()+10)
	got := len(strings.Split(strings.TrimRight(out, "\n"), "\n")) - 2 // box border rows
	if got != helpRowCount() {
		t.Fatalf("renderHelp emitted %d rows, helpRowCount() = %d", got, helpRowCount())
	}
}

// TestHelpScrollsToLastBinding: on a terminal shorter than the shortcut list the
// final section (theme/help/quit) is only reachable if help scrolls.
func TestHelpScrollsToLastBinding(t *testing.T) {
	m := newTestModel()
	m.activeView = viewHelp
	if strings.Contains(m.renderHelp(80, 20), "quit") {
		t.Skip("help fits in 20 rows; nothing to scroll")
	}
	for range helpRowCount() {
		m.handleKey(key("j"))
	}
	if !strings.Contains(m.renderHelp(80, 20), "quit") {
		t.Fatal("help cannot scroll to its last binding")
	}
}
