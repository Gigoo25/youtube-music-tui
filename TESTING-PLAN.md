# Test suite completion plan

Handoff document. Follow it top to bottom. Each task is self-contained: it names
the file to create, the functions to cover, the contract each test defends, and
the mutation that must make it fail.

Current state: 59 tests, `-race` clean.
Coverage: `tui` 58.6% · `mpris` 68.2% · `config` 34.8% · `api` 16.9% · `player` 8.7%.

---

## Rules — read before writing a single test

1. **A test that cannot fail is worse than no test.** Every test defends an
   observable contract. If you cannot name the bug it catches, delete it.
2. **Mutation-verify every test.** After it passes, break the implementation it
   covers, re-run, confirm it *fails*, then restore the implementation. A test
   that still passes against broken code is worthless — rewrite it. Each task
   below names the mutation to apply.
3. **No new dependencies.** Standard library and what is already in `go.mod`.
   No testify, no mock framework, no golden-file library.
4. **No network, no real `yt-dlp`, no real `mpv` process** in any test. Where a
   task needs an external process, it says exactly how to fake it.
5. **Table-driven where there are 3+ cases**, plain functions otherwise. Match
   the style already in `internal/api/clean_test.go`.
6. **Do not add `t.Parallel()`.** Several packages touch process-global state
   (`PATH`, D-Bus).
7. **Comment the *why*, not the *what*.** See `internal/mpris/volume_order_test.go`
   for the expected tone: the comment explains the bug the test exists to catch.
8. **Do not modify non-test code** unless a task explicitly says to. If a test
   reveals a bug, write the test so it fails, then stop and report it — do not
   silently "fix" production code to make your test green.

### Commands

```
go test ./...                 # fast loop
go test -race ./...           # must pass before you finish
go test -cover ./internal/... # progress check
gofmt -l cmd internal         # MUST print nothing
golangci-lint run ./...       # MUST print "0 issues."
```

Run all four before declaring any task done. `gofmt` is not in `golangci-lint`'s
default set, so an unformatted file passes lint — check it separately.

### Finding what is uncovered

```
go test -coverprofile=/tmp/c.out ./internal/...
go tool cover -func=/tmp/c.out | awk '$3=="0.0%"'
```

---

## Phase 1 — `internal/player` (8.7% → target 50%+)

Highest value. Every concurrency bug found in this repo so far was found by
reading, not by tests. Do this phase first.

### Task 1.1 — `internal/player/ipc_test.go` — IPC event parsing

**Cover:** `scan` (mpv.go:718), `send`, `isClosed`, `State`, `TrackEnded`,
`LoadError`, `Alive`, `Restarted`.

`scan(conn net.Conn)` reads newline-delimited JSON from a connection and updates
`p.state`. It needs no mpv: use `net.Pipe()`, write JSON lines into one end, run
`scan` on the other in a goroutine, assert on `p.State()`.

Build the player with a helper (copy the shape of `testPlayer()` in
`prefetch_test.go`, adding the channels `scan` touches):

```go
func scanPlayer() *Player {
	p := &Player{
		sendCh: make(chan []byte, 64),
		done:   make(chan struct{}, 1),
		closed: make(chan struct{}),
	}
	p.baseCtx, p.baseCancel = context.WithCancel(context.Background())
	return p
}
```

One test per property observer:

| Test | Input line | Assert |
|---|---|---|
| `TestScanUpdatesPosition` | `{"event":"property-change","id":1,"data":12.5}` | `State().Position == 12.5` |
| `TestScanUpdatesDuration` | id `2`, data `180.0` | `State().Duration == 180` |
| `TestScanUpdatesPaused` | id `3`, data `true` | `State().Paused` |
| `TestScanUpdatesVolume` | id `4`, data `72.0` | `State().Volume == 72` |
| `TestScanUpdatesIdle` | id `5`, data `true` | `State().Idle` |
| `TestScanUpdatesMuted` | id `6`, data `true` | `State().Muted` |
| `TestScanIgnoresMalformedJSON` | `not json`, then a valid position line | position still applied, no panic |

The observer ids are assigned in `observeProperties` (mpv.go:192). Read them
there — do not guess.

**Then the three that actually matter:**

- `TestTrackEndedOnlyOnNaturalEOF` — feed `{"event":"end-file","reason":"eof"}`
  → `TrackEnded()` true. On a fresh player, feed `reason":"stop"` and
  `"redirect"` → `TrackEnded()` must be **false**. This guard is what stops the
  queue auto-advancing every time a new file is loaded.
  *Mutation:* delete the `switch resp.Reason` in `scan` so every `end-file`
  signals `done`.

- `TestStreamErrorDropsCachedURL` — `p.cachePut("vid", "http://x")`, set
  `p.playingID = "vid"`, feed `{"event":"end-file","reason":"error"}`, assert
  `cacheGet("vid")` misses and `LoadError()` is non-empty. Without this a retry
  replays the same dead URL forever.
  *Mutation:* remove the `delete(p.urlCache, p.playingID)` line.

- `TestSendDropsAfterClose` — `close(p.closed)`, call `p.send([]any{"stop"})`,
  assert nothing was queued on `p.sendCh`. Then on a fresh player fill `sendCh`
  to capacity and assert `send` does not block (guard with `time.After`).
  *Mutation:* remove the `if p.isClosed() { return }` guard in `send`.

### Task 1.2 — `internal/player/volume_test.go` — volume arithmetic

**Cover:** `clampVolume`, `nudgeVolume`, `VolumeUp`, `VolumeDown`, `SetVolume`.

Table-drive `clampVolume`: `-10 → 0`, `0 → 0`, `100 → 100`, `150 → 150`,
`200 → 150`. Then `TestVolumeUpClampsAtCeiling`: set `p.state.Volume = 148`,
call `VolumeUp()` twice, assert the **returned value** is `150` both times.
Mirror with `VolumeDown` at `2`.

Assert on the return value, not on what reached `sendCh` — the return is what
the TUI paints in the shortcuts bar. If it ever exceeds mpv's clamp the bar
shows a level mpv is not at.

*Mutation:* change `clampVolume` to `return vol`.

### Task 1.3 — `internal/player/cache_test.go` — URL cache

**Cover:** `cachePut`, `cacheGet`, `cachedLocked`, and the eviction sweep.

- Round-trip: put then get.
- TTL expiry: put, then reach into `p.urlCache["vid"]` and rewrite its `at`
  field to `time.Now().Add(-2 * urlTTL)`, assert `cacheGet` misses. Do **not**
  `time.Sleep` — backdate the entry.
- Eviction: put more entries than the sweep threshold (read `cachePut` for the
  actual rule) and assert stale entries are gone while fresh ones survive.

*Mutation:* change the `time.Since(c.at) <= urlTTL` comparison to `true`.

### Task 1.4 — `internal/player/load_test.go` — load epochs

**Cover:** `beginLoad`, `Stop`.

`Load` shells out to `yt-dlp` — do **not** call it. Test the epoch bookkeeping:

- `TestBeginLoadBumpsGeneration` — two `beginLoad` calls return different gens,
  and the first call's ctx is cancelled by the second.
- `TestStopCancelsInFlightLoad` — `beginLoad("a")`, then `Stop()`, assert the
  returned ctx is cancelled and `loadGen` advanced. This is what stops an
  abandoned resolve resurrecting playback after the user cleared the queue.
- `TestStopClearsLoadingFlag` — set `p.state.Loading = true`, `Stop()`, assert
  `State().Loading` is false.

*Mutation:* remove the `p.loadGen++` from `Stop`.

### Task 1.5 — `internal/player/extract_test.go` — yt-dlp handling

**Cover:** `extractURL` and its error classification.

Fake the binary — `extractURL` looks up `yt-dlp` on `PATH`:

```go
func fakeYtdlp(t *testing.T, script string) {
	dir := t.TempDir()
	path := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}
```

Cases:
- success — `echo https://stream.example/x` → returns that URL, `resolved` true.
- not found — `t.Setenv("PATH", t.TempDir())` (empty dir) → error satisfies
  `errors.Is(err, errNoYtdlp)`. The TUI shows a specific "install yt-dlp"
  message off this; a generic error regresses it silently.
- failure — `echo "ERROR: video unavailable" >&2; exit 1` → error message
  contains the stderr text, `resolved` false.
- cancellation — script `sleep 5`, ctx with a 50ms timeout → returns promptly
  (assert elapsed < 1s), `resolved` false.

*Mutation:* replace the `errNoYtdlp` branch with a plain `fmt.Errorf`.

---

## Phase 2 — `internal/api` (16.9% → target 40%+)

**Do not fetch anything from YouTube.** Every test here is either a pure
function or a parser fed a hand-written fixture.

### Task 2.1 — `internal/api/album_pick_test.go` — `pickAlbumCandidate`

Pure function, zero dependencies, currently 0%. `internal/api/album.go:253`.

Read the tier logic first, then table-drive **every tier and every boundary**:
- exact title match beats a prefix match
- prefix match beats a substring match
- case and surrounding whitespace ignored
- empty `want` → `ok == false`
- empty candidate list → `ok == false`
- nothing matches → `ok == false`
- **two candidates in the same tier → the earlier slice entry wins, every time**
  (loop the pick 50×; this is the end-to-end guarantee `walkFirst`'s sorted
  descent exists to provide)

*Mutation:* reorder the tier checks.

### Task 2.2 — `internal/api/parse_test.go` — renderer extraction

**Cover:** `extractTwoRowTrack`, `extractPanelTrack`, `addTrack`,
`rendererVideoType`, `listItemVideoType`, `isYear`, `looksLikeStat`,
`headerTitle`, `albumHeaderArtist`.

`isYear` and `looksLikeStat` are pure — table-drive them first (`"2019"` yes,
`"219"` no, `"20199"` no, `"1.2M plays"` is a stat, `"Daft Punk"` is not).

For the extractors, hand-write **minimal** fixtures as `map[string]any` literals
— the smallest shape the function actually reads. Do not paste a captured
multi-megabyte response into the repo. Read the function, note which keys it
touches, build exactly those:

```go
r := map[string]any{
	"title": map[string]any{"runs": []any{map[string]any{"text": "Song"}}},
	// ...only the keys the function reads
}
```

Contracts to defend:
- a renderer missing its video id yields no track (`addTrack` must skip it)
- `addTrack` does not append a duplicate of an id already present
- a subtitle run that is a year or a play count is not mistaken for the artist
  name (that is what `isYear`/`looksLikeStat` guard)

*Mutation:* make `addTrack` append unconditionally.

### Task 2.3 — `internal/api/dedupe_test.go` — `dedupeTracks`

`internal/api/charts.go:190`. Pure. Assert: duplicates by ID collapse, **the
first occurrence's position is kept**, empty IDs are dropped, and a nil slice
returns whatever the code actually returns (read it — do not assume nil).

*Mutation:* make it keep the last occurrence instead of the first.

### Task 2.4 — extend `internal/api/walk_test.go`

Add `findSearchContinuation` cases to the existing file (it already covers
`walkFirst`, `walkRenderers` and `findBrowseID`):
- `continuationCommand.token` is preferred
- falls back to `nextContinuationData.continuation`
- returns `""` when neither is present — this is how paging knows to stop, so a
  wrong non-empty return makes search loop forever

---

## Phase 3 — `internal/config` (34.8% → target 75%+)

Pure data manipulation plus one atomic file write. Cheap, mechanical, high
value — this file holds the user's favourites, playlists and queue.

### Task 3.1 — `internal/config/mutations_test.go`

**Cover:** `IsFavorite`, `ToggleFavorite`, `SavePlaylist`, `DeletePlaylist`,
`AddToPlaylist`, `RemoveFromPlaylist`, `PlaylistByName`, `AddHistory`.

One test per function. Contracts that matter:
- `ToggleFavorite` is an involution — toggling the same track twice restores the
  original state exactly (same length, same order).
- `AddToPlaylist` on a name that does not exist — read the code and assert what
  it actually does. Do not assume.
- `RemoveFromPlaylist` with an absent index/id is a no-op: not a panic, and not
  a silent wrong deletion.
- `AddHistory` caps its length, newest first, and re-adding a track already
  present moves it to the front rather than duplicating it (confirm against the
  code before asserting).
- `PlaylistByName` — read whether it is case-sensitive, then pin that behaviour.

*Mutation:* off-by-one the history cap.

### Task 3.2 — extend `internal/config/config_test.go`

- **Perms:** after `Save`, `os.Stat` the file and assert mode `0600`. It can
  contain a session cookie.
- **Atomicity:** `Save` twice, assert no `.tmp` file is left in the directory.
- **Corrupt file:** write `{invalid json` to the config path and assert `Load`
  returns a usable default rather than a zero value or a panic — a corrupt
  config must not brick the app on launch.
- **Missing directory:** point the config path at a non-existent nested
  directory and assert `Save` creates it.

Use `t.TempDir()` and whatever path override the package already supports. Read
`config.go` for how the path resolves; never write to the real `~/.config`.

---

## Phase 4 — `internal/tui` (58.6% → target 70%)

Lowest priority. Already the best-covered package, and its remaining gaps are
render paths where a test mostly restates the implementation. Write only these
three.

### Task 4.1 — `pageSize` accounts for panel chrome

`model.go:1389`. `pageSize` feeds ctrl+d/ctrl+u/ctrl+f/ctrl+b. It subtracts the
list header row, and one more when the naming or filter line is showing.

Set `m.viewportH = 20`, assert `pageSize() == 19`. Set `m.filter = "x"`, assert
`18`. Same for `m.filtering = true` and `m.naming = true`. Then assert the
`viewportH == 0` pre-render fallback returns `10`.

*Mutation:* delete the `n--` branch.

### Task 4.2 — cursor clamping after a list shrinks

Removing the last item of a list must not leave the cursor out of range. Fill
`m.queue` with 5 tracks, set `m.queueCursor = 4`, delete the last entry through
the key handler that does it, assert `m.queueCursor` lands in `[0, len-1]`.
Repeat for favourites and history.

This is an index-out-of-range panic class, so also call `m.View()` afterwards
and assert it does not panic.

### Task 4.3 — filter clearing on view transitions

Every view transition must clear the local `/` filter, or the new view opens
showing a filtered subset with no filter line visible. Set `m.filter = "zzz"`,
trigger each view switch (the number keys, and `openPlaylistPicker`), assert
`m.filter == ""` after each. Table-drive over the view constants.

*Mutation:* remove one `clearFilter()` call.

---

## Definition of done

- [ ] `go test -race ./...` passes
- [ ] `gofmt -l cmd internal` prints nothing
- [ ] `golangci-lint run ./...` prints `0 issues.`
- [ ] `player` >= 50%, `api` >= 40%, `config` >= 75% statement coverage
- [ ] Every new test mutation-verified: you broke the code, watched the test
      fail, restored the code
- [ ] No new entries in `go.mod`
- [ ] No production code changed (except where a task said to). Any bug a test
      uncovered is reported, not quietly patched.

Report at the end: coverage delta per package, the list of mutations you applied
and confirmed, and any bug a test exposed.
