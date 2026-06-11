package player

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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
	sockPath   string
	conn       net.Conn
	mu         sync.Mutex  // protects state, conn, reqID, loadGen, loadCancel, urlCache, inflight
	sendCh     chan []byte // serialized writes
	reqID      int
	loadGen    int                // epoch token: cancels stale async loads
	loadCancel context.CancelFunc // cancels the in-flight load's yt-dlp, if any
	state      State
	lastErr    string               // most recent load/playback failure, for the UI
	urlCache   map[string]cachedURL // videoID -> resolved stream URL (prefetch)
	inflight   map[string]struct{}  // videoIDs with a resolve in progress (prefetch dedup)
	done       chan struct{}        // signalled on natural end-of-file
	closed     chan struct{}        // closed once on shutdown
	closeOnce  sync.Once
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

// New starts mpv and restores the given initial volume (0–150) once connected.
func New(volume float64) (*Player, error) {
	sockPath := socketPathFor()
	os.Remove(sockPath)

	// MPRIS is served in-process (see internal/mpris); the mpv-mpris plugin is no
	// longer loaded, so media-key controls map straight to the app's queue.
	args := []string{
		"--no-video",
		"--idle=yes",
		"--input-ipc-server=" + sockPath,
		"--really-quiet",
		"--no-terminal",
	}

	cmd := exec.Command("mpv", args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mpv: %w", err)
	}

	p := &Player{
		cmd:      cmd,
		sockPath: sockPath,
		sendCh:   make(chan []byte, 64),
		done:     make(chan struct{}, 1),
		closed:   make(chan struct{}),
		state: State{
			Volume: volume,
			Idle:   true,
		},
	}

	conn, err := dialWithRetry(sockPath, 30, 100*time.Millisecond)
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait() // reap; don't leave a zombie until the app exits
		return nil, fmt.Errorf("connect mpv IPC: %w", err)
	}
	p.conn = conn

	go p.writeLoop()
	go p.readLoop()

	p.send([]any{"observe_property", 1, "time-pos"})
	p.send([]any{"observe_property", 2, "duration"})
	p.send([]any{"observe_property", 3, "pause"})
	p.send([]any{"observe_property", 4, "volume"})
	p.send([]any{"observe_property", 5, "idle-active"})
	p.send([]any{"observe_property", 6, "mute"})

	// Restore the saved volume (mpv starts at 100 by default).
	p.SetVolume(volume)

	return p, nil
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
func (p *Player) beginLoad() (int, context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loadCancel != nil {
		p.loadCancel() // kill any in-flight yt-dlp from the superseded load
	}
	p.loadGen++
	ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
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
	gen, ctx := p.beginLoad()

	// Cache hit (prefetched while the previous track played): skip yt-dlp and
	// hand the URL straight to mpv — the common auto-advance path is instant.
	if url, ok := p.cacheGet(videoID); ok {
		p.mu.Lock()
		if p.loadCancel != nil {
			p.loadCancel() // no extraction for this load; release its ctx timer
			p.loadCancel = nil
		}
		p.mu.Unlock()
		return p.send([]any{"loadfile", url, "replace"})
	}

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
		if err != nil {
			p.state.Loading = false
			p.lastErr = "could not resolve stream (yt-dlp failed)"
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()

		p.send([]any{"loadfile", url, "replace"})
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
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
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
	if c, ok := p.urlCache[videoID]; ok && time.Since(c.at) <= urlTTL {
		return false
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

func (p *Player) cacheGet(videoID string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.urlCache[videoID]
	if !ok || time.Since(c.at) > urlTTL {
		return "", false
	}
	return c.url, true
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

// LoadURL sends a pre-resolved URL directly to mpv without yt-dlp extraction.
func (p *Player) LoadURL(url string) error {
	p.beginLoad()
	return p.send([]any{"loadfile", url, "replace"})
}

// extractURL resolves a direct stream URL for videoID via yt-dlp. resolved is
// false when it falls back to the raw watch-page URL (yt-dlp missing, failed,
// or cancelled) — fallbacks must not be cached, or a transient failure would
// pin the slow page-URL path for the whole TTL and suppress retries.
func extractURL(ctx context.Context, videoID string) (url string, resolved bool, err error) {
	// The id is interpolated into a URL handed to yt-dlp/mpv; reject anything
	// outside the YouTube id alphabet so a malformed API value can't smuggle
	// extra URL components or odd argv content into the subprocesses.
	if !validVideoID(videoID) {
		return "", false, fmt.Errorf("invalid video id %q", videoID)
	}
	ytURL := "https://www.youtube.com/watch?v=" + videoID

	ytdlp, lookErr := exec.LookPath("yt-dlp")
	if lookErr != nil {
		// yt-dlp not in PATH; return raw URL, mpv may handle via its ytdl hook
		return ytURL, false, nil
	}

	// Drop the `tv` client from yt-dlp's default client set: it adds ~0.5s of
	// extraction latency and isn't needed for audio. The remaining default
	// clients still resolve a bestaudio stream.
	out, err := exec.CommandContext(ctx, ytdlp,
		"--extractor-args", "youtube:player_client=default,-tv",
		"-f", "bestaudio", "-g", ytURL).Output()
	if err != nil {
		// Cancelled/timed-out extraction returns an error; the raw watch URL is a
		// best-effort fallback (the caller drops it if this load is now stale).
		return ytURL, false, err
	}
	url = strings.TrimSpace(string(bytes.TrimRight(out, "\n")))
	if url == "" {
		return ytURL, false, nil
	}
	return url, true, nil
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

	// sendCh is never closed; the closed channel guards against enqueueing after
	// shutdown (and unblocks if writeLoop has already exited with a full buffer).
	select {
	case <-p.closed:
	case p.sendCh <- b:
	default:
		// channel full; drop command
	}
	return nil
}

func (p *Player) readLoop() {
	const maxReconnects = 5

	for attempt := 0; attempt <= maxReconnects; attempt++ {
		p.mu.Lock()
		conn := p.conn
		p.mu.Unlock()

		if conn == nil {
			return
		}

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
					p.mu.Lock()
					p.state.Loading = false
					p.lastErr = "playback failed (stream error)"
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

		// scanner exited — connection lost. Stop if we're shutting down.
		select {
		case <-p.closed:
			return
		default:
		}
		if attempt == maxReconnects {
			break
		}
		time.Sleep(500 * time.Millisecond)
		select {
		case <-p.closed:
			return
		default:
		}

		newConn, err := net.Dial("unix", p.sockPath)
		if err != nil {
			continue
		}

		p.mu.Lock()
		if p.conn != nil {
			p.conn.Close()
		}
		p.conn = newConn
		p.mu.Unlock()

		// re-subscribe properties on new connection
		p.send([]any{"observe_property", 1, "time-pos"})
		p.send([]any{"observe_property", 2, "duration"})
		p.send([]any{"observe_property", 3, "pause"})
		p.send([]any{"observe_property", 4, "volume"})
		p.send([]any{"observe_property", 5, "idle-active"})
		p.send([]any{"observe_property", 6, "mute"})

		// Re-apply the last known volume so it survives an mpv restart.
		p.mu.Lock()
		vol := p.state.Volume
		p.mu.Unlock()
		p.SetVolume(vol)
	}
}

// Close shuts the player down. Safe to call more than once.
func (p *Player) Close() {
	p.closeOnce.Do(func() {
		close(p.closed) // stops writeLoop and the reconnect loop; gates send()

		p.mu.Lock()
		if p.loadCancel != nil {
			p.loadCancel() // kill any in-flight yt-dlp extraction
		}
		conn := p.conn
		p.conn = nil
		p.mu.Unlock()

		if conn != nil {
			conn.Close()
		}
		if p.cmd != nil && p.cmd.Process != nil {
			p.cmd.Process.Kill()
			p.cmd.Wait()
		}
		os.Remove(p.sockPath)
	})
}
