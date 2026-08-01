package player

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// loadTimeout caps how long a single yt-dlp stream extraction may run before it
// is abandoned, so a hung extraction can't pin the player in "loading" forever.
const loadTimeout = 60 * time.Second

// socketPathFor returns a per-process IPC socket path. The mpv IPC socket
// accepts arbitrary commands (including `run`, i.e. code execution), so it goes
// in $XDG_RUNTIME_DIR — a per-user 0700 directory — rather than the shared /tmp,
// where another local user could connect if the umask allowed it. The pid suffix
// keeps multiple instances from colliding.
func socketPathFor() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, fmt.Sprintf("ytmusic-mpv-%d.sock", os.Getpid()))
}

type State struct {
	Position float64
	Duration float64
	Volume   float64
	Paused   bool
	Idle     bool
	Loading  bool
	Muted    bool
}

type Player struct {
	cmd        *exec.Cmd
	procMu     sync.Mutex // serializes mpv process kill/spawn (recover vs Close)
	sockPath   string
	conn       net.Conn
	mu         sync.Mutex  // protects state, conn, reqID, loadGen, loadCancel, playingID, urlCache, inflight
	sendCh     chan []byte // serialized writes
	reqID      int
	loadGen    int                // epoch token: cancels stale async loads
	loadCancel context.CancelFunc // cancels the in-flight load's yt-dlp, if any
	playingID  string             // videoID last handed to mpv via loadfile (cache invalidation on stream error)
	state      State
	lastErr    string               // most recent load/playback failure, for the UI
	restarted  bool                 // set after an automatic mpv respawn; polled by the TUI
	alive      bool                 // readLoop running; false once recovery has given up
	respawns   int                  // consecutive respawn attempts (reset after a stable session)
	urlCache   map[string]cachedURL // videoID -> resolved stream URL (prefetch)
	inflight   map[string]struct{}  // videoIDs with a resolve in progress (prefetch dedup)
	done       chan struct{}        // signalled on natural end-of-file
	closed     chan struct{}        // closed once on shutdown
	closeOnce  sync.Once
	baseCtx    context.Context    // parent of every resolve ctx; cancelled by Close
	baseCancel context.CancelFunc // set in New, then immutable
}

// cachedURL is a yt-dlp-resolved stream URL with the time it was resolved.
// googlevideo URLs carry an `expire` param (hours out); urlTTL keeps us well
// inside that window so a prefetched URL is never stale when finally played.
type cachedURL struct {
	url string
	at  time.Time
}

const urlTTL = 30 * time.Minute

type ipcCmd struct {
	Command   []any `json:"command"`
	RequestID int   `json:"request_id"`
}

type ipcResp struct {
	Error     string `json:"error"`
	Data      any    `json:"data"`
	RequestID int    `json:"request_id"`
	Event     string `json:"event"`
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Reason    string `json:"reason"`
}

// spawnMPV starts a fresh mpv subprocess serving IPC on sockPath. Shared by the
// initial startup and crash-recovery respawn so both run identical flags.
func spawnMPV(sockPath string) (*exec.Cmd, error) {
	// MPRIS is served in-process (see internal/mpris); the mpv-mpris plugin is no
	// longer loaded, so media-key controls map straight to the app's queue.
	args := []string{
		"--no-video",
		"--idle=yes",
		"--input-ipc-server=" + sockPath,
		"--really-quiet",
		"--no-terminal",

		// Memory footprint: mpv's default demuxer cache (150 MiB forward +
		// 50 MiB back) is sized for video. For audio-only streaming at up to
		// 320 kbps (≈ 40 KB/s) a 4 MiB forward buffer holds ~100 s ahead,
		// which is more than sufficient and cuts RSS by ~80–90 MB.
		"--demuxer-max-bytes=4MiB",
		"--demuxer-max-back-bytes=2MiB",

		// We resolve stream URLs ourselves via yt-dlp; suppress mpv's own
		// ytdl hook (a Lua script that would otherwise be loaded and run the
		// separate yt-dlp path in parallel, wasting a Lua runtime and memory).
		"--ytdl=no",

		// Don't load user-installed mpv scripts or the on-screen controller —
		// both irrelevant for a headless audio-only subprocess.
		"--load-scripts=no",
		"--osc=no",

		// Robustness: googlevideo connections get reset routinely mid-stream.
		// Let ffmpeg transparently reconnect instead of surfacing a transient
		// drop as a fatal stream error, and fail a truly dead connection in
		// seconds (default 60) so the TUI's retry path kicks in promptly.
		"--stream-lavf-o=reconnect=1,reconnect_streamed=1,reconnect_delay_max=5",
		"--network-timeout=15",
	}

	cmd := exec.Command("mpv", args...)
	// glibc creates up to 8×ncores 64 MiB malloc arenas for a threaded process
	// and rarely returns freed pages, which alone accounts for tens of MB of
	// mpv's RSS. One arena is plenty for audio decoding (allocation contention
	// is negligible at ~44 kHz), and a low trim threshold hands freed pages
	// back to the kernel between tracks.
	cmd.Env = append(os.Environ(),
		"MALLOC_ARENA_MAX=1",
		"MALLOC_TRIM_THRESHOLD_=131072",
	)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mpv: %w", err)
	}
	return cmd, nil
}

// New starts mpv and restores the given initial volume (0–150) once connected.
func New(volume float64) (*Player, error) {
	sockPath := socketPathFor()
	os.Remove(sockPath) //nolint:errcheck

	cmd, err := spawnMPV(sockPath)
	if err != nil {
		return nil, err
	}

	p := &Player{
		cmd:      cmd,
		sockPath: sockPath,
		sendCh:   make(chan []byte, 64),
		done:     make(chan struct{}, 1),
		closed:   make(chan struct{}),
		alive:    true,
		state: State{
			Volume: volume,
			Idle:   true,
		},
	}

	conn, err := dialWithRetry(sockPath, 30, 100*time.Millisecond)
	if err != nil {
		reap(cmd)
		return nil, fmt.Errorf("connect mpv IPC: %w", err)
	}
	p.conn = conn
	p.baseCtx, p.baseCancel = context.WithCancel(context.Background())

	go p.writeLoop()
	go p.readLoop()

	p.observeProperties()

	// Restore the saved volume (mpv starts at 100 by default).
	p.SetVolume(volume) //nolint:errcheck

	return p, nil
}

// observeProperties (re)subscribes the property observations the state snapshot
// depends on. Must run after every new IPC connection.
func (p *Player) observeProperties() {
	p.send([]any{"observe_property", 1, "time-pos"}) //nolint:errcheck
	p.send([]any{"observe_property", 2, "duration"}) //nolint:errcheck
	p.send([]any{"observe_property", 3, "pause"}) //nolint:errcheck
	p.send([]any{"observe_property", 4, "volume"}) //nolint:errcheck
	p.send([]any{"observe_property", 5, "idle-active"}) //nolint:errcheck
	p.send([]any{"observe_property", 6, "mute"}) //nolint:errcheck
}

func dialWithRetry(path string, attempts int, delay time.Duration) (net.Conn, error) {
	var (
		conn net.Conn
		err  error
	)
	for i := 0; i < attempts; i++ {
		conn, err = net.Dial("unix", path)
		if err == nil {
			return conn, nil
		}
		time.Sleep(delay)
	}
	return nil, err
}

// reap kills c and waits for it so a dead mpv never lingers as a zombie. Nil-safe
// and idempotent: Kill on an already-exited process and a repeated Wait both just
// return errors we don't care about.
func reap(c *exec.Cmd) {
	if c == nil || c.Process == nil {
		return
	}
	c.Process.Kill() //nolint:errcheck
	c.Wait() //nolint:errcheck
}

func (p *Player) writeLoop() {
	for {
		select {
		case <-p.closed:
			return
		case b := <-p.sendCh:
			p.mu.Lock()
			conn := p.conn
			p.mu.Unlock()
			if conn != nil {
				conn.Write(b) //nolint:errcheck
			}
		}
	}
}

// beginLoad resets playback state and returns the new load epoch plus a context
// scoped to this load. A later load (or skip) bumps the epoch and cancels the
// previous context, so a slow/stale yt-dlp extraction is killed immediately
// instead of running to completion and piling up concurrent requests.
func (p *Player) beginLoad(videoID string) (int, context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loadCancel != nil {
		p.loadCancel() // kill any in-flight yt-dlp from the superseded load
	}
	p.loadGen++
	ctx, cancel := context.WithTimeout(p.baseCtx, loadTimeout)
	p.loadCancel = cancel
	p.state.Position = 0
	p.state.Duration = 0
	p.state.Idle = false
	p.state.Loading = true
	select {
	case <-p.done:
	default:
	}
	return p.loadGen, ctx
}

func (p *Player) Load(videoID string) error {
	gen, ctx := p.beginLoad(videoID)

	// Cache hit (prefetched while the previous track played): skip yt-dlp and
	// hand the URL straight to mpv — the common auto-advance path is instant.
	if url, ok := p.cacheGet(videoID); ok {
		p.mu.Lock()
		if p.loadCancel != nil {
			p.loadCancel() // no extraction for this load; release its ctx timer
			p.loadCancel = nil
		}
		p.playingID = videoID
		p.mu.Unlock()
		return p.send([]any{"loadfile", url, "replace"})
	}

	// Cache miss: the resolve takes seconds, and mpv would keep playing the
	// previous track the whole time — a skip must silence it immediately.
	// (end-file reason "stop" is ignored by scan, so this can't fake a track
	// ending; the loadfile below restarts playback when the URL arrives.)
	p.send([]any{"stop"}) //nolint:errcheck

	go func() {
		// Deliberately not deduped against an in-flight Prefetch of the same id:
		// a load must deliver loadfile as soon as its URL resolves, and waiting on
		// the prefetch would need per-id signaling. Worst case is one extra
		// resolve when a track is played while its prefetch is still running.
		url, resolved, err := extractURL(ctx, videoID)
		if resolved {
			p.cachePut(videoID, url)
		}

		p.mu.Lock()
		stale := gen != p.loadGen
		if stale {
			p.mu.Unlock()
			return // a newer load superseded this one
		}
		if p.loadCancel != nil {
			p.loadCancel() // extraction is done either way; release its ctx timer
			p.loadCancel = nil
		}
		if err != nil {
			p.state.Loading = false
			if errors.Is(err, errNoYtdlp) {
				p.lastErr = "yt-dlp not found in PATH"
			} else {
				p.lastErr = "could not resolve stream: " + err.Error()
			}
			p.mu.Unlock()
			return
		}
		p.playingID = videoID
		p.mu.Unlock()

	p.send([]any{"loadfile", url, "replace"}) //nolint:errcheck
	}()
	return nil
}

// Prefetch resolves a stream URL in the background and caches it so a later
// Load(videoID) is instant. Safe to call repeatedly and concurrently: it is a
// no-op on a cache hit or when a resolve for the same id is already running, so
// rapid re-triggers (e.g. queue reordering) never spawn duplicate yt-dlp
// processes for the same video — which would otherwise multiply requests to
// YouTube and risk rate limiting.
func (p *Player) Prefetch(videoID string) {
	if videoID == "" || !p.claimResolve(videoID) {
		return
	}
	go func() {
		defer p.releaseResolve(videoID)
		// Own timeout, deliberately not tied to loadCancel: a prefetch warms a
		// future track and must survive the current load being superseded.
		ctx, cancel := context.WithTimeout(p.baseCtx, loadTimeout)
		defer cancel()
		if url, resolved, _ := extractURL(ctx, videoID); resolved {
			p.cachePut(videoID, url)
		}
	}()
}

// claimResolve reports whether videoID needs a fresh resolve, marking it
// in-flight if so. It returns false when the URL is already cached or another
// resolve for the same id is already running, so each video is resolved at most
// once at a time.
func (p *Player) claimResolve(videoID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.cachedLocked(videoID); ok {
		return false // already resolved and still fresh
	}
	if _, ok := p.inflight[videoID]; ok {
		return false
	}
	if p.inflight == nil {
		p.inflight = make(map[string]struct{})
	}
	p.inflight[videoID] = struct{}{}
	return true
}

func (p *Player) releaseResolve(videoID string) {
	p.mu.Lock()
	delete(p.inflight, videoID)
	p.mu.Unlock()
}

// cachedLocked returns videoID's cached URL if present and still inside urlTTL.
// Caller holds p.mu.
func (p *Player) cachedLocked(videoID string) (string, bool) {
	c, ok := p.urlCache[videoID]
	return c.url, ok && time.Since(c.at) <= urlTTL
}

func (p *Player) cacheGet(videoID string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cachedLocked(videoID)
}

func (p *Player) cachePut(videoID, url string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.urlCache == nil {
		p.urlCache = make(map[string]cachedURL)
	}
	// Evict expired entries so a long session doesn't grow the cache unbounded
	// (stale entries are otherwise only skipped on read, never removed).
	for id, c := range p.urlCache {
		if time.Since(c.at) > urlTTL {
			delete(p.urlCache, id)
		}
	}
	p.urlCache[videoID] = cachedURL{url: url, at: time.Now()}
}

// errNoYtdlp marks a resolve failure caused by yt-dlp being absent from PATH,
// so the UI can show an actionable message instead of a generic one.
var errNoYtdlp = errors.New("yt-dlp not found in PATH")

// ytdlpPath resolves yt-dlp's location once: PATH doesn't change under a running
// process, and LookPath otherwise re-walked it on every Load miss and Prefetch.
var ytdlpPath = sync.OnceValues(func() (string, error) { return exec.LookPath("yt-dlp") })

// extractURL resolves a direct stream URL for videoID via yt-dlp. There is no
// watch-page-URL fallback: mpv runs with --ytdl=no, so handing it a watch page
// is a guaranteed playback error — failing fast here lets the caller's
// retry/skip logic react instead.
func extractURL(ctx context.Context, videoID string) (url string, resolved bool, err error) {
	// The id is interpolated into a URL handed to yt-dlp/mpv; reject anything
	// outside the YouTube id alphabet so a malformed API value can't smuggle
	// extra URL components or odd argv content into the subprocesses.
	if !validVideoID(videoID) {
		return "", false, fmt.Errorf("invalid video id %q", videoID)
	}
	ytURL := "https://www.youtube.com/watch?v=" + videoID

	ytdlp, lookErr := ytdlpPath()
	if lookErr != nil {
		return "", false, errNoYtdlp
	}

	// Drop the `tv` client from yt-dlp's default client set: it adds ~0.5s of
	// extraction latency and isn't needed for audio. The remaining default
	// clients still resolve a bestaudio stream.
	out, err := exec.CommandContext(ctx, ytdlp,
		"--extractor-args", "youtube:player_client=default,-tv",
		"-f", "bestaudio", "-g", ytURL).Output()
	if err != nil {
		// Output() stashes stderr on ExitError; surface its tail so age-gated,
		// geo-blocked and bot-check failures stay distinguishable in the UI.
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if reason := ytdlpReason(ee.Stderr); reason != "" {
				return "", false, fmt.Errorf("yt-dlp: %s", reason)
			}
		}
		return "", false, err
	}
	url = strings.TrimSpace(string(bytes.TrimRight(out, "\n")))
	if url == "" {
		return "", false, fmt.Errorf("yt-dlp returned no URL")
	}
	return url, true, nil
}

// ytdlpReason condenses yt-dlp's stderr to its last non-empty line, capped so a
// verbose traceback can't flood the one-line UI error slot.
func ytdlpReason(stderr []byte) string {
	lines := strings.Split(strings.TrimSpace(string(stderr)), "\n")
	msg := strings.TrimSpace(lines[len(lines)-1])
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

// validVideoID reports whether s looks like a YouTube video id (URL-safe base64
// alphabet). Length is left loose on purpose — ids are 11 chars today, but the
// format isn't contractual.
func validVideoID(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func (p *Player) PlayPause() error {
	return p.send([]any{"cycle", "pause"})
}

// Play resumes playback (MPRIS Play — explicit, unlike the PlayPause toggle).
func (p *Player) Play() error {
	return p.send([]any{"set_property", "pause", false})
}

// Pause pauses playback (MPRIS Pause — explicit, unlike the PlayPause toggle).
func (p *Player) Pause() error {
	return p.send([]any{"set_property", "pause", true})
}

// ToggleMute flips mpv's mute flag. mpv keeps mute independent of volume, so the
// stored volume level (and its display) is unaffected.
func (p *Player) ToggleMute() error {
	return p.send([]any{"cycle", "mute"})
}

func (p *Player) Seek(seconds float64) error {
	return p.send([]any{"seek", seconds, "relative"})
}

// SeekAbs seeks to an absolute position in seconds (MPRIS SetPosition).
func (p *Player) SeekAbs(seconds float64) error {
	return p.send([]any{"seek", seconds, "absolute"})
}

func (p *Player) SetVolume(vol float64) error {
	if vol > 150 {
		vol = 150
	}
	if vol < 0 {
		vol = 0
	}
	return p.send([]any{"set_property", "volume", vol})
}

func (p *Player) VolumeUp() error {
	p.mu.Lock()
	vol := p.state.Volume + 5
	p.mu.Unlock()
	return p.SetVolume(vol)
}

func (p *Player) VolumeDown() error {
	p.mu.Lock()
	vol := p.state.Volume - 5
	p.mu.Unlock()
	return p.SetVolume(vol)
}

func (p *Player) Stop() error {
	// Cancel any in-flight load and bump the epoch so a pending yt-dlp extraction
	// can't fire loadfile after the stop and resurrect playback (e.g. clearing the
	// queue while a track was still resolving).
	p.mu.Lock()
	if p.loadCancel != nil {
		p.loadCancel()
		p.loadCancel = nil
	}
	p.loadGen++
	p.state.Loading = false
	p.mu.Unlock()
	return p.send([]any{"stop"})
}

// SetTitle sets mpv's force-media-title so the stream shows a clean track name
// (e.g. in mpv logs) instead of the raw URL. Now-playing metadata for MPRIS
// shells is published separately by internal/mpris.
func (p *Player) SetTitle(title string) error {
	if title == "" {
		return nil
	}
	return p.send([]any{"set_property", "force-media-title", title})
}

func (p *Player) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// LoadError returns (and clears) the most recent load/playback failure message,
// or "" when none occurred since the last call. Polled by the TUI tick so
// failures surface in the status line instead of dying silently.
func (p *Player) LoadError() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := p.lastErr
	p.lastErr = ""
	return e
}

// Restarted reports (and clears) whether the mpv process was automatically
// respawned since the last call. The TUI uses it to reload the current track,
// since all playback state died with the old process.
func (p *Player) Restarted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	r := p.restarted
	p.restarted = false
	return r
}

// Alive reports whether the player still has (or can recover) an mpv process.
// False only after recovery gave up; at that point commands go nowhere and the
// UI should stop retrying.
func (p *Player) Alive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.alive
}

// TrackEnded returns true (and resets) if a track-end event was received since last call.
func (p *Player) TrackEnded() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *Player) send(cmd []any) error {
	p.mu.Lock()
	p.reqID++
	id := p.reqID
	p.mu.Unlock()

	msg := ipcCmd{Command: cmd, RequestID: id}
	b, _ := json.Marshal(msg)
	b = append(b, '\n')

	// sendCh is never closed. This was a three-way select including <-p.closed,
	// but Go picks uniformly among ready cases, so post-Close sends still enqueued
	// half the time — and the default made the closed case useless as an unblocker.
	if p.isClosed() {
		return nil
	}
	select {
	case p.sendCh <- b:
	default:
		// channel full (writeLoop wedged); drop the command rather than block
	}
	return nil
}

// maxRespawns caps consecutive mpv respawn attempts so a crash-looping mpv
// (broken install, repeated OOM kills) can't spin forever. The counter resets
// after a session that stayed up for stableSession.
const (
	maxRespawns   = 3
	stableSession = 60 * time.Second
)

func (p *Player) isClosed() bool {
	select {
	case <-p.closed:
		return true
	default:
		return false
	}
}

// readLoop reads IPC events for the lifetime of the player, surviving both
// dropped connections (redial) and mpv process death (respawn). It exits only
// on shutdown or once the respawn budget is exhausted — in which case the
// failure is surfaced to the UI rather than dying silently.
func (p *Player) readLoop() {
	for {
		p.mu.Lock()
		conn := p.conn
		p.mu.Unlock()
		if conn == nil {
			// Only Close nils conn — a normal shutdown, not an engine failure.
			return
		}

		start := time.Now()
		p.scan(conn)

		if p.isClosed() {
			return
		}
		if time.Since(start) >= stableSession {
			p.mu.Lock()
			p.respawns = 0 // mpv was stable for a while; forgive past crashes
			p.mu.Unlock()
		}
		if !p.recover() {
			break
		}
	}

	p.mu.Lock()
	p.alive = false
	p.state.Idle = true
	p.state.Loading = false
	p.lastErr = "audio engine failed — restart the app"
	p.mu.Unlock()
}

// scan consumes IPC events from one connection until it drops.
func (p *Player) scan(conn net.Conn) {
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var resp ipcResp
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			continue
		}

		switch resp.Event {
		case "end-file":
			// Only a natural end-of-file means the track finished. Loading a new
			// file emits end-file with reason "stop"/"redirect" for the previous
			// one — ignore those so we don't falsely advance the queue.
			switch resp.Reason {
			case "eof":
				select {
				case p.done <- struct{}{}:
				default:
				}
			case "error":
				// mpv couldn't play the stream (expired URL, network, bad format).
				// Drop the cached URL for this track so a retry resolves a fresh
				// one instead of replaying the same dead URL.
				p.mu.Lock()
				p.state.Loading = false
				p.lastErr = "playback failed (stream error)"
				delete(p.urlCache, p.playingID)
				p.mu.Unlock()
			}
			continue

		case "start-file":
			p.mu.Lock()
			p.state.Loading = false
			p.mu.Unlock()
			continue

		case "property-change":
			p.mu.Lock()
			switch resp.ID {
			case 1:
				if v, ok := resp.Data.(float64); ok {
					p.state.Position = v
					if v > 0 {
						p.state.Loading = false
					}
				}
			case 2:
				if v, ok := resp.Data.(float64); ok {
					p.state.Duration = v
				}
			case 3:
				if v, ok := resp.Data.(bool); ok {
					p.state.Paused = v
				}
			case 4:
				if v, ok := resp.Data.(float64); ok {
					p.state.Volume = v
				}
			case 5:
				if v, ok := resp.Data.(bool); ok {
					p.state.Idle = v
				}
			case 6:
				if v, ok := resp.Data.(bool); ok {
					p.state.Muted = v
				}
			}
			p.mu.Unlock()
		}
	}
}

// recover re-establishes IPC after a disconnect: first by redialing (mpv alive,
// connection dropped), then by respawning the mpv process (it died). Returns
// false on shutdown or once the respawn budget is spent.
func (p *Player) recover() bool {
	// The connection may have dropped while mpv itself is fine — try redialing.
	for i := 0; i < 5; i++ {
		if p.isClosed() {
			return false
		}
		if conn, err := net.Dial("unix", p.sockPath); err == nil {
			p.adoptConn(conn)
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Socket gone — mpv is dead. Respawn it.
	for {
		p.procMu.Lock()
		if p.isClosed() {
			p.procMu.Unlock()
			return false
		}
		p.mu.Lock()
		if p.respawns >= maxRespawns {
			p.mu.Unlock()
			p.procMu.Unlock()
			return false
		}
		p.respawns++
		p.mu.Unlock()

		reap(p.cmd)
		os.Remove(p.sockPath) //nolint:errcheck

		cmd, err := spawnMPV(p.sockPath)
		if err == nil {
			conn, derr := dialWithRetry(p.sockPath, 30, 100*time.Millisecond)
			if derr == nil {
				p.cmd = cmd
				p.mu.Lock()
				// Playback state died with the old process; keep user settings.
				p.state = State{Volume: p.state.Volume, Muted: p.state.Muted, Idle: true}
				p.restarted = true
				p.mu.Unlock()
				p.procMu.Unlock()
				p.adoptConn(conn)
				return true
			}
			reap(cmd)
		}
		p.procMu.Unlock()
		time.Sleep(time.Second)
	}
}

// adoptConn swaps in a new IPC connection and replays per-connection setup
// (property observation, volume/mute restore).
func (p *Player) adoptConn(conn net.Conn) {
	if p.isClosed() {
		conn.Close() //nolint:errcheck
		return
	}
	p.mu.Lock()
	if p.conn != nil {
		p.conn.Close() //nolint:errcheck
	}
	p.conn = conn
	vol := p.state.Volume
	muted := p.state.Muted
	p.mu.Unlock()

	p.observeProperties()
	p.SetVolume(vol) //nolint:errcheck
	p.send([]any{"set_property", "mute", muted}) //nolint:errcheck
}

// Close shuts the player down. Safe to call more than once.
func (p *Player) Close() {
	p.closeOnce.Do(func() {
		close(p.closed) // stops writeLoop and the reconnect loop; gates send()
		p.baseCancel()  // kills every in-flight yt-dlp: current load and prefetches

		p.mu.Lock()
		conn := p.conn
		p.conn = nil
		p.mu.Unlock()

		if conn != nil {
			conn.Close() //nolint:errcheck
		}
		// procMu: if recover() is mid-respawn it finishes first, so the process
		// killed here is the current one — a freshly respawned mpv can't leak.
		p.procMu.Lock()
		reap(p.cmd)
		p.procMu.Unlock()
		os.Remove(p.sockPath) //nolint:errcheck
	})
}
