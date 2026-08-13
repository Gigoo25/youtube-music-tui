# CLAUDE.md

Guidance for working in this repo. A fast, low-dependency Go TUI music player for
YouTube Music — a reimplementation of the Node/Ink app
[involvex/youtube-music-cli](https://github.com/involvex/youtube-music-cli).

## What it is

A terminal UI that searches YouTube Music, builds a queue, and plays audio. The UI
is a left **Quick Links sidebar** + main content **panel**, a persistent
now-playing bar, and a context-aware shortcuts bar. Navigation is focus-based
(sidebar ⇄ panel), with vim-style `j/k` movement.

## Build / run / test

```bash
go build ./...                 # build everything
go run ./cmd/ytmusic           # run the app
go test ./...                  # run tests (api, config, mpris, player, tui)
go vet ./...                   # vet
go build -o ytmusic ./cmd/ytmusic   # produce a binary

make build                     # dev binary (= go build -o ytmusic ./cmd/ytmusic)
make release                   # stripped binary, ~34% smaller (-trimpath -ldflags='-s -w')
make lint                      # gofmt check + golangci-lint (gofmt is NOT in the default linter set)
```

**Runtime requirements (external binaries, not Go deps):**
- `mpv` — audio-only playback (`--no-video`), driven over a JSON IPC unix socket.
- `yt-dlp` — resolves a stream URL from a videoId (`yt-dlp -f bestaudio -g`), so
  playback is the audio track only (YouTube Music style), never video.

MPRIS (now-playing + media-key control for noctalia/playerctl/GNOME/KDE) is served
**in-process** over D-Bus (see `internal/mpris`); the `mpv-mpris` plugin is no
longer used. Requires a session bus; absence is non-fatal (logged, MPRIS off).

On NixOS, run inside `nix-shell -p mpv yt-dlp`.

## Dependencies

Keep dependencies minimal — this is a goal of the project. Current Go deps:
`charmbracelet/bubbletea` + `bubbles` + `lipgloss` (TUI) and `godbus/dbus/v5`
(in-process MPRIS server). Everything else is the standard library. Don't add a
dependency without a strong reason; prefer stdlib + the external `mpv`/`yt-dlp`
binaries.

**`vendor/` carries one local patch.** `godbus/dbus/v5`'s `storeMapIntoMap` merged
each store into the map it already held, and `prop.Properties` hands that same map
to the encoder — both in `PropertiesChanged` and in every `Get`/`GetAll` reply. So a
shell polling MPRIS (noctalia, playerctl) iterated the live `Metadata` map on the
D-Bus goroutine while `Server.Update` wrote it: an unrecoverable
`concurrent map iteration and map write`, and stale keys that made a track with no
album show the previous track's. It now installs a fresh map per store. **Running
`go mod vendor` silently reverts this** — re-apply it, or `internal/mpris`
`TestMetadata*` will start failing and `go test -race ./internal/mpris` will flake.

## Layout

```
cmd/ytmusic/main.go      Entry point: run() wires config + player + TUI, owns cleanup.
internal/api/            YouTube Music Innertube client (raw HTTP + JSON walking).
  ytmusic.go             Core client: post(), clientCtx(), Search(), dig/digSlice/
                         str/extractTexts/extractTrack. ⚠ holds the public Innertube
                         API key — see "Secret canary" below.
  types.go               api.Track{…} + AlbumRef{…} + ArtistResult{Name,Songs,Albums}.
  client.go              SetTimeout() — sets the shared http.Client request timeout
                         (lives outside ytmusic.go to avoid the secret canary).
  charts.go              Trending() (browse endpoint, FEmusic_charts) — only used as
                         the Home Quick Picks fallback now + walkRenderers/
                         walkRenderersMulti, dedupeTracks, collectListItemTracks.
  search.go              SearchSongs() (Songs-tab filter → official ATV audio, not
                         music videos) + extractSongRow.
  explore.go             Related() (next endpoint, radio/auto-continue) + extractTwoRowTrack/
                         extractPanelTrack. Songs-only: filters to musicVideoType ATV and,
                         when the seed is a music video (all-video queue), remaps it to the
                         song version via SearchSongs and re-fetches once. Panel bylines
                         parse as Artist • Album • Year (videos: Artist • views • likes).
  album.go               AlbumByQuery()/AlbumByID() — search→album browseId→browse tracks.
                         Rows whose id links a music video (labels sometimes attach the OMV
                         even when a song version exists — metadata says 3:12, video is 5:54)
                         are remapped to the ATV via a per-row SearchSongs lookup
                         (remapVideoRows, 4-way concurrent, best-effort).
  artist.go              ArtistByQuery() — search→artist browseId→top songs + albums.
internal/player/mpv.go   mpv subprocess + IPC: send channel, readLoop, reconnect,
                         async Load (yt-dlp, ctx-cancelled on supersede/Stop),
                         property observation, Close.
internal/mpris/mpris.go  In-process MPRIS (org.mpris.MediaPlayer2) D-Bus server.
                         Handlers{…} control callbacks + Update(Now) to publish
                         state. Wired in main.go; the TUI forwards control calls
                         into the Update loop via Program.Send (internal/tui/mpris.go).
internal/config/config.go  JSON persistence of favorites/history/volume/autoContinue.
internal/tui/
  model.go               bubbletea model: state, Update, key routing, actions.
  mpris.go               MPRIS glue: message types, Handlers builder, pushMPRIS.
  render.go              View() + all render funcs (sidebar, panel, now-bar, bars).
  styles.go              lipgloss color theme, ICONS, box styles.
  keys_test.go           Key-routing + render + regression tests.
```

## Architecture notes

**TUI (bubbletea, Elm-style).** One `Update` dispatch. A 500ms `tickMsg` refreshes
the player snapshot and advances the queue on track end. Async work (search,
album, radio, random) runs in `tea.Cmd`s returning typed messages
(`searchDoneMsg`, `albumDoneMsg`, …).

- **Views** (`view` enum): Home, Search, Queue, Favorites, History, Album, Artist,
  Playlists, Help — plus contextual PlaylistDetail (enter/l on a playlist; esc
  back) and PlaylistPick (`P` on any selected song adds it to a playlist, with a
  "new playlist…" row that reuses the naming overlay via `nameTrack`). Playlists
  are queue-oriented: `p` replaces the queue, `e` appends — labels say so. Default view is **Home** (panel focused, not typing). Home = Listen Again
  (local history, deduped) + Quick Picks (Related to most-recent play, falling back
  to Trending); both are one flat-cursor list. Album and Artist are contextual
  (opened with `a`/`A`, `esc` pops the `viewStack` return path); they aren't sidebar
  entries.
  The old browse tabs (Trending/New Releases/Explore) were removed — their feeds
  lacked album/duration and were video-heavy; discovery is via Home, `R` radio,
  `z` random, and `C` auto-continue.
- **Per-tab icons**: each sidebar tab + its panel heading uses a distinct Nerd Font
  glyph (`viewIcon`). Nerd Font required; glyphs are width-1 so the layout aligns.
- **Focus model**: `focusSidebar` / `focusPanel`. `tab` toggles focus. Unified
  back/open rules (keep them consistent when adding views): `h`/`left`/`esc` step a
  *contextual* view (Album/Artist/Genres/PlaylistDetail/PlaylistPick) back to where
  it was opened from (`backFromContextual`), and return a *top-level* view's focus
  to the sidebar; `l`/`right` "open" the selection wherever something can be opened
  (sidebar entry, playlist → detail, artist album → album view). `1-6` jump to
  views. Track-list keys are uniform: `enter` queue, `p` play (album/playlist
  views: replace queue), `e` queue all, `d`/`x` remove, `f` fav, `P` add-to-
  playlist, `/` filter — selection for global actions comes from `selectedTrack`
  (one source; `contextTrack` adds the now-playing fallback).
- **Selection rendering**: the focused pane's selected row gets a full-width inverse
  highlight; unfocused/secondary selection gets a subtle marker. Use
  display-width-aware helpers (`truncate2`, `padRight`) for any styled/ANSI row —
  see the gotcha below.

**Player (mpv IPC).** Commands are newline-delimited JSON over a unix socket,
serialized through `sendCh` + `writeLoop`. `Load(videoID)` runs yt-dlp in a
goroutine then sends `loadfile`. Only `end-file` with `reason == "eof"` advances the
queue (a `loadfile replace` fires `reason == "stop"` for the previous file — do not
treat that as a finished track).

**Playback recovery (playback must never silently stop).** Layered:
ffmpeg-level stream reconnect (`--stream-lavf-o=reconnect…`, `--network-timeout=15`);
if mpv dies, `readLoop` → `recover()` redials then respawns mpv (≤3 consecutive,
counter resets after a 60s-stable session) and sets a `Restarted` flag the TUI
polls to reload the current track and seek back (`pendingSeek`); a stream error
invalidates that track's cached URL and surfaces via `LoadError`, which the TUI's
`handlePlaybackFailure` answers with one fresh-URL retry then auto-skip; a
`watchdogCheck` in the tick treats a >20s frozen position while unpaused as a
stream failure. There is no watch-page-URL fallback in `extractURL` — mpv runs
`--ytdl=no`, so it would always fail; resolve errors fail fast instead.

## Conventions & gotchas

- **Secret canary on `internal/api/ytmusic.go`.** The file embeds the *public*
  YouTube Music web Innertube key, which trips the repo's secret scanner — reads and
  edits of that file are blocked. The other `internal/api/*.go` files reuse its
  helpers (`post`, `clientCtx`, `dig`, `digSlice`, `str`, `extractTexts`,
  `extractTrack`) and are freely editable. If you must change `ytmusic.go`, the user
  has to allow it with `[allow-secret]`. Adding new API parsing? Put it in a new file
  in `package api` and reuse those helpers (see `album.go` / `explore.go`).

- **Don't store pointers into the `queue` slice.** `append` reallocates the backing
  array. The now-playing track is `m.current api.Track` (a value copy) + `hasCurrent
  bool`, never `&m.queue[i]`. When mutating the queue (remove/clear/prepend), keep
  `queuePos` / `queueCursor` / `hasCurrent` consistent.

- **ANSI width wrapping.** lipgloss wraps an ANSI-colored line that's wider than its
  container's text area onto a second (background-colored) line — invisible when
  output is piped, visible in a real terminal. Inside a box with `Padding(0,1)`, pad
  styled rows to `inner-2`, and measure widths with `lipgloss.Width` (not rune
  count). Use `truncate2`/`padRight`, not `truncate`, for styled content.

- **Queue inserts are deduped by track ID** (`enqueue`/`playNow`/`appendNew` in
  model.go): queueing an existing track is a no-op (playNow jumps to the existing
  entry), and radio/auto-continue batches drop tracks already queued. Don't add a
  new queue-insert path that bypasses these.

- **One standard track row.** Every song list renders through
  `renderTrackRow` (render.go): marker, optional per-view prefix (queue:
  now-playing glyph; history: played-at timestamp), number, heart,
  title • artist • album, right-aligned duration. New views must reuse it, not
  hand-roll a row layout.

- **Keyboard routing.** Global playback keys fire in every view *unless*
  `m.typing()` (editing the search box). Space is matched by both `tea.KeySpace` and
  `" "`. When adding a key, update both the handler (`model.go`), the context-aware
  shortcuts bar, and the Help screen (`render.go`) so they stay in sync.

- **Config writes are atomic** (temp file + rename) and a corrupt config is backed up
  to `.corrupt` rather than wiping favorites/history. Config is only touched from the
  single bubbletea `Update` goroutine.

- **Player shutdown is concurrency-safe.** `sendCh` is never closed; `send()` and
  `writeLoop` gate on a `closed` channel and `Close()` is idempotent. A `loadGen`
  epoch cancels stale loads after a fast skip. Don't reintroduce `close(sendCh)`.

## Testing

`internal/tui/keys_test.go` builds a model with a `nil` player (`New(nil, cfg)`), so
tests must not press keys that call player methods (space/n/b/seek/volume) — exercise
key *routing*, state transitions, and rendering instead. Player/mpv behavior is
verified with throwaway tests guarded by `exec.LookPath("mpv")` (skip if absent);
delete them when done.
