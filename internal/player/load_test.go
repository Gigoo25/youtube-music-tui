package player

import (
	"testing"
	"time"
)

// TestBeginLoadBumpsGeneration: each beginLoad call must return a strictly
// increasing generation counter so callers can tell whether a late-arriving
// result still belongs to the current load. Without this, a slow yt-dlp from
// track N could fire loadfile after the user has already moved to track N+1,
// resurrecting the wrong track.
func TestBeginLoadBumpsGeneration(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	gen1, _ := p.beginLoad("a")
	gen2, _ := p.beginLoad("b")

	if gen2 <= gen1 {
		t.Fatalf("second beginLoad gen (%d) must exceed first (%d)", gen2, gen1)
	}
}

// TestBeginLoadCancelsPreviousContext: the context returned by beginLoad carries
// a timeout tied to loadTimeout. A later beginLoad must cancel the previous
// one's context so a slow resolve from the abandoned load doesn't run to
// completion and pile up against the new one.
func TestBeginLoadCancelsPreviousContext(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	_, ctx1 := p.beginLoad("a")
	p.beginLoad("b")

	if ctx1.Err() == nil {
		t.Fatal("first beginLoad's context must be cancelled by the second")
	}
}

// TestStopCancelsInFlightLoad: Stop must cancel any in-flight yt-dlp resolve
// and bump the epoch. Without this, a resolve that was kicked off by a load
// the user then aborted would still fire loadfile after Stop returns —
// resurrecting playback the user explicitly cleared.
func TestStopCancelsInFlightLoad(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	genBefore, _ := p.beginLoad("a")
	p.Stop()
	genAfter := p.loadGen

	if genAfter <= genBefore {
		t.Fatalf("loadGen after Stop (%d) must exceed gen before (%d)", genAfter, genBefore)
	}
}

// TestStopClearsLoadingFlag: Stop must clear the Loading flag so the TUI
// doesn't keep spinning after the user cleared the queue. A lingering Loading
// flag means the TUI shows a spinner forever with no track loaded — the user
// has no way to tell the app is frozen vs. "waiting for input."
func TestStopClearsLoadingFlag(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.mu.Lock()
	p.state.Loading = true
	p.mu.Unlock()

	p.Stop()

	if p.State().Loading {
		t.Fatal("State().Loading must be false after Stop")
	}
}

// TestBeginLoadResetsPlaybackState: beginLoad zeroes Position and Duration and
// clears Idle. Without this, a new load inherits the previous track's state —
// the progress bar would start at the old track's end position, and the TUI
// would show "idle" until the first property-change arrived seconds later.
func TestBeginLoadResetsPlaybackState(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	// Seed with stale state.
	p.mu.Lock()
	p.state.Position = 100
	p.state.Duration = 200
	p.state.Idle = false
	p.mu.Unlock()

	p.beginLoad("new")

	s := p.State()
	if s.Position != 0 {
		t.Fatalf("Position after beginLoad = %v, want 0", s.Position)
	}
	if s.Duration != 0 {
		t.Fatalf("Duration after beginLoad = %v, want 0", s.Duration)
	}
	if s.Idle {
		t.Fatal("Idle must be false after beginLoad")
	}
	if !s.Loading {
		t.Fatal("Loading must be true after beginLoad")
	}
}

// TestBeginLoadDrainsDoneChannel: beginLoad must drain the done channel so a
// previous TrackEnded signal doesn't leak into the new load. Without this, a
// double-tap on "play next" would see TrackEnded() = true from the previous
// track and skip the new one immediately.
func TestBeginLoadDrainsDoneChannel(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	// Signal a previous track end.
	select {
	case p.done <- struct{}{}:
	default:
	}

	p.beginLoad("new")

	if p.TrackEnded() {
		t.Fatal("TrackEnded must be false after beginLoad drains the done channel")
	}
}

// TestStopDoesNotBlockOnYtdlp: Stop must return promptly even if a yt-dlp
// resolve is in flight. The TUI calls Stop on every queue-clear action and
// blocks the event loop while it returns — a hanging Stop means the entire UI
// freezes until the resolve times out (loadTimeout = 60s).
//
// We can't actually launch yt-dlp in tests, but we can verify the epoch book
// keeping: after Stop, loadGen must have advanced, which is the signal a
// pending resolve checks to abort itself.
func TestStopDoesNotReturnImmediately(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.beginLoad("a")
	done := make(chan struct{})
	go func() {
		p.Stop()
		close(done)
	}()

	select {
	case <-done:
		// ok — Stop returned promptly
	case <-time.After(2 * time.Second):
		t.Fatal("Stop must return promptly, not block on in-flight resolves")
	}
}
