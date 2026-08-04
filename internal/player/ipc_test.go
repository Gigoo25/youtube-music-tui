package player

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

// scanPlayer builds a Player with only the fields scan() and send() touch.
// No mpv subprocess, no real IPC socket — just the channels and context the
// event loop needs. This keeps the test fast and free of process-global state.
func scanPlayer() *Player {
	p := &Player{
		sendCh: make(chan []byte, 64),
		done:   make(chan struct{}, 1),
		closed: make(chan struct{}),
	}
	p.baseCtx, p.baseCancel = context.WithCancel(context.Background())
	return p
}

// feedLines writes JSON lines to one end of a net.Pipe and runs p.scan() on the
// other end in a goroutine. Returns a function that blocks until scan has
// drained every line the caller wrote — without this, the test would assert on
// state that hasn't been processed yet.
func feedLines(p *Player, lines []string) (closePipe func(), waitReady func()) {
	a, b := net.Pipe()
	go p.scan(b)
	go func() {
		for _, line := range lines {
			line = line + "\n"
			a.Write([]byte(line)) //nolint:errcheck
		}
	}()
	return func() {
			a.Close()
			b.Close()
		}, func() {
			// scan reads newline-delimited; give it a tick to process the last line.
			time.Sleep(50 * time.Millisecond)
		}
}

// TestScanUpdatesPosition: time-pos (observer id 1) drives the progress bar.
// If scan ever stops applying it, the TUI freezes on the last position it saw.
func TestScanUpdatesPosition(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	closePipe, waitReady := feedLines(p, []string{
		`{"event":"property-change","id":1,"data":12.5}`,
	})
	defer closePipe()
	waitReady()

	if got := p.State().Position; got != 12.5 {
		t.Fatalf("Position = %v, want 12.5", got)
	}
}

// TestScanUpdatesDuration: duration (id 2) is what the TUI renders as the total
// length. A missing duration means the bar has no right edge.
func TestScanUpdatesDuration(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	closePipe, waitReady := feedLines(p, []string{
		`{"event":"property-change","id":2,"data":180.0}`,
	})
	defer closePipe()
	waitReady()

	if got := p.State().Duration; got != 180 {
		t.Fatalf("Duration = %v, want 180", got)
	}
}

// TestScanUpdatesPaused: pause (id 3) flips the TUI's play/pause icon. If scan
// ignores this property the icon never reflects reality.
func TestScanUpdatesPaused(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	closePipe, waitReady := feedLines(p, []string{
		`{"event":"property-change","id":3,"data":true}`,
	})
	defer closePipe()
	waitReady()

	if !p.State().Paused {
		t.Fatal("Paused should be true after property id 3 = true")
	}
}

// TestScanUpdatesVolume: volume (id 4) is what the shortcuts bar paints. A
// stale volume reading means the bar lies about where mpv actually is.
func TestScanUpdatesVolume(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	closePipe, waitReady := feedLines(p, []string{
		`{"event":"property-change","id":4,"data":72.0}`,
	})
	defer closePipe()
	waitReady()

	if got := p.State().Volume; got != 72 {
		t.Fatalf("Volume = %v, want 72", got)
	}
}

// TestScanUpdatesIdle: idle-active (id 5) tells the TUI whether to show the
// "no track loaded" state. Missing this means the UI thinks a track is playing
// when mpv is just sitting at the idle prompt.
func TestScanUpdatesIdle(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	closePipe, waitReady := feedLines(p, []string{
		`{"event":"property-change","id":5,"data":true}`,
	})
	defer closePipe()
	waitReady()

	if !p.State().Idle {
		t.Fatal("Idle should be true after property id 5 = true")
	}
}

// TestScanUpdatesMuted: mute (id 6) controls the speaker icon. If scan ignores
// it the icon stays "unmuted" even when mpv is silenced.
func TestScanUpdatesMuted(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	closePipe, waitReady := feedLines(p, []string{
		`{"event":"property-change","id":6,"data":true}`,
	})
	defer closePipe()
	waitReady()

	if !p.State().Muted {
		t.Fatal("Muted should be true after property id 6 = true")
	}
}

// TestScanIgnoresMalformedJSON: garbage lines from a misbehaving mpv must not
// crash the scan loop. The TUI would otherwise die on a single stray byte on
// the IPC socket — a transient network glitch on the unix socket is not fatal.
func TestScanIgnoresMalformedJSON(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	closePipe, waitReady := feedLines(p, []string{
		"not json",
		`{"event":"property-change","id":1,"data":42.0}`,
	})
	defer closePipe()
	waitReady()

	if got := p.State().Position; got != 42.0 {
		t.Fatalf("Position = %v, want 42 (malformed line should have been skipped)", got)
	}
}

// TestTrackEndedOnlyOnNaturalEOF: only reason "eof" means the track finished
// playing. "stop" and "redirect" fire when mpv is being told to move on — that
// is the *start* of the next track, not the end of the current one. Without
// this guard the queue auto-advances every time a new file is loaded.
func TestTrackEndedOnlyOnNaturalEOF(t *testing.T) {
	t.Run("eof signals track ended", func(t *testing.T) {
		p := scanPlayer()
		defer p.baseCancel()

		closePipe, waitReady := feedLines(p, []string{
			`{"event":"end-file","reason":"eof"}`,
		})
		defer closePipe()
		waitReady()

		if !p.TrackEnded() {
			t.Fatal("TrackEnded should be true after end-file reason eof")
		}
	})

	t.Run("stop does not signal track ended", func(t *testing.T) {
		p := scanPlayer()
		defer p.baseCancel()

		closePipe, waitReady := feedLines(p, []string{
			`{"event":"end-file","reason":"stop"}`,
		})
		defer closePipe()
		waitReady()

		if p.TrackEnded() {
			t.Fatal("TrackEnded must be false after end-file reason stop")
		}
	})

	t.Run("redirect does not signal track ended", func(t *testing.T) {
		p := scanPlayer()
		defer p.baseCancel()

		closePipe, waitReady := feedLines(p, []string{
			`{"event":"end-file","reason":"redirect"}`,
		})
		defer closePipe()
		waitReady()

		if p.TrackEnded() {
			t.Fatal("TrackEnded must be false after end-file reason redirect")
		}
	})
}

// TestStreamErrorDropsCachedURL: a stream error must evict the cached URL so a
// retry resolves a fresh one. Without this, a retry replays the same dead URL
// forever — the TUI's "retry" button becomes a no-op.
func TestStreamErrorDropsCachedURL(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.cachePut("vid", "http://dead.example/stream")
	p.playingID = "vid"

	closePipe, waitReady := feedLines(p, []string{
		`{"event":"end-file","reason":"error"}`,
	})
	defer closePipe()
	waitReady()

	if _, ok := p.cacheGet("vid"); ok {
		t.Fatal("cached URL for errored track should be evicted")
	}
	if err := p.LoadError(); err == "" {
		t.Fatal("LoadError should report the stream failure")
	}
}

// TestSendDropsAfterClose: once the player is closed, commands must not queue
// up on sendCh. The writeLoop exits on close, so a queued command would never
// be delivered — and the channel would eventually fill, blocking the TUI.
func TestSendDropsAfterClose(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	close(p.closed)

	p.send([]any{"stop"})

	if len(p.sendCh) != 0 {
		t.Fatalf("sendCh len = %d after close, want 0 (commands must be dropped)", len(p.sendCh))
	}
}

// TestSendDoesNotBlockWhenFull: if the writeLoop is wedged and sendCh is full,
// send must return immediately rather than blocking the caller (which holds p.mu
// in Load and Stop). Blocking there would deadlock the whole player.
func TestSendDoesNotBlockWhenFull(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	// Fill sendCh to capacity.
	for len(p.sendCh) < cap(p.sendCh) {
		p.send([]any{"noop"})
	}

	done := make(chan struct{})
	go func() {
		p.send([]any{"stop"})
		close(done)
	}()

	select {
	case <-done:
		// ok — send returned without blocking
	case <-time.After(time.Second):
		t.Fatal("send blocked on full channel — writeLoop wedged or close guard missing")
	}
}

// TestIsClosed: isClosed is the gate on every code path that touches mpv. A
// player that reports Alive after Close() would let the TUI keep sending
// commands into a void.
func TestIsClosed(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	if p.isClosed() {
		t.Fatal("fresh player must not be closed")
	}
	close(p.closed)
	if !p.isClosed() {
		t.Fatal("player must report closed after close(p.closed)")
	}
}

// TestStateReturnsCopy: State returns a copy of the struct, so a caller mutating
// the returned value cannot silently corrupt the player's state. This matters
// because the TUI builds a snapshot each tick and a reference leak would let the
// snapshot race with scan's writes.
func TestStateReturnsCopy(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	s := p.State()
	s.Volume = 9999
	if got := p.State().Volume; got != 0 {
		t.Fatalf("mutating State() return must not affect the player (Volume = %v)", got)
	}
}

// TestLoadErrorClearsAfterRead: LoadError is polled by the TUI tick. If it
// never cleared, the error line would stay pinned forever even after the
// underlying problem resolved.
func TestLoadErrorClearsAfterRead(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.mu.Lock()
	p.lastErr = "something broke"
	p.mu.Unlock()

	if got := p.LoadError(); got != "something broke" {
		t.Fatalf("LoadError = %q, want 'something broke'", got)
	}
	if got := p.LoadError(); got != "" {
		t.Fatalf("second LoadError = %q, want \"\" (must clear after read)", got)
	}
}

// TestRestartedClearsAfterRead: Restarted is polled by the TUI to decide
// whether to reload the current track after a respawn. If it never cleared,
// the TUI would reload on every tick.
func TestRestartedClearsAfterRead(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.mu.Lock()
	p.restarted = true
	p.mu.Unlock()

	if !p.Restarted() {
		t.Fatal("Restarted should be true")
	}
	if p.Restarted() {
		t.Fatal("Restarted must clear after one read")
	}
}

// TestAliveReportsLiveness: Alive is the TUI's gate — false means stop trying
// to send commands. Without this check the TUI would keep painting "playing"
// while mpv is dead.
func TestAlive(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.mu.Lock()
	p.alive = true
	p.mu.Unlock()

	if !p.Alive() {
		t.Fatal("Alive should be true")
	}
	p.mu.Lock()
	p.alive = false
	p.mu.Unlock()
	if p.Alive() {
		t.Fatal("Alive must be false after setting alive = false")
	}
}

// TestScanClearsLoadingOnStartFile: start-file means mpv began loading a new
// track. The TUI's "loading" spinner must stop once actual playback starts,
// not linger while the track plays.
func TestScanClearsLoadingOnStartFile(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()
	p.state.Loading = true

	closePipe, waitReady := feedLines(p, []string{
		`{"event":"start-file"}`,
	})
	defer closePipe()
	waitReady()

	if p.State().Loading {
		t.Fatal("Loading must be false after start-file event")
	}
}

// TestScanClearsLoadingOnPosition: the first non-zero time-pos means mpv has
// enough data to play. The TUI's loading spinner is driven by the same flag —
// if it stays up the user sees a spinner while music plays.
func TestScanClearsLoadingOnPosition(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()
	p.state.Loading = true

	closePipe, waitReady := feedLines(p, []string{
		`{"event":"property-change","id":1,"data":0.5}`,
	})
	defer closePipe()
	waitReady()

	if p.State().Loading {
		t.Fatal("Loading must be false after a positive time-pos")
	}
}

// TestScanDoesNotClearLoadingOnZeroPosition: position 0 is normal at track
// start — it does not mean loading finished. Only a *positive* position clears
// the flag, so seeking back to 0 does not hide the spinner prematurely.
func TestScanDoesNotClearLoadingOnZeroPosition(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()
	p.state.Loading = true

	closePipe, waitReady := feedLines(p, []string{
		`{"event":"property-change","id":1,"data":0}`,
	})
	defer closePipe()
	waitReady()

	if !p.State().Loading {
		t.Fatal("Loading must remain true after position 0")
	}
}

// TestScanEndFileErrorSetsLoadingFalse: a stream error must clear the loading
// flag so the TUI stops spinning and shows the error message instead.
func TestScanEndFileErrorSetsLoadingFalse(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()
	p.state.Loading = true

	closePipe, waitReady := feedLines(p, []string{
		`{"event":"end-file","reason":"error"}`,
	})
	defer closePipe()
	waitReady()

	if p.State().Loading {
		t.Fatal("Loading must be false after end-file reason error")
	}
}

// TestScanUnknownEventIgnored: an unrecognized event must not panic or corrupt
// state. mpv may send future events as it evolves; scan must be forward-
// compatible.
func TestScanUnknownEventIgnored(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	closePipe, waitReady := feedLines(p, []string{
		`{"event":"some-future-event","data":"x"}`,
		`{"event":"property-change","id":1,"data":7.0}`,
	})
	defer closePipe()
	waitReady()

	if got := p.State().Position; got != 7.0 {
		t.Fatalf("Position = %v, want 7 (unknown event must not break subsequent parsing)", got)
	}
}

// TestScanPropertyWrongTypeIgnored: a property-change with the wrong Data type
// (e.g. a string where a float is expected) must be skipped, not panic on type
// assertion. mpv's protocol is loose and a future version could send unexpected
// types.
func TestScanPropertyWrongTypeIgnored(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	closePipe, waitReady := feedLines(p, []string{
		`{"event":"property-change","id":1,"data":"not-a-number"}`,
		`{"event":"property-change","id":1,"data":9.0}`,
	})
	defer closePipe()
	waitReady()

	if got := p.State().Position; got != 9.0 {
		t.Fatalf("Position = %v, want 9 (wrong-type data must be skipped)", got)
	}
}

// TestScanPropertyUnknownIDIgnored: an observer id scan does not know about
// must be ignored, not panic. New mpv versions may add property observations
// we don't subscribe to but could appear in the stream.
func TestScanPropertyUnknownIDIgnored(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	closePipe, waitReady := feedLines(p, []string{
		`{"event":"property-change","id":99,"data":123}`,
		`{"event":"property-change","id":1,"data":3.0}`,
	})
	defer closePipe()
	waitReady()

	if got := p.State().Position; got != 3.0 {
		t.Fatalf("Position = %v, want 3 (unknown id must not break parsing)", got)
	}
}

// TestSendMarshalsCommand: send must produce valid JSON followed by a newline,
// the wire format mpv's IPC server expects. A malformed line would make mpv
// silently drop the command — the TUI would appear unresponsive.
func TestSendMarshalsCommand(t *testing.T) {
	p := scanPlayer()
	defer p.baseCancel()

	p.send([]any{"stop"})

	if len(p.sendCh) != 1 {
		t.Fatalf("sendCh len = %d, want 1", len(p.sendCh))
	}
	b := <-p.sendCh
	var cmd ipcCmd
	if err := json.Unmarshal(b, &cmd); err != nil {
		t.Fatalf("send output is not valid JSON: %v", err)
	}
	if len(cmd.Command) != 1 || cmd.Command[0] != "stop" {
		t.Fatalf("command = %v, want [\"stop\"]", cmd.Command)
	}
	if !strings.HasSuffix(string(b), "\n") {
		t.Fatal("send output must end with a newline (mpv IPC wire format)")
	}
}
