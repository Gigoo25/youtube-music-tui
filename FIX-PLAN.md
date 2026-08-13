# FIX-PLAN

Mechanical fix list for the bugs found in the audit. Work top to bottom.
Each fix is self-contained: **FIND** the anchor text, **DO** the edit, **CHECK** it builds.

## Rules — read before touching anything

1. **Line numbers in this document are hints, not addresses.** Every edit renumbers the
   file. Always locate the code by the quoted **FIND** text, never by line number alone.
2. **One fix at a time.** Apply, build, commit. Never batch two fixes into one commit.
3. **Do not refactor.** No renames, no reordering, no "while I'm here" cleanups, no
   gofmt-driven rewrites of untouched code. The diff for each fix should be under ~20 lines.
4. **Do not delete existing comments** unless the fix makes them wrong. Where a fix has a
   known limitation, the plan gives you the exact `ponytail:` comment to leave behind.
5. **If the FIND text does not match the file, STOP** and report it instead of guessing.
   Same if a fix looks already applied — verify, then skip it.
6. After each fix: `go build ./...`. After each phase: the phase CHECK command.
7. Baseline before you start (all must be clean — if any is already failing, stop and report):
   ```bash
   go build ./... && go vet ./... && gofmt -l . | grep -v vendor ; golangci-lint run ./...
   ```
   Known pre-existing lint failures: `internal/player/ipc_test.go:39,40` (fixed in P6).

---

# P0 — Crash and data loss

## F01 — MPRIS panics and kills the app when the session bus dies

**FILE** `internal/mpris/mpris.go`

**WHY** `props.SetMust` panics on any emit error. When the session bus goes away mid-session,
`Emit` returns `ErrClosed` → panic on the next tick → the whole TUI dies.

**FIND**
```go
func (s *Server) Update(n Now) {
	if s == nil || s.props == nil {
		return
	}
```

**DO** — insert the recover immediately after the nil guard:
```go
func (s *Server) Update(n Now) {
	if s == nil || s.props == nil {
		return
	}
	// SetMust panics when the property emit fails — most plausibly because the
	// session bus went away mid-session. A dead bus must not take the player
	// down with it; the next Update just retries.
	defer func() { _ = recover() }()
```

**CHECK** `go build ./... && go test ./internal/mpris`

---

## F02 — `S` (save queue as playlist) silently destroys an existing playlist

**FILE** `internal/tui/model.go`

**WHY** `config.SavePlaylist` replaces a same-named playlist. Typing an existing name wipes it
with no prompt, while every other destructive action here is confirm-gated.

**FIND** (inside the `m.naming` block, the `case "enter":` branch)
```go
			m.cfg.SavePlaylist(name, m.queue)
			m.markConfigDirty()
			m.setStatus(fmt.Sprintf("saved playlist %q (%d tracks)", name, len(m.queue)))
			return m, nil
```

**DO** — replace those four lines with:
```go
			// Saving over an existing playlist destroys it — confirm first, the
			// same way deleting one does.
			if m.cfg.PlaylistByName(name) != nil {
				n, q := name, m.queue
				m.confirmPrompt = fmt.Sprintf("replace playlist %q with the queue (%d tracks)?", n, len(q))
				m.confirmFn = func() {
					m.cfg.SavePlaylist(n, q)
					m.markConfigDirty()
					m.setStatus(fmt.Sprintf("saved playlist %q (%d tracks)", n, len(q)))
				}
				return m, nil
			}
			m.cfg.SavePlaylist(name, m.queue)
			m.markConfigDirty()
			m.setStatus(fmt.Sprintf("saved playlist %q (%d tracks)", name, len(m.queue)))
			return m, nil
```

**CHECK** `go build ./... && go test ./internal/tui`

---

## F03 — `.corrupt` backup can be silently overwritten

**FILE** `internal/config/config.go`

**WHY** The uniqueness suffix has 1-second resolution (a crash-restart loop reuses it), and any
non-ENOENT `os.Stat` error is treated as "no backup exists", so the only recoverable copy of the
user's favorites/history gets replaced.

**FIND**
```go
		corrupt := path + ".corrupt"
		if _, statErr := os.Stat(corrupt); statErr == nil {
			corrupt += "." + time.Now().Format("20060102-150405")
		}
```

**DO**
```go
		corrupt := path + ".corrupt"
		// Anything other than a definite "not there" means assume it exists: a
		// stat error must never cost the user their only recoverable copy.
		if _, statErr := os.Stat(corrupt); statErr == nil || !os.IsNotExist(statErr) {
			corrupt += "." + time.Now().Format("20060102-150405.000000000")
		}
```

**CHECK** `go build ./... && go test ./internal/config`

**PHASE CHECK** `go test ./internal/mpris ./internal/config ./internal/tui`

---

# P1 — Playback livelocks (`internal/tui/model.go`)

## F04 — Unplayable track + repeat-one spins forever

**WHY** `handlePlaybackFailure` gives up and calls `nextTrack()`, which under repeat-one (or a
one-track queue) replays *the same index*. `retryID` never changes, so every failure re-enters the
skip branch: a new yt-dlp process every 500 ms, forever.

**FIND**
```go
	m.setError(errMsg + " — skipping track")
	return m.nextTrack()
}
```

**DO**
```go
	m.setError(errMsg + " — skipping track")
	from := m.queuePos
	cmd := m.nextTrack()
	if m.hasCurrent && m.queuePos == from {
		// nextTrack looped straight back onto the track that just failed
		// (repeat-one, or a one-track queue). Stop instead of reloading it forever.
		// ponytail: only breaks a self-loop. A repeat-all queue where *every*
		// track fails still cycles — add a consecutive-skip counter if that shows up.
		m.hasCurrent = false
		m.player.Stop()
		m.setError(errMsg + " — no playable track")
		return nil
	}
	return cmd
}
```

**CHECK** `go build ./...`

---

## F05 — Shuffle never ends the queue; a one-track shuffle repeats back-to-back

**WHY** `nextShuffleIdx` returns `0` for `n <= 1`, i.e. the track that just played — the exact
back-to-back repeat its doc comment promises never happens.

### F05a

**FIND**
```go
func (m *model) nextShuffleIdx() int {
	n := len(m.queue)
	if n <= 1 {
		return 0
	}
```

**DO**
```go
func (m *model) nextShuffleIdx() int {
	n := len(m.queue)
	if n <= 1 {
		return -1 // no other track to pick — the caller decides what that means
	}
```

Also update the doc comment directly above it: replace the sentence
`falls back to a plain random pick when the playing entry isn't in the queue (queuePos out of range).`
with
`Returns -1 when the queue holds no other track; falls back to a plain random pick when the playing entry isn't in the queue (queuePos out of range).`

### F05b — handle -1 at both call sites

**FIND** (in `nextTrack`)
```go
	case repeatAll:
		if m.shuffle {
			m.playAt(m.nextShuffleIdx())
		} else {
			m.playAt((m.queuePos + 1) % len(m.queue))
		}
	default:
		if m.shuffle {
			m.playAt(m.nextShuffleIdx())
		} else if m.queuePos+1 < len(m.queue) {
			m.playAt(m.queuePos + 1)
		} else if m.cfg.AutoContinue && m.hasCurrent {
			return m.continueRadio()
		} else {
			m.hasCurrent = false
			m.setStatus("queue ended")
		}
	}
```

**DO**
```go
	case repeatAll:
		next := (m.queuePos + 1) % len(m.queue)
		if m.shuffle {
			// -1 means "nothing else to pick": repeat-all on one track replays it.
			if i := m.nextShuffleIdx(); i >= 0 {
				next = i
			} else {
				next = m.queuePos
			}
		}
		m.playAt(next)
	default:
		next := -1
		if m.shuffle {
			next = m.nextShuffleIdx()
		} else if m.queuePos+1 < len(m.queue) {
			next = m.queuePos + 1
		}
		if next >= 0 {
			m.playAt(next)
		} else if m.cfg.AutoContinue && m.hasCurrent {
			return m.continueRadio()
		} else {
			m.hasCurrent = false
			m.setStatus("queue ended")
		}
	}
```

**CHECK** `go build ./...`

---

## F06 — Failed auto-continue leaves a phantom now-playing track

**WHY** `hasCurrent` is deliberately held true across the fetch so the seed survives, but no failure
path clears it: the bar keeps showing a finished track, Space controls a dead mpv, and MPRIS reports
`Playing` forever.

**FIND** (in the `case autoContinueMsg:` handler — three separate spots)
```go
		if msg.err != nil {
			m.setError("auto-continue failed: " + msg.err.Error())
			return m, nil
		}
```
```go
		if len(tracks) == 0 {
			m.setError("auto-continue: nothing found")
			return m, nil
		}
```
```go
		if added == 0 {
			m.setError("auto-continue: nothing new found")
			return m, nil
		}
```

**DO** — add `m.hasCurrent = false` before each `return m, nil` in those three branches, e.g.
```go
		if msg.err != nil {
			// Nothing is playing any more; hasCurrent was only held true so the
			// seed survived the fetch.
			m.hasCurrent = false
			m.setError("auto-continue failed: " + msg.err.Error())
			return m, nil
		}
```
Add the comment once (first branch only); the other two get the bare assignment.

**Do not** touch the staleness guard at the top of the handler
(`if !m.hasCurrent || m.current.ID != msg.seed`) — there `hasCurrent` belongs to a newer track.

**CHECK** `go build ./...`

---

## F07 — Failure after the playing track is removed stops playback dead

**WHY** Deleting the playing entry from the queue sets `queuePos = -1` (a deliberate state). A later
stream error then hits the "nothing to recover" guard and playback stops, even with a full queue.

**FIND**
```go
	if !m.hasCurrent || m.queuePos < 0 || m.queuePos >= len(m.queue) {
		m.setError(errMsg)
		return nil
	}
	if !m.player.Alive() {
```

**DO**
```go
	if !m.hasCurrent {
		m.setError(errMsg)
		return nil
	}
	if m.queuePos < 0 || m.queuePos >= len(m.queue) {
		// The playing entry was deleted from the queue (queuePos == -1 is normal
		// here). Recover by advancing into the queue rather than stopping dead.
		m.setError(errMsg)
		if len(m.queue) == 0 {
			return nil
		}
		return m.nextTrack()
	}
	if !m.player.Alive() {
```

**CHECK** `go build ./... && go test ./internal/tui`

**PHASE CHECK** `go test ./internal/tui`

---

# P2 — Player (`internal/player/mpv.go`)

## F08 — A dying track evicts the *next* track's cached URL

**WHY** `playingID` is assigned when `loadfile` is *queued*, but mpv is still playing the previous
file. An `end-file`/`error` for the old track then blames the new one: spurious
`stream failed — retrying…` right after a skip, and the new track's prefetched URL is dropped.

### F08a — add the field

**FIND**
```go
	playingID    string             // videoID last handed to mpv via loadfile (cache invalidation on stream error)
```

**DO**
```go
	playingID    string             // videoID mpv actually has open (cache invalidation on stream error)
	pendingID    string             // videoID of the most recent loadfile, promoted on start-file
```

### F08b — write `pendingID` in both `Load` paths

**FIND** (cache-hit branch)
```go
		p.playingID = videoID
		p.send([]any{"loadfile", url, "replace"})
		return
```
**DO**
```go
		p.pendingID = videoID
		p.send([]any{"loadfile", url, "replace"})
		return
```

**FIND** (cache-miss goroutine, further down — same two lines without the `return`)
```go
		p.playingID = videoID
		p.send([]any{"loadfile", url, "replace"})
	}()
```
**DO**
```go
		p.pendingID = videoID
		p.send([]any{"loadfile", url, "replace"})
	}()
```

### F08c — promote on `start-file`

**FIND**
```go
		case "start-file":
			p.mu.Lock()
			p.state.Loading = false
			p.mu.Unlock()
			continue
```
**DO**
```go
		case "start-file":
			p.mu.Lock()
			p.state.Loading = false
			// mpv has the new file open: only now does an end-file error belong
			// to it rather than to the track it replaced.
			if p.pendingID != "" {
				p.playingID = p.pendingID
			}
			p.mu.Unlock()
			continue
```

**CHECK** `go build ./... && go test ./internal/player`

---

## F09 — An evicted prefetch cancels a newer claim for the same id (ABA)

**WHY** `claimResolve` evicts the oldest in-flight resolve, but the evicted goroutine still runs
`defer p.releaseResolve(videoID)` on exit, which cancels whatever claim now holds that key. Result:
a track silently never prefetches.

### F09a — give claims a generation

**FIND**
```go
	inflight     map[string]context.CancelFunc // videoID -> cancel for an in-progress resolve
```
**DO**
```go
	inflight     map[string]inflightResolve // videoID -> cancel + claim generation
	resolveSeq   int                        // monotonic claim id, so a late release can't cancel a newer claim
```

Then add the type immediately above `// maxInflightResolves caps concurrent...`:
```go
// inflightResolve is one claimed resolve slot. gen distinguishes successive
// claims of the same videoID so a goroutine whose claim was already evicted
// can't cancel its replacement on the way out.
type inflightResolve struct {
	cancel context.CancelFunc
	gen    int
}
```

### F09b — `claimResolve` returns the generation

**FIND**
```go
func (p *Player) claimResolve(videoID string) (context.Context, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.cachedLocked(videoID); ok {
		return nil, false // already resolved and still fresh
	}
	if _, ok := p.inflight[videoID]; ok {
		return nil, false
	}
	if p.inflight == nil {
		p.inflight = make(map[string]context.CancelFunc)
	}
	for len(p.inflightFIFO) >= maxInflightResolves {
		p.dropResolveLocked(p.inflightFIFO[0])
	}
	ctx, cancel := context.WithTimeout(p.baseCtx, loadTimeout)
	p.inflight[videoID] = cancel
	p.inflightFIFO = append(p.inflightFIFO, videoID)
	return ctx, true
}
```
**DO**
```go
func (p *Player) claimResolve(videoID string) (context.Context, int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.cachedLocked(videoID); ok {
		return nil, 0, false // already resolved and still fresh
	}
	if _, ok := p.inflight[videoID]; ok {
		return nil, 0, false
	}
	if p.inflight == nil {
		p.inflight = make(map[string]inflightResolve)
	}
	for len(p.inflightFIFO) >= maxInflightResolves {
		p.dropResolveLocked(p.inflightFIFO[0])
	}
	ctx, cancel := context.WithTimeout(p.baseCtx, loadTimeout)
	p.resolveSeq++
	p.inflight[videoID] = inflightResolve{cancel: cancel, gen: p.resolveSeq}
	p.inflightFIFO = append(p.inflightFIFO, videoID)
	return ctx, p.resolveSeq, true
}
```

### F09c — release only your own claim

**FIND**
```go
func (p *Player) releaseResolve(videoID string) {
	p.mu.Lock()
	p.dropResolveLocked(videoID)
	p.mu.Unlock()
}
```
**DO**
```go
// releaseResolve drops videoID's claim only when it is still the claim gen
// identifies. An evicted resolve must not cancel the claim that replaced it.
func (p *Player) releaseResolve(videoID string, gen int) {
	p.mu.Lock()
	if cur, ok := p.inflight[videoID]; ok && cur.gen == gen {
		p.dropResolveLocked(videoID)
	}
	p.mu.Unlock()
}
```

**FIND**
```go
	if cancel, ok := p.inflight[videoID]; ok {
		cancel()
		delete(p.inflight, videoID)
	}
```
**DO**
```go
	if cur, ok := p.inflight[videoID]; ok {
		cur.cancel()
		delete(p.inflight, videoID)
	}
```

### F09d — update the caller

**FIND**
```go
	ctx, ok := p.claimResolve(videoID)
	if !ok {
		return
	}
	go func() {
		defer p.releaseResolve(videoID)
```
**DO**
```go
	ctx, gen, ok := p.claimResolve(videoID)
	if !ok {
		return
	}
	go func() {
		defer p.releaseResolve(videoID, gen)
```

### F09e — fix the test

`internal/player/prefetch_test.go` calls `claimResolve`/`releaseResolve`. Update every call to the
new signatures (`ctx, gen, ok := p.claimResolve(id)` / `p.releaseResolve(id, gen)`). Do **not**
change what the test asserts.

**CHECK** `go build ./... && go test ./internal/player`

---

## F10 — Exhausting the respawn budget leaves a zombie mpv

**FIND**
```go
		p.mu.Lock()
		if p.respawns >= maxRespawns {
			p.mu.Unlock()
			p.procMu.Unlock()
			return false
		}
```
**DO**
```go
		p.mu.Lock()
		if p.respawns >= maxRespawns {
			p.mu.Unlock()
			reap(p.cmd) // still under procMu: the dead process needs waiting on
			p.procMu.Unlock()
			return false
		}
```

**CHECK** `go build ./... && go test ./internal/player`

---

## F11 — `adoptConn` can resurrect the connection after `Close`

**WHY** The `isClosed()` check sits outside the lock it protects, so `Close()` can run entirely
between the check and the assignment — leaving `p.conn` non-nil and one socket leaked.
(`isClosed` only selects on a channel, so calling it under `p.mu` is safe.)

**FIND**
```go
func (p *Player) adoptConn(conn net.Conn) {
	if p.isClosed() {
		conn.Close() //nolint:errcheck
		return
	}
	p.mu.Lock()
	if p.conn != nil {
```
**DO**
```go
func (p *Player) adoptConn(conn net.Conn) {
	p.mu.Lock()
	// The closed check must be inside the lock: Close() nils p.conn under the
	// same mutex, and a check outside it would let this resurrect the field.
	if p.isClosed() {
		p.mu.Unlock()
		conn.Close() //nolint:errcheck
		return
	}
	if p.conn != nil {
```

**CHECK** `go build ./... && go test ./internal/player`

---

## F12 — Reconnect loop can hot-spin

**WHY** The successful-redial path returns with no delay, so a connection that dies immediately
(mpv refusing IPC clients) cycles scan → recover → adopt at full speed, pegging a core.

**FIND**
```go
		if p.isClosed() {
			return
		}
		if time.Since(start) >= stableSession {
```
**DO**
```go
		if p.isClosed() {
			return
		}
		// A connection that died immediately must not spin: recover() redials
		// with no delay of its own.
		if time.Since(start) < time.Second {
			time.Sleep(time.Second)
		}
		if time.Since(start) >= stableSession {
```

**CHECK** `go build ./... && go test ./internal/player`

---

## F13 — An oversized IPC line silently drops the connection

**WHY** `bufio.Scanner` caps a token at 64 KiB and then reports EOF, which `scan` cannot distinguish
from a clean disconnect.

**FIND**
```go
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
```
**DO**
```go
	scanner := bufio.NewScanner(conn)
	// mpv can emit large property payloads; the default 64KiB token cap would
	// end the scan mid-session and look exactly like a clean disconnect.
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
```

**Do not** surface `scanner.Err()` into `p.lastErr` — the TUI consumes `lastErr` as a playback
failure and would skip a perfectly good track on a normal reconnect.

**CHECK** `go build ./... && go test ./internal/player`

---

## F14 — `Load`'s cache-hit path ignores the load epoch

**WHY** The cache-miss path checks `gen != p.loadGen` before sending `loadfile`; the cache-hit path
never does, so a `Stop()` landing between `beginLoad` and the send is undone. Latent today (all
callers are on the Bubble Tea goroutine) but it breaks the invariant `Stop()` documents.

**FIND**
```go
	if url, ok := p.cacheGet(videoID); ok {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.loadCancel != nil {
```
**DO**
```go
	if url, ok := p.cacheGet(videoID); ok {
		p.mu.Lock()
		defer p.mu.Unlock()
		if gen != p.loadGen {
			return // a newer Load or a Stop superseded this one while we read the cache
		}
		if p.loadCancel != nil {
```

**CHECK** `go build ./... && go test ./internal/player`

---

## F15 — Respawn budget measures connection uptime, not process uptime

**WHY** `start` is re-armed on every `readLoop` iteration, including pure redials where mpv never
died. An mpv healthy for ten minutes across four short connections never forgives its respawns, so
the player can wedge on `audio engine failed — restart the app`.

### F15a — add the field

**FIND**
```go
	respawns     int                           // consecutive respawn attempts (reset after a stable session)
```
**DO**
```go
	respawns     int                           // consecutive respawn attempts (reset after a stable session)
	spawnedAt    time.Time                     // when the current mpv process started (not the current connection)
```

### F15b — stamp it at both spawn sites

Find both `spawnMPV(` call sites (one in `New`, one in `recover`).

In `New`, after the spawn succeeds and the `Player` value exists, set `p.spawnedAt = time.Now()`
alongside the other initial field assignments. If the struct is built with a literal, add
`spawnedAt: time.Now(),` to it.

In `recover`, **FIND**
```go
				p.cmd = cmd
				p.mu.Lock()
				// Playback state died with the old process; keep user settings.
				p.state = State{Volume: p.state.Volume, Muted: p.state.Muted, Idle: true}
				p.restarted = true
				p.mu.Unlock()
```
**DO**
```go
				p.cmd = cmd
				p.mu.Lock()
				// Playback state died with the old process; keep user settings.
				p.state = State{Volume: p.state.Volume, Muted: p.state.Muted, Idle: true}
				p.restarted = true
				p.spawnedAt = time.Now()
				p.mu.Unlock()
```

### F15c — judge stability by process uptime

**FIND**
```go
		if time.Since(start) >= stableSession {
			p.mu.Lock()
			p.respawns = 0 // mpv was stable for a while; forgive past crashes
			p.mu.Unlock()
		}
```
**DO**
```go
		// Uptime of the mpv *process*, not of this connection: a redial after a
		// dropped socket must not restart the clock on a healthy mpv.
		p.mu.Lock()
		if !p.spawnedAt.IsZero() && time.Since(p.spawnedAt) >= stableSession {
			p.respawns = 0 // mpv was stable for a while; forgive past crashes
		}
		p.mu.Unlock()
```
The `start := time.Now()` line above `p.scan(conn)` is still used by F12 — leave it.

**CHECK** `go build ./... && go test ./internal/player`

**PHASE CHECK** `go test -race ./internal/player`

---

# P3 — Async and UI (`internal/tui/model.go`)

## F16 — `esc`-clearing search lets in-flight results repopulate the list

**WHY** `searchGen` is bumped only in `doSearch`. Clearing the results leaves in-flight
`searchDoneMsg`/`searchMoreMsg` matching the current gen, so they refill a list the user emptied.

**FIND**
```go
		if m.activeView == viewSearch && len(m.searchResults) > 0 {
			m.searchResults = nil
			m.searchCursor = 0
```
**DO**
```go
		if m.activeView == viewSearch && len(m.searchResults) > 0 {
			m.searchResults = nil
			m.searchCursor = 0
			// Invalidate requests already on the wire: dropping the continuation
			// token alone doesn't stop a page that was fetched before this esc.
			m.searchGen++
```

**CHECK** `go build ./...`

---

## F17 — A slow random-genre search hijacks playback

**WHY** `randomDoneMsg` is the only async result with no staleness guard, and its handler calls
`playNow` unconditionally — cutting off whatever the user started meanwhile.

### F17a — carry a generation

**FIND**
```go
type randomDoneMsg struct {
	tracks []api.Track
	err    error
}
```
**DO**
```go
type randomDoneMsg struct {
	tracks []api.Track
	gen    int
	err    error
}
```

### F17b — add the model field

**FIND**
```go
	helpCursor      int // help screen scroll offset (the only scroll with no visible cursor)
```
**DO**
```go
	helpCursor      int // help screen scroll offset (the only scroll with no visible cursor)
	randomGen       int // epoch for random-genre searches; a stale result must not hijack playback
```

### F17c — bump and capture

**FIND**
```go
func (m *model) playRandomGenre(seed string) tea.Cmd {
	m.setStatus("finding a random " + seed + " song...")
	client := m.api
	return func() tea.Msg {
		tracks, err := client.SearchSongs(seed)
		return randomDoneMsg{tracks: tracks, err: err}
	}
}
```
**DO**
```go
func (m *model) playRandomGenre(seed string) tea.Cmd {
	m.setStatus("finding a random " + seed + " song...")
	m.randomGen++
	gen := m.randomGen
	client := m.api
	return func() tea.Msg {
		tracks, err := client.SearchSongs(seed)
		return randomDoneMsg{tracks: tracks, gen: gen, err: err}
	}
}
```

### F17d — drop stale results

**FIND**
```go
	case randomDoneMsg:
		// Search (the random source) lives in the secret-blocked ytmusic.go, so its
		// results are cleaned here rather than at the API boundary.
		tracks := api.CleanTracks(msg.tracks)
```
**DO**
```go
	case randomDoneMsg:
		if msg.gen != m.randomGen {
			return m, nil // the user asked for something else while this was in flight
		}
		// Search (the random source) lives in the secret-blocked ytmusic.go, so its
		// results are cleaned here rather than at the API boundary.
		tracks := api.CleanTracks(msg.tracks)
```

**CHECK** `go build ./...`

---

## F18 — Retry and mpv-respawn reloads duplicate history entries

**WHY** `playAt` doubles as "user started a track" and "reload the track already playing". A stream
hiccup or an mpv respawn therefore writes a second history entry for one listen and yanks the
History cursor to row 0 under the user's fingers.

**FIND**
```go
func (m *model) playAt(idx int) {
	if idx < 0 || idx >= len(m.queue) {
		return
	}
	m.queuePos = idx
```
**DO**
```go
func (m *model) playAt(idx int) {
	if idx < 0 || idx >= len(m.queue) {
		return
	}
	// A reload of the track already playing (retry, mpv respawn) is not a new
	// play. Must be computed before m.current is reassigned below.
	// ponytail: this also means repeat-one records one entry per track, not per
	// replay — give the reload paths their own entry point if that matters.
	reload := m.hasCurrent && m.current.ID == m.queue[idx].ID
	m.queuePos = idx
```

**FIND**
```go
	// Record the play in history (newest first); persistence is debounced.
	m.cfg.AddHistory(t)
	m.markConfigDirty()
	m.historyCursor = 0
```
**DO**
```go
	// Record the play in history (newest first); persistence is debounced.
	if !reload {
		m.cfg.AddHistory(t)
		m.markConfigDirty()
		m.historyCursor = 0
	}
```

**CHECK** `go build ./... && go test ./internal/tui`

---

## F19 — Home Quick Picks never recovers from a launch-time failure

**WHY** `loadHomeQuickPicks` has exactly one call site (`Init`). A launch without network leaves half
of Home showing a stale error for the rest of the session.

### F19a — remember when the fetch ran

**FIND**
```go
func (m *model) loadHomeQuickPicks() tea.Cmd {
	m.homeQPLoading = true
	m.homeQPErr = ""
```
**DO**
```go
func (m *model) loadHomeQuickPicks() tea.Cmd {
	m.homeQPLoading = true
	m.homeQPErr = ""
	m.homeQPAt = time.Now()
```

Add the field next to the other `homeQP*` fields (they sit near `homeQPErr string`):
```go
	homeQPAt        time.Time // last Quick Picks fetch, for the retry cooldown
```

### F19b — retry on the tick while Home is open

**FIND** (in `case tickMsg:`)
```go
		m.pushMPRIS()
		cmds = append(cmds, m.nextTick())
		return m, tea.Batch(cmds...)
```
**DO**
```go
		// Quick Picks is otherwise fetched once at startup: retry slowly while the
		// user is looking at Home so a launch without network isn't broken for the
		// whole session.
		if m.activeView == viewHome && !m.homeQPLoading && m.homeQPErr != "" &&
			time.Since(m.homeQPAt) >= 30*time.Second {
			cmds = append(cmds, m.loadHomeQuickPicks())
		}
		m.pushMPRIS()
		cmds = append(cmds, m.nextTick())
		return m, tea.Batch(cmds...)
```

**CHECK** `go build ./... && go test ./internal/tui`

---

## F20 — On an artist album row, `a`/`f`/`P` act on the playing track

**WHY** `selectedTrack` returns "no selection" for album rows, so `contextTrack` falls back to the
playing track. `enter`/`l`/`right` open the highlighted album while `a` opens a different one.

### F20a — add the helper

Add immediately after `func (m *model) artistSongAt(...)`:
```go
// selectedArtistAlbum returns the album under the artist view's cursor when the
// cursor sits on an album row rather than a song row. Album rows are a real
// selection that simply isn't a track.
func (m *model) selectedArtistAlbum() (api.AlbumRef, bool) {
	if m.activeView != viewArtist {
		return api.AlbumRef{}, false
	}
	albums := m.filtAlbums(m.artistAlbums)
	if ai := m.artistCursor - len(m.trackVisibleIndices(m.artistSongs)); ai >= 0 && ai < len(albums) {
		return albums[ai], true
	}
	return api.AlbumRef{}, false
}
```
If `filtAlbums` returns something other than `[]api.AlbumRef`, use that element type instead — check
the signature rather than assuming.

### F20b — `a` opens the highlighted album

**FIND**
```go
func (m *model) goToAlbum() tea.Cmd {
	t := m.contextTrack()
	if t == nil {
		m.setError("no track selected")
		return nil
	}
```
**DO**
```go
func (m *model) goToAlbum() tea.Cmd {
	// On an artist page the cursor may sit on an album row: open that album,
	// matching what enter/l/right already do on the same row.
	if ref, ok := m.selectedArtistAlbum(); ok {
		return m.openAlbumByID(ref)
	}
	t := m.contextTrack()
	if t == nil {
		m.setError("no track selected")
		return nil
	}
```

### F20c — `f`/`P` stop retargeting the playing track

**FIND**
```go
	// Fall back to the currently playing track (Help and empty lists).
	if m.hasCurrent {
```
**DO**
```go
	// An album row is a deliberate selection that isn't a song — don't silently
	// retarget the playing track.
	if _, ok := m.selectedArtistAlbum(); ok {
		return nil
	}
	// Fall back to the currently playing track (Help and empty lists).
	if m.hasCurrent {
```
Then confirm `toggleFavoriteContext` and the `P` handler both handle a `nil` context track with an
error message rather than dereferencing it. If either does not, add the nil guard there.

**CHECK** `go build ./... && go test ./internal/tui`

---

## F21 — `?` closes help to Home and destroys the back stack

**WHY** Help is opened from anywhere by a global key but routed through `activateView`, which nils
`viewStack`. Search → album → `?` → `?` strands the user on Home.

**FIND**
```go
	case "?":
		if m.activeView == viewHelp {
			m.activateView(viewHome)
		} else {
			m.activateView(viewHelp)
		}
		return m, nil
```
**DO**
```go
	case "?":
		// Contextual, like album/artist: help returns to wherever it was opened
		// from instead of dumping the user on Home with the back stack gone.
		if m.activeView == viewHelp {
			m.activeView = m.popView()
			m.navCursor = navIndexOf(m.activeView)
		} else {
			m.pushView()
			m.clearFilter()
			m.activeView = viewHelp
			m.focus = focusPanel
			m.helpCursor = 0
		}
		return m, nil
```

**CHECK** `go build ./... && go test ./internal/tui` — if a keys test asserts `?` lands on Home,
update the assertion to "returns to the previous view" rather than reverting this fix.

---

## F22 — `/` and `S` hand the keyboard to the panel while the sidebar is focused

**WHY** Both open a panel-scoped text input without moving focus, so the sidebar still renders as
focused while `j`/`k` type letters into the filter.

**FIND**
```go
		if m.filterableView() {
			m.filtering = true
			m.filterInput.Focus()
			return m, textinput.Blink
		}
```
**DO**
```go
		if m.filterableView() {
			// The filter box lives in the panel — move focus with the keyboard.
			m.focus = focusPanel
			m.filtering = true
			m.filterInput.Focus()
			return m, textinput.Blink
		}
```

**FIND**
```go
		m.naming = true
		m.playlistInput.SetValue("")
		m.playlistInput.Focus()
		return m, textinput.Blink
```
**DO**
```go
		m.focus = focusPanel // the naming overlay renders in the panel
		m.naming = true
		m.playlistInput.SetValue("")
		m.playlistInput.Focus()
		return m, textinput.Blink
```

**CHECK** `go build ./... && go test ./internal/tui`

---

## F23 — An empty playlist name closes the overlay and drops the pending track

**FIND**
```go
			name := strings.TrimSpace(m.playlistInput.Value())
			m.naming = false
			m.playlistInput.Blur()
			if name == "" {
				m.nameTrack = nil
				m.setError("playlist name cannot be empty")
				return m, nil
			}
```
**DO**
```go
			name := strings.TrimSpace(m.playlistInput.Value())
			if name == "" {
				// Stay in the overlay so the user can just type a name — tearing it
				// down here would also discard the track being added.
				m.setError("playlist name cannot be empty")
				return m, nil
			}
			m.naming = false
			m.playlistInput.Blur()
```

**CHECK** `go build ./... && go test ./internal/tui`

---

## F24 — Quick Picks arrival clamps the Home cursor using another view's filter

**WHY** `homeLen()` applies `m.filter` when `filterableView()` says the *active* view is filterable —
and the active view need not be Home when the async message lands.

**FIND**
```go
		if m.homeCursor >= m.homeLen() {
			m.homeCursor = max(0, m.homeLen()-1)
		}
```
**DO**
```go
		// homeLen() is filter-relative to whatever view is active, which may not be
		// Home right now; activateView re-clamps on the way back in.
		if m.activeView == viewHome && m.homeCursor >= m.homeLen() {
			m.homeCursor = max(0, m.homeLen()-1)
		}
```

**CHECK** `go build ./... && go test ./internal/tui`

---

## F25 — The persisted queue has no cap

**WHY** `History` caps at 500; `Queue` does not. With auto-continue on, the queue grows every session,
is saved whole, restored, and grown again — `config.json` reaches megabytes and every debounced save
re-marshals all of it.

**FIND**
```go
func (m *model) SnapshotSession() {
	m.cfg.Queue = m.queue
	m.cfg.QueuePos = m.queuePos
```
**DO**
```go
// maxSaveQueue caps the persisted queue the way maxHistory caps history: an
// auto-continue session grows the live queue without bound, and the whole thing
// is re-marshalled on every debounced save.
const maxSaveQueue = 500

func (m *model) SnapshotSession() {
	q, pos := m.queue, m.queuePos
	if len(q) > maxSaveQueue {
		// Keep a window around the playing track so resume lands on the same song.
		start := pos - maxSaveQueue/2
		if start < 0 {
			start = 0
		}
		if start+maxSaveQueue > len(q) {
			start = len(q) - maxSaveQueue
		}
		q, pos = q[start:start+maxSaveQueue], pos-start
	}
	m.cfg.Queue = q
	m.cfg.QueuePos = pos
```
The two replaced lines are the whole FIND block — the `m.cfg.Shuffle` / `m.cfg.Repeat` / volume
lines below them stay exactly as they are.

**CHECK** `go build ./... && go test ./internal/tui`

**PHASE CHECK** `go test ./internal/tui`

---

# P4 — Render (`internal/tui/render.go`)

## F26 — The sidebar has no scroll window; the cursor can vanish

**WHY** `buildSidebar` emits a fixed 9 rows (2 headers + 7 nav entries) and `padToHeight` truncates
from the bottom. On an 80×20 terminal with a track playing, `contentH` is 9 → the last entries and
the highlight disappear, though Enter still opens the hidden entry.

**FIND**
```go
	body := padToHeight(strings.Join(rows, "\n"), h-2)
```
**DO**
```go
	// Window on the cursor like every panel list: padToHeight alone truncates
	// from the bottom, so on a short terminal the selection scrolls out of view.
	// +2 skips the title and "Quick Links" heading rows.
	body := padToHeight(windowRows(rows, m.navCursor+2, h-2), h-2)
```

**CHECK** `go build ./... && go test ./internal/tui`

---

## F27 — The first few `j` presses in Help do nothing

**WHY** `m.helpCursor` is an offset (there is no visible cursor in Help) but it is passed to
`windowRows`, which *centers* — so nothing moves until the cursor passes half a screen.

**FIND**
```go
	return styleContentBox.Width(inner).Render(windowRows(rows, m.helpCursor, bodyH))
```
**DO**
```go
	// helpCursor is a scroll offset, not a cursor: windowRows would centre it and
	// swallow the first half-screen of j presses.
	start := m.helpCursor
	if maxStart := len(rows) - bodyH; start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}
	end := start + bodyH
	if end > len(rows) {
		end = len(rows)
	}
	return styleContentBox.Width(inner).Render(strings.Join(rows[start:end], "\n"))
```

**CHECK** `go build ./... && go test ./internal/tui`

**PHASE CHECK** `go test ./internal/tui`

---

# P5 — MPRIS and main

## F28 — Every song announces itself twice

**FILE** `internal/mpris/mpris.go`

**WHY** The change-detection key includes `LengthUS`, which arrives ~1s after the track (mpv reports
duration once the file is open), and `metadata()` mints a fresh `mpris:trackid` on every rebuild.
Shells therefore fire two now-playing notifications per song, the first showing 0:00.

### F28a — track length separately

**FIND**
```go
	lastKey   string // change detection for Metadata
```
**DO**
```go
	lastKey   string // track identity (title/artist/album) for Metadata change detection
	lastLen   int64  // last published mpris:length; arrives a beat after the track
```

### F28b — update in place when only the length changed

**FIND**
```go
	key := n.Title + "\x00" + n.Artist + "\x00" + n.Album + "\x00" + fmt.Sprint(n.LengthUS)
	if !n.HasTrack {
		key = ""
	}
	if key != s.lastKey {
		s.lastKey = key
		s.props.SetMust(playerIface, "Metadata", s.metadata(n))
	}
```
**DO**
```go
	key := n.Title + "\x00" + n.Artist + "\x00" + n.Album
	if !n.HasTrack {
		key = ""
	}
	// The duration lands a beat after the track does. Publishing it must update
	// the existing trackid, not mint a new one — a new trackid reads as a new
	// song and makes every track notify twice.
	if key != s.lastKey || n.LengthUS != s.lastLen {
		newTrack := key != s.lastKey
		s.lastKey, s.lastLen = key, n.LengthUS
		s.props.SetMust(playerIface, "Metadata", s.metadata(n, newTrack))
	}
```

### F28c — mint the trackid only for a new track

**FIND**
```go
func (s *Server) metadata(n Now) map[string]dbus.Variant {
	if !n.HasTrack {
		s.trackPath = ""
		return map[string]dbus.Variant{}
	}
	s.trackSeq++
	s.trackPath = dbus.ObjectPath(fmt.Sprintf("/org/ytmusic/track/%d", s.trackSeq))
```
**DO**
```go
func (s *Server) metadata(n Now, newTrack bool) map[string]dbus.Variant {
	if !n.HasTrack {
		s.trackPath = ""
		return map[string]dbus.Variant{}
	}
	if newTrack || s.trackPath == "" {
		s.trackSeq++
		s.trackPath = dbus.ObjectPath(fmt.Sprintf("/org/ytmusic/track/%d", s.trackSeq))
	}
```
Grep for other `s.metadata(` callers and pass `true` from any that publish a fresh track.

**CHECK** `go build ./... && go test ./internal/mpris`

---

## F29 — `^C` exits 1 with `error: program was interrupted`

**FILE** `cmd/ytmusic/main.go`

**WHY** bubbletea returns `tea.ErrInterrupted` on SIGINT. `run()` treats it as a failure, so a
systemd unit or wrapper script reads a clean user quit as a crash.

**FIND**
```go
import (
	"fmt"
	"os"
```
**DO**
```go
import (
	"errors"
	"fmt"
	"os"
```

**FIND**
```go
	if runErr != nil {
		return fmt.Errorf("error: %w", runErr)
	}
```
**DO**
```go
	// SIGINT is how a user quits a TUI — not a failure to report or exit 1 on.
	if runErr != nil && !errors.Is(runErr, tea.ErrInterrupted) {
		return fmt.Errorf("error: %w", runErr)
	}
```

**CHECK** `go build ./... && ./ytmusic --version` (build a binary first if needed)

**PHASE CHECK** `go test ./internal/mpris && go build ./...`

---

# P6 — Test and lint fixes

## F30 — Flaky volume-order assertion

**FILE** `internal/mpris/volume_order_test.go`

**WHY** The worker legitimately coalesces, so the same value can be applied twice. `<=` treats that
duplicate as a reordering and fails the test intermittently.

**FIND**
```go
		if got[i] <= got[i-1] {
			t.Fatalf("volume applied out of order: %v", got)
		}
```
**DO**
```go
		// A repeated value is the coalescer applying the newest pending level
		// twice, not a reorder — only a decrease means out of order.
		if got[i] < got[i-1] {
			t.Fatalf("volume applied out of order: %v", got)
		}
```

## F31 — Deadlock on the test's own failure path

**FILE** `internal/mpris/volume_order_test.go`

**WHY** `mu.Lock()` then `t.Fatalf` (which calls `runtime.Goexit`) never unlocks — the deferred
unlock is only registered later — so the `SetVolume` handler wedges and hides the real failure.

**FIND**
```go
		case <-deadline:
			mu.Lock()
			t.Fatalf("final volume 1.0 never applied; applied %v", got)
```
**DO**
```go
		case <-deadline:
			mu.Lock()
			applied := fmt.Sprint(got)
			mu.Unlock()
			t.Fatalf("final volume 1.0 never applied; applied %v", applied)
```
Add `"fmt"` to the test's imports if it isn't already there.

## F32 — The repo's only two lint failures

**FILE** `internal/player/ipc_test.go`

**FIND**
```go
			a.Close()
			b.Close()
```
**DO**
```go
			a.Close() //nolint:errcheck
			b.Close() //nolint:errcheck
```

**PHASE CHECK** `golangci-lint run ./... && go test -count=1 ./internal/mpris ./internal/player`

---

# P7 — Regression tests (add exactly these three, no more)

All three must work with `New(nil, cfg)` — **do not** write TUI tests that press keys reaching
player methods; there is no player in the test model. Follow the existing style in
`internal/tui/keys_test.go` and `cursor_test.go`.

**T1** — `internal/tui/shuffle_test.go`: `TestNextShuffleIdxSingleTrack`. Model with a one-track
queue, `queuePos = 0`; assert `m.nextShuffleIdx() == -1`. Add a second case with a three-track queue
asserting the result is in range and never equals `queuePos`. Guards F05.

**T2** — `internal/tui/search_gen_test.go`: `TestEscClearingSearchInvalidatesInFlight`. Set
`m.activeView = viewSearch`, `m.searchResults` to two tracks, `m.focus = focusPanel`; record
`gen := m.searchGen`; send `esc` through `Update`; assert `m.searchResults == nil` **and**
`m.searchGen != gen`. Guards F16.

**T3** — extend `internal/tui/render_bounds_test.go`: `TestHelpScrollsFromFirstPress`. Render help at
a height smaller than `helpRowCount()` with `helpCursor = 0`, then with `helpCursor = 1`, and assert
the first line of the two renders differs. Guards F27.

**PHASE CHECK** `go test ./internal/tui`

---

# P8 — API layer (BLOCKED — needs the user)

`internal/api/ytmusic.go` contains a literal Google API key (the public Innertube web key), so the
repo's secret scanner refuses to let any tool read or edit that file. **Do not attempt these fixes
silently.** Ask the user to re-run this phase with `[allow-secret]` in their message, then apply:

- **A1** — `post` never inspects `resp.StatusCode`, so a 403/429/5xx renders as an empty Home/Search
  with no error. Add `if resp.StatusCode/100 != 2 { return nil, fmt.Errorf("innertube %s: %s", endpoint, resp.Status) }`
  before `io.ReadAll`.
- **A2** — `b, _ := json.Marshal(payload)` drops the error and POSTs an empty body. Return it.
- **A3** — No request takes a `context.Context`; a quit or view switch leaves each stale request
  holding a socket for up to 15s. Thread a ctx through `post` and use `http.NewRequestWithContext`.
- **A4** — `io.ReadAll(resp.Body)` is uncapped. Wrap in `io.LimitReader(resp.Body, 32<<20)`.

These three are readable and can be done now, in the same phase:

- **A5** — `internal/api/album.go`, in `pickAlbumCandidate`'s last-resort tier:
  `if strings.HasPrefix(w, norm(c.title))` matches *every* query when a candidate's title is blank.
  Skip blank candidates: `if norm(c.title) == "" { continue }` at the top of that loop.
- **A6** — `internal/api/explore.go`: `if byline == nil {` never fires for `"runs": []`, which
  `json.Unmarshal` turns into a non-nil empty slice, so the `shortBylineText` fallback is dead.
  Change to `if len(byline) == 0 {`.
- **A7** — `internal/api/artist.go`, in `sanitizeDisplay`: the `r >= 0x200b && r <= 0x200f` case
  strips U+200D (ZWJ) and splits emoji, contradicting the function's own doc comment. Exclude
  `0x200d` from the range.

**PHASE CHECK** `go test ./internal/api`

---

# Deliberately NOT fixed

Do not "fix" these. They were considered and rejected:

- **MPRIS volume worker goroutine is never stopped.** Closing its `wake` channel in `Close()` races
  a live D-Bus callback's non-blocking send → send on a closed channel → panic. One parked goroutine
  at process exit is cheaper than that risk.
- **`pageSize()` over-counts in Search/Home/Album.** It is exactly right for Queue/Favorites/History
  (the panel is not boxed: `listH = contentH - 1 header = viewportH - 1`). Only views with extra
  chrome differ, and fixing it properly means every renderer publishing its own list height. Not
  worth it; `ctrl+f` lands a couple of rows further than a strict page in three views.
- **`nextTick` cadence lag** (up to 2s of frozen progress bar after starting from idle). Fixing it
  needs idempotent tick re-arming; the cost is a whole new invariant to hold. Leave it.
- **Two-broken-track ping-pong under repeat-all.** F04 breaks the self-loop only; see its
  `ponytail:` comment for the upgrade path if it ever shows up in practice.

---

# Final gauntlet

Run all of it, from a clean tree, before declaring done:

```bash
go build ./...
go vet ./...
gofmt -l . | grep -v vendor    # must print nothing
golangci-lint run ./...        # must be clean
go test -race -count=1 ./...
```

Then a smoke test — the tests do not cover any of the interactive paths:

```bash
go run ./cmd/ytmusic
```
1. `?` twice from the Queue view → you land back on the Queue, not Home (F21).
2. `tab` to the sidebar, then `/` → focus visibly moves to the panel (F22).
3. Resize the terminal to ~20 rows with a track playing → the sidebar cursor stays visible (F26).
4. `?`, then a single `j` → the help text scrolls by one line immediately (F27).
5. `S`, type the name of an existing playlist, Enter → a confirmation prompt appears (F02).
6. `^C` → clean exit, no `error:` line on stderr, `echo $?` prints 0 (F29).
